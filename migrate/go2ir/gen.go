/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package go2ir

import (
	"go/types"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"google.golang.org/protobuf/proto"

	crdmarkers "sigs.k8s.io/controller-tools/pkg/crd/markers"
	"sigs.k8s.io/controller-tools/pkg/genall"
	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
	ir "k8s.io/idl/ckdl-ir/goir"
	irt "k8s.io/idl/ckdl-ir/goir/types"
	irgv "k8s.io/idl/ckdl-ir/goir/groupver"
)

// +controllertools:marker:generateHelp

// Generator generates (experimental) Kubernetes API intermediate representation protos.
type Generator struct {
	// AllowDangerousTypes allows types which are usually omitted from CRD generation
	// because they are not recommended.
	//
	// Currently the following additional types are allowed when this is true:
	// float32
	// float64
	//
	// Left unspecified, the default is false
	AllowDangerousTypes *bool `marker:",optional"`

	// IgnorePrefix causes a given package's import path to be
	// stripped of the given prefix when recording it for dependency
	// information.
	IgnorePrefix string `marker:",optional"`
}

func (Generator) RegisterMarkers(into *markers.Registry) error {
	defn, err := markers.MakeDefinition("k8s:deepcopy-gen", markers.DescribesPackage, "")
	if err != nil {
		return err
	}
	if err := into.Register(defn); err != nil {
		return err
	}
	return crdmarkers.Register(into)
}

func (Generator) CheckFilter() loader.NodeFilter {
	return filterTypesForCRDs
}

func (g Generator) Generate(ctx *genall.GenerationContext) error {
	parser := &Parser{
		Collector: ctx.Collector,
		Checker:   ctx.Checker,
		// Perform defaulting here to avoid ambiguity later
		AllowDangerousTypes: g.AllowDangerousTypes != nil && *g.AllowDangerousTypes == true,
	}

	AddKnownTypes(parser)
	for _, root := range ctx.Roots {
		parser.NeedPackage(root)
	}

	metav1Pkg := FindMetav1(ctx.Roots)
	if metav1Pkg == nil {
		// no objects in the roots, since nothing imported metav1
		return nil
	}

	// TODO: allow selecting a specific object
	kubeKinds := FindKubeKinds(parser, metav1Pkg)
	if len(kubeKinds) == 0 {
		// no objects in the roots
		return nil
	}

	for groupKind := range kubeKinds {
		var pkgs []*loader.Package
		for pkg, gv := range parser.GroupVersions {
			if gv.Group != groupKind.Group {
				continue
			}
			pkgs = append(pkgs, pkg)
		}
		for _, pkg := range pkgs {
			parser.NeedKindDescFor(TypeIdent{Package: pkg, Name: groupKind.Kind})
		}
	}

	byPkg := make(map[*loader.Package]*ir.GroupVersion)
	for ident, kind := range parser.Kinds {
		if byPkg[ident.Package] == nil {
			byPkg[ident.Package] = &ir.GroupVersion{}
		}
		byPkg[ident.Package].Kinds = append(byPkg[ident.Package].Kinds, kind)
	}
	for ident, subtype := range parser.Subtypes {
		if byPkg[ident.Package] == nil {
			byPkg[ident.Package] = &ir.GroupVersion{}
		}
		if subtype == nil {
			// skip subtypes that aren't supposed to exist
			continue
		}
		byPkg[ident.Package].Types = append(byPkg[ident.Package].Types, subtype)
	}

	for pkg, gv := range byPkg {
		gvRef := parser.GroupVersions[pkg]
		gv.Description = &irgv.GroupVersion{
			Group: gvRef.Group,
			Version: gvRef.Version,
			// TODO: doc?
		}
		var deps []*ir.Partial_Dependency
		for depGV, depPkg := range parser.Deps[gvRef] {
			importPath := strings.TrimLeft(strings.TrimPrefix(depPkg.PkgPath, g.IgnorePrefix), "/")
			deps = append(deps, &ir.Partial_Dependency{
				GroupVersion: &irt.GroupVersionRef{Group: depGV.Group, Version: depGV.Version},
				From: importPath+"/types.kdl",
			})
		}
		set := ir.Partial{
			GroupVersions: []*ir.GroupVersion{gv},
			Dependencies: deps,
		}

		outBytes, err := proto.Marshal(&set)
		if err != nil {
			return err
		}

		outFile, err := ctx.Open(pkg, "types.ckdl")
		if err != nil {
			return err
		}
		defer outFile.Close()
		n, err := outFile.Write(outBytes)
		if err != nil {
			return err
		}
		if n < len(outBytes) {
			return io.ErrShortWrite
		}
	}

	return nil
}

// FindKubeKinds locates all types that contain TypeMeta and ObjectMeta
// (and thus may be a Kubernetes object), and returns the corresponding
// group-kinds.
func FindKubeKinds(parser *Parser, metav1Pkg *loader.Package) map[schema.GroupKind]struct{} {
	// TODO(directxman12): technically, we should be finding metav1 per-package
	kubeKinds := map[schema.GroupKind]struct{}{}
	for typeIdent, info := range parser.Types {
		hasObjectMeta := false
		hasTypeMeta := false

		pkg := typeIdent.Package
		pkg.NeedTypesInfo()
		typesInfo := pkg.TypesInfo

		for _, field := range info.Fields {
			if field.Name != "" {
				// type and object meta are embedded,
				// so they can't be this
				continue
			}

			fieldType := typesInfo.TypeOf(field.RawField.Type)
			namedField, isNamed := fieldType.(*types.Named)
			if !isNamed {
				// ObjectMeta and TypeMeta are named types
				continue
			}
			if namedField.Obj().Pkg() == nil {
				// Embedded non-builtin universe type (specifically, it's probably `error`),
				// so it can't be ObjectMeta or TypeMeta
				continue
			}
			fieldPkgPath := loader.NonVendorPath(namedField.Obj().Pkg().Path())
			fieldPkg := pkg.Imports()[fieldPkgPath]
			if fieldPkg != metav1Pkg {
				continue
			}

			switch namedField.Obj().Name() {
			case "ObjectMeta":
				hasObjectMeta = true
			case "TypeMeta":
				hasTypeMeta = true
			}
		}

		if !hasObjectMeta || !hasTypeMeta {
			continue
		}

		groupKind := schema.GroupKind{
			Group: parser.GroupVersions[pkg].Group,
			Kind:  typeIdent.Name,
		}
		kubeKinds[groupKind] = struct{}{}
	}

	return kubeKinds
}

// FindMetav1 locates the actual package representing metav1 amongst
// the imports of the roots.
func FindMetav1(roots []*loader.Package) *loader.Package {
	for _, root := range roots {
		pkg := root.Imports()["k8s.io/apimachinery/pkg/apis/meta/v1"]
		if pkg != nil {
			return pkg
		}
	}
	return nil
}

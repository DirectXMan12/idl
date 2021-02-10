/*
Copyright 2019 The Kubernetes Authors.

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
	"fmt"
	"go/ast"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
	irt "k8s.io/idl/ckdl-ir/goir/types"
)
// TypeIdent represents some type in a Package.
type TypeIdent struct {
	Package *loader.Package
	Name    string
}

func (t TypeIdent) String() string {
	return fmt.Sprintf("%q.%s", t.Package.ID, t.Name)
}
type Parser struct {
	Collector *markers.Collector
	Checker *loader.TypeChecker

	// packages marks packages as loaded, to avoid re-loading them.
	packages map[*loader.Package]struct{}
	// GroupVersions contains the known group-versions of each package in this parser.
	GroupVersions map[*loader.Package]schema.GroupVersion
	// Types contains the known TypeInfo for this parser.
	Types map[TypeIdent]*markers.TypeInfo
	// Kinds contains the known Kind descriptions
	Kinds map[TypeIdent]*irt.Kind
	// Subtypes contains the known Subtype descriptions
	Subtypes map[TypeIdent]*irt.Subtype
	// Deps tracks dependencies between group-versions
	Deps map[schema.GroupVersion]map[schema.GroupVersion]*loader.Package

	// PackageOverrides indicates that the loading of any package with
	// the given path should be handled by the given overrider.
	PackageOverrides map[string]PackageOverride

	AllowDangerousTypes bool
}

func (p *Parser) init() {
	if p.packages == nil {
		p.packages = make(map[*loader.Package]struct{})
	}
	if p.Types == nil {
		p.Types = make(map[TypeIdent]*markers.TypeInfo)
	}
	if p.GroupVersions == nil {
		p.GroupVersions = make(map[*loader.Package]schema.GroupVersion)
	}
	if p.Kinds == nil {
		p.Kinds = make(map[TypeIdent]*irt.Kind)
	}
	if p.Subtypes == nil {
		p.Subtypes = make(map[TypeIdent]*irt.Subtype)
	}
	if p.Deps == nil {
		p.Deps = make(map[schema.GroupVersion]map[schema.GroupVersion]*loader.Package)
	}
	if p.PackageOverrides == nil {
		p.PackageOverrides = make(map[string]PackageOverride)
	}
}

// AddPackage indicates that types and type-checking information is needed
// for the the given package, *ignoring* overrides.
// Generally, consumers should call NeedPackage, while PackageOverrides should
// call AddPackage to continue with the normal loading procedure.
func (p *Parser) AddPackage(pkg *loader.Package) {
	p.init()
	if _, checked := p.packages[pkg]; checked {
		return
	}
	p.indexTypes(pkg)
	p.Checker.Check(pkg)
	p.packages[pkg] = struct{}{}
}

// NeedPackage indicates that types and type-checking information
// is needed for the given package.
func (p *Parser) NeedPackage(pkg *loader.Package) {
	p.init()
	if _, checked := p.packages[pkg]; checked {
		return
	}
	// overrides are going to be written without vendor.  This is why we index by the actual
	// object when we can.
	if override, overridden := p.PackageOverrides[loader.NonVendorPath(pkg.PkgPath)]; overridden {
		override(p, pkg)
		p.packages[pkg] = struct{}{}
		return
	}
	p.AddPackage(pkg)
}

func (p *Parser) NeedDescFor(typ TypeIdent) {
	p.needDescFor(typ, false)
}
func (p *Parser) NeedKindDescFor(typ TypeIdent) {
	p.needDescFor(typ, true)
}

func (p *Parser) needDescFor(typ TypeIdent, isKind bool) {
	p.init()

	p.NeedPackage(typ.Package)
	if _, knownKind := p.Kinds[typ]; knownKind {
		return
	}
	if _, knownSubtype := p.Subtypes[typ]; knownSubtype {
		return
	}
	info, knownInfo := p.Types[typ]
	if !knownInfo {
		typ.Package.AddError(fmt.Errorf("unknown type %s", typ))
		return
	}

	if isKind {
		// avoid tripping up on recursive types
		p.Kinds[typ] = nil
	} else {
		p.Subtypes[typ] = nil
	}

	pkgMarkers, err := markers.PackageMarkers(p.Collector, typ.Package)
	if err != nil {
		typ.Package.AddError(err)
	}

	genCtx := NewGenContext(typ.Package, p, p.AllowDangerousTypes).
		ForInfo(info)
	genCtx.PackageMarkers = pkgMarkers

	if isKind {
		desc := InfoToKind(genCtx)
		p.Kinds[typ] = desc
	} else {
		desc := InfoToSubtype(genCtx)
		p.Subtypes[typ] = desc
	}
}

func (p *Parser) GroupVersionFor(src, pkg *loader.Package) *irt.GroupVersionRef {
	gv, known := p.GroupVersions[pkg]
	if !known {
		return nil
	}
	if src != pkg {
		// record group-version dependencies for use later when
		// assembling dependencies for partials
		srcGV, known := p.GroupVersions[src]
		if known {
			deps, exists := p.Deps[srcGV]
			if !exists {
				deps = make(map[schema.GroupVersion]*loader.Package)
				p.Deps[srcGV] = deps
			}
			deps[gv] = pkg
		}
	}
	return &irt.GroupVersionRef{Group: gv.Group, Version: gv.Version}
}

var versionRe = regexp.MustCompile(`^v[1-9][0-9]*((alpha|beta)[1-9][0-9]*)?$`)


// indexTypes loads all types in the package into Types.
func (p *Parser) indexTypes(pkg *loader.Package) {
	// autodetect
	pkgMarkers, err := markers.PackageMarkers(p.Collector, pkg)
	if err != nil {
		pkg.AddError(err)
	} else {
		if skipPkg := pkgMarkers.Get("kubebuilder:skip"); skipPkg != nil {
			return
		}
		if nameVal := pkgMarkers.Get("groupName"); nameVal != nil {
			versionVal := pkg.Name // a reasonable guess
			if versionMarker := pkgMarkers.Get("versionName"); versionMarker != nil {
				versionVal = versionMarker.(string)
			}

			p.GroupVersions[pkg] = schema.GroupVersion{
				Version: versionVal,
				Group:   nameVal.(string),
			}
		}
		// guess at k/k packages
		if copyMarker := pkgMarkers.Get("k8s:deepcopy-gen"); copyMarker != nil && copyMarker.(string) == "package" {
			versionName := pkg.Name
			if !versionRe.MatchString(versionName) {
				return
			}

			pkgParts := strings.Split(pkg.PkgPath, "/")
			groupName := pkgParts[len(pkgParts)-2]

			p.GroupVersions[pkg] = schema.GroupVersion{
				Version: versionName,
				Group:   groupName,
			}
		}
	}

	if err := markers.EachType(p.Collector, pkg, func(info *markers.TypeInfo) {
		ident := TypeIdent{
			Package: pkg,
			Name:    info.Name,
		}

		p.Types[ident] = info
	}); err != nil {
		pkg.AddError(err)
	}
}

// filterTypesForCRDs filters out all nodes that aren't used in CRD generation,
// like interfaces and struct fields without JSON tag.
func filterTypesForCRDs(node ast.Node) bool {
	switch node := node.(type) {
	case *ast.InterfaceType:
		// skip interfaces, we never care about references in them
		return false
	case *ast.StructType:
		return true
	case *ast.Field:
		_, hasTag := loader.ParseAstTag(node.Tag).Lookup("json")
		// fields without JSON tags mean we have custom serialization,
		// so only visit fields with tags.
		return hasTag
	default:
		return true
	}
}

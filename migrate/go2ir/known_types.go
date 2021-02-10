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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-tools/pkg/loader"
	//irt "sigs.k8s.io/controller-tools/pkg/interrep/types"
	//irc "sigs.k8s.io/controller-tools/pkg/interrep/constraints"
)

// PackageOverride overrides the loading of some package
// (potentially setting custom schemata, etc).  It must
// call AddPackage if it wants to continue with the default
// loading behavior.
type PackageOverride func(p *Parser, pkg *loader.Package)

// KnownPackages overrides types in some comment packages that have custom validation
// but don't have validation markers on them (since they're from core Kubernetes).
var KnownPackages = map[string]PackageOverride{
	// "k8s.io/api/core/v1": func(p *Parser, pkg *loader.Package) {
	// 	// Explicit defaulting for the corev1.Protocol type in lieu of https://github.com/kubernetes/enhancements/pull/1928
	// 	p.Subtypes[TypeIdent{Name: "Protocol", Package: pkg}] = apiext.JSONSchemaProps{
	// 		Type:    "string",
	// 		Default: &apiext.JSON{Raw: []byte(`"TCP"`)},
	// 	}
	// 	p.AddPackage(pkg)
	// },

	"k8s.io/apimachinery/pkg/apis/meta/v1": func(p *Parser, pkg *loader.Package) {
		// ObjectMeta is managed by the Kubernetes API server, so no need to
		// generate validation for it.
		// p.Subtypes[TypeIdent{Name: "ObjectMeta", Package: pkg}] = &irt.Subtype{
		// 	Name: "ObjectMeta",
		// }
		// these are primitives, just don't check them
		p.Subtypes[TypeIdent{Name: "Time", Package: pkg}] = nil
		p.Subtypes[TypeIdent{Name: "MicroTime", Package: pkg}] = nil
		p.Subtypes[TypeIdent{Name: "Duration", Package: pkg}] = nil
		p.AddPackage(pkg) // get the rest of the types
	},

	"k8s.io/apimachinery/pkg/runtime": func(p *Parser, pkg *loader.Package) {
		// TODO
		// p.Schemata[TypeIdent{Name: "RawExtension", Package: pkg}] = apiext.JSONSchemaProps{
		// 	// TODO(directxman12): regexp validation for this (or get kube to support it as a format value)
		// 	Type: "object",
		// }
		p.AddPackage(pkg) // get the rest of the types
	},

	"k8s.io/apimachinery/pkg/util/intstr": func(p *Parser, pkg *loader.Package) {
		// TODO
		// p.Schemata[TypeIdent{Name: "IntOrString", Package: pkg}] = apiext.JSONSchemaProps{
		// 	XIntOrString: true,
		// 	AnyOf: []apiext.JSONSchemaProps{
		// 		{Type: "integer"},
		// 		{Type: "string"},
		// 	},
		// }
		p.GroupVersions[pkg] = schema.GroupVersion{
			Group: "__intstr",
			Version: "__v1",
		}
		// No point in calling AddPackage, this is the sole inhabitant
	},
	"k8s.io/apimachinery/pkg/types": func(p *Parser, pkg *loader.Package) {
		p.GroupVersions[pkg] = schema.GroupVersion{
			Group: "__types",
			Version: "__v1",
		}
		p.AddPackage(pkg)
	},
}

func boolPtr(b bool) *bool {
	return &b
}

// AddKnownTypes registers the packages overrides in KnownPackages with the given parser.
func AddKnownTypes(parser *Parser) {
	// ensure everything is there before adding to PackageOverrides
	// TODO(directxman12): this is a bit of a hack, maybe just use constructors?
	parser.init()
	for pkgName, override := range KnownPackages {
		parser.PackageOverrides[pkgName] = override
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
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

package crd

import (
	"fmt"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"k8s.io/idl/tocrd/irloader"
	irt "k8s.io/idl/ckdl-ir/goir/types"
	irgv "k8s.io/idl/ckdl-ir/goir/groupver"
	ir "k8s.io/idl/ckdl-ir/goir"
)

type TypeIdent struct {
	GroupVersion
	Name string
}

type GroupVersion struct {
	Group string
	Version string
}

// Parser knows how to parse out CRD information and generate
// OpenAPI schemata from some collection of types and markers.
// Most methods on Parser cache their results automatically,
// and thus may be called any number of times.
type Parser struct {
	Loader irloader.Loader

	// Types contains the known non-Kind types for this parser.
	Types map[TypeIdent]*irt.Subtype
	// Kinds contains the known Kind types for this parser.
	Kinds map[TypeIdent]*irt.Kind
	// Schemata contains the known OpenAPI JSONSchemata for this parser.
	Schemata map[TypeIdent]apiext.JSONSchemaProps
	// CustomResourceDefinitions contains the known CustomResourceDefinitions for types in this parser.
	CustomResourceDefinitions map[GroupKind]apiext.CustomResourceDefinition
	// FlattenedSchemata contains fully flattened schemata for use in building
	// CustomResourceDefinition validation.  Each schema has been flattened by the flattener,
	// and then embedded fields have been flattened with FlattenEmbedded.
	FlattenedSchemata map[TypeIdent]apiext.JSONSchemaProps
	// GroupVersions keeps track of loaded group-versions
	GroupVersions map[GroupVersion]*irgv.GroupVersion

	flattener *Flattener
}

func (p *Parser) init() {
	if p.flattener == nil {
		p.flattener = &Flattener{
			Parser: p,
		}
	}
	if p.Schemata == nil {
		p.Schemata = make(map[TypeIdent]apiext.JSONSchemaProps)
	}
	if p.Types == nil {
		p.Types = make(map[TypeIdent]*irt.Subtype)
	}
	if p.Kinds == nil {
		p.Kinds = make(map[TypeIdent]*irt.Kind)
	}
	if p.GroupVersions == nil {
		p.GroupVersions = make(map[GroupVersion]*irgv.GroupVersion)
	}
	if p.CustomResourceDefinitions == nil {
		p.CustomResourceDefinitions = make(map[GroupKind]apiext.CustomResourceDefinition)
	}
	if p.FlattenedSchemata == nil {
		p.FlattenedSchemata = make(map[TypeIdent]apiext.JSONSchemaProps)
	}
}

// indexTypes loads all types in the package into Types.
func (p *Parser) indexTypes(set *ir.GroupVersionSet) {
	for _, gv := range set.GroupVersions {
		gvIdent := GroupVersion{Group: gv.Description.Group, Version: gv.Description.Version}
		for _, kind := range gv.Kinds {
			p.Kinds[TypeIdent{GroupVersion: gvIdent, Name: kind.Name}] = kind
		}
		for _, subtype := range gv.Types {
			p.Types[TypeIdent{GroupVersion: gvIdent, Name: subtype.Name}] = subtype
		}
		p.GroupVersions[gvIdent] = gv.Description
	}
}

// NeedSchemaFor indicates that a schema should be generated for the given type.
func (p *Parser) NeedSchemaFor(typ TypeIdent) {
	p.init()

	p.NeedGroupVersion(typ.GroupVersion)
	if _, knownSchema := p.Schemata[typ]; knownSchema {
		return
	}

	typeInfo, isType := p.Types[typ]
	kindInfo, isKind := p.Kinds[typ]
	if !isType && !isKind {
		panic(fmt.Errorf("unknown type %s", typ))
		return
	}
	if isType && isKind {
		panic("conflict")
	}

	// avoid tripping recursive schemata, like ManagedFields, by adding an empty WIP schema
	p.Schemata[typ] = apiext.JSONSchemaProps{}

	schemaCtx := NewRootContext()
	if isType {
		schema := SubtypeToSchema(schemaCtx, typeInfo)
		p.Schemata[typ] = *schema
	} else {
		schema := KindToSchema(schemaCtx, kindInfo)
		p.Schemata[typ] = *schema
	}

	if errs := schemaCtx.AllErrors(); len(errs) > 0 {
		panic(fmt.Sprintf("errors: %v", errs))
	}
}

func (p *Parser) NeedFlattenedSchemaFor(typ TypeIdent) {
	p.init()

	if _, knownSchema := p.FlattenedSchemata[typ]; knownSchema {
		return
	}

	p.NeedSchemaFor(typ)
	partialFlattened := p.flattener.FlattenType(typ)
	fullyFlattened := FlattenEmbedded(partialFlattened, nil /* todo: package as error reporter */)

	p.FlattenedSchemata[typ] = *fullyFlattened
}

// NeedCRDFor lives off in spec.go

func (p *Parser) NeedGroupVersion(gv GroupVersion) {
	p.init()

	if _, present := p.GroupVersions[gv]; present {
		return
	}
	set, err := p.Loader.Load(irloader.Hint{Group: gv.Group, Version: gv.Version})
	if err != nil {
		panic(err)
	}
	p.indexTypes(set)
}

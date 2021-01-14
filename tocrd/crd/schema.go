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
	"encoding/json"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	irt "k8s.io/idl/ckdl-ir/goir/types"
)

type SchemaError struct {
	provenance *schemaContext
	infoHint interface{}

	err error
}

func (e SchemaError) Provenance() (interface{}, []interface{}) {
	var parts []interface{}
	for ctx := e.provenance; ctx != nil; ctx = ctx.parent {
		parts = append(parts, ctx.item)
	}

	return e.infoHint, parts
}

func (e SchemaError) Error() string {
	return e.err.Error() 
}

// tracker globally records all irt<->schema mappings and errors
type tracker struct {
	// TODO(directxman12): switch to a lighter-weight method where we use proto
	// tag indicies, like the proto source maps?
	provenance map[interface{}]*schemaContext
	errors []SchemaError
}

func (t *tracker) AddError(ctx *schemaContext, infoHint interface{}, err error) {
	t.errors = append(t.errors, SchemaError{
		provenance: ctx,
		infoHint: infoHint,
		err: err,
	})
}

func (t *tracker) RecordProvenance(ctx *schemaContext, out interface{}) {
	if existing, ok := t.provenance[out]; ok {
		panic(fmt.Sprintf("duplicate provenance recorded for output %#v: (existing) %#v, (new) %#v", out, existing, ctx))
	}
	t.provenance[out] = ctx
}

// schemaContext handles recording provenance of a particular schema generation operation
// (think spans in a traditional compiler), plus recording errors during generation.
type schemaContext struct {
	parent *schemaContext
	item interface{}
	tracker *tracker
}

func NewRootContext() *schemaContext {
	tracker := &tracker{
		provenance: make(map[interface{}]*schemaContext),
	}
	return &schemaContext{
		tracker: tracker,
	}
}

// AllErrors returns all errors registered to the root context for this context.
func (c *schemaContext) AllErrors() []error {
	res := make([]error, len(c.tracker.errors))
	for i, err := range c.tracker.errors {
		res[i] = err
	}
	return res
}

// AddError marks that an error occurred when processing the HIR items that make this context.
func (c *schemaContext) AddError(target interface{}, err error) {
	c.tracker.AddError(c, target, err)
}

// From records that some operations (e.g. makes) come from a particular source HIR item.
func (c *schemaContext) From(src interface{}) *schemaContext {
	return &schemaContext{
		parent: c,
		item: src,
		tracker: c.tracker,
	}
}

// Makes marks that whatever HIR items made this context make this output schema.
func (c *schemaContext) Makes(src interface{}) {
	// TODO
}

func KindToSchema(ctx *schemaContext, kind *irt.Kind) *apiext.JSONSchemaProps {
	ctx = ctx.From(kind)

	// compute the basic schema
	schema := fieldsToSchema(ctx, kind.Fields)
	ctx.Makes(schema)

	// add in kind-related fields
	// TODO: non-persistent
	// TODO: docs and such for these
	schema.Properties["apiVersion"] = apiext.JSONSchemaProps{Type: "string"}
	schema.Properties["kind"] = apiext.JSONSchemaProps{Type: "string"}
	schema.Properties["metadata"] = apiext.JSONSchemaProps{Type: "object"}

	// add in docs
	docsToSchemaDocs(ctx, kind.Docs, schema)
	return schema
}

func SubtypeToSchema(ctx *schemaContext, subtype *irt.Subtype) *apiext.JSONSchemaProps {
	ctx = ctx.From(subtype)

	schema := bodyToSchema(ctx, subtype)
	ctx.Makes(schema)

	docsToSchemaDocs(ctx, subtype.Docs, schema)
	return schema
}

func bodyToSchema(ctx *schemaContext, subtype *irt.Subtype) *apiext.JSONSchemaProps {
	ctx = ctx.From(subtype.Type)

	var schema *apiext.JSONSchemaProps

	switch contents := subtype.Type.(type) {
	case *irt.Subtype_ReferenceAlias:
		schema = refToSchema(ctx, contents.ReferenceAlias)
	case *irt.Subtype_PrimitiveAlias:
		schema = primitiveToSchema(ctx, contents.PrimitiveAlias)
	case *irt.Subtype_Union:
		schema = unionToSchema(ctx, contents.Union)
	case *irt.Subtype_Struct:
		schema = structToSchema(ctx, contents.Struct)
	case *irt.Subtype_Set:
		schema = setToSchema(ctx, contents.Set)
	case *irt.Subtype_List:
		schema = listToSchema(ctx, contents.List)
	case *irt.Subtype_PrimitiveMap:
		schema = primitiveMapToSchema(ctx, contents.PrimitiveMap)
	case *irt.Subtype_ListMap:
		schema = listMapToSchema(ctx, contents.ListMap)
	case *irt.Subtype_Enum:
		schema = enumToSchema(ctx, contents.Enum)
	default:
		ctx.AddError(contents, fmt.Errorf("unknown body contents type"))
		return &apiext.JSONSchemaProps{}
	}

	ctx.Makes(schema)
	return schema
}

func fieldsToSchema(ctx *schemaContext, fields []*irt.Field) *apiext.JSONSchemaProps {
	props := &apiext.JSONSchemaProps{
		Type:       "object",
		Properties: make(map[string]apiext.JSONSchemaProps),
	}
	for _, field := range fields {
		fieldCtx := ctx.From(field)

		propName := field.Name

		propSchema := fieldTypeToSchema(fieldCtx, field.Type)
		fieldCtx.Makes(propSchema) // TODO: is this going to get lost b/c dereferencing?
		docsToSchemaDocs(fieldCtx, field.Docs, propSchema)
		applyGenericValidation(fieldCtx, field.Attributes, propSchema)
		if !field.Optional {
			props.Required = append(props.Required, propName)
		}

		/*
		// TODO: nullable
		if field.Nullable {
			propSchema.Nullable = true
		}
		*/

		if field.Embedded {
			props.AllOf = append(props.AllOf, *propSchema)
			continue
		}
		props.Properties[propName] = *propSchema
	}
	return props
}

func refToSchema(ctx *schemaContext, ref *irt.Reference) *apiext.JSONSchemaProps {
	ctx = ctx.From(ref)
	link := TypeRefLink(ref)
	schema := &apiext.JSONSchemaProps{
		Ref: &link,
	}
	return schema
}

func structToSchema(ctx *schemaContext, st *irt.Struct) *apiext.JSONSchemaProps {
	ctx = ctx.From(st)
	props := fieldsToSchema(ctx, st.Fields)
	// TODO: atomic
	/*
	if st.Atomic {
		defAtomic := "atomic"
		props.XMapType = &defAtomic
		// TODO(directxman12): ask about what's up with this
	}
	*/

	return props
}

func unionToSchema(ctx *schemaContext, st *irt.Union) *apiext.JSONSchemaProps {
	ctx = ctx.From(st)
	props := fieldsToSchema(ctx, st.Variants)
	props.Required = nil // specific requirements done in oneof

	if st.Untagged {
		defOne := int64(1)
		props.MaxProperties = &defOne // one variant
		props.MinProperties = &defOne
	} else {
		defTwo := int64(2)
		props.MaxProperties = &defTwo // tag and one variant
		props.MinProperties = &defTwo
		tagName := st.Tag
		props.Properties[tagName] = apiext.JSONSchemaProps{Type: "string"}

		for _, field := range st.Variants {
			fieldCtx := ctx.From(field)
			quotedFieldName, err := json.Marshal(field.Name)
			if err != nil {
				ctx.AddError(field, fmt.Errorf("unable to convert field name to string: %w", err))
				continue
			}
			oneOfSchema := apiext.JSONSchemaProps{
				Required: []string{tagName, field.Name},
				Properties: map[string]apiext.JSONSchemaProps{
					tagName: apiext.JSONSchemaProps{Enum: []apiext.JSON{{Raw: quotedFieldName}}},
				},
			}
			fieldCtx.Makes(oneOfSchema) // TODO: is this going to get lost b/c dereferencing?
			props.OneOf = append(props.OneOf, oneOfSchema)
		}
	}

	return props
}


func fieldTypeToSchema(ctx *schemaContext, fieldType interface{}) *apiext.JSONSchemaProps {
	ctx = ctx.From(fieldType)

	switch typ := fieldType.(type) {
	case *irt.Field_Primitive:
		schema := primitiveToSchema(ctx, typ.Primitive)
		ctx.Makes(schema)
		return schema
	case *irt.Field_NamedType:
		schema := refToSchema(ctx, typ.NamedType)
		ctx.Makes(schema)
		return schema
	case *irt.Field_List:
		schema := listToSchema(ctx, typ.List)
		ctx.Makes(schema)
		return schema
	case *irt.Field_Set:
		schema := setToSchema(ctx, typ.Set)
		ctx.Makes(schema)
		return schema
	case *irt.Field_PrimitiveMap:
		schema := primitiveMapToSchema(ctx, typ.PrimitiveMap)
		ctx.Makes(schema)
		return schema
	case *irt.Field_ListMap:
		schema := listMapToSchema(ctx, typ.ListMap)
		ctx.Makes(schema)
		return schema
	default:
		ctx.AddError(fieldType, fmt.Errorf("unknown field type"))
		return &apiext.JSONSchemaProps{}
	}
}

func listToSchema(ctx *schemaContext, list *irt.List) *apiext.JSONSchemaProps {
	ctx = ctx.From(list)

	valueCtx := ctx.From(list.Items)
	var valSchema *apiext.JSONSchemaProps
	switch valType := list.Items.(type) {
	case *irt.List_Primitive:
		valSchema = primitiveToSchema(valueCtx, valType.Primitive)
	case *irt.List_Reference:
		valSchema = refToSchema(valueCtx, valType.Reference)
	default:
		valueCtx.AddError(valType, fmt.Errorf("invalid set value type"))
	}
	valueCtx.Makes(valSchema)
	schema := &apiext.JSONSchemaProps{
		Type: "array",
		Items: &apiext.JSONSchemaPropsOrArray{Schema: valSchema},
	}
	return schema
}

func primitiveMapToSchema(ctx *schemaContext, primMap *irt.PrimitiveMap) *apiext.JSONSchemaProps {
	ctx = ctx.From(primMap)

	mapInfo := primMap
	valueCtx := ctx.From(mapInfo.Value)

	var valSchema *apiext.JSONSchemaProps
	switch valType := mapInfo.Value.(type) {
	case *irt.PrimitiveMap_PrimitiveValue:
		valSchema = primitiveToSchema(valueCtx, valType.PrimitiveValue)
	case *irt.PrimitiveMap_ReferenceValue:
		valSchema = refToSchema(valueCtx, valType.ReferenceValue)
	case *irt.PrimitiveMap_SimpleListValue:
		valSchema = listToSchema(valueCtx, valType.SimpleListValue)
	default:
		valueCtx.AddError(valType, fmt.Errorf("invalid simple map value type"))
	}
	valueCtx.Makes(valSchema)

	schema := &apiext.JSONSchemaProps{
		Type: "object",
		AdditionalProperties: &apiext.JSONSchemaPropsOrBool{
			Schema: valSchema,
			Allows: true, /* set automatically by serialization, but useful for testing */
		},
	}
	// TODO: atomic
	/*
	if primMap.Atomic {
		defAtomic := "atomic"
		schema.XMapType = &defAtomic
	} // default is granular, no need to set explicitly
	*/

	return schema
}

func listMapToSchema(ctx *schemaContext, listMap *irt.ListMap) *apiext.JSONSchemaProps {
	ctx = ctx.From(listMap)
	valueSchema := refToSchema(ctx, listMap.Items)
	ctx.From(listMap.Items).Makes(valueSchema)
	defMap := "map"
	return &apiext.JSONSchemaProps{
		Type: "array",
		Items: &apiext.JSONSchemaPropsOrArray{Schema: valueSchema},
		XListType: &defMap,
		XListMapKeys: listMap.KeyField,
	}
}

func setToSchema(ctx *schemaContext, set *irt.Set) *apiext.JSONSchemaProps {
	ctx = ctx.From(set)

	valueCtx := ctx.From(set.Items)

	var valSchema *apiext.JSONSchemaProps
	switch valType := set.Items.(type) {
	case *irt.Set_Primitive:
		valSchema = primitiveToSchema(valueCtx, valType.Primitive)
	case *irt.Set_Reference:
		valSchema = refToSchema(valueCtx, valType.Reference)
	default:
		valueCtx.AddError(valType, fmt.Errorf("invalid set value type"))
	}
	valueCtx.Makes(valSchema)
	defSet := "set"
	schema := &apiext.JSONSchemaProps{
		Type: "array",
		Items: &apiext.JSONSchemaPropsOrArray{Schema: valSchema},
		XListType: &defSet,
	}
	return schema
}

func primitiveToSchema(ctx *schemaContext, primitive *irt.Primitive) *apiext.JSONSchemaProps {
	ctx = ctx.From(primitive)

	var typ, format string
	var schema *apiext.JSONSchemaProps

	switch primitive.Type {
	case irt.Primitive_STRING:
		typ = "string"
	case irt.Primitive_LEGACYINT32:
		typ, format = "integer", "int32"
	case irt.Primitive_INT64:
		typ, format =  "integer", "int64"
	case irt.Primitive_BOOL:
		typ = "boolean"
	case irt.Primitive_TIME:
		typ, format = "string", "date-time"
	case irt.Primitive_DURATION:
		typ = "string" // TODO: regex or format for this
	case irt.Primitive_QUANTITY:
		// TODO: format for this -- int-or-string is incomplete, needs float
		schema = &apiext.JSONSchemaProps {
			XIntOrString: true,
			AnyOf: []apiext.JSONSchemaProps{
				{Type: "integer"},
				{Type: "string"},
			},
			Pattern: "^(\\+|-)?(([0-9]+(\\.[0-9]*)?)|(\\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\\+|-)?(([0-9]+(\\.[0-9]*)?)|(\\.[0-9]+))))?$",
		}
	case irt.Primitive_BYTES:
		typ, format = "string", "byte"
	case irt.Primitive_LEGACYFLOAT64:
		typ = "number"
	case 100: // TODO: intorstring
		schema = &apiext.JSONSchemaProps{
			XIntOrString: true,
			AnyOf: []apiext.JSONSchemaProps{
				{Type: "integer"},
				{Type: "string"},
			},
		}
	default:
		ctx.AddError(primitive, fmt.Errorf("unknown primitive type"))
		return &apiext.JSONSchemaProps{}
	}

	if schema == nil {
		schema = &apiext.JSONSchemaProps{
			Type: typ,
			Format: format,
		}
	}
	ctx.Makes(schema)
	return schema
}

func enumToSchema(ctx *schemaContext, enum *irt.Enum) *apiext.JSONSchemaProps {
	ctx = ctx.From(enum)

	vals := make([]apiext.JSON, len(enum.Variants))
	for i, val := range enum.Variants {
		// if we're expecting a string, marshal the string properly...
		// NB(directxman12): we use json.Marshal to ensure we handle JSON escaping properly
		valMarshalled, err := json.Marshal(val.Name)
		if err != nil {
			ctx.AddError(enum, fmt.Errorf("unable to serialize enum value %v: %w", val, err))
			continue
		}
		vals[i] = apiext.JSON{Raw: valMarshalled}
	}
	schema := &apiext.JSONSchemaProps{
		Type: "string",
		Enum: vals,
	}
	ctx.Makes(schema)
	return schema
}

func docsToSchemaDocs(ctx *schemaContext, docs *irt.Documentation, schema *apiext.JSONSchemaProps) {
	if docs == nil {
		return
	}
	ctx = ctx.From(docs)

	schema.Description = docs.Description
}

const (
	// defPrefix is the prefix used to link to definitions in the OpenAPI schema.
	defPrefix = "#/definitions/"
)

func TypeRefLink(name *irt.Reference) string {
	if name.GroupVersion == nil {
		return defPrefix + name.Name
	} 
	//return fmt.Sprintf("%s%s~1%s~0%s", defPrefix, name.GroupVersion.Group, name.GroupVersion.Version, name.Name)
	return fmt.Sprintf("%s%s/%s/%s", defPrefix, name.GroupVersion.Group, name.GroupVersion.Version, name.Name)
}


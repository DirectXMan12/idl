// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package fromast

import (
	"context"
	"strings"
	"fmt"

	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/proto"
	pr "google.golang.org/protobuf/reflect/protoreflect"
	pd "google.golang.org/protobuf/reflect/protodesc"

	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser/ast"
	"k8s.io/idl/kdlc/mdesc"
	irt "k8s.io/idl/ckdl-ir/goir/types"
	irm "k8s.io/idl/ckdl-ir/goir/markers"
	ire "k8s.io/idl/ckdl-ir/goir"
)

func resolveMarkerModifiers(ctx context.Context, modifiers ast.ModifierList) *ast.ResolvedTypeInfo {
	// convert to concrete typedata first so that we can know what's a type vs
	// a value that happens to be a enum variant name or whatever (this is an
	// ambiguity that we could maybe resolve, but it's easy enough to do this instead)
	typeData := modifiersToKnown(ctx, modifiers)
	ctx = trace.InSpan(trace.Describe(ctx, "type modifier"), typeData.TypeSrc)
	// TODO: maybe more detailed span info for nested stuff?
	switch typ := typeData.Type.(type) {
	case ast.RefType:
		trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
	case ast.ListType:
		_, isRef := typ.Items.(*irt.List_Reference)
		if !isRef {
			break
		}
		trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
	case ast.SetType:
		_, isRef := typ.Items.(*irt.Set_Reference)
		if !isRef {
			break
		}
		trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
	case ast.ListMapType:
		// TODO: we should figure out how to support these
		trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
	case ast.PrimitiveMapType:
		_, isRef := typ.Key.(*irt.PrimitiveMap_ReferenceKey)
		if isRef {
			trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
		}

		switch val := typ.Value.(type) {
		case *irt.PrimitiveMap_ReferenceValue:
			trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
		case *irt.PrimitiveMap_SimpleListValue:
			_, isRef := val.SimpleListValue.Items.(*irt.List_Reference)
			if !isRef {
				break
			}
			trace.ErrorAt(ctx, "only primitives and containers thereof are supported in marker definitions")
		}
	// don't care about primitives
	}
	return &typeData
}

func isPrim(typ *irm.Type, primType irt.Primitive_Type) bool {
	asPrim, is := typ.Type.(*irm.Type_Primitive)
	if !is {
		return false
	}
	return asPrim.Primitive.Type == primType
}

func valToProto(ctx context.Context, val ast.Value, typ *irm.Type, emptyVal pr.Value) pr.Value {
	switch val := val.(type) {
	case ast.StringVal:
		if !isPrim(typ, irt.Primitive_STRING) {
			// TODO: better error
			trace.ErrorAt(ctx, "mismatched marker parameter value, got a string")
			return pr.Value{}
		}
		return pr.ValueOfString(val.Value)
	case ast.NumVal:
		switch {
		case isPrim(typ, irt.Primitive_LEGACYINT32):
			return pr.ValueOfInt32(int32(val.Value))
		case isPrim(typ, irt.Primitive_INT64):
			return pr.ValueOfInt64(int64(val.Value))
		default:
			trace.ErrorAt(ctx, "mismatched marker parameter value, got a number")
			return pr.Value{}
		}
	case ast.BoolVal:
		if !isPrim(typ, irt.Primitive_BOOL) {
			// TODO: better error
			trace.ErrorAt(ctx, "mismatched marker parameter value, got a bool")
			return pr.Value{}
		}
		return pr.ValueOfBool(val.Value)
	case ast.ListVal:
		fieldList, isList := typ.Type.(*irm.Type_List)
		if !isList {
			trace.ErrorAt(ctx, "mismtched marker parameter value, got a list")
			return pr.Value{}
		}
		out := emptyVal.List()
		// TODO(directxman12): support h-lists?
		for _, elem := range val.Values {
			elemVal := valToProto(ctx, elem, fieldList.List.Items, out.NewElement())
			if !elemVal.IsValid() {
				continue
			}
			out.Append(elemVal)
		}
		return pr.ValueOfList(out)
	case ast.StructVal:
		switch typ := typ.Type.(type) {
		case *irm.Type_Map:
			out := emptyVal.Map()
			for _, kv := range val.KeyValues {
				valVal := valToProto(ctx, kv.Value, typ.Map.Values, out.NewValue())
				if !valVal.IsValid() {
					continue
				}
				out.Set(pr.ValueOfString(kv.Key.Name).MapKey(), valVal)
			}
			return pr.ValueOfMap(out)
		case *irm.Type_NamedType:
			panic("TODO: needs type graph")
		default:
			trace.ErrorAt(ctx, "mismtched marker parameter value, got a struct")
			return pr.Value{}
		}

		// TODO(directxman12): support references/sub-values
	case ast.FieldPathVal:
		panic("TODO: need a marker field primitive for field paths")

	// TODO(directxman12): support type values
	case ast.RefTypeVal:
		trace.ErrorAt(ctx, "type values are not supported in marker parameters (just yet)")
		return pr.Value{}
	case ast.PrimitiveTypeVal:
		trace.ErrorAt(ctx, "type values are not supported in marker parameters (just yet)")
		return pr.Value{}
	case ast.CompoundTypeVal:
		trace.ErrorAt(ctx, "type values are not supported in marker parameters (just yet)")
		return pr.Value{}
	default:
		panic(fmt.Sprintf("unreachable: unknown value type %T", val))
	}
}

type markerConverter struct {
	req Requester
	prefixes map[string]string

	// TODO: known types
	loadedFiles map[string]pr.MessageDescriptors
	defns map[mdesc.MarkerIdent]*irm.MarkerDef
	descNames map[mdesc.MarkerIdent]mdesc.MarkerIdent
	fieldsByName map[mdesc.MarkerIdent]map[string]int
}

func newMarkerConverter(req Requester, prefixes map[string]string) *markerConverter {
	res := &markerConverter{
		req: req,
		prefixes: prefixes,

		loadedFiles: make(map[string]pr.MessageDescriptors),
		defns: make(map[mdesc.MarkerIdent]*irm.MarkerDef),
		descNames: make(map[mdesc.MarkerIdent]mdesc.MarkerIdent),
		fieldsByName: make(map[mdesc.MarkerIdent]map[string]int),
	}
	return res
}


func (c *markerConverter) loadFile(ctx context.Context, prefix string) {
	src, known := c.prefixes[prefix]
	if !known {
		trace.ErrorAt(ctx, "unknown marker prefix (you might not've imported it)")
		return
	}
	part := c.req.Load(ctx, src)
	c.loadedFiles[prefix] = nil // mark as loaded either way to avoid spamming errors
	for _, set := range part.MarkerSets {
		// TODO: error if we have multiple marker sets per file
		c.makeDescs(ctx, prefix, set)
	}
}
// TODO: figure out how to cache the compile descriptors between files?
func (c *markerConverter) makeDescs(ctx context.Context, prefix string, set *ire.MarkerSet) {
	res := mdesc.MakeDescriptor(ctx, prefix, set)
	for k, v := range res.DescriptorNames {
		c.descNames[k] = v
	}
	for k, v := range res.Definitions {
		c.defns[k] = v
	}
	desc, err := pd.NewFile(res.File, nil)
	if err != nil {
		// TODO: context, span
		trace.ErrorAt(trace.Note(ctx, "error", err), "unable to compile markers to proto")
		return
	}
	c.loadedFiles[prefix] = desc.Messages()
}

func (c *markerConverter) getDescriptor(ctx context.Context, rawName mdesc.MarkerIdent) pr.MessageDescriptor {
	file, loaded := c.loadedFiles[rawName.Prefix]
	if !loaded {
		c.loadFile(ctx, rawName.Prefix)
		file, loaded = c.loadedFiles[rawName.Prefix]
	}
	loaded = loaded && file != nil

	name, present := c.descNames[rawName]
	if !loaded || !present {
		trace.ErrorAt(ctx, "unknown marker")
		return nil
	}
	desc := file.ByName(pr.Name(name.Name))
	if desc == nil {
		trace.ErrorAt(ctx, "unknown marker")
		return nil
	}
	return desc
}

func (c *markerConverter) markerToMsg(ctx context.Context, raw *ast.AbstractMarker) proto.Message {
	nameParts := strings.SplitN(raw.Name.Name, "::", 2)
	name := mdesc.MarkerIdent{}
	if len(nameParts) == 1 {
		name.Name = nameParts[0]
	} else {
		name.Prefix = nameParts[0]
		name.Name = nameParts[1]
	}

	desc := c.getDescriptor(ctx, name)
	if desc == nil {
		return nil
	}
	def := c.defns[name]
	fieldInds := c.fieldsByName[name]

	msg := dynamicpb.NewMessage(desc)
	defFields := desc.Fields()

	for _, param := range raw.Parameters.Params {
		paramName := strings.Replace(param.Key.Name, "-", "_", -1)
		fieldDef := defFields.ByName(pr.Name(paramName))
		if fieldDef == nil {
			// TODO: span
			trace.ErrorAt(ctx, "unknown parameter in marker")
			continue
		}
		field := def.Fields[fieldInds[param.Key.Name]]
		val := valToProto(ctx, param.Value, field.Type, msg.NewField(fieldDef))
		if !val.IsValid() {
			continue
		}
		msg.Set(fieldDef, val)
	}

	return msg
}

func (c *markerConverter) VisitMarker(ctx context.Context, marker *ast.AbstractMarker) {
	msg := c.markerToMsg(ctx, marker)
	marker.Resolved = &ast.ResolvedMarker{Message: msg}
}

type Requester interface {
	Load(ctx context.Context, path string) *ire.Partial
}

func ResolveMarkers(ctx context.Context, file *ast.File, req Requester) {
	prefixes := make(map[string]string)
	// TODO: inject the built-in markers
	if file.Imports != nil && file.Imports.Markers != nil {
		for _, src := range file.Imports.Markers.Imports {
			prefixes[src.Alias] = src.Src
		}
	}

	conv := newMarkerConverter(req, prefixes)
	for i := range file.GroupVersions {
		gv := &file.GroupVersions[i]
		VisitMarkers(ctx, conv, gv)
	}
}

func PrepMarkerDecls(ctx context.Context, file *ast.File) {
	for i := range file.MarkerDecls {
		set := &file.MarkerDecls[i]
		for i := range set.MarkerDecls {
			decl := &set.MarkerDecls[i]
			for i := range decl.Fields {
				field := &decl.Fields[i]

				ctx := trace.Describe(ctx, "field")
				ctx = trace.Note(ctx, "name", field.Name.Name)
				ctx = trace.InSpan(ctx, field)

				field.ResolvedType = resolveMarkerModifiers(ctx, field.Modifiers)
			}
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package mdesc

import (
	"context"
	"strings"
	"fmt"

	pdesc "google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/proto"

	"k8s.io/idl/kdlc/parser/trace"
	irt "k8s.io/idl/ckdl-ir/goir/types"
	irm "k8s.io/idl/ckdl-ir/goir/markers"
	ire "k8s.io/idl/ckdl-ir/goir"
)

type MarkerIdent struct {
	Prefix, Name string
}

type MarkerProto struct {
	File *pdesc.FileDescriptorProto
	DescriptorNames map[MarkerIdent]MarkerIdent
	Definitions map[MarkerIdent]*irm.MarkerDef
}

func MakeDescriptor(ctx context.Context, prefix string, set *ire.MarkerSet) MarkerProto {
	res := MarkerProto{
		File: &pdesc.FileDescriptorProto{
			Name: proto.String(prefix),
			Package: proto.String(set.Package),
			Syntax: proto.String("proto3"),
		},
		DescriptorNames: make(map[MarkerIdent]MarkerIdent),
		Definitions: make(map[MarkerIdent]*irm.MarkerDef),
	}
	for _, defn := range set.Markers {
		desc := compileDesc(ctx, defn)
		if desc == nil {
			continue
		}
		ident := MarkerIdent{Name: defn.Name, Prefix: prefix}
		res.DescriptorNames[ident] = MarkerIdent{Prefix: set.Package, Name: *desc.Name}
		res.Definitions[ident] = defn
		res.File.MessageType = append(res.File.MessageType, desc)
	}
	return res
}

func typeToProto(ctx context.Context, raw *irm.Type, outType **pdesc.FieldDescriptorProto_Type, outTypeName **string, outLabel **pdesc.FieldDescriptorProto_Label) {
	switch typ := raw.Type.(type) {
	case *irm.Type_Primitive:
		switch typ.Primitive.Type {
		case irt.Primitive_STRING:
			primType := pdesc.FieldDescriptorProto_TYPE_STRING
			*outType = &primType
		case irt.Primitive_LEGACYINT32:
			primType := pdesc.FieldDescriptorProto_TYPE_INT32
			*outType = &primType
		case irt.Primitive_INT64:
			primType := pdesc.FieldDescriptorProto_TYPE_INT64
			*outType = &primType
		case irt.Primitive_BYTES:
			primType := pdesc.FieldDescriptorProto_TYPE_BYTES
			*outType = &primType
		case irt.Primitive_BOOL:
			primType := pdesc.FieldDescriptorProto_TYPE_BOOL
			*outType = &primType
		case irt.Primitive_LEGACYFLOAT64:
			primType := pdesc.FieldDescriptorProto_TYPE_FLOAT
			*outType = &primType
		// TODO: figure out a good serialization for these
		case irt.Primitive_QUANTITY:
			trace.ErrorAt(ctx, "quantity is not supported in markers (just yet)")
		case irt.Primitive_TIME:
			trace.ErrorAt(ctx, "duration is not supported in markers (just yet)")
		case irt.Primitive_DURATION:
			trace.ErrorAt(ctx, "time is not supported in markers (just yet)")
		case irt.Primitive_INTORSTRING:
			trace.ErrorAt(ctx, "int-or-string is not supported in markers")
		default:
			trace.ErrorAt(ctx, "unknown primitive type")
		}
	case *irm.Type_List:
		fakeLbl := new(*pdesc.FieldDescriptorProto_Label)
		typeToProto(ctx, typ.List.Items, outType, outTypeName, fakeLbl)
		lbl := pdesc.FieldDescriptorProto_LABEL_REPEATED
		*outLabel = &lbl
	case *irm.Type_Map:
		panic("TODO: generate map entry")
	case *irm.Type_NamedType:
		trace.ErrorAt(ctx, "references are not supported in marker parameters (just yet)")
	case *irm.Type_TypePrimitive:
		// TODO: figure out how to encode these
		trace.ErrorAt(ctx, "type values are not supported in marker parameters (just yet)")
	default:
		panic(fmt.Sprintf("unreachable: unknown marker field type %T", typ))
	}
}

func compileDesc(ctx context.Context, defn *irm.MarkerDef) *pdesc.DescriptorProto {
	name := strings.Replace(strings.Title(strings.Replace(defn.Name, "-", " ", -1)), " ", "", -1)
	res := &pdesc.DescriptorProto{
		Name: &name,
	}

	for _, rawField := range defn.Fields {
		field := &pdesc.FieldDescriptorProto{
			Name: proto.String(strings.Replace(rawField.Name, "-", "_", -1)),
			Number: proto.Int32(int32(rawField.ProtoTag)),
		}

		typeToProto(ctx, rawField.Type, &field.Type, &field.TypeName, &field.Label)

		res.Field = append(res.Field, field)
	}

	return res
}

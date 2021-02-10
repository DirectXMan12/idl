// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package toir

import (
	"context"
	"strings"
	"fmt"

	irt "k8s.io/idl/ckdl-ir/goir/types"
	irc "k8s.io/idl/ckdl-ir/goir/constraints"
	irgv "k8s.io/idl/ckdl-ir/goir/groupver"
	irm "k8s.io/idl/ckdl-ir/goir/markers"
	ire "k8s.io/idl/ckdl-ir/goir"
	any "google.golang.org/protobuf/types/known/anypb"
	pstruct "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser/ast"
)

// TODO: check sourcemap coverage

type srcMap struct {
	locs []*ire.Location
}

type Mapper struct {
	srcMap *srcMap
	loc *ire.Location
	current protoreflect.MessageDescriptor
	rep bool
}
func startMapping(msg proto.Message) *Mapper {
	return &Mapper{
		srcMap: &srcMap{},
		current: msg.ProtoReflect().Descriptor(),
		loc: &ire.Location{},
	}
}

func (m *Mapper) partialLoc(pathItem int32) *ire.Location {
	loc := &ire.Location{
		Path: make([]int32, len(m.loc.Path), len(m.loc.Path)+1),
	}
	copy(loc.Path, m.loc.Path)
	loc.Path = append(loc.Path, pathItem)
	return loc
}

func (m *Mapper) Field(field protoreflect.Name) *Mapper {
	if m.rep || m.current == nil {
		panic("tried to specify field path element on repeated or non-message field")
	}
	fieldDesc := m.current.Fields().ByName(field)
	if fieldDesc == nil {
		panic(fmt.Sprintf("unknown cKDL message field %q", field))
	}
	return &Mapper{
		srcMap: m.srcMap,
		loc: m.partialLoc(int32(fieldDesc.Number())),
		rep: fieldDesc.Cardinality() == protoreflect.Repeated,
		current: fieldDesc.Message(),
	}
}

func (m *Mapper) Item(ind int) *Mapper {
	if !m.rep {
		panic("tried to specify item path element on non-repeated field")
	}
	return &Mapper{
		srcMap: m.srcMap,
		loc: m.partialLoc(int32(ind)),
		current: m.current,
	}
}
func (m *Mapper) From(span trace.Spannable) *Mapper {
	start, end := span.SpanStart(), span.SpanEnd()
	m.loc.Span = []int32{int32(start.Start.Offset), int32(end.End.Offset)}
	m.srcMap.locs = append(m.srcMap.locs, m.loc)
	return m
}

func File(ctx context.Context, file *ast.File) ire.Partial {
	m := startMapping(&ire.Partial{})
	return ire.Partial{
		Dependencies: Imports(ctx, m.Field("dependencies"), file.Imports),
		GroupVersions: GroupVersions(ctx, m.Field("group_versions"), file.GroupVersions),
		MarkerSets: MarkerSets(ctx, m.Field("marker_sets"), file.MarkerDecls),
		SourceMap: m.srcMap.locs,
	}
}

func Imports(ctx context.Context, m *Mapper, imports *ast.Imports) []*ire.Partial_Dependency {
	if imports == nil || imports.Types == nil {
		return nil
	}
	m.From(imports)

	var res []*ire.Partial_Dependency
	for gv, imp := range imports.Types.Imports {
		m.Item(len(res)).From(imp)
		res = append(res, &ire.Partial_Dependency{
			GroupVersion: &irt.GroupVersionRef{
				Group: gv.Group,
				Version: gv.Version,
			},
			From: imp.Src,
		})
	}
	return res
}

func GroupVersions(ctx context.Context, m *Mapper, groupVers []ast.GroupVersion) []*ire.GroupVersion {
	set := []*ire.GroupVersion{}
	for i, gv := range groupVers {
		set = append(set, GroupVersion(ctx, m.Item(i), gv))
	}
	return set
}

func Docs(ctx context.Context, m *Mapper, docs ast.Docs) *irt.Documentation {
	m.From(docs)

	ctx = ast.In(ctx, docs)
	res := irt.Documentation{}
	for _, section := range docs.Sections {
		ctx := ast.In(ctx, section)
		switch strings.ToLower(section.Title) {
		case "", "description":
			m.Field("description").From(section)
			res.Description = strings.Join(section.Lines, "\n")
		case "example":
			m.Field("example").From(section)
			res.Example = strings.Join(section.Lines, "\n")
		case "external ref":
			m.Field("external_ref").From(section)
			res.ExternalRef = strings.Join(section.Lines, "\n")
		default:
			trace.ErrorAt(ctx, "unknown documentation section, expected `example` or `external ref`")
		}
	}
	return &res
}
func Markers(ctx context.Context, m *Mapper, markers []ast.AbstractMarker) []*any.Any {
	res := make([]*any.Any, len(markers))
	for i, raw := range markers {
		m.Item(i).From(raw)
		enc, err := any.New(raw.Resolved.Message)
		if err != nil {
			trace.ErrorAt(trace.Note(ast.In(ctx, raw), "error", err), "unable to store encoded marker")
			continue
		}
		res[i] = enc
	}
	return res
}

func GroupVersion(ctx context.Context, m *Mapper, gv ast.GroupVersion) *ire.GroupVersion {
	m.From(gv)

	ctx = ast.In(ctx, gv)
	descM := m.Field("description")

	res := ire.GroupVersion{
		Description: &irgv.GroupVersion{
			Group: gv.Group,
			Version: gv.Version,
			Docs: Docs(ctx, descM.Field("docs"), gv.Docs),
			Attributes: Markers(ctx, descM.Field("attributes"), gv.Markers),
		},
	}

	kindsM := m.Field("kinds")
	typesM := m.Field("types")
	for _, decl := range gv.Decls {
		switch decl := decl.(type) {
		case *ast.KindDecl:
			res.Kinds = append(res.Kinds, Kind(ctx, kindsM.Item(len(res.Kinds)), typesM, &res, *decl))
		case *ast.SubtypeDecl:
			// Subtype adds itself for reasons of path recording
			Subtype(ctx, typesM, &res, *decl)
		default:
			panic("unreachable: unknown declaration type")
		}
	}

	return &res
}

func Kind(ctx context.Context, m *Mapper, typesM *Mapper, gv *ire.GroupVersion, kind ast.KindDecl) *irt.Kind {
	ctx = ast.In(ctx, kind)
	m.From(kind)

	res := irt.Kind{
		Name: kind.Name.Name,
		Object: true,  // TODO
		Docs: Docs(ctx, m.Field("docs"), kind.Docs),
		Attributes: Markers(ctx, m.Field("attributes"), kind.Markers),
	}
	fieldM := m.Field("fields")
	for i, field := range kind.Fields {
		res.Fields = append(res.Fields, Field(ctx, fieldM.Item(i), field))
	}
	for _, subtype := range kind.Subtypes {
		Subtype(ctx, typesM, gv, subtype)
	}
	return &res
}

func onlyConstrain(ctx context.Context, info *ast.ValidatesInfo, allowed ast.ValidationType) {
	if info == nil {
		return
	}

	// TODO: proper traces here
	if (allowed != ast.NumberValidation) && info.Number != nil {
		trace.ErrorAt(ctx, "string validation is only supported for int32, int64, and dangerous-float64")
	}
	if (allowed != ast.StringValidation) && info.String != nil {
		trace.ErrorAt(ctx, "string validation is only supported for string and bytes")
	}
	if (allowed != ast.ListValidation) && info.List != nil {
		trace.ErrorAt(ctx, "list validation is only supported for lists, sets, and list-maps")
	}
	if (allowed != ast.ObjectishValidation) && info.Objectish != nil {
		trace.ErrorAt(ctx, "object-ish validation is only supported for simple-maps and structs")
	}
}

func primConstraints(ctx context.Context, prim *irt.Primitive, info *ast.ValidatesInfo) {
	if info == nil {
		return
	}

	switch prim.Type {
	case irt.Primitive_LEGACYINT32, irt.Primitive_INT64, irt.Primitive_LEGACYFLOAT64:
		onlyConstrain(ctx, info, ast.NumberValidation)
		prim.SpecificConstraints = &irt.Primitive_NumericConstraints{
			NumericConstraints: info.Number,
		}
	case irt.Primitive_STRING, irt.Primitive_BYTES:
		onlyConstrain(ctx, info, ast.StringValidation)
		prim.SpecificConstraints = &irt.Primitive_StringConstraints{
			StringConstraints: info.String,
		}
	case irt.Primitive_TIME, irt.Primitive_DURATION, irt.Primitive_QUANTITY:
		// TODO(directxman12): bug upstream about validation for these --
		// technically they're string in openapi, but the numeric validations
		// make more sense
		onlyConstrain(ctx, info, ast.StringValidation)
		prim.SpecificConstraints = &irt.Primitive_StringConstraints{
			StringConstraints: info.String,
		}
	case irt.Primitive_BOOL:
		onlyConstrain(ctx, info, ast.NoValidation)
		// nothing to validate for bool
	case irt.Primitive_INTORSTRING:
		onlyConstrain(ctx, info, ast.NoValidation)
		// TODO(directxman12): maybe support validation for int-or-string?
	default:
		panic("unreachable: unknown primitive type")
	}
}
func refConstraints(ctx context.Context, ref *irt.Reference, info *ast.ValidatesInfo) {
	if info == nil {
		return
	}
	// TODO: proper traces on these errors
	if info.Number != nil {
		if info.String != nil || info.List != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "only one \"type\" of validation may be specified at once.  For instance, if you use numeric validation, you may not also use string validation.")
		}

		ref.Constraints = &irc.Any{Type: &irc.Any_Num{
			Num: info.Number,
		}}
	}
	if info.String != nil {
		if info.Number != nil || info.List != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "only one \"type\" of validation may be specified at once.  For instance, if you use string validation, you may not also use numeric validation.")
		}

		ref.Constraints = &irc.Any{Type: &irc.Any_Str{
			Str: info.String,
		}}
	}
	if info.List != nil {
		if info.String != nil || info.Number != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "only one \"type\" of validation may be specified at once.  For instance, if you use list validation, you may not also use string validation.")
		}

		ref.Constraints = &irc.Any{Type: &irc.Any_List{
			List: info.List,
		}}
	}
	if info.Objectish != nil {
		if info.String != nil || info.List != nil || info.Number != nil {
			trace.ErrorAt(ctx, "only one \"type\" of validation may be specified at once.  For instance, if you use object-ish validation, you may not also use string validation.")
		}

		ref.Constraints = &irc.Any{Type: &irc.Any_Obj{
			Obj: info.Objectish,
		}}
	}
}

func Subtype(ctx context.Context, typesM *Mapper, gv *ire.GroupVersion, subtype ast.SubtypeDecl) *irt.Subtype {
	m := typesM.Item(len(gv.Types)).From(subtype)

	ctx = ast.In(ctx, subtype)
	res := &irt.Subtype{
		Name: subtype.ResolvedName.FullName,
		Docs: Docs(ctx, m.Field("docs"), subtype.Docs),
		Attributes: Markers(ctx, m.Field("attributes"), subtype.Markers),
	}
	gv.Types = append(gv.Types, res) // keep our spot like we recorded above
	switch body := subtype.Body.(type) {
	case *ast.Struct:
		m := m.Field("struct").From(body)
		strct := irt.Struct{}
		// TODO: figure out how to populate preserve-unknown-fields &
		// embedded-object here (maybe parameters on struct() in the idl?)
		fieldM := m.Field("fields")
		for i, field := range body.Fields {
			strct.Fields = append(strct.Fields, Field(ctx, fieldM.Item(i), field))
		}
		// TODO: figure out constraints here
		for _, subtype := range body.Subtypes {
			Subtype(ctx, typesM, gv, subtype)
		}
		res.Type = &irt.Subtype_Struct{Struct: &strct}
	case *ast.Union:
		m := m.Field("union").From(body)
		union := irt.Union{
			Tag: body.Tag,
			Untagged: body.Untagged,
		}
		fieldM := m.Field("variants")
		for i, field := range body.Variants {
			union.Variants = append(union.Variants, Field(ctx, fieldM.Item(i), field))
		}
		// TODO: figure out constraints here
		for _, subtype := range body.Subtypes {
			Subtype(ctx, typesM, gv, subtype)
		}
		res.Type = &irt.Subtype_Union{Union: &union}
	case *ast.Enum:
		m := m.Field("enum").From(body)
		enum := irt.Enum{}
		varM := m.Field("variants")
		for i, variant := range body.Variants {
			ctx := ast.In(ctx, variant)
			m := varM.Item(i).From(variant)
			enum.Variants = append(enum.Variants, &irt.Enum_Variant{
				Name: variant.Name.Name,
				Docs: Docs(ctx, m.Field("docs"), variant.Docs),
				Attributes: Markers(ctx, m.Field("attributes"), variant.Markers),
			})
		}
		res.Type = &irt.Subtype_Enum{Enum: &enum}
	case *ast.Newtype:
		// TODO: mapping recording
		// TODO: check constraints are valid in another pass
		switch typ := body.ResolvedType.Type.(type) {
		case ast.PrimitiveType:
			m.Field("primitive_alias").From(body)
			prim := irt.Primitive{
				Type: irt.Primitive_Type(typ),
			}
			primConstraints(ctx, &prim, body.ResolvedType.Validates)
			res.Type = &irt.Subtype_PrimitiveAlias{PrimitiveAlias: &prim}
		case ast.RefType:
			m.Field("reference_alias").From(body)
			ref := irt.Reference(typ)
			refConstraints(ctx, &ref, body.ResolvedType.Validates)
			res.Type = &irt.Subtype_ReferenceAlias{ReferenceAlias: &ref}
		case ast.ListType:
			m.Field("list").From(body)
			act := irt.List(typ)
			onlyConstrain(ctx, body.ResolvedType.Validates, ast.ListValidation)
			if body.ResolvedType.Validates != nil {
				act.ListConstraints = body.ResolvedType.Validates.List
			}
			res.Type = &irt.Subtype_List{List: &act}
		case ast.SetType:
			m.Field("set").From(body)
			act := irt.Set(typ)
			onlyConstrain(ctx, body.ResolvedType.Validates, ast.ListValidation)
			if body.ResolvedType.Validates != nil {
				act.ListConstraints = body.ResolvedType.Validates.List
			}
			res.Type = &irt.Subtype_Set{Set: &act}
		case ast.ListMapType:
			m.Field("list_map").From(body)
			act := irt.ListMap(typ)
			onlyConstrain(ctx, body.ResolvedType.Validates, ast.ListValidation)
			if body.ResolvedType.Validates != nil {
				act.ListConstraints = body.ResolvedType.Validates.List
			}
			res.Type = &irt.Subtype_ListMap{ListMap: &act}
		case ast.PrimitiveMapType:
			m.Field("primitive_map").From(body)
			act := irt.PrimitiveMap(typ)
			onlyConstrain(ctx, body.ResolvedType.Validates, ast.ObjectishValidation)
			if body.ResolvedType.Validates != nil {
				act.ObjectConstraints = body.ResolvedType.Validates.Objectish
			}
			res.Type = &irt.Subtype_PrimitiveMap{PrimitiveMap: &act}
		default:
			panic("unreachable: unknown newtype")
		}
	}
	return res
}
func Field(ctx context.Context, m *Mapper, field ast.Field) *irt.Field {
	ctx = ast.In(ctx, field)
	m.From(field)
	name := field.Name.Name
	res := irt.Field{
		Name: name,
		Optional: field.ResolvedType.Optional,
		Embedded: name == "", // TODO
		// TODO: zero-means-absent
		Docs: Docs(ctx, m.Field("docs"), field.Docs),
		Attributes: Markers(ctx, m.Field("attributes"), field.Markers),
		ProtoTag: field.ProtoTag,
	}

	// TODO: mapping for default (should values get full map entries?)
	if field.ResolvedType.Default != nil {
		res.Default = Value(ctx, field.ResolvedType.Default)
	}

	switch typ := field.ResolvedType.Type.(type) {
	case ast.PrimitiveType:
		m.Field("primitive").From(field.ResolvedType.TypeSrc)
		prim := irt.Primitive{
			Type: irt.Primitive_Type(typ),
		}
		primConstraints(ctx, &prim, field.ResolvedType.Validates)
		res.Type = &irt.Field_Primitive{Primitive: &prim}
	case ast.RefType:
		m.Field("named_type").From(field.ResolvedType.TypeSrc)
		ref := irt.Reference(typ)
		refConstraints(ctx, &ref, field.ResolvedType.Validates)
		res.Type = &irt.Field_NamedType{NamedType: &ref}
	case ast.ListType:
		m.Field("list").From(field.ResolvedType.TypeSrc)
		act := irt.List(typ)
		onlyConstrain(ctx, field.ResolvedType.Validates, ast.ListValidation)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irt.Field_List{List: &act}
	case ast.SetType:
		m.Field("set").From(field.ResolvedType.TypeSrc)
		act := irt.Set(typ)
		onlyConstrain(ctx, field.ResolvedType.Validates, ast.ListValidation)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irt.Field_Set{Set: &act}
	case ast.ListMapType:
		m.Field("list_map").From(field.ResolvedType.TypeSrc)
		act := irt.ListMap(typ)
		onlyConstrain(ctx, field.ResolvedType.Validates, ast.ListValidation)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irt.Field_ListMap{ListMap: &act}
	case ast.PrimitiveMapType:
		m.Field("primitive_map").From(field.ResolvedType.TypeSrc)
		act := irt.PrimitiveMap(typ)
		onlyConstrain(ctx, field.ResolvedType.Validates, ast.ObjectishValidation)
		if field.ResolvedType.Validates != nil {
			act.ObjectConstraints = field.ResolvedType.Validates.Objectish
		}
		res.Type = &irt.Field_PrimitiveMap{PrimitiveMap: &act}
	default:
		panic("unreachable: unknown newtype")
	}

	return &res
}

func Value(ctx context.Context, value ast.Value) *pstruct.Value {
	switch value := value.(type) {
	case ast.StringVal:
		return &pstruct.Value{
			Kind: &pstruct.Value_StringValue{
				StringValue: value.Value,
			},
		}
	case ast.NumVal:
		return &pstruct.Value{
			Kind: &pstruct.Value_NumberValue{
				// TODO: :-(
				NumberValue: float64(value.Value),
			},
		}
	case ast.BoolVal:
		return &pstruct.Value{
			Kind: &pstruct.Value_BoolValue{
				BoolValue: value.Value,
			},
		}
	case ast.ListVal:
		var vals []*pstruct.Value
		for _, item := range value.Values {
			vals = append(vals, Value(ctx, item))
		}
		return &pstruct.Value{
			Kind: &pstruct.Value_ListValue{
				ListValue: &pstruct.ListValue{Values: vals},
			},
		}
	case ast.StructVal:
		var vals map[string]*pstruct.Value
		for _, item := range value.KeyValues {
			vals[item.Key.Name] = Value(ctx, item.Value)
		}
		return &pstruct.Value{
			Kind: &pstruct.Value_StructValue{
				StructValue: &pstruct.Struct{Fields: vals},
			},
		}

	// TODO: these below could be strong typed
	case ast.FieldPathVal:
		return &pstruct.Value{
			Kind: &pstruct.Value_StringValue{
				StringValue: value.Name,
			},
		}
	// NB: we don't serialize types for now (need to figure out how to do that),
	// but enum variants look like RefTypeVals, so we need to check those
	case ast.RefTypeVal:
		if value.GroupVersion == nil {
			// probably just an enum variant (would've been type-checked elsewhere)
			return &pstruct.Value{
				Kind: &pstruct.Value_StringValue{
					StringValue: value.Name.Name,
				},
			}
		}
		trace.ErrorAt(ctx, "cannot serialize type values")

	// TODO: figure out how to serialize these
	// they're currently mainly just used for list, list-map, etc
	case ast.PrimitiveTypeVal:
		trace.ErrorAt(ctx, "cannot serialize type values")
	case ast.CompoundTypeVal:
		trace.ErrorAt(ctx, "cannot serialize type values")
	default:
		panic("unreachable: unknown value type")
	}
	return nil
}

func MarkerField(ctx context.Context, m *Mapper, field ast.Field) *irm.MarkerField {
	ctx = ast.In(ctx, field)
	m.From(field)
	name := field.Name.Name
	res := irm.MarkerField{
		Name: name,
		Optional: field.ResolvedType.Optional,
		Docs: Docs(ctx, m.Field("docs"), field.Docs),
		Attributes: Markers(ctx, m.Field("attributes"), field.Markers),
		ProtoTag: field.ProtoTag,
	}

	// TODO: mapping for default (should values get full map entries?)
	if field.ResolvedType.Default != nil {
		res.Default = Value(ctx, field.ResolvedType.Default)
	}

	m = m.Field("type")
	// TODO: none of this is quite right because it's build around the normal types
	switch typ := field.ResolvedType.Type.(type) {
	case ast.PrimitiveType:
		m.Field("primitive").From(field.ResolvedType.TypeSrc)
		prim := irt.Primitive{
			Type: irt.Primitive_Type(typ),
		}
		primConstraints(ctx, &prim, field.ResolvedType.Validates)
		res.Type = &irm.Type{Type: &irm.Type_Primitive{Primitive: &prim}}
	case ast.ListType:
		m.Field("list").From(field.ResolvedType.TypeSrc)
		act := irm.List{}
		switch items := typ.Items.(type) {
		case *irt.List_Primitive:
			act.Items = &irm.Type{Type: &irm.Type_Primitive{Primitive: items.Primitive}}
			// TODO: rest of items
		default:
			panic("unreachable: unknown marker field type")
		}
		onlyConstrain(ctx, field.ResolvedType.Validates, ast.ListValidation)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irm.Type{Type: &irm.Type_List{List: &act}}
	case ast.RefType:
		panic("TODO: references in markers" )
	// TODO: typecheck marker fields (no set, list map, special map support, etc)
	default:
		panic("unreachable: unknown marker field type")
	}

	return &res
}

func MarkerDef(ctx context.Context, m *Mapper, decl ast.MarkerDecl) *irm.MarkerDef {
	m.From(decl)
	ctx = ast.In(ctx, decl)

	res := irm.MarkerDef{
		Name: decl.Name.Name,
		Docs: Docs(ctx, m.Field("docs"), decl.Docs),
		Attributes: Markers(ctx, m.Field("attributes"), decl.Markers),
	}

	fieldM := m.Field("fields")
	for i, field := range decl.Fields {
		res.Fields = append(res.Fields, MarkerField(ctx, fieldM.Item(i), field))
	}
	return &res
}

func MarkerSets(ctx context.Context, m *Mapper, astSets []ast.MarkerDeclSet) []*ire.MarkerSet {
	sets := []*ire.MarkerSet{}
	for i, set := range astSets {
		sets = append(sets, MarkerSet(ctx, m.Item(i), set))
	}
	return sets
}

func MarkerSet(ctx context.Context, m *Mapper, set ast.MarkerDeclSet) *ire.MarkerSet {
	m.From(set)
	ctx = ast.In(ctx, set)

	// TODO: ensure package is valid proto package name
	res := ire.MarkerSet{
		Package: set.Package.Name,
	}

	declsM := m.Field("markers")
	for _, decl := range set.MarkerDecls {
		res.Markers = append(res.Markers, MarkerDef(ctx, declsM.Item(len(res.Markers)), decl))
	}

	return &res
}

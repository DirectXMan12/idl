package toir

import (
	"context"
	"strings"

	irt "k8s.io/idl/ckdl-ir/goir/types"
	irc "k8s.io/idl/ckdl-ir/goir/constraints"
	irgv "k8s.io/idl/ckdl-ir/goir/groupver"
	ire "k8s.io/idl/ckdl-ir/goir"
	"github.com/golang/protobuf/ptypes/any"
	pstruct "github.com/golang/protobuf/ptypes/struct"

	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser/ast"
)

// TODO: sourcemap too

func File(ctx context.Context, file *ast.File) ire.GroupVersionSet {
	set := ire.GroupVersionSet{}
	for _, gv := range file.GroupVersions {
		set.GroupVersions = append(set.GroupVersions, GroupVersion(ctx, gv))
	}
	return set
}

func Docs(ctx context.Context, docs ast.Docs) *irt.Documentation {
	ctx = ast.In(ctx, docs)
	res := irt.Documentation{}
	for _, section := range docs.Sections {
		ctx := ast.In(ctx, section)
		switch strings.ToLower(section.Title) {
		case "", "description":
			res.Description = strings.Join(section.Lines, "\n")
		case "example":
			res.Example = strings.Join(section.Lines, "\n")
		case "external ref":
			res.ExternalRef = strings.Join(section.Lines, "\n")
		default:
			trace.ErrorAt(ctx, "unknown documentation section, expected `example` or `external ref`")
		}
	}
	return &res
}
func Markers(ctx context.Context, markers []ast.AbstractMarker) []*any.Any {
	// TODO
	return nil
}

func GroupVersion(ctx context.Context, gv ast.GroupVersion) *ire.GroupVersion {
	ctx = ast.In(ctx, gv)
	res := ire.GroupVersion{
		Description: &irgv.GroupVersion{
			Group: gv.Group,
			Version: gv.Version,
			Docs: Docs(ctx, gv.Docs),
			Attributes: Markers(ctx, gv.Markers),
		},
	}

	for _, decl := range gv.Decls {
		switch decl := decl.(type) {
		case *ast.KindDecl:
			res.Kinds = append(res.Kinds, Kind(ctx, &res, *decl))
		case *ast.SubtypeDecl:
			res.Types = append(res.Types, Subtype(ctx, &res, *decl))
		default:
			panic("unreachable: unknown declaration type")
		}
	}

	return &res
}

func Kind(ctx context.Context, gv *ire.GroupVersion, kind ast.KindDecl) *irt.Kind {
	ctx = ast.In(ctx, kind)
	res := irt.Kind{
		Name: kind.Name.Name,
		Object: true,  // TODO
		Docs: Docs(ctx, kind.Docs),
		Attributes: Markers(ctx, kind.Markers),
	}
	for _, field := range kind.Fields {
		res.Fields = append(res.Fields, Field(ctx, field))
	}
	for _, subtype := range kind.Subtypes {
		gv.Types = append(gv.Types, Subtype(ctx, gv, subtype))
	}
	return &res
}

func primConstraints(ctx context.Context, prim *irt.Primitive, info *ast.ValidatesInfo) {
	if info == nil {
		return
	}

	switch prim.Type {
	case irt.Primitive_LEGACYINT32, irt.Primitive_INT64, irt.Primitive_LEGACYFLOAT64:
		prim.SpecificConstraints = &irt.Primitive_NumericConstraints{
			NumericConstraints: info.Number,
		}
	case irt.Primitive_STRING, irt.Primitive_BYTES:
		prim.SpecificConstraints = &irt.Primitive_StringConstraints{
			StringConstraints: info.String,
		}
	case irt.Primitive_TIME, irt.Primitive_DURATION, irt.Primitive_QUANTITY:
		// TODO(directxman12): bug upstream about validation for these --
		// technically they're string in openapi, but the numeric validations
		// make more sense
	case irt.Primitive_BOOL:
		// nothing to validate for bool
	case irt.Primitive_INTORSTRING:
		// TODO(directxman12): maybe support validation for int-or-string?
	default:
		panic("unreachable: unknown primitive type")
	}
}
func refConstraints(ctx context.Context, ref *irt.Reference, info *ast.ValidatesInfo) {
	if info == nil {
		return
	}
	switch info.ExpectedType {
	case ast.NumberValidation:
		ref.Constraints = &irc.Any{Type: &irc.Any_Num{
			Num: info.Number,
		}}
	case ast.StringValidation:
		ref.Constraints = &irc.Any{Type: &irc.Any_Str{
			Str: info.String,
		}}
	case ast.ListValidation:
		ref.Constraints = &irc.Any{Type: &irc.Any_List{
			List: info.List,
		}}
	case ast.ObjectishValidation:
		ref.Constraints = &irc.Any{Type: &irc.Any_Obj{
			Obj: info.Objectish,
		}}
	}
}

func Subtype(ctx context.Context, gv *ire.GroupVersion, subtype ast.SubtypeDecl) *irt.Subtype {
	ctx = ast.In(ctx, subtype)
	res := irt.Subtype{
		Name: subtype.ResolvedName.FullName,
		Docs: Docs(ctx, subtype.Docs),
		Attributes: Markers(ctx, subtype.Markers),
	}
	switch body := subtype.Body.(type) {
	case *ast.Struct:
		strct := irt.Struct{}
		// TODO: figure out how to populate preserve-unknown-fields &
		// embedded-object here (maybe parameters on struct() in the idl?)
		for _, field := range body.Fields {
			strct.Fields = append(strct.Fields, Field(ctx, field))
		}
		// TODO: figure out constraints here
		for _, subtype := range body.Subtypes {
			gv.Types = append(gv.Types, Subtype(ctx, gv, subtype))
		}
		res.Type = &irt.Subtype_Struct{Struct: &strct}
	case *ast.Union:
		union := irt.Union{
			Tag: body.Tag,
			Untagged: body.Untagged,
		}
		for _, field := range body.Variants {
			union.Variants = append(union.Variants, Field(ctx, field))
		}
		// TODO: figure out constraints here
		for _, subtype := range body.Subtypes {
			gv.Types = append(gv.Types, Subtype(ctx, gv, subtype))
		}
		res.Type = &irt.Subtype_Union{Union: &union}
	case *ast.Enum:
		enum := irt.Enum{}
		for _, variant := range body.Variants {
			ctx := ast.In(ctx, variant)
			enum.Variants = append(enum.Variants, &irt.Enum_Variant{
				Name: variant.Name.Name,
				Docs: Docs(ctx, variant.Docs),
				Attributes: Markers(ctx, variant.Markers),
			})
		}
		res.Type = &irt.Subtype_Enum{Enum: &enum}
	case *ast.Newtype:
		// TODO: check constraints are valid in another pass
		switch typ := body.ResolvedType.Type.(type) {
		case ast.PrimitiveType:
			prim := irt.Primitive{
				Type: irt.Primitive_Type(typ),
			}
			primConstraints(ctx, &prim, body.ResolvedType.Validates)
			res.Type = &irt.Subtype_PrimitiveAlias{PrimitiveAlias: &prim}
		case ast.RefType:
			ref := irt.Reference(typ)
			refConstraints(ctx, &ref, body.ResolvedType.Validates)
			res.Type = &irt.Subtype_ReferenceAlias{ReferenceAlias: &ref}
		case ast.ListType:
			act := irt.List(typ)
			if body.ResolvedType.Validates != nil {
				act.ListConstraints = body.ResolvedType.Validates.List
			}
			res.Type = &irt.Subtype_List{List: &act}
		case ast.SetType:
			act := irt.Set(typ)
			if body.ResolvedType.Validates != nil {
				act.ListConstraints = body.ResolvedType.Validates.List
			}
			res.Type = &irt.Subtype_Set{Set: &act}
		case ast.ListMapType:
			act := irt.ListMap(typ)
			if body.ResolvedType.Validates != nil {
				act.ListConstraints = body.ResolvedType.Validates.List
			}
			res.Type = &irt.Subtype_ListMap{ListMap: &act}
		case ast.PrimitiveMapType:
			act := irt.PrimitiveMap(typ)
			if body.ResolvedType.Validates != nil {
				act.ObjectConstraints = body.ResolvedType.Validates.Objectish
			}
			res.Type = &irt.Subtype_PrimitiveMap{PrimitiveMap: &act}
		default:
			panic("unreachable: unknown newtype")
		}
	}
	return &res
}
func Field(ctx context.Context, field ast.Field) *irt.Field {
	ctx = ast.In(ctx, field)
	name := field.Name.Name
	if name == "_inline" { // TODO: this should happen during parsing
		name = ""
	}
	res := irt.Field{
		Name: name,
		Optional: field.ResolvedType.Optional,
		Embedded: name == "", // TODO
		// TODO: zero-means-absent
		Docs: Docs(ctx, field.Docs),
		Attributes: Markers(ctx, field.Markers),
	}

	if field.ResolvedType.Default != nil {
		res.Default = Value(ctx, field.ResolvedType.Default)
	}

	// TODO: proto tag

	switch typ := field.ResolvedType.Type.(type) {
	case ast.PrimitiveType:
		prim := irt.Primitive{
			Type: irt.Primitive_Type(typ),
		}
		primConstraints(ctx, &prim, field.ResolvedType.Validates)
		res.Type = &irt.Field_Primitive{Primitive: &prim}
	case ast.RefType:
		ref := irt.Reference(typ)
		refConstraints(ctx, &ref, field.ResolvedType.Validates)
		res.Type = &irt.Field_NamedType{NamedType: &ref}
	case ast.ListType:
		act := irt.List(typ)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irt.Field_List{List: &act}
	case ast.SetType:
		act := irt.Set(typ)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irt.Field_Set{Set: &act}
	case ast.ListMapType:
		act := irt.ListMap(typ)
		if field.ResolvedType.Validates != nil {
			act.ListConstraints = field.ResolvedType.Validates.List
		}
		res.Type = &irt.Field_ListMap{ListMap: &act}
	case ast.PrimitiveMapType:
		act := irt.PrimitiveMap(typ)
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
	// TODO: typecheck default in a different pass
	return nil
}

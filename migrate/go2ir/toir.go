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
	"go/types"
	"strings"
	"strconv"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"google.golang.org/protobuf/types/known/anypb"

	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
	irt "k8s.io/idl/ckdl-ir/goir/types"
	irvalid "k8s.io/idl/ckdl-ir/goir/constraints"
	"k8s.io/idl/migrate/srcmap"
)

// Schema flattening is done in a recursive mapping method.
// Start reading at infoToSchema.

const (
	// defPrefix is the prefix used to link to definitions in the OpenAPI schema.
	defPrefix = "#/definitions/"
)

var (
	// byteType is the types.Type for byte (see the types documention
	// for why we need to look this up in the Universe), saved
	// for quick comparison.
	byteType = types.Universe.Lookup("byte").Type()
)

// SchemaMarker is any marker that needs to modify the schema of the underlying type or field.
type SchemaMarker interface {
	// ApplyToSchema is called after the rest of the schema for a given type
	// or field is generated, to modify the schema appropriately.
	ApplyToSchema(*apiext.JSONSchemaProps) error
}

// applyFirstMarker is applied before any other markers.  It's a bit of a hack.
type applyFirstMarker interface {
	ApplyFirst()
}

// descRequester knows how to marker that another schema (e.g. via an external reference) is necessary.
type descRequester interface {
	NeedDescFor(typ TypeIdent)
	GroupVersionFor(src, pkg *loader.Package) *irt.GroupVersionRef
}

// schemaContext stores and provides information across a hierarchy of schema generation.
type infoContext struct {
	pkg  *loader.Package

	info *markers.TypeInfo

	descRequester descRequester
	PackageMarkers  markers.MarkerValues

	allowDangerousTypes bool

	spanTracker *srcmap.SpanTracker
}

type GenContext struct {
	*infoContext

	span *srcmap.SpanContext
}

// NewGenContext constructs a new genContext for the given package and schema requester.
// It must have type info added before use via ForInfo.
func NewGenContext(pkg *loader.Package, req descRequester, allowDangerousTypes bool) *GenContext {
	pkg.NeedTypesInfo()
	return &GenContext{
		infoContext: &infoContext{
			pkg:                 pkg,
			descRequester:     req,
			allowDangerousTypes: allowDangerousTypes,
			spanTracker: srcmap.Track(pkg.Fset),
		},
	}
}

// ForInfo produces a new GenContext containing the same information
// as this one, except with the given type information.
func (c *GenContext) ForInfo(info *markers.TypeInfo) *GenContext {
	return &GenContext{
		infoContext: &infoContext{
			pkg:                 c.pkg,
			info:                info,
			descRequester:     c.descRequester,
			allowDangerousTypes: c.allowDangerousTypes,
			spanTracker: c.spanTracker,
		},
		span: c.span,
	}
}

func (c *GenContext) Error(node ast.Node, msg string, args ...interface{}) {
	c.pkg.AddError(loader.ErrFromNode(fmt.Errorf(msg, args...), node))
}

func (c *GenContext) KindSpan(msg *irt.Kind) *GenContext{
	return &GenContext{
		infoContext: c.infoContext,
		span: c.spanTracker.ForKind(msg, c.descRequester.GroupVersionFor(c.pkg, c.pkg)),
	}
}
func (c *GenContext) SubtypeSpan(msg *irt.Subtype) *GenContext{
	// TODO: gracefully deal with missing GV
	return &GenContext{
		infoContext: c.infoContext,
		span: c.spanTracker.ForSubtype(msg, c.descRequester.GroupVersionFor(c.pkg, c.pkg)),
	}
}
func (c *GenContext) FieldSpan(name string) *GenContext{
	return &GenContext{
		infoContext: c.infoContext,
		span: c.span.ForField(name),
	}
}
func (c *GenContext) IndexSpan(idx int) *GenContext{
	return &GenContext{
		infoContext: c.infoContext,
		span: c.span.ForIndex(idx),
	}
}

func (c *GenContext) Record(node ast.Node) *GenContext{
	c.spanTracker.Record(c.span, node)
	return c
}

// requestSchema asks for the schema for a type in the package with the
// given import path.
func (c *GenContext) requestSchema(pkgPath, typeName string) {
	pkg := c.pkg
	if pkgPath != "" {
		pkg = c.pkg.Imports()[pkgPath]
	}
	c.descRequester.NeedDescFor(TypeIdent{
		Package: pkg,
		Name:    typeName,
	})
}

func (c *GenContext) groupVerFor(pkgPath string) *irt.GroupVersionRef {
	pkg := c.pkg
	if pkgPath != "" {
		pkg = c.pkg.Imports()[pkgPath]
	}
	return c.descRequester.GroupVersionFor(c.pkg, pkg)
}

func InfoToKind(ctx *GenContext) *irt.Kind {
	// kinds can't have custom serialization, no need to check for it here

	res := &irt.Kind{
		Name: ctx.info.Name,
		Object: /* TODO */ true,
	}
	ctx = ctx.KindSpan(res).Record(ctx.info.RawSpec)

	structInfo, isStruct := ctx.info.RawSpec.Type.(*ast.StructType)
	if !isStruct {
		ctx.Error(ctx.info.RawSpec.Type, "encountered kind that wasn't a struct")
		return nil
	}
	res.Fields = structToFields(ctx.FieldSpan("fields"), structInfo, true)
	res.Docs = &irt.Documentation{
		Description: ctx.info.Doc,
	}

	return res
}

func InfoToSubtype(ctx *GenContext) *irt.Subtype {
	// TODO: check for custom serialization

	res := &irt.Subtype{
		Name: ctx.info.Name,
	}
	ctx = ctx.SubtypeSpan(res).Record(ctx.info.RawSpec)

	switch info := ctx.info.RawSpec.Type.(type) {
	case *ast.StructType:
		ctx := ctx.FieldSpan("struct")
		res.Type = &irt.Subtype_Struct{
			Struct: &irt.Struct{
				Fields: structToFields(ctx.FieldSpan("fields"), info, false),
			},
		}
	case *ast.Ident:
		// TODO: fieldspan
		prim, ref := localNamedToPrimitiveOrRef(ctx, info)
		switch {
		case prim != nil:
			res.Type = &irt.Subtype_PrimitiveAlias{PrimitiveAlias: prim}
		case ref != nil:
			res.Type = &irt.Subtype_ReferenceAlias{ReferenceAlias: ref}
		}
	case *ast.MapType:
		ctx := ctx.FieldSpan("primitive_map")
		irMap := mapToPrimitiveMap(ctx, info)
		res.Type = &irt.Subtype_PrimitiveMap{PrimitiveMap: irMap}
	default:
		panic(fmt.Sprintf("TODO: %T", info))
	}
	res.Docs = &irt.Documentation{
		Description: ctx.info.Doc,
	}
	return res
}

func structToFields(ctx *GenContext, structType *ast.StructType, isKind bool) []*irt.Field {
	// double-check that we aren't trying to parse something embedded
	if ctx.info.RawSpec.Type != structType {
		ctx.Error(structType, "encountered non-top-level struct (possibly embedded), those aren't allowed")
		return nil
	}
	structCtx := ctx

	fields := make([]*irt.Field, 0, len(ctx.info.Fields))

	for fieldInd, field := range ctx.info.Fields {
		ctx = structCtx.IndexSpan(fieldInd).Record(field.RawField)

		jsonTag, hasTag := field.Tag.Lookup("json")
		if !hasTag {
			// if the field doesn't have a JSON tag, it doesn't belong in output (and shouldn't exist in a serialized type)
			ctx.Error(field.RawField, "encountered struct field %q without JSON tag in type %q", field.Name, ctx.info.Name)
			continue
		}
		jsonOpts := strings.Split(jsonTag, ",")
		if len(jsonOpts) == 1 && jsonOpts[0] == "-" {
			// skipped fields have the tag "-" (note that "-," means the field is named "-")
			continue
		}

		inline := false
		omitEmpty := false
		for _, opt := range jsonOpts[1:] {
			switch opt {
			case "inline":
				inline = true
			case "omitempty":
				omitEmpty = true
			}
		}
		fieldName := jsonOpts[0]
		inline = inline || fieldName == "" // anonymous fields are inline fields in YAML/JSON

		// if no default required mode is set, default to required
		defaultMode := "required"
		if ctx.PackageMarkers.Get("kubebuilder:validation:Optional") != nil {
			defaultMode = "optional"
		}

		protoTag := uint32(0)
		protoInfo, hasProto := field.Tag.Lookup("protobuf")
		if hasProto {
			protoTagRaw := strings.Split(protoInfo, ",")[1]
			protoTagParsed, err := strconv.ParseUint(protoTagRaw, 10, 32)
			protoTag = uint32(protoTagParsed)
			if err != nil {
				// TODO
				panic(err)
			}
		}

		irField := &irt.Field{
			Name: fieldName,
			Embedded: inline,
			Optional: true,
			Docs: &irt.Documentation{
				Description: field.Doc,
			},
			ProtoTag: protoTag,
		}

		if fieldName != "" && field.Name == "" {
			// TODO: capture embedded-in-Go-non-inline and do something with it
			//ctx.Error(field.RawField, "encountered Go-embedded non-inline field, skipping")
			//continue
		}

		switch defaultMode {
		// if this package isn't set to optional default...
		case "required":
			// ...everything that's not omitempty or explicitly optional is required
			if !omitEmpty && field.Markers.Get("kubebuilder:validation:Optional") == nil && field.Markers.Get("optional") == nil {
				irField.Optional = false
			}

		// if this package isn't set to required default...
		case "optional":
			// ...everything that isn't explicitly required is optional
			if field.Markers.Get("kubebuilder:validation:Required") != nil {
				irField.Optional = false
			}
		}

		ctx = ctx.ForInfo(&markers.TypeInfo{})
		fieldType, wasPointer := unwrapPointers(field.RawField.Type)
		if !wasPointer {
			// TODO: just a guess right now
			// TODO: guess in cases of pointer-ish types?

			// non-pointer types use a zero-value to mean unset
			irField.ZeroMeansAbsent = true
		}
		// ctx.FieldSpan("type").Record(fieldType) // TODO: this is a oneof, so it's not a distinct field any more

		// unwrap pointers
		switch fieldType := fieldType.(type) {
		case *ast.Ident:
			prim, ref := localNamedToPrimitiveOrRef(ctx, fieldType)
			switch {
			case prim != nil:
				irField.Type = &irt.Field_Primitive{Primitive: prim}
			case ref != nil:
				irField.Type = &irt.Field_NamedType{NamedType: ref}
			}
		case *ast.SelectorExpr:
			prim, ref := namedToPrimitiveOrRef(ctx, fieldType)
			switch {
			case prim != nil:
				irField.Type = &irt.Field_Primitive{Primitive: prim}
			case ref != nil:
				irField.Type = &irt.Field_NamedType{NamedType: ref}
			}
		case *ast.ArrayType:
			switch irNode := arrayToIR(ctx, fieldType, field.Markers).(type) {
			case *irt.Primitive:
				irField.Type = &irt.Field_Primitive{Primitive: irNode}
			case *irt.Set:
				irField.Type = &irt.Field_Set{Set: irNode}
			case *irt.ListMap:
				irField.Type = &irt.Field_ListMap{ListMap: irNode}
			case *irt.List:
				irField.Type = &irt.Field_List{List: irNode}
			}
			// default means error occurred
		case *ast.MapType:
			irMap := mapToPrimitiveMap(ctx.FieldSpan("primitive_map"), fieldType)
			irField.Type = &irt.Field_PrimitiveMap{PrimitiveMap: irMap}
		default:
			ctx.Error(fieldType, "unsupported AST kind %T for field", fieldType)
			// NB(directxman12): we explicitly don't handle interfaces
			continue
		}

		if isKind {
			// skip objectmeta and typemeta on root types, they're implied by the "kind" specifier
			if fieldName == "metadata" {
				continue
			}
			if inline {
				ref, isRef := irField.Type.(*irt.Field_NamedType)
				if isRef && ref.NamedType.GroupVersion.Group == "meta.k8s.io" && ref.NamedType.Name == "TypeMeta" {
					continue
				}
			}
		}

		if strings.Title(fieldName) != field.Name {
			// weirdness down the line around captialization, preserve so we can round-trip
			// properly
			any, err := anypb.New(&Name{Name: field.Name})
			if err != nil {
				// TODO
				panic(err)
			}
			irField.Attributes = append(irField.Attributes, any)
		}

		// TODO: markers
		// applyMarkers(ctx, field.Markers, propSchema, field.RawField)

		fields = append(fields, irField)
	}

	return fields
}

// mapToPrimitiveMap creates a "primitive map" (akin to a Go map instead of a kubernetes-style
// orderedmap) from a go map.  Key types must eventually resolve to string as per kubernetes API
// standards, and the value must be a primitive or a slice thereof.  This will be type-checked later.
func mapToPrimitiveMap(ctx *GenContext, mapType *ast.MapType) *irt.PrimitiveMap {
	resMap := &irt.PrimitiveMap{}
	ctx = ctx.ForInfo(&markers.TypeInfo{})
	switch keyType := mapType.Key.(type) {
	case *ast.Ident:
		prim, ref := localNamedToPrimitiveOrRef(ctx, keyType)
		switch {
		case prim != nil:
			ctx.FieldSpan("primitive_key").Record(keyType)
			resMap.Key = &irt.PrimitiveMap_PrimitiveKey{PrimitiveKey: prim}
		case ref != nil:
			ctx.FieldSpan("reference_key").Record(keyType)
			resMap.Key = &irt.PrimitiveMap_ReferenceKey{ReferenceKey: ref}
		}
	case *ast.SelectorExpr:
		prim, ref := namedToPrimitiveOrRef(ctx, keyType)
		switch {
		case prim != nil:
			ctx.FieldSpan("primitive_key").Record(keyType)
			resMap.Key = &irt.PrimitiveMap_PrimitiveKey{PrimitiveKey: prim}
		case ref != nil:
			ctx.FieldSpan("reference_key").Record(keyType)
			resMap.Key = &irt.PrimitiveMap_ReferenceKey{ReferenceKey: ref}
		}
	default:
		ctx.Error(mapType.Key, "primitive (non-ordered) maps must have string or string-alias keys, not %T", mapType.Key)
	}

	switch valType := mapType.Value.(type) {
	case *ast.Ident:
		prim, ref := localNamedToPrimitiveOrRef(ctx, valType)
		switch {
		case prim != nil:
			ctx.FieldSpan("primitive_value").Record(valType)
			resMap.Value = &irt.PrimitiveMap_PrimitiveValue{PrimitiveValue: prim}
		case ref != nil:
			ctx.FieldSpan("reference_value").Record(valType)
			resMap.Value = &irt.PrimitiveMap_ReferenceValue{ReferenceValue: ref}
		}
	case *ast.SelectorExpr:
		prim, ref := namedToPrimitiveOrRef(ctx, valType)
		switch {
		case prim != nil:
			ctx.FieldSpan("primitive_value").Record(valType)
			resMap.Value = &irt.PrimitiveMap_PrimitiveValue{PrimitiveValue: prim}
		case ref != nil:
			ctx.FieldSpan("reference_value").Record(valType)
			resMap.Value = &irt.PrimitiveMap_ReferenceValue{ReferenceValue: ref}
		}
	case *ast.ArrayType:
		// TODO: this is going to break from the span field names
		if bytesPrim := asBytes(ctx.FieldSpan("primitive_value"), valType); bytesPrim != nil {
			ctx.FieldSpan("primitive_value").Record(valType)
			resMap.Value = &irt.PrimitiveMap_PrimitiveValue{PrimitiveValue: bytesPrim}
		} else {
			arr := &irt.List{}
			ctx = ctx.FieldSpan("simple_array_value").Record(valType)
			ctx = ctx.FieldSpan("items").Record(valType.Elt)

			switch itemType := valType.Elt.(type) {
			case *ast.Ident:
				prim, ref := localNamedToPrimitiveOrRef(ctx, itemType)
				switch {
				case prim != nil:
					arr.Items = &irt.List_Primitive{Primitive: prim}
				case ref != nil:
					arr.Items = &irt.List_Reference{Reference: ref}
				}
			case *ast.SelectorExpr:
				prim, ref := namedToPrimitiveOrRef(ctx, itemType)
				switch {
				case prim != nil:
					arr.Items = &irt.List_Primitive{Primitive: prim}
				case ref != nil:
					arr.Items = &irt.List_Reference{Reference: ref}
				}
			default:
				ctx.Error(itemType, "unknown/invalid array item type %T (must be primitive or reference)", itemType)
				return nil
			}

			resMap.Value = &irt.PrimitiveMap_SimpleListValue{SimpleListValue: arr}
		}
	default:
		ctx.Error(mapType.Value, "map values must be a named type, a simple slice, or a primitive, not %T", mapType.Value)
		return nil
	}

	return resMap
}

// asBytes attempts to interpret the given array as `[]byte`, returning nil if it's
// not, or a corresponding primitive IR node if it is.
func asBytes(ctx *GenContext, array *ast.ArrayType) *irt.Primitive {
	eltType := ctx.pkg.TypesInfo.TypeOf(array.Elt)
	if eltType == byteType && array.Len == nil {
		// byte slices are represented as base64-encoded strings
		// (the format is defined in OpenAPI v3, but not JSON Schema)
		return &irt.Primitive{
			Type: irt.Primitive_BYTES,
		}
	}
	return nil
}

// arrayToIR creates an IR node for the items of the given array, dealing appropriately
// with the special `[]byte` type (according to OpenAPI standards), as well as sets,
// and maps.  The output type is either *irt.Primitive, *irt.Map, *irt.List, or *irt.Set
// markerSet should either be type markers (for a type alias) or field markers (for a field).
// it will index into the appropriate field for spans as well ("set"/"array"/"map")
func arrayToIR(ctx *GenContext, array *ast.ArrayType, markerSet markers.MarkerValues) interface{} {
	eltType := ctx.pkg.TypesInfo.TypeOf(array.Elt)
	if eltType == byteType && array.Len == nil {
		// byte slices are represented as base64-encoded strings
		// (the format is defined in OpenAPI v3, but not JSON Schema)
		return &irt.Primitive{
			Type: irt.Primitive_BYTES,
		}
	}

	// figure out if this is a set, array, or map
	fakeSchema := &apiext.JSONSchemaProps{Type: "array"}
	applyMarkers(ctx, markerSet, fakeSchema, array)
	recordedListType := ""
	if fakeSchema.XListType != nil {
		recordedListType = *fakeSchema.XListType
	}

	// the SMD schemaconv code falls back to using some extensions that aren't publically
	// exposed sometimes, so we should tackle those too.
	// namely, according to SMD, the presence of x-kubernetes-patch-strategy=merge implies either
	// set or list-map, disambiguated by the presence of x-kubernetes-patch-merge-key (present -->
	// map, absent --> set).
	if recordedListType == "" {
		// TODO: preserve retainKeys via a marker
		if strat, hasStrat := markerSet.Get("patchStrategy").(markers.RawArguments); hasStrat && (string(strat) == "merge" || string(strat) == "merge,retainKeys") {
			key, hasKey := markerSet.Get("patchMergeKey").(string)

			if hasKey {
				recordedListType = "map"
				fakeSchema.XListMapKeys = []string{key}
			} else {
				recordedListType = "set"
			}
		}
	}

	ctx = ctx.ForInfo(&markers.TypeInfo{})
	switch recordedListType {
	case "", "atomic":
		// array
		res := &irt.List{}
		//ctx = ctx.FieldSpan("list").FieldSpan("items").Record(array.Elt) // TODO: oneof, not field
		ctx = ctx.FieldSpan("list").Record(array.Elt)
		switch itemType := array.Elt.(type) {
		case *ast.Ident:
			prim, ref := localNamedToPrimitiveOrRef(ctx, itemType)
			switch {
			case prim != nil:
				res.Items = &irt.List_Primitive{Primitive: prim}
			case ref != nil:
				res.Items = &irt.List_Reference{Reference: ref}
			}
		case *ast.SelectorExpr:
			prim, ref := namedToPrimitiveOrRef(ctx, itemType)
			switch {
			case prim != nil:
				res.Items = &irt.List_Primitive{Primitive: prim}
			case ref != nil:
				res.Items = &irt.List_Reference{Reference: ref}
			}
		default:
			ctx.Error(itemType, "unknown/invalid array item type %T (must be primitive or reference)", itemType)
			return nil
		}
		return res
	case "set":
		// set
		res := &irt.Set{}
		//ctx = ctx.FieldSpan("set").FieldSpan("items").Record(array.Elt) // TODO: oneof, not field
		ctx = ctx.FieldSpan("set").Record(array.Elt)
		switch itemType := array.Elt.(type) {
		case *ast.Ident:
			prim, ref := localNamedToPrimitiveOrRef(ctx, itemType)
			switch {
			case prim != nil:
				res.Items = &irt.Set_Primitive{Primitive: prim}
			case ref != nil:
				res.Items = &irt.Set_Reference{Reference: ref}
			}
		case *ast.SelectorExpr:
			prim, ref := namedToPrimitiveOrRef(ctx, itemType)
			switch {
			case prim != nil:
				res.Items = &irt.Set_Primitive{Primitive: prim}
			case ref != nil:
				res.Items = &irt.Set_Reference{Reference: ref}
			}
		default:
			ctx.Error(itemType, "unknown/invalid set item type %T (must be primitive or reference to one)", itemType)
			return nil
		}
		return res
	case "map":
		// map
		res := &irt.ListMap{
			KeyField: fakeSchema.XListMapKeys,
		}
		ctx = ctx.FieldSpan("list_map")
		ctx.FieldSpan("items").Record(array.Elt)
		switch itemType := array.Elt.(type) {
		case *ast.Ident:
			_, ref := localNamedToPrimitiveOrRef(ctx, itemType)
			if ref == nil {
				ctx.Error(itemType, "unknown/invalid map item type %T (must be reference to struct)", itemType)
				return nil
			}
			res.Items = ref
		case *ast.SelectorExpr:
			_, ref := namedToPrimitiveOrRef(ctx, itemType)
			if ref == nil {
				ctx.Error(itemType, "unknown/invalid map item type %T (must be reference to struct)", itemType)
				return nil
			}
			res.Items = ref
		default:
			ctx.Error(itemType, "unknown/invalid set item type %T (must be primitive or reference to one)", itemType)
			return nil
		}

		return res
	default:
		ctx.Error(array, "unknown list type %q", recordedListType)
		return nil
	}
}

func checkKnownPrimitives(name, nonVendorPath string) *irt.Primitive {
	switch {
	// TODO: constants for these
	case nonVendorPath == "k8s.io/apimachinery/pkg/api/resource" && name == "Quantity":
		return &irt.Primitive{
			Type: irt.Primitive_QUANTITY,
		}
	case nonVendorPath == "k8s.io/apimachinery/pkg/apis/meta/v1" && (name == "Time" || name == "MicroTime"):
		return &irt.Primitive{
			Type: irt.Primitive_TIME,
		}
	case nonVendorPath == "k8s.io/apimachinery/pkg/apis/meta/v1" && name == "Duration":
		return &irt.Primitive{
			Type: irt.Primitive_DURATION,
		}
	case nonVendorPath == "k8s.io/apimachinery/pkg/util/intstr" && name == "IntOrString":
		return &irt.Primitive{
			Type: irt.Primitive_INTORSTRING,
		}
	default:
		return nil
	}
}

// localNamedToReforPrimitive converts an ast.Ident to either a primitive or in-package
// reference, depending on the ident.  It will only return one of its values as non-nil.
func localNamedToPrimitiveOrRef(ctx *GenContext, ident *ast.Ident) (*irt.Primitive, *irt.Reference) {
	typeInfo := ctx.pkg.TypesInfo.TypeOf(ident)
	if typeInfo == types.Typ[types.Invalid] {
		ctx.Error(ident, "unknown type %s", ident.Name)
		return nil, nil
	}
	if basicInfo, isBasic := typeInfo.(*types.Basic); isBasic {
		prim, err := builtinToPrimitive(basicInfo, ctx.allowDangerousTypes)
		if err != nil {
			ctx.pkg.AddError(loader.ErrFromNode(err, ident))
		}
		return prim, nil
	}
	// NB(directxman12): if there are dot imports, this might be an external reference,
	// so use typechecking info to get the actual object
	typeNameInfo := typeInfo.(*types.Named).Obj()
	pkg := typeNameInfo.Pkg()
	pkgPath := loader.NonVendorPath(pkg.Path())
	// check this before clearing the pkgPath, since we might be in the primitive's support
	// package itself (e.g. metav1.ObjectMeta references metav1.Time)
	if knownPrim := checkKnownPrimitives(typeNameInfo.Name(), pkgPath); knownPrim != nil {
		return knownPrim, nil
	}
	if pkg == ctx.pkg.Types {
		pkgPath = ""
	}
	ctx.requestSchema(pkgPath, typeNameInfo.Name())
	gv := ctx.groupVerFor(pkgPath)
	return nil, &irt.Reference{
		Name: typeNameInfo.Name(),
		GroupVersion: &irt.GroupVersionRef{
			Group: gv.Group,
			Version: gv.Version,
		},
	}
}

// namedToRef creates a schema (ref) for an explicitly external type reference or
// primitive, in the case of resource.Quantity, metav1.Time, or metav1.Duration
func namedToPrimitiveOrRef(ctx *GenContext, named *ast.SelectorExpr) (*irt.Primitive, *irt.Reference) {
	typeInfoRaw := ctx.pkg.TypesInfo.TypeOf(named)
	if typeInfoRaw == types.Typ[types.Invalid] {
		ctx.Error(named, "unknown type %v.%s", named.X, named.Sel.Name)
		return nil, nil
	}
	typeInfo := typeInfoRaw.(*types.Named)
	typeNameInfo := typeInfo.Obj()
	nonVendorPath := loader.NonVendorPath(typeNameInfo.Pkg().Path())

	// special-case pseudo-primitives like Quantity
	name := typeNameInfo.Name()

	if knownPrim := checkKnownPrimitives(name, nonVendorPath); knownPrim != nil {
		return knownPrim, nil
	}

	ctx.requestSchema(nonVendorPath, typeNameInfo.Name())
	gv := ctx.groupVerFor(nonVendorPath)
	if gv == nil {
		panic(fmt.Sprintf("unknown group-version for %q", nonVendorPath))
	}
	return nil, &irt.Reference{
		Name: typeNameInfo.Name(),
		GroupVersion: &irt.GroupVersionRef{
			Group: gv.Group,
			Version: gv.Version,
		},
	}
}

// unwrapPointers removes any layers of pointers, returning the resulting underlying type
// and if any pointers were unwrapped
func unwrapPointers(expr ast.Expr) (ast.Expr, bool) {
	unwrapped := false
	for starExpr, isStar := expr.(*ast.StarExpr); isStar; starExpr, isStar = expr.(*ast.StarExpr) {
		expr = starExpr.X
		unwrapped = true
	}
	return expr, unwrapped
}

// builtinToType converts builtin basic types to their equivalent JSON schema form.
// It *only* handles types allowed by the kubernetes API standards. Floats are not
// allowed unless allowDangerousTypes is true
func builtinToPrimitive(basic *types.Basic, allowDangerousTypes bool) (*irt.Primitive, error) {
	// NB(directxman12): formats from OpenAPI v3 are slightly different than those defined
	// in JSONSchema.  This'll use the OpenAPI v3 ones, since they're useful for bounding our
	// non-string types.
	basicInfo := basic.Info()
	switch {
	case basicInfo&types.IsBoolean != 0:
		return &irt.Primitive{
			Type: irt.Primitive_BOOL,
		}, nil
	case basicInfo&types.IsString != 0:
		return &irt.Primitive{
			Type: irt.Primitive_STRING,
		}, nil
	case basicInfo&types.IsInteger != 0:
		var res *irt.Primitive
		kind := basic.Kind()
		switch kind {
		case types.Int32, types.Uint32:
			res = &irt.Primitive{
				Type: irt.Primitive_LEGACYINT32,
			}
		case types.Int64, types.Uint64:
			res = &irt.Primitive{
				Type: irt.Primitive_INT64,
			}
		}

		// TODO: uint32 is actually larger than int32 -- we should have separate types in proto
		if kind == types.Uint32 || kind == types.Uint64 {
			res.SpecificConstraints = &irt.Primitive_NumericConstraints{
				NumericConstraints: &irvalid.Numeric{Minimum: 0},
			}
		}
		return res, nil
	case basicInfo&types.IsFloat != 0 && allowDangerousTypes:
		return &irt.Primitive{
			Type: irt.Primitive_LEGACYFLOAT64,
		}, nil
	default:
		// NB(directxman12): floats are *NOT* allowed in kubernetes APIs
		return nil, fmt.Errorf("unsupported type %q", basic.String())
	}
}

// applyMarkers applies schema markers to the given schema, respecting "apply first" markers.
// we use this to "cheat" a bit when applying the markers.
func applyMarkers(ctx *GenContext, markerSet markers.MarkerValues, props *apiext.JSONSchemaProps, node ast.Node) {
	// apply "apply first" markers first...
	for _, markerValues := range markerSet {
		for _, markerValue := range markerValues {
			if _, isApplyFirst := markerValue.(applyFirstMarker); !isApplyFirst {
				continue
			}

			schemaMarker, isSchemaMarker := markerValue.(SchemaMarker)
			if !isSchemaMarker {
				continue
			}

			if err := schemaMarker.ApplyToSchema(props); err != nil {
				ctx.pkg.AddError(loader.ErrFromNode(err /* an okay guess */, node))
			}
		}
	}

	// ...then the rest of the markers
	for _, markerValues := range markerSet {
		for _, markerValue := range markerValues {
			if _, isApplyFirst := markerValue.(applyFirstMarker); isApplyFirst {
				// skip apply-first markers, which were already applied
				continue
			}

			schemaMarker, isSchemaMarker := markerValue.(SchemaMarker)
			if !isSchemaMarker {
				continue
			}
			if err := schemaMarker.ApplyToSchema(props); err != nil {
				ctx.pkg.AddError(loader.ErrFromNode(err /* an okay guess */, node))
			}
		}
	}
}

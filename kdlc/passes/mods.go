package passes

import (
	"context"
	"fmt"

	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser/ast"
	ir "k8s.io/idl/ckdl-ir/goir/types"
	irc "k8s.io/idl/ckdl-ir/goir/constraints"
)

// TODO(directxman12): we could make all of this easier to expand & cleaner with reflection,
// or perhaps a global map of keys to setters


func updateValidates(ctx context.Context, v *ast.ValidatesInfo, kv ast.KeyValue) {
	// TODO: check for duplicates all at once

	ctx = trace.InSpan(trace.Describe(ctx, "validator"), kv)
	switch kv.Key.Name {
	case "max":
		if v.Number == nil {
			v.Number = &irc.Numeric{}
		}
		v.Number.Maximum = int64(assertNumber(ctx, kv.Value))
	case "min":
		if v.Number == nil {
			v.Number = &irc.Numeric{}
		}
		v.Number.Minimum = int64(assertNumber(ctx, kv.Value))
	case "exclusive-max":
		if v.Number == nil {
			v.Number = &irc.Numeric{}
		}
		v.Number.ExclusiveMaximum = assertBool(ctx, kv.Value)
	case "exclusive-min":
		if v.Number == nil {
			v.Number = &irc.Numeric{}
		}
		v.Number.ExclusiveMinimum = assertBool(ctx, kv.Value)
	case "multiple-of":
		if v.Number == nil {
			v.Number = &irc.Numeric{}
		}
		v.Number.MultipleOf = int64(assertNumber(ctx, kv.Value))

	case "max-length":
		if v.String == nil {
			v.String = &irc.String{}
		}
		v.String.MaxLength = uint64(assertUNumber(ctx, kv.Value))
	case "min-length":
		if v.String == nil {
			v.String = &irc.String{}
		}
		v.String.MinLength = uint64(assertUNumber(ctx, kv.Value))
	case "pattern":
		if v.String == nil {
			v.String = &irc.String{}
		}
		v.String.Pattern = assertString(ctx, kv.Value)

	case "max-items":
		if v.List == nil {
			v.List = &irc.List{}
		}
		v.List.MaxItems = uint64(assertUNumber(ctx, kv.Value))
	case "min-items":
		if v.List == nil {
			v.List = &irc.List{}
		}
		v.List.MinItems = uint64(assertUNumber(ctx, kv.Value))
	case "unique-items":
		if v.List == nil {
			v.List = &irc.List{}
		}
		v.List.UniqueItems = assertBool(ctx, kv.Value)
	case "max-props":
		if v.Objectish == nil {
			v.Objectish = &irc.Object{}
		}
		v.Objectish.MaxProperties = uint64(assertUNumber(ctx, kv.Value))
	case "min-props":
		if v.Objectish == nil {
			v.Objectish = &irc.Object{}
		}
		v.Objectish.MinProperties = uint64(assertUNumber(ctx, kv.Value))
	default:
		trace.ErrorAt(ctx, "unknown validator")
	}
}

func setTypeFrom(ctx context.Context, s *ast.ResolvedTypeInfo, mod ast.Modifier, typ ast.ResolvedType) {
	ctx = trace.Describe(ctx, "type")
	if s.Type != nil {
		ctx = trace.Note(ctx, "other type", s.TypeSrc)
		trace.ErrorAt(ctx, "cannot have two different types in the same modifier list")
	}
	s.Type = typ
	s.TypeSrc = mod
}
func keyToPrimitive(key string) *ir.Primitive_Type {
	var res ir.Primitive_Type
	switch key {
	case "string":
		res = ir.Primitive_STRING
	case "int32":
		res = ir.Primitive_LEGACYINT32
	case "int64":
		res = ir.Primitive_INT64
	case "quantity":
		res = ir.Primitive_QUANTITY
	case "time":
		res = ir.Primitive_TIME
	case "duration":
		res = ir.Primitive_DURATION
	case "bytes":
		res = ir.Primitive_BYTES
	case "bool":
		res = ir.Primitive_BOOL
	case "dangerous-float64":
		res = ir.Primitive_LEGACYFLOAT64
	case "int-or-string":
		res = ir.Primitive_INTORSTRING
	default:
		return nil
	}
	return &res
}
func modToList(ctx context.Context, mod ast.KeyishModifier) *ir.List {
	if mod.Name.Name != "list" {
		return nil
	}

	params := validParameters(ctx, mod.Parameters, []string{"value"}, nil)
	res := ir.List{}
	switch val := params["value"].Value.(type) {
	case ast.PrimitiveTypeVal:
		primType := keyToPrimitive(val.Name)
		if primType == nil {
			trace.ErrorAt(ctx, "unknown primitive type")
			typ := ir.Primitive_STRING
			primType = &typ // make progress
		}
		res.Items = &ir.List_Primitive{Primitive: &ir.Primitive{
			Type: *primType,
		}}
	case ast.RefTypeVal:
		ref := refModToRef(ast.RefModifier(val))
		res.Items = &ir.List_Reference{Reference: &ref}
	default:
		trace.ErrorAt(ctx, "invalid value for list, expected primitive or reference")
	case nil:
		// do nothing, we already errored
	}
	return &res
}
func updateTypeInfo(ctx context.Context, info *ast.ResolvedTypeInfo, mod ast.Modifier) {
	ctx = trace.Describe(ctx, "modifier")
	ctx = trace.InSpan(ctx, mod)

	// TODO: clean this up by checking for duplicates all at once

	switch mod := mod.(type) {
	case ast.KeyishModifier:
		if primType := keyToPrimitive(mod.Name.Name); primType != nil {
			setTypeFrom(ctx, info, mod, ast.PrimitiveType(*primType))
			break
		}

		// key or key with param list, could be modifier, compound, or primitive
		switch mod.Name.Name {
		// primitives are handled above
		// compounds
		case "list":
			ctx = trace.Note(ctx, "name", "list")
			setTypeFrom(ctx, info, mod, ast.ListType(*modToList(ctx, mod)))
		case "list-map":
			ctx = trace.Note(ctx, "name", "list-map")
			// TODO: make an optional singular parameter too
			params := validParameters(ctx, mod.Parameters, []string{"value"}, []string{"keys"})
			res := ir.ListMap{}

			switch val := params["value"].Value.(type) {
			case ast.RefTypeVal:
				ref := refModToRef(ast.RefModifier(val))
				res.Items = &ref
			default:
				valCtx := trace.InSpan(trace.Describe(ctx, "value"), params["value"].Value)
				trace.ErrorAt(valCtx, "invalid value for list-map, expected reference")
			case nil:
				// do nothing, we already errored
			}

			if params["keys"] == nil {
				res.KeyField = append(res.KeyField, "name")
			} else {
				switch keys := params["keys"].Value.(type) {
				case nil:
					// default is just name
					res.KeyField = append(res.KeyField, "name")
				case ast.ListVal:
					keysCtx := trace.InSpan(trace.Describe(ctx, "value"), params["value"].Value)
					for _, keyRaw := range keys.Values {
						key, isPath := keyRaw.(ast.FieldPathVal)
						if !isPath {
							keyCtx := trace.InSpan(trace.Describe(keysCtx, "key"), keyRaw)
							trace.ErrorAt(keyCtx, "invalid key, expected a field path")
						}
						res.KeyField = append(res.KeyField, key.Name)
					}
				default:
					keysCtx := trace.InSpan(trace.Describe(ctx, "value"), params["value"].Value)
					trace.ErrorAt(keysCtx, "invalid keys for list, expected a list of field paths")
				}
			}
			setTypeFrom(ctx, info, mod, ast.ListMapType(res))
		case "set":
			ctx = trace.Note(ctx, "name", "set")
			params := validParameters(ctx, mod.Parameters, []string{"value"}, nil)
			res := ir.Set{}
			switch val := params["value"].Value.(type) {
			case ast.PrimitiveTypeVal:
				primType := keyToPrimitive(val.Name)
				if primType == nil {
					trace.ErrorAt(ctx, "unknown primitive type")
					typ := ir.Primitive_STRING
					primType = &typ // make progress
				}
				res.Items = &ir.Set_Primitive{Primitive: &ir.Primitive{
					Type: *primType,
				}}
			case ast.RefTypeVal:
				ref := refModToRef(ast.RefModifier(val))
				res.Items = &ir.Set_Reference{Reference: &ref}
			default:
				trace.ErrorAt(ctx, "invalid value for set, expected primitive or reference")
			case nil:
				// do nothing, we already errored
			}
			setTypeFrom(ctx, info, mod, ast.SetType(res))
		case "simple-map":
			ctx = trace.Note(ctx, "name", "simple-map")
			params := validParameters(ctx, mod.Parameters, []string{"value"}, []string{"key"})
			res := ir.PrimitiveMap{}

			switch val := params["value"].Value.(type) {
			case ast.PrimitiveTypeVal:
				primType := keyToPrimitive(val.Name)
				if primType == nil {
					trace.ErrorAt(ctx, "unknown primitive type")
					typ := ir.Primitive_STRING
					primType = &typ // make progress
				}
				res.Value = &ir.PrimitiveMap_PrimitiveValue{PrimitiveValue: &ir.Primitive{
					Type: *primType,
				}}
			case ast.RefTypeVal:
				ref := refModToRef(ast.RefModifier(val))
				res.Value = &ir.PrimitiveMap_ReferenceValue{ReferenceValue: &ref}
			case ast.CompoundTypeVal:
				valCtx := trace.InSpan(trace.Describe(ctx, "value"), params["value"].Value)
				listVal := modToList(valCtx, ast.KeyishModifier(val))
				if listVal == nil {
					trace.ErrorAt(valCtx, "invalid value for simple-map, expected primitive, reference, or (primitive-y) list")
				}
				res.Value = &ir.PrimitiveMap_SimpleListValue{SimpleListValue: listVal}
			default:
				valCtx := trace.InSpan(trace.Describe(ctx, "value"), params["value"].Value)
				trace.ErrorAt(valCtx, "invalid value for simple-map, expected primitive, reference, or (primitive-y) list")
			case nil:
				// do nothing, we already errored
			}

			if params["key"] == nil {
				res.Key = &ir.PrimitiveMap_PrimitiveKey{PrimitiveKey: &ir.Primitive{
					Type: ir.Primitive_STRING,
				}}
			} else {
				switch key := params["key"].Value.(type) {
				case ast.PrimitiveTypeVal:
					primType := keyToPrimitive(key.Name)
					if primType == nil {
						trace.ErrorAt(ctx, "unknown primitive type")
						typ := ir.Primitive_STRING
						primType = &typ // make progress
					}
					res.Key = &ir.PrimitiveMap_PrimitiveKey{PrimitiveKey: &ir.Primitive{
						Type: *primType,
					}}
				case ast.RefTypeVal:
					ref := refModToRef(ast.RefModifier(key))
					res.Key = &ir.PrimitiveMap_ReferenceKey{ReferenceKey: &ref}
				default:
					keyCtx := trace.InSpan(trace.Describe(ctx, "key"), params["key"].Value)
					trace.ErrorAt(keyCtx, "invalid key for simple-map, expected primitive or reference to one")
				case nil:
					// do nothing, we already errored
				}
			}
			setTypeFrom(ctx, info, mod, ast.PrimitiveMapType(res))

		// misc modifiers
		case "optional":
			ctx = trace.Note(ctx, "name", "optional")
			if info.Optional == true {
				ctx = trace.Note(ctx, "other optional", info.OptionalSrc)
				trace.ErrorAt(ctx, "cannot set optional twice in the same modifier list")
			}
			info.Optional = true
			info.OptionalSrc = &mod

			params := validParameters(ctx, mod.Parameters, nil, []string{"default"})
			if def := params["default"]; def != nil {
				info.Default = def.Value
			}
		case "create-only":
			ctx = trace.Note(ctx, "name", "create-only")
			if info.CreateOnly == true {
				ctx = trace.Note(ctx, "other create-only", info.CreateOnlySrc)
				trace.ErrorAt(ctx, "cannot set create-only twice in the same modifier list")
			}
			info.CreateOnly = true
			info.CreateOnlySrc = &mod
		case "preserves-unknown-fields":
			// TODO: do we really want this here?  Is it a property of fields?
			panic("TODO")
		case "embedded-kind":
			// TODO: do we really want this here?  Is it a property of fields?
			panic("TODO")
		case "validates":
			ctx = trace.Note(ctx, "name", "validates")
			if info.Validates != nil {
				ctx = trace.Note(ctx, "other validates", info.ValidatesSrc)
				trace.ErrorAt(ctx, "cannot set validates twice in the same modifier list")
			}
			info.Validates = &ast.ValidatesInfo{}
			if mod.Parameters != nil {
				for _, param := range mod.Parameters.Params {
					updateValidates(ctx, info.Validates, param)
				}
			}
		default:
			ctx := trace.Note(ctx, "modifier", mod.Name.Name)
			trace.ErrorAt(ctx, "unknown type modifier")
		}
	case ast.RefModifier: // reference
		ref := refModToRef(mod)
		setTypeFrom(ctx, info, mod, ast.RefType(ref))
	default:
		panic(fmt.Sprintf("unreachable: unknown modifier type %T", mod))
	}
}

func refModToRef(mod ast.RefModifier) ir.Reference {
	ref := ir.Reference{
		Name: mod.Name.Name,
	}
	if gv := mod.GroupVersion; gv != nil {
		ref.GroupVersion.Group = gv.Group
		ref.GroupVersion.Version = gv.Version
	}
	return ref
}

func validParameters(ctx context.Context, params *ast.ParameterList, req []string, opt []string) map[string]*ast.KeyValue {
	ctx = trace.Describe(ctx, "parameters")
	if params != nil {
		ctx = trace.InSpan(ctx, params)
	}
	ctx = trace.Note(ctx, "required", req)
	ctx = trace.Note(ctx, "optional", opt)
	present := make(map[string]*ast.KeyValue, len(req))
	for _, name := range req {
		present[name] = nil
	}
	for _, name := range opt {
		present[name] = nil
	}
	if params != nil {
		for i, param := range params.Params {
			name := param.Key.Name
			paramCtx := trace.Describe(ctx, "parameter")
			paramCtx = trace.Note(paramCtx, "name", name)
			paramCtx = trace.InSpan(paramCtx, param)

			if other, known := present[name]; known {
				if other != nil {
					errCtx := trace.Note(paramCtx, "other param", other)
					trace.ErrorAt(errCtx, "cannot set the same parameter twice")
				}
				present[name] = &params.Params[i]
				continue
			}

			trace.ErrorAt(paramCtx, "unknown parameter")
		}
	}

	for _, name := range req {
		if present[name] == nil {
			errCtx := trace.Note(ctx, "missing", name)
			trace.ErrorAt(errCtx, "missing required parameter")
			// backfill so we don't panic out later on
			present[name] = &ast.KeyValue{}
		}
	}

	return present
}

func modifiersToKnown(ctx context.Context, mods ast.ModifierList) ast.ResolvedTypeInfo {
	ctx = trace.Describe(ctx, "type modifiers (to type)")
	ctx = trace.InSpan(ctx, mods)

	spec := ast.ResolvedTypeInfo{}
	for _, mod := range mods {
		updateTypeInfo(ctx, &spec, mod)
	}

	return spec
}

func assertNumber(ctx context.Context, val ast.Value) int {
	num, isNum := val.(ast.NumVal)
	if !isNum {
		ctx = trace.InSpan(ctx, val)
		trace.ErrorAt(ctx, "expected number")
	}
	return num.Value
}
func assertUNumber(ctx context.Context, val ast.Value) uint {
	num, isNum := val.(ast.NumVal)
	if !isNum {
		ctx = trace.InSpan(ctx, val)
		trace.ErrorAt(ctx, "expected number >= 0")
	}
	if num.Value < 0 {
		ctx = trace.InSpan(ctx, val)
		trace.ErrorAt(ctx, "expected number >= 0")
	}
	return uint(num.Value)
}
func assertBool(ctx context.Context, val ast.Value) bool {
	boolean, isBoolean := val.(ast.BoolVal)
	if !isBoolean {
		ctx = trace.InSpan(ctx, val)
		trace.ErrorAt(ctx, "expected boolean")
	}
	return boolean.Value
}
func assertString(ctx context.Context, val ast.Value) string {
	str, isStr := val.(ast.StringVal)
	if !isStr {
		ctx = trace.InSpan(ctx, val)
		trace.ErrorAt(ctx, "expected string")
	}
	return str.Value
}

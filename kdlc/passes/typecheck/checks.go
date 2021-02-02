// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package typecheck

import (
	"context"

	irt "k8s.io/idl/ckdl-ir/goir/types"
	"k8s.io/idl/kdlc/parser/trace"
)

// NB(directxman12): converting to IR handles the "easy" cases of checking
// validation (literally do we have the right fields), these handle cases
// that require graph info, like references

// TODO: any marker checking whatsoever

func CheckFieldValidation(ctx context.Context, g Graphish, field *irt.Field, source interface{}) {
	panic("TODO")
}

func CheckSubtypeValidation(ctx context.Context, g Graphish, subtype *irt.Subtype) {
	panic("TODO")
}

func CheckFieldDefault(ctx context.Context, g Graphish, field *irt.Field, source interface{}) {
	panic("TODO")
}

func CheckFieldType(ctx context.Context, g Graphish, field *irt.Field, source interface{}) {
	switch typ := field.Type.(type) {
	case *irt.Field_ListMap:
		checkListMap(ctx, g, typ.ListMap)
	// TODO: primitivemap keys eventually strings, values are primitive or primitive lists
	// TODO: union variants are valid, etc
	}
}

func checkListMap(ctx context.Context, g Graphish, listMap *irt.ListMap) {
	// check that:
	// - keys exists as fields in the item
	// - items are structs

	item := g.TerminalFor(ctx, NameFromRef(listMap.Items))
	switch item := item.(type) {
	case TerminalStruct:
		strct := item.Struct
KeyLoop:
		for _, key := range listMap.KeyField {
			ctx := trace.Describe(ctx, "key")
			ctx = trace.Note(ctx, "name", key)
			for _, field := range strct.Fields {
				if field.Name == key {
					// TODO: check that type is scalar:
					// ( https://github.com/kubernetes-sigs/structured-merge-diff/issues/115#issuecomment-544759657 )
					continue KeyLoop
				}
			}
			trace.ErrorAt(ctx, "key of list-map not present in item")
		}
	default:
		trace.ErrorAt(ctx, "list-map items must be structs")
	}
}

func CheckWrapperType(ctx context.Context, g Graphish, subtype *irt.Subtype) {
	switch typ := subtype.Type.(type) {
	case *irt.Subtype_ListMap:
		checkListMap(ctx, g, typ.ListMap)
	// TODO: primitivemap keys eventually strings, values are primitive or primitive lists
	// TODO: union variants are valid, etc
	}
}

func CheckReferences(ctx context.Context, g Graphish, ref *irt.Reference) {
	// TerminalFor will log an error if there's a missing terminal
	g.TerminalFor(ctx, NameFromRef(ref))
}

// TODO: start from roots, only check things that matter?

func CheckAll(ctx context.Context, g *Graph) {
	g.MergeNodes(ctx)
	if trace.HadError(ctx) {
		// can't proceed without merged nodes
		return
	}
	g.CheckReferences(ctx, CheckReferences)
	g.CheckFields(ctx, CheckFieldType)
	g.CheckSubtypes(ctx, CheckWrapperType)
	// TODO: rest aren't implemented
}

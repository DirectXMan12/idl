// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package fromast

import (
	"context"
	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser/ast"
)

type MarkerVisitor interface {
	VisitMarker(context.Context, *ast.AbstractMarker)
}

func VisitMarkers(ctx context.Context, v MarkerVisitor, gv *ast.GroupVersion) {
	gvCtx := trace.Describe(ctx, "group-version")
	gvCtx = trace.Note(gvCtx, "group", gv.Group)
	gvCtx = trace.Note(gvCtx, "version", gv.Group)
	gvCtx = trace.InSpan(gvCtx, gv)

	for _, marker := range gv.Markers {
		v.VisitMarker(gvCtx, &marker)
	}

	VisitGroupVersion(ctx, markerVisitor{v}, gv)
}

type markerVisitor struct {
	MarkerVisitor
}
func (v markerVisitor) EnterSubtype(ctx context.Context, subtype *ast.SubtypeDecl) (context.Context, TypeVisitor) {
	for i := range subtype.Markers {
		v.VisitMarker(ctx, &subtype.Markers[i])
	}

	switch body := subtype.Body.(type) {
	case *ast.Struct:
		for _, field := range body.Fields {
			ctx := trace.Describe(ctx, "field")
			ctx = trace.Note(ctx, "name", field.Name)

			for i := range field.Markers {
				v.VisitMarker(ctx, &field.Markers[i])
			}
		}
	case *ast.Union:
		for _, field := range body.Variants {
			ctx := trace.Describe(ctx, "variant")
			ctx = trace.Note(ctx, "name", field.Name)

			for i := range field.Markers {
				v.VisitMarker(ctx, &field.Markers[i])
			}
		}
	case *ast.Enum:
		for _, field := range body.Variants {
			ctx := trace.Describe(ctx, "variant")
			ctx = trace.Note(ctx, "name", field.Name)

			for i := range field.Markers {
				v.VisitMarker(ctx, &field.Markers[i])
			}
		}
	}

	return ctx, v
}
func (v markerVisitor) EnterKind(ctx context.Context, kind *ast.KindDecl) (context.Context, TypeVisitor) {
	for i := range kind.Markers {
		v.VisitMarker(ctx, &kind.Markers[i])
	}

	for _, field := range kind.Fields {
		ctx := trace.Describe(ctx, "field")
		ctx = trace.Note(ctx, "name", field.Name)

		for i := range field.Markers {
			v.VisitMarker(ctx, &field.Markers[i])
		}
	}

	return ctx, v
}

type ComplexTypeVisitor interface {
	BeginSubtype(context.Context, *ast.SubtypeDecl)
	BeginKind(context.Context, *ast.KindDecl)

	EndSubtype(context.Context, *ast.SubtypeDecl)
	EndKind(context.Context, *ast.KindDecl)
}

type TypeVisitor interface {
	EnterSubtype(context.Context, *ast.SubtypeDecl) (context.Context, TypeVisitor)
	EnterKind(context.Context, *ast.KindDecl) (context.Context, TypeVisitor)
}

func VisitGroupVersion(ctx context.Context, v TypeVisitor, gv *ast.GroupVersion) {
	ctx = trace.Describe(ctx, "group-version")
	ctx = trace.Note(ctx, "group", gv.Group)
	ctx = trace.Note(ctx, "version", gv.Group)
	ctx = trace.InSpan(ctx, gv)

	complexV, isComplex := v.(ComplexTypeVisitor)

	if isComplex {
		for _, decl := range gv.Decls {
			switch decl := decl.(type) {
			case *ast.KindDecl:
				declCtx := trace.Describe(ctx, "kind")
				declCtx = trace.InSpan(declCtx, decl)
				declCtx = trace.Note(declCtx, "name", decl.Name.Name)

				complexV.BeginKind(declCtx, decl)

			case *ast.SubtypeDecl:
				declCtx := trace.Describe(ctx, "subtype")
				declCtx = trace.InSpan(declCtx, decl)
				declCtx = trace.Note(declCtx, "name", decl.Name.Name)

				complexV.BeginSubtype(declCtx, decl)
			default:
				panic("unreachable: unknown declaration type")
			}
		}
	}

	for _, decl := range gv.Decls {
		switch decl := decl.(type) {
		case *ast.KindDecl:
			declCtx := trace.Describe(ctx, "kind")
			declCtx = trace.InSpan(declCtx, decl)
			declCtx = trace.Note(declCtx, "name", decl.Name.Name)

			subCtx, newVisitor := v.EnterKind(declCtx, decl)

			visitSubtypes(subCtx, newVisitor, decl.Subtypes)

			if complexNV, isComplex := newVisitor.(ComplexTypeVisitor); isComplex {
				complexNV.EndKind(declCtx, decl)
			}
		case *ast.SubtypeDecl:
			enterSubtype(ctx, v, decl)
		default:
			panic("unreachable: unknown declaration type")
		}
	}
}

func visitSubtypes(ctx context.Context, v TypeVisitor, subtypes []ast.SubtypeDecl) {
	complexV, isComplex := v.(ComplexTypeVisitor)
	if isComplex {
		for i := range subtypes {
			sub := &subtypes[i]
			declCtx := trace.Describe(ctx, "subtype")
			declCtx = trace.InSpan(declCtx, sub)
			declCtx = trace.Note(declCtx, "name", sub.Name.Name)

			complexV.BeginSubtype(declCtx, sub)
		}
	}
	for i := range subtypes {
		enterSubtype(ctx, v, &subtypes[i])
	}
}

func enterSubtype(ctx context.Context, v TypeVisitor, st *ast.SubtypeDecl) {
	ctx = trace.Describe(ctx, "subtype")
	ctx = trace.InSpan(ctx, st)
	ctx = trace.Note(ctx, "name", st.Name.Name)

	subCtx, newVisitor := v.EnterSubtype(ctx, st)
	if newVisitor == nil {
		return
	}
	switch body := st.Body.(type) {
	case *ast.Struct:
		visitSubtypes(subCtx, newVisitor, body.Subtypes)
	case *ast.Union:
		visitSubtypes(subCtx, newVisitor, body.Subtypes)
	case *ast.Enum:
	case *ast.Newtype:
	default:
		panic("unreachable: unknown declaration type")
	}
	if complexNV, isComplex := newVisitor.(ComplexTypeVisitor); isComplex {
		complexNV.EndSubtype(ctx, st)
	}
}

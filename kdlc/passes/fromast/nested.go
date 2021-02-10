// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package fromast

import (
	"context"
	"strings"

	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser/ast"
	ir "k8s.io/idl/ckdl-ir/goir/types"
)

type identCtx struct {
	groupVersion *ast.GroupVersionRef
	stack *identStack
}
type identStack struct {
	ident string
	parent *identStack
	inScope map[string]*ast.Identish
}

func (c *identCtx) BeginSubtype(ctx context.Context, st *ast.SubtypeDecl) {
	st.ResolvedName = &ast.ResolvedNameInfo{
		GroupVersion: *c.groupVersion,
	}
	if prefix := c.stack.fullName(); prefix != "" {
		st.ResolvedName.FullName = prefix + "::" + st.Name.Name
	} else {
		st.ResolvedName.FullName = st.Name.Name
	}

	c.stack.inScope[st.Name.Name] = &st.Name
}
func (c *identCtx) BeginKind(ctx context.Context, kind *ast.KindDecl) {
	kind.ResolvedName = &ast.ResolvedNameInfo{
		GroupVersion: *c.groupVersion,
	}
	kind.ResolvedName.FullName = c.stack.fullNameFor(kind.Name.Name)
	c.stack.inScope[kind.Name.Name] = &kind.Name
}
func (c *identCtx) EndSubtype(ctx context.Context, st *ast.SubtypeDecl) {
	switch body := st.Body.(type) {
	case *ast.Struct:
		c.resolveFields(ctx, body.Fields)
	case *ast.Union:
		c.resolveFields(ctx, body.Variants)
	case *ast.Newtype:
		body.ResolvedType = c.resolveModifiers(ctx, body.Modifiers)
	// TODO: enumerate these and panic on default to make this more resilent to change
	// don't care about types without field-ish things or newtypes
	}
}
func (c *identCtx) EndKind(ctx context.Context, kind *ast.KindDecl) {
	c.resolveFields(ctx, kind.Fields)
}

func (c *identStack) knownNames() []string {
	res := make([]string, 0, len(c.inScope))
	for name := range c.inScope {
		res = append(res, name)
	}
	return res
}

func (c *identCtx) EnterSubtype(ctx context.Context, st *ast.SubtypeDecl) (context.Context, TypeVisitor) {
	return ctx, &identCtx{
		groupVersion: c.groupVersion,
		stack: &identStack{
			parent: c.stack,
			ident: st.Name.Name,
			inScope: make(map[string]*ast.Identish),
		},
	}
}

// TODO: did-you-mean-style help

func (c *identCtx) EnterKind(ctx context.Context, kind *ast.KindDecl) (context.Context, TypeVisitor) {
	return ctx, &identCtx{
		groupVersion: c.groupVersion,
		stack: &identStack{
			parent: c.stack,
			ident: kind.Name.Name,
			inScope: make(map[string]*ast.Identish),
		},
	}
}

func (c *identStack) fullName() string {
	if c.parent == nil || c.parent.ident == "" {
		return c.ident
	}
	return c.parent.fullName() + "::" + c.ident
}
func (c *identStack) fullNameFor(name string) string {
	prefix := c.fullName()
	if prefix == "" {
		return name
	}
	return prefix + "::" + name
}

func (c *identStack) resolveName(ctx context.Context, name string) string {
	if strings.Contains(name, "::") {
		// already full qualified
		return name
	}
	for currentStack := c; currentStack != nil; currentStack = currentStack.parent {
		_, found := currentStack.inScope[name]
		if found {
			return currentStack.fullNameFor(name)
		}
	}
	ctx = trace.Note(ctx, "identifier", name)
	trace.ErrorAt(ctx, "unresolvable identifier")
	return name
}

func (c *identCtx) resolveName(ctx context.Context, name string) string {
	return c.stack.resolveName(ctx, name)
}

func (c *identCtx) resolveRef(ctx context.Context, ref *ir.Reference) {
	if ref.GroupVersion != nil {
		return
	}
	ref.GroupVersion = &ir.GroupVersionRef{
		Group: c.groupVersion.Group,
		Version: c.groupVersion.Version,
	}
	ref.Name = c.resolveName(ctx, ref.Name)
}

func (c *identCtx) resolveModifiers(ctx context.Context, modifiers ast.ModifierList) *ast.ResolvedTypeInfo {
	// convert to concrete typedata first so that we can know what's a type vs
	// a value that happens to be a enum variant name or whatever (this is an
	// ambiguity that we could maybe resolve, but it's easy enough to do this instead)
	typeData := modifiersToKnown(ctx, modifiers)
	ctx = trace.InSpan(trace.Describe(ctx, "type modifier"), typeData.TypeSrc)
	// TODO: maybe more detailed span info for nested stuff?
	switch typ := typeData.Type.(type) {
	case ast.RefType:
		ref := ir.Reference(typ)
		c.resolveRef(ctx, &ref)
		typeData.Type = ast.RefType(ref) // re-assing b/c we made a copy
	case ast.ListType:
		ref, isRef := typ.Items.(*ir.List_Reference)
		if !isRef {
			break
		}
		c.resolveRef(ctx, ref.Reference)
	case ast.SetType:
		ref, isRef := typ.Items.(*ir.Set_Reference)
		if !isRef {
			break
		}
		c.resolveRef(ctx, ref.Reference)
	case ast.ListMapType:
		c.resolveRef(ctx, typ.Items)
	case ast.PrimitiveMapType:
		keyRef, isRef := typ.Key.(*ir.PrimitiveMap_ReferenceKey)
		if isRef {
			c.resolveRef(ctx, keyRef.ReferenceKey)
		}

		switch val := typ.Value.(type) {
		case *ir.PrimitiveMap_ReferenceValue:
			c.resolveRef(ctx, val.ReferenceValue)
		case *ir.PrimitiveMap_SimpleListValue:
			ref, isRef := val.SimpleListValue.Items.(*ir.List_Reference)
			if !isRef {
				break
			}
			c.resolveRef(ctx, ref.Reference)
		}
	// don't care about primitives
	}
	return &typeData
}

func (c *identCtx) resolveField(ctx context.Context, field *ast.Field) {
	ctx = trace.Describe(ctx, "field")
	ctx = trace.Note(ctx, "name", field.Name.Name)
	ctx = trace.InSpan(ctx, field)

	field.ResolvedType = c.resolveModifiers(ctx, field.Modifiers)
}

func (c *identCtx) resolveFields(ctx context.Context, fields []ast.Field) {
	for i := range fields {
		field := &fields[i]

		c.resolveField(ctx, field)
	}
}

// ResolveNested figures out the fully-qualified name for types, and resolves
// unqualified references to those into qualified references.
// It's the first pass that should be run.
func ResolveNested(ctx context.Context, file *ast.File) {
	for i := range file.GroupVersions {
		gv := &file.GroupVersions[i]
		VisitGroupVersion(ctx, &identCtx{
			groupVersion: &ast.GroupVersionRef{Group: gv.Group, Version: gv.Version},
			stack: &identStack{
				inScope: make(map[string]*ast.Identish),
			},
		}, gv)
	}
}

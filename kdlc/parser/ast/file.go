// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package ast

import (
	"context"

	"k8s.io/idl/kdlc/lexer"
	"k8s.io/idl/kdlc/parser/trace"
)

func TokenSpan(tok lexer.Token) trace.Span {
	at := trace.TokenPosition{Start: tok.Start, End: tok.End}
	return trace.Span{Start: at, End: at}
}

type File struct {
	Imports *Imports
	GroupVersions []GroupVersion
}

type GroupVersionRef struct {
	Group, Version string
}

type Imports struct {
	// Types maps group-version imports to source files
	Types *TypeImports

	// Markers maps marker key prefix to source files
	Markers *MarkerImports

	trace.Span
}

type MarkerImports struct {
	Imports map[string]MarkerImport

	trace.Span
}

type TypeImports struct {
	Imports map[GroupVersionRef]TypeImport

	trace.Span
}

// TODO: markerimport, typeimport, and group-version need spans on their values
type MarkerImport struct {
	Alias string
	Src string

	trace.Span
}

type TypeImport struct {
	GroupVersion GroupVersionRef
	Src string

	trace.Span
}

type Docs struct {
	Sections []DocSection
	trace.Span
}

type DocSection struct {
	Title string
	Lines []string

	trace.Span
}

type GroupVersion struct {
	Group, Version string

	Markers []AbstractMarker
	Docs Docs
	Decls []Decl

	trace.Span
}

type Value interface{
	trace.Spannable
}
type StringVal struct {
	Value string
	trace.Span
}
type NumVal struct {
	Value int
	trace.Span
}
type BoolVal struct {
	Value bool
	trace.Span
}
type ListVal struct {
	Values []Value
	trace.Span
}
type StructVal struct {
	KeyValues []KeyValue
	trace.Span
}
type FieldPathVal Identish
type RefTypeVal RefModifier
type PrimitiveTypeVal Identish
type CompoundTypeVal KeyishModifier


type Decl interface{}
type SubtypeBody interface{
	trace.Spannable
}

type AbstractMarker struct {
	Name Identish

	Parameters *ParameterList

	trace.Span
}

type ParameterList struct {
	Params []KeyValue

	trace.Span
}

type KeyValue struct {
	Key Identish
	Value Value

	trace.Span
}

type KindDecl struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish

	Fields []Field
	Subtypes []SubtypeDecl

	// TODO: nonpersisted

	ResolvedName *ResolvedNameInfo

	trace.Span
}

type Field struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish
	Modifiers ModifierList
	ResolvedType *ResolvedTypeInfo
	Embedded bool

	// TODO: inline

	trace.Span
}

type ModifierList []Modifier
func (m ModifierList) SpanStart() trace.TokenPosition {
	if len(m) > 0 {
		return m[0].SpanStart()
	}
	return trace.TokenPosition{}
}
func (m ModifierList) SpanEnd() trace.TokenPosition {
	if len(m) > 0 {
		return m[len(m)-1].SpanEnd()
	}
	return trace.TokenPosition{}
}

type Modifier interface{
	trace.Spannable
}

type KeyishModifier struct {
	Name Identish
	Parameters *ParameterList

	trace.Span
}
type RefModifier struct {
	GroupVersion *GroupVersionRef
	Name Identish

	trace.Span
}

type Identish struct {
	Name string
	trace.Span
}
func IdentFrom(name string, tok lexer.Token) Identish {
	return Identish{
		Name: name,
		Span: TokenSpan(tok),
	}
}

type EnumVariant struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish

	trace.Span
}

type SubtypeDecl struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish

	Body SubtypeBody

	ResolvedName *ResolvedNameInfo

	trace.Span
}

type Newtype struct {
	Modifiers ModifierList
	ResolvedType *ResolvedTypeInfo

	trace.Span
}

type Struct struct {
	Fields []Field
	Subtypes []SubtypeDecl

	trace.Span
}
type Union struct {
	Variants []Field
	Subtypes []SubtypeDecl

	// TODO: tagged vs untagged
	Tag string
	Untagged bool

	trace.Span
}
type Enum struct {
	Variants []EnumVariant

	trace.Span
}
type Validation struct {
	trace.Span
}

func In(ctx context.Context, node trace.Spannable) context.Context {
	// TODO
	return trace.InSpan(ctx, node)
}

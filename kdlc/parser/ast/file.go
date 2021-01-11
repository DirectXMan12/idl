package ast

import (
	"k8s.io/idl/kdlc/lexer"
)

type Span struct {
	Start, End lexer.Position
}
func (s Span) SpanStart() lexer.Position {
	return s.Start
}
func (s Span) SpanEnd() lexer.Position {
	return s.End
}
func TokenSpan(tok lexer.Token) Span {
	return Span{Start: tok.Start, End: tok.End}
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

	Span
}

type MarkerImports struct {
	Imports map[string]MarkerImport

	Span
}

type TypeImports struct {
	Imports map[GroupVersionRef]TypeImport

	Span
}

// TODO: markerimport, typeimport, and group-version need spans on their values
type MarkerImport struct {
	Alias string
	Src string

	Span
}

type TypeImport struct {
	GroupVersion GroupVersionRef
	Src string

	Span
}

type Docs struct {
	Sections []DocSection
	Span
}

type DocSection struct {
	Title string
	Lines []string

	Span
}

type GroupVersion struct {
	Group, Version string

	Markers []AbstractMarker
	Docs Docs
	Decls []Decl

	Span
}

type Value interface{
	Spannable
}
type StringVal struct {
	Value string
	Span
}
type NumVal struct {
	Value int
	Span
}
type BoolVal struct {
	Value bool
	Span
}
type ListVal struct {
	Values []Value
	Span
}
type StructVal struct {
	KeyValues []KeyValue
	Span
}
type FieldPathVal Identish
type RefTypeVal RefModifier
type PrimitiveTypeVal Identish


type Decl interface{}
type SubtypeBody interface{
	Spannable
}
type Spannable interface {
	SpanStart() lexer.Position
	SpanEnd() lexer.Position
}
type AbstractMarker struct {
	Name Identish

	Parameters *ParameterList

	Span
}

type ParameterList struct {
	Params []KeyValue

	Span
}

type KeyValue struct {
	Key Identish
	Value Value

	Span
}

type KindDecl struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish
	NameSpan Span

	Fields []Field
	Subtypes []SubtypeDecl

	Span
}

type Field struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish
	Modifiers ModifierList

	Span
}

type ModifierList []Modifier
func (m ModifierList) SpanStart() lexer.Position {
	if len(m) > 0 {
		return m[0].SpanStart()
	}
	return lexer.Position{}
}
func (m ModifierList) SpanEnd() lexer.Position {
	if len(m) > 0 {
		return m[len(m)-1].SpanEnd()
	}
	return lexer.Position{}
}

type Modifier interface{
	Spannable
}

type KeyishModifier struct {
	Name Identish
	Parameters *ParameterList

	Span
}
type RefModifier struct {
	GroupVersion *GroupVersionRef
	Name Identish

	Span
}

type Identish struct {
	Name string
	Span
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

	Span
}

type SubtypeDecl struct {
	Docs Docs
	Markers []AbstractMarker

	Name Identish

	Body SubtypeBody

	Span
}

type Newtype struct {
	Modifiers ModifierList

	Span
}

type Struct struct {
	Fields []Field
	Subtypes []SubtypeDecl

	Span
}
type Union struct {
	Variants []Field
	Subtypes []SubtypeDecl

	Span
}
type Enum struct {
	Variants []EnumVariant

	Span
}
type Validation struct {
	Span
}

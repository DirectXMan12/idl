// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package ast

import (
	"google.golang.org/protobuf/proto"

	ir "k8s.io/idl/ckdl-ir/goir/types"
	irc "k8s.io/idl/ckdl-ir/goir/constraints"
)

// TODO(directxman12): we could make all of this easier to expand & cleaner with reflection,
// or perhaps a global map of keys to setters

type ResolvedType interface{ isModType() }
// primitives: string, int32, int64, quantity, time, duration, bytes, bool, int-or-string
type PrimitiveType ir.Primitive_Type
func (PrimitiveType) isModType() {}
// TypeIdent, Type::Path, group/v123::Path
type RefType ir.Reference
func (RefType) isModType() {}
// list(value: type)
type ListType ir.List
func (ListType) isModType() {}
// set(value: type)
type SetType ir.Set
func (SetType) isModType() {}
// list-map(key: [.fieldPath], value: type)
type ListMapType ir.ListMap
func (ListMapType) isModType() {}
// simple-map(key: type, value: type)
type PrimitiveMapType ir.PrimitiveMap
func (PrimitiveMapType) isModType() {}

// validates(...)
type ValidatesInfo struct {
	// have these be sub-pointers to make it easier to match
	// them to particular types later, and more easily check
	// if invalid modifiers were set

	Number *irc.Numeric
	String *irc.String
	List *irc.List
	Objectish *irc.Object
}

type ValidationType int
const (
	NoValidation ValidationType = iota
	NumberValidation
	StringValidation
	ListValidation
	ObjectishValidation
)

type ResolvedTypeInfo struct {
	// optional
	// optional(default: value)
	Optional bool
	Default Value
	OptionalSrc *KeyishModifier

	// create-only
	CreateOnly bool
	CreateOnlySrc *KeyishModifier

	Type ResolvedType
	TypeSrc Modifier

	Validates *ValidatesInfo
	ValidatesSrc *KeyishModifier
}

type ResolvedNameInfo struct {
	GroupVersion GroupVersionRef
	FullName string
}

type ResolvedMarker struct {
	Message proto.Message
}

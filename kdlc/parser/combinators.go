// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package parser

// helper combinators to make control flow easier

type combi interface {
	Parse(*Parser) interface{}
}

type Choice []ChoiceOption
type ChoiceOption struct {
	When rune
	Do combi
}

type Then []combi
type Expect rune

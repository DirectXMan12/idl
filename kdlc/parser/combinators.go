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

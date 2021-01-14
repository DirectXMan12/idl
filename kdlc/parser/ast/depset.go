package ast

type DepSet struct {
	Main File
	Deps map[GroupVersionRef]*DepSet
}

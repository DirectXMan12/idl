// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package passes

import (
	"k8s.io/idl/kdlc/parser/ast"
	ir "k8s.io/idl/ckdl-ir/goir"
)

type DepSet struct {
	Main File
	Deps map[ast.GroupVersionRef]*DepSet
	Graph *typegraph
}

type File interface {
	isFile()
}

type RawFile struct {
	File *ast.File
}
func (RawFile) isFile() {}

type CompiledFile ir.Partial
func (CompiledFile) isFile() {}

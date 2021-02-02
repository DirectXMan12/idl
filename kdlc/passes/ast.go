// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package passes

import (
	"context"

	"k8s.io/idl/kdlc/passes/fromast"
	"k8s.io/idl/kdlc/passes/toir"
	"k8s.io/idl/kdlc/parser/ast"
	"k8s.io/idl/kdlc/parser/trace"
	ire "k8s.io/idl/ckdl-ir/goir"
)

type ASTPass func(ctx context.Context, file *ast.File)

var ASTPasses = []ASTPass{
	fromast.ResolveNested,
	// TODO: do we need to check validation?
}


// NB(directxman12): typechecking is done once the transformation to IR
// has happened -- this means we can have unified typechecking logic
// between pre-compiled cKDL and just-compiled KDL

func FileToIR(ctx context.Context, file *ast.File) ire.Partial {
	// AST Passes
	for _, pass := range ASTPasses {
		pass(ctx, file)
		if trace.HadError(ctx) {
			return ire.Partial{}
		}
	}

	return toir.File(ctx, file)
}

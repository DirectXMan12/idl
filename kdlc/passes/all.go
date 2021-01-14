// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package passes

import (
	"context"

	"k8s.io/idl/kdlc/parser/ast"
)

type Pass func(ctx context.Context, input *ast.DepSet)

var All = []Pass{
	Imports,
	ResolveNested,
	TypeCheck,
}

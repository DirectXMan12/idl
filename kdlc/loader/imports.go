// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package loader

import (
	"context"
	"bytes"

	ire "k8s.io/idl/ckdl-ir/goir"

	"k8s.io/idl/kdlc/passes/typecheck"
	"k8s.io/idl/kdlc/passes"
	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/parser"
	"k8s.io/idl/kdlc/lexer"
)

type Loader interface {
	FromCompiled(ctx context.Context, path string) (*ire.Partial, bool)
	FromSource(ctx context.Context, path string) []byte
}

type Outputs interface {
	PartialFor(ctx context.Context, path string) *ire.Partial
	BundleFor(ctx context.Context, paths ...string) *ire.Bundle
}

type Config struct {
	Imports Loader
	Roots []string

	Outputs Outputs
}
func (c *Config) Load(ctx context.Context) {
	l := &loader{
		Imports: c.Imports,
	}
	l.Graph = typecheck.NewGraph(l)

	// manually add the roots to get the ball rolling
	for _, rootPath := range c.Roots {
		root := l.MaybeLoad(ctx, rootPath)
		if root == nil {
			// possible if we've checked this indirectly by being
			// a dependency from an existing root
			continue
		}
		if trace.HadError(ctx) {
			continue
		}
		l.Graph.AddFile(ctx, rootPath, root)
	}

	// check the resulting graph
	typecheck.CheckAll(ctx, l.Graph)
	if trace.HadError(ctx) {
		// TODO: output what we can?
		return
	}

	// save the output 
	c.Outputs = l.Graph
}

type loader struct {
	Imports Loader
	Graph *typecheck.Graph
}

func (l *loader) MaybeLoad(ctx context.Context, path string) *ire.Partial {
	if l.Graph.Contains(ctx, path) {
		// avoid re-loading things that were dependencies & roots when
		// loading the roots.
		return nil
	}
	return l.Load(ctx, path)
}

// Load loads the given path, compiling it to IR if necessary.
// It implements typecheck.Requester
func (l *loader) Load(ctx context.Context, path string) *ire.Partial {
	ctx = trace.Describe(ctx, "import file")
	ctx = trace.Note(ctx, "path", path)

	// first, try loading from compiled, if the loader supports that
	// and wants to give it to us (e.g. caching is enabled, bundles
	// exist, etc)
	preCompiled, useCompiled := l.Imports.FromCompiled(ctx, path)
	if useCompiled {
		// success, just go straight to returning this,
		// or failure, but we were supposed to go from
		// this (i.e. a mapped CKDL file failed to load)
		if preCompiled == nil {
			preCompiled = &ire.Partial{}
		}
		return preCompiled
	}

	// no pre-compiled version available, try to load from source
	rawSource := l.Imports.FromSource(ctx, path)
	if rawSource == nil {
		// problems reading, it'll have logged an error, return
		// empty
		return &ire.Partial{}
	}

	lex := lexer.New(bytes.NewBuffer(rawSource))
	parse := parser.New(lex)
	ctx = trace.WithFullInput(ctx, string(rawSource)) // todo: put in lookaside instead?
	file := parse.Parse(ctx)
	if trace.HadError(ctx) {
		return &ire.Partial{}
	}
	res := passes.FileToIR(ctx, file)
	return &res
}

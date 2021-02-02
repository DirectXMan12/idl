// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package loader

import (
	"io/ioutil"
	"path/filepath"
	"os"
	"context"

	ire "k8s.io/idl/ckdl-ir/goir"
	"k8s.io/idl/kdlc/parser/trace"
)

type SourceLoader struct {
	Roots []string
}

func (l *SourceLoader) FromSource(ctx context.Context, path string) []byte {
	realPath := filepath.FromSlash(path)
	for _, root := range l.Roots {
		fullPath := filepath.Join(root, realPath)
		contents, err := ioutil.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			ctx = trace.Note(ctx, "actual path", fullPath)
			trace.ErrorAt(trace.Note(ctx, "error", err), "unable to read file")
			return nil
		}
		return contents
	}
	trace.ErrorAt(ctx, "no such KDL file found")
	return nil
}

type HybridLoader struct {
	Source SourceLoader
	Compiled *CompiledLoader
}
func (l *HybridLoader) FromCompiled(ctx context.Context, path string) (*ire.Partial, bool) {
	if l.Compiled == nil {
		return nil, false
	}
	return l.Compiled.FromCompiled(ctx, path)
}

func (l *HybridLoader) FromSource(ctx context.Context, path string) []byte {
	return l.Source.FromSource(ctx, path)
}

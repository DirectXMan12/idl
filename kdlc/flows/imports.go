// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package passes

import (
	"context"
	"path/filepath"
	"os"
	"io"
	"io/ioutil"
	"bytes"

	"k8s.io/idl/kdlc/parser/ast"
	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/lexer"
	"k8s.io/idl/kdlc/parser"
)

type ctxKey int
const (
	importerKey ctxKey = iota
)

type Importer interface {
	Load(ctx context.Context, path string) *DepSet
}
func WithImporter(ctx context.Context, imp Importer) context.Context {
	return context.WithValue(ctx, importerKey, imp)
}

type noImporter struct {}
func (noImporter) Load(ctx context.Context, path string) *DepSet {
	ctx = trace.Describe(ctx, "import file")
	ctx = trace.Note(ctx, "path", path)
	trace.ErrorAt(ctx, "no importer specified, cannot import any files")
	return &DepSet{}
}

type directoryImporter struct {
	searchDirs []string
}

func ImportFrom(dirs ...string) Importer {
	return &directoryImporter{
		searchDirs: dirs,
	}
}

func (d *directoryImporter) Load(ctx context.Context, path string) *DepSet {
	ctx = trace.Describe(ctx, "import from kdl")
	ctx = trace.Note(ctx, "path", path)

	// TODO: disallow .. in path (clean, then check if joined-abs is rel)

	ext := filepath.Ext(path)
	ctx = trace.Note(ctx, "format", ext)
	switch ext {
	case ".kdl":
	case ".ckdl":
		trace.ErrorAt(ctx, "cannot import directly from ckdl files -- import the KDL file then specify the cKDL file in the compiler runner")
		return &DepSet{}
	default:
		trace.ErrorAt(ctx, "unknown import format")
		return &DepSet{}
	}

	for _, dir := range d.searchDirs {
		full := filepath.Join(dir, path)
		file, err := os.Open(full)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			ctx := trace.Note(ctx, "full path", full)
			trace.ErrorAt(ctx, "unable to load file")
			return &DepSet{}
		}
		defer file.Close()
		return processFile(ctx, file)
	}
	trace.ErrorAt(ctx, "no such file found on search paths")
	return &DepSet{}
}

func processFile(ctx context.Context, file io.Reader) *DepSet {
	// TODO: do better than this?
	fullInput, err := ioutil.ReadAll(file)
	if err != nil {
		ctx := trace.Note(ctx, "error", err)
		trace.ErrorAt(ctx, "unable to read file")
		return &DepSet{}
	}

	lex := lexer.New(bytes.NewBuffer(fullInput))
	parse := parser.New(lex)
	ctx = trace.WithFullInput(ctx, string(fullInput))
	res := parse.Parse(ctx)

	depSet := Imports(ctx, res)
	if trace.HadError(ctx) {
		return depSet
	}
	ResolveNested(ctx, res)
	if trace.HadError(ctx) {
		return depSet
	}

	return set
}

func Imports(ctx context.Context, file *ast.File) *DepSet {
	importer, exists := ctx.Value(importerKey).(Importer)
	if !exists {
		importer = noImporter{}
	}

	// TODO: marker imports

	depSet := &DepSet{
		Main: RawFile{File: file},
	}
	if file.Imports == nil {
		return depSet
	}

	// TODO: is this the conflict resolution strategy that we really want?
	if file.Imports.Types != nil {
		// TODO: spans
		for _, info := range file.Imports.Types.Imports {
			file := importer.Load(ctx, info.Src)
			depSet.Deps[info.GroupVersion] = file
		}
	}
	return depSet
}

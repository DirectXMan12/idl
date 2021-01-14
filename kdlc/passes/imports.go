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
	Load(ctx context.Context, path string) *ast.DepSet
}
func WithImporter(ctx context.Context, imp Importer) context.Context {
	return context.WithValue(ctx, importerKey, imp)
}

type noImporter struct {}
func (noImporter) Load(ctx context.Context, path string) *ast.DepSet {
	ctx = trace.Describe(ctx, "import file")
	ctx = trace.Note(ctx, "path", path)
	trace.ErrorAt(ctx, "no importer specified, cannot import any files")
	return &ast.DepSet{}
}

type directoryImporter struct {
	searchDirs []string
}

func ImportFrom(dirs ...string) Importer {
	return &directoryImporter{
		searchDirs: dirs,
	}
}

func (d *directoryImporter) Load(ctx context.Context, path string) *ast.DepSet {
	ctx = trace.Describe(ctx, "import file")
	ctx = trace.Note(ctx, "path", path)

	// TODO: disallow .. in path (clean, then check if joined-abs is rel)

	ext := filepath.Ext(path)
	ctx = trace.Note(ctx, "format", ext)
	switch ext {
	case ".kdl":
	case ".ckdl":
		panic("TODO: back-convert ckdl files to resolved AST")
	default:
		trace.ErrorAt(ctx, "unknown import format")
		return &ast.DepSet{}
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
			return &ast.DepSet{}
		}
		defer file.Close()
		return processFile(ctx, file)
	}
	trace.ErrorAt(ctx, "no such file found on search paths")
	return &ast.DepSet{}
}

func processFile(ctx context.Context, file io.Reader) *ast.DepSet {
	// TODO: do better than this?
	fullInput, err := ioutil.ReadAll(file)
	if err != nil {
		ctx := trace.Note(ctx, "error", err)
		trace.ErrorAt(ctx, "unable to read file")
		return &ast.DepSet{}
	}

	lex := lexer.New(bytes.NewBuffer(fullInput))
	parse := parser.New(lex)
	ctx = trace.WithFullInput(ctx, string(fullInput))
	res := parse.Parse(ctx)
	set := &ast.DepSet{
		Main: *res,
	}

	for _, pass := range All {
		pass(ctx, set)
		// TODO: check for errors and stop
	}

	return set
}


func Imports(ctx context.Context, depSet *ast.DepSet) {
	importer, exists := ctx.Value(importerKey).(Importer)
	if !exists {
		importer = noImporter{}
	}

	// TODO: marker imports

	file := &depSet.Main
	if file.Imports == nil {
		return
	}

	// TODO: is this the conflict resolution strategy that we really want?
	if file.Imports.Types != nil {
		// TODO: spans
		for _, info := range file.Imports.Types.Imports {
			file := importer.Load(ctx, info.Src)
			depSet.Deps[info.GroupVersion] = file
		}
	}
}

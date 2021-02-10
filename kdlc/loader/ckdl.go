// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package loader

import (
	"io/ioutil"
	"fmt"
	"sync"
	"path/filepath"
	"os"
	"context"

	"github.com/golang/protobuf/proto"

	ire "k8s.io/idl/ckdl-ir/goir"
	"k8s.io/idl/kdlc/parser/trace"
)

// TODO: convert this to use context errors

// CompiledLoader is the Loader used by the backend runner.
// It knows how to assemble things from bundles & partial files
// on disk.
//
// Actual backends should use the BackendLoader instead, as they should
// be being fed by the runner.
type CompiledLoader struct {
	// BundlePaths specify cKDL bundles to load
	BundlePaths []string
	// DescFilePaths maps source file names to partial (non-bundle) cKDL files
	DescFilePaths map[string]string
	// ImportRoots are fallback roots to join to import paths if we don't
	// have a manually-specified descriptor path (useful for making stuff
	// like a cache dir easy).
	//
	// Paths in here will be checked for the
	// corresponding cKDL file, so `foo/bar/baz.kdl` will be checked as
	// `<root>/foo/bar/baz.ckdl`.
	ImportRoots []string
	// AlwaysUse forces the use of files from ImportRoots,
	// bypassing the normal cache checks (does the hash match).
	AlwaysUse bool

	// TODO: different behavior for cache vs non-cache import sources

	loadOnce sync.Once
	// loadedFiles keeps track of group-version-sets loaded, via their from "virtual" file paths
	// (either path-relative-to-import-root or virtual path from bundle)
	loadedFiles map[string]*ire.Partial

	// loadedFileSources keeps track of which paths were loaded from which bundles
	loadedFileSources map[string]string
}

func (l *CompiledLoader) loadBundle(path string) error {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("unable to find cKDL bundle %q: %w", path, err)
	}

	var bundle ire.Bundle
	if err := proto.Unmarshal(contents, &bundle); err != nil {
		return fmt.Errorf("unable to load cKDL bundle %q: %w", path, err)
	}

	for _, file := range bundle.VirtualFiles {
		if err := l.addFile(file.Name, file.Contents); err != nil {
			return fmt.Errorf("unable to process virtual file %q from cKDL bundle %q: %w", file.Name, path, err)
		}
		l.loadedFileSources[file.Name] = path
	}
	return nil
}

func (l *CompiledLoader) loadPartial(path string, as string) error {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("unable to find cKDL file %q: %w", path, err)
	}

	var partial ire.Partial
	if err := proto.Unmarshal(contents, &partial); err != nil {
		return fmt.Errorf("unable to load cKDL file %q: %w", path, err)
	}

	if err := l.addFile(as, &partial); err != nil {
		return fmt.Errorf("unable to process cKDL file %q (as %q): %w", path, as, err)
	}

	return nil
}

func (l *CompiledLoader) addFile(path string, partial *ire.Partial) error {
	if _, exists := l.loadedFiles[path]; exists {
		from := l.loadedFileSources[path]
		if from != "" {
			return fmt.Errorf("file already loaded from bundle %q", from)
		}
		return fmt.Errorf("file already loaded from disk")
	}
	l.loadedFiles[path] = partial
	l.loadedFileSources[path] = ""

	return nil
}

// TODO: avoid import loops!

func (l *CompiledLoader) requestFile(path string) (bool, error) {
	// first, check if we already have it
	partial, loaded := l.loadedFiles[path]
	if loaded {
		if partial == nil {
			// import loop (see loadPartial)
			return true, fmt.Errorf("import loop involving cKDL file %q", path)
		}
		return true, nil
	}

	// otherwise, check if we have it from a partial descriptor file
	descPath, known := l.DescFilePaths[path]
	if known {
		return true, l.loadPartial(descPath, path)
	}

	osPath := filepath.FromSlash(path)
	ext := filepath.Ext(osPath)
	if ext == ".kdl" {
		// convert to .ckdl
		osPath = osPath[:len(osPath)-3]+"ckdl"
	}

	// otherwise check if the file is below our current import roots
	for _, root := range l.ImportRoots {
		// create a filesystem path by splitting the virtual path
		// and re-joining it to the actual path
		fullPath := filepath.Join(root, osPath)
		if _, err := os.Stat(fullPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return true, fmt.Errorf("unable to load cKDL file %q (as %q): %w", fullPath, path, err)
		}
		if !l.AlwaysUse {
			panic("TODO: cache checks")
		}
		return true, l.loadPartial(fullPath, path)
	}
	
	// not found, just say that by returning false
	return false, nil
}

func (l *CompiledLoader) ensureInit() error {
	var err error
	l.loadOnce.Do(func() {
		l.loadedFiles = make(map[string]*ire.Partial)
		l.loadedFileSources = make(map[string]string)

		// load bundles eagerly, since they're virtual file systems
		for _, bundlePath := range l.BundlePaths {
			err = l.loadBundle(bundlePath)
			if err != nil {
				return
			}
		}
	})
	return err
}

func (l *CompiledLoader) FromCompiled(ctx context.Context, path string) (*ire.Partial, bool) {
	ctx = trace.Describe(ctx, "load from cKDL")
	if err := l.ensureInit(); err != nil {
		return nil, true
	}
	found, err := l.requestFile(path)
	if err != nil {
		trace.ErrorAt(trace.Note(ctx, "error", err), "unable to load CKDL")
		return nil, true
	}
	if !found {
		return nil, false
	}

	partial, known := l.loadedFiles[path]
	if !known || partial == nil {
		panic(fmt.Sprintf("unreachable: no file loaded for %q, but no error occurred requesting", path))
	}
	return partial, true
}

/*
func (l *CompiledLoader) MakeBundle(initialPaths ...string) (*ir.Bundle, error) {
	for _, path := range initialPaths {
		_, err := l.Load(path)
		if err != nil {
			return nil, err
		}
	}

	var res ir.Bundle
	for name, set := range l.loadedFiles {
		res.VirtualFiles = append(res.VirtualFiles, &ir.Bundle_File{
			GroupVersions: set,
			Name: name,
		})
	}

	// sort for consistency
	sort.Slice(res.VirtualFiles, func(i, j int) bool {
		return res.VirtualFiles[i].Name < res.VirtualFiles[j].Name
	})

	return &res, nil
}
*/

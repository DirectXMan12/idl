// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package request

import (
	"io"
	"fmt"
	"io/ioutil"

	"github.com/golang/protobuf/proto"

	ir "k8s.io/idl/ckdl-ir/goir"
)

type GroupVersion struct {
	Group, Version string
}
func (h GroupVersion) String() string {
	return fmt.Sprintf("%s/%s", h.Group, h.Version)
}

type GroupVersionInfo struct {
	OriginalName string
	OriginalPartial *ir.Partial
	GroupVersion *ir.GroupVersion
}

// Loader loads from a reader (generally stdin)
// that expects a bundle.  Often times this comes from
// the runner.
type Loader struct {
	rawBundle *ir.Bundle

	// byPath maps paths in the bundle to the corresponding partial
	byPath map[string]*ir.Partial

	// byGV maps group-versions to the partials that describe that group-version
	byGV map[GroupVersion][]GroupVersionInfo
}
func NewLoader(src io.Reader) (*Loader, error) {
	contents, err := ioutil.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("unable to read cKDL bundle: %w", err)
	}
	var bundle ir.Bundle
	if err := proto.Unmarshal(contents, &bundle); err != nil {
		return nil, fmt.Errorf("unable to load cKDL bundle: %w", err)
	}
	l := &Loader{
		byPath: make(map[string]*ir.Partial, len(bundle.VirtualFiles)),
		byGV: make(map[GroupVersion][]GroupVersionInfo),
	}
	for _, file := range bundle.VirtualFiles {
		l.byPath[file.Name] = file.Contents
		for _, irGV := range file.Contents.GroupVersions {
			gv := GroupVersion{Group: irGV.Description.Group, Version: irGV.Description.Version}
			l.byGV[gv] = append(l.byGV[gv], GroupVersionInfo{
				OriginalPartial: file.Contents,
				GroupVersion: irGV,
				OriginalName: file.Name,
			})
		}
	}
	return l, nil
}

func (l *Loader) Load(path string) (*ir.Partial, error) {
	if set, exists := l.byPath[path]; exists {
		return set, nil
	}
	return nil, fmt.Errorf("file %q not known", path)
}

func (l *Loader) LoadGroupVersion(gv GroupVersion) ([]GroupVersionInfo, error) {
	if srcs, exists := l.byGV[gv]; exists {
		return srcs, nil
	}
	return nil, fmt.Errorf("group-version %s not known", gv)
}

func (l *Loader) GroupVersions() map[GroupVersion][]GroupVersionInfo {
	return l.byGV
}
func (l *Loader) Partials() map[string]*ir.Partial {
	return l.byPath
}

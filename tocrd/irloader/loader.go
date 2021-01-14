// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package irloader

import (
	"io/ioutil"
	"fmt"

	"github.com/golang/protobuf/proto"
	
	ir "k8s.io/idl/ckdl-ir/goir"
)

type Loader interface {
	Load(Hint) (*ir.GroupVersionSet, error)
}

type Hint struct {
	Group, Version string
}

type DescFileLoader struct {
	DescFile string
}

func (l *DescFileLoader) Load(hint Hint) (*ir.GroupVersionSet, error) {
	// TODO: save imports in IR somehow or something? This isn't great
	contents, err := ioutil.ReadFile(l.DescFile)
	if err != nil {
		return nil, fmt.Errorf("unable to load C-KDL descriptor file: %w", err)
	}
	var set ir.GroupVersionSet
	if err := proto.Unmarshal(contents, &set); err != nil {
		return nil, fmt.Errorf("unable to unmarshal C-KDL descriptor file: %w", err)
	}
	return &set, nil
}

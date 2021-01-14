// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package ast

type DepSet struct {
	Main File
	Deps map[GroupVersionRef]*DepSet
}

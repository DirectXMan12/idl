// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package main

import (
	"os"
	"fmt"
	"encoding/json"
	"bytes"

	"k8s.io/idl/backends/common/request"
	"k8s.io/idl/backends/common/respond"

	"k8s.io/idl/backends/tocrd/crd"
)

func main() {
	loader, types := request.Parse()

	parser := &crd.Parser{Loader: loader}

	hadErr := false
	for _, typ := range types {
		ident := crd.TypeIdent{
			GroupVersion: crd.GroupVersion{Group: typ.Group, Version: typ.Version},
			Name: typ.Type,
		}
		groupKind := crd.GroupKind{Group: ident.Group, Kind: ident.Name}
		parser.NeedGroupVersion(ident.GroupVersion)
		parser.NeedSchemaFor(ident)
		parser.NeedCRDFor(groupKind, nil)

		outCRD, present := parser.CustomResourceDefinitions[groupKind]
		if !present {
			respond.GeneralError(nil, "no CRD found", "group", ident.Group, "version", ident.Version, "kind", ident.Name)
			hadErr = true
			continue
		}

		asJSON, err := json.Marshal(outCRD)
		if err != nil {
			panic(err)
		}
		var out bytes.Buffer
		json.Indent(&out, asJSON, "", "  ")
		fmt.Println(out.String())
	}

	if hadErr == true {
		os.Exit(1)
	}
}

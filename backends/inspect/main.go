// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package main

import (
	"sigs.k8s.io/yaml"

	"k8s.io/idl/backends/common/request"
	"k8s.io/idl/backends/common/respond"
)

func main() {
	loader, types := request.Parse()
	if len(types) != 0 {
		panic("TODO: support generating only for specific types")
	}
	respond.GeneralInfo("beginning")

	for path, partial := range loader.Partials() {
		// TODO(directxman12): also allow writing to disk
		asYAML, err := yaml.Marshal(partial)
		if err != nil {
			respond.GeneralError(err, "unable to convert partial to YAML", "path", path)
			continue
		}
		respond.GeneralInfo("partial", "path", path, "contents", "---\n"+string(asYAML))
	}
}

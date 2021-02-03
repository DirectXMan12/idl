// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package request

import (
	"fmt"
	"strings"
	"os"

	"k8s.io/idl/backends/common/respond"
)

type TypeIdent struct {
	Group, Version, Type string
}
func (t TypeIdent) String() string {
	return fmt.Sprintf("%s/%s::%s", t.Group, t.Version, t.Type)
}


func ParseTypes(typesRaw ...string) ([]TypeIdent, error) {
	var res []TypeIdent
	for _, typeRaw := range typesRaw {
		// TODO: move to common
		gvTypeParts := strings.SplitN(typeRaw, "::", 2)
		if len(gvTypeParts) != 2 {
			return res, fmt.Errorf("invalid group/version::Type %q", typeRaw)
		}
		gvParts := strings.Split(gvTypeParts[0], "/")
		if len(gvParts) != 2 {
			return res, fmt.Errorf("invalid group/version::Type %q", typeRaw)
		}
		res = append(res, TypeIdent{
			Group: gvParts[0],
			Version: gvParts[1],
			Type: gvTypeParts[1],
		})
	}
	return res, nil
}

func Parse() (*Loader, []TypeIdent) {
	loader, err := NewLoader(os.Stdin)
	if err != nil {
		respond.GeneralError(err, "unable to load cKDL bundle")
		os.Exit(1)
	}

	types, err := ParseTypes(os.Args[1:]...)
	if err != nil {
		respond.GeneralError(err, "unable to parse type arguments")
		os.Exit(1)
	}

	return loader, types
}

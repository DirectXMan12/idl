package main

import (
	"strings"
	"os"
	"fmt"
	"encoding/json"
	"bytes"

	"k8s.io/idl/tocrd/irloader"
	"k8s.io/idl/tocrd/crd"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s group/version::Type path/to/data.ckdl", os.Args[0])
		os.Exit(1)
	}
	typRaw := os.Args[1]
	path := os.Args[2]

	loader := &irloader.DescFileLoader{
		DescFile: path,
	}

	gvTypeParts := strings.SplitN(typRaw, "::", 2)
	gvParts := strings.Split(gvTypeParts[0], "/")
	ident := crd.TypeIdent{
		GroupVersion: crd.GroupVersion{Group: gvParts[0], Version: gvParts[1]},
		Name: gvTypeParts[1],
	}

	parser := &crd.Parser{Loader: loader}

	//parser.NeedFlattenedSchemaFor(ident)
	groupKind := crd.GroupKind{Group: ident.Group, Kind: ident.Name}
	parser.NeedGroupVersion(ident.GroupVersion)
	parser.NeedSchemaFor(ident)
	parser.NeedCRDFor(groupKind, nil)

	outCRD, present := parser.CustomResourceDefinitions[groupKind]
	if !present {
		panic(fmt.Sprintf("no CRD found -- %#v", parser.CustomResourceDefinitions))
	}
	asJSON, err := json.Marshal(outCRD)
	if err != nil {
		panic(err)
	}
	var out bytes.Buffer
	json.Indent(&out, asJSON, "", "  ")
	fmt.Fprint(os.Stderr, "Schema:\n")
	fmt.Println(out.String())
}

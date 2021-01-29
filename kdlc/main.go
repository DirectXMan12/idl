// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package main

import (
	"os"
	"fmt"
	"context"
	flag "github.com/spf13/pflag"

	//"gopkg.in/yaml.v2" // just for pretty-printing
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"k8s.io/idl/kdlc/parser/trace"
	"k8s.io/idl/kdlc/passes"
	"k8s.io/idl/kdlc/toir"
)

var (
	importPaths = flag.StringArrayP("import-dir", "I", nil, "import root import paths")
	importBundles = flag.StringArrayP("import-bundle", "B", nil, "import from a CKDL bundle")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [FLAGS...] FILE.kdl\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if len(*importBundles) != 0 {
		panic("TODO: bundle imports")
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	importer := passes.ImportFrom(*importPaths...)

	ctx := trace.RecordError(context.Background())

	res := importer.Load(ctx, flag.Arg(0))
	if trace.HadError(ctx) {
		os.Exit(1)
	}

	// TODO: full input context (maybe put into lookaside)
	irRes := toir.File(ctx, &res.Main)

	out, err := prototext.MarshalOptions{
		Multiline: true,
	}.Marshal(&irRes)
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(os.Stderr, string(out))

	out, err = proto.Marshal(&irRes)
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		panic(err)
	}
}

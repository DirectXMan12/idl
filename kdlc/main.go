package main

import (
	"os"
	"fmt"
	"context"
	//"gopkg.in/yaml.v2" // just for pretty-printing
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"k8s.io/idl/kdlc/passes"
	"k8s.io/idl/kdlc/toir"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s IMPORTDIRS... file.kdl")
		os.Exit(1)
	}
	importer := passes.ImportFrom(os.Args[1:len(os.Args)-1]...)

	res := importer.Load(context.Background(), os.Args[len(os.Args)-1])

	// TODO: full input context (maybe put into lookaside)
	irRes := toir.File(context.Background(), &res.Main)

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

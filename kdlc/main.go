package main

import (
	"os"
	"fmt"
	"io/ioutil"
	"bytes"
	"context"
	"gopkg.in/yaml.v2" // just for pretty-printing

	"k8s.io/idl/kdlc/lexer"
	"k8s.io/idl/kdlc/parser"
	ptrace "k8s.io/idl/kdlc/parser/trace"
)

func main() {
	f, err := os.Open(os.Args[1]) // for testing
	if err != nil {
		panic(err)
	}
	defer f.Close()
	fullInput, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}
	lex := lexer.New(bytes.NewBuffer(fullInput))
	parse := parser.New(lex)
	res := parse.Parse(ptrace.WithFullInput(context.Background(), string(fullInput)))

	resText, err := yaml.Marshal(res)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(resText))
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package main

import (
	"strings"
	"fmt"
	"os"
	"os/exec"
	"context"
	"bytes"
	"path/filepath"

	flag "github.com/spf13/pflag"

	"k8s.io/idl/kdlc/loader"
	"k8s.io/idl/kdlc/parser/trace"
	irb "k8s.io/idl/ckdl-ir/goir/backend"
	"k8s.io/idl/backends/common/respond"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

var (
	importPaths = flag.StringArrayP("import-dir", "i", nil, "root KDL & cKDL import paths")
	importBundles = flag.StringArrayP("import-bundle", "B", nil, "import from CKDL bundle(s)")
	outputFormat = flag.StringP("output", "o", "ckdl-bundle", "what to output (ckdl-bundle, or xyz, where ckdl-to-xyz is an executable on your path)")
	outputArgs = flag.StringArrayP("output-arg", "t", nil, "arguments to pass to the output plugin (e.g. group/version::Type for ckdl-to-crd)")
	outputDir = flag.StringP("output-dir", "d", "", "path to output files from the output format relative to (defaults to the current directory)")
	verbose = flag.BoolP("verbose", "v", false, "whether to output the results as textproto to stderr")

	importPartials = new(mapValue)
	// cacheBehavior = &cacheBehaviorVal{Behavior: "alongside"}
	cacheBehavior = &cacheBehaviorVal{Behavior: "none"} // TODO: eventually alongside
	outputFlags = new(mapValue)
)

type cacheBehaviorVal struct {
	Behavior string
	Dir string
}
func (v *cacheBehaviorVal) Set(s string) error {
	parts := strings.SplitN(s, "=", 2)
	key := strings.ToLower(parts[0])
	switch key {
	case "none":
		fallthrough
	case "alongside":
		v.Behavior = key
	case "dir":
		v.Behavior = "dir"
		if len(parts) != 2 {
			return fmt.Errorf("cache behavior of dir requires an argument, like dir=/some/path")
		}
		v.Dir = parts[1]
	default:
		return fmt.Errorf("unknown cache behavior %q, expected none|alongside|dir=path", parts[0])
	}
	return nil
}
func (v *cacheBehaviorVal) Type() string {
	return "cacheBehavior"
}
func (v *cacheBehaviorVal) String() string {
	switch v.Behavior {
	case "none":
		return "none"
	case "dir":
		return fmt.Sprintf("dir=%s", v.Dir)
	case "alongside":
		return "alongside"
	default:
		return fmt.Sprintf("unknown-%s=%s", v.Type, v.Dir)
	}
}

type mapValue struct {
	Keys, Values []string
	changed bool
}
func (v *mapValue) Set(s string) error {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("must be in key=value form")
	}
	if !v.changed {
		v.Keys = []string{parts[0]}
		v.Values = []string{parts[1]}
		v.changed = true
	} else {
		v.Keys = append(v.Keys, parts[0])
		v.Values = append(v.Values, parts[1])
	}
	return nil
}
func (v *mapValue) Append(s string) error {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("must be in key=value form")
	}
	v.Keys = append(v.Keys, parts[0])
	v.Values = append(v.Values, parts[1])
	return nil
}
func (v *mapValue) Replace(vals []string) error {
	v.Keys = nil
	v.Values = nil
	for _, s := range vals {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("must be in key=value form")
		}
		v.Keys = append(v.Keys, parts[0])
		v.Values = append(v.Values, parts[1])
	}
	return nil
}
func (v *mapValue) GetSlice() []string {
	res := make([]string, len(v.Keys))
	for i, k := range v.Keys {
		res[i] = fmt.Sprintf("%s=%s", k, v.Values[i])
	}
	return res
}
func (v *mapValue) Type() string {
	return "keyValueArray"
}
func (v *mapValue) String() string {
	parts := v.GetSlice()
	return fmt.Sprintf("[%s]", strings.Join(parts, ","))
}

func main() {
	flag.VarP(importPartials, "import-partial", "I", "import from CKDL(s) partial files")
	flag.Var(cacheBehavior, "cache", "where to read/write cKDL partial files from/to")
	flag.VarP(outputFlags, "output-flag", "f", "flags to pass to the output plugin (`key=value` becomes `--kdl-key=value`)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [FLAGS...] VIRTUALPATH...\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "where VIRTUALPATH are the import-root-relative paths to KDL files to start from")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if *outputDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			panic(fmt.Sprintf("no output directory specified, unable to determine current working directory: %v", err))
		}
		*outputDir = cwd
	}

	compiledImp := &loader.CompiledLoader{
		BundlePaths: *importBundles,
		DescFilePaths: make(map[string]string),
	}
	for i, kdlPath := range importPartials.Keys {
		// TODO: validate not specified twice?
		ckdlPath := importPartials.Values[i]
		compiledImp.DescFilePaths[kdlPath] = ckdlPath
	}
	if cacheBehavior.Behavior != "none" {
		panic("TODO: cache support")
	}

	cfg := loader.Config{
		Roots: flag.Args(),
		Imports: &loader.HybridLoader{
			Source: loader.SourceLoader{Roots: *importPaths},
			Compiled: compiledImp,
		},
	}

	ctx := trace.RecordError(context.Background())
	cfg.Load(ctx)


	bundle := cfg.Outputs.BundleFor(ctx, flag.Args()...)
	if bundle == nil {
		os.Exit(1)
	}

	if *verbose {
		out, err := prototext.MarshalOptions{
			Multiline: true,
		}.Marshal(bundle)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, string(out))
	}

	bundleOut, err := proto.Marshal(bundle)
	if err != nil {
		panic(err)
	}

	if *outputFormat == "ckdl-bundle" {
		if _, err := os.Stdout.Write(bundleOut); err != nil {
			panic(err)
		}
		return
	}

	// exec ckdl-to-FORMAT [flags...] args...
	// read responses from stderr to map back to source map
	// dump output from stdout (manage output location somehow?)

	cmdName := "ckdl-to-"+*outputFormat
	var args []string
	for i, flagName := range outputFlags.Keys {
		flagVal := outputFlags.Values[i]
		args = append(args, "--%s=%s", flagName, flagVal)
	}
	if len(*outputArgs) != 0 {
		args = append(args, "--")
	}
	for _, arg := range *outputArgs {
		args = append(args, arg)
	}
	// TODO: StderrPipe
	cmd := exec.Command(cmdName, args...)
	cmdOut := new(bytes.Buffer)
	cmdErr := new(bytes.Buffer)
	cmd.Stdout = cmdOut
	cmd.Stderr = cmdErr
	cmd.Stdin = bytes.NewReader(bundleOut)

	runErr := cmd.Run()
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "error running command %q:\n\t%s\n\n", cmd.String(), cmdErr.String())
	}

	msgsRaw := cmdOut.Bytes()
	var msg irb.Response
	for len(msgsRaw) > 0 {
		msgsRaw = respond.Read(&msg, msgsRaw)

		switch msgWrapper := msg.Type.(type) {
		case *irb.Response_Result:
			file := msgWrapper.Result
			if *outputDir == "-" {
				fmt.Printf("---\n# %s\n%s\n", file.Name, string(file.Contents))
				continue
			}

			outputPath := filepath.Join(*outputDir, filepath.FromSlash(file.Name))
			// TODO(directxman12): there's no easy way to check "does this
			// contain a ../" in it in a cross-platform way for now, so trust
			// the generator for the moment.  Should fix later.
			func() {
				outFile, err := os.Create(outputPath)
				if err != nil {
					// TODO
					panic(err)
				}
				defer outFile.Close()
				// TODO: ensure the directory exists
				if _, err := outFile.Write(file.Contents); err != nil {
					// TODO
					panic(err)
				}
			}()
		case *irb.Response_Log:
			msg := msgWrapper.Log
			fmt.Fprintf(os.Stderr, "[%s] ", msg.Lvl)
			// TODO: unify with parser/trace logic
			for i, tr := range msg.Trace {
				if i != 0 {
					fmt.Fprint(os.Stderr, "\t")
				}
				fmt.Fprintf(os.Stderr, "%s", tr.Message) // TODO
				for _, kv := range tr.Values {
					// TODO: other_node
					switch val := kv.Value.(type) {
					case *irb.Log_Trace_KeyValue_Str:
						fmt.Fprintf(os.Stderr, " %s=%s", kv.Key, val.Str)
					default:
						// TODO
						fmt.Fprintf(os.Stderr, " %s=<unsupported-%T>", kv.Key, val)
					}
				}
				fmt.Fprintln(os.Stderr, "")
				// TODO: print node
			}
		default:
			panic(fmt.Sprintf("unknown response type %T", msgWrapper))
		}
	}
	fmt.Fprintln(os.Stderr, "")

	// TODO: check for error logs

	if runErr != nil {
		os.Exit(1)
	}


}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package respond

import (
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/encoding/protowire"

	ir "k8s.io/idl/ckdl-ir/goir/backend"
)

func Write(msg *ir.Response) {
	out := protowire.AppendVarint(nil, uint64(proto.Size(msg)))
	out, err := proto.MarshalOptions{}.MarshalAppend(out, msg)
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(out)
}

func File(path string, contents []byte) {
	Write(&ir.Response{
		Type: &ir.Response_Result{Result: &ir.File{
			Name: path,
			Contents: contents,
		}},
	})
}

func Read(msg *ir.Response, from []byte) []byte {
	size, sizeSize := protowire.ConsumeVarint(from)
	if size < 0 {
		panic("unable to read backend response size")
	}

	from = from[sizeSize:]
	if err := proto.Unmarshal(from[:size], msg); err != nil {
		panic(err)
	}
	return from[size:]
}

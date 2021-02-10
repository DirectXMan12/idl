// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package main

import (
	"context"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"k8s.io/idl/backends/common/request"
	"k8s.io/idl/backends/common/respond"
	"k8s.io/idl/kdlc/mdesc"
)

func main() {
	loader, types := request.Parse()
	if len(types) != 0 {
		panic("TODO: support generating only for specific types")
	}
	respond.GeneralInfo("beginning")
	ctx := context.Background()

	for srcPath, partial := range loader.Partials() {
		// TODO: share this logic between the kdlc and this?
		for _, set := range partial.MarkerSets {
			respond.GeneralInfo("processing package", "package", set.Package)
			res := mdesc.MakeDescriptor(ctx, srcPath, set)
			if len(res.DescriptorNames) != len(set.Markers) {
				// TODO: get this from trace.HadError
				respond.GeneralError(nil, "unable to convert markers to proto definitions", "package", set.Package)
				continue
			}

			// TODO: figure out how to get this as proto source instead of a descriptor
			contents, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{res.File}})
			if err != nil {
				respond.GeneralError(err, "unable to marshal proto descriptor", "package", set.Package)
				continue
			}
			descPath := srcPath+".desc"
			respond.File(descPath, contents)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package respond

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"

	ir "k8s.io/idl/ckdl-ir/goir"
)

const noLoc = -1

type Tracker struct {
	parent *Tracker
	current protoreflect.MessageDescriptor
	loc int32
	rep bool
}
func TrackSrc(file *ir.Partial) *Tracker {
	return &Tracker{
		current: file.ProtoReflect().Descriptor(),
		loc: noLoc, // sentinel, avoid us
	}
}

func (m *Tracker) Field(field protoreflect.Name) *Tracker {
	if m.rep || m.current == nil {
		panic("tried to specify field path element on repeated or non-message field")
	}
	fieldDesc := m.current.Fields().ByName(field)
	if fieldDesc == nil {
		panic(fmt.Sprintf("unknown cKDL message field %q", field))
	}
	return &Tracker{
		loc: int32(fieldDesc.Number()),
		rep: fieldDesc.Cardinality() == protoreflect.Repeated,
		current: fieldDesc.Message(),
		parent: m,
	}
}

func (m *Tracker) Item(ind int) *Tracker {
	if !m.rep {
		panic("tried to specify item path element on non-repeated field")
	}
	return &Tracker{
		loc: int32(ind),
		current: m.current,
		parent: m,
	}
}

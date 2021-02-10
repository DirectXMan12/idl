package srcmap

import (
	"go/ast"
	"go/token"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"

	irt "k8s.io/idl/ckdl-ir/goir/types"
	ir "k8s.io/idl/ckdl-ir/goir"
)

type SpanContext struct {
	// TODO: keep linked list here?
	typ *groupVerType
	parent *SpanContext
	pathElem int32
	msg protoreflect.MessageDescriptor
	field protoreflect.FieldDescriptor // used to double-check that a field is repeated
}

func (c *SpanContext) path() []int32 {
	var res []int32
	res = append(res, c.pathElem)
	for parent := c.parent; parent != nil; parent = parent.parent {
		res = append(res, parent.pathElem)
	}

	// reverse, since we traveled up the stack
	for i := len(res)/2-1; i >= 0; i-- {
		opp := len(res)-1-i
		res[i], res[opp] = res[opp], res[i]
	}

	return res
}

func (c *SpanContext) ForField(name string) *SpanContext {
	if c.msg == nil {
		panic("cannot get a field without a message (maybe you recorded a repeated field?)")
	}

	field := c.msg.Fields().ByName(protoreflect.Name(name))
	if field == nil {
		panic(fmt.Sprintf("no such field %q on message %q", name, c.msg.Name()))
	}
	res := &SpanContext{
		typ: c.typ,
		parent: c,
		pathElem: int32(field.Number()),
		field: field,
	}

	if field.Cardinality() != protoreflect.Repeated && field.Kind() == protoreflect.MessageKind {
		res.msg = field.Message()
	}

	return res
}

func (c *SpanContext) ForIndex(index int) *SpanContext {
	if c.field == nil {
		panic("cannot record an index without chosing a field first")
	}
	if c.field.Cardinality() != protoreflect.Repeated {
		panic(fmt.Sprintf("cannot record an index span on non-repeated field %q", c.field.Name()))
	}
	res := &SpanContext{
		typ: c.typ,
		parent: c,
		pathElem: int32(index),
	}
	if c.field.Kind() == protoreflect.MessageKind {
		res.msg = c.field.Message()
	}
	return res
}

type groupVerType struct {
	group, version, name string
}
type SpanTracker struct {
	Fset *token.FileSet

	locations map[groupVerType][]*ir.Location
}

func (t *SpanTracker) ForKind(msg *irt.Kind, gv *irt.GroupVersionRef) *SpanContext {
	return &SpanContext{
		typ: &groupVerType{group: gv.Group, version: gv.Version, name: msg.Name},
		pathElem: -1,
		msg: msg.ProtoReflect().Descriptor(),
	}
}

func (t *SpanTracker) ForSubtype(msg *irt.Subtype, gv *irt.GroupVersionRef) *SpanContext {
	return &SpanContext{
		typ: &groupVerType{group: gv.Group, version: gv.Version, name: msg.Name},
		pathElem: -1,
		msg: msg.ProtoReflect().Descriptor(),
	}
}

func (t *SpanTracker) Record(ctx *SpanContext, node ast.Node) {
	t.locations[*ctx.typ] = append(t.locations[*ctx.typ], &ir.Location{
		// TODO: is this right?  I think we actually need to get the offset
		// relative to the file in the fset
		Span: []int32{int32(node.Pos()), int32(node.End())},
		Path: ctx.path(),
	})
}

func Track(fset *token.FileSet) *SpanTracker {
	return &SpanTracker{
		Fset: fset,
		locations: make(map[groupVerType][]*ir.Location),
	}
}

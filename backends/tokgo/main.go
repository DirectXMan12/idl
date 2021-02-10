// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package main

import (
	"fmt"
	"strings"
	"bytes"
	"io"
	"sort"
	gofmt "go/format"
	"path"

	"k8s.io/idl/backends/common/request"
	"k8s.io/idl/backends/common/respond"
	pany "github.com/golang/protobuf/ptypes/any"


	ir "k8s.io/idl/ckdl-ir/goir"
	irt "k8s.io/idl/ckdl-ir/goir/types"
)

type comment struct {
	doc *irt.Documentation
	markers []marker
	isKind bool
}

type marker struct {
	name string
	value string
}

type recordedType struct {
	name string
	contents *bytes.Buffer
	constBlock *bytes.Buffer
}
type groupVersion struct {
	Group, Version string
}
type imports struct {
	byGV map[groupVersion]string
	used map[string]struct{}
}

type pkgWriter struct {
	Boilerplate string
	SortOrder map[string]int
	CurrentGV groupVersion

	imports imports
	header *bytes.Buffer
	types []recordedType
	typeInds map[string]int
}
func newPkgWriter() *pkgWriter {
	return &pkgWriter{
		imports: imports{
			byGV: make(map[groupVersion]string),
			used: make(map[string]struct{}),
		},
		header: new(bytes.Buffer),
		typeInds: make(map[string]int),
	}
}
func writeComments(comments *comment, out *bytes.Buffer) {
	// TODO: option to make markers second-closest instead of in-block
	// TODO: allow switching kind markers up
	// TODO: kind markers

	if doc := comments.doc; doc != nil {
		if doc.Description != "" {
			for _, line := range strings.Split(strings.TrimSuffix(doc.Description, "\n"), "\n") {
				fmt.Fprintf(out, "// %s\n", line)
			}
		}
	}
	for _, marker := range comments.markers {
		fmt.Fprintf(out, "// +%s", marker.name)
		if marker.value != "" {
			fmt.Fprintf(out, "=%s", marker.value)
		}
		fmt.Fprintln(out, "")
	}

}
func (w *pkgWriter) Bytes() []byte {
	var res bytes.Buffer
	w.Write(&res)
	return res.Bytes()
}
func (w *pkgWriter) Write(out io.Writer) {
	if w.Boilerplate != "" {
		fmt.Fprintf(out, "%s\n\n", w.Boilerplate)
	}

	io.Copy(out, w.header)

	fmt.Fprintln(out, "import (")
	for gv, alias := range w.imports.byGV {
		// TODO: figure out import paths
		var path string
		switch gv.Group {
		case "__resource":
			path =  "k8s.io/apimachinery/pkg/api/resource"
		case "__intstr":
			path = "k8s.io/apimachinery/pkg/util/intstr"
		default:
			path = fmt.Sprintf("k8s.io/api/%s/%v", gv.Group, gv.Version)
		}
		fmt.Fprintf(out, "\t%s %q\n", alias, path)
	}
	fmt.Fprintln(out, ")\n")

	if w.SortOrder != nil {
		sort.Slice(w.types, func(i, j int) bool {
			iName, jName := w.types[i].name, w.types[j].name
			return w.SortOrder[iName] < w.SortOrder[jName]
		})
	}

	for _, typ := range w.types {
		io.Copy(out, typ.contents)
		if typ.constBlock != nil {
			fmt.Fprintln(out, "")
			io.Copy(out, typ.constBlock)
		}
		fmt.Fprintln(out, "")
	}
}
func (w *pkgWriter) Package(nameHint string, comments comment) {
	// TODO: allow overriding name, etc
	writeComments(&comments, w.header)
	fmt.Fprintf(w.header, "package %s\n\n", nameHint)
}
func (w *pkgWriter) BlockType(name string, comments comment, cb func(*subWriter)) {
	out := new(bytes.Buffer)
	writeComments(&comments, out)
	fmt.Fprintf(out, "type %s struct {\n", name)
	subOut := &subWriter{
		out: out,
		parent: w,
	}
	cb(subOut)
	fmt.Fprint(out, "}\n")
	w.typeInds[name] = len(w.types)
	w.types = append(w.types, recordedType{name: name, contents: out})
}
func (w *pkgWriter) LineType(name string, comments comment, typeStr string) {
	out := new(bytes.Buffer)
	writeComments(&comments, out)
	fmt.Fprintf(out, "type %s %s\n", name, typeStr)
	w.typeInds[name] = len(w.types)
	w.types = append(w.types, recordedType{name: name, contents: out})
}
type enumConst struct {
	name string
	value string
	comment comment
}
func (w *pkgWriter) ConstBlock(typeName string, variants ...enumConst) {
	out := new(bytes.Buffer)
	fmt.Fprintln(out, "const (")
	for _, variant := range variants {
		writeComments(&variant.comment, out)
		fmt.Fprintf(out, "%s %s = %q\n", variant.name, typeName, variant.value)
	}
	fmt.Fprintln(out, ")")
	w.types[w.typeInds[typeName]].constBlock = out
}
func (w *pkgWriter) MakeGVRef(ref *irt.Reference) string {
	typeName := nameType(ref.Name, nil) // TODO: we need the typecheck graph to get the underlying markers for this
	if ref.GroupVersion.Group == w.CurrentGV.Group && ref.GroupVersion.Version == w.CurrentGV.Version {
		return typeName
	}
	gv := groupVersion{Group: ref.GroupVersion.Group, Version: ref.GroupVersion.Version}
	if existing, exists := w.imports.byGV[gv]; exists {
		return existing+"."+typeName
	}

	// first try combinations of group+version, starting with just the first
	// part, then going forward.
	groupParts := strings.Split(gv.Group, ".")
	groupParts[0] = strings.TrimPrefix(groupParts[0], "__")
	for partsToTry := 1; partsToTry <= len(groupParts); partsToTry++ {
		candidate := strings.Replace(strings.Join(groupParts[0:partsToTry], "")+gv.Version, "-", "_", -1)
		_, exists := w.imports.used[candidate]
		if !exists {
			w.imports.used[candidate] = struct{}{}
			w.imports.byGV[gv] = candidate
			return candidate+"."+typeName
		}
	}

	// last ditch, start appending numbers
	base := strings.Replace(strings.Join(groupParts, "")+gv.Version, "-", "_", -1)
	for i := 0; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		_, exists := w.imports.used[candidate]
		if !exists {
			w.imports.used[candidate] = struct{}{}
			w.imports.byGV[gv] = candidate
			return candidate+"."+typeName
		}
	}
}
type subWriter struct {
	parent *pkgWriter
	out *bytes.Buffer
}
func (w *subWriter) Field(name string, typ string, structTag tags, comments comment) {
	writeComments(&comments, w.out)
	if name != "" {
		fmt.Fprintf(w.out, "\t%s %s", name, typ)
	} else {
		fmt.Fprintf(w.out, "\t%s", typ)
	}
	if !structTag.empty() {
		fmt.Fprint(w.out, " `")
		if len(structTag.json) != 0 {
			fmt.Fprintf(w.out, `json:"%s"`, strings.Join(structTag.json, ","))
		}
		if len(structTag.proto) != 0 {
			fmt.Fprintf(w.out, ` proto:"%s"`, strings.Join(structTag.proto, ","))
		}
		fmt.Fprint(w.out, "`")
	}
	fmt.Fprintln(w.out, "")

}
func (w *subWriter) MakeGVRef(ref *irt.Reference) string {
	return w.parent.MakeGVRef(ref)
}
type tag []string
type tags struct {
	json tag
	proto tag
	patchStrategy string
	patchMergeKey string
}
func (t tags) empty() bool {
	return len(t.json) == 0 && len(t.proto) == 0 && t.patchStrategy == "" && t.patchMergeKey == ""
}

func main() {
	loader, types := request.Parse()
	if len(types) != 0 {
		panic("TODO: support generating only for specific types")
	}
	respond.GeneralInfo("beginning")

	for gv, infos := range loader.GroupVersions() {
		respond.GeneralInfo("processing group-version", "group", gv.Group, "version", gv.Version)
		out := newPkgWriter() // TODO
		out.CurrentGV = groupVersion{Group: gv.Group, Version: gv.Version}
		irs := make([]*ir.GroupVersion, len(infos))
		for i, info := range infos {
			irs[i] = info.GroupVersion
		}
		writeGo(irs, out)

		rawSrc := out.Bytes()
		fmtSrc, err := gofmt.Source(rawSrc)
		if err != nil {
			respond.GeneralError(err, "unable to gofmt file", "group", gv.Group, "version", gv.Version)
			fmtSrc = rawSrc
		}
		// everything is path, not filepath, until we read to/write from disk
		// TODO: check that path is actually the canonical format everywhere
		// TODO: marker to override this
		// TODO: flag to override this
		outFileName := path.Join(path.Dir(infos[0].OriginalName), "types.go")
		if len(infos) > 1 {
			respond.GeneralInfo("multiple source KDL files for group-version, using first for output name", "group", gv.Group, "version", gv.Version)
		}
		respond.File(outFileName, fmtSrc)
	}

}

func writeGo(inputs []*ir.GroupVersion, out *pkgWriter) {
	pkgComments := comment{
		doc: &irt.Documentation{},
	}
	for _, gv := range inputs {
		if gv.Description.Docs != nil {
			pkgComments.doc.Description += gv.Description.Docs.Description
			pkgComments.doc.ExternalRef += gv.Description.Docs.ExternalRef
			pkgComments.doc.Example += gv.Description.Docs.Example
		}
	}
	out.Package(inputs[0].Description.Version, pkgComments)

	for _, gv := range inputs {
		for _, kind := range gv.Kinds {
			typeName := nameType(kind.Name, kind.Attributes)
			out.BlockType(typeName, comment{isKind: true, doc: kind.Docs}, func(out *subWriter) {
				out.Field("", "metav1.TypeMeta", tags{json: tag{"", "inline"}}, comment{})
				out.Field("", "metav1.ObjectMeta", tags{
					json: tag{"metadata","omitempty"},
					proto: tag{"bytes", "1", "opt", "name=metadata"},
				}, comment{
					doc: &irt.Documentation{
						Description: "Standard object's metadata.\nMore info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata",
					},
					markers: []marker{{name: "optional"}},
				})

				for _, field := range kind.Fields {
					writeField(field, out)
				}
			})
		}
		for _, subtype := range gv.Types {
			typeName := nameType(subtype.Name, subtype.Attributes)
			comments := comment{doc: subtype.Docs}
			switch body := subtype.Type.(type) {
			case *irt.Subtype_ReferenceAlias:
				typeStr := out.MakeGVRef(body.ReferenceAlias)
				out.LineType(typeName, comments, typeStr)
			case *irt.Subtype_PrimitiveAlias:
				typeStr := primType(body.PrimitiveAlias, false, out)
				out.LineType(typeName, comments, typeStr)
			case *irt.Subtype_Union:
				union := body.Union
				tagType := typeName+"Type" // TODO: avoid conflicts when not in kgo mode with underscore or something
				out.BlockType(typeName, comments, func(out *subWriter) {
					if !union.Untagged {
						goTagName := strings.Title(union.Tag)
						out.Field(goTagName, tagType, tags{json: tag{union.Tag}, proto: tag{"bytes", "1"}}, comment{})
					}
					for _, field := range union.Variants {
						writeField(field, out)
					}
				})
				if !union.Untagged {
					variants := make([]enumConst, len(union.Variants))
					for i, field := range union.Variants {
						fieldName := nameField(field.Name, field.Attributes)
						variants[i] = enumConst{
							name: typeName+fieldName,
							value: fieldName,
							comment: comment{doc: field.Docs},
						}
					}
					out.ConstBlock(tagType, variants...)
				}
			case *irt.Subtype_Struct:
				strct := body.Struct
				out.BlockType(typeName, comments, func(out *subWriter) {
					for _, field := range strct.Fields {
						writeField(field, out)
					}
				})
			case *irt.Subtype_Set:
				typeStr := setType(body.Set, &tags{}, &comments, out)
				out.LineType(typeName, comments, typeStr)
			case *irt.Subtype_List:
				typeStr := listType(body.List, out)
				out.LineType(typeName, comments, typeStr)
			case *irt.Subtype_PrimitiveMap:
				typeStr := primMapType(body.PrimitiveMap, &tags{}, &comments, out)
				out.LineType(typeName, comments, typeStr)
			case *irt.Subtype_ListMap:
				typeStr := listMapType(body.ListMap, &tags{}, &comments, out)
				out.LineType(typeName, comments, typeStr)
			case *irt.Subtype_Enum:
				out.LineType(typeName, comments, "string")
				variants := make([]enumConst, len(body.Enum.Variants))
				for i, variant := range body.Enum.Variants {
					// TODO: marker to override this
					variants[i] = enumConst{
						name: typeName+variant.Name,
						value: variant.Name,
						comment: comment{doc: variant.Docs},
					}
				}
				out.ConstBlock(typeName, variants...)
			default:
				panic(fmt.Sprintf("unreachable: unknown field type %T", body))
			}
		}
	}
}

func nameField(rawName string, attrs []*pany.Any) string {
	for _, attr := range attrs {
		if attr.MessageIs(&Name{}) {
			var name Name
			if err := attr.UnmarshalTo(&name); err != nil {
				panic("TODO: "+err.Error())
			}
			return name.Name
		}
	}
	return strings.Title(rawName)
}

func nameType(rawName string, attrs []*pany.Any) string {
	for _, attr := range attrs {
		if attr.MessageIs(&Name{}) {
			var name Name
			if err := attr.UnmarshalTo(&name); err != nil {
				panic("TODO: "+err.Error())
			}
			return name.Name
		}
	}
	return strings.Replace(rawName, "::", "", -1)
}

var (
	timeRef = &irt.Reference{
		GroupVersion: &irt.GroupVersionRef{
			Group: "meta.k8s.io",
			Version: "v1",
		},
		Name: "Time",
	}

	durationRef = &irt.Reference{
		GroupVersion: &irt.GroupVersionRef{
			Group: "meta.k8s.io",
			Version: "v1",
		},
		Name: "Duration",
	}

	quantityRef = &irt.Reference{
		GroupVersion: &irt.GroupVersionRef{
			Group: "__resource",
			Version: "",
		},
		Name: "Duration",
	}

	intStrRef = &irt.Reference{
		GroupVersion: &irt.GroupVersionRef{
			Group: "__intstr",
			Version: "",
		},
		Name: "Duration",
	}
)


func primType(typ *irt.Primitive, isPtr bool, refs refMaker) string {
	typeStr := ""
	switch typ.Type {
	case irt.Primitive_STRING:
		typeStr = "string"
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_LEGACYINT32:
		typeStr = "int32"
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_INT64:
		typeStr = "int64"
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_BOOL:
		typeStr = "bool"
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_TIME:
		typeStr = refs.MakeGVRef(timeRef)
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_DURATION:
		typeStr = refs.MakeGVRef(durationRef)
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_QUANTITY:
		typeStr = refs.MakeGVRef(quantityRef)
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_BYTES:
		typeStr = "[]bytes"
	case irt.Primitive_LEGACYFLOAT64:
		typeStr = "float64"
		if isPtr {
			typeStr = "*"+typeStr
		}
	case irt.Primitive_INTORSTRING:
		typeStr = refs.MakeGVRef(intStrRef)
		if isPtr {
			typeStr = "*"+typeStr
		}
	default:
		// TODO: tracking of errors
		respond.GeneralError(nil, "unknown primitive type", "type", typ.Type)
		typeStr = "<invalid>"
	}
	return typeStr
}

type refMaker interface {
	MakeGVRef(*irt.Reference) string
}

func listType(list *irt.List, refs refMaker) string {
	switch items := list.Items.(type) {
	case *irt.List_Primitive:
		return "[]"+primType(items.Primitive, false, refs)
	case *irt.List_Reference:
		return "[]"+refs.MakeGVRef(items.Reference)
	default:
		panic(fmt.Sprintf("unreachable: unknown list items type %T"))
	}
}
func setType(set *irt.Set, fieldTag *tags, comment *comment, refs refMaker) string {
	comment.markers = append(comment.markers, marker{
		name: "listType",
		value: "set",
	})
	switch items := set.Items.(type) {
	case *irt.Set_Primitive:
		return "[]"+primType(items.Primitive, false, refs)
	case *irt.Set_Reference:
		return "[]"+refs.MakeGVRef(items.Reference)
	default:
		panic(fmt.Sprintf("unreachable: unknown set items type %T"))
	}
}
func primMapType(primMap *irt.PrimitiveMap, fieldTag *tags, comment *comment, refs refMaker) string {
	typeStr := "map["
	switch key := primMap.Key.(type) {
	case *irt.PrimitiveMap_PrimitiveKey:
		typeStr += primType(key.PrimitiveKey, false, refs)
	case *irt.PrimitiveMap_ReferenceKey:
		typeStr += refs.MakeGVRef(key.ReferenceKey)
	default:
		panic(fmt.Sprintf("unreachable: unknown simple-map key type %T"))
	}
	typeStr += "]"

	switch value := primMap.Value.(type) {
	case *irt.PrimitiveMap_PrimitiveValue:
		typeStr += primType(value.PrimitiveValue, false, refs)
	case *irt.PrimitiveMap_ReferenceValue:
		typeStr += refs.MakeGVRef(value.ReferenceValue)
	case *irt.PrimitiveMap_SimpleListValue:
		typeStr += listType(value.SimpleListValue, refs)
	default:
		panic(fmt.Sprintf("unreachable: unknown simple-map value type %T"))
	}

	return typeStr
}
func listMapType(listMap *irt.ListMap, fieldTag *tags, comment *comment, refs refMaker) string {
	comment.markers = append(comment.markers, marker{
		name: "patchMergeKey",
		value: listMap.KeyField[0],
	}, marker{
		name: "patchStrategy",
		value: "merge",
	}, marker{
		name: "listType",
		value: "map",
	})

	for _, key := range listMap.KeyField {
		comment.markers = append(comment.markers, marker{
			name: "listMapKey",
			value: key,
		})
	}

	fieldTag.patchStrategy = "merge"
	fieldTag.patchMergeKey = listMap.KeyField[0]

	return "[]"+refs.MakeGVRef(listMap.Items)
}

func writeField(field *irt.Field, out *subWriter) {
	comment := comment{doc: field.Docs}
	fieldTag := tags{
		json: tag{field.Name},
	}

	// TODO: allow override with marker
	fieldName := nameField(field.Name, field.Attributes)

	// most of the fields are ignored by the generator
	// as long as we satisfy the magic incantation of
	// `bytes,<number>,<stuff>,name=xyz`.

	// *HOWEVER* we'll go through a lot of extra work anyway
	// to avoid diff-drift where we can, guessing at what
	// people might've manually put here (people are wrong
	// frequently though, and we're a bit lazy with our guesses,
	// so *shrug*).
	optOrRep := "" // either proto opt or proto rep guess
	protoType := "bytes" // always gonna either be bytes or varint, never anything else

	if field.Optional {
		fieldTag.json = append(fieldTag.json, "omitempty")
		comment.markers = append(comment.markers, marker{name: "optional"})
		optOrRep = "opt"
	}
	if field.Embedded {
		fieldTag.json = append(fieldTag.json, "inline")
	}

	// TODO(directxman12): default
	// TODO(directxman12): constraints

	isPtr := !field.ZeroMeansAbsent && field.Optional
	typeStr := ""
	switch typ := field.Type.(type) {
	case *irt.Field_Primitive:
		switch typ.Primitive.Type {
		case irt.Primitive_BOOL, irt.Primitive_LEGACYINT32, irt.Primitive_INT64:
			protoType = "varint"
		}
		typeStr = primType(typ.Primitive, isPtr, out)
	case *irt.Field_NamedType:
		typeStr = out.MakeGVRef(typ.NamedType)
		// TODO: we need to check the eventual type of this reference to know this
		if isPtr {
			typeStr = "*"+typeStr
		}
	case *irt.Field_Set:
		optOrRep = "rep"
		typeStr = setType(typ.Set, &fieldTag, &comment, out)
	case *irt.Field_List:
		optOrRep = "rep"
		typeStr = listType(typ.List, out)
	case *irt.Field_PrimitiveMap:
		typeStr = primMapType(typ.PrimitiveMap, &fieldTag, &comment, out)
	case *irt.Field_ListMap:
		optOrRep = "rep"
		typeStr = listMapType(typ.ListMap, &fieldTag, &comment, out)
	default:
		panic(fmt.Sprintf("unreachable: unknown field type %T", typ))
	}

	if field.ProtoTag != 0 {
		fieldTag.proto = tag{protoType, fmt.Sprintf("%d", field.ProtoTag), }
		if optOrRep != "" {
			fieldTag.proto = append(fieldTag.proto, optOrRep)
		}
		fieldTag.proto = append(fieldTag.proto, "name="+field.Name)
	}

	out.Field(fieldName, typeStr, fieldTag, comment)
}

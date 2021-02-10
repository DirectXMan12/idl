// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package typecheck

import (
	"context"
	"fmt"

	irt "k8s.io/idl/ckdl-ir/goir/types"
	irgv "k8s.io/idl/ckdl-ir/goir/groupver"
	ire "k8s.io/idl/ckdl-ir/goir"
	"k8s.io/idl/kdlc/parser/trace"
)

type Requester interface {
	Load(ctx context.Context, path string) *ire.Partial
}

type GroupVersion struct {
	Group, Version string
}
func (gv GroupVersion) WithName(name string) Name {
	return Name{
		GroupVersion: gv,
		FullName: name,
	}
}
func GVFromRef(gv *irt.GroupVersionRef) GroupVersion {
	return GroupVersion{
		Group: gv.Group,
		Version: gv.Version,
	}
}
func GVFromDesc(gv *irgv.GroupVersion) GroupVersion {
	return GroupVersion{
		Group: gv.Group,
		Version: gv.Version,
	}
}
type Name struct {
	GroupVersion
	FullName string
}

func NameFromRef(ref *irt.Reference) Name {
	return GVFromRef(ref.GroupVersion).WithName(ref.Name)
}

// NB: we split subtype out into wrapper vs
// struct/union/enum b/c several checks
// care about struct, but can ignore all other
// variants of subtypes, so the need for a double
// type assertion (*grumbles about lack of ADTs and
// and pattern matching*)

type Terminal interface { isTerm() }
type TerminalWrapper struct {
	Wrapper *irt.Subtype
}
func (TerminalWrapper) isTerm() {}
type TerminalStruct struct {
	Struct *irt.Struct
}
func (TerminalStruct) isTerm() {}
type TerminalUnion struct {
	Union *irt.Union
}
func (TerminalUnion) isTerm() {}
type TerminalEnum struct {
	Enum *irt.Enum
}
func (TerminalEnum) isTerm() {}
type TerminalKind struct {
	Kind *irt.Kind
}
func (TerminalKind) isTerm() {}

// Node contains information for a given file (partial)
type Node struct {
	Partial *ire.Partial
	Path string

	References map[Name]Name
	Terminals map[Name]Terminal
}
func (n *Node) AddTerminal(ctx context.Context, from Name, term Terminal) {
	if existing, refExists := n.References[from]; refExists {
		ctx = trace.Note(ctx, "originally to", existing)
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	if _, termExists := n.Terminals[from]; termExists {
		// TODO: note node like above?
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	n.Terminals[from] = term
}
func (n *Node) AddReference(ctx context.Context, from, to Name) {
	if existing, refExists := n.References[from]; refExists {
		ctx = trace.Note(ctx, "originally to", existing)
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	if _, termExists := n.Terminals[from]; termExists {
		// TODO: note node like above?
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	n.References[from] = to
}

type MergedNode struct {
	Sources []*Node
	References map[Name]Name
	Terminals map[Name]Terminal
}
func (n *MergedNode) AddTerminal(ctx context.Context, from Name, term Terminal) {
	if existing, refExists := n.References[from]; refExists {
		ctx = trace.Note(ctx, "originally to", existing)
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	if _, termExists := n.Terminals[from]; termExists {
		// TODO: note node like above?
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	n.Terminals[from] = term
}
func (n *MergedNode) AddReference(ctx context.Context, from, to Name) {
	if existing, refExists := n.References[from]; refExists {
		ctx = trace.Note(ctx, "originally to", existing)
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	if _, termExists := n.Terminals[from]; termExists {
		// TODO: note node like above?
		trace.ErrorAt(ctx, "type with this name already exists")
	}
	n.References[from] = to
}

type Graph struct {
	// Imports is used to request the contents of referenced files.
	Imports Requester

	// PathToNode maps an import path to the corresponding graph node
	PathToNode map[string]*Node

	// GVToNode maps a group-version to the virtual (marged) node that corresponds
	// to it.  This could involve multiple nodes.
	GVToNode map[GroupVersion]*MergedNode
}

func NewGraph(imports Requester) *Graph {
	return &Graph{
		Imports: imports,
		PathToNode: make(map[string]*Node),
		GVToNode: make(map[GroupVersion]*MergedNode),
	}
}

func (g *Graph) avoidCycle(ctx context.Context, path string) {
	g.PathToNode[path] = nil // avoid cycles by explicitly adding a placeholder
}
func (g *Graph) hasCycle(ctx context.Context, path string) bool {
	node, exists := g.PathToNode[path]
	return exists && node == nil // cycle if we have an explicit placeholder
}

func (g *Graph) require(ctx context.Context, path string) {
	if g.hasCycle(ctx, path) {
		trace.ErrorAt(ctx, "import cycle detected")
		return
	}
	if _, exists := g.PathToNode[path]; exists {
		// already processed
		return
	}
	partial := g.Imports.Load(ctx, path)
	g.AddFile(ctx, path, partial)
}

func (g *Graph) buildNode(ctx context.Context, path string, partial *ire.Partial) *Node {
	node := Node{
		Partial: partial,
		Path: path,
		Terminals: make(map[Name]Terminal),
		References: make(map[Name]Name),
	}
	for _, irGV := range partial.GroupVersions {
		gv := GVFromDesc(irGV.Description)
		// first, record all the kind terminals
		for _, kind := range irGV.Kinds {
			node.AddTerminal(ctx, gv.WithName(kind.Name), TerminalKind{kind})
		}

		// then, record all the subtype terminals & references
		for _, subtype := range irGV.Types {
			stName := gv.WithName(subtype.Name)
			switch body := subtype.Type.(type) {
			case *irt.Subtype_ReferenceAlias:
				ref := body.ReferenceAlias
				refName := NameFromRef(ref)
				node.AddReference(ctx, stName, refName)
			case *irt.Subtype_PrimitiveAlias:
				node.AddTerminal(ctx, stName, TerminalWrapper{subtype})
			case *irt.Subtype_Union:
				node.AddTerminal(ctx, stName, TerminalUnion{body.Union})
			case *irt.Subtype_Struct:
				node.AddTerminal(ctx, stName, TerminalStruct{body.Struct})
			case *irt.Subtype_Enum:
				node.AddTerminal(ctx, stName, TerminalEnum{body.Enum})
			case *irt.Subtype_Set:
				node.AddTerminal(ctx, stName, TerminalWrapper{subtype})
			case *irt.Subtype_List:
				node.AddTerminal(ctx, stName, TerminalWrapper{subtype})
			case *irt.Subtype_PrimitiveMap:
				node.AddTerminal(ctx, stName, TerminalWrapper{subtype})
			case *irt.Subtype_ListMap:
				node.AddTerminal(ctx, stName, TerminalWrapper{subtype})
			default:
				panic(fmt.Sprintf("unreachable: unknown subtype type %T", body))
			}
		}
	}

	return &node
}

func (g *Graph) saveNode(ctx context.Context, node *Node, path string) {
	// node might be nil from cycle check
	if node, exists := g.PathToNode[path]; exists && node != nil {
		// verify, just in case
		panic(fmt.Sprintf("invalid: duplicate typecheck graph node for %q", path))
	}
	g.PathToNode[path] = node
}

func (g *Graph) AddFile(ctx context.Context, path string, partial *ire.Partial) {
	ctx = trace.Note(trace.Describe(ctx, "file"), "path", path)
	g.avoidCycle(ctx, path)

	// first, make sure all dependencies are present and checked
	for _, dep := range partial.Dependencies {
		depCtx := trace.Note(trace.Describe(ctx, "dependency"), "path", dep.From)
		depCtx = trace.Note(depCtx, "group-version", dep.GroupVersion)
		// TODO: context
		g.require(depCtx, dep.From)
	}

	// then, build ourself
	node := g.buildNode(ctx, path, partial)
	g.saveNode(ctx, node, path)
}

func (g *Graph) Contains(ctx context.Context, path string) bool {
	if g.hasCycle(ctx, path) {
		trace.ErrorAt(ctx, "import cycle detected")
		return true
	}
	_, exists := g.PathToNode[path]
	return exists
}

func (g *Graph) PartialFor(ctx context.Context, path string) *ire.Partial {
	node, exists := g.PathToNode[path]
	if !exists {
		trace.ErrorAt(ctx, "no IR for path")
		return nil
	}
	return node.Partial
}

func (g *Graph) BundleFor(ctx context.Context, paths ...string) *ire.Bundle {
	// TODO: only output stuff needed by paths
	bundle := &ire.Bundle{}
	for path, node := range g.PathToNode {
		bundle.VirtualFiles = append(bundle.VirtualFiles, &ire.Bundle_File{
			Contents: node.Partial,
			Name: path,
		})
	}
	return bundle
}

// MergeNodes maps all known nodes to the corresponding GVs, merging
// nodes from GVs spread across multiple partials and checking
// for duplicate names.
func (g *Graph) MergeNodes(ctx context.Context) {
	for _, node := range g.PathToNode {
		// TODO: context
		for _, irGV := range node.Partial.GroupVersions {
			gv := GVFromDesc(irGV.Description)
			merged, exists := g.GVToNode[gv]
			if !exists {
				merged = &MergedNode{
					Sources: []*Node{node},
					// in the common case, just copy over the maps --
					// we'll duplicate below if we need to, otherwise
					// avoid the extra operations
					References: node.References,
					Terminals: node.Terminals,
				}
				g.GVToNode[gv] = merged
				continue
			} else if len(merged.Sources) == 1 {
				// this would be the second node, so copy the
				// maps over to avoid mutating the source
				newRefs, newTerms := make(map[Name]Name), make(map[Name]Terminal)
				for from, to := range merged.References {
					newRefs[from] = to
				}
				for from, term := range merged.Terminals {
					newTerms[from] = term
				}
				merged.References = newRefs
				merged.Terminals = newTerms
			}

			for from, to := range node.References {
				merged.AddReference(ctx, from, to)
			}
			for from, term := range node.Terminals {
				merged.AddTerminal(ctx, from, term)
			}
			merged.Sources = append(merged.Sources, node)
		}
	}
}

func (g *Graph) TerminalFor(ctx context.Context, name Name) Terminal {
	ctx = trace.Describe(ctx, "finding terminal for reference")
	ctx = trace.Note(ctx, "original", name)
	// TODO: context
	node, exists := g.GVToNode[name.GroupVersion]
	if !exists {
		trace.ErrorAt(ctx, "reference to unkown group-version")
		return nil
	}
	// TODO: record intermediate terminals
	dest := name
	for next, hasNext := node.References[dest]; hasNext; next, hasNext = node.References[dest] {
		ctx = trace.Describe(ctx, "via reference")
		ctx = trace.Note(ctx, "via", next)
		dest = next
	}

	term, exists := node.Terminals[dest]
	if !exists {
		trace.ErrorAt(ctx, "reference to unknown type")
		return nil
	}
	return term
}

func (g *Graph) CheckReferences(ctx context.Context, check RefCheck) {
	// first, check the fields
	g.CheckFields(ctx, func(ctx context.Context, g Graphish, field *irt.Field, source interface{}) {
		switch typ := field.Type.(type) {
		case *irt.Field_Primitive: // nothing
		case *irt.Field_NamedType:
			check(ctx, g, typ.NamedType)
		case *irt.Field_Set:
			if itemsRef, isRef := typ.Set.Items.(*irt.Set_Reference); isRef {
				check(ctx, g, itemsRef.Reference)
			}
		case *irt.Field_List:
			if itemsRef, isRef := typ.List.Items.(*irt.List_Reference); isRef {
				check(ctx, g, itemsRef.Reference)
			}
		case *irt.Field_PrimitiveMap:
			primMap := typ.PrimitiveMap
			if itemsRef, isRef := primMap.Key.(*irt.PrimitiveMap_ReferenceKey); isRef {
				check(ctx, g, itemsRef.ReferenceKey)
			}
			if itemsRef, isRef := primMap.Value.(*irt.PrimitiveMap_ReferenceValue); isRef {
				check(ctx, g, itemsRef.ReferenceValue)
			}
		case *irt.Field_ListMap:
			check(ctx, g, typ.ListMap.Items)
		default:
			panic(fmt.Sprintf("unreachable: unknown field type %T", typ))
		}
	})

	// then check the wrapper subtypes (kinds are handled by the field logic)
	for _, node := range g.PathToNode {
		for _, irGV := range node.Partial.GroupVersions {
			for _, subtype := range irGV.Types {
				switch typ := subtype.Type.(type) {
				case *irt.Subtype_PrimitiveAlias: // nothing
				case *irt.Subtype_ReferenceAlias:
					check(ctx, g, typ.ReferenceAlias)
				case *irt.Subtype_Union: // handled by the field logic
				case *irt.Subtype_Struct: // handled by the field logic
				case *irt.Subtype_Set:
					if itemsRef, isRef := typ.Set.Items.(*irt.Set_Reference); isRef {
						check(ctx, g, itemsRef.Reference)
					}
				case *irt.Subtype_List:
					if itemsRef, isRef := typ.List.Items.(*irt.List_Reference); isRef {
						check(ctx, g, itemsRef.Reference)
					}
				case *irt.Subtype_PrimitiveMap:
					primMap := typ.PrimitiveMap
					if itemsRef, isRef := primMap.Key.(*irt.PrimitiveMap_ReferenceKey); isRef {
						check(ctx, g, itemsRef.ReferenceKey)
					}
					if itemsRef, isRef := primMap.Value.(*irt.PrimitiveMap_ReferenceValue); isRef {
						check(ctx, g, itemsRef.ReferenceValue)
					}
				case *irt.Subtype_ListMap:
					check(ctx, g, typ.ListMap.Items)
				case *irt.Subtype_Enum: // do nothing
				default:
					panic(fmt.Sprintf("unreachable: unknown field type %T", typ))
				}
			}
		}
	}
}

func (g *Graph) CheckSubtypes(ctx context.Context, check SubtypeCheck) {
	for _, node := range g.PathToNode {
		for _, irGV := range node.Partial.GroupVersions {
			for _, subtype := range irGV.Types {
				check(ctx, g, subtype)
			}
		}
	}
}

func (g *Graph) CheckFields(ctx context.Context, check FieldCheck) {
	for _, node := range g.PathToNode {
		for _, irGV := range node.Partial.GroupVersions {
			for _, kind := range irGV.Kinds {
				for _, field := range kind.Fields {
					check(ctx, g, field, kind)
				}
			}

			for _, subtype := range irGV.Types {
				switch typ := subtype.Type.(type) {
				case *irt.Subtype_Struct:
					for _, field := range typ.Struct.Fields {
						check(ctx, g, field, subtype)
					}
				case *irt.Subtype_Union:
					for _, field := range typ.Union.Variants {
						check(ctx, g, field, subtype)
					}
				// no other type has fields
				}
			}
		}
	}
}

type Graphish interface {
	TerminalFor(ctx context.Context, name Name) Terminal
}

type FieldCheck func(ctx context.Context, g Graphish, field *irt.Field, source interface{})
type RefCheck func(ctx context.Context, g Graphish, ref *irt.Reference)
type SubtypeCheck func(ctx context.Context, g Graphish, st *irt.Subtype)

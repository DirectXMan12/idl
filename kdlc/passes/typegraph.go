package passes

import (
	"context"
	"log"

	ir "k8s.io/idl/ckdl-ir/goir/types"

	"k8s.io/idl/kdlc/parser/ast"
	"k8s.io/idl/kdlc/parser/trace"
)

type typegraph struct {
	references map[ast.ResolvedNameInfo]ast.ResolvedNameInfo
	terminals map[ast.ResolvedNameInfo]ast.TerminalType
	externals map[ast.GroupVersionRef]*typegraph
}

func refToInfo(ref ir.Reference) ast.ResolvedNameInfo {
	return ast.ResolvedNameInfo{
		GroupVersion: ast.GroupVersionRef{
			Group: ref.GroupVersion.Group,
			Version: ref.GroupVersion.Version,
		},
		FullName: ref.Name,
	}
}

func (g *typegraph) terminalFor(ctx context.Context, orig ast.ResolvedNameInfo) ast.TerminalType {
	current := orig
	for next, hasNext := g.references[current]; hasNext; next, hasNext = g.references[current] {
		ctx = trace.Describe(ctx, "in reference")
		ctx = trace.Note(ctx, "from", current)
		ctx = trace.Note(ctx, "to", next)
		current = next
	}

	terminal, known := g.terminals[current]
	if !known {
		if ext, isExt := g.externals[current.GroupVersion]; isExt {
			return ext.terminalFor(ctx, current)
		}

		ctx = trace.Describe(ctx, "at terminal")
		ctx = trace.Note(ctx, "terminal", current)
		trace.ErrorAt(ctx, "reference to non-existant type")
		for name, typ := range g.terminals {
			log.Printf("known terminal %v -- %#v", name, typ)
		}
		return nil
	}
	return terminal
}

func (g *typegraph) EnterSubtype(ctx context.Context, st *ast.SubtypeDecl) (context.Context, TypeVisitor) {
	switch body := st.Body.(type) {
	case *ast.Newtype:
		ref, isRef := body.ResolvedType.Type.(ast.RefType)
		if isRef {
			g.references[*st.ResolvedName] = refToInfo(ir.Reference(ref))
		} else {
			g.terminals[*st.ResolvedName] = ast.TerminalAlias{Info: body.ResolvedType}
		}
	case *ast.Struct:
		g.terminals[*st.ResolvedName] = ast.TerminalStruct{
			Struct: body,
		}
	case *ast.Union:
		g.terminals[*st.ResolvedName] = ast.TerminalUnion{
			Union: body,
		}
	case *ast.Enum:
		g.terminals[*st.ResolvedName] = ast.TerminalEnum{}
	}
	return ctx, g
}
func (g *typegraph) EnterKind(ctx context.Context, kind *ast.KindDecl) (context.Context, TypeVisitor) {
	g.terminals[*kind.ResolvedName] = ast.TerminalKind{Kind: kind}
	return ctx, g
}

type resolver struct {
	graph *typegraph
}
func validationForPrim(prim ir.Primitive_Type) ast.ValidationType {
	switch prim {
	case ir.Primitive_STRING, ir.Primitive_BYTES:
		return ast.StringValidation
	case ir.Primitive_LEGACYINT32, ir.Primitive_INT64, ir.Primitive_LEGACYFLOAT64:
		return ast.NumberValidation
	default:
		return ast.NoValidation
	// nothing else gets validation at the moment
	// (but quantity really should -- need to bug upstream)
	}
}
func (r *resolver) validationForInfo(ctx context.Context, info *ast.ResolvedTypeInfo) ast.ValidationType {
	switch typ := info.Type.(type) {
	case ast.PrimitiveType:
		return validationForPrim(ir.Primitive_Type(typ))
	case ast.RefType:
		term := r.graph.terminalFor(ctx, ast.ResolvedNameInfo{
			GroupVersion: ast.GroupVersionRef{
				Group: typ.GroupVersion.Group,
				Version: typ.GroupVersion.Version,
			},
			FullName: typ.Name,
		})
		if term == nil {
			return ast.NoValidation
		}
		switch term := term.(type) {
		case ast.TerminalAlias:
			return r.validationForInfo(ctx, term.Info)
		case ast.TerminalStruct:
			return ast.ObjectishValidation
		case ast.TerminalUnion:
			return ast.ObjectishValidation
		case ast.TerminalEnum:
			return ast.NoValidation
		case ast.TerminalKind:
			// TODO(directxman12): should this be allowed?
			return ast.ObjectishValidation
		default:
			panic("unreachable: unknown terminal type")
		}
	case ast.ListType:
		return ast.ListValidation
	case ast.SetType:
		return ast.ListValidation
	case ast.ListMapType:
		return ast.ListValidation
	case ast.PrimitiveMapType:
		return ast.ObjectishValidation
	default:
		panic("unreachable: unknown resolved type")
	}
}

func (r *resolver) resolveValidation(ctx context.Context, info *ast.ResolvedTypeInfo) {
	if info.Validates == nil {
		return
	}
	info.Validates.ExpectedType = r.validationForInfo(ctx, info)
}
func (r *resolver) EnterSubtype(ctx context.Context, st *ast.SubtypeDecl) (context.Context, TypeVisitor) {
	switch body := st.Body.(type) {
	case *ast.Newtype:
		r.resolveValidation(ctx, body.ResolvedType)
	case *ast.Struct:
		for _, field := range body.Fields {
			r.resolveValidation(ctx, field.ResolvedType)
		}
	case *ast.Union:
		for _, field := range body.Variants {
			r.resolveValidation(ctx, field.ResolvedType)
		}
	case *ast.Enum:
		// nothing to do here -- no fields, etc
	default:
		panic("unreachable: unknown subtype body type")
	}
	return ctx, r
}
func (r *resolver) EnterKind(ctx context.Context, kind *ast.KindDecl) (context.Context, TypeVisitor) {
	for _, field := range kind.Fields {
		r.resolveValidation(ctx, field.ResolvedType)
	}
	return ctx, r
}

type checker struct {
	graph *typegraph
}

// check that:
// - validation matches expected type
func (c *checker) validValidation(ctx context.Context, info *ast.ValidatesInfo) {
	if info == nil {
		return
	}
	// TODO: trace this back to the specific validation
	switch info.ExpectedType {
	case ast.NoValidation:
		if info.Number != nil || info.String != nil || info.List != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "cannot have any validation for this type")
		}
	case ast.NumberValidation:
		if info.String != nil || info.List != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "can only have numeric validation for this type")
		}
	case ast.StringValidation:
		if info.Number != nil || info.List != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "can only have string validation for this type")
		}
	case ast.ListValidation:
		if info.Number != nil || info.String != nil || info.Objectish != nil {
			trace.ErrorAt(ctx, "can only have list validation for this type")
		}
	case ast.ObjectishValidation:
		if info.Number != nil || info.String != nil || info.List != nil {
			trace.ErrorAt(ctx, "can only have object-ish validation for this type")
		}
	default:
		panic("unreachable: unknown expected validation type")
	}
}

// - list-map items are references to object-ish things
// - list-map keys are valid fields (TODO: check field paths all at once)
func (c *checker) validListMap(ctx context.Context, listMap ir.ListMap) {
	// TODO: trace this back to specific key or value

	// grab the value (we'll error out in terminalFor if it can't be found)
	term := c.graph.terminalFor(ctx, refToInfo(*listMap.Items))
	if term == nil {
		return
	}

	// check the type and the corresponding key behavior
	switch term := term.(type) {
	case ast.TerminalUnion:
		union := term.Union
		if union.Untagged || len(listMap.KeyField) != 1 || listMap.KeyField[0] != union.Tag {
			trace.ErrorAt(ctx, "for unions to be used as list-map items, the key must be the union's tag")
		}
		return
	case ast.TerminalStruct:
		// TODO: make sure listMap key field is defaulted somewhere
	KeyLoop:
		for _, key := range listMap.KeyField {
			ctx := trace.Describe(ctx, "key")
			ctx = trace.Note(ctx, "name", key)
			for _, field := range term.Struct.Fields {
				if field.Name.Name == key {
					// TODO: does the key field have to be valid string?
					// TODO: check that type
					continue KeyLoop
				}
			}
			trace.ErrorAt(ctx, "key of list-map not present in item")
		}
	case ast.TerminalKind:
		trace.ErrorAt(ctx, "kinds may not be list-map items")
	case ast.TerminalAlias:
		trace.ErrorAt(ctx, "wrapper types may not be list-map items, unless they wrap a struct or union")
	case ast.TerminalEnum:
		trace.ErrorAt(ctx, "enum types may not be list-map items (try a set instead)")
	default:
		panic("unreachable: unknown terminal type")
	}
}
// - primitive-map keys are string-ish, primitive-map reference values are strings or appropriate lists
func (c *checker) validSimpleMap(ctx context.Context, primMap ir.PrimitiveMap) {
	// TODO: implement this
}
// - default matches actual type
func (c *checker) validDefault(ctx context.Context, info *ast.ResolvedTypeInfo) {
	// TODO: implement this
}
// - union variants are valid, no validation, etc
func (c *checker) validUnion(ctx context.Context, union *ast.Union) {
	// TODO: implement this
}

func (c *checker) validResolvedType(ctx context.Context, info *ast.ResolvedTypeInfo) {
	switch typ := info.Type.(type) {
	case ast.ListMapType:
		c.validListMap(ctx, ir.ListMap(typ))
	case ast.PrimitiveMapType:
		c.validSimpleMap(ctx, ir.PrimitiveMap(typ))
	default:
		// nothing specific to check for anything else
	}
}

func (c *checker) EnterSubtype(ctx context.Context, st *ast.SubtypeDecl) (context.Context, TypeVisitor) {
	ctx = ast.In(ctx, st)
	// TODO: spans
	switch body := st.Body.(type) {
	case *ast.Newtype:
		c.validResolvedType(ctx, body.ResolvedType)
	case *ast.Struct:
		for _, field := range body.Fields {
			ctx := ast.In(ctx, field)
			c.validValidation(ctx, field.ResolvedType.Validates)
			c.validResolvedType(ctx, field.ResolvedType)
		}
	case *ast.Union:
		c.validUnion(ctx, body)
	case *ast.Enum:
		// nothing to validate
	default:
		panic("unreachable: unknown subtype body")
	}
	return ctx, c
}
func (c *checker) EnterKind(ctx context.Context, kind *ast.KindDecl) (context.Context, TypeVisitor) {
	ctx = ast.In(ctx, kind)
	// TODO: spans
	for _, field := range kind.Fields {
		ctx := ast.In(ctx, field)
		c.validValidation(ctx, field.ResolvedType.Validates)
		c.validResolvedType(ctx, field.ResolvedType)
		c.validDefault(ctx, field.ResolvedType)
	}
	return ctx, c
}

type resolvedBuilder struct {
	graph *typegraph
}
func (b *resolvedBuilder) EnterKind(ctx context.Context, kind *ast.KindDecl) (context.Context, TypeVisitor) {
	b.graph.terminals[*kind.ResolvedName] = ast.TerminalKind{Kind: kind}
	return ctx, b
}
func (b *resolvedBuilder) EnterSubtype(ctx context.Context, st *ast.SubtypeDecl) (context.Context, TypeVisitor) {
	switch body := st.Body.(type) {
	case *ast.Newtype:
		if body.ResolvedType.Terminal != nil {
			b.graph.terminals[*st.ResolvedName] = body.ResolvedType.Terminal
		} else {
			b.graph.terminals[*st.ResolvedName] = ast.TerminalAlias{Info: body.ResolvedType}
		}
	case *ast.Struct:
		b.graph.terminals[*st.ResolvedName] = ast.TerminalStruct{
			Struct: body,
		}
	case *ast.Union:
		b.graph.terminals[*st.ResolvedName] = ast.TerminalUnion{
			Union: body,
		}
	case *ast.Enum:
		b.graph.terminals[*st.ResolvedName] = ast.TerminalEnum{}
	}
	return ctx, b
}

func graphFromResolved(ctx context.Context, input *ast.DepSet) *typegraph {
	graph := &typegraph{
		references: make(map[ast.ResolvedNameInfo]ast.ResolvedNameInfo),
		terminals: make(map[ast.ResolvedNameInfo]ast.TerminalType),
		externals: make(map[ast.GroupVersionRef]*typegraph),
	}
	for gv, dep := range input.Deps {
		graph.externals[gv] = graphFromResolved(ctx, dep)
	}


	return graph
}

func TypeCheck(ctx context.Context, input *ast.DepSet) {
	file := &input.Main

	// first, build up the typegraph
	graph := &typegraph{
		references: make(map[ast.ResolvedNameInfo]ast.ResolvedNameInfo),
		terminals: make(map[ast.ResolvedNameInfo]ast.TerminalType),
		externals: make(map[ast.GroupVersionRef]*typegraph),
	}
	for gv, dep := range input.Deps {
		graph.externals[gv] = graphFromResolved(ctx, dep)
	}

	for i := range file.GroupVersions {
		gv := &file.GroupVersions[i]
		VisitGroupVersion(ctx, graph, gv)
	}

	// the resolve validation expected types & terminal type
	for i := range file.GroupVersions {
		gv := &file.GroupVersions[i]
		VisitGroupVersion(ctx, &resolver{graph: graph}, gv)
	}

	// then, finally, check the types of various things
	for i := range file.GroupVersions {
		gv := &file.GroupVersions[i]
		VisitGroupVersion(ctx, &checker{graph: graph}, gv)
	}
}

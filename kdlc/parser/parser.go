// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package parser

import (
	"fmt"
	"context"
	"strings"
	"strconv"
	"unicode/utf8"
	"text/scanner"

	"k8s.io/idl/kdlc/lexer"
	"k8s.io/idl/kdlc/parser/ast"
	. "k8s.io/idl/kdlc/parser/trace"
)

type Parser struct {
	lex *lexer.Lexer
	nextTok lexer.Token

	Error func(context.Context, Span)
}

func New(input *lexer.Lexer) *Parser {
	p := &Parser{
		lex: input,
		Error: func(ctx context.Context, loc Span) {
			ErrorAtSpan(ctx, loc)
		},
	}
	input.Error = func(ctx context.Context, at lexer.Position, unexpected rune, notes ...string) {
		ctx = Describe(ctx, "scanner")
		ctx = Note(ctx, "expected", notes)
		ctx = Note(ctx, "unexpected", scanner.TokenString(unexpected))
		pos := TokenPosition{Start: at, End: at}
		if unexpected >= 0 { // not EOF
			rnLen := utf8.RuneLen(unexpected)
			pos.End.Offset += rnLen
			pos.End.Column += rnLen
		}
		p.Error(ctx, Span{Start: pos, End: pos})
	}
	p.next(context.Background())
	return p
}

func (p *Parser) markErr(ctx context.Context, token lexer.Token) {
	p.Error(Note(ctx, "token", token.Type), ast.TokenSpan(token))
}
func (p *Parser) markErrExp(ctx context.Context, token lexer.Token, exp ...rune) {
	ctx = Note(ctx, "found token", token.Type)
	ctx = Note(ctx, "expected token", exp)
	p.Error(ctx, ast.TokenSpan(token))
	p.next(ctx) // always make progress
}
func (p *Parser) markErrAt(ctx context.Context, span Span) {
	p.Error(ctx, span)
}

func (p *Parser) next(ctx context.Context) lexer.Token {
	existing := p.nextTok
	var tok lexer.Token
	for tok = p.lex.Next(ctx); tok.Type == lexer.Comment; tok = p.lex.Next(ctx) {
	}
	p.nextTok = tok
	return existing
}
func (p *Parser) peek() lexer.Token {
	return p.nextTok
}

func (p *Parser) expect(ctx context.Context, typ rune) lexer.Token {
	tok := p.peek()
	if tok.Type == typ {
		return p.next(ctx)
	}
	p.markErrExp(ctx, tok, typ)
	return lexer.Token{Type: lexer.Unexpected} // use an invalid start/end
}

func (p *Parser) expectOrRecover(ctx context.Context, typ rune) lexer.Token {
	tok := p.peek()
	if tok.Type == typ {
		return p.next(ctx)
	}

	// otherwise, error & try to recover on the principle that we scan till we
	// find the expected symbol (we could probably get better by adapting for
	// specific cases, but this is fine for now).
	p.markErrExp(ctx, tok, typ)

	return p.recoverTill(ctx, typ)
}

func (p *Parser) recoverTillDeclEnd(ctx context.Context) lexer.Token {
	var tok lexer.Token
	blockCnt := 0
SkipLoop:
	for tok := p.next(ctx); tok.Type != lexer.EOF; tok = p.next(ctx) {
		switch {
		case blockCnt == 0 && tok.Type == ';':
			break SkipLoop
		case blockCnt == 1 && tok.Type == '}':
			break SkipLoop
		case blockCnt > 1 && tok.Type == '}':
			blockCnt--
		case tok.Type == '{':
			blockCnt++
		}
	}
	if tok.Type == lexer.EOF {
		p.markErr(Describe(ctx, "unterminated declaration"), tok)
	}
	return tok
}

func (p *Parser) recoverTill(ctx context.Context, typ rune) lexer.Token {
	var tok lexer.Token
	for tok := p.next(ctx); tok.Type != lexer.EOF && tok.Type != typ; tok = p.next(ctx) {}
	if tok.Type == lexer.EOF {
		p.markErr(Note(ctx, "unterminated block missing", typ), tok)
	}
	return tok
}

func (p *Parser) tokenText() string {
	return p.lex.TokenText()
}

func (p *Parser) until(term rune, body func()) {
	for tok := p.peek(); tok.Type != term && tok.Type != lexer.EOF; tok = p.peek() {
		body()
	}
}
func (p *Parser) untilEither(term1, term2 rune, body func()) {
	for tok := p.peek(); tok.Type != term1 && tok.Type != term2 && tok.Type != lexer.EOF; tok = p.peek() {
		body()
	}
}

func (p *Parser) expectWithText(ctx context.Context, typ rune) (string, lexer.Token) {
	tok := p.peek()
	if tok.Type == typ {
		return p.tokenText(), p.next(ctx)
	}
	p.markErrExp(ctx, tok, typ)
	return "", lexer.Token{Type: lexer.Unexpected} // use an invalid start/end
}

func (p *Parser) parseKey(ctx context.Context) (string, lexer.Token) {
	tok := p.peek()
	switch tok.Type {
	case lexer.FieldIdentOrKey:
		return p.tokenText(), p.next(ctx)
	case lexer.DefinitelyKey:
		return p.tokenText(), p.next(ctx)
	default:
		p.markErrExp(ctx, tok, lexer.DefinitelyKey)
		return "", lexer.Token{Type: lexer.Unexpected} // use an invalid start/end
	}
}

func (p *Parser) parseString(ctx context.Context) (string, lexer.Token) {
	ctx = Describe(ctx, "string")
	raw, tok := p.expectWithText(ctx, lexer.String)

	// TODO: more specific location info
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		p.markErr(Note(ctx, "error", "strings must begin and end with `\"`"), tok)
		return "", tok
	}

	res := make([]byte, 0, len(raw)-2)
	raw = raw[1:len(raw)-1]
	current := 0
Loop:
	for current < len(raw) {
		nextSlash := strings.IndexByte(raw[current:], '\\')
		if nextSlash == -1 {
			chunk := raw[current:]
			res = append(res, chunk...)
			break
		}
		nextSlash += current
		chunk := raw[current:nextSlash]
		res = append(res, chunk...)

		// the lexer should've taken care of this, but just in case
		if nextSlash+1 >= len(raw) {
			panic("bad lexer output: slash without following character")
		}

		switch chr := raw[nextSlash+1]; chr {
		case '"', '\\', '/':
			res = append(res, chr)
		case 'b':
			res = append(res, '\b')
		case 'f':
			res = append(res, '\f')
		case 'n':
			res = append(res, '\n')
		case 'r':
			res = append(res, '\r')
		case 't':
			res = append(res, '\t')
		case 'u':
			// the lexer should've taken care of this, but just in case
			if nextSlash+5 >= len(raw) {
				panic("bad lexer output: slash without following character")
			}
			digits := string(raw[nextSlash+2:nextSlash+6])
			num, err := strconv.ParseUint(digits, 16, 32)
			// the lexer should've taken care of this, but just in case
			if err != nil {
				panic(fmt.Sprintf("bad hex digits %q: %v", digits, err))
			}
			var enc [utf8.UTFMax]byte
			encLen := utf8.EncodeRune(enc[:], rune(num))
			res = append(res, enc[:encLen]...)
		default: // the lexer should've taken care of this, but just in case
			p.markErr(Note(ctx, "bad escape", string(chr)), tok)
			break Loop
		}
	}

	return string(res), tok
}
func (p *Parser) parseValue(ctx context.Context) ast.Value {
	ctx = Describe(ctx, "value")
	start := p.peek()
	// TODO: wrap all these in
	switch start.Type {
	case lexer.String:
		str, tok := p.parseString(ctx)
		return ast.StringVal{Value: str, Span: ast.TokenSpan(tok)}
	case lexer.Number:
		text, tok := p.expectWithText(ctx, lexer.Number)
		num, err := strconv.Atoi(text)
		if err != nil {
			p.markErr(Note(ctx, "error", err), tok)
			return nil
		}
		return ast.NumVal{Value: num, Span: ast.TokenSpan(tok)}
	case lexer.KWTrue:
		tok := p.next(ctx)
		return ast.BoolVal{Value: true, Span: ast.TokenSpan(tok)}
	case lexer.KWFalse:
		tok := p.next(ctx)
		return ast.BoolVal{Value: false, Span: ast.TokenSpan(tok)}
	case '{': // struct
		res := ast.StructVal{}
		listCtx := BeginSpan(ctx, p.expect(ctx, '{'))

		p.until('}', func() {
			key, keyTok := p.parseKey(Describe(listCtx, "struct key"))
			kvCtx := BeginSpan(listCtx, keyTok)

			p.expect(kvCtx, ':')

			val := p.parseValue(Describe(kvCtx, "struct value"))

			res.KeyValues = append(res.KeyValues, ast.KeyValue{
				Key: ast.IdentFrom(key, keyTok),
				Value: val,
				Span: EndSpanAt(kvCtx, val.SpanEnd()),
			})
		})

		res.Span = EndSpan(listCtx, p.expect(Describe(listCtx, "list end"), ']'))
		return res
	case '[': // list
		res := ast.ListVal{}
		listCtx := BeginSpan(ctx, p.expect(ctx, '['))

		p.until(']', func() {
			itemCtx := Describe(listCtx, "list item")
			val := p.parseValue(itemCtx)
			res.Values = append(res.Values, val)
			if p.peek().Type != ']' {
				// trailing comma is optional
				p.expect(listCtx, ',')
			}
		})

		res.Span = EndSpan(listCtx, p.expect(Describe(listCtx, "list end"), ']'))
		return res
	case lexer.FieldPath:
		text, tok := p.expectWithText(ctx, lexer.FieldPath)
		return ast.FieldPathVal(ast.IdentFrom(text[1:] /* skip the dot */, tok))
	case lexer.TypeIdent:
		text, tok := p.expectWithText(ctx, lexer.TypeIdent)
		return ast.RefTypeVal(ast.RefModifier{
			Name: ast.IdentFrom(text, tok),
			Span: ast.TokenSpan(tok),
		})
	case lexer.FieldIdentOrKey: // a primitive or compound type
		text, tok := p.expectWithText(ctx, lexer.FieldIdentOrKey)
		if p.peek().Type == '(' {
			ctx = BeginSpan(ctx, tok)
			params := p.parseAnyParamList(Describe(ctx, "modifier parameters"))
			return ast.CompoundTypeVal(ast.KeyishModifier{
				Name: ast.IdentFrom(text, tok),
				Parameters: &params,
				Span: EndSpanAt(ctx, params.SpanEnd()),
			})
		}
		return ast.PrimitiveTypeVal(ast.IdentFrom(text, tok))
	case lexer.DefinitelyKey: // a primitive or compound type
		text, tok := p.expectWithText(ctx, lexer.DefinitelyKey)
		if p.peek().Type == '(' {
			ctx = BeginSpan(ctx, tok)
			params := p.parseAnyParamList(Describe(ctx, "modifier parameters"))
			return ast.CompoundTypeVal(ast.KeyishModifier{
				Name: ast.IdentFrom(text, tok),
				Parameters: &params,
				Span: EndSpanAt(ctx, params.SpanEnd()),
			})
		}
		return ast.PrimitiveTypeVal(ast.IdentFrom(text, tok))
	case lexer.QualPath:
		return ast.RefTypeVal(p.parseQualPath(ctx))
	case lexer.UnqualPath:
		return ast.RefTypeVal(p.parseUnqualPath(ctx))
	default:
		p.markErrExp(ctx, start, lexer.String, lexer.Number, lexer.KWTrue, lexer.KWFalse, '{', '[', lexer.FieldPath, lexer.TypeIdent, lexer.QualPath, lexer.UnqualPath)
		return nil
	}
}

func (p *Parser) parseMarkerImports(ctx context.Context) ast.MarkerImports {
	ctx = Describe(ctx, "marker imports")
	ctx = BeginSpan(ctx, p.expect(ctx, lexer.KWMarkers))

	p.expect(Describe(ctx, "start of markers import block"), '(')

	imports := make(map[string]ast.MarkerImport)
	p.until(')', func() {
		ctx := Describe(ctx, "marker import")

		alias, start := p.parseKey(Describe(ctx, "marker import alias"))
		ctx = BeginSpan(ctx, start)

		p.expect(ctx, lexer.KWFrom)

		path, _ := p.parseString(Describe(ctx, "marker import source"))

		span := EndSpan(ctx,
			p.expectOrRecover(Describe(ctx, "marker import end"), ';'))
		imports[alias] = ast.MarkerImport{
			Span: span,
			Alias: alias,
			Src: path,
		}

	})

	span := EndSpan(ctx,
		p.expectOrRecover(Describe(ctx, "end of markers import block"), ')'))
	return ast.MarkerImports{
		Span: span,
		Imports: imports,
	}
}

func (p *Parser) parseTypeImports(ctx context.Context) ast.TypeImports {
	ctx = Describe(ctx, "type imports")
	ctx = BeginSpan(ctx, p.expect(ctx, lexer.KWTypes))

	p.expect(Describe(ctx, "start of types import block"), '(')

	imports := make(map[ast.GroupVersionRef]ast.TypeImport)
	p.until(')', func() {
		ctx := Describe(ctx, "group-version list")
		ctx = BeginSpan(ctx, p.expect(Describe(ctx, "group-version list start"), '{'))

		var gvs []ast.GroupVersionRef
		p.until('}', func() {
			// TODO: spans for these?
			gvs = append(gvs, p.parseImportGV(ctx))
			if p.peek().Type != '}' {
				// optional trailing comma
				p.expect(ctx, ',')
			}
		})
		p.expect(Describe(ctx, "group-version list end"), '}')

		p.expect(ctx, lexer.KWFrom)

		path, _ := p.parseString(Describe(ctx, "types import source"))

		span := EndSpan(ctx, p.expectOrRecover(ctx, ';'))

		for _, gv := range gvs {
			imports[gv] = ast.TypeImport{
				Span: span,
				GroupVersion: gv,
				Src: path,
			}
		}
	})

	span := EndSpan(ctx,
		p.expectOrRecover(Describe(ctx, "end of types import block"), ')'))
	return ast.TypeImports{
		Span: span,
		Imports: imports,
	}
}

func (p *Parser) parseImportGV(ctx context.Context) ast.GroupVersionRef {
	ctx = Describe(ctx, "group-version import name")

	raw, _ := p.expectWithText(ctx, lexer.ImportName)
	// lexer makes sure the form is correct, so we can just split here

	slashParts := strings.SplitN(raw, "/", 2)

	group := slashParts[0]
	version := slashParts[1]

	return ast.GroupVersionRef{
		Group: group,
		Version: version,
	}
}

func (p *Parser) parseImports(ctx context.Context) ast.Imports {
	ctx = Describe(ctx, "import block")
	ctx = BeginSpan(ctx, p.expect(ctx, lexer.KWImport))

	switch p.peek().Type {
	case '(': // compound import
		p.expect(ctx, '(')
		types := p.parseTypeImports(ctx)
		markers := p.parseMarkerImports(ctx)
		span := EndSpan(ctx, p.expectOrRecover(Describe(ctx, "import block end"), ')'))
		return ast.Imports{
			Markers: &markers,
			Types: &types,
			Span: span,
		}
	case lexer.KWMarkers: // markers only
		markers := p.parseMarkerImports(ctx)
		span := EndSpanAt(ctx, markers.End)
		return ast.Imports{
			Span: span,
			Markers: &markers,
		}
	case lexer.KWTypes:  // types only
		types := p.parseTypeImports(ctx)
		span := EndSpanAt(ctx, types.End)
		return ast.Imports{
			Span: span,
			Types: &types,
		}
	default:
		p.markErrExp(Note(ctx, "expected token notes", []string{
			"for both import types",
			"for only markers",
			"for only types",
		}), p.peek(), '(', lexer.KWMarkers, lexer.KWTypes)
		return ast.Imports{
			Span: EndSpan(ctx, lexer.Token{Type: lexer.Unexpected}),
		}
	}
}

type Param interface{
	parse(ctx context.Context, p *Parser)
	name() string
	present() bool
}
type StringParam struct {
	Name string
	Value string
	set bool
}
func (s *StringParam) parse(ctx context.Context, p *Parser) {
	text, _ := p.parseString(ctx)
	s.Value = text
	s.set = true
}
func (s StringParam) name() string {
	return s.Name
}
func (s *StringParam) present() bool {
	return s.set
}
type BoolParam struct {
	Name string
	Value bool
	set bool
}
func (s *BoolParam) parse(ctx context.Context, p *Parser) {
	tok := p.peek()
	switch tok.Type {
	case lexer.KWTrue:
		s.Value = true
	case lexer.KWFalse:
		s.Value = false
	default:
		p.markErrExp(ctx, tok, lexer.KWTrue, lexer.KWFalse)
	}
	s.set = true
}
func (s BoolParam) name() string {
	return s.Name
}
func (s *BoolParam) present() bool {
	return s.set
}

func (p *Parser) parseParamList(ctx context.Context, defs ...Param) Span {
	ctx = Describe(ctx, "parameter list")
	ctx = BeginSpan(ctx, p.expect(Describe(ctx, "parameter list start"), '('))

	defsIdx := make(map[string]int)
	for i, def := range defs {
		defsIdx[def.name()] = i
	}

	p.until(')', func() {
		key, _ := p.parseKey(Describe(ctx, "parameter key"))
		ctx := Note(ctx, "name", key)
		p.expect(Describe(ctx, "between keys and values"), ':')
		defs[defsIdx[key]].parse(Describe(ctx, "parameter value"), p)
		if p.peek().Type != ')' {
			// don't require a trailing comma (but this'll still support it anyway)
			p.expectOrRecover(ctx, ',')
		}
	})

	return EndSpan(ctx, p.expectOrRecover(Describe(ctx, "parameter list end"), ')'))
}

func (p *Parser) parseAnyParamList(ctx context.Context) ast.ParameterList {
	ctx = Describe(ctx, "parameter list")
	ctx = BeginSpan(ctx, p.expect(Describe(ctx, "parameter list start"), '('))

	var keyVals []ast.KeyValue
	p.until(')', func() {
		key, tok := p.parseKey(Describe(ctx, "parameter key"))
		ctx := BeginSpan(ctx, tok)

		p.expect(Describe(ctx, "between keys and values"), ':')
		val := p.parseValue(Describe(ctx, "parameter value"))

		var span Span
		if p.peek().Type == ')' {
			// don't require a final comma
			span = EndSpanAt(ctx, val.SpanEnd())
		} else {
			// TODO: recover till comma *or* close paren
			span = EndSpan(ctx, p.expectOrRecover(ctx, ','))
		}
		keyVals = append(keyVals, ast.KeyValue{
			Key: ast.IdentFrom(key, tok),
			Value: val,
			Span: span,
		})
	})

	span := EndSpan(ctx, p.expectOrRecover(Describe(ctx, "parameter list end"), ')'))
	return ast.ParameterList{
		Params: keyVals,
		Span: span,
	}
}

func (p *Parser) requiredArgs(ctx context.Context, span Span, args ...Param) {
	for _, arg := range args {
		if !arg.present() {
			p.markErrAt(Note(ctx, "missing parameter", arg.name()), span)
		}
	}
}

func (p *Parser) maybeDocs(ctx context.Context) ast.Docs {
	ctx = Describe(ctx, "documentation")
	var sectionCtx context.Context
	if maybeDoc := p.peek(); maybeDoc.Type == lexer.Doc {
		// TODO: check if this
		ctx = BeginSpan(ctx, maybeDoc)
		sectionCtx = Describe(ctx, "doc section")
		sectionCtx = BeginSpan(Note(sectionCtx, "name", ""), maybeDoc)
	}
	var sections []ast.DocSection
	var lastSection ast.DocSection
	var lastEnd lexer.Token
	for maybeDoc := p.peek(); maybeDoc.Type == lexer.Doc; maybeDoc = p.peek() {
		raw, tok := p.expectWithText(sectionCtx, lexer.Doc)
		raw = raw[3:] // skip the slashes

		if len(raw) == 0 {
			lastSection.Lines = append(lastSection.Lines, "")
			lastEnd = tok
			continue
		}

		if raw[0] != ' ' {
			// TODO: more precise location
			p.markErr(Note(sectionCtx, "error", "doc comments must have a space after the slashes"), tok)
			lastEnd = tok
			continue
		}
		raw = raw[1:]
		if raw[0] == '#' { // section title
			// append the last section...
			lastSection.Span = EndSpan(sectionCtx, lastEnd)
			sections = append(sections, lastSection)

			// ...and reset
			sectionName := strings.TrimSpace(raw[1:])
			sectionCtx = Describe(ctx, "doc section")
			sectionCtx = BeginSpan(Note(sectionCtx, "name", sectionName), maybeDoc)
			lastSection = ast.DocSection{
				Title: sectionName,
			}
			lastEnd = tok
			continue
		}
		// TODO(directxman12): catch cases of `   # foo` that are probably accidents?
		lastSection.Lines = append(lastSection.Lines, raw)
		lastEnd = tok
	}
	if lastSection.Title != "" || len(lastSection.Lines) > 0 {
		lastSection.Span = EndSpan(sectionCtx, lastEnd)
		sections = append(sections, lastSection)
	}

	res := ast.Docs{
		Sections: sections,
	}

	if len(sections) > 0 {
		res.Span = EndSpanAt(ctx, sections[len(sections)-1].SpanEnd())
	}

	// TODO: make sure there's no comments between this and then start of the declaration!
	// (this'll avoid mistakes in documentation)

	return res
}
func (p *Parser) maybeMarkers(ctx context.Context) []ast.AbstractMarker {
	ctx = Describe(ctx, "markers")
	var markers []ast.AbstractMarker
	for maybeStart := p.peek(); maybeStart.Type == '@'; maybeStart = p.peek() {
		markerCtx := Describe(ctx, "marker")
		markerCtx = BeginSpan(markerCtx, p.expect(markerCtx, '@'))

		var marker ast.AbstractMarker
		switch p.peek().Type {
		case lexer.MarkerPath: // imported name
			name, tok := p.expectWithText(markerCtx, lexer.MarkerPath)
			marker.Name = ast.IdentFrom(name, tok)
			markerCtx = Note(markerCtx, "marker name", name)
		case lexer.FieldIdentOrKey: // standard name
			name, tok := p.expectWithText(markerCtx, lexer.FieldIdentOrKey)
			marker.Name = ast.IdentFrom(name, tok)
			markerCtx = Note(markerCtx, "marker name", name)
		case lexer.DefinitelyKey: // standard name
			name, tok := p.expectWithText(markerCtx, lexer.DefinitelyKey)
			marker.Name = ast.IdentFrom(name, tok)
			markerCtx = Note(markerCtx, "marker name", name)
		default:
			// TODO: recover here?
			p.markErrExp(markerCtx, p.peek(), lexer.MarkerPath, lexer.DefinitelyKey)
			continue
		}

		if p.peek().Type == '(' {
			params := p.parseAnyParamList(Describe(markerCtx, "marker parameters"))
			marker.Parameters = &params
			marker.Span = EndSpanAt(markerCtx, params.End)
		} else {
			marker.Span = EndSpanAt(markerCtx, marker.Name.End)
		}
		markers = append(markers, marker)
	}

	return markers
}

func (p *Parser) maybeDocsMarkers(ctx context.Context) (ast.Docs, []ast.AbstractMarker) {
	docs := p.maybeDocs(ctx)
	markers := p.maybeMarkers(ctx)
	// TODO: make sure there's no whitespace or distinct comments between
	// this and whatever's being described.
	// End of line comments after markers are fine though
	return docs, markers
}

func (p *Parser) parseGroupVersion(ctx context.Context) ast.GroupVersion {
	ctx = Describe(ctx, "group-version")
	docs, markers := p.maybeDocsMarkers(ctx)

	ctx = BeginSpan(ctx, p.expect(ctx, lexer.KWGroupVersion))

	group := StringParam{Name: "group"}
	version := StringParam{Name: "version"}
	argsCtx := Describe(ctx, "group-version parameters")
	p.requiredArgs(
		argsCtx,
		p.parseParamList(argsCtx, &group, &version),
		&group,
		&version,
	)
	ctx = Note(ctx, "group", group.Value)
	ctx = Note(ctx, "version", version.Value)

	p.expect(Describe(ctx, "group-version block start"), '{')

	var decls []ast.Decl
	p.until('}', func() {
		decls = append(decls, p.parseDecl(ctx))
	})

	span := EndSpan(ctx, p.expectOrRecover(Describe(ctx, "group-version block end"), '}'))
	return ast.GroupVersion{
		Group: group.Value,
		Version: version.Value,
		Span: span,
		Docs: docs,
		Markers: markers,

		Decls: decls,
	}
}

func (p *Parser) parseDecl(ctx context.Context) ast.Decl {
	ctx = Describe(ctx, "declaration")
	docs, markers := p.maybeDocsMarkers(ctx)

	declKeyword := p.peek()
	if declKeyword.Type == lexer.KWKind {
		ctx = Describe(ctx, "kind")
		decl := p.parseKindDeclRest(ctx)
		decl.Docs = docs
		decl.Markers = markers
		return &decl
	}

	// TODO: note in errors that we could be expecting a "kind" keyword too

	decl := p.parseSubtypeDeclRest(ctx)
	decl.Docs = docs
	decl.Markers = markers
	return &decl
}

func (p *Parser) parseSubtypeDeclRest(ctx context.Context) ast.SubtypeDecl {
	ctx = Describe(ctx, "subtype")
	declKeyword := p.peek()
	var typeAsStr string
	switch declKeyword.Type {
	case lexer.KWStruct:
		typeAsStr = "struct"
	case lexer.KWUnion:
		typeAsStr = "union"
	case lexer.KWEnum:
		typeAsStr = "enum"
	case lexer.KWNewType:
		typeAsStr = "newtype"
	default:
		p.markErrExp(ctx, declKeyword, lexer.KWStruct, lexer.KWEnum, lexer.KWUnion, lexer.KWNewType)
		// ... and recover
		p.recoverTillDeclEnd(ctx)
		return ast.SubtypeDecl{}
	}

	ctx = Note(ctx, "type", typeAsStr)
	ctx = BeginSpan(ctx, p.next(ctx))

	var unionTag string
	var unionUntagged bool
	switch declKeyword.Type {
	case lexer.KWUnion:
		unionTag = "type"
		if p.peek().Type == '(' {
			tag := StringParam{Name: "tag"}
			untagged := BoolParam{Name: "untagged"}
			p.parseParamList(Describe(ctx, "union params"), &tag, &untagged)

			if tag.present() {
				unionTag = tag.Value
			}
			if untagged.present() {
				unionUntagged = untagged.Value
			}
		}
	}

	name, nameTok := p.expectWithText(Describe(ctx, "subtype name"), lexer.TypeIdent)

	ctx = Note(ctx, "name", name)

	var body ast.SubtypeBody
	switch declKeyword.Type {
	case lexer.KWStruct:
		fields, subtypes, blockSpan := p.parseFieldBlock(ctx)
		body = &ast.Struct{
			Fields: fields,
			Subtypes: subtypes,
			Span: blockSpan,
		}
	case lexer.KWUnion:
		// unions parse like structs for now --
		// we sort out the differences when we resolve modifiers
		fields, subtypes, blockSpan := p.parseFieldBlock(ctx)
		body = &ast.Union{
			Variants: fields,
			Subtypes: subtypes,
			Span: blockSpan,
			Tag: unionTag,
			Untagged: unionUntagged,
		}
	case lexer.KWEnum:
		enum := p.parseEnumBlock(ctx)
		body = &enum
	case lexer.KWNewType:
		nt := p.parseNewtypeRest(ctx)
		body = &nt
	}

	return ast.SubtypeDecl{
		Name: ast.IdentFrom(name, nameTok),
		Body: body,
		Span: EndSpanAt(ctx, body.SpanEnd()),
	}
}

func (p *Parser) parseModifier(ctx context.Context) ast.Modifier {
	ctx = Describe(ctx, "type modifier")

	modStart := p.peek()
	switch modStart.Type {
	case lexer.FieldIdentOrKey:
		fallthrough
	case lexer.DefinitelyKey:
		key, tok := p.parseKey(ctx)
		ctx = Note(ctx, "modifier name", key)
		ctx = BeginSpan(ctx, tok)
		mod := ast.KeyishModifier{
			Name: ast.IdentFrom(key, tok),
		}
		if p.peek().Type == '(' {
			params := p.parseAnyParamList(Describe(ctx, "modifier parameters"))
			mod.Span = EndSpanAt(ctx, params.End)
			mod.Parameters = &params
		} else {
			mod.Span = EndSpan(ctx, tok)
		}
		return mod
	case lexer.TypeIdent:
		text, tok := p.expectWithText(ctx, lexer.TypeIdent)
		return ast.RefModifier{
			Name: ast.IdentFrom(text, tok),
			Span: ast.TokenSpan(tok),
		}
	case lexer.QualPath:
		return p.parseQualPath(ctx)
	case lexer.UnqualPath:
		return p.parseUnqualPath(ctx)
	default:
		p.markErrExp(Note(ctx, "expected token notes", []string{
			"(e.g. `optional`)",
			"(e.g. `Pod`)",
			"(e.g. `core/v1::Pod`)",
			"(e.g. `Pod::Spec`)",
		}),
		modStart, lexer.DefinitelyKey, lexer.TypeIdent, lexer.QualPath, lexer.UnqualPath)
		return nil
	}
}

func (p *Parser) parseQualPath(ctx context.Context) ast.RefModifier {
	raw, tok := p.expectWithText(ctx, lexer.QualPath)
	// lexer makes sure the form is correct, so we can just split here

	slashParts := strings.SplitN(raw, "/", 2)
	colonParts := strings.SplitN(slashParts[1], "::", 2)

	group := slashParts[0]
	version := colonParts[0]
	name := colonParts[1]

	return ast.RefModifier{
		GroupVersion: &ast.GroupVersionRef{
			Group: group,
			Version: version,
		},
		Name: ast.IdentFrom(name, tok),
		Span: ast.TokenSpan(tok),
	}
}
func (p *Parser) parseUnqualPath(ctx context.Context) ast.RefModifier {
	name, tok := p.expectWithText(ctx, lexer.UnqualPath)
	return ast.RefModifier{
		Name: ast.IdentFrom(name, tok),
		Span: ast.TokenSpan(tok),
	}
}

func (p *Parser) parseNewtypeRest(ctx context.Context) ast.Newtype {
	ctx = Describe(ctx, "newtype spec")
	ctx = BeginSpan(ctx, p.expect(ctx, ':'))
	var mods ast.ModifierList
	p.until(';', func() {
		mods = append(mods, p.parseModifier(ctx))
	})
	span := EndSpan(ctx, p.expectOrRecover(ctx, ';'))

	return ast.Newtype{
		Modifiers: mods,
		Span: span,
	}
}

func (p *Parser) parseKindDeclRest(ctx context.Context) ast.KindDecl {
	ctx = BeginSpan(ctx, p.expect(ctx, lexer.KWKind))

	name, nameTok := p.expectWithText(Describe(ctx, "kind name"), lexer.TypeIdent)
	ctx = Note(ctx, "name", name)

	fields, subtypes, blockSpan := p.parseFieldBlock(ctx)
	span := EndSpanAt(ctx, blockSpan.End)
	return ast.KindDecl{
		Name: ast.IdentFrom(name, nameTok),
		Fields: fields,
		Subtypes: subtypes,
		Span: span,
	}
}

func (p *Parser) parseFieldBlock(ctx context.Context) ([]ast.Field, []ast.SubtypeDecl, Span) {
	ctx = Describe(ctx, "field block")
	ctx = BeginSpan(ctx, p.expect(Describe(ctx, "field block start"), '{'))

	var fields []ast.Field
	var subtypes []ast.SubtypeDecl

	p.until('}', func() {
		docs, markers := p.maybeDocsMarkers(Describe(ctx, "field or subtype"))

		fieldOrKW := p.peek()
		switch fieldOrKW.Type {
		case lexer.FieldIdentOrKey:
			fallthrough
		case lexer.DefinitelyFieldIdent:
			field := p.parseField(ctx)
			field.Docs = docs
			field.Markers = markers
			fields = append(fields, field)
		default:
			// TODO: note in errors tht we could be expecting a field name too
			decl := p.parseSubtypeDeclRest(ctx)
			decl.Docs = docs
			decl.Markers = markers
			subtypes = append(subtypes, decl)
		}
	})

	span := EndSpan(ctx, p.expectOrRecover(Describe(ctx, "field block end"), '}'))
	return fields, subtypes, span
}

func (p *Parser) parseField(ctx context.Context) ast.Field {
	ctx = Describe(ctx, "field")
	start := p.peek()
	ctx = BeginSpan(ctx, start)

	var fieldName string
	var fieldTok lexer.Token
	switch start.Type {
	case lexer.DefinitelyFieldIdent:
		fieldName, fieldTok = p.expectWithText(ctx, lexer.DefinitelyFieldIdent)
	case lexer.FieldIdentOrKey:
		fieldName, fieldTok = p.expectWithText(ctx, lexer.FieldIdentOrKey)
	default:
		p.markErrExp(Describe(ctx, "field name"), start, lexer.DefinitelyFieldIdent)
	}

	inline := false
	if fieldName == "_inline" {
		fieldName = ""
		inline = true
	}

	p.expect(ctx, ':')
	var mods ast.ModifierList
	p.untilEither(',', '}', func() {
		mods = append(mods, p.parseModifier(ctx))
	})
	// TODO: recover till '}' too
	span := EndSpan(ctx, p.expectOrRecover(ctx, ','))

	return ast.Field{
		Name: ast.IdentFrom(fieldName, fieldTok),
		Modifiers: mods,
		Embedded: inline,
		Span: span,
	}
}

// TODO: figure out how to include markers & docs in spans?
// TODO: better diagnostics for missing semicolon after newtype

func (p *Parser) parseEnumBlock(ctx context.Context) ast.Enum {
	ctx = Describe(ctx, "enum block")
	ctx = BeginSpan(ctx, p.expect(Describe(ctx, "enum block start"), '{'))

	var variants []ast.EnumVariant
	p.until('}', func() {
		ctx := Describe(ctx, "enum variant")
		docs, markers := p.maybeDocsMarkers(ctx)
		name, tok := p.expectWithText(ctx, lexer.TypeIdent)
		variants = append(variants, ast.EnumVariant{
			Docs: docs,
			Markers: markers,
			Name: ast.IdentFrom(name, tok),
			Span: ast.TokenSpan(tok),
		})
		if p.peek().Type != '}' {
			// comma, optional on last entry
			p.expect(ctx, ',')
		}
	})

	span := EndSpan(ctx, p.expectOrRecover(Describe(ctx, "enum block end"), '}'))
	return ast.Enum{
		Variants: variants,
		Span: span,
	}
}

func (p *Parser) Parse(ctx context.Context) *ast.File {
	res := &ast.File{}
	if tok := p.peek(); tok.Type == lexer.KWImport {
		imports := p.parseImports(ctx)
		res.Imports = &imports
	}

	for tok := p.peek(); tok.Type != lexer.EOF; tok = p.peek() {
		res.GroupVersions = append(res.GroupVersions, p.parseGroupVersion(ctx))
	}

	return res
}


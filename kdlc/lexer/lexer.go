// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 The Kubernetes Authors
package lexer

import (
	"fmt"
	"io"
	"text/scanner"
	"unicode"
	"context"
	"os"
)

// imitate text/scanner's method for this

const (
	EOF = -(iota + 2)
	Unexpected

	FieldIdentOrKey
	DefinitelyKey
	DefinitelyFieldIdent
	TypeIdent
	Number
	String
	FieldPath
	Comment
	Doc
	QualPath
	UnqualPath
	MarkerPath
	ImportName

	KWImport
	KWTypes
	KWMarkers
	KWFrom
	KWGroupVersion
	KWKind
	KWStruct
	KWUnion
	KWEnum
	KWNewType
	KWMarker
	KWTrue
	KWFalse
	// NB: non-decl-starting types (e.g. primitives, lists, etc) don't
	// have keywords, because we'll just end up parsing type mod lists as keys
)

// TODO: span-based errors like parser

type Lexer struct {
	sc scanner.Scanner
	fakeNext Token // used for one case where we need 2 look-ahead
	tokBuf []rune

	Error func(ctx context.Context, at Position, unexpected rune, notes ...string)
}

type Position = scanner.Position

type Token struct {
	Start, End Position
	Type rune
}
// TODO: just remove this and call TokenString directly
func (t Token) TypeString() string {
	return TokenString(t.Type)
}
func TokenString(tok rune) string {
	switch tok {
	case EOF:
		return "<eof>"
	case Unexpected:
		return "<unexpected>"
	case FieldIdentOrKey:
		return "<field-or-key>"
	case DefinitelyKey:
		return "<key>"
	case DefinitelyFieldIdent:
		return "<field>"
	case TypeIdent:
		return "<type>"
	case Number:
		return "<number>"
	case String:
		return "<string>"
	case FieldPath:
		return "<field-path>"
	case Comment:
		return "<comment>"
	case Doc:
		return "<doc>"
	case QualPath:
		return "<qualifed-path>"
	case UnqualPath:
		return "<unqualified-path>"
	case MarkerPath:
		return "<qualified-marker>"
	case ImportName:
		return "<import-name>"
	case KWImport:
		return "import"
	case KWTypes:
		return "types"
	case KWMarkers:
		return "markers"
	case KWFrom:
		return "from"
	case KWGroupVersion:
		return "group-version"
	case KWKind:
		return "kind"
	case KWStruct:
		return "struct"
	case KWUnion:
		return "union"
	case KWEnum:
		return "enum"
	case KWNewType:
		return "newtype"
	case KWMarker:
		return "marker"
	case KWTrue:
		return "true"
	case KWFalse:
		return "false"
	default:
		if unicode.IsGraphic(tok) {
			return fmt.Sprintf("%q", tok)
		}
		return fmt.Sprintf("%U", tok) // just print the number in hex
	}
}

func New(src io.Reader) *Lexer {
	lex := &Lexer{
		Error: func(ctx context.Context, loc Position, unexpected rune, notes ...string) {
			fmt.Fprintf(os.Stderr, "Error @ %s, unexpected %s\n", loc, scanner.TokenString(unexpected))
			for _, note := range notes {
				fmt.Fprintf(os.Stderr, "  ...%s\n", note)
			}
			fmt.Fprintln(os.Stderr, "")
		},
	}

	// nothing besides ident is customizable, so everything else is hand-written
	// it'd be nice to just hand roll this, but it gives us a positioning helper
	// and byte -> rune logic, which is nice.
	lex.sc.Init(src)
	lex.sc.Mode = 0
	lex.sc.Error = lex.scannerError

	return lex
}

func (l *Lexer) scannerError(sc *scanner.Scanner, msg string) {
	// TODO
	panic(msg)
}

func (l *Lexer) consumeCh() rune {
	ch := l.sc.Next()
	l.tokBuf = append(l.tokBuf, ch)
	return ch
}

func (l *Lexer) peekCh() rune {
	return l.sc.Peek()
}

func (l *Lexer) resetToken() {
	l.tokBuf = l.tokBuf[:0]
}

func (l *Lexer) skipCh() {
	l.sc.Next()
}

func (l *Lexer) expectChs(ctx context.Context, expected string, msgs ...string) bool {
	for _, exp := range expected {
		if !l.expectCh(ctx, exp, msgs...) {
			return false
		}
	}
	return true
}

func (l *Lexer) expectCh(ctx context.Context, expected rune, msgs ...string) bool {
	ch := l.peekCh()
	if ch != expected {
		l.markErr(ctx, ch, msgs...)
		return false
	}
	l.consumeCh()
	return true
}

func (l *Lexer) expectThat(ctx context.Context, cond func(rune) bool, msgs ...string) bool {
	ch := l.peekCh()
	if !cond(ch) {
		l.markErr(ctx, ch, msgs...)
		l.skipCh()
		return false
	}
	l.consumeCh()
	return true
}

func (l *Lexer) skipWhile(cond func(rune) bool) bool {
	for ch := l.peekCh(); ch != scanner.EOF; ch = l.peekCh() {
		if !cond(ch) {
			return true
		}
		l.skipCh()
	}
	return false
}

func (l *Lexer) consumeWhile(cond func(rune) bool) bool {
	for ch := l.peekCh(); ch != scanner.EOF; ch = l.peekCh() {
		if !cond(ch) {
			return true
		}
		l.consumeCh()
	}
	return false
}

// a trick from text/scanner
const whitespace = (1<<'\t' | 1<<'\n' | 1<<'\r' | 1<<' ')

func (l *Lexer) Next(ctx context.Context) Token {
	// check for a fake next (only works for literal rune tokens)
	if l.fakeNext.Type != 0 {
		l.resetToken()
		l.tokBuf = append(l.tokBuf, l.fakeNext.Type)
		tok := l.fakeNext
		l.fakeNext = Token{Type: 0}
		return tok
	}

	// first, check whitespace
	l.skipWhile(func(ch rune) bool {
		return whitespace & (1 << uint(ch)) != 0
	})

	ch := l.peekCh()

	// terminate if we've hit the end
	if ch == scanner.EOF {
		return Token{Type: EOF, Start: l.sc.Pos(), End: l.sc.Pos()}
	}

	// then, on to actual stuff
	l.resetToken()
	switch {
	case ch == '/': // comment-ish (comment, doc)
		return l.scanCommentish(ctx)
	case ch == '-':  // number
		fallthrough
	case ch >= '0' && ch <= '9':
		return l.scanNumber(ctx)
	case ch == '"': // string
		return l.scanString(ctx)
	case ch == '.':  // fieldPath
		return l.scanFieldPath(ctx)
	case unicode.IsUpper(ch): // definitely type
		return l.scanTypeIdent(ctx)
	case unicode.IsLower(ch): // maybe field, key, qualified ident, keyword, etc
		return l.scanIdentish(ctx)
	case ch == '`': // raw identifier
		return l.scanRawIdent(ctx)
	case ch == '_': // _inline
		start := l.sc.Pos()
		if !l.expectChs(ctx, "_inline", "`_inline` (inline field)") {
			return Token{Start: start, End: l.sc.Pos(), Type: Unexpected}
		}
		return Token{Start: start, End: l.sc.Pos(), Type: DefinitelyFieldIdent}
	default: // basic symbols or unexpected
		switch ch {
		case '(', ')', '{', '}', '[', ']', ':', ',', ';', '@':
			start := l.sc.Pos()
			l.consumeCh()
			return Token{Start: start, End: l.sc.Pos(), Type: ch}
		}

		// we hit an error, attempt to recover
		l.markErr(ctx, ch, "`///` (docs)", "`//` (line comment)", "`/*` (block comment)", "-?[0-9] (number)",  "`\"` (string)", "`.` (field path)", "[A-Z] (type identifier)", "[a-z] (field, key, keyword, qualified identifier)", "backtick (raw identifier)")
		start := l.sc.Pos()
		l.consumeCh() // whatever we hit
		end := l.sc.Pos()

		// consume, carry on till we see a scope terminator to see if we can parse the rest
		foundTerm := l.skipWhile(func(ch rune) bool { return ch != '}' && ch != ')' })
		if foundTerm {
			l.skipCh()
		}

		return Token{Start: start, End: end, Type: Unexpected}
	}
}

func (l* Lexer) markErr(ctx context.Context, unexpected rune, expected ...string) {
	l.markErrAt(ctx, l.sc.Pos(), unexpected, expected...)
}
func (l *Lexer) markErrAt(ctx context.Context, at scanner.Position, unexpected rune, expected ...string) {
	l.Error(ctx, at, unexpected, expected...)
}

const eolBits = (1<<'\r' | 1<<'\n')

func (l *Lexer) scanCommentish(ctx context.Context) Token {
	var tokType rune = Unexpected
	start := l.sc.Pos()
	var end scanner.Position

	l.consumeCh() // skip the first slash
	switch ch := l.consumeCh(); ch {
	case '*': // block
		closed := l.consumeWhile(func(ch rune) bool {
			isStar := ch == '*'
			if isStar { // might be a closing star-slash
				l.consumeCh() // the '*'
				if l.peekCh() == '/' { // it's a closing star-slash, we're done
					l.consumeCh() // the '/'
					return false
				}
			}
			return true
		})
		if !closed {
			l.markErr(ctx, scanner.EOF, "*/ (terminated comment)")
		}
		end = l.sc.Pos()
		tokType = Comment
	case '/': // line or doc
		tokType = Comment
		isDoc := l.peekCh() == '/'
		if isDoc {
			l.consumeCh() // consume the third slash
			// we'll process doc line vs doc heading while parsing, just slurp docs normally
			tokType = Doc
		}
		// slurp till the end of the line
		l.consumeWhile(func(ch rune) bool { return ch != '\r' && ch != '\n' })
		end = l.sc.Pos() // mark the end before the newline, but still consume it anyway

		if ch == '\r' {
			l.skipCh()
		}
		if ch == '\n' {
			l.skipCh()
		}
	default:
		l.markErrAt(ctx, start, ch, "`/*` (block comment)", "`//` (line comment)", "`///` (docs)")
		end = l.sc.Pos()
	}

	return Token{
		Start: start,
		End: end,
		Type: tokType,
	}
}

func (l *Lexer) scanNumber(ctx context.Context) Token {
	start := l.sc.Pos()

	if l.peekCh() == '-' {
		l.consumeCh()
	}

	if !l.expectThat(ctx, between('1', '9'), "[1-9] (non-zero digit to start a number)") {
		l.skipCh()
		return Token{Start: start, End: l.sc.Pos(), Type: Number}
	}
	l.consumeWhile(between('0', '9'))

	return Token{Start: start, End: l.sc.Pos(), Type: Number}
}

func (l *Lexer) scanString(ctx context.Context) Token {
	start := l.sc.Pos()
	l.consumeCh() // consume the opening `"`

	consumeTillEnd := func() {
		// consume till end of string to recover gracefully
		closed := l.skipWhile(isNot('"'))
		if !closed {
			l.markErr(ctx, l.peekCh(), "(unterminated string literal)")
		}
		l.skipCh() // the close quote
	}

	var ch rune
	for ch = l.peekCh(); ch != scanner.EOF; ch = l.peekCh() {
		switch ch {
		case '\r', '\n':
			l.markErr(ctx, ch, "(strings may not have actual newlines in them)")
			end := l.sc.Pos()
			consumeTillEnd()
			return Token{Start: start, End: end, Type: String}
		case '\\':
			l.consumeCh()
			ch = l.peekCh()
			switch ch {
			case '\\', '"', '/', 'b', 'f', 'n', 'r', 't':
				l.consumeCh()
			case 'u':
				for i := 0; i < 4; i++ {
					ch = l.peekCh()
					switch {
					case ch >= '0' && ch <= '9':
					case ch >= 'a' && ch <= 'z':
					case ch >= 'A' && ch <= 'Z':
					default:
						l.markErr(ctx, ch, "[0-9a-zA-z] (in `\\uXXXX` escape)")
						end := l.sc.Pos()
						consumeTillEnd()
						return Token{Start: start, End: end, Type: String}
					}
					l.consumeCh()
				}
			default:
				l.markErr(ctx, ch, "([\"\\/bfnrt]|u[[:xdigit:]]{4}) (valid single-letter escape or unicode escape)")
				end := l.sc.Pos()
				consumeTillEnd()
				return Token{Start: start, End: end, Type: String}
			}
		case '"':
			l.consumeCh()
			return Token{Start: start, End: l.sc.Pos(), Type: String}
		default:
			l.consumeCh()
		}
	}
	if ch == scanner.EOF {
		l.markErr(ctx, ch, "(unterminated string literal)")
	}
	return Token{Start: start, End: l.sc.Pos(), Type: String}
}

func (l *Lexer) scanFieldPath(ctx context.Context) Token {
	start := l.sc.Pos()
	l.consumeCh() // skip the dot

	if !l.expectThat(ctx, unicode.IsLower, "Lu (lower case letter to start field name)") {
		return Token{Start: start, End: l.sc.Pos(), Type: FieldPath}
	}

	l.consumeWhile(func(ch rune) bool { return unicode.IsLetter(ch) || unicode.IsDigit(ch) })
	return Token{Start: start, End: l.sc.Pos(), Type: FieldPath}
}

func (l *Lexer) scanTypeIdent(ctx context.Context) Token {
	start := l.sc.Pos()
	l.scanTypeIdentInternal(ctx)
	return Token{Start: start, End: l.sc.Pos(), Type: TypeIdent}
}

func (l *Lexer) scanRawIdent(ctx context.Context) Token {
	start := l.sc.Pos()
	l.consumeCh() // skip the first backtick

	// raw idents are allowed to violate a few rules (e.g. starting
	// capitalization), but *not* others, likes spaces, etc.
	// We might weaken this in the future, which is one of several reasons
	// why this is terminated with another backtick.
	//
	// We to allow this to be used for keys too, we check for dashes -- if
	// we have a dash, we treat this as a key, otherwise it's just a weirdly
	// named field identifier.
	//
	// they're largely intended to allow you to manually specify a keyword.
	// Technically, we could probably allow this from context, but this is
	// safer.

	defKey := false
	l.consumeWhile(func(ch rune) bool {
		if ch == '-' {
			defKey = true
			return true
		}
		return unicode.IsLetter(ch) || unicode.IsDigit(ch)
	})

	typ := rune(FieldIdentOrKey)
	if defKey {
		typ = DefinitelyKey
	}

	if !l.expectCh(ctx, '`', "backtick (end of literal identifier, literal identifiers can only contain norml field, type, or key identifier characters)") {
		end := l.sc.Pos()
		// attempt to recover by scanning till a backtick or EOL
		l.skipWhile(isNot('`'))
		if l.peekCh() == '`' {
			l.skipCh()
		}
		return Token{Start: start, End: end, Type: typ}
	}

	return Token{Start: start, End: l.sc.Pos(), Type: typ}
}

func (l *Lexer) scanIdentish(ctx context.Context) Token {
	// either: a keword, a key, a qualified path, or a field identifier
	start := l.sc.Pos()

	firstCh := l.consumeCh() // skip the first letter

	// here, parse until we hit the end or we conclusively figure it out, then
	// hand off to more specific logic
	nonAscii := firstCh >= unicode.MaxASCII // group names only have ascii, so keep track of this
	allLower := unicode.IsLower(firstCh) // distinguish marker imports from unqualified paths
Loop:
	for ch := l.peekCh(); ch != scanner.EOF; ch = l.peekCh() {
		// look for hints, specifically:
		// - anything with a `-` is either a group name (qualified ident),
		//   keyword, or key
		// - anything with an uppercase letter is a field identifier
		// - anything else might be any of the above
		switch {
		case allLower && ch == '-': // definitely key/keyword/group name
			return l.scanRestOfKeyish(ctx, start, nonAscii)
		case allLower && ch == ':': // either a marker path, or a key with a colon after it
			// we need to double-check that this isn't just a key:BLAH case where someone
			// skipped a space.  The grammar isn't ambiguous here, but if we split into
			// lex-then-parse we need to an extra lookahead.
			potentialEnd := l.sc.Pos()
			l.consumeCh()
			ch = l.peekCh()
			if ch == ':' {
				return l.scanRestOfMarkerPath(ctx, start)
			}
			l.tokBuf = l.tokBuf[:len(l.tokBuf)-1]
			// inject a colon back in for our next call to Next
			l.fakeNext = Token{Start: potentialEnd, End: l.sc.Pos(), Type: ':'}
			break Loop // just a key
		case !allLower && ch == ':': // an unqualified path
			return l.scanRestOfUnqualPath(ctx, start)
		case allLower && !nonAscii && (ch == '/' || ch == '.'):
			return l.scanRestOfQualPath(ctx, start)
		case unicode.IsUpper(ch): // definitely a field identifier
			return l.scanRestOfFieldIdent(start)
		case unicode.IsLetter(ch), unicode.IsDigit(ch):
			if ch > unicode.MaxASCII {
				nonAscii = true
			}
			if allLower {
				allLower = unicode.IsLower(ch)
			}
			// fine, just continue
		default:
			// not part of an ident-ish
			break Loop
		}
		l.consumeCh()
	}

	// check if it's a keyword that doesn't have a dash...
	var tokType rune
	switch string(l.tokBuf) {
	case "import":
		tokType = KWImport
	case "types":
		tokType = KWTypes
	case "markers":
		tokType = KWMarkers
	case "from":
		tokType = KWFrom
	case "kind":
		tokType = KWKind
	case "struct":
		tokType = KWStruct
	case "union":
		tokType = KWUnion
	case "enum":
		tokType = KWEnum
	case "newtype": // TODO: make this "wrapper" or something?
		tokType = KWNewType
	case "marker":
		tokType = KWMarker
	case "true":
		tokType = KWTrue
	case "false":
		tokType = KWFalse
	default:
		// ...otherwise it's an all-lowercase field ident or a key w/o a dash,
		// parser will figure it out
		tokType = FieldIdentOrKey
	// group-version has a dash, will be picked up above
	}

	return Token{Start: start, End: l.sc.Pos(), Type: tokType}
}

func (l *Lexer) scanRestOfKeyish(ctx context.Context, start scanner.Position, hadNonAscii bool) Token {
	l.consumeCh() // consume the dash

Loop:
	for ch := l.peekCh(); ch != scanner.EOF; ch = l.peekCh() {
		if hadNonAscii { // gonna be a key
			if !unicode.IsLower(ch) || unicode.IsDigit(ch) || ch == '-' {
				return Token{Start: start, End: l.sc.Pos(), Type: DefinitelyKey}
			}
			l.consumeCh()
			continue
		}

		// might still be a key, or could be the start of a qualified identifier
		switch {
		case ch == '/' || ch == '.': // definitely qualified path
			return l.scanRestOfQualPath(ctx, start)
		case unicode.IsLower(ch) || unicode.IsDigit(ch) || ch == '-':
			if ch > unicode.MaxASCII {
				hadNonAscii = true
			}
			l.consumeCh()
			// valid, just continue
		default:
			break Loop
		}
	}

	// not a path, must be either `group-version` or a key
	if string(l.tokBuf) == "group-version" {
		return Token{Start: start, End: l.sc.Pos(), Type: KWGroupVersion}
	}

	return Token{Start: start, End: l.sc.Pos(), Type: DefinitelyKey}
}
func (l *Lexer) scanRestOfFieldIdent(start scanner.Position) Token {
	l.consumeCh() // consume the first most recent character
	l.consumeWhile(func(ch rune) bool { return unicode.IsLetter(ch) || unicode.IsDigit(ch) })
	return Token{Start: start, End: l.sc.Pos(), Type: DefinitelyFieldIdent}
}
func (l *Lexer) scanTypeIdentInternal(ctx context.Context) bool {
	if !l.expectThat(ctx, unicode.IsUpper, "Lu (upper case, start of type identifier)") {
		return false
	}

	l.consumeWhile(func(ch rune) bool { return unicode.IsLetter(ch) || unicode.IsDigit(ch) })
	return true
}

func (l *Lexer) scanUnqualPathInternal(ctx context.Context) bool {
	if !l.scanTypeIdentInternal(ctx) {
		// error, don't continue
		return false
	}

	// see if we have more parts...
	ch := l.peekCh()
	if ch != ':' {
		// if not, return
		return true
	}

	// otherwise, continue scanning ::TypeIdent parts
	for ch != scanner.EOF && ch == ':' {
		l.consumeCh() // consume the first colon

		// expect the second colon
		if !l.expectCh(ctx, ':', "`:` (as part of :: for an unqualified path)") {
			return false
		}
		if !l.scanTypeIdentInternal(ctx) {
			// error, don't try an continue
			return false
		}
		ch = l.peekCh()
	}

	// we got a whole path!
	return true
}

func (l *Lexer) scanRestOfUnqualPath(ctx context.Context, start scanner.Position) Token {
	// already consumed the first colon above to check if this was just a key
	if !l.expectCh(ctx, ':', "`:` (as part of :: for an unqualified path)") {
		return Token{Start: start, End: l.sc.Pos(), Type: UnqualPath}
	}

	l.scanUnqualPathInternal(ctx)
	return Token{Start: start, End: l.sc.Pos(), Type: UnqualPath}
}

func (l *Lexer) scanRestOfMarkerPath(ctx context.Context, start scanner.Position) Token {
	// already consumed the first colon above to check if this was just a key
	if !l.expectCh(ctx, ':', "`:` (as part of :: for an imported marker name)") {
		return Token{Start: start, End: l.sc.Pos(), Type: UnqualPath}
	}

	// scan a key
	if !l.expectThat(ctx, unicode.IsLower, "[a-z] (to start a marker name)") {
		return Token{Start: start, End: l.sc.Pos(), Type: MarkerPath}
	}
	l.consumeWhile(func(ch rune) bool {
		return unicode.IsLower(ch) || unicode.IsDigit(ch) || ch == '-'
	})

	return Token{Start: start, End: l.sc.Pos(), Type: MarkerPath}
}

func (l *Lexer) scanGroupDNSLabelInternal(ctx context.Context) (int, bool) {
	count := 0
	if !l.expectThat(ctx, between('a', 'z'), "[a-z] (to start a DNS label in a group name)") {
		return 0, false
	}
	count++

	var ch rune
	for i := 0; i < 62; i++ {
		ch = l.peekCh()
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= '0' && ch <= '9':
		case ch == '-':
		default:
			break
		}
		count++
		l.consumeCh()
	}
	if ch == '-' {
		l.markErr(ctx, ch, "(last character of a DNS label may not be a dash)")
		return 0, false
	}

	return count, true
}

func (l *Lexer) scanRestOfQualPath(ctx context.Context, start scanner.Position) Token {
	slashOrDot := l.consumeCh()
	if slashOrDot == '.' {
		// scan one or more dot-separated dns labels...
		totalCount := 0
		var ch rune
		for ch = l.peekCh(); ch != scanner.EOF; ch = l.peekCh() {
			count, ok := l.scanGroupDNSLabelInternal(ctx)
			if !ok {
				return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
			}
			totalCount += count
			ch = l.peekCh()
			if ch != '.' {
				break
			}
			l.consumeCh()
			totalCount++

			if totalCount > 253 {
				l.markErr(ctx, ch, "(group names may not be longer than 253 bytes)")
				return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
			}
		}
		if ch == '.' {
			l.markErr(ctx, ch, "(last character of grou name cannot be a dot)")
			return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
		}

		// ... then a slash to resume below
		if !l.expectCh(ctx, '/', "`/` (separating group from version in a qualified path)") {
			return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
		}
	}

	// scan a version (/v[1-9][0-9]*((alpha|beta)[1-9][0-9]*)?/)
	ch := l.peekCh()
	if ch == '_' { // __internal
		if !l.expectChs(ctx, "__internal", "`__internal` (internal version)", "`v` (the start of a version)") {
			return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
		}
	} else {
		if !l.expectCh(ctx, 'v', "`__internal` (internal version)", "`v` (the start of a version)") {
			return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
		}
		if !l.expectThat(ctx, between('1', '9'), "[1-9] (to start a version number)") {
			return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
		}

		l.consumeWhile(between('0', '9'))

		ch = l.peekCh()
		if ch == 'a' || ch == 'b' {
			switch ch {
			case 'a': // alpha
				if !l.expectChs(ctx, "alpha", "`alpha` (as part of a version name)", "`beta` (as part of a version name)") {
					return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
				}
			case 'b': // beta
				if !l.expectChs(ctx, "beta", "`alpha` (as part of a version name)", "`beta` (as part of a version name)") {
					return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
				}
			}
			if !l.expectThat(ctx, between('1', '9'), "[1-9] (in a version, after alpha or beta)") {
				return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
			}
			l.consumeWhile(between('0', '9'))
		}
	}

	// we use group/version in the imports section too, so just skip if we don't see
	// a colon
	if ch := l.peekCh(); ch != ':' {
		return Token{Start: start, End: l.sc.Pos(), Type: ImportName}
	}

	// then two colons, and an unqualified path
	if !l.expectCh(ctx, ':', "`:` (as part of :: for a qualified path)") {
		return Token{Start: start, End: l.sc.Pos(), Type: UnqualPath}
	}
	if !l.expectCh(ctx, ':', "`:` (as part of :: for a qualified path)") {
		return Token{Start: start, End: l.sc.Pos(), Type: UnqualPath}
	}

	l.scanUnqualPathInternal(ctx)
	return Token{Start: start, End: l.sc.Pos(), Type: QualPath}
}

func (l *Lexer) TokenText() string {
	return string(l.tokBuf)
}

func between(start, end rune) func(rune) bool {
	return func(ch rune) bool {
		return ch >= start && ch <= end
	}
}

func isNot(end rune) func(rune) bool {
	return func(ch rune) bool {
		return ch != end
	}
}

package trace

import (
	"fmt"
	"context"
	"io"
	"strings"
	"os"

	"k8s.io/idl/kdlc/lexer"
)

type ctxKey int
const (
	tracesKey ctxKey = iota
	inputKey
)

type TokenPosition struct {
	Start, End lexer.Position
}
func (p TokenPosition) IsValid() bool {
	// an empty token would be non-sensical, so it's fine to treat
	// "end-at-zero" as "unset end" and thus as invalid
	return p.End.Offset != 0
}
func posFor(tok lexer.Token) TokenPosition {
	return TokenPosition{
		Start: tok.Start,
		End: tok.End,
	}
}

type Span struct {
	Start, End TokenPosition
}
func (s Span) SpanStart() TokenPosition {
	return s.Start
}
func (s Span) SpanEnd() TokenPosition {
	return s.End
}

func (s Span) Complete() bool {
	// an empty token would be non-sensical, so it's fine to treat
	// "end-at-zero" as "unset end" and thus as invalid
	return s.End.IsValid()
}

type Spannable interface {
	SpanStart() TokenPosition
	SpanEnd() TokenPosition
}

type Traces struct {
	Parent *Traces

	Span *Span
	Desc, Key string
	Value interface{}
}

func parentSpan(ctx context.Context) *Span {
	parent, hadParent := ctx.Value(tracesKey).(*Traces)
	if !hadParent {
		return nil
	}
	eventual := parent
	for eventual != nil && eventual.Span == nil {
		eventual = eventual.Parent
	}
	if eventual == nil {
		return nil
	}
	return eventual.Span
}

func BeginSpan(ctx context.Context, tok lexer.Token) context.Context {
	parent, _ := ctx.Value(tracesKey).(*Traces)
	return context.WithValue(ctx, tracesKey, &Traces{
		Parent: parent,
		Span: &Span{Start: posFor(tok)},
	})
}
func EndSpan(ctx context.Context, tok lexer.Token) Span {
	current := parentSpan(ctx)
	if current == nil {
		panic(fmt.Sprintf("ended a non-existent span with (%s, %s) [%s]", tok.Start, tok.End, tok.TypeString()))
	}
	if current.Complete() {
		panic(fmt.Sprintf("ended an already-complete span (%s, %s) @ (%s, %s) [%s]", current.Start, current.End, tok.Start, tok.End, tok.TypeString()))
	}

	if tok.End.Offset == 0 {
		// don't mark complete, this was an error token anway
		return *current
	}

	current.End = posFor(tok)
	return *current
}

func EndSpanAt(ctx context.Context, end TokenPosition) Span {
	current := parentSpan(ctx)
	if current == nil {
		panic(fmt.Sprintf("ended a non-existent span with %s", end))
	}
	if current.Complete() {
		panic(fmt.Sprintf("ended an already-complete span (%s, %s) @ %s", current.Start, current.End, end))
	}

	if !end.IsValid() {
		// don't mark complete, this was an error token anway
		return *current
	}

	current.End = end
	return *current
}

func TokenSpan(ctx context.Context, tok lexer.Token) (context.Context, Span) {
	parent, _ := ctx.Value(tracesKey).(*Traces)
	span := Span{Start: posFor(tok), End: posFor(tok)}
	return context.WithValue(ctx, tracesKey, &Traces{
		Parent: parent,
		Span: &span,
	}), span
}

func Describe(ctx context.Context, msg string) context.Context {
	parent, _ := ctx.Value(tracesKey).(*Traces)
	return context.WithValue(ctx, tracesKey, &Traces{
		Parent: parent,
		Desc: msg,
	})
}
func Note(ctx context.Context, key string, value interface{}) context.Context {
	parent, _ := ctx.Value(tracesKey).(*Traces)
	return context.WithValue(ctx, tracesKey, &Traces{
		Parent: parent,
		Key: key,
		Value: value,
	})
}

func SpanFrom(ctx context.Context) (Span, bool)  {
	span := parentSpan(ctx)
	if span == nil {
		return Span{}, false
	}
	return *span, true
}

func FullTraceFrom(ctx context.Context) (Traces, bool) {
	traces, ok := ctx.Value(tracesKey).(*Traces)
	if !ok {
		return Traces{}, false
	}
	return *traces, true
}

// TODO: constants for well-known keys

type note struct {
	key string
	value interface{}
}
func PrintTrace(ctx context.Context, out io.Writer, input string) {
	traces, ok := ctx.Value(tracesKey).(*Traces)
	if !ok {
		fmt.Fprintf(out, "[no trace]\n")
	}

	var span *Span
	var notes []note

	for current := traces; current != nil; current = current.Parent {
		// collect until we hit a description, then print everything out together
		switch {
		case current.Key != "":
			notes = append(notes, note{key: current.Key, value: current.Value})
		case current.Span != nil:
			// we should have one span per description, but just in case, handle it
			// as if we had an empty description here
			if span != nil {
				printChunk(out, input, "", notes, span)
				span = nil
				notes = notes[:0]
			}
			span = current.Span
		case current.Desc != "":
			printChunk(out, input, current.Desc, notes, span)
			span = nil
			notes = notes[:0]
		}
	}
}

func printChunk(out io.Writer, input string, desc string, notes []note, span *Span) {
	fmt.Fprintf(out, "  ...in %s", desc)
	for _, note := range notes {
		fmt.Fprintf(out, ", %s=", note.key)
		switch val := note.value.(type) {
		case rune:
			fmt.Fprint(out, lexer.TokenString(val))
		case []rune:
			fmt.Fprint(out, "[")
			for i, rn := range val {
				if i != 0 {
					fmt.Fprint(out, " | ")
				}
				fmt.Fprintf(out, "%s", lexer.TokenString(rn))
			}
			fmt.Fprint(out, "]")
		case string:
			fmt.Fprintf(out, "%q", val)
		default:
			fmt.Fprintf(out, "%v", val)
		}
	}
	if span != nil {
		if span.Complete() {
			fmt.Fprintf(out, " @ [%s, %s]\n\t%s", span.Start, span.End, Snippet(*span, input))
		} else {
			// TODO: partial snippet
			fmt.Fprintf(out, " @ [%s, <incomplete>]\n\t%s", span.Start, Snippet(*span, input))
		}
	}
	fmt.Fprintln(out, "")
}


func FullInputFrom(ctx context.Context) (string, bool) {
	input, ok := ctx.Value(inputKey).(string)
	return input, ok
}
func WithFullInput(ctx context.Context, input string) context.Context {
	return context.WithValue(ctx, inputKey, input)
}

func Snippet(loc Span, input string) string {
	if !loc.Complete() {
		// treat this as a single-token span if we're incomplete
		loc.End = loc.Start
	}

	lineStart := strings.LastIndexByte(input[:loc.Start.Start.Offset], '\n')+1
	if loc.Start.Start.Line == loc.End.End.Line {
		lineEnd := strings.IndexByte(input[loc.End.End.Offset:], '\n')
		if lineEnd == -1 {
			lineEnd = len(input)
		} else {
			lineEnd += loc.End.End.Offset
		}

		prefix := input[lineStart:loc.Start.Start.Offset]
		snip := input[loc.Start.Start.Offset:loc.End.End.Offset]
		suffix := input[loc.End.End.Offset:lineEnd]

		// add a little output-medium-agnostic styling
		// TODO(directxman12): in the future, we'll want to underline
		// or something, maybe like the rust compiler does
		return prefix+"\u300C"+snip+"\u300D"+suffix
	}

	// grab the first line
	lineEnd := strings.IndexByte(input[loc.Start.Start.Offset:loc.End.End.Offset], '\n')
	if lineEnd == -1 {
		lineEnd = len(input)
	} else {
		lineEnd += loc.Start.Start.Offset
	}

	prefix := input[lineStart:loc.Start.Start.Offset]
	snip := input[loc.Start.Start.Offset:lineEnd]
	return prefix+"\u300C"+snip+"...\u22EF"
}

func fromSpannable(sp Spannable) Span {
	if actSpan, isSpan := sp.(Span); isSpan {
		return actSpan
	}
	return Span{Start: sp.SpanStart(), End: sp.SpanEnd()}
}

func InSpan(ctx context.Context, sp Spannable) context.Context {
	parent, _ := ctx.Value(tracesKey).(*Traces)
	span := fromSpannable(sp)
	return context.WithValue(ctx, tracesKey, &Traces{
		Parent: parent,
		Span: &span,
	})
}
func ErrorAt(ctx context.Context, msg string) {
	// TODO
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	input, present := FullInputFrom(ctx)
	if !present {
		panic(msg)
	}
	PrintTrace(ctx, os.Stderr, input)
}
// TODO: In function that automatically attaches relevant info from ast nodes

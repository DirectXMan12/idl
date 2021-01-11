package trace

import (
	"fmt"
	"context"
	"io"
	"strings"

	"k8s.io/idl/kdlc/lexer"
	"k8s.io/idl/kdlc/parser/ast"
)

type ctxKey int
const (
	tracesKey ctxKey = iota
	inputKey
)

type Span struct {
	This ast.Span
	Complete bool
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
		Span: &Span{This: ast.Span{Start: tok.Start}, Complete: false},
	})
}
func EndSpan(ctx context.Context, tok lexer.Token) ast.Span {
	current := parentSpan(ctx)
	if current == nil {
		panic(fmt.Sprintf("ended a non-existent span with (%s, %s) [%s]", tok.Start, tok.End, tok.TypeString()))
	}
	if current.Complete {
		panic(fmt.Sprintf("ended an already-complete span (%s, %s) @ (%s, %s) [%s]", current.This.Start, current.This.End, tok.Start, tok.End, tok.TypeString()))
	}

	if tok.End.Offset == 0 {
		// don't mark complete, this was an error token anway
		return current.This
	}

	current.This.End = tok.End
	current.Complete = true
	return current.This
}

func EndSpanAt(ctx context.Context, end lexer.Position) ast.Span {
	current := parentSpan(ctx)
	if current == nil {
		panic(fmt.Sprintf("ended a non-existent span with %s", end))
	}
	if current.Complete {
		panic(fmt.Sprintf("ended an already-complete span (%s, %s) @ %s", current.This.Start, current.This.End, end))
	}

	if end.Offset == 0 {
		// don't mark complete, this was an error token anway
		return current.This
	}

	current.This.End = end
	current.Complete = true
	return current.This
}
func TokenSpan(ctx context.Context, tok lexer.Token) (context.Context, ast.Span) {
	parent, _ := ctx.Value(tracesKey).(*Traces)
	span := ast.TokenSpan(tok)
	return context.WithValue(ctx, tracesKey, &Traces{
		Parent: parent,
		Span: &Span{This: span, Complete: true},
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
		if span.Complete {
			fmt.Fprintf(out, " @ [%s, %s]\n\t%s", span.This.Start, span.This.End, Snippet(span.This, input))
		} else {
			// TODO: partial snippet
			fmt.Fprintf(out, " @ [%s, <incomplete>]", span.This.Start)
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

func Snippet(loc ast.Span, input string) string {
	lineStart := strings.LastIndexByte(input[:loc.Start.Offset], '\n')+1
	if loc.Start.Line == loc.End.Line {
		lineEnd := strings.IndexByte(input[loc.End.Offset:], '\n')
		if lineEnd == -1 {
			lineEnd = len(input)
		} else {
			lineEnd += loc.End.Offset
		}

		prefix := input[lineStart:loc.Start.Offset]
		snip := input[loc.Start.Offset:loc.End.Offset]
		suffix := input[loc.End.Offset:lineEnd]

		// add a little output-medium-agnostic styling
		// TODO(directxman12): in the future, we'll want to underline
		// or something, maybe like the rust compiler does
		return prefix+"\u300C"+snip+"\u300D"+suffix
	}

	// grab the first line
	lineEnd := strings.IndexByte(input[loc.Start.Offset:loc.End.Offset], '\n')
	if lineEnd == -1 {
		lineEnd = len(input)
	} else {
		lineEnd += loc.Start.Offset
	}

	prefix := input[lineStart:loc.Start.Offset]
	snip := input[loc.Start.Offset:lineEnd]
	return prefix+"\u300C"+snip+"...\u22EF"
}

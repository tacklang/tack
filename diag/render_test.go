package diag_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
)

// newFile returns a File over data with its line starts recorded the way the
// lexer records them.
func newFile(data string) *source.File {
	f := &source.File{ID: "test", Filename: "case.tck", Data: []byte(data)}
	f.AddLineStart(0)
	for i := range len(data) {
		if data[i] == '\n' {
			f.AddLineStart(i + 1)
		}
	}
	return f
}

// errorAt returns an error diagnostic covering [start, end).
func errorAt(msg string, start, end int) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  msg,
		Span:     ast.Span{Start: start, End: end},
	}
}

func TestSeverityNames(t *testing.T) {
	tests := []struct {
		want string
		sev  diag.Severity
	}{
		{"note", diag.Note},
		{"warning", diag.Warning},
		{"error", diag.Error},
		{"unknown", diag.Severity(-1)},
		{"unknown", diag.Severity(3)},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(tt.sev), got, tt.want)
		}
	}
}

func TestRender(t *testing.T) {
	f := newFile("ab\ncd\n")

	got := diag.Render(f, errorAt("bad", 4, 5))
	want := "error: case.tck:2:2: (4) bad"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// TestRenderPretty pins the full layout: header, a line of context either
// side, and a caret with tildes under the span, with nothing after them.
func TestRenderPretty(t *testing.T) {
	f := newFile("one\ntwo three\nfour\n")

	got := diag.RenderPretty(f, errorAt("bad", 8, 13))
	want := "case.tck:2:5: error: bad\n" +
		"01 |\tone\n" +
		"02 |\ttwo three\n" +
		"   |\t    ^~~~~\n" +
		"03 |\tfour\n"
	if got != want {
		t.Errorf("RenderPretty =\n%s\nwant\n%s", got, want)
	}
}

// The edges of a file: the first line has no line before it, the last line
// has no newline after it, and an empty span (an inserted token, or EOF)
// still gets a caret.
func TestRenderPrettyAtTheEdges(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
		d    diag.Diagnostic
	}{
		{
			"first byte of the file",
			"ab\ncd",
			"case.tck:1:1: error: e\n01 |\tab\n   |\t^\n02 |\tcd\n",
			errorAt("e", 0, 1),
		},
		{
			"last line without a newline",
			"ab\ncd",
			"case.tck:2:2: error: e\n01 |\tab\n02 |\tcd\n   |\t ^\n",
			errorAt("e", 4, 5),
		},
		{
			"empty span at the end of the input",
			"ab",
			"case.tck:1:3: error: e\n01 |\tab\n   |\t  ^\n",
			errorAt("e", 2, 2),
		},
		{
			"empty file",
			"",
			"case.tck:1:1: error: e\n01 |\t\n   |\t^\n",
			errorAt("e", 0, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diag.RenderPretty(newFile(tt.data), tt.d); got != tt.want {
				t.Errorf("RenderPretty =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// A marker follows the underline, and each related note is rendered after
// the main diagnostic with its own context.
func TestRenderPrettyWithMarkerAndRelated(t *testing.T) {
	f := newFile("a\nb\n")
	d := errorAt("dup", 2, 3)
	d.Marker = " here"
	d.Related = []diag.Diagnostic{
		{Severity: diag.Note, Message: "first", Span: ast.Span{Start: 0, End: 1}},
	}

	got := diag.RenderPretty(f, d)
	want := "case.tck:2:1: error: dup\n" +
		"01 |\ta\n" +
		"02 |\tb\n" +
		"   |\t^ here\n" +
		"03 |\t\n" +
		"case.tck:1:1: note: first\n" +
		"01 |\ta\n" +
		"   |\t^\n" +
		"02 |\tb\n"
	if got != want {
		t.Errorf("RenderPretty =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderAllKeepsOrder(t *testing.T) {
	f := newFile("abc")
	got := diag.RenderAll(f, []diag.Diagnostic{errorAt("first", 0, 1), errorAt("second", 2, 3)})

	want := "error: case.tck:1:1: (0) first\nerror: case.tck:1:3: (2) second\n"
	if got != want {
		t.Errorf("RenderAll = %q, want %q", got, want)
	}
}

func TestRenderAllPrettyRendersEveryDiagnostic(t *testing.T) {
	f := newFile("abc")
	got := diag.RenderAllPretty(
		f,
		[]diag.Diagnostic{errorAt("first", 0, 1), errorAt("second", 2, 3)},
	)

	if strings.Index(got, "first") > strings.Index(got, "second") ||
		!strings.Contains(got, "second") {
		t.Errorf("RenderAllPretty lost or reordered diagnostics:\n%s", got)
	}
}

// TestRenderPrettyDropsCarriageReturns checks a line ending in \r\n is shown
// without its carriage return, so a source saved with CRLF line endings
// renders as one saved with \n does, at the same line and column.
func TestRenderPrettyDropsCarriageReturns(t *testing.T) {
	lf := newFile("ab\ncd\nef\n")
	crlf := newFile("ab\r\ncd\r\nef\r\n")

	got := diag.RenderPretty(crlf, errorAt("bad", 4, 6))
	want := diag.RenderPretty(lf, errorAt("bad", 3, 5))
	if got != want {
		t.Errorf("RenderPretty = %q, want %q", got, want)
	}
	if strings.Contains(got, "\r") {
		t.Errorf("RenderPretty kept a carriage return: %q", got)
	}
}

// TestRenderPrettyKeepsALoneCarriageReturn checks a carriage return that
// does not end a line is shown as the byte it is.
func TestRenderPrettyKeepsALoneCarriageReturn(t *testing.T) {
	got := diag.RenderPretty(newFile("a\rb\n"), errorAt("bad", 1, 2))
	if !strings.Contains(got, "a\rb\n") {
		t.Errorf("RenderPretty = %q, want the line a\\rb", got)
	}
}

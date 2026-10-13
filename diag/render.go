package diag

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/source"
)

// Render formats a diagnostic as a single line: severity, file:line:col, the
// byte offset, and the message.
func Render(f *source.File, d Diagnostic) string {
	line, col := f.Position(d.Span.Start)
	return fmt.Sprintf(
		"%s: %s:%d:%d: (%d) %s",
		d.Severity.String(),
		f.Filename,
		line,
		col,
		d.Span.Start,
		d.Message,
	)
}

// RenderPretty formats a diagnostic and its related notes for a person, each
// followed by the surrounding source lines with the span underlined.
func RenderPretty(f *source.File, d Diagnostic) string {
	line, col := f.Position(d.Span.Start)
	var str strings.Builder
	fmt.Fprintf(&str, "%s:%d:%d: %s: %s\n", f.Filename, line, col, d.Severity.String(), d.Message)
	renderCtx(f, &str, line, col, &d, 1)

	for _, r := range d.Related {
		line, col := f.Position(r.Span.Start)
		fmt.Fprintf(
			&str,
			"%s:%d:%d: %s: %s\n",
			f.Filename,
			line,
			col,
			r.Severity.String(),
			r.Message,
		)
		renderCtx(f, &str, line, col, &r, 1)
	}

	return str.String()
}

// renderCtx writes the n lines either side of line, and under line itself a
// marker for d's span followed by d.Marker. Each line is written without
// its newline, \n or \r\n, and ended with \n.
func renderCtx(f *source.File, str *strings.Builder, line, col int, d *Diagnostic, n int) {
	numLines := f.NumLines()
	for i := range 2*n + 1 {
		l := line - n + i
		if l < 1 || l > numLines {
			continue
		}
		lval := bytes.TrimSuffix(f.Line(l), []byte("\n"))
		// A line ending in \r\n is shown without the carriage return, as
		// the newline it is a part of; a lone one is shown as it is.
		if len(lval) < len(f.Line(l)) {
			lval = bytes.TrimSuffix(lval, []byte("\r"))
		}
		fmt.Fprintf(str, "%02d |\t%s\n", l, lval)
		if l == line {
			str.WriteString("   |\t")
			renderTokenMarker(str, col, &d.Span)
			str.WriteString(d.Marker)
			str.WriteRune('\n')
		}
	}
}

// renderTokenMarker writes a caret at col and tildes under the rest of span,
// and nothing after them. An empty span, such as an inserted semicolon or
// the end of the input, still gets its caret.
func renderTokenMarker(str *strings.Builder, col int, span *ast.Span) {
	start, end := col-1, col-1+span.End-span.Start
	for c := range max(end, start+1) {
		switch {
		case c == start:
			str.WriteRune('^')
		case c > start && c < end:
			str.WriteRune('~')
		default:
			str.WriteRune(' ')
		}
	}
}

// RenderAll renders each diagnostic with Render, one per line.
func RenderAll(f *source.File, diags []Diagnostic) string {
	var str strings.Builder
	for _, d := range diags {
		str.WriteString(Render(f, d))
		str.WriteString("\n")
	}
	return str.String()
}

// RenderAllPretty renders each diagnostic with RenderPretty, separated by
// blank lines.
func RenderAllPretty(f *source.File, diags []Diagnostic) string {
	var str strings.Builder
	str.WriteString("\n")
	for _, d := range diags {
		str.WriteString(RenderPretty(f, d))
		str.WriteString("\n")
	}
	return str.String()
}

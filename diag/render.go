package diag

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/source"
)

// unknownFile is what a diagnostic whose span points into no file given to
// the renderer shows in place of the file's name. That is a compiler bug,
// but it is rendered rather than panicked on.
const unknownFile = "<unknown file>"

// Render formats a diagnostic as a single line: severity, file:line:col, the
// byte offset, and the message. files finds the file d's span points into.
func Render(files source.Files, d Diagnostic) string {
	f := files.Lookup(d.Span.File)
	if f == nil {
		return fmt.Sprintf(
			"%s: %s: (%d) %s",
			d.Severity.String(),
			unknownFile,
			d.Span.Start,
			d.Message,
		)
	}
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
// followed by the surrounding source lines with the span underlined. files
// finds the file each span points into, so a note may be in a different
// file from its diagnostic.
func RenderPretty(files source.Files, d Diagnostic) string {
	var str strings.Builder
	renderOne(files, &str, &d)
	for i := range d.Related {
		renderOne(files, &str, &d.Related[i])
	}
	return str.String()
}

// renderOne writes d's header line and its source context, without its
// related notes. A span in no known file gets the header alone.
func renderOne(files source.Files, str *strings.Builder, d *Diagnostic) {
	f := files.Lookup(d.Span.File)
	if f == nil {
		fmt.Fprintf(str, "%s: %s: %s\n", unknownFile, d.Severity.String(), d.Message)
		return
	}
	line, col := f.Position(d.Span.Start)
	fmt.Fprintf(str, "%s:%d:%d: %s: %s\n", f.Filename, line, col, d.Severity.String(), d.Message)
	renderCtx(f, str, line, col, d, 1)
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
func RenderAll(files source.Files, diags []Diagnostic) string {
	var str strings.Builder
	for _, d := range diags {
		str.WriteString(Render(files, d))
		str.WriteString("\n")
	}
	return str.String()
}

// RenderAllPretty renders each diagnostic with RenderPretty, separated by
// blank lines.
func RenderAllPretty(files source.Files, diags []Diagnostic) string {
	var str strings.Builder
	str.WriteString("\n")
	for _, d := range diags {
		str.WriteString(RenderPretty(files, d))
		str.WriteString("\n")
	}
	return str.String()
}

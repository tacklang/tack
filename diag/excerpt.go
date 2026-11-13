package diag

import (
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/source"
)

// Excerpt returns the source context a diagnostic at span, a range of f,
// shows below its header line, exactly as RenderPretty writes it for a
// diagnostic with no marker text: the line before the one span starts on,
// that line and a marker under span, and the line after, those of them f
// has. The type checker gives it to a testing.Site (see typeck), so a
// place a program reports at run time reads as a diagnostic does.
func Excerpt(f *source.File, span ast.Span) string {
	var str strings.Builder
	line, col := f.Position(span.Start)
	renderCtx(f, &str, line, col, &Diagnostic{Span: span}, 1)
	return str.String()
}

package diag

import (
	"github.com/tacklang/tack/ast"
)

// Diagnostic is one problem found in user input. Marker is optional text
// printed after the underline in pretty output, and Related holds supporting
// notes, such as where a conflicting declaration was first made.
type Diagnostic struct {
	Message  string
	Marker   string
	Related  []Diagnostic
	Span     ast.Span
	Severity Severity

	// Unused says the span is code that is never used, which an editor
	// may dim: the name of a declaration nothing uses, or an expression
	// whose value is dropped. Only a warning sets it; it changes nothing
	// rendered.
	Unused bool
}

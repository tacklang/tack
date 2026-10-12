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
}

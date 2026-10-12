package ast

import (
	"github.com/tacklang/tack/source"
)

// Span is a half-open byte range [Start, End) in the file identified by File.
type Span struct {
	File  source.FileID
	Start int
	End   int
}

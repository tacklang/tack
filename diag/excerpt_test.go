package diag_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// TestExcerptIsWhatRenderPrettyWritesBelowItsHeader checks Excerpt gives
// exactly the lines RenderPretty writes after a diagnostic's header, for
// spans on one line and over several, at the first and the last line of a
// file, and in a file saved with \r\n.
func TestExcerptIsWhatRenderPrettyWritesBelowItsHeader(t *testing.T) {
	cases := []struct {
		data       string
		start, end int
	}{
		{"one\ntwo three\nfour\n", 4, 7},
		{"one\ntwo three\nfour\n", 0, 3},
		{"one\ntwo three\nfour", 14, 18},
		{"x\nab(\ncd\n)\ny\n", 2, 10},
		{"x\r\nab(\r\ncd\r\n)\r\ny\r\n", 3, 13},
		{"a\tb\xff\n", 2, 3},
	}
	for _, tc := range cases {
		f := newFile(tc.data)
		_, below, _ := strings.Cut(diag.RenderPretty(f, errorAt("m", tc.start, tc.end)), "\n")
		got := diag.Excerpt(f, ast.Span{File: f.ID, Start: tc.start, End: tc.end})
		if got != below {
			t.Errorf("Excerpt(%q, %d, %d) = %q, want %q", tc.data, tc.start, tc.end, got, below)
		}
	}
}

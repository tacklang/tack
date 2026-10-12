package source_test

import (
	"testing"

	"github.com/tacklang/tack/source"
)

// newFile returns a File over data with its line starts recorded the way the
// lexer records them: 0, then one past every newline.
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

// TestPosition pins the line and column of offsets at the edges of lines: the
// first byte, a newline itself, the byte after it, and the end of the input,
// which is where the EOF token sits.
func TestPosition(t *testing.T) {
	tests := []struct {
		data      string
		off       int
		line, col int
	}{
		{"", 0, 1, 1},
		{"abc", 0, 1, 1},
		{"abc", 2, 1, 3},
		{"abc", 3, 1, 4},
		{"ab\ncd", 2, 1, 3},
		{"ab\ncd", 3, 2, 1},
		{"ab\ncd", 5, 2, 3},
		{"ab\n", 3, 2, 1},
		{"\n\n\n", 1, 2, 1},
		{"\n\n\n", 3, 4, 1},
		{"é\nx", 2, 1, 3},
		{"é\nx", 3, 2, 1},
	}

	for _, tt := range tests {
		line, col := newFile(tt.data).Position(tt.off)
		if line != tt.line || col != tt.col {
			t.Errorf(
				"Position(%q, %d) = %d:%d, want %d:%d",
				tt.data,
				tt.off,
				line,
				col,
				tt.line,
				tt.col,
			)
		}
	}
}

// TestLine pins each line's bytes, including its trailing newline, and nil
// for line numbers out of range on both sides.
func TestLine(t *testing.T) {
	f := newFile("ab\n\ncd")

	tests := []struct {
		want string
		n    int
		nil  bool
	}{
		{"", -1, true},
		{"", 0, true},
		{"ab\n", 1, false},
		{"\n", 2, false},
		{"cd", 3, false},
		{"", 4, true},
	}

	for _, tt := range tests {
		got := f.Line(tt.n)
		if tt.nil {
			if got != nil {
				t.Errorf("Line(%d) = %q, want nil", tt.n, got)
			}
			continue
		}
		if string(got) != tt.want {
			t.Errorf("Line(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// A file ending in a newline has an empty last line, and it is a line.
func TestTrailingNewlineStartsAnEmptyLine(t *testing.T) {
	f := newFile("ab\n")

	if got := f.NumLines(); got != 2 {
		t.Fatalf("NumLines() = %d, want 2", got)
	}
	if got := f.Line(2); got == nil || len(got) != 0 {
		t.Errorf("Line(2) = %q, want an empty non-nil line", got)
	}
}

// LineStarts reports exactly what was recorded, in order.
func TestLineStarts(t *testing.T) {
	got := newFile("a\nbb\n\nc").LineStarts()
	want := []int{0, 2, 5, 6}

	if len(got) != len(want) {
		t.Fatalf("LineStarts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LineStarts() = %v, want %v", got, want)
		}
	}
}

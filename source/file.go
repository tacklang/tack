package source

import "sort"

// FileID identifies a source file. Spans carry it to say which file they
// point into.
type FileID string

// File is a source file's name and contents, plus the line starts the lexer
// records while scanning it.
type File struct {
	ID         FileID
	Filename   string
	Data       []byte
	lineStarts []int
}

// AddLineStart records that a line begins at byte offset ls. Offsets must be
// added in increasing order, starting with 0.
func (f *File) AddLineStart(ls int) {
	f.lineStarts = append(f.lineStarts, ls)
}

// Position returns the 1-based line and column of byte offset off. Columns
// count bytes, not characters.
func (f *File) Position(off int) (line, col int) {
	i := sort.Search(len(f.lineStarts), func(i int) bool {
		return f.lineStarts[i] > off
	}) - 1
	return i + 1, off - f.lineStarts[i] + 1
}

// Line returns the bytes of 1-based line n, including its trailing newline
// if it has one, or nil if there is no such line.
func (f *File) Line(n int) []byte {
	if n <= 0 || n > len(f.lineStarts) {
		return nil
	}
	start := f.lineStarts[n-1]
	end := len(f.Data)
	if n < len(f.lineStarts) {
		end = f.lineStarts[n]
	}
	return f.Data[start:end]
}

// LineStarts returns the recorded line start offsets. The slice is shared,
// not copied.
func (f *File) LineStarts() []int {
	return f.lineStarts
}

// NumLines returns how many lines have been recorded.
func (f *File) NumLines() int {
	return len(f.lineStarts)
}

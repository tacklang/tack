package ast

import "encoding/json"

// File is the root of the tree for one source file: its package clause, its
// top-level declarations in source order, and the comments found in it.
type File struct {
	Package  string
	Decls    []Decl
	Comments []Comment
	Span     Span
}

var _ Node = (*File)(nil)

// NewFile returns an empty File whose slices are non-nil, so that it
// marshals to [] rather than null.
func NewFile() *File {
	return &File{Decls: make([]Decl, 0), Comments: make([]Comment, 0)}
}

func (f *File) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the whole file.
func (f *File) Spanned() Span { return f.Span }

// String returns the tree as indented JSON, for debugging and the -ast flag.
func (f *File) String() string {
	js, _ := json.MarshalIndent(f, "", "  ")
	return string(js)
}

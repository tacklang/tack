package lexer

import "strconv"

// KeywordType identifies a reserved word. A Keyword token carries one as its
// value. Reserving a word here makes it unusable as an identifier, whether or
// not the parser accepts it yet.
type KeywordType int

// The keywords.
const (
	// InvalidKeyword is the zero value and names no keyword.
	InvalidKeyword KeywordType = iota

	// Lib and Pub control visibility.
	Lib
	Pub

	// Const through Extern start declarations. Var declares an immutable
	// variable and Mut a mutable one. Extern starts a function whose body
	// is written in C.
	Const
	Var
	Mut
	Type
	Func
	Struct
	Interface
	Map
	Extern

	// Package, Import and From structure a file. From is reserved
	// everywhere, though only an import uses it.
	Package
	Import
	From

	// If and Else are conditionals.
	If
	Else

	// For and Range are loops.
	For
	Range

	// Switch through Select are multi-way branches.
	Switch
	Case
	Default
	Select

	// Break through Return are jumps.
	Break
	Continue
	Goto
	Return

	// Async and Defer are concurrency and cleanup.
	Async
	Defer

	// True and False are the boolean literals.
	True
	False

	// NoError is the literal noerror, the error value that is no error.
	NoError

	// List starts a list type, list[T], as map would start a map type.
	List

	// CAllocator is the literal c_allocator, the allocator that lowers to
	// C's malloc, realloc and free.
	CAllocator

	// Union starts a union type, type Expr union { ... }, a value of
	// which holds one of its named cases. It comes last so that adding
	// it numbered no earlier keyword anew.
	Union
)

var keywordNames = map[KeywordType]string{
	Lib: "lib",
	Pub: "pub",

	Const:     "const",
	Var:       "var",
	Mut:       "mut",
	Type:      "type",
	Func:      "func",
	Struct:    "struct",
	Interface: "interface",
	Union:     "union",
	Map:       "map",
	Extern:    "extern",

	Package: "package",
	Import:  "import",
	From:    "from",

	If:   "if",
	Else: "else",

	For:   "for",
	Range: "range",

	Switch:  "switch",
	Case:    "case",
	Default: "default",
	Select:  "select",

	Break:    "break",
	Continue: "continue",
	Goto:     "goto",
	Return:   "return",

	Async: "async",
	Defer: "defer",

	True:  "true",
	False: "false",

	NoError: "noerror",

	List: "list",

	CAllocator: "c_allocator",
}

// String returns the keyword as written in source.
func (k KeywordType) String() string {
	if s, ok := keywordNames[k]; ok {
		return s
	}
	return "KeywordType(" + strconv.Itoa(int(k)) + ")"
}

// LookupKeyword returns the keyword ident spells, if it is one.
func LookupKeyword(ident string) (KeywordType, bool) {
	for kw, name := range keywordNames {
		if name == ident {
			return kw, true
		}
	}
	return InvalidKeyword, false
}

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

	// Own marks a parameter that takes its argument from the caller, own t
	// T, and the argument a call moves into one, f(own t). It comes after
	// Union for the same reason Union comes last.
	Own

	// Ensures starts a function's contract, written after its results:
	// func f(d int64) ensures d != 0, facts the compiler proves the
	// function makes true about its parameters whenever it returns. It
	// comes after Own for the same reason Own comes after Union.
	Ensures

	// Unreachable is a statement marking a place the compiler proves no
	// run reaches. It comes after Ensures for the same reason Ensures
	// comes after Own.
	Unreachable

	// Fallthrough is a statement that ends a switch case's body and
	// enters the next case's body without testing its values. It comes
	// after Unreachable for the same reason Unreachable comes after
	// Ensures.
	Fallthrough

	// Scope marks a parameter of a function type whose value never
	// escapes the call, scope f func(), the only parameter a closure can
	// be passed to. It comes after Fallthrough for the same reason
	// Fallthrough comes after Unreachable.
	Scope

	// Arena starts an arena type, arena[T], as list starts a list type:
	// an append-only store whose ids index it with no proof. It comes
	// after Scope for the same reason Scope comes after Fallthrough.
	Arena

	// Pool starts a pool type, pool[T], as arena starts an arena type: a
	// store whose elements can be removed one at a time, named by
	// handles that a removal makes stale. It comes after Arena for the
	// same reason Arena comes after Scope.
	Pool
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

	Own: "own",

	Ensures: "ensures",

	Unreachable: "unreachable",

	Fallthrough: "fallthrough",

	Scope: "scope",

	Arena: "arena",

	Pool: "pool",
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
	kw, ok := keywordsByName[ident]
	return kw, ok
}

// keywordsByName maps each keyword's spelling to the keyword, the reverse
// of keywordNames, so the lexer looks up each identifier it reads at once
// rather than by a scan of every keyword. No two keywords share a spelling
// (see TestKeywordSpellingsAreDistinct).
var keywordsByName = func() map[string]KeywordType {
	m := make(map[string]KeywordType, len(keywordNames))
	for kw, name := range keywordNames {
		m[name] = kw
	}
	return m
}()

// Package parser builds the abstract syntax tree for one Tack source file.
//
// It sits between the lexer and the resolver. It takes the lexer's token
// stream, which must end in an EOF token and already carry the semicolons
// that automatic insertion adds at line ends, and produces an *ast.File: the
// package name, every top-level declaration in source order (functions, and
// variables declared with var or mut), and the comments found between
// declarations. The short forms x := v and mut x := v are for function
// bodies only.
//
// The parser is recursive descent, with expressions parsed by precedence
// climbing using Go's operator precedence; every binary operator is
// left-associative. A statement that starts with a name is told apart by the
// token after it: := declares a variable, = assigns, and ( calls. It stops at
// the first syntax error and returns that error's diagnostics with a nil
// file, so a caller never sees a partial tree.
//
// The parser only checks shape, so it accepts a declaration like var x int
// whatever its mutability, and leaves whether it may lack a value to the
// type checker. Names are left unbound (every Ident.Def is nil) and types
// unresolved (every Kind is types.Invalid) for the resolver and type checker
// to fill in. Integer literals are the exception: they are
// decoded here, and one that does not fit in an int is reported here. Every
// node's span is tagged with the FileID the parser was created with.
package parser

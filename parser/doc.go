// Package parser builds the abstract syntax tree for one Tack source file.
//
// It sits between the lexer and the resolver. It takes the lexer's token
// stream, which must end in an EOF token and already carry the semicolons
// that automatic insertion adds at line ends, and produces an *ast.File: the
// package name, every top-level declaration in source order (functions, and
// variables declared with var or mut), and every comment in the file. The
// short forms x := v and mut x := v are for function bodies only.
//
// Comment tokens are set apart before parsing, so the grammar never sees
// them and a comment is accepted at every token boundary. A line comment
// ends at a newline, and the lexer has already inserted the semicolon that
// newline calls for, so a comment parses exactly as a newline in its place
// would. The comments are kept, in source order, in File.Comments.
//
// The parser is recursive descent, with expressions parsed by precedence
// climbing using Go's operator precedence (from loosest: ||, &&, the
// comparisons, + and -, then * / and %); every binary operator is
// left-associative. The prefix operators ! and - apply to the operand right
// after them, so they bind tighter than any binary operator. A - right before
// an integer literal is part of the literal instead, which is how the
// smallest int64 is written. Parentheses group without leaving a node of
// their own, but an expression whose first or last operand is written in
// them spans them: (a) == (b) covers both pairs, while (a) alone is the
// Ident a, spanning only the name. The keywords true and false are bool literals, so
// neither can start a statement or be declared as a name.
//
// A type is a name or an array type, [n]T, nested to any depth; in an array
// literal, [...]T leaves the length to the elements, and [] would be a
// slice, which is reported. An array literal is its type and its elements in
// braces, a comma allowed after the last, and an element that is only
// braces is a literal whose type comes from the enclosing one. Indexes,
// x[i], follow any operand and bind tighter than every operator. An
// element of a variable, a[i], can be assigned like a name, in every form of
// assignment; a statement that starts with one is told apart by what follows
// its brackets.
//
// A function's results are one type name, or a parenthesized list of them
// separated by commas, and a return holds its values in a list the same way.
//
// A statement that starts with a name is told apart by the token after it:
// := declares a variable, = or a compound operator such as += assigns, ++ or
// -- increments or decrements, ( calls, and a comma starts a declaration of
// several variables from one call's results, a, mut b := f(), where each mut
// belongs to the name after it, or an assignment of them, a, b = f(), which
// may not have a mut. These, with declarations starting with var
// or mut, are the simple statements. A for whose body's
// opening brace comes straight after the keyword has no condition, and one
// whose header holds semicolons has the three clauses init; cond; post, each
// optional, where init and post are simple statements and post may not
// declare a variable. One written for i := range x, for i, v := range x or
// for range x is a range loop; mut on one of its names, which are always
// immutable, and a third name are reported here, once the ranged
// expression is parsed.
//
// The parser stops at the first syntax error and returns that error's
// diagnostics with a nil file, so a caller never sees a partial tree.
//
// The parser only checks shape, so it accepts a declaration like var x int64
// whatever its mutability, and leaves whether it may lack a value to the
// type checker. Names are left unbound (every Ident.Def is nil) and types
// unresolved (every Type is types.Invalid) for the resolver and type checker
// to fill in. Integer literals are the exception: they are decoded here, to
// their exact value, and one outside the range every literal must lie in,
// the smallest int64 to the largest uint64, is reported here. A character
// literal becomes an ast.CharLit holding its one byte, which the lexer has
// already decoded and checked. Every
// node's span is tagged with the FileID the parser was created with.
//
// A node's span runs from the first byte of its first token to the last
// byte of its last one. No span covers a semicolon: one that ends a
// statement or declaration is left out, whether it was written in the
// source or the lexer inserted it at a line end, so a span ends at the last
// token that is not a semicolon and never takes in the spaces, comment or
// semicolon after it. The file's own span is the exception: it runs to the
// end of the input.
package parser

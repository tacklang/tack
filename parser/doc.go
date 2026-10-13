// Package parser builds the abstract syntax tree for one Tack source file.
//
// It sits between the lexer and the resolver. It takes the lexer's token
// stream, which must end in an EOF token and already carry the semicolons
// that automatic insertion adds at line ends, and produces an *ast.File: the
// package name, its imports, every top-level declaration in source order
// (functions, variables declared with var or mut, and struct types
// declared with type), and every comment in the file.
//
// Imports come right after the package clause, one per line, before every
// other declaration: import x from "path", where the path is any string
// literal. from is not a keyword; it is a name only an import gives a
// meaning, and may name anything elsewhere. A grouped import, import (...),
// an import without a name, and one after another declaration are each
// reported. Since every import is known before any declaration is parsed,
// and an import's name can never be redeclared, a name the file imports
// followed by a period and a name is parsed as one qualified ast.Ident,
// x.name, wherever a name may be: a callee, a value, an assignment target,
// a literal's type. In a type, where a field cannot be, any name, a period
// and a name is a qualified name, and the resolver reports one whose first
// name is not an import's. Anywhere else a name, a period and a name is a
// field selection. The
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
// slice, which is reported. A type declaration, at package level only, is
// type Name struct { ... }, its fields a name and a type each, separated by
// semicolons, written or inserted at line ends; struct {} has none. Other
// named types, anonymous struct types, methods, embedded fields and a
// visibility keyword on a field are each reported here, as is a type
// declared inside a function. A composite literal is its type and its
// elements in braces, a comma allowed after the last; an element may have a
// key, key: value, which is how a struct literal names its fields, and an
// element or value that is only braces is a literal whose type comes from
// the enclosing one. Which literal may have keys, and which needs them, is
// the type checker's to judge. Indexes, x[i], and field selections, x.f,
// follow any operand and bind tighter than every operator; a call right
// after a field selection would call a method and is reported. An element
// or a field of a variable, a[i].f, can be assigned like a name, in every
// form of assignment; a statement that starts with one is told apart by
// what follows its brackets and fields.
//
// A name followed by an opening brace starts a struct literal, Point{...},
// except in the header of an if, a for or a range loop, where, as in Go,
// the brace opens the body: there a struct literal must be written in
// parentheses, or inside a call, an index or another literal. A literal
// left bare there would end the header at its type name, so a body that
// starts with a name and a colon, which no statement can, is reported as
// one. A literal's key written in parentheses, (x): v, is marked on its
// KeyValue, since parentheses leave no node behind, for the type checker
// to reject.
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
// A switch is switch, an optional tag, and its cases in braces: case and a
// list of values separated by commas, or default, then a colon and the
// statements up to the next case, default or closing brace. A struct
// literal in the tag must be in parentheses, as in an if header. A second
// default is reported here, with a note at the first. There is no init
// statement, since an if has none, and fallthrough is not a keyword: it is
// an ordinary name.
//
// A defer is defer and then a block in braces, which may hold any
// statements. Anything else after defer, a lone call or assignment
// included, is reported as "defer must be followed by a block: defer
// { ... }", spanning the token after the keyword. Whether a return, break
// or continue in a deferred block leaves it, and a defer inside one, are
// the type checker's to report.
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

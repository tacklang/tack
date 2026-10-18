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
// literal. from is a keyword, reserved everywhere though only an import
// uses it, so it is never a name. A grouped import, import (...),
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
// A function declaration ends in its body, or in nothing, its signature
// followed by the end of the line, which sets Func.NoBody: whether a
// function may go without a body is the resolver's to judge. extern before
// func declares an extern func, whose body is written in C: it sets
// Func.Extern, takes the function's name as its C name, Func.CName, or the
// string literal extern("c_name") gives, recording its span in CNameSpan,
// and starts the function's span, and an extern func written with a body
// is reported at its opening brace. extern is a keyword, reserved
// everywhere, and anything after it but func or a parenthesized string is
// reported. pub or lib may come before extern, which the resolver rejects.
// A parenthesis right after func starts a method's receiver, func (b
// Builder) len() or func (mut b Builder) write(s string): one parameter,
// a comma after it allowed, which becomes the method's first parameter,
// Params[0], with Func.Method set. No receiver before a name, func () m(),
// several, and a variadic one are reported; func () with no name after
// it is a function's name left out. Which types may have methods, and
// whether one may be extern, is the resolver's to judge. Brackets right
// after a function's name hold its type parameters, func f[W Writer, V
// sink.Sink](...): each a name and a constraint, parsed as a type, with
// commas between them optional as between parameters, recorded in
// Func.TypeParams, the brackets' span in TypeParamsSpan. An empty list, a
// name with no constraint after it, and type parameters on a method or
// an extern func are reported; whether a constraint is an interface is
// the type checker's to judge. A call is never written with type
// arguments: a name followed by brackets and then a parenthesis, f[T](x),
// is reported, since nothing else takes that shape.
//
// Comment tokens are set apart before parsing, so the grammar never sees
// them and a comment is accepted at every token boundary. A line comment
// ends at a newline, and the lexer has already inserted the semicolon that
// newline calls for, so a comment parses exactly as a newline in its place
// would. The comments are kept, in source order, in File.Comments.
//
// The parser is recursive descent, with expressions parsed by precedence
// climbing using Go's operator precedence (from loosest: ||, &&, the
// comparisons, + - | and ^, then * / % << >> & and &^); every binary
// operator is left-associative. The prefix operators ! - and ^ apply to the
// operand right after them, so they bind tighter than any binary operator. A - right before
// an integer literal is part of the literal instead, which is how the
// smallest int64 is written. Parentheses group without leaving a node of
// their own, but an expression whose first or last operand is written in
// them spans them: (a) == (b) covers both pairs, while (a) alone is the
// Ident a, spanning only the name. The keywords true and false are bool literals, and
// noerror is a NoErrorLit, the error that is no error, and c_allocator a
// CAllocatorLit, the Allocator that is C's malloc, so none of the four can
// start a statement or be declared as a name.
//
// A type is a name, an array type, [n]T, a view type, []T, a list type,
// list[T], list being a keyword, a map type, map[K]V, map being one, or a
// function type, func(A, mut B, own C) R, an ast.FuncType: its parameters'
// types in parentheses, mut or own before one that is so, with commas between them,
// none named, a name before a type reported, and none variadic, then its
// results as a function's are written; a name and a type after one with
// no results start the next parameter of a list it is written in, f func()
// n int64. Nested to any depth; in an array literal, [...]T leaves the length to the
// elements. Where a type is allowed is the type checker's to judge, so a
// view type is parsed wherever a type is. A list or a map type is parsed
// where an operand is too, as a ListType or a MapType, which make's first
// argument is, and one
// followed by braces is a literal, which the type checker reports, as it
// does a list type anywhere else a value is wanted. func where an operand
// is would start a function literal, which Tack does not have, and is
// reported. In an expression, a view type
// followed by parentheses is a conversion, []uint8(s), a Call whose Fn is
// the ViewType, and one followed by braces is a literal, which the type
// checker reports. A type declaration, at package level only, is
// type Name struct { ... }, its fields a name and a type each, separated by
// semicolons, written or inserted at line ends; struct {} has none. Or it
// is type Name interface { ... }, an InterfaceType, its methods separated
// the same way, each an IfaceMethod: mut, if its receiver is mut, a name,
// parameters and results, as a function's signature is written after its
// name; interface {} has none. Visibility on a method, a name alone,
// which would embed an interface, a field and a method's body are
// reported. Or it is type Name union { ... }, a UnionType, its cases
// separated the same way, each a UnionCase: a name, and the type of its
// payload unless the case ends after its name; union {} has none.
// Visibility on a case, which takes its union's, and a value given to
// one, eof = 1, which would be a tag, are reported. A union's
// construction, Expr.int(5) or Expr.eof, parses as a method call or a
// field selection does, and a switch's case values as any values do,
// int(n) a call: which of them build or match a union is found from types
// later. Other named types, anonymous struct and union types and embedded
// fields are each reported here, as is a type declared inside a function. A field may
// start with pub or lib, and so may a package-level function, variable
// (var or mut) or type declaration, whose span then starts at the keyword;
// the parser records the visibility and the keyword's span, and anything
// else after the keyword, a second one included, is reported. Whether the
// keyword is allowed where it is written is the resolver's to judge; one
// on a local or a parameter is not parsed as a keyword there and is
// reported as unexpected. A composite literal is its type and its
// elements in braces, a comma allowed after the last; an element may have a
// key, key: value, which is how a struct literal names its fields, and an
// element or value that is only braces is a literal whose type comes from
// the enclosing one. Which literal may have keys, and which needs them, is
// the type checker's to judge. Indexes, x[i], slices, x[i:j], field
// selections, x.f, and type assertions, x.(T) with any type T or x.(type),
// follow any operand and bind tighter than every operator; a field
// selection followed by parentheses is a method call, x.m(args), a Call
// whose Fn is the Selector, whose X is the receiver. mut before an operand
// that is a method call, mut x.m(args), makes the receiver, what comes
// before the last .m(, a MutArg, the call of a mut receiver; mut before a
// call in parentheses, mut (x.m()), covers the call. own before an operand
// makes an OwnArg of it, whatever the operand, a method call included,
// since no receiver is ever own; which ones are allowed is the type
// checker's to judge. own before an interface's method name, which would
// take its receiver as own, is reported. A statement may be a
// method call, x.m(), f().m() or a[i].f.m(), told apart from an
// assignment by looking past its brackets, fields, calls and assertions,
// each matched pair scanned once, to see that it ends in .name(...); and
// one starting with mut is a mut receiver's call, mut x.m(), rather than
// a declaration, mut x := v, by the same look ahead. A for header
// starting with one is a condition, or a call statement before a
// semicolon, as for any call. Where an
// assertion, and which types, may be written is the type checker's to
// judge. Either bound of a slice may be left out, x[:j], x[i:] or
// x[:], and a second colon, Go's three-index form x[i:j:k], is reported
// at that colon. An element
// or a field of a variable, a[i].f, can be assigned like a name, in every
// form of assignment; a statement that starts with one is told apart by
// what follows its brackets, fields and assertions, which the resolver
// then reports as a target when it is an assertion.
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
// A contract may follow them, ensures and a condition, before the body,
// parsed into Func.Ensures, the keyword's span in EnsuresSpan; as in an
// if's header, a name before an opening brace there is not a struct
// literal, and one written without parentheses is reported. ensures is a
// keyword, so it is never a name.
// A parameter is a name and a type, with mut before the name for a mut
// parameter, mut p T, or own for an own one, own p T, whose span then
// starts at the keyword, mut and own together, in either order, reported
// at the second, and ... before the
// type for a variadic one, args ...T, whose TypeExpr is then the element
// type T. A variadic parameter followed by another, a second variadic
// included, is reported at its ..., and so is a ... wherever else a type
// is parsed, a result's, a variable's, a field's or an element's: only a
// parameter's type may follow one. A call's last argument may be
// followed by ..., a spread, f(a, v...), which marks the Call rather than
// wrapping the argument in a node; one after any other argument is
// reported at the .... Whether the callee can take a spread, and the
// count of arguments around it, are the type checker's to judge. mut before an operand
// makes a MutArg of it, mut x, binding as a prefix operator does, so
// mut a + 1 is (mut a) + 1. It is parsed wherever an operand may be, so
// that the type checker can report one anywhere but as a call's argument,
// and before anything that is not a place, with a precise message.
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
// an ordinary name. A type switch has x.(type) as its tag, and its case
// values are types, parsed as types are; a name and := after the keyword,
// switch v := x.(type), bind its value, recorded as the Switch's Bind,
// and the parser gives each case a variable of its own under that name.
// A name bound to a tag that is not x.(type) is reported as the init
// statement a switch does not have, and mut on the name as a range loop's
// is.
//
// A defer is defer and then a block in braces, which may hold any
// statements. Anything else after defer, a lone call or assignment
// included, is reported as "defer must be followed by a block: defer
// { ... }", spanning the token after the keyword. Whether a return, break
// or continue in a deferred block leaves it, and a defer inside one, are
// the type checker's to report.
//
// After a syntax error the parser records it and recovers (see
// recover.go): it skips the rest of the statement, to the semicolon at
// its own depth of braces or the brace that closes its block, or the rest
// of the declaration, to the next line that starts one, puts an
// ast.BadStmt or ast.BadDecl in the tree where the skipped text was, and
// goes on. A BadDecl records the name the declaration was declaring, and
// whether it was a method's, when the parser got as far as the name. Parse
// returns the file and every error, so the tree holds a bad node exactly
// when there are errors; the resolver and the type checker run on it all
// the same, and report nothing about what the bad nodes hide (see their
// packages). Only a program nested too deeply gives no tree.
// Limit makes the parser stop past a number of errors.
//
// Recovery reads what a line starts as, without reading past it, to keep
// the damage to the construct an error is in: a declaration line the
// failed construct consumed is given back, a } or a ; inside a damaged
// line is part of the damage rather than its end, and a brace left out
// or one too many, which closes a block too soon, is reported once, its
// switch's clauses, its function's spilled body and its leftover brace
// skipped with no errors of their own (see spill.go). What only
// indentation tells apart is reported as it parses: a brace left out
// before declarations that read as statements, var x int64 = 1, is
// reported missing at the next line only a declaration starts, those
// declarations taken for the block's statements, and a body's line that
// reads as a declaration, pub var x int64 = 1, ends the body there. A
// lookahead for a closing bracket stops at the end of its statement, so a
// file of unclosed brackets parses in linear time.
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
//
// A program nests at most maxNesting levels deep, a thousand, counted
// together across expressions, statements and types (see nest), so the
// parser's recursion and every later stage's is bounded, and the C the
// program becomes stays within what clang parses. The first construct
// past it is reported, once, at the token that opens it, and parsing
// stops: nothing nests further, and the file has no tree.
//
// An else if is the else branch of the if before it, holding that if
// alone, as in the source's meaning, but a chain of them is parsed in a
// loop, not by recursion, so a chain flat in the source costs no stack
// however long it is; every if of the chain spans to the chain's end.
package parser

// Package ast defines the syntax tree for a Tack source file.
//
// The parser produces a *File from the lexer's tokens. Later passes read the
// tree and fill in fields the parser leaves empty: the resolver sets
// Ident.Def to the declaration a name refers to, and the type checker sets
// Param.Kind, Var.Kind and Func.ResultKind to the types those declarations
// denote. A Var is both a declaration, so a name can refer to it, and a
// statement, so it can appear in a function body. Code
// generation then reads the fully annotated tree. Until a pass has run, the
// fields it owns hold their zero values (nil, or types.Invalid).
//
// Nodes fall into three closed families, Decl, Stmt and Expr, each sealed by
// an unexported marker method so that no type outside this package can join
// one. Every node carries a Span locating it in the source, which is what
// lets every diagnostic point at the code it is about.
//
// Walk is the one place that knows how nodes nest. A pass that needs to visit
// every node goes through it, so a new node type only has to be taught to
// Walk once.
package ast

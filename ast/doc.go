// Package ast defines the syntax tree for a Tack source file.
//
// The parser produces a *File from the lexer's tokens. Later passes read the
// tree and fill in fields the parser leaves empty: the resolver sets
// Ident.Def to the declaration a name refers to, a type name's included,
// and the type checker sets Param.Type, Var.Type, Func.ResultType and
// TypeDecl.Type to the types those declarations denote, the type of every operator expression and integer literal (a
// character literal is always a uint8, so it has no field for one),
// Binary.Const and Unary.Const to the value of an integer constant
// expression, Var.Const to a package-level variable's computed initial value,
// CompositeLit.Type, Index.Type and Selector.Type to an array or struct
// literal's, an element's and a field's type, Call.Const to a len the checker can compute, and File.Types to the
// table that gives those types meaning. The types as
// written in source are kept apart, in Param.TypeExpr, Var.TypeExpr and
// Func.Result. A Var is both a declaration, so a name can refer to it, and a
// statement, so it can appear in a function body; at package level it is a
// top-level declaration, as is a TypeDecl, which declares a struct type: a
// StructType, whose Fields are part of it rather than nodes of their own.
// A struct literal is a CompositeLit whose elements are KeyValues, a field's
// name and a value, and a field read or written is a Selector; neither the
// key nor the selected name is an Ident the resolver binds, since a field is
// found through the type of what holds it. Code
// generation then reads the fully annotated tree. Until a pass has run, the
// fields it owns hold their zero values (nil, or types.Invalid).
//
// Nodes fall into three closed families, Decl, Stmt and Expr, each sealed by
// an unexported marker method so that no type outside this package can join
// one. Every node carries a Span locating it in the source, which is what
// lets every diagnostic point at the code it is about.
//
// File.String, and the driver's --ast and --res dumps, render the tree as
// JSON. An Ident's Def is rendered as a DeclRef, the declaration's kind,
// name and name span, rather than followed: after resolution a recursive
// call's Def points back at the function containing it, a cycle the encoder
// would otherwise reject.
//
// Walk is the one place that knows how nodes nest. A pass that needs to visit
// every node goes through it, so a new node type only has to be taught to
// Walk once.
package ast

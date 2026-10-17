// Package ast defines the syntax tree for a Tack source file, and the
// Package and Program that group files.
//
// The parser produces a *File from the lexer's tokens. The loader groups
// the files of one directory into a Package, and the packages a build
// needs into a Program, whose last package is the one being built; the
// later passes take a Program. File.PackageSpan is the span of a file's
// package clause's name, where a problem with the clause is reported. Later passes read the
// tree and fill in fields the parser leaves empty: the resolver sets
// Ident.Def to the declaration a name refers to, a type name's included,
// and the type checker sets Param.Type, Var.Type, Func.ResultTypes and
// TypeDecl.Type to the types those declarations denote, the type of every operator expression and integer literal (a
// character literal is always a uint8, so it has no field for one),
// Binary.Const and Unary.Const to the value of an integer constant
// expression, Var.Const to a package-level variable's computed initial value,
// CompositeLit.Type, Index.Type and Selector.Type to an array or struct
// literal's, an element's and a field's type, Call.Const to a len the checker can compute, and Program.Types,
// and File.Types of every file, to the table that gives those types meaning. The types as
// written in source are kept apart, in Param.TypeExpr, Var.TypeExpr and
// Func.Results. A Var is both a declaration, so a name can refer to it, and a
// statement, so it can appear in a function body; at package level it is a
// top-level declaration, as is a TypeDecl, which declares a struct type: a
// StructType, whose Fields are part of it rather than nodes of their own;
// or a union type, a UnionType, whose UnionCases are part of it likewise,
// each a name and its payload's type, if any. A union is built by naming
// a case through its type, Expr.int(5), a Call whose Fn is a Selector, or
// Expr.eof, a Selector, whose operand the resolver binds to the union's
// TypeDecl; Construction reads either, and Call.Receiver gives such a call
// none. A switch on a union matches its cases: its case values are
// patterns, CasePattern reads one, a name, eof, or a name and a binding,
// int(n), and a clause naming one case binds its payload to its
// CaseClause's Var, which the resolver declares; the type checker sets
// Switch.Exhaustive when the clauses name every case. A package-level
// union's computed value is a UnionConst.
// A Func, a package-level Var, a TypeDecl and a Field carry a Visibility,
// Private unless pub or lib was written, with the keyword's span; a
// Package carries the name of the module it is in, which lib is relative
// to, and the directory it was read from and the C files in it, which the
// driver compiles. A Func declared extern func has its body in C: Extern
// is set, CName holds the C symbol, and like any function written without
// a body it has NoBody set and an empty Body. A Switch's CaseClauses are likewise part of it: each holds its values
// and its body, which Walk visits, but is not a node.
// A type switch is a Switch whose Tag is a TypeAssert with no TypeExpr,
// x.(type), and whose case values are types; Bind is the v of switch v :=
// x.(type), and each CaseClause has a Var of its own under that name,
// which the type checker gives the case's type, recording the types a
// case names in CaseClause.Types. A type assertion, x.(T), is a
// TypeAssert too, whose Type the checker sets to T.
// A Defer holds the statements of its defer { ... } as a body, a block of
// their own, which may hold another Defer; the checker rejects that.
// A Param written mut p T has Mut set. A call's argument written mut x is
// a MutArg holding x, the place the parameter refers to; the parser
// accepts one before any operand, and the type checker reports one
// anywhere but as a call's argument.
// A method, func (b Builder) len(), is a Func with Method set, whose
// receiver is its first parameter, Params[0] (Func.Recv); the resolver
// records each method on its type's TypeDecl.Methods, a struct's or a
// union's, and the type on Func.RecvType. A method call, x.m(args), is a
// Call whose Fn is a Selector, whose X is the receiver, a MutArg for mut
// x.m(args); Receiver returns it and Operands every value the call
// passes, the receiver first. The type checker records the method it
// calls in Call.Method, and Callee gives a call's function either way.
// A generic function, func f[W Writer](w W), has its type parameters in
// Func.TypeParams, each a TypeParam, a declaration a name written as a
// type binds to, whose Constraint is an interface's name; the type
// checker sets TypeParam.Type and TypeParam.Iface. A TypeDecl whose
// TypeExpr is an InterfaceType declares an interface, whose methods are
// IfaceMethods, each with its parameters, results and whether its
// receiver is mut. A method call on a value of a type parameter has as
// Call.Method a stand-in the type checker makes, with Func.Interface set
// to the interface's method. A call to a generic function records the
// type arguments the checker inferred in Call.TypeArgs, and CallResults
// gives its results with them in place. The checker records every
// instance of a generic function the program needs, a function and its
// type arguments, in Program.Instances, and on every file, as it records
// Types.
// A Param written args ...T, the last of a function's, has Variadic set
// and T as its TypeExpr; the type checker gives it the view type []T. A
// call whose last argument is followed by ..., f(v...), has Spread set;
// the parser accepts it only after the last argument, and the type
// checker only in a call to a variadic function, whose variadic parameter
// v is then passed to whole.
// A list type written list[T] is a ListType, and a map type written
// map[K]V a MapType, wherever a type is written, and as an operand, as
// make's first argument; the type checker records the type of a call to
// make, append, reserve, pop, remove, replace, put or delete on Call.Type, and marks a name
// whose use moves its variable's value with Ident.Move. c_allocator is a
// CAllocatorLit.
// A view type written []T is a ViewType, wherever a type is written; the
// type checker accepts it only as a parameter's type. A conversion to one,
// []uint8(s), is a Call whose Fn is the ViewType, and whose Conv the type
// checker sets, since the resolver sets Conv only for a basic type's name.
// A slice, x[i:j], is a Slice, its Lo or Hi nil where a bound is left out,
// x[:j] or x[i:]; the type checker sets Slice.Type, a string for a
// string's slice and a view for an array's or a view's. A slice is never
// a place: the parser takes one wherever an index may be written, and the
// type checker reports one assigned or passed as mut.
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

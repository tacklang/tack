// Package resolver binds every name in a parsed program to what it refers
// to.
//
// It runs after the loader and before the type checker, taking an
// *ast.Program and returning the diagnostics it found. Packages are
// resolved in the program's order, every package after those it imports,
// so a qualified name, x.name, finds name among the package-level names of
// the package the import x stands for, and names it only if it is visible
// from the package being resolved: pub, or lib with both packages in one
// module (ast.Package.Module). Anything else is reported, name is not pub
// in package path, or name is lib in module a and cannot be used from
// module b. In package main, which nothing can import, pub and lib are
// reported on a declaration; a field may take either, which only says
// which packages' fields loops list it. Elsewhere, a mut global may be
// neither. A global that is pub or lib is never reported as
// unused, since packages that may read it may not be in the program. The
// visibility of fields, and of the types a declaration exposes, is the
// type checker's to judge.
//
// Imports are per file. Each file has a scope of its own holding its
// imports, enclosed by the package's global scope and enclosing each of
// its functions. An import's name may not be reserved, a builtin's, any
// package-level name of the package, in any file, or another import's of
// its file, and a file may not import one package twice; a local or
// parameter may not reuse it either, which is reported as an imported
// package that cannot be redeclared. The name alone is a package, not a
// value, a type, a function or a variable. An import no qualified name of
// its file uses is reported as imported and not used, unless its
// declaration was reported, and a qualified name through such an import is
// not reported again. A qualified name binds Ident.Def to the function,
// global or type it names, and marks a global read; one naming nothing is
// undefined. A package other than main may declare nothing named main. A package may be
// several files: every package-level name of every file is declared before
// anything is resolved, so each is visible in all of them, in any order of
// declarations and of files, and a name declared in two files is reported
// as declared twice, with a note in the other file. A global that is never
// read is reported only after every package has been resolved. It checks
// the program's shape first:
// the package being built must be package main, which must declare a main
// function that is not extern, takes no parameters and declares no results;
// with Resolver.MainOptional, which the driver's --check sets, the package
// being built may be any package, as a library's is, and a missing main
// function is left to MissingMain, so the caller can report it and check
// on;
// no function may be declared twice,
// none may reuse a builtin's name, and only an extern func, whose body is
// in C, may be declared without a body, and a method never may. An extern func can be neither pub
// nor lib: its package exports a Tack function that calls it. It then walks each function body with that
// function's parameters in scope, setting Ident.Def on every callee, and on
// every parameter and variable reference and assignment target. A
// function literal's parameter and result types are resolved where it is
// written, its parameters declared in a scope of their own nested there,
// none reusing a name visible there, and its body resolved inside that, so
// it sees, and binds to, every variable and parameter around it. A
// function's contract, Func.Ensures, is resolved first, with the
// parameters in scope and nothing its body declares; which names it may
// use is the type checker's to judge. A call whose
// callee is a type name, such as int8(x), is a conversion: it binds to no
// declaration, and the resolver records the type in Call.Conv instead. A
// conversion to a view type, []uint8(s), has the names in its type bound
// as any type's are, and leaves Call.Conv to the type checker, which
// builds the view type, and so does a conversion to an id, a span or a
// handle type, id[T](n), which the type checker reports. Type
// names are reserved like keywords: nothing may be declared with one but
// a field or a method (see memberName), and one is never read, called or
// assigned as a name; id, span and handle
// are type names too, written only with an element type, so each alone as
// a type is reported. A callee that
// names a builtin is bound to a nil *ast.Func, since builtins have no
// declaration in the tree. A callee is bound to the function or builtin
// of its name first, even where a variable or parameter of that name is
// visible, which is recorded in Ident.Hidden for the type checker, and a
// variable never read otherwise is reported unused with a note at the
// call; a callee no function or builtin has is bound to the variable or
// parameter of its name, a global included, which the call calls through,
// and marks a variable read. A name used as a value that no parameter,
// variable or type has where it is used is bound to the function of the
// package of that name, plain or qualified, whose value it is; a
// builtin's name is no value, `cannot use the builtin len as a value;
// only a declared function can be one`. The names in a function type,
// func(A) R, are bound as any type's are. The builtins are those of the builtin.Set New
// is given, print and exit, which the compiler implements, and the generic
// ones, len, make, append, reserve, pop, remove, swap, replace, put,
// delete, has and drop.
//
// Package-level variables and types share one namespace with functions and
// builtins, and form the outermost scope, enclosing every function, so a
// global can be used from any function, and a type anywhere, whatever the
// order of declarations. Functions are declared first, then types, then
// globals, so a type or global that clashes with an earlier kind is the one
// reported. Every global is declared before any global's type or
// initializer is resolved, so either may name a global declared after it;
// the type checker says whether that use is allowed. A name bound to a type is reported where it is used as a
// value, assigned or called, and a name written as a type that is bound to
// anything but a type is reported too; one bound to nothing is left for the
// type checker, which reports it as an unknown type.
//
// A struct's fields are resolved after every package-level name is
// declared, so a field's type may name any type. A name in an array length
// in it is bound too, to a global if it names one, and the type checker
// then reports it, since a field's length names no variable. Field names are a namespace of their own, one per struct, so
// a field may share a name with anything outside it, but not with another
// field of its struct. A field may be named like any builtin, a builtin
// type's name included, span or int64, and is never _: a field selection,
// p.x, and a key in a struct literal, x: v, are the only places a field's
// name is written, and they name a field the type checker finds through
// the type of what holds it, so neither is bound here and neither can be
// read as a type or a builtin.
//
// A union's cases are resolved with the structs' fields: each payload's
// type, and each case's name, which is not reserved, unlike a field's,
// since a switch's clause writes it bare, case int64(n), where a switch on
// an any writes types, and is declared once per union. A selector whose operand names a type,
// plain or qualified, builds a union's case, Expr.int(5) or ast.Expr.eof,
// or names a method of the type, a method expression, Pair.first or
// lexer.Token.Kind, used as a value or called (see ast.MethodExpr): the
// operand is bound to the type's TypeDecl, a struct's, a union's or an
// interface's, the qualified one only if this package may see it, or to
// the type parameter it names, and a basic type's name, int64.abs, which
// nothing declares, is left bound to nothing; the case's or the method's
// name is left to the type checker, which finds it in the type and
// reports a type with neither. A call through a type has no receiver
// (ast.Call.Receiver). A union type of the package may have methods, as a
// struct type may (below).
//
// The values of an expression switch's cases may be a union's patterns,
// which the resolver cannot tell from values, since that depends on the
// tag's type; union.go's Patterns says how each is bound. A value whose
// last name is a variable or parameter visible at the switch is resolved
// as a value, and any other of the shape name or name(x) is bound as a
// pattern, reporting nothing: a clause of one such value whose binding is
// a name visible nowhere declares it, its CaseClause's Var, at the start
// of its body, never reported unused here, and in a clause of several the
// name is rejected in the body. The type checker reports what is wrong
// with either reading once it knows the tag's type.
//
// A method, func (b Builder) len(), is a function whose first parameter
// is its receiver, so its body is resolved as any function's, the
// receiver in scope as a parameter under every parameter's rule, type
// names reserved, _ declaring nothing, and no other parameter, local or
// global sharing its name. Its name is not one of the package's: methods are not declared
// with the functions, so one may share a name with a function, a global,
// a builtin or main. Once every receiver's type is bound, each method is
// recorded on its type, in TypeDecl.Methods, and its type in
// Func.RecvType. Its receiver's type must be a struct or union type
// declared in the package; a type of another package, an interface, a
// basic type, an array, a view or a list is reported, and a name bound to
// nothing is left for the type checker to report as an unknown type. A
// method's name follows a field's rule, a builtin type's allowed, since a
// method is written only after a dot, and may not be a field's of its
// struct, a case's of its union, or another method's of its type, so a
// union's method and its cases, Expr.int(5) built through the type and
// e.m() called on a value, never share a name. An extern method is
// reported: a method calls an extern func instead. A method call, x.m(),
// has its receiver resolved as a value, and mut x.m() as a mut argument;
// which method it calls is the type checker's to find, from the
// receiver's type, so a binding of nothing but _ of a method call's
// results is left to the type checker to report too.
//
// An interface, type Writer interface { ... }, is declared as a struct
// type is, in the package's namespace, and is a TypeDecl a name written
// as a type binds to; where it may be written is the type checker's to
// judge. Its methods' parameter and result types are resolved in its
// file's scope; a method's name is in its interface's namespace, follows
// a field's naming rule, a builtin type's allowed, and is declared once, and its parameters are
// named once each, none reserved, but may share names with anything
// else, since a method of an interface has no body that could see them;
// any number of them may be _.
// A generic function's type parameters form a scope of their own,
// enclosing its parameters and nested in its file's: they are declared
// first, under a parameter's naming rules, so none is reserved, a
// package-level name, an import's or another type parameter's, then
// their constraints are resolved in that scope, then the function's
// signature, which may name them, and its body, in which a parameter or
// local cannot reuse one's name. A type parameter is a type: a name bound
// to one is reported as a value, called or assigned, as a type's name is.
// Data, the builtin constraint, binds to nothing, as a basic type's name
// does, and no declaration, parameter or local may take its name. In
// fields(T), T is resolved as a type, and in v.[f], v as a value and f as
// a name read, which the type checker requires to be a fields loop's
// variable. In a switch on a type parameter, the tag and each case value
// are resolved as types, but the name a case's list[E] or map[K]V writes
// for its element or value type, which declares an ast.TypeParam of the
// case's own in the case's block, as a type switch's variable is
// declared, so only that case's body names it; it must be a new name, not
// a type's (see paramswitch.go).
//
// A parameter named _, of a function, a method, its receiver, a function
// literal or an interface's method, declares nothing: any number of a
// signature's parameters may be _, none clashes with another, and a _ in
// the body is reported as any other _ is, cannot use _ as a value or
// cannot assign to _, since no scope holds one. The type checker never
// warns of one as unused.
//
// Each block is a scope: a function's parameters come next, its body is
// nested in that, and each branch of an if, and each case of a switch, is
// nested in the block holding the statement; a switch's tag and case
// values are resolved there too, a type switch's case values as types. A
// type switch's binding, v in switch v := x.(type), is checked where the
// switch is, as a variable declared there would be, but declared in no
// scope there: each case declares its own variable under the name at the
// start of its block, so the tag cannot see it, and the binding is
// reported unused, once, at the binding, when no case reads its
// variable. A type assertion, x.(T), binds x as a value and T as a type,
// and is reported as an assignment's target. What a defer defers, its block, is a
// block nested in the one holding the defer, resolved where
// the defer is written, so it sees only the names declared before it, though
// it runs later, and a variable it reads counts as read. A loop is a scope of its own, holding any variable its init
// statement or range declares, with the body nested in it. What a range
// loop ranges over is resolved outside that scope. A region, region r
// { ... }, is a scope of its own too, holding r, an immutable variable,
// with the body nested in it, so nothing after the region can name r; r
// is reported unused, as a range loop's variable is, when the body never
// reads it, and _ binds nothing. A variable is visible from its declaration to the end of its block,
// and its value is resolved before it is declared, so it cannot refer to
// itself. The variables a, b := f() declares are declared in order after f()
// is resolved, as if each were declared on its own, and the targets of
// a, b = f() are bound after f() is resolved, without counting as reads.
//
// The names in a type as written, an array length that names a constant,
// a list type's element type, make's first argument included, are bound too: a local's in its own scope, a global's and a signature's at
// package level. make's first argument, when it is a name of a type, as
// in make(T, alloc) of a Data type, is bound as a type, not a value, and
// id, span, handle or Data written there is reported as a type written
// wrongly, not as a value. The target of an assignment to an element or a field,
// a[i].x, binds its variable without counting as a read of it, and its
// index like any value. A mut argument, mut x, binds x as a value, so it
// counts as a read: the callee may read the variable through it; an own
// argument, own x, likewise, the callee reading the value it takes. A slice,
// x[i:j], is bound as a value wherever it is written, an assignment's
// target included, which the type checker then reports, since a slice is
// never a place.
//
// The name _ (ast.Blank) binds nothing. In a, _ := f() or a, _ = f() it
// discards the result in its position and is neither declared nor bound;
// a list of nothing but _ is reported, as is mut on _, except from a call
// to a function whose last result is written error: that drops the error,
// which the type checker reports, since the call made on its own, which
// this error would suggest, drops it too. In a range loop with
// two variables, for _, x := range a, a _ declares nothing, and both being
// _ is reported. Anywhere else, _ is
// reported: as a declared name of any kind, as a value, and as the target
// of a single assignment, increment or decrement. No variable or parameter may reuse a name visible where it is
// declared, whether a parameter's or a variable's, package-level ones
// included. A local may reuse a function's or a builtin's name, since a call
// always resolves to a function. A variable that is never read, only
// assigned or not mentioned at all, is reported at the end of its block, and
// a package-level one after every function.
//
// Every name it reports as undefined gets its own diagnostic, so one run
// lists them all. An error that follows from one already reported is not
// reported, as in the type checker: a use or assignment of a name whose
// declaration was rejected, being reserved or already declared, is not
// reported, and a variable whose declaration had an error, or that was
// named where a type was expected, is not reported as unused.
//
// # After errors
//
// The resolver runs whatever the loader reported, on trees the parser
// recovered from syntax errors in, with bad nodes (ast.BadDecl,
// ast.BadStmt) where the damaged source was, and imports the loader could
// not load bound to no package; a bad node is skipped. A name that names
// nothing usable because of an error already known is invalid: it is bound
// to ast.Unbound, and the type checker reports nothing more about it or
// anything that uses it. That is a name it reported, such as an undefined
// one or a type used as a value; a use of a name whose declaration was
// rejected, in the scope of the use or any scope out to the declaration it
// would otherwise find, since which one it means is unknown, the
// declaration found counting as read; a qualified name through an import
// that was rejected or not loaded, which is not reported unused either; a
// call by the name of a builtin the package tried to redeclare; and, not
// reported, a name damaged source may have declared: the name a bad
// declaration of the package was declaring, any name when one got to no
// name, the name of an import a bad declaration may have been, and any
// name used after a bad statement of its function. A name written as a
// type that names nothing is left for the type checker to report unless
// damaged source may have declared it, when it is bound to ast.Unbound.
// Damaged source may also have used what it names: no import of a file
// with a bad node, no global of a package with one, and no variable
// declared before a bad statement of its function is reported unused, and
// a missing main is not reported when a bad declaration may have been it.
// Resolve adds the errors it reports to the program's Errors, which keeps
// code from being generated from it (see codegen.Refuse).
//
// What is reported unused, a local, a global, a type switch's binding or
// an import, is a warning (diag.Warning), not an error: it stops nothing
// and is not counted in Errors; each is marked diag.Diagnostic.Unused, its
// span the name, which an editor may dim. A local never read that a call
// or a binding of several names gave a value, declaring or assigning it,
// is not reported here but marked ast.Var.UnusedDeferred: it may hold an
// error or an ok, which never read is an error, and only the type checker
// knows; it reports that, or the warning (see package typeck). A variable warned of is marked
// ast.Var.Unused, every clause's variable of a binding warned of
// included, so the backend can declare it as one C need not see read.
//
// When the program has no errors, every Ident in the tree used as a
// value, a callee or an assignment target is bound to a declaration, which
// the type checker and code generator rely on; a name written as a type is
// bound unless it is a basic type's, or names nothing, which the type
// checker reports. Only a program with errors has names bound to
// ast.Unbound.
//
// An else branch that is a single if, as an else if is, opens no scope,
// since it declares nothing: a chain of else ifs is resolved in a loop,
// every if of it in the scope the first one is in, so its length costs
// neither stack nor longer lookups.
package resolver

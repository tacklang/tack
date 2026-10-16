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
// reported wherever written, on a declaration or a field; elsewhere, a mut
// global may be neither. A global that is pub or lib is never reported as
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
// no function may be declared twice,
// none may reuse a builtin's name, and only an extern func, whose body is
// in C, may be declared without a body, and a method never may. An extern func can be neither pub
// nor lib: its package exports a Tack function that calls it. It then walks each function body with that
// function's parameters in scope, setting Ident.Def on every callee, and on
// every parameter and variable reference and assignment target. A call whose
// callee is a type name, such as int8(x), is a conversion: it binds to no
// declaration, and the resolver records the type in Call.Conv instead. A
// conversion to a view type, []uint8(s), has the names in its type bound
// as any type's are, and leaves Call.Conv to the type checker, which
// builds the view type. Type
// names are reserved like keywords: nothing may be declared with one, and
// one is never read or assigned as a name. A callee that
// names a builtin is bound to a nil *ast.Func, since builtins have no
// declaration in the tree. The builtins are those of the builtin.Set New
// is given, which std/builtin declares, and the generic ones, len, make,
// append and reserve.
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
// field of its struct, and the reserved names are reserved for fields too.
// A field selection, p.x, and a key in a struct literal, x: v, name a field
// the type checker finds through the type of what holds it, so neither is
// bound here.
//
// A union's cases are resolved with the structs' fields: each payload's
// type, and each case's name, which follows a field's rule, reserved
// names excluded, once per union. A selector whose
// operand names a union type, plain or qualified, Expr.int(5) or ast.Expr.eof,
// builds a value of it: the operand is bound to the union's TypeDecl, the
// qualified one only if this package may see it, and the case's name is
// left to the type checker, which finds it in the union. A struct type's
// name there is reported as a type used as a value, as before. A method
// on a union type is reported as one on any type but a struct of the
// package is.
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
// A method, func (b Builder) len(), is a function whose first parameter is
// its receiver, so its body is resolved as any function's, the receiver in
// scope as a parameter under every parameter's rule, _ and type names
// reserved and no other parameter, local or global sharing its name. Its
// name is not one of the package's: methods are not declared with the
// functions, so one may share a name with a function, a global, a builtin
// or main. Once every receiver's type is bound, each method is recorded on
// its type, in TypeDecl.Methods, and its type in Func.RecvType. Its
// receiver's type must be a struct type declared in the package; a type of
// another package, a basic type, an array, a view or a list is reported,
// and a name bound to nothing is left for the type checker to report as
// an unknown type. A method's name follows a field's rule, and may not be
// a field's of its type or another method's of it. An extern method is
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
// a field's naming rule and is declared once, and its parameters are
// named once each, none reserved, but may share names with anything
// else, since a method of an interface has no body that could see them.
// A generic function's type parameters form a scope of their own,
// enclosing its parameters and nested in its file's: they are declared
// first, under a parameter's naming rules, so none is reserved, a
// package-level name, an import's or another type parameter's, then
// their constraints are resolved in that scope, then the function's
// signature, which may name them, and its body, in which a parameter or
// local cannot reuse one's name. A type parameter is a type: a name bound
// to one is reported as a value, called or assigned, as a type's name is.
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
// loop ranges over is resolved outside that scope. A variable is visible from its declaration to the end of its block,
// and its value is resolved before it is declared, so it cannot refer to
// itself. The variables a, b := f() declares are declared in order after f()
// is resolved, as if each were declared on its own, and the targets of
// a, b = f() are bound after f() is resolved, without counting as reads.
//
// The names in a type as written, an array length that names a constant,
// a list type's element type, make's first argument included, are bound too: a local's in its own scope, a global's and a signature's at
// package level. The target of an assignment to an element or a field,
// a[i].x, binds its variable without counting as a read of it, and its
// index like any value. A mut argument, mut x, binds x as a value, so it
// counts as a read: the callee may read the variable through it. A slice,
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
// declaration was rejected, being reserved or already declared, which
// refers to that declaration, is not reported where it would be an error,
// and a variable whose declaration had an error, or that was named where a
// type was expected, is not reported as unused. When Resolve returns no
// diagnostics, every Ident in the tree used as a value, a callee or an
// assignment target is bound, which
// the type checker and code generator rely on; a name written as a type is
// bound unless it is a basic type's, or names nothing, which the type
// checker reports.
package resolver

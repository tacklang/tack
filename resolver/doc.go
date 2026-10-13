// Package resolver binds every name in a parsed file to what it refers to.
//
// It runs after the parser and before the type checker, taking an *ast.File
// and returning the diagnostics it found. It checks the file's shape first:
// the package must be main, a main function must exist, take no parameters
// and declare no results, no function may be declared twice, and none may
// reuse a builtin's name. It then walks each function body with that
// function's parameters in scope, setting Ident.Def on every callee, and on
// every parameter and variable reference and assignment target. A call whose
// callee is a type name, such as int8(x), is a conversion: it binds to no
// declaration, and the resolver records the type in Call.Conv instead. Type
// names are reserved like keywords: nothing may be declared with one, and
// one is never read or assigned as a name. A callee that
// names a builtin is bound to a nil *ast.Func, since builtins have no
// declaration in the tree.
//
// Package-level variables and types share one namespace with functions and
// builtins, and form the outermost scope, enclosing every function, so a
// global can be used from any function, and a type anywhere, whatever the
// order of declarations. Functions are declared first, then types, then
// globals, so a type or global that clashes with an earlier kind is the one
// reported. A name bound to a type is reported where it is used as a
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
// Each block is a scope: a function's parameters come next, its body is
// nested in that, and each branch of an if, and each case of a switch, is
// nested in the block holding the statement; a switch's tag and case
// values are resolved there too. What a defer defers, its block, is a
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
// are bound too: a local's in its own scope, a global's and a signature's at
// package level. The target of an assignment to an element or a field,
// a[i].x, binds its variable without counting as a read of it, and its
// index like any value.
//
// The name _ (ast.Blank) binds nothing. In a, _ := f() or a, _ = f() it
// discards the result in its position and is neither declared nor bound;
// a list of nothing but _ is reported, as is mut on _. In a range loop with
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

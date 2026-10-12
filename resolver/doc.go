// Package resolver binds every name in a parsed file to what it refers to.
//
// It runs after the parser and before the type checker, taking an *ast.File
// and returning the diagnostics it found. It checks the file's shape first:
// the package must be main, a main function must exist, take no parameters
// and declare no results, no function may be declared twice, and none may
// reuse a builtin's name. It then walks each function body with that
// function's parameters in scope, setting Ident.Def on every callee, and on
// every parameter and variable reference and assignment target. A callee that
// names a builtin is bound to a nil *ast.Func, since builtins have no
// declaration in the tree.
//
// Package-level variables share one namespace with functions and builtins,
// and form the outermost scope, enclosing every function, so a global can be
// used from any function whatever the order of declarations. Functions are
// declared first, so a global that clashes with one is the one reported.
//
// Each block is a scope: a function's parameters come next, its body is
// nested in that, and each branch of an if is nested in the block holding
// the if. A loop is a scope of its own, holding any variable its init
// statement or range declares, with the body nested in it. A range loop's
// count is resolved outside that scope. A variable is visible from its declaration to the end of its block,
// and its value is resolved before it is declared, so it cannot refer to
// itself. The variables a, b := f() declares are declared in order after f()
// is resolved, as if each were declared on its own, and the targets of
// a, b = f() are bound after f() is resolved, without counting as reads. No variable or parameter may reuse a name visible where it is
// declared, whether a parameter's or a variable's, package-level ones
// included. A local may reuse a function's or a builtin's name, since a call
// always resolves to a function. A variable that is never read, only
// assigned or not mentioned at all, is reported at the end of its block, and
// a package-level one after every function.
//
// Every name it reports as undefined gets its own diagnostic, so one run
// lists them all. When Resolve returns no diagnostics, every Ident in the
// tree is bound, which the type checker and code generator rely on.
package resolver

// Package resolver binds every name in a parsed file to what it refers to.
//
// It runs after the parser and before the type checker, taking an *ast.File
// and returning the diagnostics it found. It checks the file's shape first:
// the package must be main, a main function must exist, take no parameters
// and declare no result, no function may be declared twice, and none may
// reuse a builtin's name. It then walks each function body with that
// function's parameters in scope, setting Ident.Def on every callee, and on
// every parameter and variable reference and assignment target. A callee that
// names a builtin is bound to a nil *ast.Func, since builtins have no
// declaration in the tree.
//
// Each block is a scope: a function's parameters are the outermost, its body
// is nested in that, and each branch of an if is nested in the block holding
// the if. A variable is visible from its declaration to the end of its block,
// and its value is resolved before it is declared, so it cannot refer to
// itself. No variable may reuse a name visible where it is declared, whether
// a parameter's or another variable's; reusing a function's or a builtin's
// name is allowed, since a call always resolves to a function. A variable
// that is never read, only assigned or not mentioned at all, is reported at
// the end of its block.
//
// Every name it reports as undefined gets its own diagnostic, so one run
// lists them all. When Resolve returns no diagnostics, every Ident in the
// tree is bound, which the type checker and code generator rely on.
package resolver

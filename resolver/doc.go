// Package resolver binds every name in a parsed file to what it refers to.
//
// It runs after the parser and before the type checker, taking an *ast.File
// and returning the diagnostics it found. It checks the file's shape first:
// the package must be main, a main function must exist, take no parameters
// and declare no result, no function may be declared twice, and none may
// reuse a builtin's name. It then walks each function body with that function's
// parameters in scope, setting Ident.Def on every callee and parameter
// reference. A callee that names a builtin is bound to a nil *ast.Func, since
// builtins have no declaration in the tree.
//
// Every name it reports as undefined gets its own diagnostic, so one run
// lists them all. When Resolve returns no diagnostics, every Ident in the
// tree is bound, which the type checker and code generator rely on.
package resolver

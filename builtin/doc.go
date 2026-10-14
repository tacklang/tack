// Package builtin describes the functions Tack provides without a
// declaration.
//
// A Tack program calls print and exit without declaring them, so something
// has to know their names and signatures. The resolver needs the names, to
// bind a call to a builtin and to stop a program from declaring a function
// with a builtin's name. The type checker needs the signatures, to check the
// number and types of arguments. Both read them from here, which is what keeps
// the two stages from disagreeing about what the builtins are.
//
// len is a builtin too, but generic: it takes any array or a string, which
// no Func can describe, so it is listed apart and the type checker checks
// it itself. Names lists every builtin's name, generic ones included.
//
// Each builtin is a Func: a name, the types of its parameters, and its C
// name. No builtin returns a value today, so a Func has no result. Funcs
// lists every builtin in a fixed order, and Lookup finds one by name.
//
// print and exit are extern funcs: their bodies are C, under the C names
// tack_builtin_print and tack_builtin_exit, and a call to one is a call to
// that C function. This package says what the builtins are and what they
// are called in C, not how they run: the C bodies belong to the code
// generation backend, which supplies the ones for its target, so a target
// without a C library can swap them without the language changing.
//
// src/std/builtin/builtin.tck documents the same builtins as extern func
// declarations. A test in this package parses that file with the compiler's
// own front end and fails if the two drift apart, C names included.
package builtin

// Package cname holds the rules for the names a Tack program gives to C.
//
// An extern func's body is a C function, defined under a C name, which has
// to be one C can declare: a C identifier, ASCII letters, digits and
// underscores not starting with a digit, that is not one of C's keywords.
// The type checker holds an extern func's C name to that.
//
// The keywords are those of C23, which include C11's and the words C23
// made keywords (bool, true, false, alignas and the rest), together with
// asm and typeof, which clang reserves in its default GNU modes. A name
// that is not a keyword but that a C header declares, such as int64_t or
// printf, is not rejected here: it clashes only with what a C file
// declares, which clang reports when it compiles that file.
//
// The package is a leaf: it imports nothing of the compiler's.
package cname

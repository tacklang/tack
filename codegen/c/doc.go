// Package c is the C backend. It translates a resolved, type-checked Tack file
// into a single self-contained C translation unit, which clang compiles and
// links into an executable. It registers itself with codegen under the name
// "c".
//
// The translation is direct, with no intermediate representation. Every
// function body is generated before anything is written, so a construct the
// backend cannot lower produces a diagnostic instead of a half-written file.
// The output is laid out in this order:
//
//   - a preamble: the C headers it uses and the tack_string type;
//   - the string pool: every distinct string the program uses, each as a
//     static byte array named str0, str1, and so on;
//   - tack_print, the helper print lowers to, only if the program prints;
//   - a prototype for every Tack function, since Tack lets a function call one
//     declared later in the file and C does not;
//   - every Tack function, as a static C function;
//   - a C main that calls the Tack main and returns 0.
//
// Values map to C types directly. An int is an int64_t and a bool is a C bool.
// A string is a tack_string, a struct holding a pointer and a length and
// passed by value, so a string is one value that can be passed and returned
// like any other.
//
// Strings are pooled as byte arrays rather than C string literals, so any byte
// survives without an escaping scheme. Each array has a trailing zero, so an
// empty string is still a legal array, but the zero is never written out,
// since every write is bounded by the length. When the program prints, the
// newline print appends is pooled first, as str0.
//
// Names are rewritten so that nothing a program declares can collide with what
// the backend emits. A Tack function f in package main becomes tack_main_f,
// which is unambiguous only while package names cannot contain an underscore.
// A parameter x becomes p_x, since Tack has no reserved words and a parameter
// may legally be called int, exit or str0.
//
// print lowers to two tack_print calls, one for its argument and one for the
// newline, each a length-bounded fwrite to stdout. exit lowers to C's exit.
// Every binary expression is parenthesised, so Tack's grouping survives
// whatever C's own precedence would have made of it. An if whose condition is
// one gets no second pair, which would make clang warn on an equality test.
package c

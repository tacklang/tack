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
//   - every package-level variable, as a static C variable;
//   - a results struct for each function with more than one result;
//   - a prototype for every Tack function, since Tack lets a function call one
//     declared later in the file and C does not;
//   - every Tack function, as a static C function;
//   - a C main that calls the Tack main and returns 0.
//
// Values map to C types directly. An int is an int64_t and a bool is a C bool,
// with true and false written as C's own.
// A string is a tack_string, a struct holding a pointer and a length and
// passed by value, so a string is one value that can be passed and returned
// like any other.
//
// Strings are pooled as byte arrays rather than C string literals, so any byte
// survives without an escaping scheme. Each array has a trailing zero, so an
// empty string is still a legal array, but the zero is never written out,
// since every write is bounded by the length.
//
// Names are rewritten so that nothing a program declares can collide with what
// the backend emits. A Tack function f in package main becomes tack_main_f,
// which is unambiguous only while package names cannot contain an underscore.
// A parameter x becomes p_x and a local variable x becomes v_x, since Tack has
// no reserved words and either may legally be called int, exit or str0.
//
// A package-level variable x becomes a static C variable named like a
// function, tack_main_x, since the two share a namespace. It is const unless
// mutable, and initialized with the value the type checker computed, never
// its source expression, so C never evaluates anything Tack has not already
// checked. The smallest int64 is written as an expression, since C has no
// literal for it.
//
// A local variable becomes a C local declared where the Tack one is, inside
// the same block. An immutable variable is a const C local, so the C compiler
// holds it to the same rule. A mutable one declared without a value starts at
// its zero value: 0, false, or an empty string that points at the pooled
// empty array rather than NULL.
//
// A function with several results returns them as one struct,
// tack_results_main_f for a function f, with a field r0, r1, and so on for
// each. The structs are defined before the prototypes, and only for functions
// with more than one result. A return builds one with a compound literal. A
// declaration a, mut b := f() holds the struct in a const temporary named
// like any other, so the call runs once, then declares each variable from its
// field, const unless the variable is mutable.
//
// print lowers to one tack_print call, a length-bounded fwrite of its argument
// to stdout with nothing added. exit lowers to C's exit.
// Every binary expression is parenthesised, so Tack's grouping survives
// whatever C's own precedence would have made of it. An if whose condition is
// one gets no second pair, which would make clang warn on an equality test.
//
// A compound assignment x op= v is written out as x = (x op v), the way Tack
// defines it, and x++ and x-- as x = (x + 1) and x = (x - 1). A loop lowers
// to a C while, or to for (;;) when it has no condition, with its condition
// parenthesised as an if's is; break and continue lower to C's own. A
// three-clause loop lowers to a C block holding its init statement and a C
// for whose post expression is its post statement, so that continue runs it.
// A range loop lowers to a C block that evaluates the count once into a
// temporary and counts up to it, declaring the loop variable as a fresh const
// each iteration. Temporaries are named tack_ followed by a letter and a
// number, which no mangled Tack name can be.
//
// && and || lower to C's own, which short-circuit as Tack's do. ! lowers to
// C's ! with no parentheses of its own, since it binds tightest in both
// languages. A - and a negative literal are parenthesised, so that two minus
// signs in a row never read as C's decrement operator.
package c

// Package c is the C backend. It translates a resolved, type-checked Tack
// program, every file of every package of it, into a single self-contained
// C translation unit, which clang compiles and
// links into an executable. It registers itself with codegen under the name
// "c".
//
// The build runs clang with -w. A warning about the generated C would name a
// temporary file and code the user never wrote, and a valid program can
// cause one: x == x makes clang warn that the comparison always holds.
// clang's errors still reach the user. The package's own tests compile with
// -Wall -Wextra -Werror, so the backend is still held to warning-free C for
// what it controls.
//
// The translation is direct, with no intermediate representation. Every
// function body is generated before anything is written, so a construct the
// backend cannot lower produces a diagnostic instead of a half-written file.
// The output is laid out in this order:
//
//   - a preamble: the C headers it uses and the tack_string type;
//   - a C struct for every array and struct type, then
//     tack_compare_strings if the program compares strings, directly or
//     inside an array or struct, then an equality helper for every array
//     and struct type == compares;
//   - the string pool: every distinct string the program uses, each as a
//     static byte array named tack_str0, tack_str1, and so on;
//   - tack_print, the helper print lowers to, only if the program prints;
//   - every package-level variable, as a static C variable;
//   - a results struct for each function with more than one result;
//   - TACK_C_NAME, if the program has an extern func;
//   - a prototype for every Tack function, since Tack lets a function call one
//     declared later in the file, or in another file, and C does not;
//   - every Tack function but an extern func, as a static C function;
//   - a C main that calls the Tack main and returns 0.
//
// Values map to C types directly. Each integer type is the C fixed-width
// type of its name, int8_t through uint64_t, and a bool is a C bool, with
// true and false written as C's own. A lossless widening needs nothing
// written, since C converts a narrower integer to a wider one keeping its
// value. A conversion T(x), which the checker allows only where it cannot
// fail, is a C cast; v, ok := T(x) holds x in a temporary, tests it against
// only the bounds of T it can fall outside, and gives T's cast of it, or 0.
// A constant expression is written as the value the checker computed, never
// as its operators; one too large for an int64 gets a u suffix. A character
// literal is written as its byte's value, a plain C int literal; arithmetic
// on it is uint8 arithmetic, lowered like any other.
// A string is a tack_string, a struct holding a pointer and a length and
// passed by value, so a string is one value that can be passed and returned
// like any other.
//
// Strings are pooled as byte arrays rather than C string literals, so any byte
// survives without an escaping scheme. Each array has a trailing zero, so an
// empty string is still a legal array, but the zero is never written out,
// since every write is bounded by the length.
//
// Every comparison of two strings, with any of the six operators, lowers to
// tack_compare_strings(l, r) op 0, and an array or struct equality helper
// compares a string element or field the same way. The helper walks the
// common prefix byte by byte through the unsigned char pointer, so bytes
// from 0x80 up are above ASCII and a zero byte is an ordinary byte, then
// orders by length, so a proper prefix is less. It does not use strcmp,
// which stops at a zero byte, or memcmp, which C leaves undefined on the
// null pointer a zero string in an array or struct holds, even for no
// bytes. Two strings are equal by their bytes, never by where they point.
//
// Names are rewritten so that nothing a program declares can collide with what
// the backend emits. A package-level name becomes tack_ followed by each
// part of its package's import path and then the name, each written as its
// length, an underscore, and the part with every byte but an ASCII letter
// or digit escaped as an underscore and two hex digits: First in calc/lexer
// is tack_4_calc5_lexer5_First, f in the main package of a program built
// from one file, whose path is main, is tack_4_main1_f, and my_f there is
// tack_4_main6_my_5ff. A part is read back by its length, so two such names
// never collide, whatever underscores either holds, and every other name
// the backend makes after tack_ has a letter where these have a digit; the
// proof is on mangle.
// A parameter x becomes p_x and a local variable x becomes v_x, since Tack has
// no reserved words and either may legally be called int or exit.
//
// A package-level variable x becomes a static C variable named like a
// function, tack_4_main1_x, since the two share a namespace. It is const unless
// mutable, and initialized with the value the type checker computed, never
// its source expression, so C never evaluates anything Tack has not already
// checked. The smallest int64 is written as an expression, since C has no
// literal for it.
//
// An extern func's body is C, compiled from the C files of its package and
// linked with this translation unit. Its prototype is declared extern,
// under its mangled name like any function, with an assembler label giving
// the symbol it links to: TACK_C_NAME("add") expands to
// __asm__ of __USER_LABEL_PREFIX__ and add, so the label is the symbol a C
// definition of add gets on any platform, _add on macOS and add on Linux.
// A call is then an ordinary call by the mangled name. The C name never
// appears as a C identifier here, so it cannot clash with one this file
// declares or includes: an extern func may be named puts, or str0, without
// conflicting with stdio.h or anything generated. What it may not equal is
// a symbol this file defines, since the label would bind to that instead:
// main, which the type checker rejects as a C name, and the static
// functions and pooled strings, which all start tack_, reserved outside
// the standard library.
//
// Header returns tack.h, which the C files are compiled against: an
// include guard, stdbool.h and stdint.h, and a prototype for every extern
// func under its C name, package by package, with its parameters' types
// and not their names, which could be C keywords, and its Tack declaration
// in a comment beside it. Integers and bools map to the same C types as in
// the generated source, so the two agree on every call.
//
// A defer emits nothing where it is written. Each block with defers ends in
// a cleanup chain that lowers each of its defers once, last first, so the C
// grows linearly with the source. Falling off the block's end runs the
// whole chain; a return, break or continue that leaves the block jumps
// into it with a C goto, at the label of the last defer it reached, and an
// int records which chain it goes on to after, or how it ends. Tack has no
// goto, so which defers an exit has passed is known from where the exit is
// written, and no flag records whether a defer was reached. A return holds
// its value in a variable declared at the top of the function before the
// defers run, and a continue runs them before a three-clause loop's post
// statement. Gotos only jump forward to the end of an enclosing block, and
// deferred code reads no variable a goto can skip the initialization of.
// Each deferred block is lowered as a C block. The backend relies on the
// checker having rejected a defer inside deferred code, so a deferred block
// has no chains of its own and never disturbs the int of the exit running
// it; one handed to it anyway is reported. defer.go has the details.
//
// A local variable becomes a C local declared where the Tack one is, inside
// the same block. An immutable variable is a const C local, so the C compiler
// holds it to the same rule. A mutable one declared without a value starts at
// its zero value: 0, false, or an empty string that points at the pooled
// empty array rather than NULL.
//
// Array and struct types are defined after the preamble, each after every
// type it holds, since C needs a member's type complete first; otherwise in
// the order the checker met them. The checker rejects a struct that holds
// itself, so that order always exists. An array type lowers to a struct
// holding a C array, e; tack_array_ and the type's ID name it. A C array cannot be
// assigned, passed or returned, and a struct holding one can, which gives an
// array Tack's value semantics. A literal is a compound literal of the
// struct, an element x.e[i], and == calls a helper per array type,
// tack_eq_ and its ID, that compares element by element. A package-level
// array is initialized with braces, which a static variable needs, or
// {0}. Every index the checker accepts is in range, so an element read is
// plain C. v, ok := a[i] holds the array, unless it is a variable, and the
// index in temporaries, the array first, and reads the element only when
// the index is in range, testing only the bounds the index's type can
// cross, and giving the element type's zero value otherwise. A string's
// byte is x.ptr[i]; v, ok := s[i] holds the string and the index the same
// way, and reads the byte only when the index is at least 0, tested only
// for a signed index, and below the string's length, read at runtime,
// giving 0 otherwise.
//
// A struct type lowers to a typedef'd C struct named like a package-level
// name of its package, tack_4_main5_Point, since the two share a namespace, with one member
// per field, in order, named f_ and the field's name, so a field may be
// called int or while. A struct with no fields holds one char member, since
// C has no empty struct, and nothing reads it. A literal is a compound
// literal with a designated initializer for each field, in the order the
// literal names them, a field read or write is x.f_name, and == calls a
// helper per struct type, tack_eq_ and its ID, that compares field by field,
// an array or struct field through its own helper. Assigning, passing and
// returning a struct copies it, as C does. A package-level struct is
// initialized with braces, its fields in order, or {0}, and a local one
// without a value with a zeroed compound literal, which leaves a string
// field a null pointer of length 0; tack_print writes nothing for it.
//
// A function with several results returns them as one struct,
// tack_results_4_main1_f for a function f, with a field r0, r1, and so on for
// each. The structs are defined before the prototypes, and only for functions
// with more than one result. A return builds one with a compound literal. A
// declaration a, mut b := f() holds the struct in a const temporary named
// like any other, so the call runs once, then declares each variable from its
// field, const unless the variable is mutable. An assignment a, b = f() is
// lowered the same way, assigning each field. A _ in either gets no C
// variable and no assignment; its field is simply never read. As a three-clause loop's post
// statement, which must be one C expression, its temporary is declared in
// the loop's block instead, and the post assigns the call to it and then
// each field, joined with the comma operator.
//
// print lowers to one tack_print call, a length-bounded fwrite of its argument
// to stdout with nothing added. exit lowers to C's exit, which takes an int,
// so its int64 status is cast to int explicitly, exit((int)(x)), rather than
// narrowed implicitly. A status outside int's range gives an
// implementation-defined value in C11 (6.3.1.3); clang keeps the low 32
// bits, and the OS keeps fewer still, the low 8 on Linux and macOS, so only
// the low bits of the status reach the OS: exit(70000) exits with 112.
// Every binary expression is parenthesised, so Tack's grouping survives
// whatever C's own precedence would have made of it. An if whose condition is
// one gets no second pair, which would make clang warn on an equality test.
//
// Arithmetic wraps on overflow at its type's width, where C leaves signed
// overflow undefined, and promotes a narrow type to int, where it can
// overflow too: + - * and unary - are done on uint64_t, which C defines to
// wrap, and converted to the expression's type. / and % call a helper per
// operator and type, such as tack_div_int8, emitted only when used, which
// gives the smallest signed value divided by -1 as itself and its remainder
// as 0. q, ok := a / b holds both operands in temporaries, the dividend
// first, and divides only when the divisor is not zero, giving 0 otherwise;
// one of two constants, which the checker has computed, is written as its
// value and true, with no temporary. Every other division has a divisor the checker proved not zero. A compound assignment x op= v is written out as
// x = x op v the same way, the way Tack defines it, and x++ and x-- as
// x = x + 1 and x = x - 1. A loop lowers
// to a C while, or to for (;;) when it has no condition, with its condition
// parenthesised as an if's is; break and continue lower to C's own. A
// three-clause loop lowers to a C block holding its init statement and a C
// for whose post expression is its post statement, so that continue runs it.
// A range loop lowers to a C block that evaluates the count once into a
// temporary and counts up to it, declaring the loop variable as a fresh const
// each iteration. Over an array or a string, the block copies the value into
// a const temporary first, so the body assigning the array it came from
// changes nothing the loop reads, and counts up to the array's constant
// length or the copy's len; each iteration declares the index and the
// element, read from the copy, as consts, and a _ gets no C variable. Over
// an array with no element variable nothing reads the copy, so the value is
// only evaluated, cast to void. Temporaries are named tack_ followed by a letter and a
// number, which no mangled Tack name can be.
//
// A switch lowers to a chain of C ifs, never a C switch, whose break would
// leave the switch where Tack's leaves the loop around it, and which cannot
// take a string, an array or a struct. An expression switch holds its tag in
// a const temporary, in a C block of its own, and a case tests it against
// each value with ==, or the type's equality helper, joined with ||, so the
// values are evaluated left to right and no further than the first match;
// the default is the final else. A tag that no case tests is evaluated,
// cast to void. A switch on a string, an array or a struct counts as
// comparing that type when deciding which helpers to emit.
//
// Tack evaluates operands left to right everywhere it evaluates several,
// and C leaves the order of a call's arguments, an operator's operands and
// an initializer's values unspecified, so order.go enforces it where it
// could show. An operand is pure when it calls no Tack function and reads
// no mutable package-level variable: no call can change a constant, a
// parameter, a local or an immutable global. When at least two of the
// operands of one binary operator other than && and ||, one call's
// arguments, one return's values, one array literal's elements, one
// struct literal's values (in the order written, not the fields' order),
// an index's array and index, or a compound assignment's target and value
// are not pure, and one of those calls a function, every operand that is
// not pure is assigned, left to right, to a temporary of its own type
// with C's comma operator, and the operation reads the temporaries:
// f(g(), h()) becomes (tack_o0 = g(), tack_o1 = h(), f(tack_o0, tack_o1)).
// Otherwise nothing is held, since no order could be seen. The
// temporaries are declared, uninitialized, at the top of the function, so
// they work inside any expression, a loop's condition and the right side
// of && included, and no goto can skip their declaration. A switch, a
// checked division and a checked index already hold their operands in
// order. An assignment's target is evaluated before its value, but the
// checker allows no call and no mutable global in a target's indexes, so
// that order needs nothing written.
//
// && and || lower to C's own, which short-circuit as Tack's do. ! lowers to
// C's ! with no parentheses of its own, since it binds tightest in both
// languages. A - and a negative literal are parenthesised, so that two minus
// signs in a row never read as C's decrement operator.
package c

// Package c is the C backend. It translates a resolved, type-checked Tack
// program, every file of every package of it, into a single C translation
// unit, which clang compiles and links into an executable. It registers
// itself with codegen under the name "c". A program codegen.Refuse
// refuses, one not checked or with errors, is reported and nothing is
// generated from it.
//
// The unit, and the tack.h of a program's C files, include the runtime's
// <tack/tack.h>, built into the compiler (see package builtin), which
// defines what every
// program's C shares: tack_string and the string helpers TACK_C_STR_LEN,
// TACK_STR and TACK_STR_N, tack_error and the error helpers TACK_ERROR and
// TACK_NO_ERROR, and tack_any, its type ids TACK_TYPE_INVALID
// through TACK_TYPE_STRING, TACK_ANY_IS and the unpack helpers. Nothing the backend generates defines them, so
// there is one definition, which std's own C files include as well, and
// CompileCmd and BuildCmd put the include directory the driver writes the
// runtime header to on the include path for it. --emit output therefore
// compiles only with a directory holding tack/tack.h on the include path,
// such as an installed std's include directory, which holds a copy.
//
// The build runs clang with -w. A warning about the generated C would name a
// temporary file and code the user never wrote, and a valid program can
// cause one: x == x makes clang warn that the comparison always holds.
// clang's errors still reach the user. The package's own tests compile with
// -Wall -Wextra -Werror, so the backend is still held to warning-free C for
// what it controls.
//
// VersionCmd is clang --version, whose output names clang's version, its
// default target and the directory it is installed in; the driver reuses a
// cached std object only for the same output.
//
// The translation is direct, with no intermediate representation. Every
// function body is generated before anything is written, so a construct the
// backend cannot lower produces a diagnostic instead of a half-written file.
// The output is laid out in this order:
//
//   - a preamble: <stdbool.h> and <stdint.h>, and <stddef.h> if the
//     program uses unsafe_size_t or calls a builtin that takes one, which
//     a freestanding C implementation has, and the runtime's <tack/tack.h>, for
//     tack_string, TACK_C_STR_LEN, tack_error, tack_any and its type ids,
//     which includes only those three;
//   - a C struct for every array, view, list, struct and union type, then
//     tack_compare_strings if the program compares strings, directly or
//     inside an array, a struct or a union, then an equality helper for
//     every array, list, struct and union type == compares, and every one
//     of those types they are made of, then a free helper
//     for every union type that holds a list and every list type whose
//     elements are owned;
//   - the string pool: every distinct string the program uses, each as a
//     static byte array named tack_str0, tack_str1, and so on;
//   - a prototype for each builtin extern func the program calls, only if
//     it calls it, then the division helpers, then the slice helpers;
//   - every package-level variable, as a static C variable;
//   - a results struct for each function with more than one result;
//   - TACK_C_NAME, if the program has an extern func;
//   - a prototype for every Tack function, since Tack lets a function call one
//     declared later in the file, or in another file, and C does not;
//   - every Tack function but an extern func, as a static C function;
//   - the globals tack_argc_ and tack_argv_, which the runtime's tack.h declares,
//     and a C main, int main(int argc, char **argv), that stores its
//     arguments in them, for std/os to read, calls the Tack main and
//     returns 0. A library, a program whose last package is not a main
//     package, which only the driver's --check makes, has neither; the
//     statements its C main would run first are the body of an unused
//     static function, tack_init_, when there are any.
//
// Values map to C types directly. Each integer type is the C fixed-width
// type of its name, int8_t through uint64_t, but for the target-width
// types, unsafe_size_t and unsafe_uintptr_t, which are size_t and
// uintptr_t, and a bool is a C bool, with true and false written as C's
// own. A lossless widening needs nothing written, since C converts a
// narrower integer to a wider one keeping its value. A conversion T(x),
// which the checker allows only where it cannot fail, is a C cast;
// v, ok := T(x) holds x in a temporary, tests it against only the bounds
// of T it can fall outside, and gives T's cast of it, or 0. A target-width
// T's largest value is tested as C's SIZE_MAX or UINTPTR_MAX, and a
// target-width x is taken to reach the largest 64-bit value; either is
// compared as a uint64_t, so the test is never one clang can tell is
// always true on a target where the two types are as wide as each other.
// A constant expression is written as the value the checker computed, never
// as its operators; one too large for an int64 gets a u suffix. A character
// literal is written as its byte's value, a plain C int literal; arithmetic
// on it is uint8 arithmetic, lowered like any other.
//
// Two integers of any types compare by their values (compare.go), where
// C's usual arithmetic conversions would make -1 < 0u false. Two operands
// of one type compare as they are; two of different types are each cast,
// to uint64_t when both are unsigned and to int64_t when both are signed
// or the unsigned one has at most 32 bits, so that every value of both
// survives and clang cannot tell a comparison with a constant is always
// true or false. A signed s and a uint64 or target-width u test the sign
// first, s < u being (s < 0 || (uint64_t)s < (uint64_t)u), which reads s
// twice and may skip u, so each operand that is not pure is held in a
// temporary first, in order, whether or not the order could otherwise
// show (see the evaluation order below). A switch compares its tag with a
// case value of another integer type the same way.
// A string is a tack_string, a struct holding a pointer and a length and
// passed by value, so a string is one value that can be passed and returned
// like any other.
//
// Strings are pooled as byte arrays rather than C string literals, so any byte
// survives without an escaping scheme. Each array has a trailing zero, so an
// empty string is still a legal array, but the zero is never written out,
// since every write is bounded by the length. A string value's length is
// TACK_C_STR_LEN of its array, the array's size less that zero, rather than
// a number written beside it: (tack_string){tack_str0,
// TACK_C_STR_LEN(tack_str0)}.
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
// proof is on mangle. A method is named as if its type's name and its own,
// joined by a period, were one name, so write on Builder in the main
// package is tack_4_main13_Builder_2ewrite: no other name holds a period,
// so a method's C name is never a function's, a type's, a global's or
// another method's, even a function's of a package whose path ends in the
// type's name (see funcCName). An instance of a generic function is named
// as if its function's name and its type arguments, each a struct's or a
// union's package path, a period and its name, or any other type as a
// program writes it, its structs and unions written so, separated by
// commas and in brackets, were one name, so render with File of the main
// package of a program of one file is
// tack_4_main23_render_5bmain_2eFile_5d: no other name holds a bracket,
// a path holds no comma or bracket, and a type's name balances its own,
// so an instance's C name is never another declaration's or another
// instance's (see instanceCName).
// A parameter x becomes p_x and a local variable x becomes v_x, since a Tack
// name may be a C keyword or a name C declares, and either may legally be
// called int or exit. A function whose lowered body never names one of its
// parameters, which Tack allows, as a callback or a generic function ignoring
// one does, starts with (void)p_x; for it, so C's unused-parameter
// warning never fires (see unusedParams). A function, an instance, a map
// type's has, at, put or take helper, a list's, a map's or a union's free
// helper, a division, remainder, shift or slice helper, or a global that
// no other function's lowered body names, a function of std the program
// never calls among them, is declared with __attribute__((unused)), so
// C's unused-function and unused-variable warnings never fire either (see
// unused.go).
//
// A mut parameter, mut p T, is a pointer, T *p_p, to the caller's
// variable. Every read and write of it goes through the pointer, (*p_p),
// which C's postfix operators follow as they follow a name, so p.x is
// (*p_p).f_x and p[i] (*p_p).e[i]. A mut argument is the address of its
// place, &v_b, &v_b.f_lo or &v_cells.e[v_i], and a mut parameter passed on
// is the pointer it already holds, p_p. The checker has proven every
// index in a place in range, so taking its address is plain C. A plain
// parameter of any type is still passed by value, a copy, so an argument
// that reads a variable another argument passes as mut gets the value it
// had when it was evaluated.
//
// An own parameter, own p T, is passed by value too, T p_p, the C type a
// plain one has, so a function value's type needs nothing new in C. An own
// argument, own x, is the move of x, which the checker marked: its value
// taken through a temporary and the variable emptied, (tack_o3 = v_x, v_x
// = (T){}, tack_o3), so nothing in the caller frees it again. The callee's
// body, the first block its frame opens, adds a free for each own
// parameter of an owned type to its cleanup chain ahead of every
// statement (addParamFrees), so it is freed at every exit, after every
// defer, as a local declared first would be; a move out of it, return p
// or own p to another call, empties p_p the same way, which makes that
// free a no-op. In an instance of a generic function whose type argument
// owns nothing, there is no free and a move is a copy.
//
// A method, of a struct or a union, is a C function of its own taking its
// receiver first, a mut receiver as a pointer, as a mut parameter is. A
// method call, x.m(args) or mut x.m(args), is lowered as the call of that
// function with the receiver as its first argument (see lowered), so the
// receiver is evaluated first, a mut one as the address of its place,
// then the arguments, under every rule of a call's order and of the
// copies and views an argument may need.
//
// A generic function is never emitted itself: each of its instances the
// type checker recorded in Program.Instances is, once, as a function of
// its own, with its prototype and its results struct, after the
// functions that are not generic (generic.go). Its body is generated
// with the type table binding each type parameter to the instance's type
// argument (types.Table.Bind), so every C type it writes and every
// question it asks of a type, whether it is owned, how it is freed, is
// its type argument's, and a type made of a type parameter, list[T], is
// the type made the same way of its type argument, whose C struct and
// helpers are the caller's own; every C name made from a type's ID is
// made from the type it stands for (see names.go), and a type made of a
// type parameter nothing binds is never written itself. A caller passes
// its arguments as the types the checker recorded for the call
// (Call.ParamTypes), its type arguments in place. A call of an interface method on a value of a
// type parameter calls the type argument's method of that name directly,
// and a call of a generic function calls its instance for the call's
// type arguments, as bound. The checker moves a value of a type
// parameter, which may hold a list; where its type argument holds none,
// the move is a plain copy and the variable is left as it is.
//
// A view, []T, lowers to a C struct of a pointer to const elements and a
// length, tack_view_ and the type's ID, defined after its element type,
// the layout of tack_string, so a view's element is x.ptr[i], its length
// x.len, and v, ok := v[i] and a range over a view are lowered as they are
// for a string. A view parameter is passed by value. A view passed on is
// passed as it is; []uint8(s) is the string's pointer and length, the
// string held in a temporary first unless it is a name or a literal, which
// is its pooled array and that array's length; and an array, which the
// checker has made sure is a place, is (tack_view_N){place.e, n}, a pointer
// into the caller's array. The callee must see the elements as they were
// when the argument was evaluated, as a plain argument's copy would show
// them, so the array is first copied into a temporary declared at the top
// of the function, and the view points at the copy, when the callee could
// otherwise see it change: when it is part of a mutable global, which any
// function can assign, or when the call's arguments, at any depth, pass as
// mut a place that may overlap it. Places compare as paths: two different
// fields never overlap, and any two elements may. A copy taken where none
// was needed shows nothing. An array of no elements is viewed through its
// struct's one unused element, with a length of 0. A temporary holding a
// call's argument to a view parameter, for the order of evaluation, is a
// view, whatever the argument's own type.
//
// A function returning a view returns its struct by value. A return
// renders its value as a view argument is rendered, a list as its buffer
// and length, an array as a pointer to its first element and its length,
// a slice through its helper, a view as it is, and never copies it: the
// checker has made sure the view points into its caller's data, which
// outlives the call, and no copy is needed there, since what it views is
// not part of a mutable global and overlaps no mut argument. The value is
// held in the function's ret variable, zeroed first as a list's is, when
// defers run before it returns. A call returning a view is an operand
// like any other: len reads its len, a range holds it in its const
// temporary, v, ok := f()[i] holds it in a temporary, as a string's call
// is held, so the call runs once, and nothing it points into is copied or
// freed.
//
// A variadic parameter, args ...T, is a view parameter in C, and a spread,
// f(a, v...), is a view argument to it, lowered as any is, copies
// included. Values passed one by one, f(a, x, y), are packed by the
// call: (tack_view_N){(const T[2]){x, y}, 2}, a compound literal array on
// the caller's stack, which lives to the end of the block holding the
// call, and so for the whole call, viewed with the number of values. The
// values are operands of the call like its other arguments, each
// evaluated once, left to right, into a temporary when the order shows,
// before they are packed, and C converts each to T as it initializes the
// array, a widening the checker allowed. No values is (tack_view_N){0, 0}:
// C has no array of no elements, and a null pointer with a length of 0 is
// never read, nor moved by a slice, as for the zero string's bytes.
//
// An any is a tack_any, {uint32_t type; const void *ptr;}, by value, and
// []any, so ...any, a view of them, tack_view_N like any view. A value
// passed as any, to an any parameter or as a value of an ...any, becomes
// (tack_any){TACK_TYPE_INT64, &x} at the call, its type's id from std's
// tack.h and the address of its value as it was when the argument was
// evaluated (any.go): the argument itself when it is a name no call can
// change while the function runs, a plain parameter, an immutable local
// or global, or a mutable local the function never passes as mut; the
// temporary the order of evaluation already holds it in, when it does;
// and otherwise a copy, assigned in the argument to a temporary declared
// at the top of the function, (tack_a0 = x, (tack_any){..., &tack_a0}),
// which lives for the whole call. So a mutable global, or a variable the
// same call passes as mut, is copied, and the callee sees the value as
// at the call, as a plain argument's copy shows it. An any passed on is
// passed as it is. The values of an ...any are made anys before they are
// packed. An any local, b := a, is a const tack_any, and the empty any an
// index out of range gives, (tack_any){}, type id 0 and a null pointer.
// A view of a basic type passed as any is its view struct, rendered as a
// view argument is (view.go), so an array it slices is copied first when
// the call could change it, and its id is TACK_TYPE_VIEW_ and the end of
// its element's macro, TACK_TYPE_VIEW_UINT8 for []uint8: (tack_a0 =
// tack_slice_from_N(...), (tack_any){TACK_TYPE_VIEW_UINT8, &tack_a0}), or
// &p_v for a view parameter, which nothing in its function can change. A
// type switch's case naming a view declares v as the view struct,
// *(const tack_view_N *)tmp.ptr, as for any one type.
//
// A type assertion, v, ok := x.(T), holds x in a const temporary, as a
// call bound to two names holds its results, and reads v as (tmp.type ==
// TACK_TYPE_T ? *(const T *)tmp.ptr : zero), the id checked before the
// pointer is read, and ok as the comparison (any.go). A type switch is a
// chain of ifs like any switch (switch.go), on a const temporary holding
// x, each case comparing its id with each type's; a case's variable, when
// its body reads it, is declared first in its block, *(const T *)tmp.ptr
// for a case naming one type and the tack_any itself otherwise. Neither
// needs a helper: the runtime's tack.h names the ids.
//
// A slice, x[i:j], is a call to a helper for its type, tack_slice_string
// or tack_slice_N for the view type N, or tack_slice_from_ either when j is
// left out and the helper reads the length itself; each is emitted only
// when the program uses it. The helper moves the pointer by i only when i
// is not 0, so the null pointer of a zero string in a zeroed struct, or of
// its bytes' view, is never moved, which C leaves undefined even by 0; any
// i past 0 is at most a length that is not 0, so the pointer is not null.
// An array is viewed whole first, (tack_view_N){place.e, n}, and copied
// first under the rule for a view argument when the slice is one. What is
// sliced and the bounds are the helper's arguments, sequenced left to
// right, and the proven bounds are cast to int64_t, which keeps their
// values. t, ok := s[i:j] holds the string and each bound in temporaries,
// in order, tests 0 <= i for a signed i, i <= j or the omitted bound's
// test, and j <= len(s), each by value as intComparison compares, and
// calls the helper only when they hold, giving the pooled "" otherwise. A
// global whose initializer slices a string has its computed value pooled.
//
// A package-level variable x becomes a static C variable named like a
// function, tack_4_main1_x, since the two share a namespace. It is const unless
// mutable, and initialized with the value the type checker computed, never
// its source expression, so C never evaluates anything Tack has not already
// checked. The smallest int64 is written as an expression, since C has no
// literal for it. A value whose initializer would nest deeper than
// maxDepth, an array, struct or union literal a few hundred levels deep,
// is too deep for clang, which checks an initializer one level of
// subobject at a time, recursively, whatever its syntax, and no function
// holds a static variable's parts in temporaries. Such a variable starts
// zeroed, not const, and C's main gives it its value before Tack's main
// runs, in a block that builds it bottom up, each part nested past half of
// maxDepth held in a local temporary first (deepGlobal). No other
// global's initializer reads it, since the checker computed them all,
// and Tack code runs only after.
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
// include guard, TACK_H, stdbool.h and stdint.h, stddef.h if a signature
// uses size_t, the runtime's <tack/tack.h> if a signature uses a string, an error
// or any, for tack_string, tack_error, tack_any and their helpers, a definition of every array and struct type an
// extern signature uses, and of every type those hold, each after its
// parts, the results struct of every extern func with several results, and
// a prototype for every extern func under its C name, package by package,
// with each parameter's type followed by its name in a comment, not as a
// C name, which could be a C keyword, a mut parameter's being a pointer to
// its type, T *, and its
// Tack declaration in a comment beside it. Every type has
// the generated source's layout, member for member, under other names: a
// struct of package main is its Tack name, one of another package
// <package name>_<name>, an array tack_array_<length>_<element>, the
// element's Tack name if basic, a view tack_view_<element>, named the same
// way, with members ptr and len, so []any is tack_view_any, an any being
// the runtime's tack_any, and []error tack_view_error, an error being the runtime's
// tack_error, and a results struct tack_results_<C
// name>, with members r0, r1 and on; a struct's members are its fields'
// Tack names. The generated source declares each extern func with its own
// types, so a value passes between the two unchanged. Generate reports,
// before generating anything, a name tack.h would give two things (two
// types, a type and a results struct or extern func, or one of tack.h's own
// names, TACK_H, or of the runtime's tack.h, tack_string, its string helpers,
// tack_error, its error helpers,
// tack_any, its type ids and its helpers, and its guard TACK_STD_TACK_H,
// whether or not tack.h includes it, or a macro the C
// standard specifies <stdbool.h>, <stdint.h> or <stddef.h> defines, such as
// NULL, INT64_MAX or INT64_C, whether or not tack.h includes that header),
// a type of package main or a field named with a C keyword, and a field
// named like an object-like macro among those, TACK_H, TACK_STD_TACK_H, a
// TACK_TYPE_ id or a standard header's, which would replace the member's
// name, among the types tack.h defines, whether or not the program has C
// files: the diagnostic is at the second of two, with a note at the first.
// A field may be named like a function-like macro, TACK_STR, TACK_ANY_IS or
// offsetof, which expands only before a (, or like tack_string, tack_any or
// an unpack helper, whose names a member's name does not clash with. The generated source is unaffected by any of
// these names: its fields are f_<name> and its types and extern funcs
// mangled. reservedNames lists them all.
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
// checker having rejected a defer inside deferred code, so a deferred
// block's only chains free its own lists, whose exits route through an int
// of the deferred block's own, never disturbing the one of the exit
// running it; a defer inside deferred code handed to it anyway is
// reported. defer.go has the details.
//
// A list, list[T], lowers to a C struct per element type, tack_list_ and
// the type's ID, of its buffer, ptr, its length, len, its capacity, cap,
// and its allocator, alloc, a tack_allocator from the runtime's tack.h. make is a
// compound literal holding only the allocator; append of one value is
// std's TACK_LIST_APPEND on the address of the list's place, which calls
// the runtime's tack_list_grow_ when the buffer is full, and append of any other
// number of values, none included, std's TACK_LIST_APPEND_N given a
// compound literal array of them, or the pointer and length of a spread
// view held in a temporary, which grows the buffer once, to the final
// length, before writing anything; either is turned into no error, or
// into code 1 and the pooled message "out of memory". reserve is std's
// TACK_LIST_RESERVE, after a count that is not a constant is tested for
// being negative, code 2 and the pooled message "negative count". An append
// of owned values holds each in a temporary first, and frees them, last
// first, when the buffer cannot grow. pop is std's TACK_LIST_POP and
// remove its TACK_LIST_REMOVE, each on the address of the list's place;
// their checked forms hold that address, remove's index and whether the
// list has the element in temporaries, and take the element only when it
// does, giving the element type's zero value otherwise. Written as a
// statement, either holds the same temporaries and, only when the list has
// the element, takes it under an if and drops it, freeing it when its type
// is owned (see dropTake). replace(mut place, new) is a comma expression
// over temporaries at the function's top: the place's address, the new
// value, the old value read, the new one stored, the old one given; as a
// statement, the old value is freed when owned (see replace.go).
// swap(mut l, i, j), a statement, holds the list's address and both
// indexes in temporaries and exchanges the two elements through a third,
// freeing nothing (see swap.go). A map
// lowers to a struct of its own per map type, a buffer of entries in
// insertion order and a table of slots probed from a key's hash, with
// helpers per map type to find, ask has, take the address of a value
// under a proven key (at), grow, put and take, and a hash helper per key
// type, all written after every free and equality helper they call, and
// freed by tack_free_ like a list of owned elements; put, has, a checked
// lookup, m[k] read or written in place through at, delete and a range
// over one lower through them (see maps.go). Where the C starts a map
// before Tack gives it one, a function's result, or the value a checked
// pop or delete gives when there is none, it is a zeroed struct, an
// empty map no code reads (see zeroValue). A function value is a
// tack_fnptr, one C function pointer type for every function type,
// defined before any type that may hold one, so a struct may hold one
// that takes the struct; a function named as a value is a pointer of its
// function type's own pointer type, tack_fn_ and the type's ID, a
// compound literal C checks the function's type against, converted to
// tack_fnptr, and a call through one converts it back before calling,
// the value an operand evaluated before the arguments. A function of
// several results named as a value is a thunk, tack_value_ and its name,
// returning them in its function type's results struct, tack_fnres_ and
// the type's ID (see funcvalue.go). An
// element, a length, a
// checked element, a range and a view of a list are lowered as a view's
// are, but a list is written too, x.ptr[i] = v, and is ranged over in
// place; a range's variable bound to an owned element is a pointer to it,
// read as (*v_x). A variable of an owned type, a list or a struct holding one, is
// never const; its declaration adds a free to its block's cleanup chain,
// lowered like a defer, so frees and defers interleave by position and
// run at every exit from the block. A call to the builtin exit leaves no
// block, so neither runs; std's exit has every allocator free each
// buffer it still holds instead. A free is a call to the runtime's tack_list_free_ per list,
// a struct's last field first, or, for a list whose elements are owned,
// to its tack_free_, which frees each element, last first, then the
// buffer. Assigning one, an element of a list included, computes the new value into a
// temporary, frees the old one, then stores the new one. A name the
// checker marked as moving its variable's value empties the variable
// through a temporary, so a later free of it does nothing, which is what
// makes a conditional move free right on every path. list.go has the
// details.
//
// A local variable becomes a C local declared where the Tack one is, inside
// the same block. An immutable variable is a const C local, so the C compiler
// holds it to the same rule, unless it owns a list (see above). A mutable one declared without a value starts at
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
// {}. Every zero value is written {}, never {0}: clang rejects {0} for an
// array whose element count is a multiple of 2^32, which it takes for an
// aggregate with no elements, and takes {}, standard in C23 and an
// extension before it that only -Wpedantic reports, for any array. Every index the checker accepts is in range, so an element read is
// plain C. v, ok := a[i] holds the index in a temporary and reads the
// element only when the index is in range, testing only the bounds the
// index's type can cross, and giving the element type's zero value
// otherwise. The array is evaluated first, as Tack orders it, and is read
// where it is, after the index, when it calls no function and the order
// cannot show (see order.go). Otherwise it is held first: an array that is
// a place, a variable or a field or element of one, by its address, a
// pointer to const, so no element read copies the array, unless the index
// calls a function and can change the array, being part of a mutable
// global or passing it as mut, when the array is copied, so the read sees
// it as it was. A string's byte is x.ptr[i]; v, ok := s[i] holds the index
// the same way, and the string too unless it is a name read where it is,
// and reads the byte only when the index is at least 0, tested only for a
// signed index, and below the string's length, read at runtime, giving 0
// otherwise. A uint64 or target-width index is compared with the length
// with both cast to uint64_t, which keeps every value of either, since a
// length is never negative, and draws no warning that their signs differ
// where size_t is 64 bits wide. Any other index converts to int64_t, the
// length's type, as C does by itself. A target-width index reads an
// element as size_t or uintptr_t, at any width.
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
// initialized with braces, its fields in order, or {}, and a local one
// without a value with a zeroed compound literal, which leaves a string
// field a null pointer of length 0; print writes nothing for it.
//
// A union type lowers to a typedef'd C struct named as a struct is,
// holding its tag, the position of the case it holds, in the narrowest of
// uint8_t, uint16_t and uint32_t that holds every case's, and u, a C union
// of one member per payload, c_ and the case's name. Building one is a
// compound literal setting the tag and the payload, (T){.tag = 0,
// .u.c_int = 5}; a package-level one is initialized with braces the same
// way. == calls tack_eq_ and its ID, which compares the tags, then the
// payloads of the case both hold. A union holding a list is freed by
// tack_free_ and its ID, which frees what its case's payload holds, and a
// struct holding one frees it through that helper. Every tack_eq_ and
// tack_free_ takes const pointers to the values it reads, so a helper
// calling the helper of a type nested in its own uses one small frame
// whatever the size of the values, and a thousand types nested one in
// another compare and free within the stack. A list's tack_eq_ compares
// the lengths, then each element below the length through the element
// type's own ==, reading the buffer in place; typeck makes sure neither
// operand holding a list is a temporary or passed as mut in the other, so
// the buffers it reads are the operands' own and still allocated. An
// operand of == that is not a place C can take the address of, such as a
// call's result, or a literal whose C runs the calls in it first, (o0 =
// f(1), o1 = f(2), (S){...}), a comma expression's value, is held in a
// temporary first, where the comparison evaluates it. A switch on a union is
// a chain of ifs on its tag, through a pointer to the switched place, or
// to a copy of a value that is no place or of a mutable global; the last
// clause of one covering every case is the final else, and one with no
// clause, on a union with no cases, is __builtin_unreachable(). A clause's
// binding is a pointer to the payload, read as (*v_n), declared only when
// the clause reads it. union.go has the details.
//
// An error is the struct no declaration gives: the runtime's tack_error,
// {int32_t code; tack_string msg;}, which the generated source never
// defines, whose members are code and msg, not f_code and f_msg, and
// whose equality helper, tack_eq_ and error's ID, is emitted, like any
// struct's, when == compares two. A literal is a compound literal of it,
// (tack_error){.code = 2, .msg = ...}, noerror the zeroed (tack_error){},
// and a global holding noerror, or any error with code 0 and an empty
// message, is initialized with {}, whose message is a null pointer of
// length 0, as a zeroed string field's is. The rules that keep an error
// from being dropped or its results from being read unchecked are the
// type checker's, so an error is lowered as the plain value it is.
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
// print and exit are the builtins with a C body, which the compiler's
// builtin.Set, the one Generate is given, lists with their C names, and a
// call to one lowers like a call to any function, to its C
// name: tack_builtin_print(s), tack_builtin_exit(x). The generated C only
// declares them, one prototype for each builtin the program calls, after
// the string pool: void tack_builtin_print(tack_string);, its parameters'
// C types unnamed, exit's declared never returning, _Noreturn void
// tack_builtin_exit(int32_t);, as its body is, since the type checker
// counts a call to it as leaving its block, so a C function with a result
// may end in one with no return after it, which clang accepts only knowing
// the call never returns. Their bodies are the runtime's C files, built
// into the compiler, which the driver compiles with CompileStdCmd and links only when the program calls
// that builtin, so the generated C includes no hosted header for them, and
// a program that calls neither compiles with -ffreestanding. A target
// without a C library swaps those files. Their names start tack_, which no
// user extern func's C name may, and the generated C's own tack_ names
// never start tack_builtin_. print's body is a length-bounded fwrite of its
// argument to stdout with nothing added. exit's status is an int32, an
// int32_t in C, which its body passes to C's exit as its int: on every
// target Tack builds for int32_t is int, and the type checker has already
// made the program convert any wider status, so no narrowing is left to
// the C. The OS keeps only the low bits of the
// status, the low 8 on Linux and macOS: exit(70000) exits with 112.
// Every binary expression is parenthesised, so Tack's grouping survives
// whatever C's own precedence would have made of it. An if whose condition is
// one gets no second pair, which would make clang warn on an equality test.
//
// Arithmetic wraps on overflow at its type's width, where C leaves signed
// overflow undefined, and promotes a narrow type to int, where it can
// overflow too: + - * and unary - are done on uint64_t, which C defines to
// wrap, and converted to the expression's type, and so are & | ^ &^ and
// unary ^, on the two's complement bits a conversion to uint64_t keeps. /
// and % call a helper per operator and type, such as tack_div_int8, emitted
// only when used, which gives the smallest signed value divided by -1 as
// itself and its remainder as 0, and so do << and >>, tack_shl_int8 and
// tack_shr_int8, which take the count as a uint64_t and give 0, or -1 for
// >> of a negative value, for a count at or past the width, where C's
// shift is undefined: << shifts on uint64_t, and a signed >> shifts a
// negative value's complement and flips it back, an arithmetic shift that
// never shifts a negative value in C (see emitShift). q, ok := a / b holds both operands in temporaries, the dividend
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
// element, read from the copy, as consts, and a _ gets no C variable. An
// array that is a place the body never assigns, increments, decrements or
// passes as mut in any part, and, for a mutable global, in a body that
// calls no function, cannot change while the loop runs, so the block holds
// a pointer to it instead and reads each element through it: the same
// values a copy gives, without the copy. Over an array with no element
// variable nothing reads the copy, so the value is only evaluated, cast to
// void. Temporaries are named tack_ followed by a letter and a
// number, which no mangled Tack name can be.
//
// A switch lowers to a chain of C ifs, never a C switch, whose break would
// leave the switch where Tack's leaves the loop around it, and which cannot
// take a string, an array or a struct. An expression switch holds its tag in
// a const temporary, in a C block of its own, and a case tests it against
// each value with ==, or the type's equality helper, or by value for an
// integer of another type, joined with ||, so the
// values are evaluated left to right and no further than the first match;
// the default is the final else. A tag that no case tests is evaluated,
// cast to void. A switch on a string, an array or a struct counts as
// comparing that type when deciding which helpers to emit.
//
// Tack evaluates operands left to right everywhere it evaluates several,
// and C leaves the order of a call's arguments, an operator's operands and
// an initializer's values unspecified, so order.go enforces it where it
// could show. An operand is pure when it calls no Tack function and reads
// no mutable package-level variable and no variable the function passes as
// mut anywhere: no call can change a constant, an immutable global, or a
// parameter or local except through a mut argument. A mut argument is
// pure when the indexes in its place are, since a call cannot move the
// place it names. When at least two of the
// operands of one binary operator other than && and ||, one call's
// arguments, one return's values, one array literal's elements, one
// struct literal's values (in the order written, not the fields' order),
// an index's array and index, or a compound assignment's target and value
// are not pure, and one of those calls a function, every operand that is
// not pure is assigned, left to right, to a temporary of its own type
// with C's comma operator, and the operation reads the temporaries:
// f(g(), h()) becomes (tack_o0 = g(), tack_o1 = h(), f(tack_o0, tack_o1)).
// Otherwise nothing is held, since no order could be seen, except in a
// comparison of a signed integer with a uint64 or target-width one, which
// holds every operand that is not pure, since it reads one twice and may
// skip the other. The
// temporaries are declared, uninitialized, at the top of the function, so
// they work inside any expression, a loop's condition and the right side
// of && included, and no goto can skip their declaration. A switch, a
// checked division and a checked index already hold their operands in
// order, a checked index's array by its address where it can. An
// assignment's target indexes are evaluated before its value: the checker
// allows no call in them, but an index that needs no proof, a uint8 into
// an array of 256, may read a mutable global or a variable passed as mut,
// which the value can change, so where the order shows, each index that
// is not pure is held in a temporary, as a statement before the value,
// and the store reads it: a[g] = f() becomes tack_o0 = g; a.e[tack_o0] =
// f(); (see holdTargetIndexes). Only the index is held, never the
// element's address, so the store finds its container after the value.
//
// C's parser recurses once per level of nesting, and clang runs out of
// stack at about a thousand: each Tack operator adds several levels, so a
// chain of a few hundred terms used to stop the build. So no
// subexpression's C nests deeper than maxDepth, 32 levels of parentheses,
// brackets and braces (order.go): a deeper one is held in a temporary,
// declared at the top of the function like the order's, and assigned by
// a step, and an expression a context renders on its own, a statement's
// value, a condition, an index, runs its steps first with the comma
// operator: x + x + ... + x becomes ((tack_d0 = ..., tack_d1 = ..., x)),
// whatever the chain's length or which side it nests on, as are nested
// calls and minuses. A run of many steps is grouped in halves, since C
// nests a run of commas too. A step runs before everything left in the
// expression, so a part is held only when nothing evaluated before it
// that is not pure is left in place; otherwise the expression is rendered
// again with every part that is not pure held, in order, so each runs
// where Tack runs it. The right side of && and || runs only when the
// left side does not decide, so it keeps its steps inside it, and a chain
// nested on the right of && or || still nests a level per operator in C.
// The expression a context renders on its own is never held itself, so a
// range, a union switch and a checked index still take its address.
// Array and struct literals and union constructions nest the same way,
// three levels of C each, and are held the same way. Where every part that
// is not pure is held, in order, a literal that only builds a value of its
// parts is not held for that, nor are its values held again to keep
// their order, only for depth, so a deep literal keeps one copy of its
// inner values in the frame every few levels, not one for each.
//
// clang parses else if as an if nested in the else before it, and runs
// out of stack at a few thousand, though the chain is flat in the source.
// A chain of up to maxChain branches is written as C's else if; a longer
// one as ifs one after another, each branch ending in a goto past the
// rest of the chain and the else block after them all, so its conditions
// still run in order, each only until one holds, exactly one branch runs,
// and C still sees that a chain whose branches all return returns
// (flatChain). The chain is lowered in a loop, so its length costs no
// stack here either.
//
// && and || lower to C's own, which short-circuit as Tack's do. ! lowers to
// C's ! with no parentheses of its own, since it binds tightest in both
// languages. A - and a negative literal are parenthesised, so that two minus
// signs in a row never read as C's decrement operator.
package c

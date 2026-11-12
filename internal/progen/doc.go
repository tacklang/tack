// Package progen generates whole Tack programs that are valid, terminating
// and deterministic, for testing the compiler where byte-mutation fuzzing
// seldom reaches: deep in the type checker's proofs, in code generation,
// and in the runtime's safety rules. It is used only by tests.
//
// # Choices
//
// Every choice the generator makes comes from a Source: Bytes, which reads
// them from a fuzz engine's input, a byte a choice, so a mutation of the
// input changes the program's structure and a corpus entry stays small;
// or Seeded, for a fixed program per seed. A Source that has run out
// answers 0, and the generator makes 0 the simplest option, the one that
// ends a list or a block, so every input is a finite program.
//
// # Programs
//
// Generate returns a Program: its source, main.tck, which imports std/fmt
// and declares two extern funcs whose C, CSource, the program is built
// with: live, c_allocator's count of live buffers, and setup, which makes
// c_allocator fail every allocation after the first n when the environment
// variable PG_FAIL_AFTER is n. A program declares a few structs, maybe a
// union, a few functions, methods among them, and run, and main calls
// setup, then run, then checks live is 0, printing "leak" and exiting 3
// when it is not, and prints a checksum: every value the program observes,
// through obs, obsu or obsb, folded into h as h = h *% 31 +% v, which
// wraps at 64 bits.
//
// What it writes: integers of every fixed width, with the wrapping
// operators +% -% *% and -%x, their compound forms, and +%= 1 and -%= 1 for
// steps, since plain arithmetic needs a proof that it fits,
// bitwise operators and shifts, comparisons across types, bools, strings,
// arrays, structs, unions with switches, expression switches, tagged and
// with no tag, whose cases may end in fallthrough, lists and maps with
// every builtin on them (make, append, pop, put, delete, has, swap, len, range
// and drop), arenas of int64s, made with a sentinel and grown by add and
// add_all, their elements read through an id, the zero id and a span's
// view, ranged over,
// views, function values and parameters of function types, methods with
// plain and mut receivers, called on a receiver or through their struct,
// S.m(r, args), and those with a plain receiver named as values, S.m,
// mut and own parameters, functions returning an
// error, a ? on a call of one, or on append or put, in a function that
// returns one, as a statement, a binding or a value in an expression,
// block-scoped defers, loops of a constant number of trips, if,
// break, continue and early returns, the checked forms of division,
// conversion, indexing, pop and map reads, std/bytes and std/sort, whose less may be a closure, and a
// generic function, rev, reversing a list with swap.
//
// # Valid by construction
//
// The generator tracks every variable in scope, its type, whether it may
// be written, whether it was moved, and whether it is read, and writes only
// what Tack accepts: every variable it declares is read; an index, a byte
// or a divisor is proven by an if that guards a single statement, or read
// in the checked form, a signed /'s divisor proven not -1 as well, and a
// divisor that may be written also by an early
// exit ending in exit, never taken, since an if before it makes a zero 1, or by a call to a
// helper, pg_fix_T(mut d), whose contract, ensures d != 0, the compiler
// proves in its body and gives the division after the call; a constant is never the only operand of arithmetic,
// so the compiler never computes one that overflows; a list or a map is
// never copied, a list passed as mut or own is named by no other argument,
// a variable moved by own or dropped is never used again and is never
// moved in a loop it was declared outside; a collection being ranged over, a loop's
// counter and a switch's union are never written; a value a call returned
// with an error is read only where the error is proven checked; deferred
// code allocates nothing, reads no list and never leaves its block; and
// calls go only to functions generated earlier, so there is no recursion.
// A program the compiler rejects is therefore a bug in the generator, or a
// compiler that rejects a valid program.
//
// # The expected output
//
// The generator builds the interpreter's run of a program as it writes it:
// each expression and statement carries, beside its text, a Go closure
// that computes it as Tack does, the wrapping operators wrapping at their
// width, the
// smallest signed value divided by -1 being itself, a shift past the
// width shifting every bit out, maps iterating in insertion order, defers
// running when their block is left, the last first, and a ? whose call
// failed stopping its statement where it is and returning the call's
// error from the function, through every block's defers. Program.Expect runs
// it, within a budget of steps, and returns what the program must print.
// Program.Trace gives the program changed to print every value it
// observes, and the interpreter's list of them, for finding where a
// checksum that does not match went wrong.
//
// # Oracles
//
// The tests (see progen_test.go) put each program through: the front end
// accepts it; its C compiles with -Wall -Wextra -Werror at -O0 and -O2,
// under the address and undefined behavior sanitizers; each binary exits 0
// printing what Expect computed, which also makes the -O0 and -O2 output
// the same; and, run with allocations failing after each n from 0 until
// none fails, it still exits 0, which main's check of live makes a check
// that nothing leaked. Findings, minimized, are kept in findings_test.go.
//
// Building and running every program is thorough but slow, clang taking
// most of the time. FuzzGeneratedFrontEnd is the front-end-only mode: it
// generates programs from the fuzz engine's bytes as FuzzGenerated does,
// but compiles none, so it needs no clang and gets through many times as
// many programs, for runs of hours. It checks that the front end accepts
// each program and the C backend generates its C and its header, with no
// stage panicking, and that the interpreter's run of it does not panic
// and gives the same result each time it is run; with no binary, the
// checksum is compared with nothing else. Both targets read the same
// input, so an input it saves under testdata/fuzz/FuzzGeneratedFrontEnd
// runs through every oracle when copied to testdata/fuzz/FuzzGenerated.
package progen

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
// through obs, obsu or obsb, folded into h as h = h * 31 + v.
//
// What it writes: integers of every fixed width, with wrapping arithmetic,
// bitwise operators and shifts, comparisons across types, bools, strings,
// arrays, structs, unions with switches, lists and maps with every builtin
// on them (make, append, pop, put, delete, has, swap, len and range),
// views, function values and parameters of function types, methods with
// plain and mut receivers, mut and own parameters, functions returning an
// error, block-scoped defers, loops of a constant number of trips, if,
// break, continue and early returns, the checked forms of division,
// conversion, indexing, pop and map reads, std/bytes and std/sort, and a
// generic function, rev, reversing a list with swap.
//
// # Valid by construction
//
// The generator tracks every variable in scope, its type, whether it may
// be written, whether it was moved, and whether it is read, and writes only
// what Tack accepts: every variable it declares is read; an index, a byte
// or a divisor is proven by an if that guards a single statement, or read
// in the checked form, and a divisor that may be written also by an early
// exit ending in exit, never taken, since an if before it makes a zero 1; a constant is never the only operand of arithmetic,
// so the compiler never computes one that overflows; a list or a map is
// never copied, a list passed as mut or own is named by no other argument,
// a variable moved by own is never used again and is never moved in a
// loop it was declared outside; a collection being ranged over, a loop's
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
// that computes it as Tack does, integers wrapping at their width, the
// smallest signed value divided by -1 being itself, a shift past the
// width shifting every bit out, maps iterating in insertion order, defers
// running when their block is left, the last first. Program.Expect runs
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
package progen

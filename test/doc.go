// Package compilertest drives the full compiler pipeline against Tack programs
// in process. It holds only tests.
//
// TestGolden runs every program under examples/, including the malformed ones
// under examples/invalid/, and compares what the compiler produced against a
// golden file in testdata/. A program rejected before code generation gives
// the same diagnostic whatever the backend, so it has one golden,
// <case>.golden. A program that reaches code generation is pinned once per
// registered backend, in <case>.<backend>.golden. Each golden records the
// stage that ended the run and that stage's output.
//
// Goldens record what the compiler does today. They show that output is
// stable, not that it is correct. After an intentional change to the output,
// regenerate them and read the diff:
//
//	go test ./test -update
//
// The other tests here assert intended behavior directly, so a failure in one
// of them is a bug, not a stale golden. TestAcceptsValidPrograms covers syntax
// the language should accept that no example exercises. TestRejectsInvalidPrograms
// checks that malformed input is reported by the stage that should notice it,
// with a message naming the actual problem.
//
// Every pipeline run is bounded by a timeout and recovers from panics, so an
// infinite loop or a crash in the compiler is reported as a failing test rather
// than hanging or killing the suite.
package compilertest

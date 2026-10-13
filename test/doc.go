// Package compilertest drives the full compiler pipeline against Tack programs
// in process. It holds only tests.
//
// TestGolden runs every program under examples/, each directory directly in
// it that holds Tack files, and compares what the compiler produced against
// a golden file in testdata/. A directory holding only main.tck is compiled
// as that file alone; any other as the package its files make. The harness
// reads every program into an in-memory file system and loads it through
// package loader, as the driver does from disk. A program rejected
// before code generation gives the same diagnostic whatever the backend, so
// it has one golden, <case>.golden. A program that reaches code generation is
// pinned once per registered backend, in <case>.<backend>.golden. Each golden
// records the stage that ended the run and that stage's output.
//
// TestInvalid does the same for the malformed programs, which live together
// in examples/invalid.txtar (format in package internal/invalid): one subtest
// per case, compared against that case's expected section, and failing if a
// case compiles. Diagnostics name the program <case>/main.tck. One case runs
// alone with
//
//	go test ./test -run 'TestInvalid/^garbage$'
//
// Goldens and expected sections record what the compiler does today. They
// show that output is stable, not that it is correct. After an intentional
// change to the output, regenerate them, which rewrites the golden files and
// the expected sections of examples/invalid.txtar, and read the diff:
//
//	go test ./test -update
//
// TestInvalidPackages does the same for the malformed programs of several
// files, in examples/invalid-packages.txtar (format in internal/invalid),
// naming each file by its path in the archive. A run the loader ends with
// an error rather than diagnostics, such as a directory with no Tack files,
// records the load stage and the error's text.
//
// FuzzFileOrder checks that the order of a package's files changes nothing
// the front end reports but positions: it loads and resolves the same files
// named to sort both ways.
//
// TestSafetyDoc ties docs/safety.md to the programs that test it. An HTML
// comment before each example or quoted diagnostic on that page names a case
// in examples/invalid.txtar or a directory under examples/, and the test
// checks the block is in that program and the diagnostic, an error or a
// note on one, in that case's expected output. Every case named safety-* must be named by the page.
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

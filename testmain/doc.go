// Package testmain generates the main tack test builds a package's tests
// with, and deals with what the compiler reports in it.
//
// # Role in the pipeline
//
// tack test loads a package for its tests (see loader.Loader.TestMain):
// its own files and its test files, those whose names end in _test.tck,
// parsed. The loader then asks for one file more, which this package
// writes from the package's syntax trees, before anything is resolved:
// Source, the generated main, a Tack file of the package under test
// named FileName. The loader parses it into the package, as its last
// file, and loads what it imports, std/testing. The driver sets the
// program's entry to its function (Entry, ast.Program.Entry), so the C
// backend calls it in place of main; the resolver asks for no main
// function (resolver.Resolver.MainOptional); and the type checker, with
// Tests set, checks every test's declaration. Diagnostics then deals
// with what any stage reported in the generated file.
//
// # Input and output
//
// Source takes a package loaded for its tests, the files it was read
// from, which give each test's place, and Options, the run's -run
// pattern, -v and the name the last line gives the package. It returns
// Tack source:
//
//	package p
//
//	import tack_testing_ from "std/testing"
//
//	var tack_test_names_ = [2]string{"test_a", "test_b"}
//
//	func tack_test_main_() {
//	    mut r := tack_testing_.start("m/p", "", false, tack_test_names_)
//	    mut r.run("test_a", "p/a_test.tck", 5, 6, "<excerpt>", test_a)
//	    mut r.run("test_b", "p/a_test.tck", 9, 6, "<excerpt>", test_b)
//	    exit(mut r.finish())
//	}
//
// A test is every function, not a method, of a test file whose name
// starts with test_ (Tests), in file order and then source order, the
// order they run in. Every one is passed to run, whether -run will run
// it or not, as std/testing matches the pattern at run time; the global
// array names them all for start, so the runner can count, should a
// test call exit, how many of the rest would have run. A test's place
// is its name's, as a diagnostic there shows it.
//
// # Invariants
//
// Every string in the generated file is a literal, as std/testing's
// start and run require, since testing.c keeps the strings; each is
// written in printable ASCII, any other byte, and every " and \, as \xHH,
// so a name, a path or a pattern of any bytes reads back exactly. The
// names the file declares end in an underscore and start with tack_; a
// package-level declaration of the package named like one, or an import
// named like the function or the array, is reported at its own name by
// Clashes, before any stage runs, so the clash is never reported in the
// generated file.
// When the package is std/testing itself, the file calls start
// unqualified and imports nothing.
//
// What the stages report in the generated file is never shown as it is:
// a test declared wrongly, or a syntax error in a test file, is reported
// where it is written, and the generated main's call of it is then wrong
// too, so Diagnostics drops every diagnostic located in the generated
// file when any other is an error, and otherwise turns each into an
// internal error at the start of the package's first file, since a
// generated main that does not compile when nothing else is wrong is the
// compiler's mistake. A note located there is dropped from a diagnostic
// that is kept.
package testmain

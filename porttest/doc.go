// Package porttest checks the Tack compiler written in Tack, the port,
// against this one, the Go compiler, which is frozen at the tag go-frozen
// while the port is written. It holds only tests: this file is its one
// non-test source, and it declares nothing.
//
// # Running
//
// TestPort, the check of the port, runs only when TACK_PORT_TEST is 1 and
// go test is given -count, as make port-test runs it:
//
//	TACK_PORT_TEST=1 go test -count=1 ./porttest
//
// go test caches a passing result keyed on this package's sources, not on
// the port's or on the binaries the tests run, so a cached result could
// pass a port that no longer does; with -count it never caches. Without
// TACK_PORT_TEST, as in go test ./..., TestPort skips, saying so, and
// builds nothing. With it but without -count, TestPort fails. The tests
// of the harness itself, which need no compiler, run either way.
//
// # Binaries
//
// TestMain gets two compilers, each once per run. The Go one is the
// binary $TACK_GO names, or, when that is unset or empty, ./tack of this
// module built into a temporary directory. The port is the binary
// $TACK_PORT_BIN names, or, when that is unset or empty, the program in
// the driver directory of $TACK_PORT, the port's source tree, which
// defaults to ../tack next to this module's root. A relative path in any
// of the three is taken from this module's root, not from where go test
// or make was run, which the tests cannot know. The port is built with
// the Go compiler, from the source tree's root, as
//
//	tack --out <temp>/tack-port ./driver
//
// with TACK_HOME set to this module's root, so std is the repository's
// src/std, and TACK_CACHE set to the Go compiler's temporary cache. A
// source tree with no driver directory, the default as much as one
// TACK_PORT names, or a driver that does not build, fails TestPort: once
// TACK_PORT_TEST asks for the check, nothing missing skips it. A checkout
// without the port still passes go test ./..., where TACK_PORT_TEST is
// unset and TestPort skips. Setting TACK_PORT_BIN to the Go
// binary runs the Go compiler against itself, which must pass every
// stage: it checks the harness, not the port.
//
// # Protocol
//
// The port is run with exactly the command line the Go compiler is run
// with, from the same working directory, with the same relative paths and
// the same environment but for TACK_CACHE: each compiler has a temporary
// cache of its own, so neither can read what the other wrote. A run of
// the port that exits with status 3 and writes to standard error exactly
// one line of the form
//
//	tack: not implemented in the port: <flag or argument>
//
// says the port cannot do that yet. Such a case skips, without running
// the Go compiler, unless its stage is named in required.txt, one name a
// line, where blank lines and lines starting with # are ignored; then it
// fails. A stage enters required.txt once the port implements it, so it
// cannot quietly stop working. A name in required.txt that is no stage
// fails TestPort, and TestRequiredFile in every go test ./porttest.
//
// # Stages
//
// TestPort has one subtest per stage, and under each one subtest per
// case, run in parallel, as many at once as go test's -parallel allows,
// GOMAXPROCS by default:
//
//   - backends runs --backends once.
//   - lex-single runs --lex on each single file of the corpus.
//   - ast-single runs --ast on each.
//   - res-single runs --res on each.
//
// backends and lex-single compare standard output, standard error and the
// exit status byte for byte. ast-single and res-single compare standard
// error and the exit status byte for byte, and decode both standard
// outputs as JSON, numbers kept as their text, and compare the values.
// That comparison ignores white space between tokens, the order of an
// object's keys and how a string's characters are escaped, \u0041 or A;
// a number's text counts, so 1 and 1.0 differ. The two standard outputs
// are compared byte for byte instead when the Go compiler's is not one
// JSON value, as on a syntax error, and when either is not valid UTF-8,
// which decoding would make equal by replacing each invalid byte with
// U+FFFD. A difference is reported by the first line that differs, of
// the bytes or of the two values written out as indented JSON with
// sorted keys.
//
// # Corpus
//
// The single files are every .tck file under examples/, run from this
// module's root and named examples/<path>, and every .tck file of every
// case of examples/invalid.txtar, examples/warnings.txtar,
// examples/invalid-packages.txtar and examples/warnings-packages.txtar.
// Each archive's cases are written, once per run, to a temporary
// directory of its own, each case as <name>/<path>, and run from that
// directory, so a file is named <name>/<path> as the golden tests name
// it. The files of a case with several are each run alone, as single
// files, its .std files among them. Every case is run with the
// repository's std, which a case's .std does not replace. The spec's code
// blocks in docs/spec and examples/invalid-c.txtar are not part of the
// corpus yet.
//
// # Fuzzing
//
// FuzzLexDiff runs both compilers with --lex on generated inputs, each
// written to fuzz.tck in a temporary directory of its own and run from
// there, and compares them as lex-single does, a not implemented answer
// skipping the input unless lex-single is in required.txt. The fuzz
// engine's coverage cannot see into the compilers' processes, so each
// input is also lexed in process by this module's lexer, and the
// guidance comes from the Go lexer alone: an input that reaches new code
// only in the port is found by chance. It is seeded with the lexer's
// committed FuzzLex inputs, lexer/testdata/fuzz/FuzzLex, and, when
// fuzzing, every single file of the corpus; run as a test, as make
// port-test runs it, it checks the committed inputs alone, since
// lex-single runs the corpus already. Like TestPort it runs only with
// TACK_PORT_TEST=1 and -count, and skips without them. Each fuzz worker
// is a process of its own that runs TestMain, so without TACK_GO and
// TACK_PORT_BIN every worker builds both compilers; make port-fuzz
// builds them once and passes them, fuzzing for $(FUZZTIME):
//
//	make port-fuzz FUZZTIME=60s
package porttest

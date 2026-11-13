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
// cannot quietly stop working. required.txt also names loaded and
// types-decls, the checks TestLoadOrder and TestTypes make outside
// TestPort. A name in required.txt that is no
// stage or check fails TestPort, and TestRequiredFile in every go test
// ./porttest.
//
// Every run of the port goes through a shell wrapper TestMain writes,
// which holds its stack to 4 MB with ulimit -s before it runs the port:
// half the 8 MB Linux gives a program by default, so a port whose
// recursion needs more than that for any input fails, where it would
// pass at the default.
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
//   - lex-packages runs --lex on each package of the corpus.
//   - ast-packages runs --ast on each.
//   - res-packages runs --res on each.
//   - check-decls, check-core, check-calls, check-facts, check-arith,
//     check-loops, check-own and check-generic run --check --max-errors 0,
//     every error reported, on the cases of the type checker's of S1,
//     S2a, S2b, S3, S4, S5, S6 and S7, the stages the type checker is
//     ported in (see Type checker stages).
//   - check-backend runs the same on the cases the backend rejects,
//     whose --check output holds the backend's errors, and on those
//     with no error before the backend, warnings aside, whose --check
//     output the backend decides too (see Type checker stages).
//   - check-clean runs the same on each package input that is no case of
//     an archive: the examples, std and the port's driver. Go accepts
//     nearly all of them and runs its backend on each it accepts, so
//     until the backend is ported, at M7, check-clean is wholly the
//     backend's: the port's --check declines every program in which
//     the stages before the backend find no error. TestTypes covers
//     the type checker on them.
//
// backends, the lex stages and the check stages compare standard
// output, standard error and the exit status byte for byte. The ast and
// res stages compare standard error and the exit status byte for byte,
// and decode both
// standard outputs as JSON, numbers kept as their text, and compare the
// values.
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
// The packages are every directory under examples/ and under src/std
// that holds a .tck file directly, run from this module's root and named
// examples/<path> and src/std/<path>; every module under examples/, a
// directory holding a tack.mod, and std, each run from its root as .,
// and each of its packages by its import path from there; and every
// case of the archives of several files a case that is built from a
// directory, its target, run from its archive's directory; and the
// port's own driver, ./driver run from the port's source tree when it is
// there, the largest program of all, which imports the whole port and
// most of std.
//
// # Type checker stages
//
// The type checker is ported in stages, each needing the ones before
// it: S1 the declarations, S2a statements and expressions, S2b calls and
// containers, S3 facts, S4 arithmetic and relations, S5 loops and
// contracts, S6 ownership and S7 generics and closures. Its cases are
// every case of examples/invalid.txtar, examples/invalid-packages.txtar,
// examples/warnings.txtar and examples/warnings-packages.txtar whose
// program Go's --check loads, and so type checks, whatever the stage
// that reports its errors: a resolve case, a parse case whose tree the
// parser recovered, a case the backend rejects. Each is run as the other
// stages run its archive's cases, but for a case with a std of its own,
// its .std, which runs with TACK_HOME set to a directory beside the
// archives' whose src/std holds that std, as test/ loads it in place of
// the repository's. Each is the case of one check stage, the first whose
// port can pass it: the higher of two stages, worked out in process at
// the start of the run, the same each time. A case the backend rejects,
// whose expected stage is codegen, is check-backend's instead, whatever
// its two stages: --check shows the backend's errors, which the port
// gives only once its backend is ported, so check-backend is required
// at M7 and not before. So is a case in which Go's loader, resolver and
// type checker, run in process, report no error, a warnings case among
// them: --check runs the backend on it, and what it shows, warnings
// and exit status alike, depends on whether the backend reports.
//
// The first is that of its messages. Every string literal, and every
// string built with + from literals and calls of fmt.Sprintf, of the
// non-test files of typeck is harvested with go/parser when the tests
// run, a format's verbs taken as any text, and given the stage of the
// file it is in, or of the function, by its receiver's type and its
// name, for the files split between stages: check.go, expr.go,
// extern.go, struct.go, union.go, list.go, maps.go and closure.go, and
// the declaration phase's helpers in files of later stages, which the
// port of S1 carries, view.go's paramType and plainParamType,
// viewresult.go's resultType and generic.go's resolveInterfaces,
// implicitTypeParams and resolveTypeParams; the rest of generic.go,
// instantiation, constraints and type arguments, is S7. A file
// of typeck with no stage fails the harvest, and a function the split
// names that its file does not declare fails TestTypeckFilesStaged.
// The packages outside the checker whose diagnostics --check shows,
// lexer, parser, loader, modfile, resolver, constant and the backend's,
// are harvested too, as a stage of their own below S1. A literal with
// no two letters in a row, as "%s: %s", is no template. Each message,
// warning and note of the case's expected output is matched against
// them: of the templates that match the whole of it, those that fix the
// most of its text decide, and when no template does, those found
// within it that together fix the most of it without overlapping, for
// a message built from pieces, as an error with advice appended; a
// template a piece may be needs six literal bytes, a space and two
// words, or one in ten bytes, so that a name is no piece. When those
// that decide are of several stages, the message is of the highest, and
// when none matches, of S7. TestClassifyCounts fails for every message
// no template matches.
//
// The second is that of Go's typed tree of the case, loaded as --check
// loads it, as a library may be, resolved and checked, every package's
// files, std's and any other import's as much as the case's own, since
// Go's checker checks every function's body: S1 when no function has a statement in its body, S2a when
// one does; S2b with a call that is no conversion, a mut or own
// argument, a type assertion, a ? or a value or type of a list, map,
// arena, pool, view, id, span, handle, any or error; S3 with an index or
// a divisor that is not a constant, or a read of a variable that a
// binding of an error holds, or that the ok of a checked pop or remove
// holds, which Go's checker allows only once the facts in force prove
// the error or the ok checked; S4 with integer arithmetic, a unary
// operator or a compound assignment with no constant value, an
// increment or decrement, a slice, a conversion of a value whose type
// does not widen to the target's, or a conversion in its checked form;
// S5 with a counter loop, a three-clause loop whose init declares a
// mutable variable, any loop that writes an integer declared before it,
// whose bounds its invariants carry, an ensures clause or unreachable;
// S6 with an owned parameter or variable, a region, a call with two mut
// arguments or more, a call of drop or swap, or a function whose result
// is a view; and S7 with type parameters or arguments, a fields loop,
// a function literal, a function type, a function's name anywhere but
// as a callee, whether the checker accepted it as a value or not, a
// value of a function type or a call of one. Where an error
// leaves a type unknown, an operator that could be an integer's counts
// as one.
//
// TestClassifyCounts logs how many cases each stage has, and what set
// their stages; TestClassifyHandChecked checks the stages of cases read
// by hand, and TestClassifyDeterministic that classifying twice agrees.
// They need no compiler and run in every go test ./porttest. None of
// the check stages is in required.txt until the port's --check passes
// it.
//
// TestCheckExpected runs the Go compiler on every check case as the
// stages run it and checks that it gives the archive's expected output,
// as test/ made it, so the command line the port is compared on is the
// one the archive was made with. A file of a case's own std is named by
// Go below its TACK_HOME, and is compared under the archive's name for
// it. The cases for which the driver's --check differs from test/'s
// pipeline by design are listed in checkDiffers, with why: for a
// program the backend rejects it shows the warnings with the backend's
// errors, it needs no main, and it names std by its absolute path. The
// test fails when a listed case stops differing. Like TestPort it runs
// only with TACK_PORT_TEST=1 and -count.
//
// The 16 type checker cases of examples/invalid-tests.txtar are no
// check case: their diagnostics need a package's tests loaded, which
// --check cannot ask for. They are compared when tack test is ported,
// at M8.
//
// # Loading
//
// TestLoadTrees makes file trees the corpus has none of, in a temporary
// directory: symbolic links, to files, to directories, to nothing and to
// themselves, where a package's files are; names that are not UTF-8,
// hold a newline or dots, or differ only in case; files and directories
// that cannot be read or searched; a tack.mod that is a directory or
// above the current directory, and a current directory reached through
// a symbolic link, with and without PWD naming it; every way a target
// can be written relative to the current directory; imports of a file,
// through a file, through a link, of std/builtin and of the main
// package; a C file named like an option; a directory 300 deep; and an
// import chain of 3000 packages closed by a cycle. Each runs as the
// three package stages run, with the environment an input adds; the
// port may answer not implemented for the trees it cannot read the way
// Go words, even in a required stage.
//
// TestLoadOrder checks what the port loads, for every package input of
// the corpus, the port's own driver among them, and every input of
// TestLoadTrees but those that set PWD: --res shows none of a package's
// directory or C files, nor the program's error count, nor what an
// import no qualified name uses is bound to, and dumps nothing at all for
// a program with an error, so the port writes these with --port-loaded,
// a flag of its own, and this module's loader, run in process as the
// driver runs it for --res, gives what they must be: every package of
// the program in order, with its directory, files, C files and what
// each import is bound to, the program's error count, and every
// diagnostic in the order it was reported. --port-loaded is a test hook
// the port keeps until its builds check the same. A port that answers
// it is not implemented skips, unless loaded, the check's name, is in
// required.txt, as it is, where then it fails, but for an input marked
// as one the port may decline.
//
// # Types
//
// TestTypes checks what the port's type checker finds, for every input
// of the corpus, single files and packages, the port's own driver among
// them, that the Go compiler accepts with no error, and for every case
// of check-decls, check-core, check-calls and check-facts, the type
// checker's cases whose stage is S1, the declarations, S2a, statements
// and expressions, S2b, calls and containers, or S3, facts:
// each loaded, resolved and checked in process by this module's loader,
// resolver and checker, typeck.New(...).Check(), as --port-types checks
// it, whatever the stages before reported. Any other input of the corpus
// is no input of the check, and passes at once, counted in the log. The
// port writes what it finds with --port-types, a flag of its own, as
// flat JSON, lists and never a nested tree, so a program of any size
// dumps: its phase, every type of the table with every query the Table
// answers about it, every declaration of the program, in prog.Decls()
// order, with the types and the constant value the checker recorded on
// it, the instances, the errors, in order, each with its file, its
// span's start and end, its message and its notes, once bodies are
// checked every node of every file, in package order and ast.Walk's,
// with its kind, its type, its constant, what a call converts to, what
// a name was narrowed to, whether a switch is exhaustive and whether a
// variable was unrolled, and the kinds of
// judgment the port took as holding without proving them, logged, not
// compared, and none so far. The test builds the same from Go's checker
// and compares what the port's phase covers: in "bodies", everything,
// exactly, Go's whole table and every node among it; in "decls", which
// the port writes when it declines a body, its declaration phase,
// everything Go's Check does before the first function body
// (typeck/check.go:604), the declarations, the errors and the table up to
// the port's count, which must be a prefix of Go's, since every type Go
// adds before bodies comes first. A function's value Go sets
// only where a body uses it as a value, so in "decls" Go's is compared
// as it stood then, invalid; the instances are found after bodies. A
// port that answers --port-types is not implemented skips, unless
// types-decls, the check's name, is in
// required.txt, as it is, where then it fails. An input whose Go typed
// tree's stage is S1, S2a, S2b or S3 (see Type checker stages) must dump in the
// phase "bodies" when types-bodies is in required.txt, as it is: the
// port's body check covers every such input. TestTypesDeep runs the
// same check on declaration chains a hundred thousand links long, which
// have no nesting limit and which Go's checker walks with a frame a
// link: globals each naming the next, in its value and in its type's
// array length, structs each holding the next by value, and array
// lengths, values and bools written as long chains of operators; and a
// thousand globals each adding types before and after it names the next,
// whose IDs show that the checks of globals nested in each other add
// them in Go's order. The port runs with its stack held to deepStack KB.
// Both run only as TestPort does.
//
// TestCheckDeep runs --check --max-errors 0, compared as check-core
// compares its cases, on bodies nested as deeply as the parser allows,
// in every statement that holds a block and in parenthesized, negated
// and array literal expressions, and on chains a hundred thousand links
// long, else ifs, statements, && chains, in a value and in an if's
// condition, locals each the next's value, an else if chain each of whose
// branches returns, early exits each giving the rest of their block a
// fact, an else if chain each of whose conditions gives the next link a
// fact, and a selector down a chain of structs each holding the next,
// and an else if chain of early exits each giving the statement after it
// a fact, two thousand links long, which Go's checker takes time
// quadratic in; and on calls: calls, method
// calls, calls of a method expression and struct literals nested in each
// other's arguments as deeply as the parser allows, with an invalid or an
// untyped constant at the bottom too, chains of method calls ten thousand
// links long, as long as Go's checker, which takes time quadratic in
// one, checks in seconds, on a name bound to nothing and with a missing
// method too, an argument list a hundred thousand values long, of the
// wrong type too, a field chain as long passed as mut and assigned, and a
// printf whose format has as many verbs, run with a std whose fmt holds
// only the format functions, which the port's checker can check. Each
// ends in an error, so the backend decides nothing, and most hold a
// function with the same shape and no error too, whose moves Go's
// checkFunc then follows; chains of method calls each followed by a ?
// are among them. The port runs with its stack held to deepStack KB, as
// TestPort does.
//
// TestCheckDeepPairs nests every ordered pair of a family of
// constructs an expression nests in, alternating, as deeply as the
// parser allows: a call's argument, a method call's argument and
// receiver, a call of a method expression, an operator's left and right
// operands, !, parentheses, an array literal's element and a struct
// literal's field read back, a union's payload, a ?, a value passed as
// any, a conversion, a value tested in an if and a switch, the right
// side of an && and of an || whose left side gives it a fact, and an
// element whose index the facts around it prove in range. Each pair
// runs as a valid nest, as one with a value of the wrong type at its
// bottom and as one with an untyped constant there, under --check,
// compared as check-core compares its cases, and under --port-types,
// compared as TestTypes compares a case of the type checker's, and as a
// nest in a program Go accepts, under --port-types, compared as
// TestTypes compares it in the phase "bodies", the port's stack held to
// deepStack KB: every level of every construct must cost the port's
// checker less than that over the nesting limit. Four pairs, an || whose
// facts an && around it asks for and the && it holds, are nested a
// quarter as deeply: Go's checker takes time cubic in their depth, and
// each of their routes is nested to the limit in its other pairs.
//
// # Deep nesting
//
// TestDeepNesting runs the programs that cost the most stack for each
// level of nesting, in expressions, statements and types and in every
// place an expression is parsed from, at the parser's nesting limit, a
// thousand levels, and one past it, through both compilers with --ast
// and with --res, compared as ast-single and res-single compare them,
// the port's stack held to 4 MB. A port whose recursion, the parser's or
// the resolver's, needs more than that at the limit fails here. At the
// limit the trees of most of them would dump to hundreds of megabytes of
// Go's indented JSON, so a syntax error after the nesting leaves the
// dump out while the parse, and the resolver after it, still go all the
// way down; a few cheap ones are dumped whole. TestDeepChains runs, with
// --res, chains the parser builds with no nesting it counts, a hundred
// thousand links long: selectors, indexes, slices, method calls, type
// assertions, ?, binary operators, assignment targets, else ifs and
// statements, which the resolver must walk without a frame a link, and
// one with no error, whose tree both compilers refuse to dump as nested
// too deeply. Both run as TestPort does, with TACK_PORT_TEST=1 and
// -count, and skip otherwise.
//
// # Fuzzing
//
// FuzzLoadDiff writes each generated input as a file tree in a
// temporary directory, entries separated by 0 bytes, each a path, a
// newline and the file's content, and runs both compilers on the
// directory of its first file: what the port loads, compared as
// TestLoadOrder compares it, then --ast and --res, compared as
// ast-packages and res-packages compare them. The tree is loaded and
// resolved in process by this module's loader and resolver first, for
// the fuzz engine's coverage. Run as a test it
// checks its seeds, written in the test.
//
// FuzzLexDiff runs both compilers with --lex on generated inputs, each
// written to fuzz.tck in a temporary directory of its own and run from
// there, and compares them as lex-single does, a not implemented answer
// skipping the input unless lex-single is in required.txt. FuzzParseDiff
// does the same with --ast, compared as ast-single does, and FuzzResDiff
// with --res, compared as res-single does. The fuzz engine's coverage
// cannot see into the compilers' processes, so each input is also lexed
// in process by this module's lexer, and, for FuzzParseDiff and
// FuzzResDiff, parsed by its parser when it lexes, as the loader does,
// and for FuzzResDiff resolved by its resolver as a program of that one
// file, and the guidance comes from the Go lexer, parser and resolver
// alone: an input that reaches new code only in the port is found by
// chance. FuzzLexDiff is seeded with the lexer's committed FuzzLex
// inputs, lexer/testdata/fuzz/FuzzLex, and FuzzParseDiff and FuzzResDiff
// with their own, porttest/testdata/fuzz/<name>, which go test reads
// itself; when fuzzing, each is seeded with every single file of the
// corpus too. Run as a test, as make port-test runs them, each checks
// its committed inputs alone, since the single-file stages run the
// corpus already.
// Like TestPort they run only with TACK_PORT_TEST=1 and -count, and skip
// without them. Each fuzz worker is a process of its own that runs
// TestMain, so without TACK_GO and TACK_PORT_BIN every worker builds both
// compilers; make port-fuzz builds them once and passes them, fuzzing
// $(PORT_FUZZ), FuzzLexDiff by default, for $(FUZZTIME):
//
//	make port-fuzz FUZZTIME=60s
//	make port-fuzz PORT_FUZZ=FuzzParseDiff FUZZTIME=60s
//	make port-fuzz PORT_FUZZ=FuzzResDiff FUZZTIME=60s
//	make port-fuzz PORT_FUZZ=FuzzLoadDiff FUZZTIME=60s
//
// # Embedded files
//
// The Go compiler embeds the builtins' documentation, builtin.tck, and
// the C runtime, its header and its C files, with go:embed; the port
// holds them as string literals in its builtin/embedded.tck, which its
// tools/embed writes, as make port-embed runs it. TestEmbedded reads that
// file with this module's lexer and parser, as the Go compiler reads it
// building the port, and checks that it holds each of builtin/'s
// originals byte for byte, the C files in the byte order of their names,
// as the Go compiler lists them; and builds tools/embed and runs it on
// the originals, to check that the file is what it writes.
//
// # Performance
//
// TestResPerf is a tripwire: it runs --res of the port's driver, the
// largest program the corpus has, three times through each compiler, one
// run at a time, measuring wall time and peak memory, each compiler
// started by a launcher, the test binary run anew, which reads the peak
// from wait4; it fails when the port takes more than twice Go's time and
// a tenth of a second, or twice its memory, and logs both.
package porttest

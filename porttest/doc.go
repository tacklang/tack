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
//   - probes runs the same on each program of testdata/probes, and on
//     the target each directory of it names in its file target, the
//     probes of reviews that found the port giving other output than
//     Go where no case of the corpus did, each kept so that it stays
//     covered; TestProbesPairStack runs them again with the port's stack
//     held to pairStack KB.
//   - check-clean runs the same on each package input that is no case of
//     an archive: the examples, std and the port's driver. Go accepts
//     nearly all of them and runs its backend on each it accepts, so
//     until the backend is ported, at M7, check-clean is wholly the
//     backend's: the port's --check declines every program in which
//     the stages before the backend find no error. TestTypes covers
//     the type checker on them.
//   - emit-single runs --emit on each .tck file under examples/.
//   - emit-packages runs --emit on each package and module of
//     examples/, as the package stages name them.
//   - emit-lib runs --check --emit on each package of std, by its path
//     and by its import path, and on the port's driver: --emit of a
//     package that is not main is an error, and --check checks one as a
//     library, whose C it then emits.
//   - emit-cases runs --emit on each case of the warnings archives, the
//     programs with no error, and on each program harvested from the C
//     backend's tests (see Backend stages).
//   - emit-header runs --emit-header, and --check --emit-header for a
//     package of std, on each input of emit-single, emit-packages and
//     std whose package holds a C file or an extern func, a directory
//     outside a module, which the loader rejects, left out: each
//     compiler in a fresh copy of the tree at the same path, the
//     input's directory and std, with TACK_HOME the copy's, and every
//     tack.h of a package directory removed first, so that the tack.h
//     files there afterwards are what the run wrote. It compares the
//     files each copy holds afterwards, path and bytes, too, and fails
//     a case Go passes without writing a tack.h.
//
// check-backend, check-clean and the emit stages classify their cases
// by the stages of the backend's port each needs (see Backend stages),
// and the emit stages compare standard output, standard error and the
// exit status byte for byte.
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
// # Known divergences
//
// The port does not copy the Go compiler's bugs. Where an accepted
// program reaches one, the port does what is right, as ruled, and the
// case is a known divergence, an entry of testdata/divergences.txtar:
// the case, by its suite, the name of the stage whose case it is, or
// emit-probes for TestEmitProbes's programs, and by its subtest's name;
// what the Go compiler gets wrong; what the port does instead and what
// ruled it, with its date; and the port's output, reviewed: its exit
// status, its standard output and its standard error, each but those
// the entry's same names, where the port gives what Go gives, so an
// entry whose C is Go's records its warnings alone. Wherever the case
// runs, compareResults compares the port with that output in place of
// Go's, as the stage compares the two, and fails the case when Go gives
// it, the entry having gone stale, as much as when the port does not; a
// case the list holds that the port declines fails, required or not, so
// an entry is never a skip.
//
// An entry applies to its suite's cases alone. TestPort marks each case
// of a stage as that stage's, TestProbesPairStack the probes as the
// probes stage's, and TestEmitProbes its programs as emit-probes'. An
// input of no suite, as the deep, perf and fuzz tests make, never takes
// an entry, and fails when an entry of the stage it runs as names it, so
// that no entry applies silently to an input that only shares a case's
// name. TestPort fails a stage for each entry of it whose case the stage
// does not have, and TestDivergencesFile, in every go test ./porttest,
// reads the list, rejecting a malformed one, and fails an entry of the
// probes or the emit probes naming no program of theirs. The files an
// emit-header case leaves are no part of an entry, and are compared with
// Go's always. TestParseDivergences, TestParseDivergencesRejects,
// TestDivergenceVerdict, TestMatchDivergence, TestMissingDivergences and
// TestDivergedDecline check the mechanism itself.
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
// whose bounds its invariants carry, an ensures clause, unreachable or
// an immutable error declared with an append's value, whose success
// proves the list's length (Go's appendedLen);
// S6 with an owned parameter or variable, a region, a call with two mut
// arguments or more, a call of drop or swap, or a function whose result
// is a view; and S7 with type parameters or arguments, a fields loop,
// a function literal, a function type, a function's name anywhere but
// as a callee, whether the checker accepted it as a value or not, a
// value of a function type or a call of one. Where an error
// leaves a type unknown, an operator that could be an integer's counts
// as one.
//
// The walk notes every stage some part of the tree implies, not only the
// highest: a tree of S7 that implies S4 or S5 as well waits for those
// stages too (see TestTypes).
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
// # Backend stages
//
// The C backend is ported in stages, M7's: E0 the handover, H the
// header, E1 the core, E2 values, E3 owned values and containers and E4
// generics and functions, named emit-E0, emit-H, emit-E1, emit-E2,
// emit-E3 and emit-E4 in required.txt. What --emit, --emit-header and
// --check give for a program the type checker accepts depends on the
// backend's paths, the order it renders in, its spills, equality
// helpers, frees, defers and thunks, which no walk of the typed tree
// predicts, so a case is classified by what Go's backend runs on it: Go
// is built once a run with go build -cover -covermode=set
// -coverpkg=./tack,./codegen/...,./types, into the run's temporary
// directory (the driver's package must be among them: built with only
// other packages, go1.27's programs write no coverage when they exit),
// each case is run through it as its stage runs it, with GOCOVERDIR set,
// and go tool covdata textfmt lists the blocks it ran. Each block of
// codegen and types gets a stage: by its file, as the M7 plan splits
// them (emit_stages_test.go's emitFileStages), or by the function it is
// in, found with go/parser as the type checker's split files' are, for
// the functions emitFuncStages names: every function of gen.go, each
// group with why, the functions of later stages' files every core
// program runs for real, E1's (see below), union.go's free helpers but
// emitFrees, E3's, and generate's leading
// check of tack.h, headerDiags, and its return, H's (emitSplits). A
// source file with no stage, a gen.go function the table does not stage
// or stages twice, and a name it gives that its file does not declare
// fail TestEmitStagesComplete. Every file of types, and the driver, are
// E0's, which every case needs.
//
// That is a change to the plan's scope of E1, ruled so and overrulable:
// these functions of later stages' files are E1's whole, every branch
// of them, not only the paths a core program takes: array.go's indexExpr
// and lenValue, whose array, arena and pool branches so count as E1,
// slice.go's elementExpr, defer.go's funcFrame, generateJump and stops,
// view.go's callArgs, argTypes, paramsOf, viewParamAt, packs and hasLen,
// struct.go's walk of the types, emitTypes, emittedTypes, isComposite,
// hasEqualityHelper, parts, emitTypeDef, emitDef and comparedTypes, and
// its constOf and specialConst, the type walk's filters arena.go's
// arenaTypes, maps.go's mapTypes, pool.go's poolTypes and funcvalue.go's
// usesFuncTypes, union.go's emitFrees, whose dispatch to each kind's
// free helper so counts as E1, and switch.go's caseVarsRead. The helpers
// they dispatch to stay their own stages'.
//
// Code of a later stage runs in every program: the core asks, of every
// name and every call, whether it is a capture, a generic call, a list's builtin, and so on, and the answer's
// not-taken path is the core's to port. So each checkpoint, E1, E2 and
// E3, has baseline programs written in its stages' features alone
// (emit_baseline_test.go's baselinePrograms: hello, scalars, strings
// and a library for E1; structs, arrays, unions, switches, slices, any,
// several values and the checked forms for E2; lists, maps, arenas,
// pools, defers, ?, own, replace, regions and drop for E3), each run
// with --emit, or --check --emit for the library, and --check. A block
// of a later stage that E1's baselines run is E1's, every lowered
// program needing E1; one that only a later checkpoint's run is the
// work of its own stage and of each such checkpoint, any of which may
// be ported first, so a case that needs none of them for its other
// blocks needs the first of them. TestEmitBaselineFeatures checks, from
// each baseline's typed tree, not from what it runs, that it uses no
// feature of a later stage, and TestFeatureStagesSeesLaterFeatures that
// the check sees one. TestEmitFeatureBound checks the other way: every
// single-file case of emit-single and emit-cases Go's front end accepts
// needs no stage beyond its own features' and H, and so does every
// directory case of emit-packages, emit-lib and emit-header, so every
// not-taken path the corpus runs is some baseline's, with
// featureBoundAllowed for exceptions, none so far. Its slack: a case given by its import path,
// and the port's driver, which loads past typedTree's timeout, are not
// bounded; and featureStages counts a division E2's even when the
// checker folded it to a constant. Blocks a run of --backends covers, the
// package variables' initializers, which every process runs, count for
// nothing. TestStagesOfSynthetic checks each of these rules on coverage
// and baselines made by hand. A case needs E0 and the stage of every other block it runs; the 26
// cases the backend rejects, whose expected stage is codegen, need E0
// and H alone, which TestEmitHeaderRejects checks. A case the port
// declines skips, unless its stage is in required.txt, or every stage
// it needs is; while emit-E0 is not, no case is classified for that,
// and TestDeclinedRatchet checks the rule with required stages of its
// own.
//
// What a case runs is cached, below $PORTTEST_COVER_CACHE, or
// tack-porttest/cover in os.UserCacheDir, keyed on the
// instrumented binary's SHA-256, the command line and the bytes the run
// can read: the files of the input's module, or of its directory, or, for
// a single file, the file and the C beside it, each named from the
// working directory, so a case's key is the same wherever the archives
// are written, and the std it runs with. A fuzz input's is never cached.
// TestEmitCensus classifies every case of every stage that classifies
// them and logs how many need each set of stages, by stage and in all,
// how many each stage would leave required, ported in each order the
// plan allows, and the same were the header's part of every program,
// newHeader's walk of a program with no extern func, the core's;
// TestEmitBaseline logs, for each checkpoint, the functions of later
// stages its baselines run; TestEmitClassifyDeterministic runs every
// case again, the cache left alone, and checks that it needs the stages
// the cache's run gave it, logging how many cases run other blocks, as
// a sort over a map's keys may.
// They run only as TestPort does.
//
// emit-cases's programs from the C backend's tests are harvested by
// running those tests, go test ./codegen/c, with -overlay swapping in
// copies of gen_test.go and printf_test.go whose compile and
// compileWithStd, the helpers every test's program goes through, first
// write their src to a directory, and with a clang on PATH that only
// writes, at its -o, a program that exits 0; no file of the frozen tree
// changes. The programs, each once, those that start with a package
// clause, are cached keyed on Go's version, porttest's harvest file
// whole, every Go file of the module but porttest's, go.mod and go.sum,
// and examples/, src/std, codegen/c/testdata and builtin/, which the
// tests read or the compiler embeds; a key the cache misses harvests
// anew. TestEmitHarvest pins how many there are and their
// digest, so a harvest that changes fails.
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
// of check-decls, check-core, check-calls, check-facts and check-arith,
// the type checker's cases whose stage is S1, the declarations, S2a,
// statements and expressions, S2b, calls and containers, S3, facts, or
// S4, arithmetic and relations, of check-loops, S5, loops and
// contracts, and of check-own, S6, ownership, each once it is in
// required.txt (until S5 is ported, some cases of either are declined:
// a case is of the highest stage it needs, and S6 orders after S5), and
// of check-generic, S7, generics and closures, whose cases that need S5
// too skip when the port declines their bodies, until check-loops is in
// required.txt:
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
// a name was narrowed to, whether a switch is exhaustive, whether a
// variable was unrolled and whether a name moves its variable's value,
// Ident.Move; a call of a generic function with its type arguments and
// its parameters' and results' types, a fields loop's fields(T) with
// what it ranges over, the type parameter its fields have, whether its
// body reads their names and the fields it lists for each type, a
// switch on a type parameter with how each value of each case matches,
// and a closure with its captures; what else the backend reads of a
// node (see handoverEntry): a call's Type, Method and Dynamic, a method
// expression's Method, each a function's place in prog.Decls(), name,
// value, parameters' and results' types and the interface method a
// stand-in is for, a ?'s Results, an unreachable's Where, a variable's
// Unused, a name's Def, a union case pattern's cleared, the Types of
// each case of a type switch, where the clause a
// fallthrough enters starts, a closure's function's Value and
// ResultTypes, and the testing.Site arguments the checker puts among a
// call's arguments, with the parameter a name passes on and the file,
// line, column and excerpt a literal holds; what the program needs of
// the runtime, builtin's Used, RunsC, UsesUnreachable and UsesLists, in
// "bodies"; and the kinds of
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
// tree's stage is S1, S2a, S2b, S3 or S4 (see Type checker stages), S5
// once check-loops is in required.txt, S6 once check-own is, or S7 when
// no part of the tree implies S5 or check-loops is in required.txt, must
// dump in the phase "bodies" when types-bodies is in required.txt, as it
// is: the port's body check covers every such input. TestTypesDeep runs the
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
// quadratic in; on arithmetic: sums a hundred thousand terms long, proven
// and not, chains of wrapping additions, divisions and masks added to, an
// index adding 0 as long, an unsigned subtraction chain four hundred
// links long, which Go's checker takes time cubic in, and two thousand
// statements stepping a variable, by +=, ++ and *=; and on calls: calls, method
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
// are among them. And on owned values, in a function h with no error,
// whose moves and escapes Go's checkFunc follows, and in main: every
// statement that holds a block nested as deeply as the parser allows
// around moves of owned locals, a range over a list among them, and
// regions making lists; locals a hundred thousand long each moved into
// the next; an else if chain two thousand links long moving a local in
// every branch, as long as Go's move check, which takes time growing
// faster than the square of the chain's length, checks in seconds; and
// a field chain a hundred thousand structs deep ending in a list, read,
// assigned and passed as mut. These but the field chain are run with
// --port-types too, compared as TestTypes compares a case of
// check-core. The port runs
// with its stack held to deepStack KB, as TestPort does.
//
// TestCheckDeepPairs nests every ordered pair of a family of
// constructs an expression nests in, alternating, as deeply as the
// parser allows: a call's argument, a method call's argument and
// receiver, a call of a method expression, an operator's left and right
// operands, !, parentheses, an array literal's element and a struct
// literal's field read back, a union's payload, a ?, a value passed as
// any, a conversion, a narrowing one, + - * and unary - whose overflow
// the bounds of an & prove, a wrapping +, a division, shifts, a slice's
// bound, a value tested in an if and a switch, the right
// side of an && and of an || whose left side gives it a fact, and an
// element whose index the facts around it prove in range, a generic
// function's call with its type argument written and one inferring it,
// each also under a ?, a value passed as any to a function and to a
// generic one, under a ?, a call through a function value held by a scope
// parameter, also under a ?, by a local and by a local holding a method
// expression, and a closure returning the value, passed to a scope
// parameter, also after a loop writing a local declared before it, whose
// bounds a probe of the body looks for, and before an unreachable its
// return proves, and a call of a function with a contract; and of
// values that own a list, a call and a method call taking one as own, a call
// taking another variable as own beside it, a struct literal's owned
// field, replace, each with a place of its own, and a closure returning
// one after a probed loop. swap and drop give
// no value, and nest in nothing. Each pair
// runs as a valid nest, as one with a value of the wrong type at its
// bottom and as one with an untyped constant there, under --check,
// compared as check-core compares its cases, and under --port-types,
// compared as TestTypes compares a case of the type checker's, and,
// unless a route of it is one Go always reports, a narrowing conversion
// or a slice's bound, as a nest in a program Go accepts, under
// --port-types, compared as
// TestTypes compares it in the phase "bodies", the port's stack held to
// deepStack KB, and each --check nest again at pairStack KB, 3.5 MB:
// every level of every construct must cost the port's checker less than
// that over the nesting limit, which leaves half a megabyte of deepStack
// for what later stages add. Four pairs, an || whose
// facts an && around it asks for and the && it holds, are nested a
// quarter as deeply: Go's checker takes time cubic in their depth, and
// each of their routes is nested to the limit in its other pairs. So is
// a pair holding a probed loop, whose probe takes Go's checker memory
// cubic in the depth, as are TestCheckDeepStmts's nests of probed loops,
// an eighth as deeply, and its nests of a loop and a statement putting
// in force a fact the loop's writes end, a quarter as deeply, for the
// same reason, as are its nests of counting loops, which take Go's
// checker time cubic in the depth.
//
// TestCheckDeepPortOnly nests, at the limit, what TestCheckDeepPairs and
// TestCheckDeepStmts nest shallower for Go's sake: probed loops of every
// kind, counting loops, loops around facts and contract calls, and the
// closures holding a probed loop. Go's checker runs out of memory there,
// so the port at pairStack KB is compared with the port at 64 MB: the
// same exit status and output, and never killed.
//
// TestCheckDeepStmts nests every ordered pair of the statements that
// hold a block, an if, an else, an else if, loops, a counting loop and
// loops writing a variable declared before them, switches of every
// kind, one whose case falls through into the default holding the rest
// among them, a defer, a region, an unreachable a return before it proves and
// one the facts prove, and a call whose contract gives a variable a
// fact, alternating, as deeply as the parser allows, and each around a
// value nested in every route of the bool family, compared as
// TestCheckDeepPairs compares its nests.
//
// TestCheckDeepGeneric nests, in a generic function's body, every
// ordered pair of fields loops, switches on a type parameter, each on
// the one the case around it declares, closures passed to a scope
// parameter, ifs and ranges, alternating, as deeply as the parser
// allows, valid, with a value of the wrong type and in a program Go
// accepts, as TestCheckDeepStmts does; a nest holding switches on a
// type parameter is compared under --port-types a hundred levels deep,
// since every case narrows the switches around it to a type of its own,
// and the whole nest's dump would run to gigabytes. And it runs chains
// of generic functions each calling the next, nine thousand links long,
// whose instances are found one from another, as Go's checker finds
// them from a worklist, and one more than ten thousand long, which
// needs one instance too many.
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
// TestEmitDeep runs --emit, compared as emit-single compares its cases,
// on programs Go's backend lowers nested as deeply as the parser
// allows, in every statement that holds a block, regions, closures,
// closures each writing a capture of the one around it, fields loops
// and switches on type parameters in an instance, calls through a
// function value, parenthesized, negated and array literal expressions,
// list and array types and a generic function's list type argument,
// whose instances instantiate one another down it, and on chains a hundred thousand links long, else ifs,
// statements, && chains and locals, the port's stack held to deepStack
// KB at the -O3 it is built at. TestEmitTypeChains runs chains of
// structs each holding the one before by value, plain, owning a list,
// compared, a map's key, an extern func's parameter, a fields loop's
// instance for each, each instantiating the next's, and make of the
// outermost, under --emit and
// the last under --emit-header too, typeChainLength links long: Go's
// backend takes time quadratic in such a chain, so TestEmitDeepPortOnly
// runs them a hundred thousand links long on the port alone, at
// deepStack KB against bigStack KB, as TestCheckDeepPortOnly does, a
// decline judged by the shorter twin's stages. TestEmitDeepPairs runs
// every program of TestCheckDeepPairs and TestCheckDeepStmts that Go
// accepts, with the bool family's routes paired too with emitPairRoutes,
// nested closures capturing through each other and a closure passed to
// a generic function, each program whole and pruned of what it never
// uses, at deepStack KB and at pairStack KB. TestEmitProbes runs --emit
// on the programs of testdata/emit-probes, written against the port of
// generics and functions, at deepStack KB. Each skips a case the
// port declines as emit-single does, failing one whose every emit stage
// is required, and fails when it has such cases and none of them was
// compared (see tallyVerdict), so none passes having compared what it
// could. Their programs are valid ones, which every one needs E1 or a
// later stage to lower: with E1 required, those of the core's features
// alone, the nests of blocks, loops and operators and the chains of
// else ifs, statements, && and locals, are compared, and the rest skip
// until their stages are; the depth of E0's and H's work, the header's
// check before lowering, is guarded by TestHeaderDeep too.
//
// # Fuzzing
//
// FuzzEmitDiff generates a program from each input with
// internal/progen, which reads its choices from the bytes, and runs
// both compilers with --emit on it, compared as emit-single compares
// its cases, after putting it through this module's front end and C
// backend in process for the fuzz engine's coverage. It is seeded with
// progen's own fuzz seeds, and run as a test checks them alone.
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
//	make port-fuzz PORT_FUZZ=FuzzEmitDiff FUZZTIME=60s
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
// a tenth of a second, or twice its memory, and logs both. TestCheckPerf
// is the same for --check --max-errors 0 on a program of three thousand
// functions whose bodies Go accepts and the ownership checks follow,
// moves, own parameters, replace, view results, ranges, regions and
// defers, with one error in main, so that --check stops before the
// backend; it first checks that both compilers give the same output.
// TestGenericPerf is the same for a program of two thousand functions
// each making forty calls of generic functions, eight hundred instances
// in all, and passing as many closures to a scope parameter, with one
// error at the end of main, so that both compilers run only the type
// checker, the instances and all; it first checks that the port's
// --port-types of the program without the error, which Go accepts, is
// Go's. TestClosurePerf is the same for functions of eight thousand
// locals and closures written under the facts on them, each shape one
// that is quadratic when a closure's check copies the facts in force,
// and TestBindPerf for a hundred thousand locals bound to a call's
// value, nearly all unused, each a warning.
// TestArithPerf does the same for the arithmetic and the relations: on
// three thousand functions whose bodies Go accepts, each proving
// overflows, indexes, a slice and a conversion, and on one body holding
// fifteen hundred values each related to another by an early exit, all
// in force while each is asked about again, on three thousand early exits
// each putting the same relation in force again, and on a switch of eight
// thousand cases. TestLoopPerf does the same for the loops and the
// contracts: on fifteen hundred functions whose bodies Go accepts, each
// probed for the bounds its loops keep, a counting loop, nested loops
// and a range among them, with a contract proven at each return, a call
// of a function with one, an append whose length an index reads and
// unreachable places; on two thousand calls of a function with a
// contract, each followed by a division its fact proves; on two
// thousand early exits each followed by an unreachable the facts prove
// and an append followed by an index of its list; and on a hundred and
// twenty loops nested in each other, each writing a variable declared
// before the outermost. TestChainPerf does the same for chains of four
// thousand && and || operands, each putting the facts of those before
// it in force for itself: in an if's condition, values ruled out and
// joined by && or || equalities, and in an ensures clause, values ruled
// out and lower bounds, given to a caller. TestDivisionChainPerf does
// the same, and runs --emit as TestEmitPerf does, for a block of a
// hundred thousand checked divisions, each binding two locals and
// closing an if's block among all of them. TestDeepFactsMemory runs
// --check and --emit of ifs nested as deeply as the parser allows, each
// on a write, and of switches nested as deeply, each falling through
// into the default holding the next, failing as TestResPerf does. TestCheckSliceChains runs a spine of slices with no high
// bound, under len and indexed, and chains of locals each slicing the one before, from a
// parameter and from a literal, five thousand links long, under --check,
// the port's stack held to deepStack KB and to a quarter of it.
// TestProbesTypes runs the probes with --port-types too. TestEmitPerf
// runs --emit, compared as emit-single compares its cases, and then
// measures it as TestResPerf does, on the port's driver, on
// TestCheckPerf's functions and TestGenericPerf's program, each with a
// main Go accepts, and on a main printing twenty thousand strings.
//
// # The backend's parts
//
// What the backend will need that no stage shows until it is ported is
// checked with flags of the port's own (see handover_test.go). TestBind
// compares --port-bind, what the type table's bindings give a checked
// program's types under each of its first sixteen instances' bindings,
// Go's Bind, BindMore, Unbind, Binding, Bound, HasTypeParam, NameWith
// with the qualifiers of generic.go and header.go, and every other
// query of the Table, each followed by the table's count, so the IDs of
// the types they add, in order, which C names carry, are compared, with
// the same from Go's Table, on every input of the corpus Go accepts with
// instances of generic functions; TestBindDeep does the same on generic
// functions making values of types nested in lists, maps and function
// types nearly as deep as the parser allows, the port's stack held to
// deepStack KB. TestHexFloats compares --port-floats, the C literal the
// backend writes for a float constant, a hexadecimal float, with the Go
// backend's floatLiteral, on float64s and float32s of every kind:
// zeros, infinities, NaNs, subnormals, the extremes, powers of two over
// every exponent, their neighbours and random bit patterns.
//
// The header, H's, is checked the same way (see header_test.go).
// TestHeaderRender compares --port-header, tack.h as the port's backend
// renders it for a checked program, with nothing lowered, with Go's
// backend's Header run in process, on every input of emit-header and
// the programs of testdata/header, Go's --emit-header writing tack.h
// only after lowering the whole program. TestReservedTables compares
// --port-reserved, the names tack.h may not give and the any type ids,
// with a program made of Go's own declarations of reservedNames,
// anyIDs and viewAnyID, copied from codegen/c by go/parser and run with
// go run from an overlay. TestHeaderDeep runs externs of arrays nested
// nearly as deeply as the parser allows, chains of structs held by
// value and many externs through --emit, whose check of tack.h comes
// before any lowering, and --port-header, the port's stack held to
// deepStack KB and pairStack KB, and TestHeaderDeepPortOnly the same
// with chains of chainLength links on the port alone.
package porttest

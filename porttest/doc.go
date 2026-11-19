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
// reference: ./tack of this module, its Go code the frozen compiler's,
// built into a temporary directory with go build's -overlay replacing
// the files its package builtin embeds, builtin.tck, the runtime header
// and the runtime's C files, by the port's own in its runtime/, which
// the port owns (per the owner's ruling, 2026-11-18). A program either
// compiler builds links the runtime, and the build stages compare
// binaries, cache keys and clang runs, so Go must compile the port's
// runtime for them to agree when the port is right. $TACK_GO, when set
// and not empty, names a reference already built, as the Makefile's
// port-go builds one, through TestMain run with PORTTEST_BUILD_GO; it
// must not name the frozen compiler as it is, whose runtime the port's
// no longer equals. That one, ./tack built without the overlay, is
// built only by the tests of where the port's runtime differs from it
// on purpose (see File-size limit), and $TACK_GO never stands for it.
// The port is the binary
// $TACK_PORT_BIN names, or, when that is unset or empty, the program in
// the driver directory of $TACK_PORT, the port's source tree, which
// defaults to ../tack next to this module's root. A relative path in any
// of the three is taken from this module's root, not from where go test
// or make was run, which the tests cannot know. The port is built with
// the Go compiler, from the source tree's root, as
//
//	tack --out <temp>/tack-port ./driver
//
// with TACK_HOME set to the port's source tree, so std is the port's own
// src/std, which the port owns (per the owner's ruling, 2026-11-18), and
// TACK_CACHE set to the Go compiler's temporary cache; the build of the
// port with its allocations counted and the build of its tools/embed
// read the port's std the same way. Built by the reference, the port
// carries the port's runtime, so it is the binary the port builds of
// itself. Every comparison runs both compilers with the same
// TACK_HOME, the port's source tree, so both read the port's std; std's
// packages in the corpus are read from there too, named src/std/... as
// from its root, and every copy of std a stage makes, a read-only
// TACK_HOME or emit-header's copy, is of the port's. This module's
// src/std and builtin/ are frozen, and TestPortFiles checks the port's
// against them (see Port files). A source tree with no src/std fails
// TestPort, even with TACK_PORT_BIN set. The in-process checks, which
// load programs with this module's loader and typeck, the classification
// of the archives' cases and the backend's baseline among them, read the
// port's std (see inProcessStd) but this module's builtin package, and
// so its builtin.tck. The harness's own tests that classify without
// TACK_PORT_TEST, TestClassifyCounts and the baseline's feature tests
// among them, have no port tree to read and read this module's frozen
// std: they check the classifier, which reads only std's declarations. A
// source tree with no driver directory, the default as much as one
// TACK_PORT names, or a driver that does not build, fails TestPort: once
// TACK_PORT_TEST asks for the check, nothing missing skips it. A checkout
// without the port still passes go test ./..., where TACK_PORT_TEST is
// unset and TestPort skips. Setting TACK_PORT_BIN to the Go
// binary runs the Go compiler against itself, which must pass every
// stage but corpus-runner and bootstrap, which it skips, since Go has
// none of the port's --port-case flags the corpus runner needs and the
// port it builds is no copy of Go: it checks the harness, not the port.
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
// fails. A build stage's case may also decline late: once clang, which
// writes to the driver's own outputs as it runs, has started, a decline
// comes after the warnings and what clang wrote. Such a run exits 3
// with that line last on standard error, after other output on either
// stream, and a build's run with anything on standard output is such a
// run, whatever its standard error holds; it is a decline only when what came before is what the Go compiler wrote
// before its own final error: its standard output the Go compiler's,
// and its standard error the Go compiler's with everything from its
// last line that starts tack: to its end removed, then the not
// implemented line: the Go compiler's final error may run over several
// lines, as two errors joined do, the second not starting tack: . The Go
// compiler runs to tell, and a late decline that writes anything else,
// or one where Go's standard error has no line starting tack: , fails
// the case, required or not. A stage enters required.txt once the port implements it, so it
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
// pass at the default. $PORTTEST_STACK_SCALE, a whole number from 1 up,
// multiplies that stack, and every other the harness holds the port to,
// for a port built with the sanitizers, whose frames are larger (see
// Sanitizers).
//
// runCompiler, through which every stage runs a compiler, starts each
// run in a process group of its own (see runInGroup in group_test.go),
// so a timeout kills the whole group, the toolchain and anything else
// the compiler started, and a process the run started that is still
// running two seconds after it ends fails the case, its standard error
// shown, and is killed; a run that also failed is reported as that
// first. Each run gets SIGKILL should the harness die first
// (Pdeathsig), and TestMain makes SIGINT, SIGTERM and SIGHUP, which a
// terminal's Ctrl-C and timeout send to their own process group, not
// these, kill every such group before the harness exits with status 1.
// What can still outlive the harness: a descendant of a run, which
// Pdeathsig does not reach, when the harness ends with no signal it can
// handle, as on SIGKILL or when go test's -timeout panics it. The
// processes the harness starts with exec of its own, outside
// runCompiler, run in the harness's group: TestMain's builds of the two
// compilers, the coverage and harvest runs of the backend stages'
// classification, and those of the allocation-failure, performance,
// embed and header tests.
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
//   - lex-probes runs --lex on each program of testdata/lex-probes, the
//     files whose dump a known divergence records, which no file of the
//     corpus can hold.
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
//     package of std and for examples/extern-module/geo:check, a library
//     whose C uses its own type, on each input of emit-single, emit-packages and
//     std whose package holds a C file or an extern func, a directory
//     outside a module, which the loader rejects, left out: each
//     compiler in a fresh copy of the tree at the same path, the
//     input's directory and std, with TACK_HOME the copy's, and every
//     tack.h of a package directory removed first, so that the tack.h
//     files there afterwards are what the run wrote. It compares the
//     files and directories each copy holds afterwards, with their
//     modes and the files' bytes, too, and fails
//     a case Go passes without writing a tack.h.
//   - build-examples builds each input of emit-single and emit-packages
//     into a binary, with --out, build-invalid-c each case of
//     examples/invalid-c.txtar, whose C clang or the driver rejects, and
//     build-probes each of the probes of testdata/build-probes;
//     each compares what the driver writes and leaves, and, where both
//     compilers built one, the binaries and what they do when run (see
//     Build stages).
//   - build-warm builds each input of build-examples cold and warm, each
//     compiler on its own cache and on the other's, and build-cache
//     each of the probes of the build cache, a damaged cache, a build
//     that must miss and two builds at once; each compares the driver's
//     output, the binaries, what was compiled and the caches (see Build
//     cache stages).
//   - test-examples runs tack test on each package of examples/tack-test,
//     examples/test-files, std/hex and std/unicode, with each set of
//     flags, and on
//     each of testProbes, comparing what the driver writes, the files it
//     leaves and the generated C of each test binary (see Test stage).
//   - corpus-runner builds the port's tools/corpus with the port and runs
//     it over the port's corpus with the port, which must pass; it
//     compares nothing with Go (see Corpus runner).
//   - bootstrap builds stage 2, the port's driver built by the port, and
//     stage 3, the driver built by stage 2, and requires the port, stage
//     1, and both to be the same bytes (see Bootstrap).
//   - unicode-tables builds the port's tools/unicodegen with the port and
//     requires what it writes from the port's copy of the Unicode
//     Character Database to be std/unicode's committed tables; it
//     compares nothing with Go (see Unicode tables).
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
// entry whose C is Go's records its warnings alone. An entry of
// emit-header may end in one more section, file: the path of a file its
// case's copy holds afterwards, and that file's mode and bytes as the
// port writes them, which runHeaderCase compares the port's copy with in
// place of Go's file there, failing the case when Go's is the recorded
// one; such an entry may name every stream in same. An entry of the
// suite header-render records, as its standard output, the tack.h
// TestHeaderRender compares the port's --port-header with in place of
// Go's backend's, which must differ from it. Wherever the case
// runs, compareResults compares the port with that output in place of
// Go's, as the stage compares the two, and fails the case when Go gives
// it, the entry having gone stale, as much as when the port does not; a
// case the list holds that the port declines fails, required or not, so
// an entry is never a skip.
//
// An entry applies to its suite's cases alone. TestPort marks each case
// of a stage as that stage's, TestProbesPairStack the probes as the
// probes stage's, TestEmitProbes its programs as emit-probes', and
// TestHeaderRender its inputs as header-render's. An
// input of no suite, as the deep, perf and fuzz tests make, never takes
// an entry, and fails when an entry of the stage it runs as names it, so
// that no entry applies silently to an input that only shares a case's
// name. TestPort fails a stage for each entry of it whose case the stage
// does not have, and TestDivergencesFile, in every go test ./porttest,
// reads the list, rejecting a malformed one, and fails an entry of the
// probes or the emit probes naming no program of theirs. The files an
// emit-header case leaves are compared with Go's, but for the one file
// an entry of the case records. TestParseDivergences, TestParseDivergencesRejects,
// TestDivergenceVerdict, TestMatchDivergence, TestMissingDivergences and
// TestDivergedDecline check the mechanism itself.
//
// # File-size limit
//
// The port's runtime ignores SIGXFSZ, as it does SIGPIPE, so a write
// past the process's file-size limit, RLIMIT_FSIZE, fails with EFBIG
// rather than kill the process, and std/os's run gives every child
// SIGXFSZ's default action back, an ignored signal staying ignored
// across exec (per the owner's ruling, 2026-11-15). The frozen Go
// compiler's runtime does not ignore it: a program it builds dies of
// SIGXFSZ there. That is a divergence of the runtime, not of a case of
// a stage, so divergencesFile, whose entries record the two compilers'
// runs of a case, does not hold it; TestFileSizeLimit records it,
// running every program under sh's ulimit -f 1, which counts 512-byte
// blocks in the sh it runs, bash in its POSIX mode, and ulimit -c 0. It
// builds a program writing 64 KiB through std/os's write_file with the
// port and the reference, and checks it prints "File too large" and
// exits 0, and with the frozen Go compiler, and checks it dies of
// SIGXFSZ; builds with the port one that starts sh -c 'exec yes > out'
// through std/os's run, and checks the child dies of SIGXFSZ; and runs
// both compilers' --emit with standard output a file under the limit.
// There the two leave the same bytes and neither is killed, Go's
// runtime ignoring SIGXFSZ too, and both exit 1 with "tack: failed to
// write output: write /dev/stdout: file too large": the port writes its
// output through std/os's write_stdout and its warnings through
// write_stderr, port-only std that returns a failed write's error (per
// the owner's ruling, 2026-11-18), where std/fmt's write reports none.
//
// TestOutputWriteFailure does the same for every other output Go's
// driver reports a failed write of, each run's wait status, standard
// streams and files compared byte for byte: --lex, --ast, --res and
// --check --emit to standard output, --backends appended to a file at
// the limit, and the warnings of --emit, --res, --check, --check --emit,
// --emit-header and --check --emit-header to standard error, where a
// failed write leaves no tack.h behind --emit-header, as in Go; and
// tack.h itself, cut short by the limit with the warnings on a pipe,
// cannot write and write tack.h: file too large, the port wording it
// from std/os's write_file_result, which names the call that failed;
// TestEmitHeaderReadOnly does the same for an open refused in a
// read-only directory. TestNonblockingStdout checks that a non-blocking
// standard output, full while its reader waits, gets every byte, in
// order, from write_stdout and from --emit, as from Go, which waits for
// it; TestWriteFileResult checks write_file_result's op and error for
// a write that succeeds, an open and a write that fail.
// TestOutputBrokenPipe checks that a broken pipe on either stream ends
// both: Go by SIGPIPE, the port with status 141, as the runtime ends a
// process whose standard output or standard error has no reader.
// TestStdWrite checks write_stdout and write_stderr themselves, built
// by the port and by the reference: "File too large" past the limit,
// their bytes in order among print's and std/fmt's, an empty write no
// error, and status 141 on a broken pipe. A build's warnings are no case
// of these: the port writes them just before its first clang starts,
// where Go writes them before it builds, and under the limit the port
// declines a build, which cannot write its temporary files.
// TestBuildWarningsUnwritable checks, with standard error a file opened
// for reading alone, that a build whose warnings cannot be written ends
// as Go's does: exit 1, no clang started, --out left as it was, missing
// or holding its old bytes and mode, and the build cache as Go leaves
// it; TestTestWarningsUnwritable checks that tack test so ends as Go's
// does, exit 1 with no test run.
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
// blocks in docs/spec are not part of the corpus yet, and
// examples/invalid-c.txtar is the build stages' alone (see Build stages).
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
// # Build stages
//
// The build stages check the port's driver, which M8 ports stage by
// stage, building a program: tack --out <path> <target>, clang and the
// link. build-examples runs on each input of emit-single and
// emit-packages, whatever the driver makes of it, a program built or
// the driver's errors; build-invalid-c on each case of
// examples/invalid-c.txtar, written once a run to a directory of its
// own, apart from the corpus's archives, and run from there with its
// target, as TestInvalidC builds it; build-probes on each of
// buildProbes, a main.tck of a directory of testdata/build-probes built
// from there with what the corpus's builds never meet: an --out path Go's
// checkOutPath refuses, a directory, one below a missing directory or a
// file, and the input itself; a PATH with no clang, an empty one, and
// one whose clang is testdata/build-probes/clang-fails/clang, which
// writes a line to each stream and exits 1; an --out path in the case's
// read-only TACK_HOME, where clang's link fails; an empty and a relative
// element, neither holding a clang, before the PATH the compilers
// inherit, and a relative element holding one, which Go's exec.LookPath
// refuses; a program that warns; each -O flag but the default; and the
// program that warns built with a fake clang first on PATH, before the
// real one: one SIGKILL, SIGTERM or signal 34 kills, one that cannot be
// started, its interpreter missing or its #! line, one no one may run,
// which LookPath skips for the real clang, and one compiled from C,
// once a run, that writes the argv[0] it is given; and one that writes
// to each stream in turn, built with standard output and standard error
// one pipe, read as standard output, so the order of the warning,
// clang's lines and the error is compared; a program using std, its C
// and objects of the runtime linked, at the default level and at -O2,
// and with a PATH whose clang fails, where compiling std's C fails; and a
// program that links objects of the runtime but has no C file, with no
// clang on PATH, an empty PATH, a clang that fails and one SIGKILL
// kills, so that clang --version fails each way, and with a build cache
// that cannot be made: below a file, in the read-only TACK_HOME, and
// none found, $XDG_CACHE_HOME relative or neither it nor $HOME set. No
// fake dumps core, which the machine's limits and core pattern decide.
// A probe's --out path, when it has one, is its own, relative to its
// directory, in place of the case's, its PATH, when it has one,
// replaces the one the compilers inherit, and its variables, when it
// has any, are set after every other. build-probes is required, so
// it holds no build the port declines: TestBuildDeclines runs those,
// the port alone, the warning program built where Go words what the
// port cannot, a TMPDIR that is missing or a file, one so long that the
// runtime's directories cannot be made below it, a current directory
// that is gone, and, building a
// program that links objects of the runtime, a clang that writes no
// object, which the port cannot read, where Go words debug/elf's error,
// a build cache whose objects or keys directory cannot be written, or one
// whose every key, or every object, is a directory, where Go's error
// names a temporary file of a random name, one whose every key names an
// ELF32, a big-endian or a Mach-O object, which Go reads and the port's
// ELF reader does not take, and
// checks that each declines as a case must to skip: exit status 3, the
// not implemented line with the case's reason, nothing else written for
// a decline before clang runs, and for those after it, the late
// declines, the Go compiler's build of the case run too, in directories
// laid out alike, and what the port wrote before the line what Go wrote
// before its last error (see Protocol); nothing left in TMPDIR but what
// that clang left there, and
// nothing at the --out path, but for what was there before the build,
// which is left as it was. Its match cases, once declines, are builds
// whose temporary directory cannot be removed whole, which std/os's
// remove_all removes or fails at as Go's RemoveAll does, so the port
// writes what Go writes: a clang that leaves in it what cannot be
// removed, one directory or several at several depths, a file and a
// dangling symbolic link at --out before a build whose clang does that
// and writes no binary, a C file clang rejects with a clang that leaves
// that, where Go's error is two joined over two lines, and a path past
// PATH_MAX, which both remove, after diagnostics; each runs both
// compilers and compares their output, what they leave in TMPDIR and
// at --out. Each compiler builds a case in a
// fresh copy of its tree, the directory of its module or of its file,
// each file copied with mode 0644, or 0755 when it may be run,
// at the same path below the case's temporary directory, the port's
// first; the Go compiler through a wrapper that holds its stack to the
// port's, deepStack KB, so that the clang each runs has the same limit;
// with TACK_HOME a copy of the repository's std made once for the case
// and read-only, every file and directory, so that a build that writes
// there fails; with a fresh TACK_CACHE and a fresh TMPDIR, each at the
// same path for both, made anew for each; and with the same --out path,
// in a directory of its own made anew, from which each binary is moved
// aside before the other compiler runs. The stage compares the driver's
// standard output, standard error and exit status byte for byte, but
// for the letters and digits after tk- of each path component
// tk-<letters and digits> right below the build's TMPDIR, the directory
// the Go driver makes with os.MkdirTemp, tk- and decimal digits, and the
// port with std/os's make_temp_dir, tk- and six letters and digits, and
// writes the program's C to, which clang's notes name and which differ
// run to run; what each copy holds afterwards, every file and
// directory by its path and mode and each file's bytes, as emit-header
// does; and the same of the --out path's directory, but for the
// binary's bytes, which build-identical compares. A build that leaves
// anything in its TMPDIR fails, and so do one that fails and leaves its
// --out path, one that exits 0 and writes nothing there, and a case one
// compiler builds a binary for and the other does not. The built
// binaries run in process groups of their own, as runCompiler's runs
// do.
//
// Four declines have no case, for want of a clean way to make them:
// "with a clang that cannot be run", which std/os's run_inherit gives
// when /dev/null cannot be opened, which needs a file system without
// it, or its wait fails, which the runtime's reset of SIGCHLD leaves no
// way to make, and "with a clang --version that cannot be run", which
// std/os's run gives for those and for a pipe that cannot be made or a
// read or poll that fails, which nothing a test controls makes either;
// "with a clang that cannot be started" for an errno the port has no Go
// words for, which on Linux is EHWPOISON alone, as Go's words for it
// differ between amd64 and arm64, an errno no exec can be made to give
// on demand; and "with a build cache that cannot be made" for a mkdir
// that fails with an errno the port has no Go words for, the same one.
// A temporary directory that cannot be removed after the definitions
// check gave diagnostics, for EACCES or EPERM, is no decline: Go drops
// the removal's error, which the port reports after them, a known
// divergence TestBuildRemovalReported checks, outside the stages, since
// its clang leaves what the harness must make removable first, against
// the error os.RemoveAll gives for the same tree; an errno the port has
// no Go words for there, EHWPOISON alone, is written in std/os's words,
// decided 2026-11-18, since Go writes no tack: line there that a late
// decline could stand for; after a build that succeeded or failed with
// an error of one line, where Go's output ends in its tack: line, such
// an errno is declined, nothing of that line written first. A build
// tack test runs declines naming test, not --out.
//
// TestObjectSymbols checks the port's ELF reader, which finds what
// the objects of a build define and leave undefined, with no tool but
// clang: its --port-symbols, on objects clang compiles from the
// runtime's C files and a file holding every kind of symbol, at every
// -O level, with -g, compressed debugging sections and -fcommon, and an
// object of seventy thousand sections, read through extended section
// numbering, must read what Go's readSymbols reads, and on damaged
// copies of them, truncated or with bytes changed, extended numbering's
// fields among them, what Go reads or that it cannot read the object,
// wherever Go fails.
//
// Where both compilers built a binary, of a case of build-examples or
// build-probes, the two are compared by their SHA-256: build-identical,
// a name required.txt may hold, fails a case whose binaries differ once
// it is required, and logs the verdict until then, and fails a case of
// either the port declines as a required stage does. A case of
// build-probes is held to build-identical while build-probes is
// required, whether required.txt names build-identical or not, so that
// a port whose -O flags do not reach clang fails its -O probes;
// required.txt cannot name build-identical itself while build-examples
// has cases the port declines, each of which it would fail; it names it
// now, so every case of build-examples and build-probes is held to it.
// build-invalid-c builds no binary: every case of it fails. The loops
// program of the -O probes is too small for every level to tell, its
// -O2 binary its -O3 one, but std-opt-2's program, which uses std, has
// two that differ, so a port that lost -O2, or gave -O3 for it, fails
// it. The Go compiler, run as both compilers, builds the same binary
// twice for every example it builds, with a cache of its own each time,
// so a port that runs the same clang and link commands builds it too;
// goIrreproducible lists, with why, any case where Go does not, which is
// never compared, none so far. The two binaries' modes must be the
// same. Each binary is then run, the
// port's first, with defaultRun, no arguments and the environment of go
// test, and with the runs buildRuns gives its case, each copied from
// the Go compiler's test of the example: examples/os with its
// arguments, its variables and at most 64 open files, through sh as
// that test runs it, writing examples/os/output, examples/endless-loops
// with the one case whose loop ends, and examples/fmt-interleave with
// both streams one pipe, whose bytes must also be
// examples/fmt-interleave/combined. Every run's standard input is
// /dev/null, its working directory and its TMPDIR fresh directories,
// the TMPDIR empty when it ends, and it is killed, failing the case,
// past a third of compilerTimeout. The stage compares the exit status,
// or the signal that ended the run, standard output and standard error,
// byte for byte, and what each run leaves in its working directory, as
// the copies are compared. examples/broken-pipe's runs, which break a
// pipe and read a descriptor of their own, are not covered: with no
// arguments it only says what it takes. Each build is at the default
// optimization level, but those of the build probes that give an -O
// flag.
//
// Every case's cache is fresh, so no case reads what another wrote; a
// build from a warm cache, and what a build writes to its cache, are
// the build cache stages' to compare.
//
// # Build cache stages
//
// build-warm and build-cache check the port's build cache, which holds
// the objects compiled from the runtime's C files, against Go's, which
// it must read and write exactly as Go does, so that the two compilers
// share one cache. Each build of either stage runs as a build stage's
// does, in a fresh copy of its tree, with a read-only TACK_HOME and a
// fresh TMPDIR, which must be empty afterwards, at the same paths for
// both compilers, but with a cache that is not made anew: each cache of
// a case lives beside it and is moved, by a rename, which keeps every
// mode and byte, to the one TACK_CACHE path every build of the case
// runs with, and back. Every build runs with a counting clang first on
// PATH, a script that runs the real clang by its absolute path and logs
// each runtime object it compiles, std-<name>.o, so a build that
// compiles what it should have found in the cache is seen; given
// --version with PORTTEST_CLANG_VERSION set, it writes that line after
// clang's output, a toolchain of another identity. A step compares the
// driver's output and exit status as compareResults does, the binaries
// by their SHA-256 and their modes, the objects each compiled, and the
// caches, every file and directory by its path, mode and bytes.
//
// build-warm runs on each input of build-examples: Go builds it on an
// empty cache, A, and the port on another, B, after which A and B must
// hold the same files; then Go builds it on A again, a warm build, and
// the port on A, Go on B and the port on B, each of which must give
// what Go's warm build gave, compile nothing and leave its cache as it
// was. A program that needs no object of the runtime leaves both
// caches empty. Its cases are build-examples', built as that stage
// builds them, so the port's runs are compared through build-examples'
// entries of the divergence list, and one of Go's with another of Go's
// byte for byte.
//
// build-cache runs on each of cacheProbes, each of the program lists,
// which links objects of the runtime and has no C file of its own: a
// cache Go filled, twice, each copy then damaged alike, every key cut
// short, without its newline, in upper case, of bytes that are not hex,
// naming a path, empty or unreadable, every object gone, with a byte
// changed or unreadable, every other key gone, so a build mixes hits
// and misses, or every key naming bytes that hash right but are no
// object, which Go's cachedStdObject compiles again, where its
// readSymbols fails; Go builds on one copy and the port on the other,
// which must compile what Go's does and leave the cache Go's leaves,
// and each builds again, which must compile nothing, the cache
// repaired. miss-opt and miss-toolchain build on an empty cache at the
// default level with clang, then at -O2, or with a clang whose
// --version differs, which must miss, compiling every object again and
// adding as many keys, then so again, which must not; Go and the port
// each on a cache of its own, compared at each step. concurrent runs
// two of the port's builds at once on one empty cache, each with a
// TMPDIR, copy and --out path of its own, each of which must give what
// Go's build on an empty cache gives, the cache afterwards holding what
// Go's held, no temporary file of either among it.
//
// A key or an object that is a directory is no case of build-cache:
// Go's put cannot rename a file onto one, and fails naming its
// temporary file, of a random name, so the port declines, late, after
// clang ran; TestBuildDeclines runs both, checking too that the port
// leaves the cache Go leaves. So is a key naming an object Go's
// readSymbols reads and the port's ELF reader does not take, ELF32,
// big-endian or Mach-O, which Go links and the port declines; bytes
// Go is sure to refuse, as those of key-not-elf, are compiled again by
// both. So do the builds on a cache whose keys or
// objects directory cannot be written, which Go's error words naming a
// temporary file too: every error os.CreateTemp, a write, a close or a
// rename gives names it, but for CreateTemp finding 10000 names taken,
// which no build meets. Not covered: the port's retry on a temporary
// name that is taken, which no build can be made to meet without a
// hook in the driver for the test alone; the concurrent case shows only
// that two builds at once succeed and leave no temporary file.
//
// # Test stage
//
// test-examples checks the port's tack test. Its cases are each package
// testCorpusDirs names, run from its own directory with . as its target
// and each of testFlagSets: none, -v, a -run pattern, -v with another,
// and -O2; examples/tack-test/pipe with -v and standard output a pipe
// whose reader is closed, as the Go compiler's test runs it, so its
// test's writes end the binary with status 141; each of testTargets, a
// package named otherwise than ., by its import path from its module's
// root, by a relative path from above it, one that names nothing, and
// std/hex from std/testing as ../hex; and each of testProbes, a module
// named m written once a run below the run's temporary directory and run
// from its root: every command-line mistake Go's flag package and
// parseTestArgs word, the help by each of its names, every way a flag
// can be written, targets that are empty, a file, missing, in another
// module, or a directory with no Tack file, test files alone and in
// name order with ones that are no test files, -run matching no test,
// every way a test can be declared wrongly, a syntax and a lex error, a
// clause that does not match, each name the generated main declares,
// an import cycle through the package tested, a warning, a test binary
// SIGTERM, SIGKILL and SIGSEGV kill and one that exits 7, through C, 2000
// tests, names and a pattern of odd bytes, the PWD a test sees, a
// temporary directory a test leaves what may not be read in, after
// tests that fail and pass, a test binary a hook on clang's link makes
// one exec cannot run, of no format it knows or not executable, or
// fills with a tree deeper than PATH_MAX, or fails beside a temporary
// directory that cannot be removed, types
// whose tack.h names the header ruling changes, a relative TMPDIR, every
// mistake in the format of T.errorf and T.fatalf, called as methods and
// through testing.T, and formats that are right, a main package with no
// main function, and a package whose C uses a type of the package, as
// package main and as another, which the Go compiler names wrongly in
// tack.h, a known divergence.
//
// Each compiler runs the case as a build stage's case builds, in a fresh
// copy of the case's whole module at the same path, the port first, with
// a read-only TACK_HOME, a fresh TACK_CACHE and TMPDIR at the same paths,
// the Go compiler through goStackWrapper, and a case in std, which the
// loader finds std's only below TACK_HOME, in that read-only copy of
// std; and each with a clang first on PATH that copies the generated C
// it links, program.c, aside (see savingClangDir). The stage compares
// standard output, standard error and the exit status byte for byte, but
// for the random part of a build's temporary directory, the files the
// copy holds afterwards, and the two generated C files where both
// compilers linked one, one compiler linking a binary and the other
// not failing a case the divergence list does not hold, and the stage
// failing when no case compared any; a run that leaves anything in
// TMPDIR fails, a decline's too, but for the probes whose test leaves
// what may not be read, which must leave it, and whose TMPDIR is made
// removable after each run. Paths below the case's TMPDIR are written
// $TMPDIR, the random part of a build's or tack test's directory there
// hidden too, so an entry can record them. A decline is held to the
// Protocol, as a build's is. Where tack test still declines: a
// failure the loader cannot word, before anything is built; the build's
// own declines (see Build stages); a temporary directory that cannot be
// made; a test binary that cannot be started for an errno the port has
// no Go words for, in place of Go's tack: cannot run line; and a
// temporary directory that cannot be removed for such an errno, EHWPOISON
// alone on Linux, after a run that passed or an error of one line, in
// place of the tack: line Go ends in, which the port holds back until the
// removal is done, so nothing of it is written first. After tests that
// failed or diagnostics, where Go writes nothing of the removal, such an
// errno is written in std/os's words, decided 2026-11-18, and nothing is
// declined. The temporary directory is removed with std/os's
// remove_all, as Go's RemoveAll removes it, relative to each directory,
// so a tree below it deeper than PATH_MAX, which a probe's hook on
// clang's link makes, is removed as Go removes it. A known limitation no
// probe covers: a tree deeper than the descriptors the port may open,
// about a thousand levels at Linux's usual soft limit, fails with EMFILE
// where Go, whose runtime raises that limit to the hard one, removes it.
//
// # Bootstrap
//
// The bootstrap stage, one case, checks that the port builds itself:
// stage 1, the port porttest builds with the Go compiler or the one
// TACK_PORT_BIN names, builds stage 2 from the port's ./driver, and
// stage 2 builds stage 3 the same way, each with -O3, the level the
// compiler is built at and the Go compiler's default, in the port's
// source tree with the port's std and a fresh cache of its own, so every
// object is compiled anew. Each build runs with its stack held to
// deepStack KB, through sh's ulimit given on its command line rather
// than a wrapper script, since stage 2 is written during the case (see
// the scripts written once a run, below), in a process group of its own,
// and fails the case when it does not end within bootstrapTimeouts times
// a compiler run's time limit. The case fails unless the three binaries
// are the same bytes, naming each neighboring pair that differs, the
// first byte offset where it does and both sizes, and the three SHA-256
// sums: stage 1 equals stage 2 because the C the port writes is Go's,
// and stage 2 equals stage 3 because stage 2 is then stage 1. A decline
// of a build fails once the stage is required, as any case's does. A
// port that is the Go compiler, byte for byte, skips the case, and make
// port-sanitize skips the stage, since its stage 1 is sanitized and the
// stage 2 it builds is not. TestBootstrapDiffs checks the comparison on
// small files and needs no port. The port's make bootstrap runs the same
// two builds with no Go, and make port-test-stage2 runs porttest with
// stage 2 as the port.
//
// # Corpus runner
//
// The port keeps a copy of this module's test corpus in its corpus/:
// examples/, with its archives and committed tack.h files, and golden/,
// test/testdata's goldens, with a tack.mod that makes corpus/ a module
// root only so a library check of the port skips the examples. Its
// tools/corpus, a program written in Tack, checks that copy with the
// port alone, as this module's test/ harness checks examples/ with Go
// (per the owner's ruling, 2026-11-19): each archive's cases, each
// golden and each committed tack.h, through the port's test-only
// --port-case and --port-test-case, which write a case's stage line and
// output as test/'s in-process harness renders them, and through
// --emit-header in a scratch copy and --out for invalid-c.txtar; the
// port's README says how to run it and how to update the corpus. The
// corpus-runner stage, one case, builds tools/corpus with the port in
// the port's source tree and runs it over corpus/ with the port's std and
// the port as the compiler, in a temporary directory of the case's own
// and a process group of its own, as every run of a compiler is, and
// fails, showing its FAIL lines, unless it passes, or when it does not
// end within corpusRunnerTimeouts times a compiler run's time limit; a
// decline of the build fails once the stage is required, as any case's
// does. A port that is the Go compiler, byte for byte, as TACK_PORT_BIN
// names it to check the harness, skips the case. The runner also checks
// each case of invalid.txtar and invalid-packages.txtar and each
// program with CRLF line endings, as TestCRLFMatchesLF does, and refuses
// a $TMPDIR below a directory holding a tack.mod. TestPortFiles keeps
// the copy equal to this module's (see
// Port files), so the corpus the port checks itself against is the one
// Go's harness checks this compiler against, but for the cases and files
// whose differences it lists. TestFailLines checks how a failed run's
// output is shown, and needs no port. TestCorpusRunnerChecks builds the
// runner with the port and checks what a passing run cannot show: the
// refusal of a $TMPDIR below a tack.mod, the temporary directory removed
// when a run stops on an error, an archive of an unknown kind failing, a
// stub compiler whose output changes with CRLF line endings caught, a
// compiler that is not there failing once, before any case, and the
// port's --port-std with an empty name a usage error.
//
// The scripts the tests run that are written once a run, the Go
// compiler's stack wrapper and the counting and saving clangs, are
// written in TestMain's setup, before any test runs, and the link hooks
// of test-examples' probes with its inputs, before any case of it runs:
// a script written while parallel cases fork can be held open for
// writing by a child between its fork and its exec, and running it then
// fails with ETXTBSY.
//
// # Unicode tables
//
// The port's std/unicode classifies runes by its tables,
// src/std/unicode/tables.tck, which the port's tools/unicodegen, a
// program written in Tack, writes from the port's copy of the Unicode
// Character Database, tools/unicodegen/ucd/<version>, as Go's unicode
// package defines its classes, at Unicode 18.0.0 (owner ruling
// 2026-11-19), where this module's frozen copy is Go's, at 17.0.0. The
// unicode-tables stage, one case, builds tools/unicodegen with the port
// in the port's source tree, runs it on the one directory below
// tools/unicodegen/ucd, which must hold nothing else, once each file of
// it has the SHA-256 its SHA256SUMS pins, every file but SHA256SUMS is
// pinned and its PropList.txt names the version the directory is named
// for, and fails unless it writes the committed tables byte for byte,
// the checks the port's make check-unicode-tables makes with no Go; then
// runs the tool on each
// case of tools/unicodegen/testdata, a small UCD, most of them missing or
// malformed, and fails unless its standard error and exit status are the
// case's want, and its tables the case's want.tck where it has one, as
// the port's make test-unicodegen checks; and runs std/unicode's tests,
// tack test ./src/std/unicode, with the port, which must pass, as the
// port's make test-std requires: test-examples runs them with both
// compilers and compares them, which agree when both fail. A decline of
// the build fails once the stage is required, as any case's does. TestPortFiles lists
// tables.tck and unicode.tck among the files the port's std may differ
// in, and unicode_test.tck, std/unicode's tests, which the frozen copy
// has not got, the first file of std only the port has; and the goldens
// whose C holds the tables among the corpus's (see Port files). The reference Go compiler classifies with Go's unicode
// package, at 17.0.0, where the port's lexer, type checker and driver
// classify and quote with std/unicode, at 18.0.0, wherever the port
// mirrors one of Go's: the owner's ruling of 2026-11-19 covers every
// place the compiler quotes or classifies a rune, diagnostic wording
// included. Each such place of the Go compiler, with the port's, is
// either a known divergence, a probe recording the port's output on
// U+0558, a letter 18.0.0 assigns, which Go writes as \u0558; or
// a place no such rune can reach, for the reason given:
//
//   - lexer/token.go's dump of a string, char or comment token, %q;
//     lexer/token.tck: unicode18-lex, a case of lex-probes.
//   - typeck/printf.go's printf verb, unicode.IsGraphic, IsSpace and
//     strconv.Quote; typeck/printf.tck: unicode18-printf-verb, a case of
//     probes.
//   - typeck/switch.go's duplicate string case, strconv.Quote;
//     typeck/switch.tck: unicode18-switch-case, of probes.
//   - typeck/extern.go's C name that is no C identifier, strconv.Quote;
//     typeck/extern.tck: unicode18-extern-name, of probes.
//   - modfile/modfile.go's show of a module name, strconv.Quote;
//     modfile/modfile.tck: unicode18-module-name, a directory probe.
//   - loader/names.go's show of a package name from a directory,
//     strconv.Quote; the port's loader, through modfile.tck's show:
//     unicode18-package-dir, a directory probe.
//   - tack/main.go's flag value Go's flag package cannot parse, %q;
//     driver/main.tck: unicode18-flag-value, a command line of
//     lex-probes.
//   - tack/main.go's arguments past the one file, strconv.Quote;
//     driver/main.tck: unicode18-two-files, of lex-probes.
//   - tack/main.go's unknown backend, %q; driver/main.tck:
//     unicode18-backend, of lex-probes.
//   - tack test's boolean flag value Go's flag package cannot parse, %q;
//     driver/test.tck: unicode18-bool-value, a probe of test-examples.
//   - tack test's arguments past the one target, strconv.Quote;
//     driver/test.tck: unicode18-two-targets, of test-examples.
//   - lexer/lex.go's invalid escape sequence, %q; lexer/lex.tck: no
//     divergence, since the sequence quoted is the backslash and at most
//     the one byte after it, or \x and the hex digits after it, never a
//     whole rune of two bytes or more, as "\՘" and "\x՘" show.
//   - lexer/lex.go's token that ends after EOF, %q: an invariant of the
//     lexer, which only a bug in it breaks; the port has none.
//   - typeck/maps.go's key of a fact of a known string, strconv.Quote;
//     typeck/cond.tck: a key that facts are compared by and never
//     written, the messages naming the key as the source spells it.
//   - codegen/c/gen.go's TACK_C_NAME(%q): a C name the checker has
//     accepted, ASCII alone, as cname.IsIdent requires.
//   - codegen/c/arena.go's and pool.go's %q, codegen/backend.go's: the
//     compiler's own constant ASCII strings.
//   - codegen/c/struct.go's isCName, unicode.IsLetter and IsDigit: C the
//     backend writes, whose names are ASCII, as identifiers are, and a
//     string literal's starts with a quote, which no name holds.
//   - the port's driver/build.tck, which quotes "clang", a constant.
//   - the port's lexer/space.tck and driver/link.tck, unicode.is_space:
//     White_Space and Go's Latin-1 set are the same at 17.0.0 and
//     18.0.0, so no rune's space class differs.
//
// The README's Known divergences lists the same. A fuzz input is no
// case of a suite, so every fuzz target skips an input holding a rune
// the two tables classify apart, as itself, spelt by \x escapes or in a
// token's value, the runes computed from this module's frozen tables and
// the port's (see skipUnicodeDrift); TestUnicodeDriftFilter checks the
// filter, TestFuzzTargetsFilterDrift that every fuzz target calls it,
// and FuzzLoadDiff's seed of a module named with U+0558, which would
// fail the comparison, that it is skipped. TestMalformed and
// TestProbesTypes skip such an input the same way.
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
// its parameters' and results' types, and a call of add, add_all or
// insert with its results' types, the id, span or handle and an error,
// which Go's checker records on it too, a fields loop's fields(T) with
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
// its cases, with --check, compared as check-clean compares its cases,
// and with --emit-header, each in a fresh copy of its directory,
// compared as emit-header compares its cases, the files left included,
// after putting it through this module's front end and C backend in
// process for the fuzz engine's coverage. No program progen writes
// reaches a known divergence, so none needs an entry. It is seeded with
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
//	make port-fuzz PORT_FUZZ=FuzzEmitDiff FUZZTIME=60s PORT_FUZZ_MINIMIZE=20x
//
// # Embedded files
//
// The Go compiler embeds the builtins' documentation, builtin.tck, and
// the C runtime, its header and its C files, with go:embed; the port
// holds them as string literals in its builtin/embedded.tck, which its
// tools/embed writes, as its make embed runs it, and make port-embed
// here. The port's originals are in its runtime/: builtin.tck,
// tack/tack.h and the C files, and a tack.mod that makes the directory a
// module root only so a library check of the port skips builtin.tck,
// which is no code. TestEmbedded reads embedded.tck with this module's
// lexer and parser, as the Go compiler reads it building the port, and
// checks that it holds each of the port's originals byte for byte, the C
// files in the byte order of their names, as the Go compiler lists them;
// and builds tools/embed and runs it on the originals, to check that the
// file is what it writes.
//
// # Port files
//
// The port owns std and the runtime, and this module's src/std and
// builtin/'s builtin.tck and runtime are frozen copies of them, as they
// were when the port took them over; the comparisons run both compilers
// with the port's, the reference Go compiler embedding the port's
// runtime (see Binaries). TestPortFiles checks that
// the port's src/std holds the same files as this module's, each with
// the same bytes and executable bit, and every file below its runtime/
// the same as builtin/builtin.tck for runtime/builtin.tck and as
// builtin/runtime/<p> for every other runtime/<p>, both ways, so a file
// only one side has fails too; a path listed in portFileDivergences,
// with its reason, must differ instead, and an entry that names a file
// the same on both sides, or none, fails, so the list cannot go stale.
// It lists the files the port's handling of SIGXFSZ changed (see
// File-size limit): runtime/process.c, runtime/tack/tack.h and std's
// copy of it, and src/std/os's os.c and os.tck; and std/unicode's
// tables.tck, unicode.tck and unicode_test.tck, the Unicode 18.0.0
// upgrade's, the last a file of std the frozen copy has not got (see
// Unicode tables). portOnlyFiles lists, each with its reason, the port's
// files with no frozen copy, runtime/tack.mod alone, each of which must
// be in the port. It also checks that the port's
// std copies, src/std/builtin/builtin.tck and
// src/std/include/tack/tack.h, are its runtime's, which its make
// std-copies writes. TestComparePortFiles checks the comparison itself
// on small trees, and needs no port.
//
// TestPortFiles checks the port's corpus/ the same way (see Corpus
// runner): every file of its examples/ the same as this module's at the
// same path, and of its golden/ as test/testdata's, both ways, and any
// other file below corpus/ a failure, but for the paths
// portCorpusDivergences lists, each with its reason, which must differ:
// the goldens and tack.h copies of the programs that import std/os,
// which the port's std adds to, each of which must also be what the
// reference Go compiler's --emit or --emit-header writes on the port's
// std, so that the drift is std's alone (see checkDrift), unless
// portCorpusUnchecked lists it with the reason it cannot be. Each txtar
// archive directly in examples/ is compared case by case instead, read
// with internal/invalid, its comment too: a case only one side has
// fails, and so does a case whose files or expected section differ
// unless portCaseDivergences lists it, each entry naming the divergence
// of testdata/divergences.txtar whose output the port's expected section
// holds. A listed case may differ only in its expected section, which
// must be this module's stage line and --- and then the record's
// standard error with its warnings left out, as the harness renders a
// case the backend rejects; an entry for a case that does not differ, or
// that neither side has, fails, so neither list can go stale.
// portCorpusOnly lists the port's corpus/tack.mod, which must be there.
// TestCompareCorpus and TestWithoutWarnings check that comparison on
// small corpora, and need no port.
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
// value, nearly all unused, each a warning. TestOwnArgPerf does the same
// for a call of two own parameters nested in its second argument as
// deeply as the parser allows, each level passing a local of its own as
// own, whose check that nothing else in a call reaches what it moves
// takes time cubic in the depth.
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
// bound, under len and indexed, a spine of slices with no bound at all
// of a string literal, under len, whose value a condition asks for,
// and chains of locals each slicing the one before, from a
// parameter and from a literal, five thousand links long, under --check,
// the port's stack held to deepStack KB and to a quarter of it.
// TestProbesTypes runs the probes with --port-types too. TestEmitPerf
// runs --emit, compared as emit-single compares its cases, and then
// measures it as TestResPerf does, on the port's driver, on
// TestCheckPerf's functions and TestGenericPerf's program, each with a
// main Go accepts, and on a main printing twenty thousand strings.
//
// Every run of runCompiler, the corpus's, the deep tests' and the
// probes', is measured when $PORTTEST_MEASURE_LOG names a file: it goes
// through the launcher, which then passes the compiler's standard
// output on and writes its wall time and peak memory for the harness,
// and is appended to the file as a line of JSON, with its test, its
// compiler, its directory and its arguments. TestBudget, run with
// $PORTTEST_BUDGET_LOG naming such a log, pairs each run of the port
// with Go's of the same test and command line and fails each pair over
// TestResPerf's bounds, logging the twenty worst ratios of time and of
// memory, and fails a log with no pair in it. It measures nothing again:
// the runs share the machine with the parallel cases, so a pair it fails
// is to be rerun alone, its test named with -run and -parallel 1 into a
// fresh log, and that log checked, before the failure is taken as the
// port's. make port-measure runs TestPort so and then TestBudget.
//
// # Sanitizers
//
// make port-sanitize builds the port with clang's address, leak and
// undefined behavior sanitizers, from the C the Go compiler writes for
// it, through a clang on PATH that adds them to every command, and runs
// every test but the performance ones against it, with
// $PORTTEST_STACK_SCALE at 4 and $PORTTEST_TIMEOUT_SCALE, which
// multiplies the minute each run of a compiler gets, at 8: a report of
// any of the sanitizers ends the port with exit status 86, which fails
// its case, the one status they share linked together. It sets $PORTTEST_MALFORMED, which asks
// for TestMalformed: that many programs whose --emit Go passes, spread over
// examples/, each cut short at each sixth of its length and mutated
// eight times, byte by byte, from a generator seeded with its name, each
// variant run with --check --max-errors 0 and with --emit through both
// compilers and compared as emit-single compares its cases, but for the
// --emit of a variant whose package a mutation made other than main,
// where the port stops at "main package not found" and Go does not, a
// known divergence: there the port's is compared with Go's --res, its
// loader's and resolver's diagnostics. TestMalformedWant checks both.
//
// TestAllocFail, which runs only with $PORTTEST_ALLOC_FAIL set, as make
// port-alloc-fail sets it, builds the port with the same sanitizers and
// a C file of its own linked in, which counts the runtime's calls of
// malloc and realloc and sets the runtime's hook,
// tack_c_allocator_fail_after_, from $TACK_FAIL_AFTER. It runs a sample
// of the corpus, a dozen cases of each stage but emit-header, the emit
// probes with --emit and --check, the port's driver, and builds of
// examples/conv, which uses std, and examples/extern, which has C files,
// and of examples/conv on a build cache Go's build of it warmed, so the
// runtime's objects come from the cache, once counting its allocations
// and then failing every one from the Nth on, for up to
// $PORTTEST_ALLOC_FAIL values of N: a run that allocates nothing fails,
// a build that does not exit 0 with every allocation granted fails, and
// each failing run must exit 1, ending in the driver's "tack: out of
// memory", with no sanitizer report. Every run has a fresh build cache
// of its own, but the warm build's, whose cache every run shares and
// none writes to, a hit never writing, which the sweep checks, the
// cache the same after it as before, so a build allocates as its count
// did, and half of a build's points fail an allocation of the build
// itself, past where --emit of the same target ends; the temporary
// directories a build refused memory leaves, which the driver documents,
// are in a TMPDIR of the case's. That checks the C-level allocations and
// the out-of-memory paths: exit frees every buffer c_allocator holds
// before the leak sanitizer looks, so it sees only the C memory the
// runtime does not track, and that Tack code frees what it owns the
// checker enforces statically. Nothing is compared with Go there, whose
// runtime ends a process out of memory with a fatal error of its own.
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
// instances of generic functions, and on bindKinds, whose instances bind
// type parameters to every kind a type argument can be, error and
// Allocator among them. InAny, IsStruct, HasZero and Fields are asked of
// what the type stands for, Bound's: Go's ask part of their question of
// the type itself, so a type parameter bound to int64 takes no any, one
// bound to error is no struct and has no fields, and one bound to
// Allocator has a zero value, a Go bug the port does not copy, which no
// program shows in what the backend writes; TestBindDeep does the same
// on generic functions making values of types nested in lists, maps and
// function types nearly as deep as the parser allows, the port's stack
// held to deepStack KB. TestHexFloats compares --port-floats, the C
// literal the backend writes for a float constant, a hexadecimal float,
// with the Go backend's floatLiteral, on float64s and float32s of every
// kind: zeros, infinities, NaNs, subnormals, the extremes, powers of two
// over every exponent, their neighbours and random bit patterns.
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

// Command rungen writes the port's run records that it knows how to make
// from this module's Go tests: corpus/runs/prototype.txtar, the records
// of phase 1 of moving the Go tests' runtime assertions into data. Each
// record names the Go tests it replaces. The programs, inputs, tables
// and expectations the Go tests assert are read from the tests' own
// source with go/parser and evaluated as constants (see the source type):
// string and integer literals, + of them, durations, named constants and
// variables, composite literals of tables and filepath.Join of literal
// parts, so a change in a test shows up in the next generated archive,
// and no value is copied by hand where the test holds it as a literal.
//
// A record pins every stream and the status, more than most Go tests
// assert, so rungen writes what the Go test asserts and leaves the rest
// to the port's runner: after rungen, run tools/corpus --update in the
// port (make update-corpus), which writes each record's missing or
// differing expectations from the port's runs, and read the diff line by
// line before keeping it; porttest's TestRunRecords then checks every
// record against the reference Go compiler, and TestRungenLines that
// every line rungen writes is in the committed archive as rungen wrote
// it. A case file whose bytes would not survive the archive, a line that
// reads as a txtar header or no final newline, stops rungen.
//
// It is run from this module's root as
//
//	go run ./porttest/rungen <dir>
//
// and writes prototype.txtar in dir, the port's corpus/runs or another.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tacklang/tack/internal/invalid"
)

// record is one case of a run archive: its name, its files and its
// record's lines.
type record struct {
	name  string
	files [][2]string
	lines []string
}

// main writes the archive, exiting 1 with a message when it cannot.
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: rungen <dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "rungen:", err)
		os.Exit(1)
	}
}

// q returns s as a Go string literal, as a record's quoted operand.
func q(s string) string { return strconv.Quote(s) }

// chunks returns one line `key "<line>"` for each line of s, each with
// its newline, and one for what follows the last, so they read back as s.
func chunks(key, s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, key+" "+q(s))
			break
		}
		out = append(out, key+" "+q(s[:i+1]))
		s = s[i+1:]
	}
	return out
}

// source is a Go file of this module, parsed, whose top-level constants,
// variables and functions' tables rungen reads.
type source struct {
	file *ast.File
	path string
}

// sources caches the files rungen has read, by path below this module's
// root.
var sources = map[string]*source{}

// src returns the parsed Go file at path, below this module's root.
func src(p string) *source {
	if s, ok := sources[p]; ok {
		return s
	}
	f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
	if err != nil {
		fail(err)
	}
	s := &source{file: f, path: p}
	sources[p] = s
	return s
}

// fail stops rungen with err.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "rungen:", err)
	os.Exit(1)
}

// decl returns the value of the top-level constant or variable name.
func (s *source) decl(name string) ast.Expr {
	for _, d := range s.file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range g.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name == name && i < len(vs.Values) {
					return vs.Values[i]
				}
			}
		}
	}
	fail(fmt.Errorf("%s declares no %s", s.path, name))
	return nil
}

// fn returns the body of the top-level function name.
func (s *source) fn(name string) *ast.BlockStmt {
	for _, d := range s.file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Recv == nil && f.Name.Name == name {
			return f.Body
		}
	}
	fail(fmt.Errorf("%s has no function %s", s.path, name))
	return nil
}

// str returns the string constant or variable name.
func (s *source) str(name string) string { return s.eval(s.decl(name), nil).(string) }

// eval returns the value of e, evaluated as a constant: a string, an
// int64, a bool or a time.Duration; an identifier is one of locals, or a
// top-level constant or variable of s; filepath.Join(x, parts...) gives
// the literal parts joined, x being the directory the test joins them
// to.
func (s *source) eval(e ast.Expr, locals map[string]any) any {
	switch x := e.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING:
			v, err := strconv.Unquote(x.Value)
			if err != nil {
				fail(err)
			}
			return v
		case token.INT:
			v, err := strconv.ParseInt(x.Value, 0, 64)
			if err != nil {
				fail(err)
			}
			return v
		}
	case *ast.ParenExpr:
		return s.eval(x.X, locals)
	case *ast.Ident:
		if v, ok := locals[x.Name]; ok {
			return v
		}
		if x.Name == "true" || x.Name == "false" {
			return x.Name == "true"
		}
		return s.eval(s.decl(x.Name), nil)
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "time" {
			switch x.Sel.Name {
			case "Millisecond":
				return time.Millisecond
			case "Second":
				return time.Second
			}
		}
	case *ast.BinaryExpr:
		a, b := s.eval(x.X, locals), s.eval(x.Y, locals)
		switch av := a.(type) {
		case string:
			if x.Op == token.ADD {
				return av + b.(string)
			}
		case int64:
			if d, ok := b.(time.Duration); ok && x.Op == token.MUL {
				return time.Duration(av) * d
			}
			if x.Op == token.ADD {
				return av + b.(int64)
			}
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Join" && len(x.Args) > 1 {
			var parts []string
			for _, a := range x.Args[1:] {
				parts = append(parts, s.eval(a, locals).(string))
			}
			return path.Join(parts...)
		}
	}
	fail(fmt.Errorf("%s: %T is no constant rungen evaluates", s.path, e))
	return nil
}

// rows returns the rows of the composite literal of a table, lit, each a
// map of its fields' values by name, fields given positionally taking
// their names from names in order, or, when names is nil, from the
// struct the table's own type declares; a field rungen cannot evaluate
// is left out of the row, for the caller to do without.
func (s *source) rows(lit *ast.CompositeLit, names []string, locals map[string]any) []map[string]any {
	if names == nil {
		names = structFields(lit.Type)
	}
	var out []map[string]any
	for _, el := range lit.Elts {
		row := map[string]any{}
		c, ok := el.(*ast.CompositeLit)
		if !ok {
			fail(fmt.Errorf("%s: a table row is no composite literal", s.path))
		}
		for i, f := range c.Elts {
			name, val := "", f
			if kv, ok := f.(*ast.KeyValueExpr); ok {
				name, val = kv.Key.(*ast.Ident).Name, kv.Value
			} else if i < len(names) {
				name = names[i]
			}
			if v, ok := tryEval(s, val, locals); ok {
				row[name] = v
			}
		}
		out = append(out, row)
	}
	return out
}

// structFields returns the names of the fields, in order, of the
// struct a table's type, []struct{...}, holds, or none when it is no such
// type.
func structFields(t ast.Expr) []string {
	arr, ok := t.(*ast.ArrayType)
	if !ok {
		return nil
	}
	st, ok := arr.Elt.(*ast.StructType)
	if !ok {
		return nil
	}
	var names []string
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

// tryEval is eval, reporting whether e is a constant rungen evaluates
// instead of stopping.
func tryEval(s *source, e ast.Expr, locals map[string]any) (v any, ok bool) {
	switch x := e.(type) {
	case *ast.CallExpr:
		if sel, isSel := x.Fun.(*ast.SelectorExpr); !isSel || sel.Sel.Name != "Join" {
			return nil, false
		}
	case *ast.SelectorExpr:
		if id, isID := x.X.(*ast.Ident); !isID || id.Name != "time" {
			return nil, false
		}
	}
	return s.eval(e, locals), true
}

// rangeTable returns the n-th table, from 0, that a range statement of
// the function fn ranges over as a composite literal.
func (s *source) rangeTable(fn string, n int) *ast.CompositeLit {
	var found []*ast.CompositeLit
	ast.Inspect(s.fn(fn), func(node ast.Node) bool {
		if r, ok := node.(*ast.RangeStmt); ok {
			if c, ok := r.X.(*ast.CompositeLit); ok {
				found = append(found, c)
			}
		}
		return true
	})
	if n >= len(found) {
		fail(fmt.Errorf("%s: %s ranges over %d tables, not %d", s.path, fn, len(found), n+1))
	}
	return found[n]
}

// returnStrings returns the string literals among the arguments of the
// call the function fn returns, as osExampleEnv returns append(env, ...).
func (s *source) returnStrings(fn string) []string {
	var out []string
	ast.Inspect(s.fn(fn), func(node ast.Node) bool {
		if r, ok := node.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
			if c, ok := r.Results[0].(*ast.CallExpr); ok {
				for _, a := range c.Args {
					if l, ok := a.(*ast.BasicLit); ok && l.Kind == token.STRING {
						out = append(out, s.eval(l, nil).(string))
					}
				}
			}
		}
		return true
	})
	return out
}

// strs returns the []string literal value of the variable name.
func (s *source) strs(name string) []string {
	c, ok := s.decl(name).(*ast.CompositeLit)
	if !ok {
		fail(fmt.Errorf("%s: %s is no literal list", s.path, name))
	}
	var out []string
	for _, e := range c.Elts {
		out = append(out, s.eval(e, nil).(string))
	}
	return out
}

// tackLexerFull returns a source of exactly 4096 tokens and what
// examples/tack-lexer prints for it, as tack/main_test.go's tackLexerFull
// makes them, by a loop, which rungen cannot read as a constant: 2047
// lines of x, each an identifier and a semicolon, then a written
// semicolon and the EOF.
func tackLexerFull() (string, string) {
	var out strings.Builder
	for i := range 2047 {
		at, next := strconv.Itoa(2*i), strconv.Itoa(2*i+1)
		out.WriteString("ident " + at + " " + next + " \"x\"\n; " + next + " " + next + " \"\"\n")
	}
	out.WriteString("; 4094 4095 \";\"\nEOF 4095 4095 \"\"\n")
	return strings.Repeat("x\n", 2047) + ";", out.String()
}

// sanitizeFlags are porttest's sanitizeFlags, read from its source, with
// codegen/c's useAfterReturn, as codegen/c's sanitizer builds pass them.
func sanitizeFlags() string {
	base := src("porttest/allocfail_test.go").str("sanitizeFlags")
	uar := src("codegen/c/sanitize_test.go").str("useAfterReturn")
	return strings.Replace(base, "-fno-sanitize-recover=all", uar+" -fno-sanitize-recover=all", 1)
}

// records returns the records of prototype.txtar, in no order.
func records() []record {
	var rs []record
	mainT, extern, arena, pipeT := src("tack/main_test.go"), src("tack/extern_test.go"),
		src("codegen/c/arena_runtime_test.go"), src("tack/brokenpipe_test.go")
	var opts []string
	for _, e := range src("codegen/backend.go").decl("Opts").(*ast.CompositeLit).Elts {
		opts = append(opts, src("codegen/backend.go").eval(e, nil).(string))
	}

	argsSrc := "package main\n\n" +
		"func greet(s string, n int64) {\n    print(s)\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n\n" +
		"func main() {\n    greet(\"hello\", 7)\n}\n"
	rs = append(rs, record{"args-exit", [][2]string{{"main.tck", argsSrc}}, []string{
		"replaces codegen/c/gen_test.go TestGeneratedProgramPassesArgumentsAtRuntime", "exit 7", "stdout " + q("hello")}})
	rs = append(rs, record{"hello", nil, []string{"replaces tack/main_test.go TestBuiltHelloPrints", "dir examples/hello",
		"exit 0", "stdout " + q("Hello, World!\n")}})

	full, fullOut := tackLexerFull()
	lexRows := mainT.rows(mainT.rangeTable("TestBuiltTackLexer", 0), nil, map[string]any{"full": full, "fullOut": fullOut})
	for _, row := range lexRows {
		name := row["name"].(string)
		sub := strings.ReplaceAll(name, " ", "_")
		lines := []string{"replaces tack/main_test.go TestBuiltTackLexer/" + sub, "dir examples/tack-lexer", "target cmd/lex"}
		in := row["in"].(string)
		if in == "" {
			lines = append(lines, "stdin "+q(""))
		}
		lines = append(lines, chunks("stdin", in)...)
		lines = append(append(lines, "exit "+strconv.FormatInt(row["code"].(int64), 10)), chunks("stdout", row["out"].(string))...)
		rs = append(rs, record{"lexer-" + strings.ReplaceAll(name, " ", "-"), nil, lines})
	}

	rs = append(rs, record{"logical", nil, []string{"replaces tack/main_test.go TestBuiltLogicalShortCircuitsAndExits13",
		"dir examples/logical", "exit 13", "stdout " + q("first\n"), "stdout " + q("second\n")}})
	var osLines []string
	for _, o := range opts {
		osLines = append(osLines, "replaces tack/main_test.go TestBuiltOsPassesEveryCheck/"+o)
	}
	osLines = append(osLines, "dir examples/os", "opt "+strings.Join(opts, " "))
	for _, a := range mainT.strs("osExampleArgs") {
		osLines = append(osLines, "arg "+q(a))
	}
	for _, kv := range mainT.returnStrings("osExampleEnv") {
		osLines = append(osLines, "env "+q(kv))
	}
	osLines = append(osLines, "limit nofile 64", "exit 0", "stdout-file examples/os/output")
	rs = append(rs, record{"os", nil, osLines})

	for _, row := range mainT.rows(mainT.rangeTable("TestBuiltPackagesRunsBothMains", 0), nil, nil) {
		name := row["name"].(string)
		lines := []string{"replaces tack/main_test.go TestBuiltPackagesRunsBothMains/" + strings.ReplaceAll(name, " ", "_"),
			"dir examples/packages", "target " + row["target"].(string), "exit " + strconv.FormatInt(row["code"].(int64), 10)}
		rs = append(rs, record{"packages-" + strings.ReplaceAll(name, " ", "-"), nil, append(lines, chunks("stdout", row["out"].(string))...)})
	}
	rs = append(rs, record{"print", nil, append([]string{"replaces tack/main_test.go TestBuiltPrintWritesNoNewlineOfItsOwn",
		"dir examples/print", "exit 0"}, chunks("stdout", "one line, three prints\n*\n**\n***\ntab\tand\nbreak\n")...)})

	flags := sanitizeFlags()
	// codegen/c's sanitizer builds give clang -O0 and -O1; a record sets
	// the level with opt alone.
	rs = append(rs, record{"arena-unreachable",
		[][2]string{{"live.c", arena.str("arenaAbortC")}, {"main.tck", arena.str("arenaAbortSrc")}},
		append([]string{"replaces codegen/c/arena_runtime_test.go TestArenaFreedAtUnreachable/-O0",
			"replaces codegen/c/arena_runtime_test.go TestArenaFreedAtUnreachable/-O1",
			"opt 0 1", "cflags " + flags, "signal 6"}, chunks("stdout", "held\nlive 0\n")...)})
	rs = append(rs, record{"exit-frees-asan", nil, []string{
		"replaces codegen/c/exit_test.go TestExitFreesExampleIsSanitizerClean/-O0",
		"replaces codegen/c/exit_test.go TestExitFreesExampleIsSanitizerClean/-O1",
		"dir examples/exit-frees", "opt 0 1", "cflags " + flags, "exit 3", "stdout-file examples/exit-frees/output"}})

	pipeRows := pipeT.rows(pipeT.decl("brokenPipeRuns").(*ast.CompositeLit), []string{"name", "stream", "other", "report", "status"}, nil)
	for _, row := range pipeRows {
		name := row["name"].(string)
		if name != "print" && name != "eprintln" {
			continue
		}
		broken := map[int64]string{1: "stdout", 2: "stderr"}[row["stream"].(int64)]
		lines := []string{"replaces tack/brokenpipe_test.go TestBuiltBrokenPipeEndsLikeExit/2/" + name,
			"dir examples/broken-pipe", "opt 2", "stream fd3 capture", "stream fd4 broken", "arg " + q(name),
			"stream " + broken + " broken", "exit " + strconv.FormatInt(row["status"].(int64), 10), "fd3 " + q(row["report"].(string))}
		other := map[string]string{"stdout": "stderr", "stderr": "stdout"}[broken]
		rs = append(rs, record{"broken-pipe-" + name + "-O2", nil, append(lines, chunks(other, row["other"].(string))...)})
	}
	live := pipeT.str("brokenPipeLive")
	for _, row := range pipeT.rows(pipeT.rangeTable("TestBuiltBrokenPipeOtherDescriptors", 1), nil, nil) {
		if row["name"].(string) != "child" {
			continue
		}
		lines := []string{"replaces tack/brokenpipe_test.go TestBuiltBrokenPipeOtherDescriptors/2/child",
			"dir examples/broken-pipe", "opt 2", "stream fd3 capture", "stream fd4 broken", "arg " + q("child"),
			"stream stderr broken", "exit 0", "fd3 " + q(live)}
		rs = append(rs, record{"broken-pipe-child-O2", nil, append(lines, chunks("stdout", row["out"].(string))...)})
	}
	rs = append(rs, record{"broken-pipe-write-file-capture-O2", nil, []string{
		"selfcheck a captured fd4, reopened through /dev/fd/4, is a regular file the program writes",
		"dir examples/broken-pipe", "opt 2", "arg " + q("write-file"), "stream fd3 capture", "stream fd4 capture", "exit 0",
		"fd3 " + q(live), "fd4 " + q("x")}})
	rs = append(rs, record{"fmt-stderr-closed", nil, []string{"replaces tack/main_test.go TestBuiltFmtStderrWritesBothStreams/0",
		"partial the run with standard error closed alone, not the pipe and /dev/full runs",
		"dir examples/fmt-stderr", "opt 0", "stream stderr closed", "exit 0", "stdout-file examples/fmt-stderr/output"}})
	for _, which := range []string{"stdout", "stderr"} {
		rs = append(rs, record{"fmt-exit-" + which + "-file", nil, []string{
			"replaces tack/fmt_streams_linux_test.go TestBuiltFmtStreamsWhateverTheTarget/fmt-exit/0/" + which + "_file",
			"dir examples/fmt-exit", "opt 0", "stream " + which + " file", "exit 3",
			"stdout-file examples/fmt-exit/output", "stderr-file examples/fmt-exit/stderr"}})
	}
	grace := mainT.eval(mainT.decl("endlessGrace"), nil).(time.Duration)
	part := "partial one case of twenty, and not the build without -fno-finite-loops"
	rs = append(rs, record{"endless-bare", nil, []string{"replaces tack/main_test.go TestBuiltEndlessLoopsNeverEnd/2", part,
		"dir examples/endless-loops", "opt 2", "arg " + q("bare"), "still-running " + strconv.FormatFloat(grace.Seconds(), 'f', -1, 64)}})
	rs = append(rs, record{"endless-ends", nil, []string{"replaces tack/main_test.go TestBuiltEndlessLoopsNeverEnd/2", part,
		"dir examples/endless-loops", "opt 2", "arg " + q("ends"), "exit 0", "stdout " + q("ended\n")}})
	for _, m := range []struct{ name, line string }{{"pipe", ""}, {"file", "stream stdout file"}} {
		lines := []string{"replaces tack/main_test.go TestBuiltFmtStreamsInOnePlaceKeepCallOrder/0", "dir examples/fmt-interleave", "opt 0"}
		if m.line != "" {
			lines = append(lines, m.line)
		}
		rs = append(rs, record{"fmt-interleave-" + m.name, nil, append(lines, "stream stderr merged", "exit 0",
			"stdout-file examples/fmt-interleave/combined")})
	}

	sumFiles := [][2]string{{"helpers.c", "#include \"tack.h\"\n\n" + extern.str("sumC")}, {"main.tck", extern.str("sumProgram")}}
	rs = append(rs, record{"emit-header-sum", sumFiles, []string{"replaces tack/extern_test.go TestEmitHeaderWritesTackH",
		"invoke " + q("--emit-header"), "invoke " + q("$RUN/main.tck"), "exit 0"}})
	rs = append(rs, record{"emit-header-sum-run", sumFiles, []string{"replaces tack/extern_test.go TestEmitHeaderWritesTackH", "exit 42"}})
	empty := "package main\n\nfunc main() {\n}\n"
	rs = append(rs, record{"directory-diagnostics", [][2]string{{"a.tck", empty}, {"b.tck", empty}, {"tack.mod", "module m\n"}},
		[]string{"replaces tack/main_test.go TestDirectoryDiagnosticsNameEachFile", "cwd empty",
			"invoke " + q("--emit"), "invoke " + q("$SRC"), "exit 1"}})
	rs = append(rs, record{"extern-keyword-names-emit", [][2]string{{"extern-keyword-names/main.tck", invalidSource("examples/invalid.txtar", "extern-keyword-names")}}, []string{
		"replaces tack/main_test.go TestInvalidProgramsReportToStderrOnly/extern-keyword-names",
		"partial the --out build of the case is not held",
		"note the port names the type's package, a known divergence",
		"invoke " + q("--emit"), "invoke " + q("extern-keyword-names/main.tck"), "exit 1"}})

	for _, code := range []string{"124", "125"} {
		rs = append(rs, record{"selfcheck-exit-" + code, [][2]string{{"main.tck", "package main\n\nfunc main() {\n    exit(" + code + ")\n}\n"}},
			[]string{"selfcheck an exit status coreutils' timeout and the launcher also use is checked as any other", "exit " + code}})
	}
	rs = append(rs, record{"selfcheck-paths", [][2]string{{"main.tck", pathsProgram}}, []string{
		"selfcheck $RUN, $TMP and $PROG stand for the run's directories and program in arguments, outputs and files",
		"arg " + q("$RUN/x"), "arg " + q("$TMP/y"), "exit 0",
		"stdout " + q("argv0 $PROG\n"), "stdout " + q("arg $RUN/x\n"), "stdout " + q("arg $TMP/y\n"), "stdout " + q("TMPDIR $TMP\n"),
		"stderr " + q("cwd $RUN\n"), "file " + q("where.txt") + " " + q("$RUN\n")}})
	rs = append(rs, record{"selfcheck-literal-placeholder", [][2]string{{"main.tck", literalProgram}}, []string{
		"selfcheck a program that prints the text $RUN, never its argument, shows it as $$RUN, which no run directory can stand for",
		"arg " + q("$RUN/x"), "exit 0", "stdout " + q("arg $$RUN/x\n"), "stderr " + q("")}})
	rs = append(rs, record{"selfcheck-still-running-ignores-term", [][2]string{{"hold.c", holdC}, {"main.tck", holdProgram}}, []string{
		"selfcheck a program that ignores SIGTERM, signals its group and goes on is still running at the deadline, and is stopped",
		"still-running 0.5", "stdout " + q(""), "stderr " + q("")}})
	rs = append(rs, record{"selfcheck-symlinks", [][2]string{{"ln.c", linksC}, {"main.tck", linksProgram}}, []string{
		"selfcheck symbolic links the program leaves, one dangling and one to a directory, are pinned by symlink lines and never followed",
		"env " + q("SRC_MAIN=$SRC/main.tck"), "exit 0", "stdout " + q(""), "stderr " + q(""),
		"symlink " + q("dangling") + " " + q("no-such-file"), "symlink " + q("up") + " " + q(".."),
		"symlink " + q("sub/abs") + " " + q("$SRC/main.tck")}})
	rs = append(rs, record{"selfcheck-environment", [][2]string{{"fds.c", fdsC}, {"main.tck", environmentProgram}}, []string{
		"selfcheck a program's environment is PATH, TMPDIR and the record's env lines alone, and descriptors 3 to 62 are closed",
		"env " + q("RECORD_VAR=set"), "exit 0"}})
	rs = append(rs, record{"selfcheck-multi-line-file", [][2]string{{"main.tck", twoLinesProgram}}, []string{
		"selfcheck a file of several lines, written by file and file+ lines", "exit 0"}})
	rs = append(rs, record{"selfcheck-file-names", [][2]string{{"main.tck", fileNamesProgram}}, []string{
		"selfcheck file paths any clean relative path is: one named expected, one with a space, one starting with ..",
		"exit 0", "file " + q("expected") + " " + q("1"), "file " + q("a b") + " " + q("2"), "file " + q("..x") + " " + q("3")}})
	rs = append(rs, record{"selfcheck-raw-byte", [][2]string{{"main.tck", rawByteProgram}}, []string{
		"selfcheck a quoted operand holds a raw byte that is no UTF-8 as that byte, on every reader",
		"arg \"\xff\"", "exit 0", "stdout " + q("\xff\n")}})
	return rs
}

// invalidSource returns the program of the case name of the archive
// file, in this module's invalid.txtar format.
func invalidSource(file, name string) string {
	data, err := os.ReadFile(file) //nolint:gosec // G304: a file of this module
	if err != nil {
		fail(err)
	}
	_, cases, err := invalid.Parse(data)
	if err != nil {
		fail(err)
	}
	for _, c := range cases {
		if c.Name == name {
			return string(c.Source)
		}
	}
	fail(fmt.Errorf("%s has no case %s", file, name))
	return ""
}

// checkFile reports why the bytes data of the file p of a case cannot be
// a section of a run archive as they are: no final newline, or a line
// that reads as a txtar header, or nil.
func checkFile(p string, data string) error {
	if data != "" && !strings.HasSuffix(data, "\n") {
		return fmt.Errorf("file %s does not end with a newline", p)
	}
	for _, l := range strings.Split(data, "\n") {
		if strings.HasPrefix(l, "-- ") && strings.HasSuffix(l, " --") {
			return fmt.Errorf("file %s holds a line that reads as a txtar header: %q", p, l)
		}
	}
	return nil
}

// render returns the archive of rs, which must have names of their own,
// and files every one of which checkFile passes.
func render(rs []record) ([]byte, error) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].name < rs[j].name })
	var b bytes.Buffer
	b.WriteString("Run records of the prototype, written by tackc's porttest/rungen and completed\n" +
		"by tools/corpus --update; see tools/corpus/runs.tck for the format.\n")
	for i, r := range rs {
		if i > 0 && rs[i-1].name == r.name {
			return nil, fmt.Errorf("two records named %s", r.name)
		}
		for _, f := range r.files {
			if err := checkFile(r.name+"/"+f[0], f[1]); err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "-- %s/%s --\n%s", r.name, f[0], f[1])
		}
		for _, l := range r.lines {
			if strings.Contains(l, "\n") {
				return nil, errors.New("a record line holds a newline: " + l)
			}
		}
		fmt.Fprintf(&b, "-- %s/record --\n%s\n", r.name, strings.Join(r.lines, "\n"))
	}
	return b.Bytes(), nil
}

// run writes prototype.txtar in dir.
func run(dir string) error {
	data, err := render(records())
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "prototype.txtar"), data, 0o644) //nolint:gosec // G306: a corpus file
}

// pathsProgram prints its argv[0], arguments and TMPDIR, and writes its
// directory to standard error and to where.txt, so the runner's $PROG,
// $RUN and $TMP are exercised.
const pathsProgram = `package main

import fmt from "std/fmt"
import os from "std/os"

// main prints its argv[0], arguments and TMPDIR, and writes its
// directory to standard error and to where.txt, so the runner's $PROG,
// $RUN and $TMP are exercised.
func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 {
        exit(1)
    }
    for i, a := range args {
        if i == 0 {
            fmt.printf("argv0 %s\n", a.bytes())
        } else {
            fmt.printf("arg %s\n", a.bytes())
        }
    }
    tmp, set, e2 := os.env([]uint8("TMPDIR"), c_allocator)
    if e2.code != 0 || !set {
        exit(2)
    }
    fmt.printf("TMPDIR %s\n", tmp.bytes())
    wd, e3 := os.getwd(c_allocator)
    if e3.code != 0 {
        exit(3)
    }
    fmt.eprintf("cwd %s\n", wd.bytes())
    mut line := make(list[uint8], c_allocator)
    e4 := fmt.appendf(mut line, "%s\n", wd.bytes())
    if e4.code != 0 {
        exit(4)
    }
    e5 := os.write_file([]uint8("where.txt"), line[:])
    if e5.code != 0 {
        exit(5)
    }
}
`

// environmentProgram prints, for each variable a shell or a launcher
// could leave, whether it is set, and which of descriptors 3 to 62 are
// open, so a runner that lets either drift shows it.
const environmentProgram = `package main

import fmt from "std/fmt"
import os from "std/os"

// open_fds returns the descriptors from 3 to 62 that are open, a bit each.
extern("env_open_fds") func open_fds() int64

// show prints name and its value, or that it is unset.
func show(name string) {
    v, set, e := os.env([]uint8(name), c_allocator)
    if e.code != 0 {
        exit(1)
    }
    if set {
        fmt.printf("%s=%s\n", name, v.bytes())
    } else {
        fmt.printf("%s unset\n", name)
    }
}

// main prints the variables and the open descriptors.
func main() {
    for _, n := range [7]string{"RECORD_VAR", "PWD", "OLDPWD", "SHLVL", "_", "HOME", "LC_ALL"} {
        show(n)
    }
    fmt.printf("open fds %d\n", open_fds())
}
`

// fdsC is environmentProgram's C: which of descriptors 3 to 62 are open.
const fdsC = `#include <fcntl.h>
#include "tack.h"

int64_t env_open_fds(void) {
    int64_t bits = 0;
    for (int fd = 3; fd <= 62; fd++) {
        if (fcntl(fd, F_GETFD) != -1) {
            bits |= (int64_t)1 << fd;
        }
    }
    return bits;
}
`

// twoLinesProgram writes a file of two lines, so a record holds a file
// line and a file+ line.
const twoLinesProgram = `package main

import os from "std/os"

// main writes two.txt, two lines.
func main() {
    e := os.write_file([]uint8("two.txt"), []uint8("first\nsecond\n"))
    if e.code != 0 {
        exit(1)
    }
}
`

// fileNamesProgram writes files whose names test the rule for file
// paths: expected, a b and ..x are all clean relative paths.
const fileNamesProgram = `package main

import os from "std/os"

// main writes expected, a b and ..x.
func main() {
    if os.write_file([]uint8("expected"), []uint8("1")).code != 0 {
        exit(1)
    }
    if os.write_file([]uint8("a b"), []uint8("2")).code != 0 {
        exit(2)
    }
    if os.write_file([]uint8("..x"), []uint8("3")).code != 0 {
        exit(3)
    }
}
`

// rawByteProgram prints its first argument and a newline.
const rawByteProgram = `package main

import fmt from "std/fmt"
import os from "std/os"

// main prints its first argument and a newline.
func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 {
        exit(1)
    }
    for i, a := range args {
        if i == 1 {
            fmt.printf("%s\n", a.bytes())
        }
    }
}
`

// literalProgram prints the text arg $RUN/x, never its argument.
const literalProgram = `package main

// main prints the text arg $RUN/x.
func main() {
    print("arg $RUN/x\n")
}
`

// holdProgram, with holdC, ignores SIGTERM, signals its own group with
// it, and then waits for a signal forever.
const holdProgram = `package main

// hold ignores SIGTERM, signals the group with it and waits, in C.
extern("probe_hold") func hold()

// main holds on until it is killed.
func main() {
    hold()
}
`

// holdC is holdProgram's C.
const holdC = `#include <signal.h>
#include <unistd.h>
#include "tack.h"

void probe_hold(void) {
    signal(SIGTERM, SIG_IGN);
    kill(0, SIGTERM);
    for (;;) {
        pause();
    }
}
`

// linksProgram, with linksC, leaves symbolic links in its directory: one
// dangling, one to the directory above, and one in a new directory to a
// file of $SRC.
const linksProgram = `package main

// links makes the links, in C, and returns 0, or 1 when it cannot.
extern("probe_links") func links() int64

// main makes the links and exits 1 when it cannot.
func main() {
    if links() != 0 {
        exit(1)
    }
}
`

// linksC is linksProgram's C; the program's directory is $RUN, and
// $SRC/main.tck, the target of sub/abs, is the environment's SRC_MAIN.
const linksC = `#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>
#include "tack.h"

int64_t probe_links(void) {
    const char *src = getenv("SRC_MAIN");
    if (src == NULL) {
        return 1;
    }
    if (symlink("no-such-file", "dangling") != 0 || symlink("..", "up") != 0) {
        return 1;
    }
    if (mkdir("sub", 0755) != 0 || symlink(src, "sub/abs") != 0) {
        return 1;
    }
    return 0;
}
`

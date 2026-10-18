package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// libModule is a module with no main package: a root with no Tack files,
// the library geom, and the library shapes, which imports geom and std.
var libModule = map[string]string{
	"tack.mod": "module calc\n",
	"geom/geom.tck": "package geom\n\n" +
		"pub func Area(w int64, h int64) int64 {\n    return w * h\n}\n",
	"geom/round/round.tck": "package round\n\n" +
		"pub func Half(n int64) int64 {\n    return n / 2\n}\n",
	"shapes/shapes.tck": "package shapes\n\n" +
		"import geom from \"calc/geom\"\n\n" +
		"pub func Square(s int64) int64 {\n    return geom.Area(s, s)\n}\n",
}

// withFiles returns a copy of base with files added or replaced.
func withFiles(base map[string]string, files map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(files))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range files {
		out[k] = v
	}
	return out
}

// dirEntries returns the names of every file at or below dir, sorted, so a
// test can check that a run wrote nothing there.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	return names
}

// assertCheckClean checks r is a clean check: exit 0 and nothing printed.
func assertCheckClean(t *testing.T, r result) {
	t.Helper()
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and nothing printed",
			r.code, r.stdout, r.stderr)
	}
}

// TestCheckLibraryModule checks --check on a module with no main package:
// from the root, which holds no Tack files, every package below it is
// checked; from a package's directory, that package and those below it;
// and by import path, the same. Nothing is written anywhere, and the
// toolchain is never run: a backend whose toolchain does not exist still
// succeeds.
func TestCheckLibraryModule(t *testing.T) {
	root := writeDir(t, libModule)
	tmp := isolateTemp(t)
	before := dirEntries(t, root)
	for _, tc := range []struct{ name, cwd, target string }{
		{"root", ".", root},
		{"package", ".", filepath.Join(root, "geom")},
		{"leaf package", ".", filepath.Join(root, "geom", "round")},
		{"by path", root, "calc/shapes"},
		{"module by path", root, "calc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cwd != "." {
				t.Chdir(tc.cwd)
			}
			assertCheckClean(t, runTack(t, "--check", tc.target))
			assertCheckClean(t, runTack(t, "--check", "--backend", missingBackend, tc.target))
		})
	}
	if after := dirEntries(t, root); !slices.Equal(before, after) {
		t.Errorf("--check changed the module:\nbefore %q\nafter  %q", before, after)
	}
	assertEmptyDir(t, tmp)
}

// TestCheckLibraryModuleErrors checks --check reports the problems of every
// package of a library module, sorted by file, with exit status 1 and
// nothing on stdout, and that a package below the target is checked even
// though nothing imports it.
func TestCheckLibraryModuleErrors(t *testing.T) {
	root := writeDir(t, withFiles(libModule, map[string]string{
		"shapes/shapes.tck": "package shapes\n\n" +
			"pub func Square(s int64) int64 {\n    return \"s\"\n}\n",
		"geom/round/round.tck": "package round\n\n" +
			"pub func Half(n int64) int64 {\n    return \"h\"\n}\n",
	}))
	t.Chdir(root)
	r := runTack(t, "--check", ".")
	assertDiagnostics(t, r)
	round := strings.Index(r.stderr, "geom/round/round.tck:4:12: error:")
	shapes := strings.Index(r.stderr, "shapes/shapes.tck:4:12: error:")
	if round < 0 || shapes < 0 || round > shapes {
		t.Errorf("want round.tck's error, then shapes.tck's:\n%s", r.stderr)
	}

	// Checking only shapes leaves round out.
	r = runTack(t, "--check", "shapes")
	assertDiagnostics(t, r)
	if strings.Contains(r.stderr, "round.tck") || !strings.Contains(r.stderr, "shapes.tck") {
		t.Errorf("checking shapes alone reported:\n%s", r.stderr)
	}
}

// TestCheckLibraryWithoutCheck checks that, without --check, a library
// directory is still rejected as not a main package.
func TestCheckLibraryWithoutCheck(t *testing.T) {
	root := writeDir(t, libModule)
	t.Chdir(root)
	r := runTack(t, "--emit", "geom")
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "error: calc/geom is not a main package") {
		t.Errorf("stderr:\n%s", r.stderr)
	}
}

// TestCheckSkipsMainsAndNestedModules checks a library check leaves out a
// main package below its target, which is checked by naming it, a
// directory holding its own module, and a directory whose name starts
// with a dot, and that a target with no package to check is an error.
func TestCheckSkipsMainsAndNestedModules(t *testing.T) {
	root := writeDir(t, withFiles(libModule, map[string]string{
		"cmd/tool/main.tck":   "package main\n\nfunc main() {\n    undefined()\n}\n",
		"other/tack.mod":      "module other\n",
		"other/x/x.tck":       "package x\n\nfunc f() int64 {\n    return \"x\"\n}\n",
		".hidden/h/h.tck":     "package h\n\nfunc f() int64 {\n    return \"h\"\n}\n",
		"empty/notes.txt":     "",
		"onlymain/a/main.tck": "package main\n\nfunc main() {}\n",
	}))
	t.Chdir(root)
	assertCheckClean(t, runTack(t, "--check", "."))

	r := runTack(t, "--check", "cmd/tool")
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "undefined: undefined") {
		t.Errorf("checking the main package itself reported:\n%s", r.stderr)
	}

	assertFailure(t, runTack(t, "--check", "empty"), "no .tck files in empty or below")
	assertFailure(t, runTack(t, "--check", "onlymain"),
		"no library package in onlymain or below; check a main package by naming its directory")
}

// TestCheckMainWithoutFunc checks --check on a main package that declares
// no main function: the missing function is reported, and checking goes
// on, so a type error is reported with it, sorted by position.
func TestCheckMainWithoutFunc(t *testing.T) {
	src := "package main\n\nfunc helper() int64 {\n    return \"s\"\n}\n"
	file := writeSource(t, []byte(src))

	r := runTack(t, "--check", file)
	assertDiagnostics(t, r)
	missing := strings.Index(r.stderr, "error: main function not found")
	typeErr := strings.Index(r.stderr, ":4:12: error:")
	if missing < 0 || typeErr < 0 || missing > typeErr {
		t.Errorf("want the missing main, then the type error:\n%s", r.stderr)
	}

	// Without --check the missing main is reported with the type error
	// too, since every stage runs whatever an earlier one reported.
	r = runTack(t, "--emit", file)
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "main function not found") ||
		!strings.Contains(r.stderr, ":4:12:") {
		t.Errorf("without --check:\n%s", r.stderr)
	}

	// A clean package missing only main reports only that.
	clean := writeSource(t, []byte("package main\n\nfunc helper() int64 {\n    return 1\n}\n"))
	r = runTack(t, "--check", clean)
	assertDiagnostics(t, r)
	if strings.Count(r.stderr, "error:") != 1 {
		t.Errorf("want one error:\n%s", r.stderr)
	}

	// A main package directory in a module is the same.
	dir := writeDir(t, map[string]string{
		"tack.mod": "module app\n",
		"main.tck": src,
	})
	r = runTack(t, "--check", dir)
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "main function not found") ||
		!strings.Contains(r.stderr, ":4:12:") {
		t.Errorf("directory:\n%s", r.stderr)
	}
}

// TestCheckSingleLibraryFile checks --check on a single file whose package
// is not main: it is checked as a library, while without --check it is
// rejected for having no main package.
func TestCheckSingleLibraryFile(t *testing.T) {
	file := writeSource(t, []byte(
		"package geom\n\npub func Area(w int64, h int64) int64 {\n    return w * h\n}\n"))
	assertCheckClean(t, runTack(t, "--check", file))
	r := runTack(t, "--emit", file)
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "main package not found") {
		t.Errorf("without --check:\n%s", r.stderr)
	}

	bad := writeSource(t, []byte("package geom\n\nfunc f() int64 {\n    return \"s\"\n}\n"))
	assertDiagnostics(t, runTack(t, "--check", bad))
}

// TestCheckValidProgram checks --check on a valid program prints nothing,
// exits 0, and builds nothing: no file appears beside the source or in
// the temp directory, and a toolchain that fails or is missing is never
// run.
func TestCheckValidProgram(t *testing.T) {
	file := writeSource(t, []byte("package main\n\nfunc main() {\n    print(\"hi\\n\")\n}\n"))
	tmp := isolateTemp(t)
	before := dirEntries(t, filepath.Dir(file))
	for _, backend := range []string{defaultBackend, failBackend, missingBackend} {
		assertCheckClean(t, runTack(t, "--check", "--backend", backend, file))
	}
	if after := dirEntries(t, filepath.Dir(file)); !slices.Equal(before, after) {
		t.Errorf("--check wrote files: %q", after)
	}
	assertEmptyDir(t, tmp)
}

// TestCheckUsageErrors checks --check with --out, or with an -O flag, is
// a usage error, since a check builds nothing.
func TestCheckUsageErrors(t *testing.T) {
	file := writeSource(t, []byte("package main\n\nfunc main() {}\n"))
	assertUsageError(t, runTack(t, "--check", "--out", "x", file),
		"--out cannot be combined with --check, which builds nothing")
	assertUsageError(t, runTack(t, "--check", "-O2", file),
		"-O2 cannot be combined with --check, which builds nothing")
	assertUsageError(t, runTack(t, "--check", "--emit", "--ast", file),
		"--ast and --emit cannot be combined")
}

// TestCheckDumpsOfValidProgram checks every dump flag gives the same
// output with --check as without it, for a valid program.
func TestCheckDumpsOfValidProgram(t *testing.T) {
	for _, target := range []string{example("hello"), filepath.Join(examplesDir, "multi-file")} {
		for _, flag := range []string{"--lex", "--ast", "--res", "--emit"} {
			t.Run(filepath.Base(filepath.Dir(target))+flag, func(t *testing.T) {
				plain := runTack(t, flag, target)
				checked := runTack(t, "--check", flag, target)
				assertOnlyStdout(t, checked)
				if checked.stdout != plain.stdout {
					t.Errorf("%s differs with --check", flag)
				}
			})
		}
	}
}

// TestCheckDumpsWithErrors checks each dump flag with --check on a
// program with a type error: the stages before type checking print their
// output, the diagnostics are printed too, and the exit status is 1; the
// generated code, whose stage comes after, is absent.
func TestCheckDumpsWithErrors(t *testing.T) {
	file := writeSource(
		t,
		[]byte("package main\n\nfunc main() {\n    var x int64 = \"s\"\n    print(x)\n}\n"),
	)
	for _, tc := range []struct {
		flag   string
		output bool
	}{
		{"--lex", true}, {"--ast", true}, {"--res", true}, {"--emit", false}, {"--emit-header", false},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			r := runTack(t, "--check", tc.flag, file)
			if r.code != exitFailure || !strings.Contains(r.stderr, ":4:19: error:") {
				t.Errorf("exit %d, stderr:\n%s", r.code, r.stderr)
			}
			if got := r.stdout != ""; got != tc.output {
				t.Errorf("printed output = %v, want %v:\n%s", got, tc.output, r.stdout)
			}
			// --lex, --ast and --res without --check run no type
			// checker, so they print the same dump; --emit and
			// --emit-header have none.
			if tc.output {
				if r.stdout != runTack(t, tc.flag, file).stdout {
					t.Errorf("output differs from the plain dump")
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), headerName)); err == nil {
		t.Error("--check --emit-header wrote tack.h for a program with errors")
	}
}

// TestCheckDumpsWithParseError checks a parse error, which the later
// stages run past, reporting nothing that follows from it: --check --lex
// still prints the tokens, with the error, and
// --check --ast prints the tree the parser recovered, with a bad node
// where the damage is and the declarations after it intact, and the
// errors; --ast without --check prints only the errors. A lex error
// leaves no tree to print.
func TestCheckDumpsWithParseError(t *testing.T) {
	broken := writeSource(t, []byte("package main\n\nfunc main() {\n"))
	r := runTack(t, "--check", "--lex", broken)
	if r.code != exitFailure || r.stdout == "" || !strings.Contains(r.stderr, "error:") {
		t.Errorf(
			"--lex of a parse error: exit %d, stdout %q, stderr:\n%s",
			r.code,
			r.stdout,
			r.stderr,
		)
	}

	damaged := writeSource(t, []byte(
		"package main\n\nfunc g( {\n}\n\nfunc main() {\n    x := (\n    print(\"a\")\n}\n"))
	r = runTack(t, "--check", "--ast", damaged)
	assertRecoveredTree(t, r.stdout)
	if r.code != exitFailure || strings.Count(r.stderr, "error:") != 2 {
		t.Errorf("exit %d, want 1 and both errors:\n%s", r.code, r.stderr)
	}
	assertDiagnostics(t, runTack(t, "--ast", damaged))

	lexErr := writeSource(t, []byte("package main\n\nvar s = \"open\n"))
	assertDiagnostics(t, runTack(t, "--check", "--ast", lexErr))
}

// assertRecoveredTree checks that stdout is the tree --check --ast prints
// for the damaged program of TestCheckDumpsWithParseError: a bad node for
// the damaged func g, which has no body and records the name it got to,
// and func main after it, intact.
func assertRecoveredTree(t *testing.T, stdout string) {
	t.Helper()
	var tree struct {
		Decls []map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(stdout), &tree); err != nil || len(tree.Decls) != 2 {
		t.Fatalf("--check --ast is not a tree of 2 declarations (%v):\n%s", err, stdout)
	}
	if _, body := tree.Decls[0]["Body"]; body || string(tree.Decls[0]["Name"]) != `"g"` {
		t.Errorf("the damaged func g is not a bad node named g: %s", tree.Decls[0])
	}
	if string(tree.Decls[1]["Name"]) != `"main"` {
		t.Errorf("func main after the damage is lost: %s", tree.Decls[1])
	}
}

// TestCheckMaxErrors checks --max-errors caps what --check reports, ends
// the report with the line saying there were more, and that the default
// is 10.
func TestCheckMaxErrors(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n")
	for range 12 {
		b.WriteString("    exit(\"s\")\n")
	}
	b.WriteString("}\n")
	file := writeSource(t, []byte(b.String()))
	for _, tc := range []struct {
		args []string
		n    int
		more bool
	}{
		{nil, 10, true},
		{[]string{"--max-errors", "3"}, 3, true},
		{[]string{"--max-errors", "0"}, 12, false},
		{[]string{"--max-diags", "2"}, 2, true},
	} {
		r := runTack(t, append(append([]string{"--check"}, tc.args...), file)...)
		assertDiagnostics(t, r)
		if got := strings.Count(r.stderr, "error:"); got != tc.n {
			t.Errorf("%q: %d errors, want %d", tc.args, got, tc.n)
		}
		if got := strings.Contains(r.stderr, "too many errors:"); got != tc.more {
			t.Errorf("%q: too many errors line = %v, want %v", tc.args, got, tc.more)
		}
	}
}

// TestCheckDumpsOfMainWithoutFunc checks --check --res on a main package
// with no main function prints the resolved tree with the diagnostic,
// and --check --emit prints no code.
func TestCheckDumpsOfMainWithoutFunc(t *testing.T) {
	file := writeSource(t, []byte("package main\n\nfunc helper() int64 {\n    return 1\n}\n"))
	r := runTack(t, "--check", "--res", file)
	if r.code != exitFailure || !json.Valid([]byte(r.stdout)) ||
		!strings.Contains(r.stderr, "main function not found") {
		t.Errorf("exit %d, stdout %q, stderr:\n%s", r.code, r.stdout, r.stderr)
	}
	assertDiagnostics(t, runTack(t, "--check", "--emit", file))
}

// TestCheckDumpsOfLibrary checks each dump flag with --check on a library
// module: --lex and --ast cover the files of every package checked, --res
// the whole program, --emit is C with no entry point, and --emit-header
// writes tack.h into each package of the module with C files or extern
// funcs, and no other.
func TestCheckDumpsOfLibrary(t *testing.T) {
	root := writeDir(t, withFiles(libModule, map[string]string{
		"sys/sys.tck": "package sys\n\nextern func now() int64\n\npub func Now() int64 {\n    return now()\n}\n",
		"sys/sys.c":   "#include <stdint.h>\nint64_t now(void) { return 0; }\n",
	}))
	t.Chdir(root)
	t.Run("lex", checkLibraryLex)
	t.Run("ast", checkLibraryAST)
	t.Run("res", checkLibraryRes)

	emit := runTack(t, "--check", "--emit", ".")
	assertOnlyStdout(t, emit)
	if strings.Contains(emit.stdout, "int main(") || !strings.Contains(emit.stdout, "Square") {
		t.Errorf("--emit of a library:\n%s", emit.stdout)
	}

	hdr := runTack(t, "--check", "--emit-header", ".")
	if hdr.code != exitOK || hdr.stdout != "" || hdr.stderr != "" {
		t.Errorf("--emit-header: exit %d, stdout %q, stderr %q", hdr.code, hdr.stdout, hdr.stderr)
	}
	for dir, want := range map[string]bool{"sys": true, "geom": false, "shapes": false, ".": false} {
		_, err := os.Stat(filepath.Join(root, dir, headerName))
		if got := err == nil; got != want {
			t.Errorf("tack.h in %s: %v, want %v", dir, got, want)
		}
	}
}

// checkLibraryLex checks --check --lex in the library of
// TestCheckDumpsOfLibrary prints the tokens of every package's file.
func checkLibraryLex(t *testing.T) {
	lex := runTack(t, "--check", "--lex", ".")
	assertOnlyStdout(t, lex)
	for _, f := range []string{"geom/geom.tck", "geom/round/round.tck", "shapes/shapes.tck", "sys/sys.tck"} {
		if !strings.Contains(lex.stdout, "-- "+f+" --\n") {
			t.Errorf("--lex has no tokens for %s:\n%s", f, lex.stdout)
		}
	}
}

// checkLibraryAST checks --check --ast in the library of
// TestCheckDumpsOfLibrary prints a list of every package's file's tree.
func checkLibraryAST(t *testing.T) {
	var trees []json.RawMessage
	ast := runTack(t, "--check", "--ast", ".")
	assertOnlyStdout(t, ast)
	if err := json.Unmarshal([]byte(ast.stdout), &trees); err != nil || len(trees) != 4 {
		t.Errorf("--ast is not a list of 4 trees (%v):\n%s", err, ast.stdout)
	}
}

// checkLibraryRes checks --check --res in the library of
// TestCheckDumpsOfLibrary prints a program holding every package.
func checkLibraryRes(t *testing.T) {
	var prog struct{ Packages []struct{ Name string } }
	res := runTack(t, "--check", "--res", ".")
	assertOnlyStdout(t, res)
	if err := json.Unmarshal([]byte(res.stdout), &prog); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(prog.Packages))
	for _, p := range prog.Packages {
		names = append(names, p.Name)
	}
	slices.Sort(names)
	if want := []string{"geom", "round", "shapes", "sys"}; !slices.Equal(names, want) {
		t.Errorf("--res packages = %q, want %q", names, want)
	}
}

// TestCheckLibraryEmitWithDeepGlobal checks a library whose global needs
// a runtime initializer still emits it, in a function of its own, since
// there is no C main to run it.
func TestCheckLibraryEmitWithDeepGlobal(t *testing.T) {
	file := writeSource(t, []byte(deepGlobalLibrary))
	r := runTack(t, "--check", "--emit", file)
	assertOnlyStdout(t, r)
	if strings.Contains(r.stdout, "int main(") ||
		!strings.Contains(r.stdout, "static void tack_init_(void)") {
		t.Errorf("--emit of a library with a deep global:\n%s", r.stdout)
	}
}

// deepGlobalLibrary is a library whose global's initializer nests 40
// structs deep, deeper than the C backend writes a static initializer.
var deepGlobalLibrary = func() string {
	const depth = 40
	var b strings.Builder
	b.WriteString("package deep\n\ntype G0 struct {\n    v int64\n}\n\n")
	for i := 1; i <= depth; i++ {
		b.WriteString(
			"type G" + strconv.Itoa(i) + " struct {\n    n G" + strconv.Itoa(i-1) + "\n}\n\n",
		)
	}
	lit := "G0{v: 1}"
	for i := 1; i <= depth; i++ {
		lit = "G" + strconv.Itoa(i) + "{n: " + lit + "}"
	}
	b.WriteString("var g = " + lit + "\n\npub func Get() int64 {\n    return g" +
		strings.Repeat(".n", depth) + ".v\n}\n")
	return b.String()
}()

// TestCheckLibraryLoadProblems checks a library check reports what keeps
// its packages from loading: an import cycle between two of its own
// packages, an import of a main package below it, which the check leaves
// out, a package whose clause does not match its directory, and a lex
// error, which stops loading at that package; and that --max-diags caps
// what is printed.
func TestCheckLibraryLoadProblems(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"cycle", map[string]string{
			"tack.mod": "module m\n",
			"a/a.tck":  "package a\n\nimport b from \"m/b\"\n",
			"b/b.tck":  "package b\n\nimport a from \"m/a\"\n",
		}, []string{"error: import cycle: m/a imports m/b imports m/a"}},
		{"imports main", map[string]string{
			"tack.mod":       "module m\n",
			"a/a.tck":        "package a\n\nimport t from \"m/cmd/t\"\n",
			"cmd/t/main.tck": "package main\n\nfunc main() {}\n",
		}, []string{"error: cannot import m/cmd/t: it is a main package"}},
		{"clause", map[string]string{
			"tack.mod": "module m\n",
			"a/a.tck":  "package zz\n",
		}, []string{"error: package zz does not match its directory; expected package a"}},
		{"import fails to parse", map[string]string{
			"tack.mod": "module m\n",
			"m.tck":    "package m\n\nimport a from \"m/a\"\n",
			"a/a.tck":  "",
		}, []string{"a/a.tck:1:1: error:"}},
		{"lex", map[string]string{
			"tack.mod": "module m\n",
			"a/a.tck":  "package a\n\nvar s = \"open\n",
			"b/b.tck":  "package b\n\nfunc f() int64 {\n    return \"x\"\n}\n",
		}, []string{"a/a.tck:3:9: error:"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(writeDir(t, tc.files))
			r := runTack(t, "--check", ".")
			assertDiagnostics(t, r)
			for _, w := range tc.want {
				if !strings.Contains(r.stderr, w) {
					t.Errorf("stderr does not contain %q:\n%s", w, r.stderr)
				}
			}
			if tc.name == "lex" && strings.Contains(r.stderr, "b.tck") {
				t.Errorf("loading went on past the lex error:\n%s", r.stderr)
			}
		})
	}

	t.Chdir(writeDir(t, map[string]string{
		"tack.mod": "module m\n",
		"a/a.tck":  "package a\n\nfunc f() int64 {\n    return \"x\"\n}\n",
		"b/b.tck":  "package b\n\nfunc f() int64 {\n    return \"x\"\n}\n",
	}))
	r := runTack(t, "--check", "--max-diags", "1", ".")
	assertDiagnostics(t, r)
	if strings.Count(r.stderr, "error:") != 1 || !strings.Contains(r.stderr, "a/a.tck") {
		t.Errorf("--max-diags 1 printed:\n%s", r.stderr)
	}
}

// TestCheckMissingTargets checks --check on a target that does not exist
// fails as a build of it does.
func TestCheckMissingTargets(t *testing.T) {
	t.Chdir(t.TempDir())
	assertFailure(t, runTack(t, "--check", "nothing.tck"), "failed to open file")
	assertFailure(t, runTack(t, "--check", "calc/x"), "no tack.mod found")
}

// checkCheckCoherent checks that r, a run with --check, is internally
// consistent: success with nothing on stderr, or failure with something
// there, whatever is on stdout.
func checkCheckCoherent(t *testing.T, r result) {
	t.Helper()
	switch {
	case r.code == exitOK && r.stderr != "":
		t.Errorf("succeeded but wrote to stderr: %q", r.stderr)
	case r.code == exitFailure && r.stderr == "":
		t.Errorf("failed with nothing on stderr")
	case r.code != exitOK && r.code != exitFailure:
		t.Errorf("exit code = %d, want 0 or 1", r.code)
	}
}

// FuzzRunCheck runs the driver's --check --res on arbitrary source as a
// single file and checks the result is coherent, never a panic: success
// with nothing on stderr, or failure with diagnostics on stderr, with or
// without the resolved tree on stdout; and that a file --emit accepts,
// --check accepts too. It is seeded as FuzzRunEmit is.
func FuzzRunCheck(f *testing.F) {
	paths, _ := filepath.Glob(filepath.Join(examplesDir, "*", "*.tck"))
	for _, p := range paths {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(src)
		}
	}
	for _, tc := range invalidCases(f) {
		f.Add(tc.Source)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		path := writeSource(t, src)
		r := runTack(t, "--check", "--res", path)
		checkCheckCoherent(t, r)
		if e := runTack(t, "--emit", path); e.code == exitOK && r.code != exitOK {
			t.Errorf("--emit accepts the file but --check rejects it:\n%s", r.stderr)
		}
	})
}

// TestCheckReportsEveryStage checks that --check on a library goes on past
// an import that cannot be loaded: that error and an independent type
// error are both reported, nothing that uses the import is, and --emit
// prints no code for it.
func TestCheckReportsEveryStage(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"tack.mod": "module lib\n",
		"a/a.tck": "package a\n\nimport nope from \"lib/nope\"\n\n" +
			"pub func F() int64 {\n    return nope.G() + 1\n}\n",
		"b/b.tck": "package b\n\npub func H() string {\n    return 1\n}\n",
	})
	for _, args := range [][]string{{"--check", dir}, {"--check", "--emit", dir}} {
		r := runTack(t, args...)
		assertDiagnostics(t, r)
		if !strings.Contains(r.stderr, "cannot find package lib/nope") ||
			!strings.Contains(r.stderr, "b.tck:4:12: error:") {
			t.Errorf("%q: want the import error and the type error:\n%s", args, r.stderr)
		}
		if got := strings.Count(r.stderr, ": error:"); got != 2 {
			t.Errorf("%q: %d errors, want 2:\n%s", args, got, r.stderr)
		}
	}
}

// TestCheckLibraryWithSyntaxError checks a syntax error in a library's
// first file leaves the package a library: --check reports the error and
// goes on to check the package, reporting a type error after it, and
// never calls the package not a main package. A lexical error is reported
// the same way, without that message.
func TestCheckLibraryWithSyntaxError(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"stray name in a body", "    l\n"},
		{"stray func between functions", "}\n\nfunc\n\nfunc Other() {\n"},
		{"lexical error", "    @\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeDir(t, withFiles(libModule, map[string]string{
				"geom/geom.tck": "package geom\n\n" +
					"pub func Area(w int64, h int64) int64 {\n" + tc.line +
					"    return w * h\n}\n\n" +
					"pub func Wrong() int64 {\n    return \"w\"\n}\n",
			}))
			t.Chdir(root)
			r := runTack(t, "--check", "geom")
			assertDiagnostics(t, r)
			if strings.Contains(r.stderr, "not a main package") {
				t.Errorf("a library with an error was called not a main package:\n%s", r.stderr)
			}
			typeErr := "cannot use string as int64 in return from Wrong"
			if tc.name != "lexical error" && !strings.Contains(r.stderr, typeErr) {
				t.Errorf("the package was not checked past its syntax error:\n%s", r.stderr)
			}
		})
	}
}

// TestCheckStd checks --check on the standard library itself: the
// installed std, TACK_HOME's, and a copy of it elsewhere, a checkout
// being worked on, which is a module named std whose imports of std
// resolve in the copy. Both check clean, std/builtin, which only documents
// the builtins, being skipped as no package.
func TestCheckStd(t *testing.T) {
	assertCheckClean(t, runTack(t, "--check", stdtest.Dir()))
	copied := filepath.Join(copyStd(t), "src", "std")
	assertCheckClean(t, runTack(t, "--check", copied))

	// An error in the copy is reported in the copy's file, so the copy's
	// packages, not the installed ones, are the ones checked.
	appendTo(t, filepath.Join(copied, "errors", "errors.tck"),
		"\nfunc broken() int64 {\n    return \"b\"\n}\n")
	r := runTack(t, "--check", copied)
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, filepath.Join(copied, "errors", "errors.tck")) &&
		!strings.Contains(r.stderr, "errors/errors.tck") {
		t.Errorf("the copy's error was not reported:\n%s", r.stderr)
	}
}

// TestCheckPackagesNamedBuiltinAndStd checks builtin and std are package
// names like any other: a module's packages may have them, and a package
// named builtin declares and calls its own functions beside the builtins.
func TestCheckPackagesNamedBuiltinAndStd(t *testing.T) {
	root := writeDir(t, map[string]string{
		"tack.mod": "module calc\n",
		"builtin/b.tck": "package builtin\n\n" +
			"pub func Twice(n int64) int64 {\n    print(\"twice\\n\")\n    return helper(n) * 2\n}\n\n" +
			"func helper(n int64) int64 {\n    return n + len(\"\")\n}\n",
		"std/s.tck": "package std\n\npub func Three() int64 {\n    return 3\n}\n",
		"main.tck": "package main\n\n" +
			"import b from \"calc/builtin\"\nimport s from \"calc/std\"\n\n" +
			"func main() {\n    if b.Twice(s.Three()) != 6 {\n        exit(1)\n    }\n}\n",
	})
	t.Chdir(root)
	assertCheckClean(t, runTack(t, "--check", "."))
}

// TestCheckBuiltinDoc checks --check on std/builtin/builtin.tck, the copy
// of the builtins' documentation, which no parser but the compiler's own
// builtin mode reads: named as a file or by its directory, in the
// installed std or a checkout of it, it is clean when it matches the
// builtins built into the compiler, and one error saying how to refresh it
// when it does not. Another .tck file anywhere is checked as code, a file
// named builtin.tck outside std among them.
func TestCheckBuiltinDoc(t *testing.T) {
	installed := filepath.Join(stdtest.Dir(), "builtin")
	copied := filepath.Join(copyStd(t), "src", "std", "builtin")
	for _, dir := range []string{installed, copied} {
		assertCheckClean(t, runTack(t, "--check", filepath.Join(dir, "builtin.tck")))
		assertCheckClean(t, runTack(t, "--check", dir))
	}

	appendTo(t, filepath.Join(copied, "builtin.tck"), "\n// edited\n")
	for _, target := range []string{filepath.Join(copied, "builtin.tck"), copied} {
		r := runTack(t, "--check", target)
		if r.code != exitFailure || !strings.Contains(r.stderr, "make std-copies") {
			t.Errorf("--check %s on a stale copy: exit %d, stderr %q", target, r.code, r.stderr)
		}
	}

	elsewhere := writeDir(t, map[string]string{
		"tack.mod":            "module calc\n",
		"builtin/builtin.tck": "package builtin\n\ntype int64 int64\n",
	})
	r := runTack(t, "--check", filepath.Join(elsewhere, "builtin", "builtin.tck"))
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "only struct, interface and union types can be declared") {
		t.Errorf("a builtin.tck outside std was not checked as code:\n%s", r.stderr)
	}
}

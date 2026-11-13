package compilertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/testmain"
	"github.com/tacklang/tack/typeck"
)

// runTestPipeline mirrors tack test's compileTests (tack/test.go): p's
// directory is loaded for its tests with the main tack test generates
// for them, every test run, every stage runs whatever an earlier one
// reported, what is reported in the generated main is dropped or made
// an internal error, and code is generated only when no error is left.
// The stage names the stages that reported errors, as runPipeline's do.
func runTestPipeline(backendName string, p program) result {
	backend, ok := codegen.Get(backendName)
	if !ok {
		return result{stage: "codegen", output: "no backend registered under " + backendName}
	}
	l := p.loader()
	l.TestMain = func(pkg *ast.Package, files source.Files) (string, []byte) {
		return testmain.FileName, testmain.Source(pkg, files, testmain.Options{Pkg: pkg.Path})
	}
	res, err := l.Dir(p.target)
	if err != nil {
		return result{stage: "load", output: err.Error() + "\n"}
	}
	diags := append([]diag.Diagnostic(nil), res.Diags...)
	var stages []string
	if diag.HasErrors(res.Diags) {
		stages = append(stages, string(res.Stage))
	}
	prog := res.Program
	var pkg *ast.Package
	if prog != nil {
		pkg = prog.Main()
	}
	if clashes := testmain.Clashes(pkg); len(clashes) > 0 {
		return result{stage: "load", output: renderSorted(res.Files, append(diags, clashes...))}
	}
	if prog != nil {
		prog.Entry = testmain.Entry(prog)
		missing := resolver.MissingMain(prog)
		diags = append(diags, missing...)
		r := resolver.New(prog, stdtest.MustBuiltins())
		r.MainOptional = true
		d := append(missing, r.Resolve()...)
		if diags = append(diags, d[len(missing):]...); diag.HasErrors(d) {
			stages = append(stages, "resolve")
		}
		c := typeck.New(prog, res.Files, stdtest.MustBuiltins())
		c.Tests = true
		d = c.Check()
		if diags = append(diags, d...); diag.HasErrors(d) {
			stages = append(stages, "typeck")
		}
	}
	diags = testmain.Diagnostics(diags, pkg)
	if diag.HasErrors(diags) {
		return result{stage: strings.Join(stages, "+"), output: renderSorted(res.Files, diags)}
	}
	out, genDiags := backend.Generate(prog, stdtest.MustBuiltins())
	if len(genDiags) > 0 {
		return result{stage: "codegen", output: diag.RenderAllPretty(res.Files, genDiags)}
	}
	r := result{stage: "ok", output: string(out)}
	if len(diags) > 0 {
		r.warnings = renderSorted(res.Files, diags)
	}
	return r
}

// TestInvalidTests runs every case of examples/invalid-tests.txtar as tack
// test builds it, one subtest per case, and compares the output with the
// case's expected section, or rewrites that section under -update, as
// TestInvalidPackages does for a build. Every case must be rejected.
func TestInvalidTests(t *testing.T) {
	path := filepath.FromSlash(invalid.TestsPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	comment, cases, err := invalid.ParseTree(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s holds no cases", path)
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.Name, func(t *testing.T) {
			got := runTestPipeline(frontEndBackend, treeProgram(*c))
			if got.stage == "ok" {
				t.Fatalf("the tests compiled; an invalid example must be rejected")
			}
			if *update {
				c.Expected = []byte(got.String())
				return
			}
			if got.String() != string(c.Expected) {
				t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s",
					path, c.Expected, got)
			}
		})
	}
	if *update {
		writeTree(t, path, data, comment, cases)
	}
}

// TestTestModeGolden pins the C tack test generates for the tests of
// examples/tack-test/fail, whose tests fail every way a run goes on
// past: the generated main, the entry C's main calls, and the store of a
// ?'s place in a test before it returns, which no build has. Like
// TestGolden it is a regression net; the driver's tests run the binary.
func TestTestModeGolden(t *testing.T) {
	files, _ := readTree(t, filepath.Join(examplesDir, "tack-test"))
	p := program{files: fstest.MapFS(files), target: "examples/tack-test/fail"}
	got := runTestPipeline(frontEndBackend, p)
	if got.stage != "ok" || got.warnings != "" {
		t.Fatalf("tack test's build of fail: %s\nwarnings:\n%s", got, got.warnings)
	}
	checkGolden(t, filepath.Join("testdata", "tack-test_fail.test.c.golden"), got.String())
}

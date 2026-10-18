package compilertest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
)

// checkRefused runs the whole front end on p, as the driver does, every
// stage whatever an earlier one reported, and then hands whatever program
// the loader built straight to the backend, as a caller that forgot to
// look at the diagnostics would. It fails t when the front end reported
// an error and the backend generated code anyway, or when the program's
// count of its errors is not the number of errors reported, warnings
// aside. It returns the diagnostics reported.
func checkRefused(t *testing.T, p program) []diag.Diagnostic {
	t.Helper()
	l := p.loader()
	var res *loader.Result
	var err error
	if p.single {
		res, err = l.File(p.target)
	} else {
		res, err = l.Dir(p.target)
	}
	if err != nil {
		return nil
	}
	diags, _ := checkAll(res)
	prog := res.Program
	if prog == nil {
		if len(diags) == 0 {
			t.Fatalf("the loader built no program and reported nothing")
		}
		return diags
	}
	errs := diag.ErrorCount(diags)
	if prog.Errors != errs {
		t.Errorf("the program counts %d errors; %d were reported", prog.Errors, errs)
	}
	backend, _ := codegen.Get(frontEndBackend)
	out, refused := backend.Generate(prog, stdtest.MustBuiltins())
	if errs > 0 && (out != nil || len(refused) == 0) {
		t.Errorf("code was generated from a program with %d errors", errs)
	}
	return diags
}

// TestCodegenRefusesEveryInvalidProgram checks, for every case of
// examples/invalid.txtar and examples/invalid-packages.txtar, that the
// backend refuses the program the front end rejected, however far the
// front end got with it: the front end now goes on past errors, so the
// tree it leaves may hold bad nodes and names bound to nothing, and the
// backend's own check (codegen.Refuse) is what keeps them from being
// lowered.
func TestCodegenRefusesEveryInvalidProgram(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(invalid.Path))
	if err != nil {
		t.Fatal(err)
	}
	_, cases, err := invalid.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			// A case the front end accepts is one code generation rejects,
			// which TestInvalid checks.
			checkRefused(t, singleFile(c.Name+"/main.tck", c.Source))
		})
	}

	data, err = os.ReadFile(filepath.FromSlash(invalid.TreePath))
	if err != nil {
		t.Fatal(err)
	}
	_, trees, err := invalid.ParseTree(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range trees {
		t.Run(c.Name, func(t *testing.T) {
			checkRefused(t, treeProgram(c))
		})
	}
}

// FuzzFrontEndAfterErrors compiles random single files through the whole
// front end, which goes on past every error, so that the resolver and
// the type checker see what the parser recovered from damaged source,
// bad nodes and all, and names an earlier error left bound to nothing.
// It checks that no stage panics or hangs, that a tree with a bad node
// came with a syntax error, and that the backend refuses every program
// with an error (see checkRefused). The seeds are every invalid example
// and every example that compiles with warnings.
func FuzzFrontEndAfterErrors(f *testing.F) {
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(filepath.FromSlash(path))
		if err != nil {
			f.Fatal(err)
		}
		for _, c := range cases {
			f.Add(c.Source)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		done := make(chan string, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Sprintf("panic: %v", r)
				}
			}()
			p := singleFile("main.tck", src)
			diags := checkRefused(t, p)
			if hasBadNode(t, p) && !diag.HasErrors(diags) {
				done <- "a tree with a bad node came with no error"
				return
			}
			done <- ""
		}()
		select {
		case msg := <-done:
			if msg != "" {
				t.Fatal(msg)
			}
		case <-time.After(caseTimeout):
			t.Fatalf("the front end did not finish within %s", caseTimeout)
		}
	})
}

// hasBadNode reports whether the tree the loader builds for p holds a bad
// declaration or a bad statement.
func hasBadNode(t *testing.T, p program) bool {
	t.Helper()
	res, err := p.loader().File(p.target)
	if err != nil || res.Program == nil {
		return false
	}
	for _, f := range res.Program.Files() {
		for _, d := range f.Decls {
			switch x := d.(type) {
			case *ast.BadDecl:
				return true
			case *ast.Func:
				if len(ast.BadStmts(x.Body)) > 0 {
					return true
				}
			}
		}
	}
	return false
}

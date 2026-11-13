package typeck_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// TestSiteArgumentPassedOnce checks examples/call-site with every body
// checked once and with every body checked twice, and requires each time
// that no error is reported and that every call given a testing.Site
// argument holds it exactly once among its arguments, with one argument
// for each of its callee's parameters, its receiver aside: a second check
// of a call takes out the argument the first gave it before passing it
// again.
func TestSiteArgumentPassedOnce(t *testing.T) {
	for _, twice := range []bool{false, true} {
		l := &loader.Loader{
			FS:       loader.FromFS(os.DirFS(stdtest.Home())),
			Builtins: stdtest.MustBuiltins(),
			Std:      stdtest.Std(),
		}
		res, err := l.Dir("examples/call-site")
		if err != nil {
			t.Fatal(err)
		}
		if diags := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); diag.HasErrors(diags) {
			t.Fatalf("resolve: %s", diag.RenderAllPretty(res.Files, diags))
		}
		c := typeck.New(res.Program, res.Files, stdtest.MustBuiltins())
		if twice {
			c.CheckEveryBodyTwice()
		}
		if diags := c.Check(); len(diags) > 0 {
			t.Fatalf("twice %v: %s", twice, diag.RenderAllPretty(res.Files, diags))
		}
		sites := 0
		for _, f := range res.Program.Files() {
			ast.Walk(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.Call); ok && call.Site != nil {
					sites++
					if problem := siteProblem(call); problem != "" {
						t.Errorf("twice %v: %s", twice, problem)
					}
				}
				return true
			})
		}
		if sites == 0 {
			t.Errorf("twice %v: no call was given a Site", twice)
		}
	}
}

// siteProblem returns what is wrong with call, a call given a Site
// argument, or "": its Site must be among its arguments exactly once,
// and it must have one argument for each of its callee's parameters, its
// receiver aside.
func siteProblem(call *ast.Call) string {
	held := 0
	for _, arg := range call.Args {
		if arg == call.Site {
			held++
		}
	}
	want := len(ast.Callee(call).Params)
	if call.Receiver() != nil {
		want--
	}
	if held != 1 || len(call.Args) != want {
		return fmt.Sprintf("call at %d holds its Site %d times among %d arguments, want once among %d",
			call.Span.Start, held, len(call.Args), want)
	}
	return ""
}

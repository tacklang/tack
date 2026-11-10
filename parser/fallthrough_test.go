package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// fallthroughSwitch is a switch whose fallthroughs stand in each place
// one can be written: last in a case before another, last in the last
// case, before another statement, and inside an if in a case.
const fallthroughSwitch = `package main

func main() {
    switch n {
    case 1:
        fallthrough
    default:
        g()
        fallthrough
    case 2:
        fallthrough
        g()
    case 3:
        if true {
            fallthrough
        }
    case 4:
        fallthrough
    }
}
`

// TestLinksFallthroughs checks a fallthrough that is the last statement
// of a case other than the last links to the case written after it,
// whatever kind that case is, and that every other one links to nothing,
// for the checker to report.
func TestLinksFallthroughs(t *testing.T) {
	file, diags := parse(t, fallthroughSwitch)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	s := file.Decls[0].(*ast.Func).Body[0].(*ast.Switch)
	next := func(cl *ast.CaseClause, i int) *ast.CaseClause {
		t.Helper()
		f, ok := cl.Body[i].(*ast.Fallthrough)
		if !ok {
			t.Fatalf("statement %d of a case is %T, want a fallthrough", i, cl.Body[i])
		}
		return f.Next
	}
	if got := next(s.Cases[0], 0); got != s.Cases[1] {
		t.Errorf("the first case's fallthrough links to %v, want the default", got)
	}
	if got := next(s.Cases[1], 1); got != s.Cases[2] {
		t.Errorf("the default's fallthrough links to %v, want case 2", got)
	}
	if got := next(s.Cases[2], 0); got != nil {
		t.Errorf("a fallthrough before another statement links to %v, want nothing", got)
	}
	inner := s.Cases[3].Body[0].(*ast.If).Then
	if got := next(&ast.CaseClause{Body: inner}, 0); got != nil {
		t.Errorf("a fallthrough inside an if links to %v, want nothing", got)
	}
	if got := next(s.Cases[4], 0); got != nil {
		t.Errorf("the last case's fallthrough links to %v, want nothing", got)
	}
	if got := ast.FallsInto(s.Cases[0].Body); got != s.Cases[1] {
		t.Errorf("FallsInto gives %v, want the default", got)
	}
	if got := ast.FallsInto(s.Cases[2].Body); got != nil {
		t.Errorf("FallsInto of a body ending in a call gives %v, want nothing", got)
	}
}

// TestFallthroughIsAKeyword checks fallthrough is never a name: not a
// variable's, a parameter's or a function's, and not a value.
func TestFallthroughIsAKeyword(t *testing.T) {
	for _, src := range []string{
		"package main\n\nfunc main() {\n    fallthrough := 1\n}\n",
		"package main\n\nfunc f(fallthrough int64) {\n}\n",
		"package main\n\nfunc fallthrough() {\n}\n",
		"package main\n\nfunc main() {\n    print(fallthrough)\n}\n",
	} {
		if _, diags := parse(t, src); len(diags) == 0 {
			t.Errorf("%q parsed without error", src)
		}
	}
}

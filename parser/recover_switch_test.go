package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// clauseWant is what a clause of a recovered switch must be: a bad clause
// (see ast.CaseClause), a default, or a case with values values; and
// body statements after its header, none of them bad.
type clauseWant struct {
	values, body int
	bad, def     bool
}

// bad, cased and def build a clauseWant for a bad clause, a case and a
// default, each with body statements.
func bad(body int) clauseWant { return clauseWant{bad: true, body: body} }

func cased(values, body int) clauseWant { return clauseWant{values: values, body: body} }

func def(body int) clauseWant { return clauseWant{def: true, body: body} }

// switchCases are switches with a damaged clause header: a line marked //E
// gets exactly one error and every other line none, and the first switch
// in main stays a switch, with the clauses clauses says.
var switchCases = []struct {
	name, src string
	clauses   []clauseWant
}{
	{"a first case edited to type", `package main

func main() {
    switch 1 {
    type 1: //E
        a()
    case 2:
        b()
        c()
    default:
        d()
    }
}
`, []clauseWant{bad(1), cased(1, 2), def(1)}},
	{"a first case's keyword mistyped", `package main

func main() {
    switch x {
    cas 1, 2: //E
        a()
    case 3:
        b()
    }
}
`, []clauseWant{bad(1), cased(1, 1)}},
	{"a case's value cut off", `package main

func main() {
    switch 1 {
    case 1:
        a()
    case 1 +: //E
        b()
    case 3, 4:
        c()
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(2, 1)}},
	{"a case with no value", `package main

func main() {
    switch 1 {
    case 1:
        a()
    case : //E
        b()
    case 3:
        c()
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(1, 1)}},
	{"a case's colon left out", `package main

func main() {
    switch 1 {
    case 1:
        a()
    case 2 //E
        b()
    case 3:
        c()
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(1, 1)}},
	{"a default's colon left out", `package main

func main() {
    switch 1 {
    case 1:
        a()
    default
        b() //E
    case 3:
        c()
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(1, 1)}},
	{"a second default", `package main

func main() {
    switch 1 {
    default:
        a()
    default: //E
        b()
        c()
    case 3:
        d()
    }
}
`, []clauseWant{def(1), bad(2), cased(1, 1)}},
	{"a damaged last case", `package main

func main() {
    switch 1 {
    case 1:
        a()
    case (2: //E
        b()
    }
    after()
}
`, []clauseWant{cased(1, 1), bad(1)}},
	{"a case's keyword mistyped in the middle", `package main

func main() {
    switch 1 {
    case 1:
        a()
    csae 2: //E
        b()
    case 3:
        c()
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(1, 1)}},
	{"a default's keyword mistyped", `package main

func main() {
    switch 1 {
    case 1:
        a()
    dfault: //E
        b()
    }
}
`, []clauseWant{cased(1, 1), bad(1)}},
	{"a type switch's case type cut off", `package main

func main() {
    switch v := y.(type) {
    case int64:
        a(v)
    case [: //E
        b(v)
    case string:
        c(v)
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(1, 1)}},
	{"a type switch's case edited to type", `package main

func main() {
    switch v := y.(type) {
    case int64:
        a(v)
    type string: //E
        b(v)
        c(v)
    default:
        d()
    }
}
`, []clauseWant{cased(1, 1), bad(2), def(1)}},
	{"a tagless switch's damaged case keeps its blocks", `package main

func main() {
    switch {
    case a > 1 +: //E
        if b {
            c()
        }
        for {
            d()
        }
    case a < 0:
        e()
    }
}
`, []clauseWant{bad(2), cased(1, 1)}},
	{"a case value's parenthesis left open", `package main

func main() {
    switch 1 {
    case f(1: //E
        a()
    case 2:
        b()
    }
}
`, []clauseWant{bad(1), cased(1, 1)}},
	{"a case's values split over a damaged second line", `package main

func main() {
    switch 1 {
    case 1,
        2 3: //E
        a()
    case 4:
        b()
    }
}
`, []clauseWant{bad(1), cased(1, 1)}},
	{"a statement where the first case goes", `package main

func main() {
    switch 1 {
        a() //E
    case 2:
        b()
    }
}
`, []clauseWant{bad(0), cased(1, 1)}},
	{"two damaged clauses", `package main

func main() {
    switch 1 {
    type 1: //E
        a()
    case 2:
        b()
    case 3 4: //E
        c()
    default:
        d()
        e()
    }
}
`, []clauseWant{bad(1), cased(1, 1), bad(1), def(2)}},
	{"a damaged clause of a switch in a clause", `package main

func main() {
    switch 1 {
    case 1:
        switch 2 {
        case 2 +: //E
            a()
        case 3:
            b()
        }
    case 2:
        c()
    }
}
`, []clauseWant{cased(1, 1), cased(1, 1)}},
}

// TestDamagedClausesKeepTheSwitch checks each of switchCases: the damaged
// clause is a bad clause in the switch, and every other clause is in it,
// parsed as it would be with no damage.
func TestDamagedClausesKeepTheSwitch(t *testing.T) {
	for _, tc := range switchCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, diags := parse(t, tc.src)
			checkErrorLines(t, tc.src, diags)
			if tree == nil {
				t.Fatal("no tree")
			}
			s := mainSwitch(t, tree)
			if s == nil {
				t.Fatal("the switch is not in main's body")
			}
			if len(s.Cases) != len(tc.clauses) {
				t.Fatalf("%d clauses, want %d", len(s.Cases), len(tc.clauses))
			}
			for i, cl := range s.Cases {
				checkClause(t, i, cl, tc.clauses[i])
			}
		})
	}
}

// mainSwitch returns the first statement of main's body in tree that is
// a switch, or nil.
func mainSwitch(t *testing.T, tree *ast.File) *ast.Switch {
	t.Helper()
	for _, d := range tree.Decls {
		fn, ok := d.(*ast.Func)
		if !ok || fn.Name != "main" {
			continue
		}
		for _, stmt := range fn.Body {
			if s, ok := stmt.(*ast.Switch); ok {
				return s
			}
		}
	}
	return nil
}

// checkClause checks cl, the i'th clause of a recovered switch, is what
// want says, with no bad statement in its body but a bad clause's header
// (a switch in it may have bad clauses of its own). A bad clause has no values, is no default, and starts its
// body with the bad statement standing for its header, which spans what
// the clause does.
func checkClause(t *testing.T, i int, cl *ast.CaseClause, want clauseWant) {
	t.Helper()
	body := cl.Body
	if cl.Bad {
		if len(body) == 0 {
			t.Fatalf("clause %d is bad with no bad statement for its header", i)
		}
		hdr, ok := body[0].(*ast.BadStmt)
		if !ok || hdr.Span != cl.Span {
			t.Errorf("clause %d is bad, but its body starts with %T, not its header", i, body[0])
		}
		if len(cl.Values) > 0 || cl.Default {
			t.Errorf("clause %d is bad with values or as a default", i)
		}
		body = body[1:]
	}
	got := clauseWant{values: len(cl.Values), body: len(body), bad: cl.Bad, def: cl.Default}
	if got != want {
		t.Errorf("clause %d is %+v, want %+v", i, got, want)
	}
	for _, stmt := range body {
		if _, isBad := stmt.(*ast.BadStmt); isBad {
			t.Errorf("clause %d holds a bad statement in its body", i)
		}
	}
}

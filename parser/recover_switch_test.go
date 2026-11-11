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

// bad builds a clauseWant for a bad clause with body statements.
func bad(body int) clauseWant { return clauseWant{bad: true, body: body} }

// cased builds a clauseWant for a case with values values and body
// statements.
func cased(values, body int) clauseWant { return clauseWant{values: values, body: body} }

// def builds a clauseWant for a default with body statements.
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
	{"a damaged header after a statement and a ;", `package main

func main() {
    switch x {
    case 1:
        a(); csae 2: //E
        b()
    case 3:
        c()
    }
}
`, []clauseWant{cased(1, 0), bad(1), cased(1, 1)}},
	{"a case's colon left out after a statement and a ;", `package main

func main() {
    switch x {
    case 1:
        a(); case 2 //E
        b()
    case 3:
        c()
    }
}
`, []clauseWant{cased(1, 1), bad(1), cased(1, 1)}},
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

// statementBeforeCaseErrors are switches where a clause's statement,
// on the line of the next case, fails: a line marked //E gets exactly
// one error and every other line none, and the switch keeps every
// clause, the failing one holding a bad statement for it and kinds
// saying, clause by clause, whether each is a default.
var statementBeforeCaseErrors = []struct {
	name, src string
	kinds     []bool
}{
	{"a statement's ; left out before a case on its line", `package main

func main() {
    switch x {
    case 1:
        a() case 2: //E
        b()
    case 3:
        c()
    }
}
`, []bool{false, false, false}},
	{"a stray ; before a case on a statement's line", `package main

func main() {
    switch x {
    default:
        a(); ; case 2: //E
        b()
    case 3:
        c()
    }
}
`, []bool{true, false, false}},
}

// TestStatementErrorsBeforeACaseKeepTheClause checks each of
// statementBeforeCaseErrors: the error is the statement's, its clause
// ends at the case on its line, which starts a clause of its own, and
// the clauses after it parse as they would with no damage.
func TestStatementErrorsBeforeACaseKeepTheClause(t *testing.T) {
	for _, tc := range statementBeforeCaseErrors {
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
			if len(s.Cases) != len(tc.kinds) {
				t.Fatalf("%d clauses, want %d", len(s.Cases), len(tc.kinds))
			}
			for i, cl := range s.Cases {
				if cl.Bad || cl.Default != tc.kinds[i] {
					t.Errorf("clause %d is bad %v, default %v; want a good clause, default %v",
						i, cl.Bad, cl.Default, tc.kinds[i])
				}
			}
			if !hasBadStmt(s.Cases[0].Body) {
				t.Errorf("the first clause holds no bad statement for its failed one")
			}
			for i, cl := range s.Cases[1:] {
				if hasBadStmt(cl.Body) || len(cl.Body) != 1 {
					t.Errorf("clause %d holds %d statements, some bad; want one good one", i+1, len(cl.Body))
				}
			}
		})
	}
}

// hasBadStmt reports whether any of body is a bad statement.
func hasBadStmt(body []ast.Stmt) bool {
	for _, stmt := range body {
		if _, ok := stmt.(*ast.BadStmt); ok {
			return true
		}
	}
	return false
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

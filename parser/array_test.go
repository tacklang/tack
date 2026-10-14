package parser_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// typeShape renders a type as written, nesting included.
func typeShape(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.ArrayType:
		n := "..."
		if x.Len != nil {
			n = shape(t, x.Len)
		}
		return "[" + n + "]" + typeShape(t, x.Elem)
	default:
		t.Fatalf("unexpected type expression %T", e)
		return ""
	}
}

// TestParsesArrayTypes checks array types wherever a type is written: a
// variable, a parameter, and a result, nested to any depth.
func TestParsesArrayTypes(t *testing.T) {
	tests := []struct{ src, want string }{
		{"package main\n\nfunc main() {\n    mut a [3]int64\n}\n", "[3]int64"},
		{"package main\n\nfunc main() {\n    mut a [2][3]bool\n}\n", "[2][3]bool"},
		{"package main\n\nfunc main() {\n    mut a [n + 1]string\n}\n", "[(n + 1)]string"},
		{"package main\n\nvar a [4]uint8\n\nfunc main() {\n}\n", "[4]uint8"},
	}
	for _, tt := range tests {
		file, diags := parse(t, tt.src)
		if len(diags) > 0 {
			t.Fatalf("%q: %+v", tt.src, diags)
		}
		var typ ast.Expr
		switch d := file.Decls[0].(type) {
		case *ast.Var:
			typ = d.TypeExpr
		case *ast.Func:
			typ = d.Body[0].(*ast.Var).TypeExpr
		}
		if got := typeShape(t, typ); got != tt.want {
			t.Errorf("type = %s, want %s", got, tt.want)
		}
	}

	file, diags := parse(t, "package main\n\nfunc f(v [3]int64) [2][2]bool {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("%+v", diags)
	}
	fn := file.Decls[0].(*ast.Func)
	if got := typeShape(t, fn.Params[0].TypeExpr); got != "[3]int64" {
		t.Errorf("parameter type = %s", got)
	}
	if got := typeShape(t, fn.Results[0]); got != "[2][2]bool" {
		t.Errorf("result type = %s", got)
	}
}

// litShape renders an array literal's elements, nested ones in braces.
func litShape(t *testing.T, lit *ast.CompositeLit) string {
	t.Helper()
	parts := make([]string, len(lit.Elems))
	for i, e := range lit.Elems {
		if inner, ok := e.(*ast.CompositeLit); ok {
			parts[i] = litShape(t, inner)
		} else {
			parts[i] = shape(t, e)
		}
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// TestParsesArrayLiterals checks the elements of each literal form: listed,
// empty, over several lines with a comma after the last, nested with the
// inner types left out, and with [...] for the length.
func TestParsesArrayLiterals(t *testing.T) {
	tests := []struct{ expr, typ, elems string }{
		{"[3]int64{1, 2, 3}", "[3]int64", "{1 2 3}"},
		{"[0]bool{}", "[0]bool", "{}"},
		{"[2]int64{\n        1,\n        2,\n    }", "[2]int64", "{1 2}"},
		{"[2][2]bool{{true, false}, {false, true}}", "[2][2]bool", "{{true false} {false true}}"},
		{"[...]int64{a + 1, f()}", "[...]int64", "{(a + 1) f()}"},
		{"[2][1]int64{\n        {1},\n        {2},\n    }", "[2][1]int64", "{{1} {2}}"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			lit, ok := declared(t, tt.expr).(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%q is not a literal", tt.expr)
			}
			if got := typeShape(t, lit.TypeExpr); got != tt.typ {
				t.Errorf("type = %s, want %s", got, tt.typ)
			}
			if got := litShape(t, lit); got != tt.elems {
				t.Errorf("elements = %s, want %s", got, tt.elems)
			}
		})
	}
}

// indexShape renders an expression with each index as name[index].
func indexShape(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.Index:
		return indexShape(t, x.X) + "[" + indexShape(t, x.Index) + "]"
	case *ast.CompositeLit:
		return "lit"
	case *ast.StrLit:
		return strconv.Quote(x.Value)
	case *ast.Unary:
		return "(" + x.Op.String() + indexShape(t, x.X) + ")"
	case *ast.Binary:
		return "(" + indexShape(t, x.X) + " " + x.Op.String() + " " + indexShape(t, x.Y) + ")"
	default:
		return shape(t, e)
	}
}

// TestParsesIndexes checks an index binds tighter than any operator, chains,
// and follows a name, a call, a parenthesized expression or a literal.
func TestParsesIndexes(t *testing.T) {
	tests := []struct{ expr, want string }{
		{"a[0]", "a[0]"},
		{"a[i][j + 1]", "a[i][(j + 1)]"},
		{"f()[2]", "f()[2]"},
		{"(a)[1]", "a[1]"},
		{"[2]int64{1, 2}[1]", "lit[1]"},
		{"-a[0]", "(-a[0])"},
		{"!b[0]", "(!b[0])"},
		{"a[0] + b[1] * c[2]", "(a[0] + (b[1] * c[2]))"},
		{"a[b[c[0]]]", "a[b[c[0]]]"},
		{`"tack"[1]`, `"tack"[1]`},
		{`"tack"[i] == 'a'`, `("tack"[i] == 'a')`},
		{`""[0]`, `""[0]`},
		{"s[len(s) - 1]", "s[(len() - 1)]"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := indexShape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s parsed as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// TestParsesElementTargets checks an element can be what an assignment,
// a compound assignment, an increment or an assignment of several values
// writes.
func TestParsesElementTargets(t *testing.T) {
	body := mainBody(
		t,
		"    a[0] = 1\n    a[i][j] += 2\n    a[1]++\n    a[0], b = f()\n    b, a[1] = f()",
	)
	if got := indexShape(t, body[0].(*ast.Assign).Target); got != "a[0]" {
		t.Errorf("assignment target = %s", got)
	}
	if a := body[1].(*ast.Assign); indexShape(t, a.Target) != "a[i][j]" || a.Op != ast.Add {
		t.Errorf("compound target = %s, op %v", indexShape(t, a.Target), a.Op)
	}
	if got := indexShape(t, body[2].(*ast.IncDec).Target); got != "a[1]" {
		t.Errorf("increment target = %s", got)
	}
	for i, want := range []string{"a[0] b", "b a[1]"} {
		m := body[3+i].(*ast.MultiAssign)
		got := indexShape(t, m.Targets[0]) + " " + indexShape(t, m.Targets[1])
		if got != want {
			t.Errorf("targets = %s, want %s", got, want)
		}
	}
}

// A loop header tells an element target, in its init or post, from a
// condition that starts with an element.
func TestParsesElementTargetsInLoopHeaders(t *testing.T) {
	loops := mainBody(t, "    for a[0] < 3 {\n    }\n    for a[0] = 0; a[0] < 3; a[0]++ {\n    }")
	if l := loops[0].(*ast.For); l.Init != nil || l.Cond == nil {
		t.Error("for a[0] < 3 was not read as a condition")
	}
	if l := loops[1].(*ast.For); l.Init == nil || l.Post == nil {
		t.Error("an element target in a loop header was not read as init and post")
	}
}

// TestRejectsMalformedArrays checks each malformed array form is a parse
// error, including a view with no element type and declaring an element.
func TestRejectsMalformedArrays(t *testing.T) {
	tests := []struct{ stmt, want string }{
		{"mut a []", "expected 'type name' found ';'"},
		{"x := []int64", "expected '{' found ';'"},
		{"x := [](s)", "expected 'type name' found '('"},
		{"mut a [...]int64", "[...] can only be used in an array literal"},
		{"x := [3]int64{1 2}", "expected ',' found '2'"},
		{"x := [3]int64{,}", "expected 'value' found ','"},
		{"x := [3]{1}", "expected 'type name' found '{'"},
		{"x := [3]int64", "expected '{' found ';'"},
		{"x := a[", "expected 'value' found '}'"},
		{"x := a[]", "expected 'value' found ']'"},
		{"x := a[1", "expected ']' found ';'"},
		{"a[0] := 1", "expected '=' found ':='"},
		{"x, a[0] := f()", "only a name can be declared; use = to assign to an element"},
		{"mut a[0], b = f()", "cannot use mut in an assignment"},
		{"x := {1, 2}", "expected 'value' found '{'"},
	}
	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.stmt+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.stmt)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

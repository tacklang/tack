package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// assertShape renders a type assertion as its operand, then .(T), with T
// as typeShape renders it, or .(type).
func assertShape(t *testing.T, a *ast.TypeAssert) string {
	t.Helper()
	typ := "type"
	if a.TypeExpr != nil {
		typ = anyTypeShape(t, a.TypeExpr)
	}
	return structShape(t, a.X) + ".(" + typ + ")"
}

// anyTypeShape renders a type as written, a view's and a list's included,
// and a qualified name with its package.
func anyTypeShape(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.ViewType:
		return "[]" + anyTypeShape(t, x.Elem)
	case *ast.ListType:
		return "list[" + anyTypeShape(t, x.Elem) + "]"
	case *ast.Ident:
		return x.Qualified()
	default:
		return typeShape(t, e)
	}
}

// TestParsesTypeAssertions checks x.(T) after any operand that a field
// can follow, with every kind of type, and x.(type), and that indexes and
// fields can follow one.
func TestParsesTypeAssertions(t *testing.T) {
	tests := []struct{ src, want string }{
		{"v, ok := a.(int64)", "a.(int64)"},
		{"v, ok := a.(string)", "a.(string)"},
		{"v, ok := args[0].(bool)", "args[0].(bool)"},
		{"v, ok := p.x.(uint8)", "p.x.(uint8)"},
		{"v, ok := (a).(int8)", "a.(int8)"},
		{"v, ok := a.([3]int64)", "a.([3]int64)"},
		{"v, ok := a.([]int64)", "a.([]int64)"},
		{"v, ok := a.(list[int64])", "a.(list[int64])"},
		{"v, ok := a.(Point)", "a.(Point)"},
		{"v, ok := a.(any)", "a.(any)"},
		{"v, ok := a.(type)", "a.(type)"},
		{"v, ok := a.(int64).x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			body := mainBody(t, "    "+tt.src)
			value := body[0].(*ast.MultiVar).Value
			if tt.want == "" {
				sel, ok := value.(*ast.Selector)
				if !ok {
					t.Fatalf("value is %T, want a field of an assertion", value)
				}
				value = sel.X
			}
			a, ok := value.(*ast.TypeAssert)
			if !ok {
				t.Fatalf("value is %T, want *ast.TypeAssert", value)
			}
			if got := assertShape(t, a); tt.want != "" && got != tt.want {
				t.Errorf("shape = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTypeAssertionSpans checks an assertion spans its operand, its
// parentheses included, through its closing parenthesis.
func TestTypeAssertionSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    v, ok := (a).(int64)\n    switch b.(type) {\n    }\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	body := file.Decls[0].(*ast.Func).Body
	a := body[0].(*ast.MultiVar).Value
	if got := src[a.Spanned().Start:a.Spanned().End]; got != "(a).(int64)" {
		t.Errorf("assertion spans %q", got)
	}
	tag := body[1].(*ast.Switch).Tag
	if got := src[tag.Spanned().Start:tag.Spanned().End]; got != "b.(type)" {
		t.Errorf("tag spans %q", got)
	}
}

// TestParsesTypeSwitches checks a type switch's binding, its case types,
// and the variable each case declares, named after the binding.
func TestParsesTypeSwitches(t *testing.T) {
	src := "    switch v := a.(type) {\n    case int64, []string:\n        f()\n" +
		"    case pkg.T, [2]bool, list[int8]:\n    default:\n    }"
	s := firstSwitch(t, src)
	if !s.IsTypeSwitch() || s.Bind == nil || s.Bind.Name != "v" {
		t.Fatalf("switch is not a type switch binding v: %+v", s)
	}
	want := []string{"int64,[]string", "pkg.T,[2]bool,list[int8]", ""}
	for i, cl := range s.Cases {
		if got := caseTypes(t, cl); got != want[i] {
			t.Errorf("case %d types = %q, want %q", i, got, want[i])
		}
		checkCaseVar(t, i, cl, s.Bind)
	}
}

// caseTypes renders the types a case names, joined by commas.
func caseTypes(t *testing.T, cl *ast.CaseClause) string {
	t.Helper()
	got := make([]string, len(cl.Values))
	for i, v := range cl.Values {
		got[i] = anyTypeShape(t, v)
	}
	return strings.Join(got, ",")
}

// checkCaseVar checks case i, cl, of a type switch binding b declares a
// variable of its own, immutable and named and placed as b.
func checkCaseVar(t *testing.T, i int, cl *ast.CaseClause, b *ast.Var) {
	t.Helper()
	switch v := cl.Var; {
	case v == nil:
		t.Errorf("case %d declares no variable", i)
	case v == b || v.Name != b.Name || v.Mutable || v.NameSpan != b.NameSpan:
		t.Errorf("case %d variable = %+v, want an immutable %s of its own", i, v, b.Name)
	}
}

// TestParsesTypeSwitchesWithoutBindings checks a type switch with no
// binding declares no variable in any case, and that a switch on an
// assertion x.(T) is an expression switch, whose case values stay values.
func TestParsesTypeSwitchesWithoutBindings(t *testing.T) {
	s := firstSwitch(t, "    switch a.(type) {\n    case int64:\n    default:\n    }")
	if !s.IsTypeSwitch() || s.Bind != nil {
		t.Fatalf("switch is not a type switch without a binding")
	}
	for i, cl := range s.Cases {
		if cl.Var != nil {
			t.Errorf("case %d declares a variable", i)
		}
	}
	s = firstSwitch(t, "    switch a.(int64) {\n    case x:\n    }")
	if s.IsTypeSwitch() || s.Cases[0].Var != nil {
		t.Errorf("a switch on an assertion is a type switch")
	}
}

// TestParsesAssignmentToAnAssertion checks an assertion followed by = is
// parsed as an assignment's target, which the resolver reports, rather
// than misread as a field.
func TestParsesAssignmentToAnAssertion(t *testing.T) {
	a, ok := mainBody(t, "    p.(x) = 1")[0].(*ast.Assign)
	if !ok {
		t.Fatalf("statement is not an assignment")
	}
	if _, ok := a.Target.(*ast.TypeAssert); !ok {
		t.Errorf("target is %T, want *ast.TypeAssert", a.Target)
	}
}

// malformedTypeAssertions pairs malformed or truncated type assertions and
// type switches with the message each must be reported with.
var malformedTypeAssertions = []struct {
	src  string
	want string
}{
	{"v, ok := a.(", "expected 'type name' found '}'"},
	{"v, ok := a.()", "expected 'type name' found ')'"},
	{"v, ok := a.(int64", "expected ')' found ';'"},
	{"v, ok := a.(type", "expected ')' found '}'"},
	{"v, ok := a.(1)", "expected 'type name' found '1'"},
	{"v, ok := a.(int64 + 1)", "expected ')' found '+'"},
	{"v, ok := a.(struct{})", "anonymous structs are not supported"},
	{"v, ok := a.(...int64)", "can only use ... with the final parameter"},
	{"v, ok := a.(type)(1)", "expected ';' found '('"},
	{"v, ok := .(int64)", "expected 'value' found '.'"},
	{"v, ok := a.(int64)()", "expected ';' found '('"},
	{"switch v := a.(type {\n    }", "expected ')' found '{'"},
	{"a.(int64)", "expected 'statement' found 'a'"},
	{"a.(int64)()", "expected 'statement' found 'a'"},
	{"a.() = 1", "expected 'type name' found ')'"},
	{"switch v := {\n    }", "expected 'value' found '{'"},
	{"switch v := a {\n    }", "a switch has no init statement"},
	{"switch v := a.(int64) {\n    }", "a switch has no init statement"},
	{"switch mut v := a.(type) {\n    }", "cannot use mut on a type switch variable"},
	{"switch v, w := a.(type) {\n    }", "expected '{' found ','"},
	{"switch _ = a.(type) {\n    }", "expected '{' found '='"},
	{"switch a.(type) {\n    case 1:\n    }", "expected 'type name' found '1'"},
	{"switch a.(type) {\n    case int64,:\n    }", "expected 'type name' found ':'"},
	{"switch a.(type) {\n    case f():\n    }", "expected ':' found '('"},
	{"switch a.(type) {\n    case int64\n    }", "expected ':' found ';'"},
	{"switch a.(type) {\n    case struct{}:\n    }", "anonymous structs are not supported"},
}

// TestRejectsMalformedTypeAssertions checks every malformed or truncated
// type assertion and type switch is a parse error with its message.
func TestRejectsMalformedTypeAssertions(t *testing.T) {
	for _, tt := range malformedTypeAssertions {
		t.Run(tt.src, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.src+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

// TestTruncatedTypeSwitchesNeverParse cuts a type switch and an
// assertion off after every byte and checks each prefix is a parse error,
// never a panic or a tree.
func TestTruncatedTypeSwitchesNeverParse(t *testing.T) {
	src := "package main\n\nfunc main() {\n    switch v := a.(type) {\n    case int64, []string:\n" +
		"        w, ok := v.(bool)\n    default:\n        h()\n    }\n}\n"
	end := strings.LastIndex(src, "}")
	for n := strings.Index(src, "switch"); n < end; n++ {
		file, diags := parse(t, src[:n])
		if file != nil || len(diags) == 0 {
			t.Errorf("prefix %q parsed, want an error", src[:n])
		}
	}
}

// TestParsesDeeplyNestedTypeSwitches checks a type switch nested a
// thousand deep, in a case body each time, and a chain of a thousand
// assertions, parse.
func TestParsesDeeplyNestedTypeSwitches(t *testing.T) {
	const depth = 1000
	body := strings.Repeat("switch v := a.(type) {\ncase int64:\n", depth) +
		strings.Repeat("}\n", depth)
	s := firstSwitch(t, body)
	for range depth - 1 {
		s = s.Cases[0].Body[0].(*ast.Switch)
	}
	if !s.IsTypeSwitch() || len(s.Cases[0].Body) != 0 {
		t.Errorf("innermost switch is not an empty type switch")
	}

	stmts := mainBody(t, "    v, ok := a"+strings.Repeat(".(int64)", depth))
	x := stmts[0].(*ast.MultiVar).Value
	for range depth {
		a, ok := x.(*ast.TypeAssert)
		if !ok {
			t.Fatalf("value is %T, want *ast.TypeAssert", x)
		}
		x = a.X
	}
	if id, ok := x.(*ast.Ident); !ok || id.Name != "a" {
		t.Errorf("innermost operand is %T, want a", x)
	}
}

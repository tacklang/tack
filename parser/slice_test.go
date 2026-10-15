package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// sliceShapes are slices as written, which bounds each has, and what its
// bounds span: every form of x[i:j] with either bound left out, bounds
// that are expressions, calls and struct literals, and a slice of a slice,
// of an element and of a field.
var sliceShapes = []struct {
	expr           string
	lo, hi, bounds string // "" for a bound left out
	x              string
}{
	{"s[1:3]", "1", "3", "1:3", "s"},
	{"s[:3]", "", "3", ":3", "s"},
	{"s[1:]", "1", "", "1:", "s"},
	{"s[:]", "", "", ":", "s"},
	{"s[i + 1:len(s) - 1]", "i + 1", "len(s) - 1", "i + 1:len(s) - 1", "s"},
	{"f(x)[g(1):g(2)]", "g(1)", "g(2)", "g(1):g(2)", "f(x)"},
	{"s[P{a: 1}.a:]", "P{a: 1}.a", "", "P{a: 1}.a:", "s"},
	{"s[1:][:2]", "", "2", ":2", "s[1:]"},
	{"a[0][1:2]", "1", "2", "1:2", "a[0]"},
	{"p.items[:n]", "", "n", ":n", "p.items"},
	{"(s)[1:2]", "1", "2", "1:2", "s"},
}

// TestParsesSlices checks every form of slice parses to an ast.Slice,
// with a nil bound where one is left out, spanning what it slices through
// the closing bracket, parentheses included, its colon and its bounds
// where they are written.
func TestParsesSlices(t *testing.T) {
	for _, tt := range sliceShapes {
		t.Run(tt.expr, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    f(" + tt.expr + ")\n}\n"
			s, ok := firstArg(t, src).(*ast.Slice)
			if !ok {
				t.Fatalf("%s is %T, want *ast.Slice", tt.expr, firstArg(t, src))
			}
			text := func(span ast.Span) string { return src[span.Start:span.End] }
			if got := text(s.Span); got != tt.expr {
				t.Errorf("slice spans %q, want %q", got, tt.expr)
			}
			if got := text(s.ColonSpan); got != ":" {
				t.Errorf("colon spans %q", got)
			}
			if got := text(s.Bounds()); got != tt.bounds {
				t.Errorf("bounds span %q, want %q", got, tt.bounds)
			}
			checkBound(t, "lo", s.Lo, tt.lo, text)
			checkBound(t, "hi", s.Hi, tt.hi, text)
			if got := text(s.X.Spanned()); got != tt.x {
				t.Errorf("x spans %q, want %q", got, tt.x)
			}
		})
	}
}

// checkBound checks the bound of a slice called name, e, is left out when
// want is "", and otherwise spans want, as text writes a span.
func checkBound(t *testing.T, name string, e ast.Expr, want string, text func(ast.Span) string) {
	t.Helper()
	switch {
	case want == "" && e != nil:
		t.Errorf("%s is %T, want it left out", name, e)
	case want != "" && e == nil:
		t.Errorf("%s is left out, want %q", name, want)
	case want != "" && text(e.Spanned()) != want:
		t.Errorf("%s spans %q, want %q", name, text(e.Spanned()), want)
	}
}

// TestSliceIsNotAPlaceToTheParser checks a slice written as an assignment's
// target, an increment's and a mut argument parses, so that the type
// checker can report each with its own message.
func TestSliceIsNotAPlaceToTheParser(t *testing.T) {
	body := mainBody(t, "    s[1:2] = t\n    a[:]++\n    f(mut a[1:])\n    a[1:][0] = 3")
	if _, ok := body[0].(*ast.Assign).Target.(*ast.Slice); !ok {
		t.Errorf("assignment target is %T, want *ast.Slice", body[0].(*ast.Assign).Target)
	}
	if _, ok := body[1].(*ast.IncDec).Target.(*ast.Slice); !ok {
		t.Errorf("increment target is %T, want *ast.Slice", body[1].(*ast.IncDec).Target)
	}
	arg := body[2].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.MutArg)
	if _, ok := arg.X.(*ast.Slice); !ok {
		t.Errorf("mut argument is mut %T, want mut *ast.Slice", arg.X)
	}
	ix, ok := body[3].(*ast.Assign).Target.(*ast.Index)
	if !ok {
		t.Fatalf("element target is %T, want *ast.Index", body[3].(*ast.Assign).Target)
	}
	if _, ok := ix.X.(*ast.Slice); !ok {
		t.Errorf("element target indexes %T, want *ast.Slice", ix.X)
	}
}

// TestRejectsMalformedSlices checks a slice cut short, one with the
// three-index form, and colons where none belongs are each a syntax error
// at the right token.
func TestRejectsMalformedSlices(t *testing.T) {
	tests := []struct{ stmt, want, at string }{
		{"x := s[:", "expected 'value' found '}'", ""},
		{"x := s[1:", "expected 'value' found '}'", ""},
		{"x := s[1:2", "expected ']' found ';'", ""},
		{
			"x := s[1:2:3]",
			"a slice has at most two bounds, x[i:j]; there is no three-index form",
			":",
		},
		{"x := s[1:2:]", "a slice has at most two bounds", ":"},
		{"x := s[::]", "a slice has at most two bounds", ":"},
		{"x := s[:2:3]", "a slice has at most two bounds", ":"},
		{"x := s[1::2]", "a slice has at most two bounds", ":"},
		{"x := s[1:=2]", "expected ']' found ':='", ""},
		{"x := s[1 2]", "expected ']' found '2'", ""},
		{"x := s[1:2 3]", "expected ']' found '3'", ""},
		{"x := s[:)", "expected 'value' found ')'", ""},
	}
	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    " + tt.stmt + "\n}\n"
			_, diags := parse(t, src)
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.stmt)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
			span := diags[0].Span
			if tt.at != "" && src[span.Start:span.End] != tt.at {
				t.Errorf("error spans %q, want %q", src[span.Start:span.End], tt.at)
			}
		})
	}
}

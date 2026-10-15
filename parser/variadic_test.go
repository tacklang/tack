package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// variadicSrc declares a function whose last parameter is variadic, an
// extern func with only a variadic one of an array type, and a mut one,
// which the type checker reports, not the parser.
const variadicSrc = "package main\n\n" +
	"func f(label string, nums ...int64) {\n}\n\n" +
	"extern func g(rows ...[2]lexer.Token)\n\n" +
	"func h(mut xs ...bool) {\n}\n"

// TestParsesVariadicParams checks that args ...T is a Param with Variadic
// set, its EllipsisSpan the ..., its TypeExpr the element type alone, and
// its Span from the name, or mut, through the end of the type.
func TestParsesVariadicParams(t *testing.T) {
	file, diags := parse(t, variadicSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	text := func(s ast.Span) string { return variadicSrc[s.Start:s.End] }
	tests := []struct {
		param               *ast.Param
		span, typ, ellipsis string
	}{
		{file.Decls[0].(*ast.Func).Params[1], "nums ...int64", "int64", "..."},
		{file.Decls[1].(*ast.Func).Params[0], "rows ...[2]lexer.Token", "[2]lexer.Token", "..."},
		{file.Decls[2].(*ast.Func).Params[0], "mut xs ...bool", "bool", "..."},
	}
	for _, tt := range tests {
		p := tt.param
		if !p.Variadic {
			t.Errorf("%s is not variadic", p.Name)
		}
		if got := text(p.Span); got != tt.span {
			t.Errorf("%s spans %q, want %q", p.Name, got, tt.span)
		}
		if got := text(p.TypeExpr.Spanned()); got != tt.typ {
			t.Errorf("%s's type spans %q, want %q", p.Name, got, tt.typ)
		}
		if got := text(p.EllipsisSpan); got != tt.ellipsis {
			t.Errorf("%s's ellipsis spans %q, want %q", p.Name, got, tt.ellipsis)
		}
	}
}

// TestParsesVariadicShapes checks a variadic's element type keeps its
// own shape, an array type here, that mut is kept for the type checker
// to report, and that a parameter without ... is not variadic.
func TestParsesVariadicShapes(t *testing.T) {
	file, diags := parse(t, variadicSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	if rows := file.Decls[1].(*ast.Func).Params[0]; rows.TypeExpr == nil {
		t.Error("rows has no element type")
	} else if _, ok := rows.TypeExpr.(*ast.ArrayType); !ok {
		t.Errorf("rows's element is %T, want *ast.ArrayType", rows.TypeExpr)
	}
	if !file.Decls[2].(*ast.Func).Params[0].Mut {
		t.Error("mut xs ...bool is not mut")
	}
	if label := file.Decls[0].(*ast.Func).Params[0]; label.Variadic ||
		label.EllipsisSpan != (ast.Span{}) {
		t.Errorf("label string is variadic: %+v", label)
	}
}

// TestParsesSpreads checks the shapes a call's arguments take with a
// spread: the ... after the last argument sets Spread and EllipsisSpan,
// leaves the argument itself as written, and is counted in the call's
// span; a call without one has neither.
func TestParsesSpreads(t *testing.T) {
	tests := []struct {
		call, ellipsis, last string
		args                 int
	}{
		{"f(v...)", "...", "v", 1},
		{"f(1, v...)", "...", "v", 2},
		{"f(a[1:]...)", "...", "a[1:]", 1},
		{"f(p.items...)", "...", "p.items", 1},
		{"f([]uint8(s)...)", "...", "[]uint8(s)", 1},
		{"f(mut a...)", "...", "mut a", 1},
		{"f(g()...)", "...", "g()", 1},
		{"lexer.f(1,\n        v...)", "...", "v", 2},
		{"f(v)", "", "v", 1},
		{"f()", "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			src := "package main\n\nimport lexer from \"std/lexer\"\n\nfunc main() {\n    " + tt.call + "\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			call := file.Decls[len(file.Decls)-1].(*ast.Func).Body[0].(*ast.ExprStmt).X.(*ast.Call)
			if got := src[call.Span.Start:call.Span.End]; got != tt.call {
				t.Errorf("the call spans %q, want %q", got, tt.call)
			}
			if call.Spread != (tt.ellipsis != "") {
				t.Errorf("Spread = %v, want %v", call.Spread, tt.ellipsis != "")
			}
			if got := src[call.EllipsisSpan.Start:call.EllipsisSpan.End]; got != tt.ellipsis {
				t.Errorf("the ellipsis spans %q, want %q", got, tt.ellipsis)
			}
			if len(call.Args) != tt.args {
				t.Fatalf("%d arguments, want %d", len(call.Args), tt.args)
			}
			if tt.args > 0 {
				last := call.Args[tt.args-1].Spanned()
				if got := src[last.Start:last.End]; got != tt.last {
					t.Errorf("the last argument spans %q, want %q", got, tt.last)
				}
			}
		})
	}
}

// TestRejectsMisplacedEllipses checks every place ... may not be written
// is a diagnostic at the ..., never a panic or a tree that drops it: a
// variadic before another parameter, two variadics, a result, a
// variable's, a field's and an element's type, a ... with no type or no
// name, a parameter's ... after its type, and a spread before another
// argument, alone, or twice.
func TestRejectsMisplacedEllipses(t *testing.T) {
	tests := []struct{ src, want, at string }{
		{"func f(a ...int64, b int64) {\n}\n", "can only use ... with the final parameter", "..."},
		{
			"func f(a ...int64, b ...int64) {\n}\n",
			"can only use ... with the final parameter",
			"...",
		},
		{"func f(a ...int64 b int64) {\n}\n", "can only use ... with the final parameter", "..."},
		{"func f() ...int64 {\n}\n", "can only use ... with the final parameter", "..."},
		{"func f() (int64, ...int64) {\n}\n", "can only use ... with the final parameter", "..."},
		{"var x ...int64 = 1\n", "can only use ... with the final parameter", "..."},
		{
			"type T struct {\n    x ...int64\n}\n",
			"can only use ... with the final parameter",
			"...",
		},
		{"func f(a [2]...int64) {\n}\n", "can only use ... with the final parameter", "..."},
		{"func f(a ......int64) {\n}\n", "can only use ... with the final parameter", "..."},
		{"func f(a ...) {\n}\n", "expected 'type name' found ')'", ")"},
		{"func f(...int64) {\n}\n", "expected 'parameter name' found '...'", "..."},
		{"func f(a int64...) {\n}\n", "expected 'parameter name' found '...'", "..."},
		{"func main() {\n    f(v..., 1)\n}\n", "can only use ... with the final argument", "..."},
		{
			"func main() {\n    f(v..., w...)\n}\n",
			"can only use ... with the final argument",
			"...",
		},
		{"func main() {\n    f(...)\n}\n", "expected 'value' found '...'", "..."},
		{"func main() {\n    f(v...\n}\n", "can only use ... with the final argument", "..."},
		{"func main() {\n    x := v...\n}\n", "expected ';' found '...'", "..."},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			src := "package main\n\n" + tt.src
			_, diags := parse(t, src)
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if diags[0].Message != tt.want {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want)
			}
			// An unexpected token's error is a point at its start.
			at := diags[0].Span
			if at.End == at.Start {
				at.End = at.Start + len(tt.at)
			}
			if got := src[at.Start:at.End]; got != tt.at {
				t.Errorf("the error is at %q, want %q", got, tt.at)
			}
		})
	}
}

package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesMutParams checks that mut before a parameter's name marks it
// mut, with the keyword's span, and that its span starts at mut; a
// parameter without mut is plain.
func TestParsesMutParams(t *testing.T) {
	src := "package main\n\nfunc f(mut p Point, k int64, mut a [3]int64) {\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	params := file.Decls[0].(*ast.Func).Params
	want := []struct {
		name, span, mutSpan string
		mut                 bool
	}{
		{"p", "mut p Point", "mut", true},
		{"k", "k int64", "", false},
		{"a", "mut a [3]int64", "mut", true},
	}
	if len(params) != len(want) {
		t.Fatalf("got %d parameters, want %d", len(params), len(want))
	}
	for i, w := range want {
		p := params[i]
		if p.Name != w.name || p.Mut != w.mut {
			t.Errorf("param %d: got %s mut=%v, want %s mut=%v", i, p.Name, p.Mut, w.name, w.mut)
		}
		if got := src[p.Span.Start:p.Span.End]; got != w.span {
			t.Errorf("param %s spans %q, want %q", p.Name, got, w.span)
		}
		if got := src[p.MutSpan.Start:p.MutSpan.End]; got != w.mutSpan {
			t.Errorf("param %s mut keyword spans %q, want %q", p.Name, got, w.mutSpan)
		}
	}
}

// TestParsesMutArgs checks that mut before an argument makes a MutArg
// spanning the keyword through the place, for a name, a field, an element
// and a nested path, and that plain arguments beside it stay plain.
func TestParsesMutArgs(t *testing.T) {
	tests := []struct {
		arg   string
		place string
	}{
		{"mut b", "b"},
		{"mut p.x", "p.x"},
		{"mut a[i]", "a[i]"},
		{"mut w.rows[1].pts[j].y", "w.rows[1].pts[j].y"},
		{"mut  b", "b"},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    f(1, " + tt.arg + ", x)\n}\n"
			args := callArgs(t, src)
			if len(args) != 3 {
				t.Fatalf("got %d arguments, want 3", len(args))
			}
			m, ok := args[1].(*ast.MutArg)
			if !ok {
				t.Fatalf("argument is %T, want *ast.MutArg", args[1])
			}
			if got := src[m.Span.Start:m.Span.End]; got != tt.arg {
				t.Errorf("mut argument spans %q, want %q", got, tt.arg)
			}
			if got := src[m.X.Spanned().Start:m.X.Spanned().End]; got != tt.place {
				t.Errorf("place spans %q, want %q", got, tt.place)
			}
			for _, i := range []int{0, 2} {
				if _, isMut := args[i].(*ast.MutArg); isMut {
					t.Errorf("argument %d is a mut argument", i)
				}
			}
		})
	}
}

// TestMutBindsOneOperand checks that mut takes one operand, as a prefix
// operator does, so mut a + 1 is (mut a) + 1, for the checker to report,
// and that mut stacks and nests in arguments of arguments.
func TestMutBindsOneOperand(t *testing.T) {
	src := "package main\n\nfunc main() {\n    f(mut a + 1, mut mut b, g(mut c))\n}\n"
	args := callArgs(t, src)
	if bin, ok := args[0].(*ast.Binary); !ok {
		t.Errorf("mut a + 1 is %T, want *ast.Binary", args[0])
	} else if _, ok := bin.X.(*ast.MutArg); !ok {
		t.Errorf("left of + is %T, want *ast.MutArg", bin.X)
	}
	if outer, ok := args[1].(*ast.MutArg); !ok {
		t.Errorf("mut mut b is %T, want *ast.MutArg", args[1])
	} else if _, ok := outer.X.(*ast.MutArg); !ok {
		t.Errorf("inside mut mut b is %T, want *ast.MutArg", outer.X)
	}
	call, ok := args[2].(*ast.Call)
	if !ok {
		t.Fatalf("g(mut c) is %T, want *ast.Call", args[2])
	}
	if _, ok := call.Args[0].(*ast.MutArg); !ok {
		t.Errorf("g's argument is %T, want *ast.MutArg", call.Args[0])
	}
}

// TestMalformedMutIsReported checks that a mut with nothing after it, in a
// parameter list or an argument list, is a diagnostic, never a panic.
func TestMalformedMutIsReported(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"param without name", "package main\n\nfunc f(mut) {\n}\n", "parameter name"},
		{"param without type", "package main\n\nfunc f(mut p) {\n}\n", "found ')'"},
		{"mut twice in params", "package main\n\nfunc f(mut mut p int64) {\n}\n", "parameter name"},
		{"argument without place", "package main\n\nfunc main() {\n    f(mut)\n}\n", "found ')'"},
		{"truncated", "package main\n\nfunc main() {\n    f(mut", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := parse(t, tt.src)
			if len(diags) == 0 {
				t.Fatalf("parse accepted %q", tt.src)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("got %q, want it to mention %q", diags[0].Message, tt.want)
			}
		})
	}
}

// TestDeeplyNestedMutArgs checks that a long chain of mut keywords and
// nested calls parses without trouble.
func TestDeeplyNestedMutArgs(t *testing.T) {
	depth := 500
	arg := strings.Repeat("mut ", depth) + "x"
	call := strings.Repeat("f(mut ", depth) + "x" + strings.Repeat(")", depth)
	src := "package main\n\nfunc main() {\n    f(" + arg + ")\n    " + call + "\n}\n"
	if _, diags := parse(t, src); len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags[0])
	}
}

package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// invalidRangeCases are range loops the checker must reject, with the
// diagnostics each must produce.
var invalidRangeCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"string",
		inMain("for range \"abc\" {\n    }"),
		[]spanned{{"cannot range over string", "\"abc\""}},
	},
	{
		"bool",
		inMain("for i := range 1 < 2 {\n        exit(i)\n    }"),
		[]spanned{{"cannot range over bool", "1 < 2"}},
	},
	{
		"nothing",
		"package main\n\nfunc f() {\n}\n\nfunc main() {\n    for range f() {\n    }\n}\n",
		[]spanned{{"f returns no value", "f()"}},
	},
	{
		"assigned",
		inMain("for i := range 3 {\n        i++\n        exit(i)\n    }"),
		[]spanned{{"cannot assign to immutable variable i", "i"}},
	},
	{
		"used as a string",
		inMain("for i := range 3 {\n        print(i)\n    }"),
		[]spanned{{"cannot use int64 as string in argument to print", "i"}},
	},
	{
		"error in the body",
		inMain("for range 3 {\n        exit(\"s\")\n    }"),
		[]spanned{{"cannot use string as int64 in argument to exit", "\"s\""}},
	},
}

// TestRangeLoops checks the count is an int, the variable an immutable int,
// and the body a loop body.
func TestRangeLoops(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for i := range 3 {\n        if i == 1 {\n            continue\n        }\n        exit(i + 1)\n    }\n}\n"
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	if v := file.Decls[0].(*ast.Func).Body[0].(*ast.ForRange).Var; v.Type != types.Int64 {
		t.Errorf("variable type = %v, want int64", v.Type)
	}

	forever := "package main\n\nfunc f() int64 {\n    for range 3 {\n        return 1\n    }\n}\n\nfunc main() {\n    exit(f())\n}\n"
	if _, diags := checkTree(
		t,
		forever,
	); len(diags) != 1 ||
		diags[0].Message != "f must end in a return" {
		t.Errorf("range loop at the end of f: got %v, want f must end in a return", diags)
	}

	for _, tt := range invalidRangeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

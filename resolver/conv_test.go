package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A call to a type name is a conversion: it records the type and binds to
// no declaration.
func TestResolvesConversions(t *testing.T) {
	file, diags := resolveTree(
		t,
		"package main\n\nfunc main() {\n    x := 1\n    exit(int64(uint8(x)))\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("got %q", messagesOf(diags))
	}
	outer := file.Decls[0].(*ast.Func).Body[1].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Call)
	inner := outer.Args[0].(*ast.Call)
	if outer.Conv != types.Int64 || inner.Conv != types.Uint8 {
		t.Errorf("conversions record %v and %v, want int64 and uint8", outer.Conv, inner.Conv)
	}
	if outer.Fn.(*ast.Ident).Def != nil {
		t.Error("a conversion bound its type name to a declaration")
	}
}

// typeNameCases put a type name everywhere a name can go other than a type
// or a conversion, with the one message each must produce.
var typeNameCases = []struct{ name, src, msg string }{
	{
		"function",
		"func int8() {\n}\n\nfunc main() {\n}\n",
		"cannot use int8 as a name; it is a type",
	},
	{"global", "var uint64 = 1\n\nfunc main() {\n}\n", "cannot use uint64 as a name; it is a type"},
	{
		"parameter",
		"func f(string int64) {\n}\n\nfunc main() {\n    f(1)\n}\n",
		"cannot use string as a name; it is a type",
	},
	{"local", "func main() {\n    bool := true\n}\n", "cannot use bool as a name; it is a type"},
	{
		"local var",
		"func main() {\n    mut int16 int64\n}\n",
		"cannot use int16 as a name; it is a type",
	},
	{
		"several results",
		"func p() (int64, int64) {\n    return 1, 2\n}\n\nfunc main() {\n    a, int32 := p()\n    exit(a)\n}\n",
		"cannot use int32 as a name; it is a type",
	},
	{
		"range variable",
		"func main() {\n    for uint8 := range 3 {\n    }\n}\n",
		"cannot use uint8 as a name; it is a type",
	},
	{"value", "func main() {\n    exit(int64)\n}\n", "cannot use int64 as a value; it is a type"},
	{"assigned", "func main() {\n    uint32 = 1\n}\n", "cannot assign to uint32; it is a type"},
	{"incremented", "func main() {\n    int8++\n}\n", "cannot assign to int8; it is a type"},
}

// TestTypeNamesAreReserved checks a type name can be written only as a type
// or a conversion, like a keyword.
func TestTypeNamesAreReserved(t *testing.T) {
	for _, tt := range typeNameCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, "package main\n\n"+tt.src)
			if got := messagesOf(diags); strings.Join(got, "\n") != tt.msg {
				t.Errorf("got %q, want only %q", got, tt.msg)
			}
		})
	}
}

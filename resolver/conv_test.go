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

// A type name cannot be declared as a function, since every call to it is a
// conversion.
func TestRejectsFunctionNamedAfterAType(t *testing.T) {
	for _, name := range []string{"int8", "uint64", "string", "bool"} {
		_, diags := resolveTree(t, "package main\n\nfunc "+name+"() {\n}\n\nfunc main() {\n}\n")
		want := name + " is a type and cannot be declared as a function"
		if got := messagesOf(diags); strings.Join(got, "\n") != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

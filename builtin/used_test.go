package builtin_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

// checked parses, resolves and type-checks src with builtins, returning
// the program and every diagnostic.
func checked(t *testing.T, src string, builtins *builtin.Set) (*ast.Program, []diag.Diagnostic) {
	t.Helper()
	f := &source.File{ID: "main.tck", Filename: "main.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}
	prog := ast.SingleFile(file)
	if diags := resolver.New(prog, builtins).Resolve(); len(diags) > 0 {
		return prog, diags
	}
	return prog, typeck.New(prog, f, builtins).Check()
}

// The builtins with a C body are the compiler's own: print and exit,
// each found used wherever a call to it is, a defer's block included, and
// listed once, under its C name.
func TestDefaultBuiltinsAreUsed(t *testing.T) {
	set := builtin.Default()
	prog, diags := checked(
		t,
		"package main\n\nfunc main() {\n    print(\"a\")\n    defer {\n        print(\"b\")\n    }\n}\n",
		set,
	)
	if len(diags) > 0 {
		t.Fatalf("a call to print was rejected: %+v", diags)
	}
	used := set.Used(prog)
	if len(used) != 1 || used[0].Name != "print" || used[0].CName != "tack_builtin_print" {
		t.Errorf("Used = %+v, want print once", used)
	}
}

// A builtin is checked and reserved: wrong arguments are rejected, and so
// is a declaration taking its name, whether the builtin has a C body or is
// generic; with no builtins, a nil Set, print is no builtin at all.
func TestBuiltinsAreCheckedAndReserved(t *testing.T) {
	set := builtin.Default()
	_, diags := checked(t, "package main\n\nfunc main() {\n    exit(\"a\")\n}\n", set)
	if len(diags) == 0 {
		t.Error("exit with a string was accepted")
	}
	for _, name := range []string{"print", "exit", "len", "has"} {
		_, diags = checked(t, "package main\n\nfunc "+name+"() {\n}\n\nfunc main() {\n}\n", set)
		if len(diags) == 0 || !strings.Contains(diags[0].Message, "builtin") {
			t.Errorf("declaring %s, a builtin, was accepted: %+v", name, diags)
		}
	}

	_, diags = checked(t, "package main\n\nfunc main() {\n    print(\"a\")\n}\n", nil)
	if len(diags) == 0 || !strings.Contains(diags[0].Message, "print") {
		t.Errorf("print resolved with no builtins: %+v", diags)
	}

	if len(set.Used(ast.SingleFile(ast.NewFile()))) != 0 {
		t.Error("an empty program uses a builtin")
	}
}

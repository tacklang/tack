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

// The builtins are whatever the std/builtin loaded declares: a builtin
// declared there is called, checked and found used like print.
func TestBuiltinsComeFromTheLoadedSet(t *testing.T) {
	set, err := builtin.Load(writeStd(t, map[string]string{
		"builtin.tck": "package builtin\n\nextern(\"tack_builtin_twice\") func twice(x int64, s string)\n" +
			"extern(\"tack_builtin_never\") func never()\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	prog, diags := checked(
		t,
		"package main\n\nfunc main() {\n    twice(1, \"a\")\n    defer {\n        twice(2, \"b\")\n    }\n}\n",
		set,
	)
	if len(diags) > 0 {
		t.Fatalf("a call to a loaded builtin was rejected: %+v", diags)
	}
	used := set.Used(prog)
	if len(used) != 1 || used[0].Name != "twice" || used[0].CName != "tack_builtin_twice" {
		t.Errorf("Used = %+v, want twice once", used)
	}
}

// A loaded builtin is checked and reserved like print: wrong arguments are
// rejected, a declaration of its name is too, and print, not declared in
// the set, is no builtin at all.
func TestLoadedBuiltinsAreTheOnlyBuiltins(t *testing.T) {
	set, err := builtin.Load(writeStd(t, map[string]string{
		"builtin.tck": "package builtin\n\nextern(\"tack_builtin_twice\") func twice(x int64, s string)\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, diags := checked(t, "package main\n\nfunc main() {\n    twice(\"a\", 1)\n}\n", set)
	if len(diags) == 0 {
		t.Error("twice with its arguments swapped was accepted")
	}

	_, diags = checked(t, "package main\n\nfunc main() {\n    print(\"a\")\n}\n", set)
	if len(diags) == 0 || !strings.Contains(diags[0].Message, "print") {
		t.Errorf("print, which the set does not declare, resolved: %+v", diags)
	}

	_, diags = checked(t, "package main\n\nfunc twice() {\n}\n\nfunc main() {\n}\n", set)
	if len(diags) == 0 || !strings.Contains(diags[0].Message, "builtin") {
		t.Errorf("declaring twice, a loaded builtin, was accepted: %+v", diags)
	}

	if len(set.Used(ast.SingleFile(ast.NewFile()))) != 0 {
		t.Error("an empty program uses a builtin")
	}
}

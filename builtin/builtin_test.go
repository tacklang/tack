package builtin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// declarationsPath is the Tack file that documents the builtins. It is written
// for a reader, and this package is what the compiler actually consults, so
// the test below is what stops the two from drifting apart.
var declarationsPath = filepath.Join("..", "src", "std", "builtin", "builtin.tck")

// declared parses the declaration file and returns what it says the builtins
// are. It fails the test if the file does not parse, which also pins that the
// compiler can read its own documentation.
func declared(t *testing.T) []builtin.Func {
	t.Helper()

	src, err := os.ReadFile(declarationsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", declarationsPath, err)
	}

	f := &source.File{ID: source.FileID("builtin"), Filename: declarationsPath, Data: src}

	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lexing %s:\n%s", declarationsPath, diag.RenderAllPretty(f, diags))
	}

	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parsing %s:\n%s", declarationsPath, diag.RenderAllPretty(f, diags))
	}

	var funcs []builtin.Func
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.Func)
		if !ok {
			continue
		}

		// A bodyless declaration and one written "{}" are the same tree, so
		// this catches an implementation, not the braces around it.
		if len(fn.Body) > 0 {
			t.Errorf(
				"%s gives %s an implementation; builtins are lowered by the backends, not written in Tack",
				declarationsPath,
				fn.Name,
			)
		}

		funcs = append(funcs, builtin.Func{Name: fn.Name, Params: declaredParams(t, fn)})
	}
	return funcs
}

// declaredParams returns the types of fn's parameters as the declaration file
// writes them. It fails the test on a type it cannot name.
func declaredParams(t *testing.T, fn *ast.Func) []types.Type {
	t.Helper()

	params := make([]types.Type, len(fn.Params))
	for i, param := range fn.Params {
		id, ok := param.TypeExpr.(*ast.Ident)
		if !ok {
			t.Fatalf(
				"%s: parameter %s of %s has an unsupported type expression",
				declarationsPath,
				param.Name,
				fn.Name,
			)
		}
		typ, known := types.Lookup(id.Name)
		if !known {
			t.Fatalf(
				"%s: parameter %s of %s has unknown type %q",
				declarationsPath,
				param.Name,
				fn.Name,
				id.Name,
			)
		}
		params[i] = typ
	}
	return params
}

func TestDeclarationFileListsExactlyTheImplementedBuiltins(t *testing.T) {
	got := declared(t)
	want := builtin.Funcs()

	if len(got) != len(want) {
		t.Fatalf(
			"%s declares %d builtins, but the compiler implements %d;\ndeclared: %v\nimplemented: %v",
			declarationsPath,
			len(got),
			len(want),
			names(got),
			names(want),
		)
	}

	table := types.NewTable()
	byName := make(map[string]builtin.Func, len(got))
	for _, fn := range got {
		byName[fn.Name] = fn
	}

	for _, fn := range want {
		declared, ok := byName[fn.Name]
		if !ok {
			t.Errorf(
				"the compiler implements %s but %s does not declare it",
				fn.Name,
				declarationsPath,
			)
			continue
		}

		if len(declared.Params) != len(fn.Params) {
			t.Errorf("%s declares %s with %d parameter(s), the compiler takes %d",
				declarationsPath, fn.Name, len(declared.Params), len(fn.Params))
			continue
		}
		for i := range fn.Params {
			if declared.Params[i] != fn.Params[i] {
				t.Errorf(
					"%s declares parameter %d of %s as %s, the compiler takes %s",
					declarationsPath,
					i,
					fn.Name,
					table.Name(declared.Params[i]),
					table.Name(fn.Params[i]),
				)
			}
		}
	}
}

func names(funcs []builtin.Func) []string {
	out := make([]string, len(funcs))
	for i, fn := range funcs {
		out[i] = fn.Name
	}
	return out
}

// Every builtin Funcs lists is found by Lookup under its own name, with the
// same signature.
func TestLookupFindsEveryBuiltin(t *testing.T) {
	for _, want := range builtin.Funcs() {
		got, ok := builtin.Lookup(want.Name)
		if !ok || got.Name != want.Name || len(got.Params) != len(want.Params) {
			t.Errorf("Lookup(%q) = %+v, %v, want %+v, true", want.Name, got, ok, want)
			continue
		}
		for i := range want.Params {
			if got.Params[i] != want.Params[i] {
				t.Errorf(
					"Lookup(%q) param %d = %v, want %v",
					want.Name,
					i,
					got.Params[i],
					want.Params[i],
				)
			}
		}
	}
}

// The builtins' signatures are part of the language, so they are pinned here
// rather than only read back from the table.
func TestBuiltinSignatures(t *testing.T) {
	tests := []struct {
		name   string
		params []types.Type
	}{
		{"print", []types.Type{types.String}},
		{"exit", []types.Type{types.Int}},
	}
	if len(builtin.Funcs()) != len(tests) {
		t.Errorf("Funcs() has %d builtins, want %d", len(builtin.Funcs()), len(tests))
	}
	for _, tt := range tests {
		got, ok := builtin.Lookup(tt.name)
		if !ok {
			t.Errorf("Lookup(%q) found nothing", tt.name)
			continue
		}
		if len(got.Params) != len(tt.params) {
			t.Errorf("%s takes %d params, want %d", tt.name, len(got.Params), len(tt.params))
			continue
		}
		for i := range tt.params {
			if got.Params[i] != tt.params[i] {
				t.Errorf("%s param %d = %v, want %v", tt.name, i, got.Params[i], tt.params[i])
			}
		}
	}
}

// Names that are not builtins are not found: near misses, other languages'
// spellings, and Go's builtins Tack does not have.
func TestLookupRejectsNamesThatAreNotBuiltins(t *testing.T) {
	for _, name := range []string{"", "Print", "PRINT", "println", "printf", " print", "exit ", "Exit", "len", "panic", "main"} {
		if got, ok := builtin.Lookup(name); ok {
			t.Errorf("Lookup(%q) = %+v, true, want not found", name, got)
		}
	}
}

package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// mainBody parses a program whose main has body as its statements, and
// returns them. It fails the test if parsing reports anything.
func mainBody(t *testing.T, body string) []ast.Stmt {
	t.Helper()

	file, diags := parse(t, "package main\n\nfunc main() {\n"+body+"\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}
	return file.Decls[len(file.Decls)-1].(*ast.Func).Body
}

// typeName returns the name a type expression spells, or "" for nil.
func typeName(t *testing.T, typ ast.Expr) string {
	t.Helper()

	if typ == nil {
		return ""
	}
	id, ok := typ.(*ast.Ident)
	if !ok {
		t.Fatalf("type is %T, want *ast.Ident", typ)
	}
	return id.Name
}

// TestParsesEveryDeclarationForm checks each way to declare a variable gives
// the right name, mutability, declared type, and whether it has a value.
func TestParsesEveryDeclarationForm(t *testing.T) {
	tests := []struct {
		src      string
		name     string
		typ      string
		mutable  bool
		hasValue bool
	}{
		{"x := 1", "x", "", false, true},
		{"mut x := 1", "x", "", true, true},
		{"var x int64 = 1", "x", "int64", false, true},
		{"var x = 1", "x", "", false, true},
		{"var x int64", "x", "int64", false, false},
		{"mut x int64 = 1", "x", "int64", true, true},
		{"mut x = 1", "x", "", true, true},
		{"mut x int64", "x", "int64", true, false},
		{"mut s string", "s", "string", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			body := mainBody(t, "    "+tt.src)
			if len(body) != 1 {
				t.Fatalf("got %d statements, want 1", len(body))
			}
			v, ok := body[0].(*ast.Var)
			if !ok {
				t.Fatalf("statement is %T, want *ast.Var", body[0])
			}
			if v.Name != tt.name {
				t.Errorf("name = %q, want %q", v.Name, tt.name)
			}
			if got := typeName(t, v.TypeExpr); got != tt.typ {
				t.Errorf("type = %q, want %q", got, tt.typ)
			}
			if v.Mutable != tt.mutable {
				t.Errorf("mutable = %v, want %v", v.Mutable, tt.mutable)
			}
			if (v.Value != nil) != tt.hasValue {
				t.Errorf("has value = %v, want %v", v.Value != nil, tt.hasValue)
			}
		})
	}
}

// The value of a declaration is a full expression.
func TestDeclarationValueIsAnExpression(t *testing.T) {
	v := mainBody(t, "    x := f(1) + 2 * 3")[0].(*ast.Var)

	bin, ok := v.Value.(*ast.Binary)
	if !ok || bin.Op != ast.Add {
		t.Fatalf("value is %T, want an addition", v.Value)
	}
}

// Spans cover the whole declaration, and the name span covers only the name.
func TestDeclarationSpans(t *testing.T) {
	src := "    mut count int64 = 42"
	v := mainBody(t, src)[0].(*ast.Var)

	// The body starts after "package main\n\nfunc main() {\n".
	base := len("package main\n\nfunc main() {\n")
	if v.Span.Start != base+4 || v.Span.End != base+len(src) {
		t.Errorf("span = [%d:%d], want [%d:%d]", v.Span.Start, v.Span.End, base+4, base+len(src))
	}
	if v.NameSpan.Start != base+8 || v.NameSpan.End != base+13 {
		t.Errorf(
			"name span = [%d:%d], want [%d:%d]",
			v.NameSpan.Start,
			v.NameSpan.End,
			base+8,
			base+13,
		)
	}
}

func TestParsesAssignment(t *testing.T) {
	body := mainBody(t, "    mut x := 1\n    x = x + 1")
	if len(body) != 2 {
		t.Fatalf("got %d statements, want 2", len(body))
	}

	a, ok := body[1].(*ast.Assign)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Assign", body[1])
	}
	if a.Target.Name != "x" {
		t.Errorf("target = %q, want x", a.Target.Name)
	}
	if _, ok := a.Value.(*ast.Binary); !ok {
		t.Errorf("value is %T, want *ast.Binary", a.Value)
	}
}

// Declarations and assignments are statements like any other, so they work
// inside branches and next to other statements.
func TestDeclarationsInsideBranches(t *testing.T) {
	body := mainBody(
		t,
		"    if 1 < 2 {\n        x := 1\n        exit(x)\n    } else {\n        mut y := 2\n        y = 3\n    }",
	)

	ifs := body[0].(*ast.If)
	if _, ok := ifs.Then[0].(*ast.Var); !ok {
		t.Errorf("then[0] is %T, want *ast.Var", ifs.Then[0])
	}
	if _, ok := ifs.Else[1].(*ast.Assign); !ok {
		t.Errorf("else[1] is %T, want *ast.Assign", ifs.Else[1])
	}
}

// A declaration can be the last statement before a closing brace on the same
// line, where no semicolon is inserted.
func TestDeclarationBeforeClosingBraceOnOneLine(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc main() { x := 1 }\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %v", diags)
	}
	if _, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.Var); !ok {
		t.Errorf("statement is %T, want *ast.Var", file.Decls[0].(*ast.Func).Body[0])
	}
}

// TestRejectsMalformedDeclarations checks each malformed declaration or
// assignment is a parse error, reported where the problem is.
func TestRejectsMalformedDeclarations(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"var x", "expected 'type or =' found ';'"},
		{"mut x", "expected 'type or =' found ';'"},
		{"var x := 1", "expected 'type or =' found ':='"},
		{"var := 1", "expected 'variable name' found ':='"},
		{"var 1 = 2", "expected 'variable name' found '1'"},
		{"mut mut x := 1", "expected 'variable name' found 'mut'"},
		{"mut var x = 1", "expected 'variable name' found 'var'"},
		{"var var x = 1", "expected 'variable name' found 'var'"},
		{"x :=", "expected 'value' found '}'"},
		{"x =", "expected 'value' found '}'"},
		{"var x int64 =", "expected 'value' found '}'"},
		{"var x int64 int64", "expected ';' found 'int64'"},
		{"x := 1 2", "expected ';' found '2'"},
		{"x = 1 2", "expected ';' found '2'"},
		{"x == 1", "expected 'statement' found 'x'"},
		{"x", "expected 'statement' found 'x'"},
		{"1 = 2", "expected 'statement' found '1'"},
		{"var x ...int64 = 1", "variadic parameters are not supported"},
	}

	for _, tt := range tests {
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

// packageVars returns a file's package-level variables, in source order.
func packageVars(file *ast.File) []*ast.Var {
	var vars []*ast.Var
	for _, decl := range file.Decls {
		if v, ok := decl.(*ast.Var); ok {
			vars = append(vars, v)
		}
	}
	return vars
}

// TestParsesPackageLevelDeclarations checks var and mut declarations between
// functions become top-level declarations, in source order.
func TestParsesPackageLevelDeclarations(t *testing.T) {
	src := "package main\n\nvar limit int64 = 10\n\nmut count int64\n\n// a note\nvar name = \"tack\"\n\nfunc main() {\n}\n\nmut last = 1 < 2\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %v", diags)
	}

	want := []struct {
		name     string
		typ      string
		mutable  bool
		hasValue bool
	}{
		{"limit", "int64", false, true},
		{"count", "int64", true, false},
		{"name", "", false, true},
		{"last", "", true, true},
	}

	vars := packageVars(file)
	if len(vars) != len(want) {
		t.Fatalf("got %d package-level variables, want %d", len(vars), len(want))
	}
	for i, w := range want {
		v := vars[i]
		if v.Name != w.name || typeName(t, v.TypeExpr) != w.typ || v.Mutable != w.mutable ||
			(v.Value != nil) != w.hasValue {
			t.Errorf("variable %d = {%s %q mut=%v value=%v}, want %+v",
				i, v.Name, typeName(t, v.TypeExpr), v.Mutable, v.Value != nil, w)
		}
	}
	if _, ok := file.Decls[len(file.Decls)-1].(*ast.Var); !ok {
		t.Errorf(
			"last declaration is %T, want the variable after main",
			file.Decls[len(file.Decls)-1],
		)
	}
}

// The short forms are for function bodies only.
func TestRejectsShortDeclarationsAtPackageLevel(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"x := 1", "expected 'func' found 'x'"},
		{"mut x := 1", "expected 'type or =' found ':='"},
		{"x = 1", "expected 'func' found 'x'"},
		{"var x", "expected 'type or =' found ';'"},
	}
	for _, tt := range tests {
		_, diags := parse(t, "package main\n\n"+tt.src+"\n\nfunc main() {\n}\n")
		if len(diags) == 0 {
			t.Errorf("%q parsed without error", tt.src)
			continue
		}
		if diags[0].Message != tt.want {
			t.Errorf("%q: message = %q, want %q", tt.src, diags[0].Message, tt.want)
		}
	}
}

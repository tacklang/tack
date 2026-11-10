package resolver_test

import (
	"slices"
	"testing"

	"github.com/tacklang/tack/ast"
)

// structProgram puts type declarations before a main function with body.
func structProgram(types, body string) string {
	return "package main\n\n" + types + "\n\nfunc main() {\n" + body + "\n}\n"
}

// TestBindsTypeNamesToTheirDeclarations checks a struct's name is bound
// wherever a type is written, before its declaration included: in a field,
// a variable, a parameter, a result and a literal.
func TestBindsTypeNamesToTheirDeclarations(t *testing.T) {
	src := "package main\n\nfunc f(p P) P {\n    return p\n}\n\n" +
		"type L struct {\n    a [2]P\n}\n\ntype P struct {\n    x int64\n}\n\n" +
		"func main() {\n    mut l L\n    q := P{x: 1}\n    l.a[0] = f(q)\n    exit(l.a[0].x)\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v", messagesOf(diags))
	}
	p := file.Decls[2].(*ast.TypeDecl)
	var bound []string
	ast.Walk(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "P" {
			if id.Def != p {
				t.Errorf("P at %d is bound to %v", id.Span.Start, id.Def)
			}
			bound = append(bound, id.Name)
		}
		return true
	})
	if len(bound) != 4 {
		t.Errorf("found %d uses of P, want 4", len(bound))
	}
}

// TestAcceptsStructNames lists programs that use struct, field and other
// names in ways that must resolve cleanly: field names are a namespace of
// their own, and writing a field or a key does not read or bind anything.
func TestAcceptsStructNames(t *testing.T) {
	tests := []struct{ name, types, body string }{
		{
			"field named like a variable",
			"type P struct { x int64 }",
			"    x := P{x: 1}\n    exit(x.x)",
		},
		{
			"field named like a type",
			"type P struct { P int64; Q bool }\n\ntype Q struct {}",
			"    p := P{P: 1, Q: true}\n    exit(p.P)",
		},
		{
			"field named like a function",
			"type P struct { main int64; exit int64 }",
			"    p := P{main: 1, exit: 2}\n    exit(p.exit)",
		},
		{
			"field named like a keyword-free C word",
			"type P struct { int int64; while bool }",
			"    p := P{int: 1, while: true}\n    exit(p.int)",
		},
		{
			"the same field in two structs",
			"type P struct { x int64 }\n\ntype Q struct { x bool }",
			"    p := P{x: 1}\n    q := Q{x: true}\n    if q.x {\n        exit(p.x)\n    }",
		},
		{
			"a field array length naming a later global",
			"type P struct { a [n]int64 }\n\nvar n = 2",
			"    mut p P\n    p.a[1] = 1\n    exit(p.a[1])",
		},
		{
			"field written, variable read",
			"type P struct { x int64 }",
			"    mut p P\n    p.x = 1\n    exit(p.x)",
		},
		{"empty struct", "type E struct {}", "    e := E{}\n    if e == e {\n    }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := resolveTree(t, structProgram(tt.types, tt.body)); len(diags) > 0 {
				t.Errorf("got %v", messagesOf(diags))
			}
		})
	}
}

// TestKeysAndFieldsAreNotBound checks that the name in a key and in a field
// selection is left unbound, even when a variable of that name is in scope.
func TestKeysAndFieldsAreNotBound(t *testing.T) {
	src := structProgram("type P struct { x int64 }", "    x := 1\n    p := P{x: x}\n    exit(p.x)")
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v", messagesOf(diags))
	}
	body := file.Decls[1].(*ast.Func).Body
	kv := body[1].(*ast.Var).Value.(*ast.CompositeLit).Elems[0].(*ast.KeyValue)
	if key := kv.Key.(*ast.Ident); key.Def != nil {
		t.Errorf("key x is bound to %v", key.Def)
	}
	if value := kv.Value.(*ast.Ident); value.Def != body[0].(*ast.Var) {
		t.Errorf("value x is bound to %v, want the variable", value.Def)
	}
}

// structNameErrors are misuses of a struct's name, of a field's name, and
// of a name that is not a type, with the messages each must give, in order.
var structNameErrors = []struct {
	name, types, body string
	want              []string
}{
	{
		"two types of one name",
		"type P struct {}\n\ntype P struct {}",
		"",
		[]string{"P is already declared"},
	},
	{"a type and a function", "type main struct {}", "", []string{"main is already declared"}},
	{
		"a type and a global",
		"type P struct {}\n\nvar P = 1",
		"",
		[]string{"P is already declared"},
	},
	{
		"a type and a builtin",
		"type len struct {}",
		"",
		[]string{"len is a builtin and cannot be redeclared"},
	},
	{
		"a type named like a basic type",
		"type bool struct {}",
		"",
		[]string{"cannot use bool as a name; it is a type"},
	},
	{"a type named _", "type _ struct {}", "", []string{"cannot use _ as a name"}},
	{
		"a local named like a type",
		"type P struct {}",
		"    P := 1\n    exit(P)",
		[]string{"P is already declared"},
	},
	{
		"a range variable named like a type",
		"type P struct {}",
		"    for P := range 3 {\n        exit(P)\n    }",
		[]string{"P is already declared"},
	},
	{
		"a parameter named like a type",
		"type P struct {}\n\nfunc f(P int64) {\n}",
		"",
		[]string{"P is already declared"},
	},
	{
		"a type as a value",
		"type P struct {}",
		"    x := P\n    exit(x)",
		[]string{"cannot use P as a value; it is a type"},
	},
	{
		"a type assigned",
		"type P struct {}",
		"    P = 1",
		[]string{"cannot assign to P; it is a type"},
	},
	{"a type called", "type P struct {}", "    P()", []string{"cannot call P; it is a type"}},
	// A name selected through a type is a method expression, or a case
	// built, whose name only the type checker looks up.
	{"a field of a type", "type P struct { x int64 }", "    exit(P.x)", nil},
	{
		"a variable as a type",
		"",
		"    n := 1\n    mut m n\n    exit(m)",
		[]string{"n is not a type"},
	},
	// A signature's types see only package-level names, so the type
	// checker reports n as an unknown type.
	{"a parameter as a type", "func f(n int64, m n) {\n}", "", nil},
	{
		"a global as a field type",
		"type P struct { x g }\n\nvar g = 1",
		"",
		[]string{"g is not a type"},
	},
	{
		"duplicate fields",
		"type P struct { x int64; y int64; x bool }",
		"",
		[]string{"duplicate field name"},
	},
	{"a field named _", "type P struct { _ int64 }", "", []string{"cannot use _ as a name"}},
	{
		"a field named like a basic type",
		"type P struct { uint8 uint8 }",
		"",
		[]string{"cannot use uint8 as a name; it is a type"},
	},
	{
		"an undefined name in a field length",
		"type P struct { a [k]int64 }",
		"",
		[]string{"undefined: k"},
	},
	{
		"an undefined value in a literal",
		"type P struct { x int64 }",
		"    p := P{x: k}\n    exit(p.x)",
		[]string{"undefined: k"},
	},
	{"a field read from an undefined name", "", "    exit(q.x)", []string{"undefined: q"}},
	{"a field written to an undefined name", "", "    q.x = 1", []string{"undefined: q"}},
	{
		"a field written, never read",
		"type P struct { x int64 }",
		"    mut p P\n    p.x = 1",
		[]string{"declared and not used: p"},
	},
}

// TestRejectsStructNameErrors checks each misuse above is reported as
// listed.
func TestRejectsStructNameErrors(t *testing.T) {
	for _, tt := range structNameErrors {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, structProgram(tt.types, tt.body))
			if got := messagesOf(diags); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

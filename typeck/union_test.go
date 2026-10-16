package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// unionDecls declares the unions, a struct holding one and the helper
// the cases below use.
const unionDecls = `type E union {
    int  int64
    name string
    pair [2]int8
    eof
}

type V union {
    items list[int64]
    e     E
    none
}

type S struct {
    e E
    k int64
}

func use(n int64) {
}

func size(v V) int64 {
    switch v {
    case items(l):
        return len(l)
    default:
        return 0
    }
}`

// unionCases are programs building, matching and comparing unions that
// must check cleanly.
var unionCases = []struct{ name, src string }{
	{"build every case", withTypes(unionDecls, "a := E.int(1)", "b := E.name(\"x\")",
		"c := E.pair([2]int8{1, -1})", "d := E.eof", "use(len(\"x\"))",
		"if a == b || c == d {", "}")},
	{"untyped and narrower payloads", withTypes(unionDecls, "x := int8(3)", "a := E.int(x)",
		"b := E.int(-9223372036854775808)", "if a != b {", "}")},
	{"switch every case", withTypes(unionDecls, "e := E.eof", "switch e {", "case int(n):",
		"    use(n)", "case name(s):", "    use(len(s))", "case pair(p):", "    use(p[0])",
		"case eof:", "}")},
	{"switch with a default", withTypes(unionDecls, "e := E.eof", "switch e {",
		"case int(n):", "    use(n)", "default:", "}")},
	{"bare payload case", withTypes(unionDecls, "e := E.eof", "switch e {", "case int, name:",
		"case pair:", "case eof:", "}")},
	{"switch on a call", withTypes(unionDecls+"\n\nfunc f() E {\n    return E.eof\n}",
		"switch f() {", "case int(n):", "    use(n)", "default:", "}")},
	{"switch on a field and an element", withTypes(unionDecls,
		"s := S{e: E.int(1), k: 2}", "es := [2]E{E.eof, s.e}", "switch s.e {",
		"case int(n):", "    use(n + s.k)", "default:", "}", "switch es[1] {", "case int(n):",
		"    use(n)", "default:", "}")},
	{
		"a clause binding nothing may assign",
		withTypes(
			unionDecls,
			"mut e := E.eof",
			"switch e {",
			"case eof:",
			"    e = E.int(1)",
			"default:",
			"}",
			"use(len(\"\"))",
			"if e == E.eof {",
			"}",
		),
	},
	{"nested switch on a binding", withTypes(unionDecls, "v := V.e(E.int(3))", "switch v {",
		"case e(inner):", "    switch inner {", "    case int(n):", "        use(n)",
		"    default:", "    }", "default:", "}")},
	{"owned union moved and freed", withTypes(unionDecls,
		"mut v := V.items(make(list[int64], c_allocator))", "use(size(v))", "w := v",
		"v = V.none", "use(size(v) + size(w))")},
	{"binding of a list borrowed", withTypes(unionDecls,
		"v := V.items(make(list[int64], c_allocator))", "switch v {", "case items(l):",
		"    for _, x := range l {", "        use(x)", "    }", "default:", "}")},
	{"facts about a binding", withTypes(unionDecls, "e := E.int(4)", "switch e {",
		"case int(n):", "    if n != 0 {", "        use(10 / n)", "    }", "default:", "}")},
	{"function ending in a covering switch", withTypes(unionDecls+
		"\n\nfunc k(e E) int64 {\n    switch e {\n    case int(n):\n        return n\n"+
		"    case name, pair, eof:\n        return 0\n    }\n}", "use(k(E.eof))")},
}

// TestAcceptsUnions checks each of unionCases checks cleanly.
func TestAcceptsUnions(t *testing.T) {
	for _, tc := range unionCases {
		t.Run(tc.name, func(t *testing.T) {
			if diags := check(t, tc.src); len(diags) > 0 {
				t.Errorf("reported %v", diags)
			}
		})
	}
}

// TestRecordsUnions checks the checker records a construction's type, the
// type of a clause's binding, and whether a switch covers every case, and
// that the type table knows which unions are owned, have a zero value or
// compare with ==.
func TestRecordsUnions(t *testing.T) {
	file, diags := checkFile(t, withTypes(unionDecls, "e := E.int(1)", "switch e {",
		"case int(n):", "    use(n)", "case name, pair, eof:", "}", "switch e {",
		"case eof:", "default:", "}"))
	if len(diags) > 0 {
		t.Fatalf("reported %v", diags)
	}
	switches, built := switchesAndConstruction(file)
	union := file.Decls[0].(*ast.TypeDecl).Type
	if built == nil || ast.TypeOf(built) != union {
		t.Fatalf("construction %v has type %v, want E", built, ast.TypeOf(built))
	}
	if len(switches) != 3 || !switches[1].Exhaustive || switches[2].Exhaustive {
		t.Fatalf("switches covering every case not recorded: %v", switches)
	}
	if v := switches[1].Cases[0].Var; v == nil || v.Type != types.Int64 {
		t.Errorf("binding %v, want an int64", v)
	}
	checkUnionTable(t, file)
}

// switchesAndConstruction returns every switch of file, in order, and its
// first union construction written as a call.
func switchesAndConstruction(file *ast.File) ([]*ast.Switch, *ast.Call) {
	var switches []*ast.Switch
	var built *ast.Call
	ast.Walk(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Switch:
			switches = append(switches, x)
		case *ast.Call:
			if _, _, _, ok := ast.Construction(x); ok && built == nil {
				built = x
			}
		}
		return true
	})
	return switches, built
}

// tableFact is one thing a test expects a type table to say, and whether
// it does.
type tableFact struct {
	what string
	ok   bool
}

// checkUnionTable fails the test unless the table of file, which declares
// unionDecls, says E is no owned type, V, holding a list, is owned, and
// neither has a zero value, nor S, which holds E, and knows E's cases.
func checkUnionTable(t *testing.T, file *ast.File) {
	t.Helper()
	table := file.Types
	e, v := file.Decls[0].(*ast.TypeDecl).Type, file.Decls[1].(*ast.TypeDecl).Type
	s := file.Decls[2].(*ast.TypeDecl).Type
	facts := []tableFact{
		{
			"E is a union, no struct or array",
			table.IsUnion(e) && !table.IsStruct(e) && !table.IsArray(e),
		},
		{"E is not owned, V is", !table.Owned(e) && table.Owned(v)},
		{"no zero value", !table.HasZero(e) && !table.HasZero(v) && !table.HasZero(s)},
		{"S holds a union, int64 none", table.HoldsUnion(s) && !table.HoldsUnion(types.Int64)},
	}
	for _, f := range append(facts, caseFacts(table, e, v)...) {
		if !f.ok {
			t.Errorf("the table is wrong about: %s", f.what)
		}
	}
}

// caseFacts returns what the table should say about the cases of E and V,
// unionDecls' unions.
func caseFacts(table *types.Table, e, v types.Type) []tableFact {
	pair, at, hasPair := table.Case(e, "pair")
	eof, _, hasEOF := table.Case(e, "eof")
	return []tableFact{
		{"E.pair", hasPair && at == 2 && pair.Payload && table.Name(pair.Type) == "[2]int8"},
		{"E.eof", hasEOF && !eof.Payload},
		{"names and counts", table.Name(e) == "E" && len(table.Cases(v)) == 3},
	}
}

// TestUnionMessagesNameTheUnion checks a message about building or
// matching a union names the union, as the code being checked names it.
func TestUnionMessagesNameTheUnion(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{withTypes(unionDecls, "e := E.nope", "use(len(\"\"))", "if e == e {", "}"), "E has no case nope"},
		{
			withTypes(unionDecls, "e := E.int(\"x\")", "if e == e {", "}"),
			"cannot use string as int64 in argument to E.int",
		},
		{
			withTypes(unionDecls, "e := E.eof", "switch e {", "case int(n):", "    use(n)", "}"),
			"switch on E misses cases name, pair, eof; cover them or add a default",
		},
		{withTypes(unionDecls, "v := V.none", "if v == v {", "}"), "operator == is not defined on V"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			diags := check(t, tc.src)
			if len(diags) == 0 || !strings.Contains(diags[0].Message, tc.want) {
				t.Errorf("reported %v, want %q", diags, tc.want)
			}
		})
	}
}

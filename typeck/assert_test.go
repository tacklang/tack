package typeck_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// anyFunc returns a program declaring P, a struct, one, taking an any,
// and f(a any, xs ...any), whose body is lines, with no result.
func anyFunc(lines ...string) string {
	return "package main\n\ntype P struct {\n    d int64\n}\n\n" +
		"func one(a any) {\n}\n\n" +
		"func f(a any, xs ...any) {\n    " + strings.Join(lines, "\n    ") + "\n}\n\n" +
		"func main() {\n}\n"
}

// assertAcceptedCases are type assertions and type switches the checker
// accepts.
var assertAcceptedCases = []struct{ name, src string }{
	{"every type asserted", anyFunc(
		"v1, ok1 := a.(int8)", "v2, ok2 := a.(int16)", "v3, ok3 := a.(int32)",
		"v4, ok4 := a.(int64)", "v5, ok5 := a.(uint8)", "v6, ok6 := a.(uint16)",
		"v7, ok7 := a.(uint32)", "v8, ok8 := a.(uint64)", "v9, ok9 := a.(unsafe_size_t)",
		"v10, ok10 := a.(unsafe_uintptr_t)", "v11, ok11 := a.(bool)", "v12, ok12 := a.(string)",
		"one(v1)", "one(v2)", "one(v3)", "one(v4)", "one(v5)", "one(v6)", "one(v7)", "one(v8)",
		"one(v9)", "one(v10)", "one(v11)", "one(v12)", "one(ok1)", "one(ok2)", "one(ok3)",
		"one(ok4)", "one(ok5)", "one(ok6)", "one(ok7)", "one(ok8)", "one(ok9)", "one(ok10)",
		"one(ok11)", "one(ok12)",
	)},
	{"every type in a switch", anyFunc(
		"switch v := a.(type) {",
		"case int8, int16, int32:", "    one(v)",
		"case int64:", "    one(v + 1)",
		"case uint8, uint16, uint32, uint64, unsafe_size_t, unsafe_uintptr_t:", "    one(v)",
		"case bool:", "    one(!v)",
		"case string:", "    one(len(v))",
		"}",
	)},
	{"blanks and a mutable ok", anyFunc(
		"v, _ := a.(int64)", "_, ok := a.(bool)", "_, mut more := a.(string)", "more = !more",
		"one(v)", "one(ok)", "one(more)",
	)},
	{"assigned, widened", anyFunc(
		"mut n int64 = 0", "mut ok bool = false", "n, ok = a.(int8)", "n, ok = a.(uint32)",
		"_, ok = a.(string)", "n, _ = a.(int16)", "one(n)", "one(ok)",
	)},
	{"any locals", anyFunc(
		"b := a", "c, ok := xs[0]", "for _, x := range xs {", "    one(x)", "}",
		"if ok {", "    one(c)", "}",
		"switch v := b.(type) {", "default:", "    w := v", "    one(w)", "}",
	)},
	{"switch with no binding, empty and default only", anyFunc(
		"switch a.(type) {", "}",
		"switch a.(type) {", "default:", "}",
		"switch v := a.(type) {", "default:", "    one(v)", "}",
		"switch a.(type) {", "case int64:", "    one(1)", "case string:", "}",
	)},
	{"binding used in one case only", anyFunc(
		"switch v := a.(type) {", "case int64:", "case string:", "    one(v)", "default:", "}",
	)},
	{"nested and on locals", anyFunc(
		"for i := range xs {",
		"    switch v := xs[i].(type) {",
		"    case int64, string:",
		"        switch w := v.(type) {",
		"        case int64:",
		"            if w > 0 {", "                break", "            }",
		"            continue",
		"        }",
		"    case bool:", "        one(v)",
		"    }",
		"}",
	)},
	{"defers read any locals", anyFunc(
		"b := a",
		"defer {", "    switch v := b.(type) {", "    case int64:", "        one(v)", "    }", "}",
		"defer {", "    w, ok := b.(string)", "    if ok {", "        one(w)", "    }", "}",
	)},
	{"facts inside a case", anyFunc(
		"switch v := a.(type) {",
		"case int64:",
		"    if v != 0 {", "        one(10 / v)", "    }",
		"case string:",
		"    if len(v) > 0 {", "        one(v[0])", "    }",
		"}",
	)},
	{"facts after an assertion", anyFunc(
		"n, ok := a.(int64)", "if !ok || n == 0 {", "    return", "}", "one(10 / n)",
	)},
	{
		"switch terminates",
		"package main\n\nfunc f(a any) int64 {\n    switch v := a.(type) {\n    case int64:\n" +
			"        return v\n    default:\n        return 0\n    }\n}\n\nfunc main() {\n}\n",
	},
	{
		"in a function returning an error",
		"package main\n\nfunc f(a any) (int64, error) {\n    n, ok := a.(int64)\n    if !ok {\n" +
			"        return 0, error{code: 1, msg: \"no\"}\n    }\n    return n, noerror\n}\n\n" +
			"func main() {\n    n, err := f(1)\n    if err.code == 0 && n == 1 {\n        exit(0)\n    }\n}\n",
	},
}

// TestAcceptsTypeAssertions checks each of assertAcceptedCases has no
// diagnostics.
func TestAcceptsTypeAssertions(t *testing.T) {
	for _, tt := range assertAcceptedCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// assertErrorCases are misuses of type assertions and type switches, each
// with the diagnostics it gives, in order.
var assertErrorCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"one value",
		anyFunc("x := a.(int64)", "one(x)"),
		[]string{"a type assertion needs two results; write v, ok := a.(int64)"},
	},
	{
		"as an argument",
		anyFunc("if len(xs) > 0 {", "    one(xs[0].(string))", "}"),
		[]string{"a type assertion needs two results; write v, ok := xs[0].(string)"},
	},
	{
		"as a condition",
		anyFunc("if a.(bool) {", "}"),
		[]string{"a type assertion needs two results; write v, ok := a.(bool)"},
	},
	{
		"in a call bound with ok",
		"package main\n\nfunc g(n int64) (int64, bool) {\n    return n, true\n}\n\n" +
			"func f(a any) {\n    v, ok := g(a.(int64))\n    f(v)\n    f(ok)\n}\n\nfunc main() {\n}\n",
		[]string{"a type assertion needs two results; write v, ok := a.(int64)"},
	},
	{
		"not the whole value",
		anyFunc("v, ok := a.(int64) + 1", "one(v)", "one(ok)"),
		[]string{
			"the checked form must be the whole value: v, ok := a.(int64), then compute v + 1",
		},
	},
	{
		"three names",
		anyFunc("v, ok, w := a.(int64)", "one(v)", "one(ok)", "one(w)"),
		[]string{"assignment mismatch: 3 variables but a type assertion gives 2 values"},
	},
	{
		"assigned a type it does not widen to",
		anyFunc(
			"mut n int64 = 0",
			"mut ok bool = false",
			"n, ok = a.(string)",
			"one(n)",
			"one(ok)",
		),
		[]string{"cannot use string as int64 in assignment to n"},
	},
	{
		"assigned to an immutable variable",
		anyFunc("n := 0", "ok := false", "n, ok = a.(int64)", "one(n)", "one(ok)"),
		[]string{"cannot assign to immutable variable n", "cannot assign to immutable variable ok"},
	},
	{
		"a struct", anyFunc("v, ok := a.(P)", "one(v)", "one(ok)"),
		[]string{"an any never holds P: only the builtin types can be passed as any for now"},
	},
	{
		"never-held types",
		anyFunc(
			"v1, ok1 := a.([2]int64)", "v2, ok2 := a.([]int64)", "v3, ok3 := a.(list[int64])",
			"v4, ok4 := a.(error)", "v5, ok5 := a.(Allocator)", "v6, ok6 := a.(any)",
			"one(v1)", "one(v2)", "one(v3)", "one(v4)", "one(v5)", "one(v6)", "one(ok1)",
			"one(ok2)", "one(ok3)", "one(ok4)", "one(ok5)", "one(ok6)",
		),
		[]string{
			"an any never holds [2]int64: only the builtin types can be passed as any for now",
			"an any never holds []int64: only the builtin types can be passed as any for now",
			"an any never holds list[int64]: only the builtin types can be passed as any for now",
			"an any never holds error: only the builtin types can be passed as any for now",
			"an any never holds Allocator: only the builtin types can be passed as any for now",
			"an any never holds any: only the builtin types can be passed as any for now",
		},
	},
	{
		"unknown type", anyFunc("v, ok := a.(Nope)", "one(v)", "one(ok)"),
		[]string{"unknown type: Nope"},
	},
	{
		"not an any", anyFunc("n := 1", "v, ok := n.(int64)", "one(v)", "one(ok)"),
		[]string{"cannot assert on n: it is int64, not any"},
	},
	{
		"a constant", anyFunc("v, ok := \"s\".(string)", "one(v)", "one(ok)"),
		[]string{"cannot assert on \"s\": it is string, not any"},
	},
	{
		"a view", anyFunc("v, ok := xs.(int64)", "one(v)", "one(ok)"),
		[]string{"cannot assert on xs: it is []any, not any"},
	},
	{
		"not an any and a bad type",
		anyFunc("n := 1", "v, ok := n.(P)", "one(v)", "one(ok)"),
		[]string{
			"cannot assert on n: it is int64, not any",
			"an any never holds P: only the builtin types can be passed as any for now",
		},
	},
	{
		".(type) declared", anyFunc("x := a.(type)", "one(x)"),
		[]string{".(type) can only be the tag of a switch"},
	},
	{
		".(type) bound with ok", anyFunc("v, ok := a.(type)", "one(v)", "one(ok)"),
		[]string{".(type) can only be the tag of a switch"},
	},
	{
		".(type) in an argument", anyFunc("one(a.(type))"),
		[]string{".(type) can only be the tag of a switch"},
	},
	{
		".(type) in a larger tag", anyFunc("switch a.(type) == 1 {", "}"),
		[]string{".(type) can only be the tag of a switch"},
	},
	{
		".(type) as a case value", anyFunc("switch {", "case a.(type):", "}"),
		[]string{".(type) can only be the tag of a switch"},
	},
	{
		".(type) of a non-any", anyFunc("n := 1", "x := n.(type)", "one(x)"),
		[]string{".(type) can only be the tag of a switch"},
	},
	{
		"switch on a non-any",
		anyFunc("n := 1", "switch v := n.(type) {", "case int64:", "    one(v)", "}"),
		[]string{"cannot assert on n: it is int64, not any"},
	},
	{
		"type twice in two cases",
		anyFunc("switch a.(type) {", "case int64, string:", "case bool, int64:", "}"),
		[]string{"int64 appears twice in this type switch"},
	},
	{
		"type twice in one case",
		anyFunc("switch a.(type) {", "case string, string:", "}"),
		[]string{"string appears twice in this type switch"},
	},
	{
		"never-held type in a case",
		anyFunc("switch a.(type) {", "case P, [1]bool:", "}"),
		[]string{
			"an any never holds P: only the builtin types can be passed as any for now",
			"an any never holds [1]bool: only the builtin types can be passed as any for now",
		},
	},
	{
		"one-type case variable used as another type",
		anyFunc("switch v := a.(type) {", "case string:", "    one(v + 1)", "}"),
		[]string{"operator + is not defined on string"},
	},
	{
		"multi-type case variable is any",
		anyFunc("switch v := a.(type) {", "case int8, int16:", "    one(v + 1)", "}"),
		[]string{"operator + is not defined on any"},
	},
	{
		"default variable is any",
		anyFunc("switch v := a.(type) {", "default:", "    x := int64(v)", "    one(x)", "}"),
		[]string{"cannot convert any to int64"},
	},
	{
		"case variable is immutable",
		anyFunc("switch v := a.(type) {", "case int64:", "    v = 2", "    one(v)", "}"),
		[]string{"cannot assign to immutable variable v"},
	},
	{
		"mut any local from a case",
		anyFunc("switch v := a.(type) {", "default:", "    mut w := v", "    one(w)", "}"),
		[]string{"cannot declare mut w as any; an any variable cannot be reassigned"},
	},
	{
		"mut any local from a range",
		"package main\n\nfunc f(xs ...any) {\n    for _, x := range xs {\n        mut y := x\n" +
			"        f(y)\n    }\n}\n\nfunc main() {\n}\n",
		[]string{"cannot declare mut y as any; an any variable cannot be reassigned"},
	},
	{
		"divisor from a case unproven",
		anyFunc("switch v := a.(type) {", "case int64:", "    one(10 / v)", "}"),
		[]string{"divisor v may be zero; check v != 0 first, or use q, ok := ... / v"},
	},
	{
		"type switch proves nothing after it",
		anyFunc(
			"n := len(xs)",
			"switch a.(type) {",
			"case int64:",
			"    return",
			"}",
			"one(10 / n)",
		),
		[]string{"divisor n may be zero; check n != 0 first, or use q, ok := ... / n"},
	},
	{
		"switch does not terminate without a default",
		"package main\n\nfunc f(a any) int64 {\n    switch a.(type) {\n    case int64:\n" +
			"        return 1\n    }\n}\n\nfunc main() {\n}\n",
		[]string{"f must end in a return"},
	},
}

// TestRejectsTypeAssertionMisuse checks each of assertErrorCases gives
// exactly its diagnostics.
func TestRejectsTypeAssertionMisuse(t *testing.T) {
	for _, tt := range assertErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			got := make([]string, len(diags))
			for i, d := range diags {
				got[i] = d.Message
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRecordsTypeSwitchTypes checks the types the checker records: an
// assertion's asserted type, each case's types, and each case variable's,
// the case's one type or any.
func TestRecordsTypeSwitchTypes(t *testing.T) {
	file, diags := checkTree(t, anyFunc(
		"n, ok := a.(uint16)", "one(n)", "one(ok)",
		"switch v := a.(type) {",
		"case string:", "    one(v)",
		"case int8, bool:", "    one(v)",
		"default:", "    one(v)",
		"}",
	))
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	body := funcBody(file, "f")
	m := body[0].(*ast.MultiVar)
	if a := m.Value.(*ast.TypeAssert); a.Type != types.Uint16 || ast.TypeOf(a) != types.Uint16 {
		t.Errorf("assertion records %v", a.Type)
	}
	if m.Vars[0].Type != types.Uint16 || m.Vars[1].Type != types.Bool {
		t.Errorf("n and ok are %v and %v", m.Vars[0].Type, m.Vars[1].Type)
	}
	s := body[3].(*ast.Switch)
	wantTypes := [][]types.Type{{types.String}, {types.Int8, types.Bool}, nil}
	wantVars := []types.Type{types.String, types.Any, types.Any}
	for i, cl := range s.Cases {
		if !slices.Equal(cl.Types, wantTypes[i]) {
			t.Errorf("case %d types %v, want %v", i, cl.Types, wantTypes[i])
		}
		if cl.Var.Type != wantVars[i] {
			t.Errorf("case %d variable is %v, want %v", i, cl.Var.Type, wantVars[i])
		}
	}
}

// funcBody returns the body of the function named name in file.
func funcBody(file *ast.File, name string) []ast.Stmt {
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.Func); ok && fn.Name == name {
			return fn.Body
		}
	}
	return nil
}

// TestDeeplyNestedTypeSwitchesCheckInLinearTime checks type switches
// nested hundreds deep, each switching on the variable the default of the
// one around it binds, check without error and quickly: nothing is
// checked more than once per level.
func TestDeeplyNestedTypeSwitchesCheckInLinearTime(t *testing.T) {
	const depth = 400
	var b strings.Builder
	b.WriteString("package main\n\nfunc one(a any) {\n}\n\nfunc f(a any) {\n    v0 := a\n")
	for i := range depth {
		b.WriteString(
			strings.Repeat(" ", 4) + "switch v" + strconv.Itoa(i+1) + " := v" + strconv.Itoa(i) +
				".(type) {\ncase int64:\n    one(v" + strconv.Itoa(i+1) + ")\ndefault:\n",
		)
	}
	b.WriteString("one(v" + strconv.Itoa(depth) + ")\n")
	b.WriteString(strings.Repeat("}\n", depth))
	b.WriteString("}\n\nfunc main() {\n}\n")
	if _, diags := checkTree(t, b.String()); len(diags) > 0 {
		t.Fatalf("got %v", diags[0])
	}
}

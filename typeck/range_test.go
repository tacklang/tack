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
		"two names over a count",
		inMain("for i, x := range 3 {\n        exit(i +% x)\n    }"),
		[]spanned{
			{
				"assignment mismatch: 2 variables but range over int64 gives 1 value",
				"i, x := range 3",
			},
		},
	},
	{
		"two names over a typed count",
		inMain("var n uint8 = 3\n    for _, x := range n {\n        exit(x)\n    }"),
		[]spanned{
			{
				"assignment mismatch: 2 variables but range over uint8 gives 1 value",
				"_, x := range n",
			},
		},
	},
	{
		"two names over a bool",
		inMain("for i, x := range true {\n        exit(i +% x)\n    }"),
		[]spanned{{"cannot range over bool", "true"}},
	},
	{
		"several results",
		"package main\n\nfunc two() (int64, int64) {\n    return 1, 2\n}\n\nfunc main() {\n    for i, x := range two() {\n        exit(i +% x)\n    }\n}\n",
		[]spanned{{"two returns 2 values", "two()"}},
	},
	{
		"element assigned",
		inMain("for _, x := range \"ab\" {\n        x = 'c'\n        exit(x)\n    }"),
		[]spanned{{"cannot assign to immutable variable x", "x"}},
	},
	{
		"element compound assigned",
		inMain(
			"for _, x := range [2]int64{1, 2} {\n        x *= 2\n        code, fits := int32(x)\n        if fits { exit(code) }\n    }",
		),
		[]spanned{{"cannot assign to immutable variable x", "x"}},
	},
	{
		"element incremented",
		inMain("for _, x := range \"ab\" {\n        x++\n        exit(x)\n    }"),
		[]spanned{{"cannot assign to immutable variable x", "x"}},
	},
	{
		"element's element assigned",
		inMain(
			"for _, row := range [2][2]int64{{1, 2}, {3, 4}} {\n        row[0] = 1\n        code, fits := int32(row[0])\n        if fits { exit(code) }\n    }",
		),
		[]spanned{{"cannot assign to element of immutable variable row", "row"}},
	},
	{
		"array index assigned",
		inMain(
			"for i, x := range [2]int64{1, 2} {\n        i = 1\n        code, fits := int32(i +% x)\n        if fits { exit(code) }\n    }",
		),
		[]spanned{{"cannot assign to immutable variable i", "i"}},
	},
	{
		"index assigned with several",
		"package main\n\nfunc two() (int64, int64) {\n    return 1, 2\n}\n\nfunc main() {\n    for i, x := range \"ab\" {\n        mut y := 0\n        y, i = two()\n        code, fits := int32(i +% x +% y)\n        if fits { exit(code) }\n    }\n}\n",
		[]spanned{{"cannot assign to immutable variable i", "i"}},
	},
	{
		"byte is not an int8",
		inMain("for _, x := range \"ab\" {\n        var y int8 = x\n        exit(y)\n    }"),
		[]spanned{{"cannot use uint8 as int8 in declaration of y", "x"}},
	},
	{
		"element of a bool array is a bool",
		inMain("for _, x := range [1]bool{true} {\n        exit(x)\n    }"),
		[]spanned{{"cannot use bool as int32 in argument to exit", "x"}},
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
		inMain(
			"for i := range 3 {\n        i +%= 1\n        code, fits := int32(i)\n        if fits { exit(code) }\n    }",
		),
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
		[]spanned{{"cannot use string as int32 in argument to exit", "\"s\""}},
	},
}

// TestRangeLoops checks the count is an int, the variable an immutable int,
// and the body a loop body.
func TestRangeLoops(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for i := range 3 {\n        if i == 1 {\n            continue\n        }\n        code, fits := int32(i +% 1)\n        if fits { exit(code) }\n    }\n}\n"
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	if v := file.Decls[0].(*ast.Func).Body[0].(*ast.ForRange).Var; v.Type != types.Int64 {
		t.Errorf("variable type = %v, want int64", v.Type)
	}

	forever := "package main\n\nfunc f() int64 {\n    for range 3 {\n        return 1\n    }\n}\n\nfunc main() {\n    code, fits := int32(f())\n    if fits { exit(code) }\n}\n"
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

// rangeTypeCases are range loops over each kind of value, with the type
// each gives its index and its element variable.
var rangeTypeCases = []struct {
	name, src   string
	index, elem string
}{
	{
		"array",
		"a := [3]int64{1, 2, 3}\n    for i, x := range a {\n        code, fits := int32(i +% x)\n        if fits { exit(code) }\n    }",
		"int64",
		"int64",
	},
	{
		"array of bytes",
		"for i, x := range [2]uint8{1, 2} {\n        code, fits := int32(i +% x)\n        if fits { exit(code) }\n    }",
		"int64",
		"uint8",
	},
	{
		"array of arrays",
		"for i, row := range [2][3]int8{{1, 2, 3}, {4, 5, 6}} {\n        code, fits := int32(i + row[0])\n        if fits { exit(code) }\n    }",
		"int64",
		"[3]int8",
	},
	{
		"array of strings",
		"for i, x := range [1]string{\"a\"} {\n        print(x)\n        code, fits := int32(i)\n        if fits { exit(code) }\n    }",
		"int64",
		"string",
	},
	{
		"string literal",
		"for i, b := range \"ab\" {\n        code, fits := int32(i + b)\n        if fits { exit(code) }\n    }",
		"int64",
		"uint8",
	},
	{
		"string variable",
		"s := \"ab\"\n    for i, b := range s {\n        code, fits := int32(i + b)\n        if fits { exit(code) }\n    }",
		"int64",
		"uint8",
	},
	{
		"empty array",
		"for i, x := range [0]int64{} {\n        code, fits := int32(i +% x)\n        if fits { exit(code) }\n    }",
		"int64",
		"int64",
	},
	{"typed count", "var n int16 = 3\n    for i := range n {\n        exit(i)\n    }", "int16", ""},
	{"count read from a string", "for i := range \"ab\"[0] {\n        exit(i)\n    }", "uint8", ""},
	{"blank index", "for _, x := range \"ab\" {\n        exit(x)\n    }", "int64", "uint8"},
}

// TestRangeTypes checks each of rangeTypeCases is accepted and gives its
// variables the types it lists.
func TestRangeTypes(t *testing.T) {
	for _, tt := range rangeTypeCases {
		t.Run(tt.name, func(t *testing.T) {
			src := inMain(tt.src)
			file, diags := checkTree(t, src)
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
			var loop *ast.ForRange
			for _, stmt := range file.Decls[0].(*ast.Func).Body {
				if l, ok := stmt.(*ast.ForRange); ok {
					loop = l
				}
			}
			if got := file.Types.Name(loop.Var.Type); got != tt.index {
				t.Errorf("index type = %s, want %s", got, tt.index)
			}
			elem := ""
			if loop.Elem != nil {
				elem = file.Types.Name(loop.Elem.Type)
			}
			if elem != tt.elem {
				t.Errorf("element type = %s, want %s", elem, tt.elem)
			}
		})
	}
}

// provenRangeIndexCases are indexes a range over an array or a string
// proves in range, and divisions its body proves safe, each by one of the
// rules.
var provenRangeIndexCases = []struct{ name, src string }{
	{"array read", indexing("for k := range a {", "    return a[k]", "}")},
	{"array write", indexing("for k, x := range a {", "    m[k] = x", "}")},
	{"array of the same length", indexing("for k := range m {", "    m[k] = a[k]", "}")},
	{"shorter array", indexing("for k := range [2]int64{1, 2} {", "    m[k] = a[k]", "}")},
	{
		"array the body writes",
		indexing("for k := range m {", "    m[k] = 1", "    m = a", "    m[k] = 2", "}"),
	},
	{"ranged call", indexing("for k := range arr() {", "    return a[k]", "}") + arrFunc},
	{
		"empty array, so the body never runs",
		indexing("for k := range [0]int64{} {", "    return a[k]", "}"),
	},
	{
		"nested over rows",
		indexing(
			"grid := [2][3]int64{{1, 2, 3}, {4, 5, 6}}",
			"for r := range grid {",
			"    for c := range grid[r] {",
			"        m[c] = grid[r][c]",
			"    }",
			"}",
		),
	},
	{
		"byte element as an index",
		indexing(
			"mut seen [256]bool",
			"for _, b := range \"ab\" {",
			"    seen[b] = true",
			"}",
			"if seen[97] {",
			"    return 1",
			"}",
		),
	},
	{
		"element proven not zero",
		indexing("for _, x := range a {", "    if x != 0 {", "        return 6 / x", "    }", "}"),
	},
	{
		"index guarded above zero",
		indexing("for k := range a {", "    if k > 0 {", "        return 6 / k", "    }", "}"),
	},
	{"string parameter", stringing("for j := range s {", "    return s[j]", "}")},
	{
		"string parameter, both names",
		stringing("for j, c := range s {", "    if s[j] == c {", "        return c", "    }", "}"),
	},
	{"mutable string not reassigned", stringing("for j := range t {", "    return t[j]", "}")},
	{"immutable global", stringing("for j := range k {", "    return k[j]", "}")},
	{"literal", stringing("for j := range \"tack\" {", "    return \"tack\"[j]", "}")},
	{
		"literal indexes a constant array",
		indexing("for k := range \"abc\" {", "    return a[k]", "}"),
	},
	{
		"string reassigned after the loop",
		stringing(
			"for j := range t {",
			"    if t[j] == 'a' {",
			"        return 1",
			"    }",
			"}",
			"t = \"\"",
		),
	},
	{
		"nested over the same string",
		stringing(
			"for j := range s {",
			"    for l := range s {",
			"        if s[j] == s[l] {",
			"            return s[l]",
			"        }",
			"    }",
			"}",
		),
	},
	{
		"byte compared with a character",
		stringing(
			"for _, c := range s {",
			"    if c >= 'a' && c <= 'z' {",
			"        return c",
			"    }",
			"}",
		),
	},
	{
		"checked index into the ranged array",
		inMain(
			"a := [2]int64{1, 2}\n    for i := range a {\n        v, ok := a[i]\n        if ok {\n            code, fits := int32(v)\n            if fits { exit(code) }\n        }\n    }",
		),
	},
	{
		"checked index into the ranged string",
		inMain(
			"s := \"ab\"\n    for i := range s {\n        v, ok := s[i]\n        if ok {\n            exit(v)\n        }\n    }",
		),
	},
}

// arrFunc declares arr, which returns an array of three, for the cases that
// range over a call.
const arrFunc = "\nfunc arr() [3]int64 {\n    return [3]int64{1, 2, 3}\n}\n"

// TestAcceptsProvenRangeIndexes checks each of provenRangeIndexCases has no
// diagnostics.
func TestAcceptsProvenRangeIndexes(t *testing.T) {
	for _, tt := range provenRangeIndexCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unprovenRangeIndexCases are indexes and divisions in a range loop's body
// that it does not prove, with the diagnostic each must produce.
var unprovenRangeIndexCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"longer array",
		indexing("for k := range [4]int64{1, 2, 3, 4} {", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check 0 <= k && k < 3 first, or use v, ok := ...[k]",
			"k",
		},
	},
	{
		"index divides",
		indexing("for k := range a {", "    return 6 / k", "}"),
		spanned{"divisor k may be zero; check k != 0 first, or use q, ok := ... / k", "k"},
	},
	{
		"element divides",
		indexing("for _, x := range a {", "    return 6 / x", "}"),
		spanned{"divisor x may be zero; check x != 0 first, or use q, ok := ... / x", "x"},
	},
	{
		"another string",
		stringing("for j := range s {", "    return t[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(t) first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"string reassigned in the body",
		stringing("for j := range t {", "    t = \"\"", "    return t[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(t) first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"string reassigned later in the body",
		stringing("for j := range t {", "    if t[j] == 'a' {", "        t = \"x\"", "    }", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(t) first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"mutable global",
		stringing("for j := range g {", "    return g[j]", "}"),
		spanned{"index j may be out of range; use v, ok := ...[j]", "j"},
	},
	{
		"ranged call",
		stringing("for j := range name() {", "    return s[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(s) first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"longer literal",
		stringing("for j := range \"tacks\" {", "    return \"tack\"[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < 4 first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"string variable indexes an array",
		indexing("mut w := \"abc\"", "for k := range w {", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check 0 <= k && k < 3 first, or use v, ok := ...[k]",
			"k",
		},
	},
	{
		"index used after an inner loop over another string",
		stringing(
			"for j := range s {",
			"    for l := range t {",
			"        return s[l]",
			"    }",
			"    return s[j]",
			"}",
		),
		spanned{
			"index l may be out of range; check 0 <= l && l < len(s) first, or use v, ok := ...[l]",
			"l",
		},
	},
}

// TestRejectsUnprovenRangeIndexes checks each of unprovenRangeIndexCases
// gives exactly its diagnostic.
func TestRejectsUnprovenRangeIndexes(t *testing.T) {
	for _, tt := range unprovenRangeIndexCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want one diagnostic", diags)
			}
			if diags[0].Message != tt.want.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want.msg)
			}
			if got := tt.src[diags[0].Span.Start:diags[0].Span.End]; got != tt.want.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.want.at)
			}
		})
	}
}

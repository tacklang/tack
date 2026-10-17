package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// errorProgram returns a program declaring parse, returning an int64 and
// an error, divide, returning two int64s and an error, ready, returning an
// error alone, and bump, taking a mut int64, whose main's body is lines.
func errorProgram(lines ...string) string {
	return "package main\n\n" +
		"func parse(s string) (int64, error) {\n    if len(s) == 0 {\n" +
		"        return 0, error{code: 1, msg: \"empty\"}\n    }\n    return 1, noerror\n}\n\n" +
		"func divide(a int64, b int64) (int64, int64, error) {\n    if b == 0 {\n" +
		"        return 0, 0, error{code: 2, msg: \"zero\"}\n    }\n    return a / b, a % b, noerror\n}\n\n" +
		"func ready() error {\n    return noerror\n}\n\n" +
		"func bump(mut n int64) {\n    n++\n}\n\n" +
		"func use(n int64) {\n}\n\n" +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// ownedErrorProgram is errorProgram with two more functions returning an
// error beside owned results, which a binding cannot drop with _: owned,
// a list and a copyable struct, and several, lists and a struct holding
// one around an int64.
func ownedErrorProgram(lines ...string) string {
	return errorProgram(lines...) + "\ntype Copied struct {\n    n int64\n}\n\n" +
		"type Holder struct {\n    l list[int64]\n}\n\n" +
		"func owned() (list[int64], Copied, error) {\n" +
		"    return make(list[int64], c_allocator), Copied{n: 1}, noerror\n}\n\n" +
		"func several() (list[int64], int64, Holder, list[uint8], list[int64], error) {\n" +
		"    return make(list[int64], c_allocator), 1, Holder{l: make(list[int64], c_allocator)}, " +
		"make(list[uint8], c_allocator), make(list[int64], c_allocator), noerror\n}\n"
}

// errorAcceptedCases are uses of errors the checker accepts, each check
// one of the conditions that proves err.code == 0 where v is read.
var errorAcceptedCases = []struct{ name, src string }{
	{
		"early return",
		errorProgram(`v, err := parse("1")`, "if err.code != 0 {", "    return", "}", "use(v)"),
	},
	{"then branch", errorProgram(`v, err := parse("1")`, "if err.code == 0 {", "    use(v)", "}")},
	{
		"else branch",
		errorProgram(`v, err := parse("1")`, "if err.code != 0 {", "} else {", "    use(v)", "}"),
	},
	{"negated", errorProgram(`v, err := parse("1")`, "if !(err.code != 0) {", "    use(v)", "}")},
	{
		"zero on the left",
		errorProgram(`v, err := parse("1")`, "if 0 == err.code {", "    use(v)", "}"),
	},
	{
		"known zero",
		errorProgram(
			`v, err := parse("1")`,
			"zero := 0",
			"if err.code == zero {",
			"    use(v)",
			"}",
		),
	},
	{
		"both bounds",
		errorProgram(
			`v, err := parse("1")`,
			"if err.code >= 0 && err.code <= 0 {",
			"    use(v)",
			"}",
		),
	},
	{
		"either side leaves",
		errorProgram(
			`v, err := parse("1")`,
			"if err.code < 0 || err.code > 0 {",
			"    return",
			"}",
			"use(v)",
		),
	},
	{
		"right of &&", errorProgram(`v, err := parse("1")`, "if err.code == 0 && v > 0 {", "}"),
	},
	{
		"right of ||", errorProgram(`v, err := parse("1")`, "if err.code != 0 || v > 0 {", "}"),
	},
	{
		"case 0",
		errorProgram(
			`v, err := parse("1")`,
			"switch err.code {",
			"case 0:",
			"    use(v)",
			"case 1:",
			"}",
		),
	},
	{
		"after a switch",
		errorProgram(
			`v, err := parse("1")`,
			"switch err.code {",
			"case 0:",
			"default:",
			"    return",
			"}",
			"use(v)",
		),
	},
	{
		"loop condition",
		errorProgram(`v, err := parse("1")`, "for err.code == 0 {", "    use(v)", "    break", "}"),
	},
	{
		"defer after the check",
		errorProgram(
			`v, err := parse("1")`,
			"if err.code != 0 {",
			"    return",
			"}",
			"defer {",
			"    use(v)",
			"}",
		),
	},
	{
		"rebinding",
		errorProgram(
			`mut v, mut err := parse("1")`, "if err.code == 0 {", "    use(v)", "}",
			`v, err = parse("")`, "if err.code != 0 {", "    return", "}", "use(v)",
		),
	},
	{
		"three results",
		errorProgram(
			"q, r, err := divide(7, 2)",
			"if err.code != 0 {",
			"    return",
			"}",
			"use(q + r)",
		),
	},
	{
		"held by two errors",
		errorProgram(
			`mut v, e1 := parse("1")`, "if e1.code != 0 {", "    return", "}", "mut e2 error",
			`v, e2 = parse("2")`, "if e2.code != 0 {", "    return", "}", "use(v)",
		),
	},
	{
		"the error itself",
		errorProgram(
			`v, err := parse("1")`,
			"print(err.msg)",
			"if err.code == 0 {",
			"    use(v)",
			"}",
		),
	},
	{"blank companion", errorProgram(`_, err := parse("1")`, "print(err.msg)")},
	{
		"error alone as a value",
		errorProgram("err := ready()", "e := ready().code", "use(e)", "print(err.msg)"),
	},
	{
		"v assigned whole",
		errorProgram(`mut v, err := parse("1")`, "v = 5", "if err.code == 0 {", "    use(v)", "}"),
	},
	{
		"unheld error assigned",
		errorProgram(
			"mut e error",
			"e = error{code: 3, msg: \"three\"}",
			"e.code = 4",
			"e.code++",
			"print(e.msg)",
		),
	},
	{
		"error results passed on",
		"package main\n\nfunc g() (int64, error) {\n    return 2, noerror\n}\n\n" +
			"func f() (int64, error) {\n    v, err := g()\n    if err.code != 0 {\n        return 0, err\n    }\n" +
			"    return v, noerror\n}\n\nfunc main() {\n    _, err := f()\n    print(err.msg)\n}\n",
	},
	{
		"extern",
		"package main\n\nextern func f() (int64, error)\n\nfunc main() {\n    v, err := f()\n" +
			"    if err.code == 0 && v > 0 {\n        exit(1)\n    }\n}\n",
	},
	{
		"comparisons and fields",
		"package main\n\nvar none = noerror\n\nmut last error\n\ntype P struct {\n    err error\n}\n\n" +
			"func main() {\n    p := P{err: none}\n    a := [2]error{noerror, error{code: 1, msg: \"x\"}}\n" +
			"    last = a[1]\n    if p.err == none && a[0] != last && last.code == 1 {\n        print(p.err.msg)\n    }\n" +
			"    switch last {\n    case none:\n    }\n}\n",
	},
}

// TestAcceptsErrors checks each of errorAcceptedCases has no diagnostics.
func TestAcceptsErrors(t *testing.T) {
	for _, tt := range errorAcceptedCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// readBeforeCheck is the error for reading v before err is checked.
const readBeforeCheck = "v cannot be read before err is checked; check err.code == 0 first"

// heldAlone is the error for assigning err, which holds v, without it.
const heldAlone = "err holds the error that came with v, so only a binding that also assigns v can assign it"

// errorRejectedCases are misuses of errors, each with the one diagnostic it
// gives.
var errorRejectedCases = []struct{ name, src, want string }{
	{
		"call statement", errorProgram(`parse("1")`),
		"the error parse returns cannot be dropped; bind it, as in _, err := parse(...)",
	},
	{
		"call statement of three results", errorProgram("divide(1, 2)"),
		"the error divide returns cannot be dropped; bind it, as in _, _, err := divide(...)",
	},
	{
		"call statement with an owned result",
		ownedErrorProgram("owned()"),
		"the error owned returns cannot be dropped; bind it, as in x, _, err := owned(...)",
	},
	{
		"call statement with owned results around a copyable one",
		ownedErrorProgram("several()"),
		"the error several returns cannot be dropped; bind it, as in x, _, y, z, x4, err := several(...)",
	},
	{
		"call statement of an error alone", errorProgram("ready()"),
		"the error ready returns cannot be dropped; bind it, as in err := ready(...)",
	},
	{
		"blank error", errorProgram(`v, _ := parse("1")`, "use(v)"),
		"the error parse returns cannot be dropped; bind it to a variable instead of _",
	},
	{
		"every name blank", errorProgram(`_, _, _ := divide(1, 2)`),
		"the error divide returns cannot be dropped; bind it to a variable instead of _",
	},
	{
		"unchecked",
		errorProgram(`v, err := parse("1")`, "print(err.msg)", "use(v)"),
		readBeforeCheck,
	},
	{
		"check that does not leave",
		errorProgram(
			`v, err := parse("1")`,
			"if err.code != 0 {",
			"    print(err.msg)",
			"}",
			"use(v)",
		),
		readBeforeCheck,
	},
	{
		"wrong branch",
		errorProgram(`v, err := parse("1")`, "if err.code != 0 {", "    use(v)", "}"),
		readBeforeCheck,
	},
	{
		"bounded below only",
		errorProgram(`v, err := parse("1")`, "if err.code >= 0 {", "    use(v)", "}"),
		readBeforeCheck,
	},
	{
		"either side",
		errorProgram(`v, err := parse("1")`, "if err.code == 0 || v > 0 {", "}"),
		readBeforeCheck,
	},
	{
		"before the check",
		errorProgram(`v, err := parse("1")`, "if v > 0 && err.code == 0 {", "}"),
		readBeforeCheck,
	},
	{
		"exit",
		errorProgram(`v, err := parse("1")`, "if err.code != 0 {", "    exit(1)", "}", "use(v)"),
		readBeforeCheck,
	},
	{
		"compound",
		errorProgram(`mut v, err := parse("1")`, "v += 1", "if err.code == 0 {", "    use(v)", "}"),
		readBeforeCheck,
	},
	{
		"increment",
		errorProgram(`mut v, err := parse("1")`, "v--", "if err.code == 0 {", "    use(v)", "}"),
		readBeforeCheck,
	},
	{
		"mut argument",
		errorProgram(`mut v, err := parse("1")`, "bump(mut v)", "print(err.msg)"),
		readBeforeCheck,
	},
	{
		"in arithmetic",
		errorProgram(`v, err := parse("1")`, "print(err.msg)", "x := v + 1", "use(x)"),
		readBeforeCheck,
	},
	{
		"in a defer before the check",
		errorProgram(
			`v, err := parse("1")`,
			"defer {",
			"    use(v)",
			"}",
			"if err.code != 0 {",
			"    return",
			"}",
		),
		readBeforeCheck,
	},
	{
		"check dropped by a rebinding before the read",
		errorProgram(
			`mut v, mut err := parse("1")`, "if err.code != 0 {", "    return", "}",
			`v, err = parse("2")`, "use(v)", "if err.code != 0 {", "    return", "}",
		),
		readBeforeCheck,
	},
	{
		"only one of two errors checked",
		errorProgram(
			`mut v, e1 := parse("1")`,
			"mut e2 error",
			`v, e2 = parse("2")`,
			"if e2.code != 0 {",
			"    return",
			"}",
			"print(e1.msg)",
			"use(v)",
		),
		"v cannot be read before e1 is checked; check e1.code == 0 first",
	},
	{
		"assigned alone",
		errorProgram(
			`v, mut err := parse("1")`,
			"err = noerror",
			"if err.code == 0 {",
			"    use(v)",
			"}",
		),
		heldAlone,
	},
	{
		"field assigned alone",
		errorProgram(
			`v, mut err := parse("1")`,
			"err.code = 0",
			"if err.code == 0 {",
			"    use(v)",
			"}",
		),
		heldAlone,
	},
	{
		"field incremented",
		errorProgram(
			`v, mut err := parse("1")`,
			"err.code++",
			"if err.code == 0 {",
			"    use(v)",
			"}",
		),
		heldAlone,
	},
	{
		"bound beside another",
		errorProgram(
			`v, mut err := parse("1")`,
			"mut w int64 = 0",
			`w, err = parse("2")`,
			"if err.code == 0 {",
			"    use(v + w)",
			"}",
		),
		heldAlone,
	},
	{
		"bound to a checked index",
		errorProgram(
			`v, mut err := parse("1")`,
			"a := [1]error{noerror}",
			"err, _ = a[0]",
			"if err.code == 0 {",
			"    use(v)",
			"}",
		),
		heldAlone,
	},
	{
		"mut argument of the error",
		"package main\n\nfunc parse() (int64, error) {\n    return 1, noerror\n}\n\n" +
			"func reset(mut e error) {\n    e = noerror\n}\n\nfunc main() {\n    v, mut err := parse()\n" +
			"    reset(mut err)\n    if err.code == 0 && v > 0 {\n    }\n}\n",
		"cannot pass mut err: " + heldAlone,
	},
	{
		"error to a global",
		"package main\n\nmut g error\n\nfunc parse() (int64, error) {\n    return 1, noerror\n}\n\n" +
			"func main() {\n    mut v int64 = 0\n    v, g = parse()\n    print(g.msg)\n    if v > 0 {\n    }\n}\n",
		"g cannot take the error of parse beside v; bind it to a local variable",
	},
	{
		"result to a parameter",
		"package main\n\nfunc parse() (int64, error) {\n    return 1, noerror\n}\n\n" +
			"func f(mut n int64) {\n    mut err error\n    n, err = parse()\n    print(err.msg)\n}\n\n" +
			"func main() {\n}\n",
		"n cannot take a result of parse beside its error; bind it to a local variable",
	},
	{
		"result to an element",
		"package main\n\nfunc parse() (int64, error) {\n    return 1, noerror\n}\n\n" +
			"func main() {\n    mut a [1]int64\n    mut err error\n    a[0], err = parse()\n    print(err.msg)\n    exit(int32(len(a)))\n}\n",
		"a[0] cannot take a result of parse beside its error; bind it to a local variable",
	},
	{
		"error not last",
		"package main\n\nfunc f() (error, int64) {\n    return noerror, 1\n}\n\nfunc main() {\n}\n",
		"error must be the last result of f",
	},
	{
		"error not last in an extern",
		"package main\n\nextern func f() (int64, error, bool)\n\nfunc main() {\n}\n",
		"error must be the last result of f",
	},
	{
		"literal missing a field",
		mainLines("e := error{code: 1}", "print(e.msg)"),
		"error literal is missing field msg",
	},
	{
		"literal of nothing",
		mainLines("e := error{}", "print(e.msg)"),
		"error literal is missing fields code, msg",
	},
	{
		"literal with an int64 code",
		mainLines("n := int64(1)", "e := error{code: n, msg: \"\"}", "print(e.msg)"),
		"cannot use int64 as int32 in field code of error literal",
	},
	{"unknown field", mainLines("e := noerror", "print(e.text)"), "error has no field text"},
	{"ordered", mainLines("if noerror < noerror {", "}"), "operator < is not defined on error"},
	{
		"as any",
		"package main\n\nfunc f(a any) {\n}\n\nfunc main() {\n    f(noerror)\n}\n",
		"cannot pass error as any: only the builtin types and views of them can be passed as any for now",
	},
	{"converted", mainLines("exit(int32(noerror))"), "cannot convert error to int32"},
	{
		"divisor of a mutable error's code",
		mainLines("mut e error", "e.code = 2", "if e.code != 0 {", "    exit(10 / e.code)", "}"),
		"divisor may be zero; use q, ok := ... / ...",
	},
}

// TestRejectsErrorMisuse checks each of errorRejectedCases gives exactly
// its diagnostic.
func TestRejectsErrorMisuse(t *testing.T) {
	for _, tt := range errorRejectedCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want one diagnostic %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsErrorTypes checks a function's error result, noerror and an
// error literal are each types.Error, and err.code an int32 and err.msg a
// string.
func TestRecordsErrorTypes(t *testing.T) {
	file, diags := checkTree(t, errorProgram(
		`v, err := parse("1")`, "e := error{code: err.code, msg: err.msg}", "f := noerror",
		"print(e.msg)", "print(f.msg)", "if err.code == 0 {", "    use(v)", "}",
	))
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	funcs := funcsOf(file)
	if got := funcs["parse"].ResultTypes; len(got) != 2 || got[1] != types.Error {
		t.Errorf("parse's results are %v, want an error last", got)
	}
	body := funcs["main"].Body
	if typ := body[0].(*ast.MultiVar).Vars[1].Type; typ != types.Error {
		t.Errorf("err is %d, want error", typ)
	}
	lit := body[1].(*ast.Var).Value.(*ast.CompositeLit)
	if lit.Type != types.Error {
		t.Errorf("the literal is %d, want error", lit.Type)
	}
	code := lit.Elems[0].(*ast.KeyValue).Value
	msg := lit.Elems[1].(*ast.KeyValue).Value
	if ast.TypeOf(code) != types.Int32 || ast.TypeOf(msg) != types.String {
		t.Errorf(
			"err.code is %d and err.msg %d, want int32 and string",
			ast.TypeOf(code),
			ast.TypeOf(msg),
		)
	}
	if got := ast.TypeOf(body[2].(*ast.Var).Value); got != types.Error {
		t.Errorf("noerror is %d, want error", got)
	}
}

// funcsOf returns the functions file declares, by name.
func funcsOf(file *ast.File) map[string]*ast.Func {
	funcs := make(map[string]*ast.Func)
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.Func); ok {
			funcs[fn.Name] = fn
		}
	}
	return funcs
}

// TestNotesOnAnUncheckedRead checks the notes that explain a read before
// the error is checked: a check dropped by a later assignment, with the
// assignment, a check whose branch ends in exit, and a check that leaves
// an error possible.
func TestNotesOnAnUncheckedRead(t *testing.T) {
	head := "package main\n\nfunc parse() (int64, error) {\n    return 1, noerror\n}\n\nfunc main() {\n"
	expectNotes(t,
		head+"    mut v, mut err := parse()\n    if err.code != 0 {\n        return\n    }\n"+
			"    for range 2 {\n        exit(int32(v))\n        v, err = parse()\n    }\n}\n",
		readBeforeCheck,
		wantNote{"this early exit proves err.code == 0 after it", "if err.code != 0", 9, 5},
		wantNote{"but err is assigned here, so the proof ends", "err", 14, 12},
	)
	expectNotes(
		t,
		head+"    v, err := parse()\n    if err.code != 0 {\n        exit(1)\n    }\n    exit(int32(v))\n}\n",
		readBeforeCheck,
		wantNote{"this early exit would prove err.code == 0 after it", "if err.code != 0", 9, 5},
		wantNote{"but its branch ends in exit, not in return, break or continue", "exit(1)", 10, 9},
	)
	expectNotes(
		t,
		head+"    v, err := parse()\n    if err.code < 0 {\n        return\n    }\n    exit(int32(v))\n}\n",
		readBeforeCheck,
		wantNote{"this early exit allows err.code == 1", "err.code < 0", 9, 8},
	)
}

// TestNoteAtTheBindingThatHoldsAVariable checks an error assigned alone
// points at the binding that tied it to the variable it holds.
func TestNoteAtTheBindingThatHoldsAVariable(t *testing.T) {
	expectNotes(t,
		"package main\n\nfunc parse() (int64, error) {\n    return 1, noerror\n}\n\n"+
			"func main() {\n    v, mut err := parse()\n    err = noerror\n    if err.code == 0 && v > 0 {\n    }\n}\n",
		heldAlone,
		wantNote{"err is bound beside v here", "v, mut err := parse()", 8, 5},
	)
}

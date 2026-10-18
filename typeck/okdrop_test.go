package typeck_test

import (
	"strings"
	"testing"
)

// okForms are the checked forms: what declares what each reads, the form
// as written, which is also how its error names it, and the type of its
// value.
var okForms = []struct {
	name, setup, form, typ string
}{
	{"array index", "arr := [3]int64{1, 2, 3}", "arr[i]", "int64"},
	{"string index", `s := "abc"`, "s[i]", "uint8"},
	{"view index", "", "v[i]", "int64"},
	{"list index", "l := make(list[int64], c_allocator)", "l[i]", "int64"},
	{"map index", "m := make(map[int64]int64, c_allocator)", "m[i]", "int64"},
	{"string slice", `s := "abc"`, "s[i:]", "string"},
	{"division", "", "n / d", "int64"},
	{"remainder", "", "n % d", "int64"},
	{"conversion", "", "uint8(n)", "uint8"},
	{"type assertion", "", "a.(int64)", "int64"},
	{"pop", "mut l := make(list[int64], c_allocator)", "pop(mut l)", "int64"},
	{"remove", "mut l := make(list[int64], c_allocator)", "remove(mut l, i)", "int64"},
	{"delete", "mut m := make(map[int64]int64, c_allocator)", "delete(mut m, i)", "int64"},
}

// okProgram returns a program whose f runs lines, which may read v, a
// view, a, an any, and the int64s i, n and d, and may pass any value to
// use, which reads it.
func okProgram(lines ...string) string {
	return "package main\n\ntype Any interface {\n}\n\nfunc use[T Any](x T) {\n}\n\n" +
		"func f(v []int64, a any, i int64, n int64, d int64) {\n    " +
		strings.Join(lines, "\n    ") + "\n}\n\nfunc main() {\n}\n"
}

// withSetup returns lines after f's setup, when it has one.
func withSetup(setup string, lines ...string) []string {
	if setup == "" {
		return lines
	}
	return append([]string{setup}, lines...)
}

// TestRejectsDroppedOks checks that binding a checked form's ok to _, with
// := or =, is an error at the _, naming the form as written.
func TestRejectsDroppedOks(t *testing.T) {
	for _, f := range okForms {
		for _, op := range []string{":=", "="} {
			t.Run(f.name+" "+op, func(t *testing.T) {
				var lines []string
				if op == "=" {
					lines = append(lines, "mut x "+f.typ)
				}
				lines = append(lines, "x, _ "+op+" "+f.form, "use(x)")
				src := okProgram(withSetup(f.setup, lines...)...)
				_, diags := checkTree(t, src)
				want := "the ok " + f.form + " gives cannot be dropped; bind it to a variable instead of _"
				if len(diags) != 1 || diags[0].Message != want {
					t.Fatalf("got %v, want only %q", diags, want)
				}
				if got := src[diags[0].Span.Start:diags[0].Span.End]; got != "_" {
					t.Errorf("error covers %q, want _", got)
				}
			})
		}
	}
}

// TestAcceptsKeptOks checks that a checked form whose ok is bound, with
// its value kept or dropped, needs nothing more.
func TestAcceptsKeptOks(t *testing.T) {
	for _, f := range okForms {
		t.Run(f.name, func(t *testing.T) {
			kept := okProgram(
				withSetup(f.setup, "x, ok := "+f.form, "if ok {", "    use(x)", "}")...)
			if _, diags := checkTree(t, kept); len(diags) > 0 {
				t.Errorf("value and ok: got %v", diags)
			}
			alone := okProgram(withSetup(f.setup, "_, ok := "+f.form, "use(ok)")...)
			if _, diags := checkTree(t, alone); len(diags) > 0 {
				t.Errorf("ok alone: got %v", diags)
			}
		})
	}
}

// TestAcceptsTakesAsStatements checks that pop, remove and delete written
// as statements, which bind neither result, still compile.
func TestAcceptsTakesAsStatements(t *testing.T) {
	src := okProgram(
		"mut l := make(list[int64], c_allocator)",
		"pop(mut l)",
		"remove(mut l, i)",
		"mut m := make(map[int64]int64, c_allocator)",
		"delete(mut m, i)",
	)
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v", diags)
	}
}

// TestDroppedOkPrecedence checks the error that takes the place of the
// dropped ok's for a pop whose element cannot be read until its ok is
// checked, which says so. Both names _ is the resolver's error, every name
// is _, so nothing is bound, which the type checker never sees.
func TestDroppedOkPrecedence(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{
			"an element with no zero value",
			"package main\n\nfunc f(mut l list[Allocator]) {\n    x, _ := pop(mut l)\n    use(x)\n}\n\n" +
				"func use(a Allocator) {\n}\n\nfunc main() {\n}\n",
			"the ok pop returns cannot be dropped: x cannot be read until it is checked; " +
				"bind it to a variable instead of _",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want only %q", diags, tt.want)
			}
		})
	}
}

package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// reflectFieldsChecks is how many checks examples/reflect-fields prints.
const reflectFieldsChecks = 24

// TestReflectFieldsExampleIsSanitizerClean builds examples/reflect-fields,
// its packages geo and parts, its C file and std's C at -O2, and with the
// address and undefined behavior sanitizers at -O0 and -O1, every finding
// fatal, and checks every check it prints passed: fields loops over
// structs of three packages, Texts, lists, maps and basic types, through
// borrowed and mut fields passed on to other instances, with returns,
// defers, nested loops, closures reading v.[f] and an error passed on by
// ? at every allocation in turn, leaving c_allocator holding nothing.
func TestReflectFieldsExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	// The example's C file is one of its main package's, which
	// buildProgram compiles with the rest.
	prog := compileExampleDir(t, "reflect-fields")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), reflectFieldsChecks)
		})
	}
}

// fieldsLowering ranges over the fields of a struct holding a list, a
// Text and a nested struct, reading, passing as mut and deferring in each
// pass, from a generic function and from one that is not.
const fieldsLowering = `package main

import text from "std/text"

type In struct {
    k int64
}

type S struct {
    a int64
    l list[int64]
    t text.Text
    in In
}

func walk[T Data](mut v T) int64 {
    mut n := 0
    for f := range fields(T) {
        defer {
            n +%= 1
        }
        n +%= f.index +% walk(mut v.[f])
    }
    return n
}

func main() {
    mut s := S{a: 1, l: make(list[int64], c_allocator), t: text.new(c_allocator), in: In{k: 2}}
    n := walk(mut s)
    for f := range fields(S) {
        print(f.name)
    }
    if n == 11 {
        print(" ok\n")
    }
}
`

// TestFieldsLoopLowering checks a fields loop is unrolled into one C block
// per field, in order, each lowering v.[f] to that field's member and
// calling the instance for its type, and that the program runs.
func TestFieldsLoopLowering(t *testing.T) {
	gen, diags := c.New().Generate(compileWithStd(t, fieldsLowering), stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	out := string(gen)
	for _, want := range []string{
		"(&(*p_v).f_a)", "(&(*p_v).f_l)", "(&(*p_v).f_t)", "(&(*p_v).f_in)", "(&(*p_v).f_k)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated C has no %s:\n%s", want, out)
		}
	}
	a, l := strings.Index(out, "(&(*p_v).f_a)"), strings.Index(out, "(&(*p_v).f_l)")
	if a < 0 || l < 0 || a > l {
		t.Errorf("field a is not lowered before field l:\n%s", out)
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	bin := buildWithStd(t, fieldsLowering, "-O0", "-fsanitize=address,undefined", useAfterReturn,
		"-fno-sanitize-recover=all")
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if string(got) != "altin ok\n" {
		t.Errorf("printed %q, want %q", got, "altin ok\n")
	}
}

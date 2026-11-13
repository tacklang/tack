package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// reflectMakeChecks is how many checks examples/reflect-make prints.
const reflectMakeChecks = 37

// TestReflectMakeExampleIsSanitizerClean builds examples/reflect-make,
// its package vault, its C file and std's C at -O2, and with the address,
// undefined behavior and leak sanitizers at -O0 and -O1, every finding
// fatal, and checks every check it prints passed: make of every basic
// type, of text.Text, of structs at every depth, another package's
// private field and std's big.Int and os.Info among them, and of type
// parameters, narrowed cases, a list case's E and fields' types through
// generic functions; fill and clone built on it, each failing at every
// allocation in turn; make through a region, in a closure, a defer and a
// case value, discarded, and with an allocator expression evaluated
// once, leaving c_allocator holding nothing.
func TestReflectMakeExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "reflect-make")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), reflectMakeChecks)
		})
	}
}

// makeDataLowering makes a struct with two lists, a struct with none and
// an int64, each through a call that returns the allocator, a name of
// one and a generic function's scope parameter, and discards an int64's
// and a Flat's as statements.
const makeDataLowering = `package main

type Two struct {
    a list[int64]
    n int64
    b list[uint8]
}

type Flat struct {
    x int64
    y bool
}

func pick() Allocator {
    return c_allocator
}

func made[T Data](mut v T, scope alloc Allocator) {
    v = make(T, alloc)
}

func main() {
    two := make(Two, pick())
    one := make(Two, c_allocator)
    flat := make(Flat, pick())
    n := make(int64, pick())
    make(int64, pick())
    make(Flat, pick())
    make(int64, c_allocator)
    mut m := 5
    made(mut m, c_allocator)
    if len(two.a) == 0 && len(one.b) == 0 && flat.x == 0 && !flat.y && n == 0 && m == 0 {
        print("ok\n")
    }
}
`

// TestMakeDataLowering checks the C make(T, alloc) lowers to: a struct's
// lists each hold the allocator, which a call gives once, through a
// temporary; c_allocator is read in place; a value holding no list
// evaluates the allocator, cast to void, beside its zeroed value, a make
// written as a statement, which owns nothing, is that value cast to void;
// and an instance for int64 does the same with its scope parameter. The
// program then runs under the sanitizers.
func TestMakeDataLowering(t *testing.T) {
	gen, diags := c.New().Generate(compileWithStd(t, makeDataLowering), stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	out := string(gen)
	main := instanceBody(t, out, "_main")
	for _, want := range []string{
		"v_two = (tack_a0 = tack_4_main4_pick(), ((tack_4_main3_Two){" +
			".f_a = ((tack_list_22){.alloc = tack_a0}), .f_n = 0, " +
			".f_b = ((tack_list_23){.alloc = tack_a0})}));",
		"v_one = ((tack_4_main3_Two){.f_a = ((tack_list_22){.alloc = TACK_C_ALLOCATOR}), " +
			".f_n = 0, .f_b = ((tack_list_23){.alloc = TACK_C_ALLOCATOR})});",
		"v_flat = ((void)(tack_4_main4_pick()), (tack_4_main4_Flat){});",
		"v_n = ((void)(tack_4_main4_pick()), 0);",
		"\n    (void)(((void)(tack_4_main4_pick()), 0));",
		"\n    (void)(((void)(tack_4_main4_pick()), (tack_4_main4_Flat){}));",
		"\n    (void)(((void)(TACK_C_ALLOCATOR), 0));",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main lacks %q:\n%s", want, main)
		}
	}
	if got := strings.Count(main, "pick()"); got != 5 {
		t.Errorf("main calls pick %d times, want 5, once per make:\n%s", got, main)
	}
	inst := instanceBody(t, out, "made_5bint64_5d")
	if want := "(*p_v) = ((void)(p_alloc), 0);"; !strings.Contains(inst, want) {
		t.Errorf("made[int64] lacks %q:\n%s", want, inst)
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	bin := buildWithStd(t, makeDataLowering, "-O0", "-fsanitize=address,undefined",
		useAfterReturn, "-fno-sanitize-recover=all")
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if string(got) != "ok\n" {
		t.Errorf("printed %q, want %q", got, "ok\n")
	}
}

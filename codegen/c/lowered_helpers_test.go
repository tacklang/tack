package c_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// sliceAcrossInstances slices lists of a type parameter's elements, with
// and without a high bound, in generic functions constrained by Data and
// by an interface, for elements of several types, and passes each view
// on to another generic function.
const sliceAcrossInstances = `package main

type Any0 interface {
}

type P struct {
    n int64
}

func vlen[U Data](xs []U) int64 {
    return len(xs)
}

func ilen[U Any0](xs []U) int64 {
    return len(xs)
}

func g[T Data](xs list[T]) int64 {
    if len(xs) > 1 {
        return vlen(xs[1:]) +% vlen(xs[:1])
    }
    return 0
}

func h[T Any0](xs list[T]) int64 {
    if len(xs) > 1 {
        return ilen(xs[1:]) +% ilen(xs[0:1])
    }
    return 0
}

func main() {
    mut a := make(list[int64], c_allocator)
    ea := append(mut a, 1, 2, 3)
    mut b := make(list[uint8], c_allocator)
    eb := append(mut b, 1, 2)
    mut p := make(list[P], c_allocator)
    ep := append(mut p, P{n: 1}, P{n: 2}, P{n: 3}, P{n: 4})
    if ea.code == 0 && eb.code == 0 && ep.code == 0 && g(a) == 3 && g(b) == 2 && h(p) == 4 {
        print("ok\n")
    }
}
`

// TestSliceHelpersPerInstance checks a slice in a generic function calls
// the helper for the view type each instance makes it, and that helper is
// emitted, whatever the type parameter's constraint: none is emitted for a
// view of an unbound type parameter, which C has no type for.
func TestSliceHelpersPerInstance(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	bin := buildWithStd(t, sliceAcrossInstances, "-O0", "-fsanitize=address,undefined",
		useAfterReturn, "-fno-sanitize-recover=all")
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if string(got) != "ok\n" {
		t.Errorf("printed %q, want %q", got, "ok\n")
	}
}

// unloweredCompares compares structs, an array of strings and strings
// only where no C is lowered: in a generic function no call instantiates,
// and in package-level initializers, which the compiler computes.
const unloweredCompares = `package main

type P struct {
    x int64
}

type Any0 interface {
}

var same = "a" == "a"

func never[T Any0](v T, a P, b P, s [2]string, t [2]string) bool {
    return a == b && s == t && "x" < "y"
}

func main() {
    if same {
        print("ok\n")
    }
}
`

// TestHelpersOnlyForLoweredCode checks no equality or string comparison
// helper is emitted for comparisons no lowered code makes, which strict C
// would reject as unused functions, and that the program builds under
// -Werror and runs.
func TestHelpersOnlyForLoweredCode(t *testing.T) {
	gen, diags := c.New().Generate(compileWithStd(t, unloweredCompares), stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	for _, helper := range []string{"tack_eq_", "tack_compare_strings"} {
		if strings.Contains(string(gen), helper) {
			t.Errorf("generated C holds %s, which no lowered code calls:\n%s", helper, gen)
		}
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	got, err := exec.Command(buildWithStd(t, unloweredCompares)).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if string(got) != "ok\n" {
		t.Errorf("printed %q, want %q", got, "ok\n")
	}
}

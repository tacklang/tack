package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// reflectSwitchChecks is how many checks examples/reflect-switch prints.
const reflectSwitchChecks = 29

// TestReflectSwitchExampleIsSanitizerClean builds examples/reflect-switch,
// its C file and std's C at -O2, and with the address and undefined
// behavior sanitizers at -O0 and -O1, every finding fatal, and checks
// every check it prints passed: show, equal and count_ints over every
// kind of Data, nested lists, maps and structs, cases narrowing a mut
// parameter, a local, a closure's capture and a deferred write, a switch
// nested in a case, and show failing at every allocation in turn, leaving
// c_allocator holding nothing.
func TestReflectSwitchExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "reflect-switch")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), reflectSwitchChecks)
		})
	}
}

// paramSwitchLowering switches on a type parameter, calling a different
// function in each case, with a local and a parameter only some cases
// name, from instances for an int64, a list of int64 and a struct.
const paramSwitchLowering = `package main

type P struct {
    a int64
}

func on_int() int64 {
    return 1
}

func on_list() int64 {
    return 2
}

func on_other() int64 {
    return 3
}

func kind[T Data](v T) int64 {
    k := 10
    switch T {
    case int64:
        return on_int() +% v +% k
    case list[E]:
        return on_list() +% len(v)
    default:
        return on_other()
    }
}

func main() {
    l := make(list[int64], c_allocator)
    if kind(5) == 16 && kind(l) == 2 && kind(P{a: 1}) == 3 {
        print("ok\n")
    }
}
`

// TestParamSwitchLowering checks each instance of a function holding a
// switch on a type parameter is lowered to the one case its type
// argument matches, with no test at run time, that a local or a
// parameter only other cases name leaves C nothing to warn about, and
// that the program runs.
func TestParamSwitchLowering(t *testing.T) {
	gen, diags := c.New().Generate(compileWithStd(t, paramSwitchLowering), stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	out := string(gen)
	for name, want := range map[string]string{
		"kind_5bint64_5d":           "on_5fint",
		"kind_5blist_5bint64_5d_5d": "on_5flist",
		"kind_5bmain_2eP_5d":        "on_5fother",
	} {
		body := instanceBody(t, out, name)
		for _, callee := range []string{"on_5fint", "on_5flist", "on_5fother"} {
			if got := strings.Contains(body, callee); got != (callee == want) {
				t.Errorf("instance %s calls %s: %v, want %v:\n%s", name, callee, got,
					callee == want, body)
			}
		}
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	bin := buildWithStd(t, paramSwitchLowering, "-O0", "-fsanitize=address,undefined",
		useAfterReturn, "-fno-sanitize-recover=all")
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if string(got) != "ok\n" {
		t.Errorf("printed %q, want %q", got, "ok\n")
	}
}

// instanceBody returns the body of the C function whose name ends in
// suffix, as defined in out, from its opening brace through the line
// closing it.
func instanceBody(t *testing.T, out, suffix string) string {
	t.Helper()
	for _, line := range strings.SplitAfter(out, "\n") {
		if strings.Contains(line, suffix+"(") && strings.HasSuffix(line, "{\n") &&
			!strings.HasPrefix(line, " ") {
			start := strings.Index(out, line)
			end := strings.Index(out[start:], "\n}\n")
			if end < 0 {
				t.Fatalf("no end to %s in:\n%s", suffix, out)
			}
			return out[start : start+end]
		}
	}
	t.Fatalf("no function %s in:\n%s", suffix, out)
	return ""
}

// paramSwitchInteractions puts switches on type parameters where other
// features are: a closure in a case of a list calling an instance for
// the element type, a switch on a map's value type nested in the case of
// the map, a switch on T inside a fields loop inside the case of struct,
// a mut list's elements popped and appended back, a narrowed int64 passed
// as mut to a function taking an int64 and compared, a defer reading a
// narrowed value, and a switch in a loop whose cases continue and break.
const paramSwitchInteractions = `package main

import text from "std/text"

type In struct {
    a int64
    b list[int64]
}

type Out struct {
    i In
    m map[text.Text]list[uint8]
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func run(scope f func()) {
    f()
}

func inc(mut x int64) {
    if x < 1000 && x > -1000 {
        x = x + 1
    }
}

func ints[T Data](v T) int64 {
    switch T {
    case int64:
        return 1
    case list[E]:
        mut n := 0
        for _, e := range v {
            run(func() {
                n +%= ints(e)
            })
        }
        return n
    case map[text.Text]V:
        mut n := 0
        for _, x := range v {
            switch V {
            case list[F]:
                n +%= 100 +% len(x)
            default:
                n +%= ints(x)
            }
        }
        return n
    case struct:
        mut n := 0
        for f := range fields(T) {
            switch T {
            case text.Text:
                n +%= 1000
            case struct:
                n +%= ints(v.[f])
            }
        }
        return n
    default:
        return 0
    }
}

func rotate[T Data](mut v T) error {
    switch T {
    case list[E]:
        if len(v) > 0 {
            x := pop(mut v)
            append(mut v, x)?
        }
    case int64:
        inc(mut v)
        if v == 7 {
            v = 70
        }
    default:
    }
    return noerror
}

func deferred[T Data](v T) int64 {
    mut seen := 0
    switch T {
    case int64:
        defer {
            seen = v
        }
        seen = -1
    default:
    }
    return seen
}

func loopy[T Data](v T) int64 {
    mut total := 0
    for i := range 3 {
        switch T {
        case int64:
            if i == 1 {
                continue
            }
            total +%= v
        default:
            break
        }
    }
    return total
}

func main() {
    mut o := Out{i: In{a: 1, b: make(list[int64], c_allocator)}, m: make(map[text.Text]list[uint8], c_allocator)}
    e := append(mut o.i.b, 4, 5)
    mut k := text.new(c_allocator)
    e2 := mut k.append([]uint8("k"))
    mut bs := make(list[uint8], c_allocator)
    e3 := append(mut bs, 1, 2, 3)
    e4 := put(mut o.m, k, bs)
    check(e.code == 0 && e2.code == 0 && e3.code == 0 && e4.code == 0, "build")
    check(ints(o) == 106, "ints")
    mut l := make(list[int64], c_allocator)
    e5 := append(mut l, 1, 2, 3)
    r := rotate(mut l)
    mut n := 6
    r2 := rotate(mut n)
    mut s := 3
    r3 := rotate(mut s)
    check(e5.code == 0 && r.code == 0 && len(l) == 3 && r2.code == 0 && n == 70 && r3.code == 0 && s == 4, "rotate")
    check(deferred(9) == 9 && deferred(true) == 0, "defer reads narrowed")
    check(loopy(5) == 10 && loopy(true) == 0, "loop")
}
`

// TestParamSwitchInteractions builds paramSwitchInteractions with the
// address and undefined behavior sanitizers, every finding fatal, and
// checks every check it prints passed.
func TestParamSwitchInteractions(t *testing.T) {
	bin := buildWithStd(t, paramSwitchInteractions, "-O0", "-fsanitize=address,undefined",
		useAfterReturn, "-fno-sanitize-recover=all")
	runChecks(t, bin, 5)
}

package c_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// methodsSrc declares methods of every kind and calls them, printing a
// check for each.
const methodsSrc = `package main

type B struct {
    buf list[int64]
    n   int64
}

type W struct {
    b  B
    ps [2]P
}

type P struct {
    x int64
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func newB() B {
    return B{buf: make(list[int64], c_allocator), n: 0}
}

func (b B) len() int64 {
    return len(b.buf)
}

func (mut b B) append(x int64) error {
    defer {
        b.n +%= 1
    }
    return append(mut b.buf, x)
}

func (mut b B) all(xs ...int64) error {
    for _, x := range xs {
        err := mut b.append(x)
        if err.code != 0 {
            return err
        }
    }
    return noerror
}

func (b B) at(i int64) (int64, bool) {
    x, ok := b.buf[i]
    return x, ok
}

func (p P) get() int64 {
    return p.x
}

func (mut p P) set(x int64) {
    p.x = x
}

func (p P) fact(n int64) int64 {
    if n <= 1 {
        return 1
    }
    return n *% p.fact(n - 1)
}

func (p P) digits(a int64, b int64) int64 {
    return p.x *% 100 +% a *% 10 +% b
}

func next(mut n int64) int64 {
    old := n
    n +%= 1
    return old
}

func at(n int64) P {
    return P{x: n}
}

func main() {
    mut w := W{b: newB(), ps: [2]P{P{x: 1}, P{x: 2}}}
    err := mut w.b.all(4, 5, 6)
    if err.code == 0 {
        x, ok := w.b.at(2)
        check(w.b.len() == 3 && w.b.n == 3 && x == 6 && ok, "mut receiver through a field")
    }
    mut w.ps[1].set(7)
    check(w.ps[1].get() == 7 && w.ps[0].get() == 1, "element receivers")
    check(P{x: 0}.fact(5) == 120, "recursion on a temporary")
    mut k int64 = 1
    check(at(k).digits(next(mut k), next(mut k)) == 112 && k == 3, "receiver first")
}
`

// TestMethodsBuildStrictlyAndRun builds a program calling methods of every
// kind with clang's warnings as errors and its address and undefined
// behavior sanitizers, and checks every check it prints passes.
func TestMethodsBuildStrictlyAndRun(t *testing.T) {
	bin := buildWith(t, methodsSrc, "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 4 {
		t.Errorf("printed\n%s\nwant 4 checks, every one ok", out)
	}
}

// TestLowersMethodsAsFunctions checks a method is a C function of its own,
// named from its type and its name, taking its receiver first, a mut one
// as a pointer, and that a call passes the receiver first, a mut one as
// the address of its place.
func TestLowersMethodsAsFunctions(t *testing.T) {
	out := generate(t, methodsSrc)
	for _, want := range []string{
		"static int64_t tack_4_main7_B_2elen(tack_4_main1_B p_b)",
		"static tack_error tack_4_main10_B_2eappend(tack_4_main1_B * p_b, int64_t p_x)",
		"static void tack_4_main7_P_2eset(tack_4_main1_P * p_p, int64_t p_x)",
		"tack_4_main7_P_2eset(&v_w.f_ps.e[1], 7)",
		"tack_4_main10_B_2eappend(p_b, v_x)",
		"tack_4_main7_B_2elen(v_w.f_b)",
		"tack_results_4_main6_B_2eat",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated C lacks %q:\n%s", want, out)
		}
	}
}

// TestUncheckedMethodCallIsReported checks the backend reports a method
// call whose method the checker did not record, here a call statement of
// set, rather than emitting C that calls nothing.
func TestUncheckedMethodCallIsReported(t *testing.T) {
	file := compile(t, methodsSrc)
	ast.Walk(file, func(n ast.Node) bool {
		if stmt, ok := n.(*ast.ExprStmt); ok {
			if call, ok := stmt.X.(*ast.Call); ok && call.Method != nil &&
				call.Method.Name == "set" {
				call.Method = nil
			}
		}
		return true
	})
	_, diags := c.New().Generate(ast.SingleFile(file), stdtest.MustBuiltins())
	if len(diags) == 0 || !strings.Contains(diags[0].Message, "method call must be checked") {
		t.Errorf("reported %v", diags)
	}
}

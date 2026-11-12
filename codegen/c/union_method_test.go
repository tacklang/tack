package c_test

import (
	"os/exec"
	"strings"
	"testing"
)

// unionMethodsSrc declares methods on a union holding no list and on one
// holding a list, and passes both, and a struct, through generic
// functions, printing a check for each: a plain method switching on its
// receiver, a mut one switching on its receiver and reassigning it, a mut
// one replacing a list, and mut and plain methods through a type
// parameter.
const unionMethodsSrc = `package main

type Sizer interface {
    size() int64
}

type Resetter interface {
    mut reset()
    size() int64
}

type E union {
    int int64
    eof
}

func (e E) size() int64 {
    switch e {
    case int(n):
        return n
    case eof:
        return 0
    }
}

func (mut e E) bump() {
    mut next := e
    switch e {
    case int(n):
        next = E.int(n +% 1)
    case eof:
        e = E.int(0)
        return
    }
    e = next
}

func (mut e E) reset() {
    e = E.eof
}

type V union {
    items list[int64]
    none
}

func (v V) size() int64 {
    switch v {
    case items(l):
        return len(l)
    case none:
        return 0
    }
}

func (mut v V) push(x int64) error {
    mut next := make(list[int64], c_allocator)
    switch v {
    case items(l):
        err := append(mut next, l...)
        if err.code != 0 {
            return err
        }
    default:
    }
    err := append(mut next, x)
    if err.code != 0 {
        return err
    }
    v = V.items(next)
    return noerror
}

func (mut v V) reset() {
    v = V.none
}

type P struct {
    x int64
}

func (p P) size() int64 {
    return p.x
}

func measure[S Sizer](s S) int64 {
    return s.size()
}

func wipe[R Resetter](mut r R) int64 {
    n := r.size()
    mut r.reset()
    return n
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func main() {
    mut e := E.eof
    mut e.bump()
    mut e.bump()
    check(e.size() == 1, "mut method switching on its receiver")
    mut v := V.none
    err := mut v.push(4)
    err2 := mut v.push(5)
    check(err.code == 0 && err2.code == 0 && v.size() == 2, "mut method replacing a list")
    check(measure(e) +% measure(v) +% measure(P{x: 3}) == 6, "unions and a struct through one instance each")
    check(wipe(mut v) == 2 && v.size() == 0, "list freed through a generic mut method")
    check(wipe(mut e) == 1 && e == E.eof, "union reset through a generic mut method")
}
`

// TestUnionMethodsBuildStrictlyAndRun builds a program of methods on
// unions, passed through generic functions, with clang's warnings as
// errors and its address and undefined behavior sanitizers, so a list a
// mut method replaces is freed exactly once, and checks every check it
// prints passes.
func TestUnionMethodsBuildStrictlyAndRun(t *testing.T) {
	bin := buildWith(
		t,
		unionMethodsSrc,
		"-fsanitize=address,undefined", useAfterReturn,
		"-fno-sanitize-recover=all",
	)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 5 {
		t.Errorf("printed\n%s\nwant 5 checks, every one ok", out)
	}
}

// TestUnionMethodsAreFunctions checks a union's method is a C function
// named after the union and the method, taking a mut receiver as a
// pointer and a plain one as a value, and that each instance of a generic
// function a union is passed to is named after the union and calls its
// method directly.
func TestUnionMethodsAreFunctions(t *testing.T) {
	out := generate(t, unionMethodsSrc)
	for _, want := range []string{
		"static int64_t tack_4_main8_E_2esize(tack_4_main1_E p_e) {",
		"static void tack_4_main8_E_2ebump(tack_4_main1_E * p_e) {",
		"static tack_error tack_4_main8_V_2epush(tack_4_main1_V * p_v, int64_t p_x) {",
		"static int64_t tack_4_main21_measure_5bmain_2eE_5d(tack_4_main1_E p_s) {",
		"static int64_t tack_4_main21_measure_5bmain_2eV_5d(tack_4_main1_V p_s) {",
		"static int64_t tack_4_main21_measure_5bmain_2eP_5d(tack_4_main1_P p_s) {",
		"return tack_4_main8_E_2esize(p_s);",
		"tack_4_main9_V_2ereset(p_r);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated C lacks %q:\n%s", want, out)
		}
	}
}

package c_test

import (
	"os/exec"
	"strings"
	"testing"
)

// genericsSrc declares interfaces and generic functions and calls them
// with a type holding a list and one holding none, printing a check for
// each: mut and plain interface methods, several results and a spread
// through an interface method, a narrower constraint, recursion swapping
// type arguments, a defer, and values of a type parameter moved between
// locals, on a path that never runs.
const genericsSrc = `package main

type Writer interface {
    mut write(b []uint8) error
    size() int64
}

type Sizer interface {
    size() int64
}

type Source interface {
    mut next() (uint8, error)
}

type Parts interface {
    mut writeAll(parts ...string) error
}

type Counter struct {
    n int64
}

func (mut c Counter) write(b []uint8) error {
    c.n +%= len(b)
    return noerror
}

func (c Counter) size() int64 {
    return c.n
}

type Buffer struct {
    bytes list[uint8]
    read  int64
}

func newBuffer() Buffer {
    return Buffer{bytes: make(list[uint8], c_allocator), read: 0}
}

func (mut b Buffer) write(bs []uint8) error {
    return append(mut b.bytes, bs...)
}

func (b Buffer) size() int64 {
    return len(b.bytes)
}

func (mut b Buffer) next() (uint8, error) {
    v, ok := b.bytes[b.read]
    if !ok {
        return 0, error{code: 1, msg: "end"}
    }
    b.read +%= 1
    return v, noerror
}

func (mut b Buffer) writeAll(parts ...string) error {
    for _, p := range parts {
        err := mut b.write([]uint8(p))
        if err.code != 0 {
            return err
        }
    }
    return noerror
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func measure[S Sizer](s S) int64 {
    return s.size()
}

func render[W Writer](mut w W, s string) (int64, error) {
    defer {
        e := mut w.write([]uint8("!"))
        if e.code != 0 {
            print("defer failed\n")
        }
    }
    err := mut w.write([]uint8(s))
    if err.code != 0 {
        return 0, err
    }
    return measure(w), noerror
}

func sum[S Source](mut s S) int64 {
    mut total int64 = 0
    for {
        v, err := mut s.next()
        if err.code != 0 {
            return total
        }
        total +%= v
    }
}

func joined[P Parts](mut p P, parts ...string) error {
    return mut p.writeAll(parts...)
}

func alternate[A Sizer, B Sizer](a A, b B, n int64) int64 {
    if n <= 0 {
        return a.size() -% b.size()
    }
    return alternate(b, a, n - 1)
}

func main() {
    mut c := Counter{n: 0}
    n, err := render(mut c, "abc")
    check(err.code == 0 && n == 3 && c.n == 4 && measure(c) == 4, "counter")
    mut b := newBuffer()
    m, err2 := render(mut b, "ab")
    check(err2.code == 0 && m == 2 && b.size() == 3, "buffer")
    err3 := joined(mut b, "c", "d")
    check(err3.code == 0 && b.size() == 5, "spread into an interface method")
    check(sum(mut b) == 427, "several results through an interface method")
    check(alternate(b, c, 3) == 4 - 5, "type arguments swapped")
}
`

// TestGenericsBuildStrictlyAndRun builds a program of generic functions
// with clang's warnings as errors and its address and undefined behavior
// sanitizers, and checks every check it prints passes.
func TestGenericsBuildStrictlyAndRun(t *testing.T) {
	bin := buildWith(t, genericsSrc, "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 5 {
		t.Errorf("printed\n%s\nwant 5 checks, every one ok", out)
	}
}

// TestEachInstanceIsDefinedOnce checks an instance needed by several
// calls, measure with Counter here, from main and from two instances of
// render, is declared and defined once, under its own name, that a
// method call on a type parameter calls the type argument's method
// directly, and that a generic function is never emitted itself.
func TestEachInstanceIsDefinedOnce(t *testing.T) {
	out := generate(t, genericsSrc)
	measure := "static int64_t tack_4_main27_measure_5bmain_2eCounter_5d(tack_4_main7_Counter p_s)"
	if got := strings.Count(out, measure+";"); got != 1 {
		t.Errorf("measure[main.Counter] is declared %d times, want 1:\n%s", got, out)
	}
	if got := strings.Count(out, measure+" {"); got != 1 {
		t.Errorf("measure[main.Counter] is defined %d times, want 1:\n%s", got, out)
	}
	for _, want := range []string{
		"return tack_4_main14_Counter_2esize(p_s);",
		"tack_4_main14_Buffer_2ewrite(p_w, ",
		"tack_results_4_main13_Buffer_2enext",
		"tack_results_4_main25_render_5bmain_2eBuffer_5d",
		"tack_4_main45_alternate_5bmain_2eBuffer_2cmain_2eCounter_5d(p_b, p_a, ",
		"tack_4_main45_alternate_5bmain_2eCounter_2cmain_2eBuffer_5d(p_b, p_a, ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated C lacks %q:\n%s", want, out)
		}
	}
	// A generic function emitted itself would be named after its own type
	// parameters, which belong to no package.
	for _, own := range []string{"_5bS_5d", "_5bW_5d", "_5bP_5d", "_5bA_2cB_5d"} {
		if strings.Contains(out, own) {
			t.Errorf("a generic function is emitted itself, as %s:\n%s", own, out)
		}
	}
}

// TestUncalledGenericFunctionIsNotEmitted checks a generic function no
// call reaches has no instance, so nothing of it is in the C, though its
// body was checked.
func TestUncalledGenericFunctionIsNotEmitted(t *testing.T) {
	src := `package main

type Sizer interface {
    size() int64
}

func unused[S Sizer](s S) int64 {
    return s.size()
}

func main() {
}
`
	if out := generate(t, src); strings.Contains(out, "unused") {
		t.Errorf("an uncalled generic function is emitted:\n%s", out)
	}
}

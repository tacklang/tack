package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// compileExampleDir runs the front end on the program the module
// directory examples/<name> builds, read from the repository so that its
// files, and its C files, are named relative to the repository's root,
// and fails the test if any stage rejects it.
func compileExampleDir(t *testing.T, name string) *ast.Program {
	t.Helper()
	l := &loader.Loader{
		FS:       loader.FromFS(os.DirFS(stdtest.Home())),
		Builtins: stdtest.MustBuiltins(),
		Std:      stdtest.Std(),
	}
	res, err := l.Dir("examples/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) > 0 {
		t.Fatalf("load: %s", diag.RenderAllPretty(res.Files, res.Diags))
	}
	if diags := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); diag.HasErrors(diags) {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(res.Files, diags))
	}
	diags := typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check()
	if diag.HasErrors(diags) {
		t.Fatalf("typeck: %s", diag.RenderAllPretty(res.Files, diags))
	}
	return res.Program
}

// TestViewResultsExampleIsSanitizerClean builds examples/view-results,
// its package text, its C file and std's C with clang's warnings as
// errors and the address and undefined behavior sanitizers on, every
// finding fatal, at -O0 and -O2, and checks it writes exactly its output,
// every check ok: no view a function or a method returns reads past what
// it views, or memory freed or gone by the time it is read, whether it
// views a list's buffer, an array in a view's or a list's element, a
// slice of either, or a string's bytes passed through a view parameter.
func TestViewResultsExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	want := readFile(t, filepath.Join("..", "..", "examples", "view-results", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/view-results/output expects a check to fail")
	}
	prog := compileExampleDir(t, "view-results")
	for _, opt := range []string{"-O0", "-O2"} {
		t.Run(opt, func(t *testing.T) {
			bin := buildProgram(t, prog, "", opt, "-fsanitize=address,undefined", useAfterReturn,
				"-fno-sanitize-recover=all")
			out, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("view-results failed: %v\n%s", err, out)
			}
			if string(out) != want {
				t.Errorf("view-results wrote\n%s\nwant\n%s", out, want)
			}
		})
	}
}

// viewResultCText returns views from a method of a struct holding a list,
// a function slicing a view parameter, and one returning an array element
// of a list, and reads each where a view result may be used, counting in
// calls how often a view-returning function runs, so that every use is
// shown to call it exactly once; ranges over values a variadic call
// packs; and returns the view of an array its call copied, since the same
// call passes the array as mut.
const viewResultCText = `package main

mut calls int64

type Text struct {
    b list[uint8]
}

func (t Text) bytes() []uint8 {
    calls +%= 1
    return t.b
}

func rest(v []uint8) []uint8 {
    calls +%= 1
    if len(v) == 0 {
        return v
    }
    return v[1:]
}

func row(rows list[[2]uint8], none []uint8) []uint8 {
    if len(rows) > 0 {
        return rows[0]
    }
    return none
}

func all(xs ...uint8) []uint8 {
    return xs
}

func first(v []uint8, mut a [3]uint8) []uint8 {
    a[0] = 9
    return v
}

func sum(v []uint8) int64 {
    mut n := 0
    for _, c := range v {
        n +%= c
    }
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
    mut t := Text{b: make(list[uint8], c_allocator)}
    err := append(mut t.b, 1, 2, 3)
    check(err.code == 0, "appended")
    check(len(t.bytes()) == 3 && calls == 1, "len calls once")
    c, ok := t.bytes()[2]
    check(ok && c == 3 && calls == 2, "checked index calls once")
    c2, ok2 := rest(t.bytes())[5]
    check(!ok2 && c2 == 0 && calls == 4, "checked index past the end")
    mut n := 0
    for _, x := range rest(t.bytes()) {
        n +%= x
    }
    check(n == 5 && calls == 6, "range calls once")
    check(sum(t.bytes()[:]) == 6 && calls == 7, "slice calls once")
    mut rows := make(list[[2]uint8], c_allocator)
    err2 := append(mut rows, [2]uint8{4, 5})
    check(err2.code == 0 && sum(row(rows, t.bytes())) == 9 && calls == 8, "array element of a list")
    mut m := 0
    for _, x := range all(1, 2, 3) {
        m +%= x
    }
    check(m == 6 && sum(all()) == 0, "packed values ranged over")
    mut a := [3]uint8{1, 2, 3}
    check(sum(first(a, mut a)) == 6 && a[0] == 9, "a copy viewed and returned")
}
`

// TestViewResultLowering checks how viewResultCText's views lower: a
// function returning a view returns the view's struct by value; a list
// returned as a view is its buffer and length, an array element of a
// list a pointer to its first element and its length, and a slice of a
// view the slice helper's result, nothing copied; a call's view indexed
// with ok is held in a temporary, and one ranged over is held once.
func TestViewResultLowering(t *testing.T) {
	got := generate(t, viewResultCText)
	for _, want := range []string{
		"static tack_view_",
		"return (tack_view_",
		"{p_t.f_b.ptr, p_t.f_b.len};",
		".e, 2};",
		"return tack_slice_from_",
		" = tack_4_main4_rest(tack_4_main12_Text_2ebytes(v_t));\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C lacks %q:\n%s", want, got)
		}
	}
}

// TestViewResultsCallOnceAndRunClean builds viewResultCText with the
// address and undefined behavior sanitizers on and checks every check
// passes: each use of a view a call returns calls it exactly once, and
// reads only what it points into.
func TestViewResultsCallOnceAndRunClean(t *testing.T) {
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWith(t, viewResultCText, flags...)
		out, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("view results failed: %v\n%s", err, out)
		}
		if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 9 {
			t.Errorf("view results printed\n%s", out)
		}
	})
}

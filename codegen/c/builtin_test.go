package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// freestanding is a program that uses every construct the C backend lowers
// without a builtin: strings, arrays, structs, several results, defers,
// division, string comparison and indexing, loops, switches and globals.
const freestanding = `package main

type Pair struct {
    a int64
    s string
}

var g = 3

func two(x int64) (int64, bool) {
    return x / 2, x % 2 == 0
}

func main() {
    mut arr := [3]int64{1, 2, 3}
    mut p := Pair{a: 1, s: "x"}
    defer {
        p.a = 2
    }
    for i, v := range arr {
        arr[i] = v * g
    }
    q, even := two(arr[0])
    s := "hello"
    if s == p.s || even {
        p.a = q
    }
    switch len(s) {
    case 5:
        p.a++
    }
    b, ok := s[1]
    if ok && b == 'e' {
        p.a--
    }
}
`

// TestNoBuiltinNeedsNoHostedHeader checks a program that calls neither
// print nor exit includes only the headers a freestanding C implementation
// provides, and compiles as freestanding C with no library. It is only
// compiled, not linked: with no C library there is nothing to start it.
func TestNoBuiltinNeedsNoHostedHeader(t *testing.T) {
	got := generate(t, freestanding)
	for _, hosted := range []string{"<stdio.h>", "<stdlib.h>"} {
		if strings.Contains(got, hosted) {
			t.Errorf("a program without print or exit includes %s:\n%s", hosted, got)
		}
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	csrc := filepath.Join(dir, "out.c")
	if err := os.WriteFile(csrc, []byte(got), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("clang", "-std=c11", "-ffreestanding", "-nostdlib",
		"-Wall", "-Wextra", "-Werror", "-I", stdtest.IncludeDir(),
		"-c", "-o", filepath.Join(dir, "out.o"), csrc)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("freestanding compile failed: %v\n%s\n%s", err, out, got)
	}
}

// TestBuiltinsIncludeNoHostedHeader checks a program calling builtins
// includes no hosted header for them either: their bodies, which do, are
// compiled apart, from std/builtin, and only declared here.
func TestBuiltinsIncludeNoHostedHeader(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
	}{
		{"neither", []string{"if true {", "}"}},
		{"print", []string{`print("a")`}},
		{"exit", []string{"exit(1)"}},
		{"both", []string{`print("a")`, "exit(1)"}},
		{"print in a defer", []string{`defer {`, `    print("a")`, `}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate(t, program(tt.lines...))
			for _, h := range []string{"<stdio.h>", "<stdlib.h>"} {
				if strings.Contains(got, h) {
					t.Errorf("%s included:\n%s", h, got)
				}
			}
		})
	}
}

// TestBuiltinsCallTheirCNames checks print and exit lower to calls of the
// C functions builtin.tck names, each declared once, by a prototype, and
// only when used, never defined, and that nothing else calls the C library
// directly.
func TestBuiltinsCallTheirCNames(t *testing.T) {
	got := generate(t, program(`print("hi")`, "exit(42)", `print("again")`))
	for _, want := range []string{
		"tack_builtin_print((tack_string){tack_str0, TACK_C_STR_LEN(tack_str0)});",
		"tack_builtin_exit(42);",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
	for _, decl := range []string{
		"\nvoid tack_builtin_print(tack_string);\n",
		"\nvoid tack_builtin_exit(int64_t);\n",
	} {
		if strings.Count(got, decl) != 1 {
			t.Errorf("want %q declared once:\n%s", decl, got)
		}
	}
	if strings.Contains(got, "static void tack_builtin") || strings.Contains(got, "fwrite") {
		t.Errorf("a builtin's body is in the generated C:\n%s", got)
	}
	if n := strings.Count(got, "exit("); n != 2 {
		t.Errorf("want exit( twice, the call and the prototype; got %d:\n%s", n, got)
	}

	only := generate(t, program(`print("hi")`))
	if strings.Contains(only, "tack_builtin_exit") {
		t.Errorf("exit declared for a program that does not call it:\n%s", only)
	}
	none := generate(t, program("if true {", "}"))
	if strings.Contains(none, "tack_builtin_") {
		t.Errorf("a builtin declared for a program that calls none:\n%s", none)
	}
}

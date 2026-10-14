package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		"-Wall", "-Wextra", "-Werror", "-c", "-o", filepath.Join(dir, "out.o"), csrc)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("freestanding compile failed: %v\n%s\n%s", err, out, got)
	}
}

// TestHeadersFollowTheBuiltinsUsed checks each builtin brings in the one
// hosted header its C body needs, and only when the program calls it.
func TestHeadersFollowTheBuiltinsUsed(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		want     []string
		dontWant []string
	}{
		{"neither", []string{"if true {", "}"}, nil, []string{"<stdio.h>", "<stdlib.h>"}},
		{"print", []string{`print("a")`}, []string{"<stdio.h>"}, []string{"<stdlib.h>"}},
		{"exit", []string{"exit(1)"}, []string{"<stdlib.h>"}, []string{"<stdio.h>"}},
		{"both", []string{`print("a")`, "exit(1)"}, []string{"<stdio.h>", "<stdlib.h>"}, nil},
		{
			"print in a defer",
			[]string{`defer {`, `    print("a")`, `}`},
			[]string{"<stdio.h>"},
			[]string{"<stdlib.h>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate(t, program(tt.lines...))
			for _, h := range tt.want {
				if strings.Count(got, "#include "+h) != 1 {
					t.Errorf("want %s included once:\n%s", h, got)
				}
			}
			for _, h := range tt.dontWant {
				if strings.Contains(got, h) {
					t.Errorf("%s included, though nothing uses it:\n%s", h, got)
				}
			}
		})
	}
}

// TestBuiltinsCallTheirCNames checks print and exit lower to calls of the
// C functions builtin.tck names, each defined once and only when used, and
// that nothing else calls the C library directly.
func TestBuiltinsCallTheirCNames(t *testing.T) {
	got := generate(t, program(`print("hi")`, "exit(42)"))
	for _, want := range []string{
		"tack_builtin_print((tack_string){tack_str0, 2});",
		"tack_builtin_exit(42);",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
	for _, def := range []string{
		"static void tack_builtin_print(tack_string s) {",
		"static void tack_builtin_exit(int64_t code) {",
	} {
		if strings.Count(got, def) != 1 {
			t.Errorf("want %q defined once:\n%s", def, got)
		}
	}
	if n := strings.Count(got, "exit("); n != 3 {
		t.Errorf("want exit( three times, the call, the definition and C's exit; got %d:\n%s",
			n, got)
	}

	only := generate(t, program(`print("hi")`))
	if strings.Contains(only, "tack_builtin_exit") {
		t.Errorf("exit's body emitted for a program that does not call it:\n%s", only)
	}
}

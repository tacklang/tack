package c_test

import (
	"fmt"
	"math/rand/v2"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// randomPaths returns n paths of up to 12 bytes from '/', '.', 'a' and
// 'b', the bytes path's rules are about, from a fixed seed, so a failure
// repeats: runs of slashes, . and .. elements, names that only begin with
// dots, rooted and relative, and the empty path, all come up.
func randomPaths(n int) []string {
	r := rand.New(rand.NewPCG(3, 4))
	const alphabet = "/.ab"
	paths := make([]string, n)
	for i := range paths {
		b := make([]byte, r.IntN(13))
		for j := range b {
			b[j] = alphabet[r.IntN(len(alphabet))]
		}
		paths[i] = string(b)
	}
	return paths
}

// pathProgramHead is the start of the program TestPathMatchesGo builds:
// printers for each of std/path's functions, one result per line, and
// "error" for a failure. Its main follows, one call per input.
const pathProgramHead = `package main

import fmt from "std/fmt"
import path from "std/path"
import text from "std/text"

func show(t text.Text) {
    fmt.write(t.bytes())
    print("\n")
}

func clean_of(p []uint8) {
    t, e := path.clean(p, c_allocator)
    if e.code != 0 {
        print("error\n")
        return
    }
    show(t)
}

func base_of(p []uint8) {
    t, e := path.base(p, c_allocator)
    if e.code != 0 {
        print("error\n")
        return
    }
    show(t)
}

func dir_of(p []uint8) {
    t, e := path.dir(p, c_allocator)
    if e.code != 0 {
        print("error\n")
        return
    }
    show(t)
}

func one(p []uint8) {
    clean_of(p)
    base_of(p)
    dir_of(p)
    fmt.write(path.ext(p))
    print("\n")
    if path.is_abs(p) {
        print("abs\n")
    } else {
        print("rel\n")
    }
}

func two(a []uint8, b []uint8) {
    t, e := path.join(a, b, c_allocator)
    if e.code != 0 {
        print("error\n")
        return
    }
    show(t)
}

func main() {
`

// pathProgram returns the program TestPathMatchesGo builds for paths, and
// what Go's path package gives for each line it writes: clean, base, dir,
// ext and is_abs of each path, then join of each pair of them.
func pathProgram(paths []string) (src, want string) {
	var s, w strings.Builder
	s.WriteString(pathProgramHead)
	for _, p := range paths {
		fmt.Fprintf(&s, "    one([]uint8(%q))\n", p)
		abs := "rel"
		if path.IsAbs(p) {
			abs = "abs"
		}
		fmt.Fprintf(&w, "%s\n%s\n%s\n", path.Clean(p), path.Base(p), path.Dir(p))
		fmt.Fprintf(&w, "%s\n%s\n", path.Ext(p), abs)
	}
	for i := 0; i+1 < len(paths); i += 2 {
		a, b := paths[i], paths[i+1]
		fmt.Fprintf(&s, "    two([]uint8(%q), []uint8(%q))\n", a, b)
		fmt.Fprintf(&w, "%s\n", path.Join(a, b))
	}
	s.WriteString("}\n")
	return s.String(), w.String()
}

// TestPathMatchesGo builds a program that applies std/path's clean, base,
// dir, ext and is_abs to 600 random paths, and join to 300 pairs of them,
// and checks every result equals what Go's path package gives, an
// implementation written apart from Tack's.
func TestPathMatchesGo(t *testing.T) {
	src, want := pathProgram(randomPaths(600))
	bin := buildWithStd(t, src)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("path program failed: %v\n%s", err, out)
	}
	got, exp := strings.Split(string(out), "\n"), strings.Split(want, "\n")
	if len(got) != len(exp) {
		t.Fatalf("printed %d lines, want %d", len(got), len(exp))
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], exp[i])
		}
	}
}

// TestPathExampleIsSanitizerClean builds examples/path, its C file and
// std's C with clang's warnings as errors and the address and undefined
// behavior sanitizers on, every finding fatal, and checks it writes
// exactly its output, every check ok: std/path, written in Tack, reads no
// view past its end and leaks no list, through its failed allocations
// too, which the example's own count of live buffers checks.
func TestPathExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "path")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/path/output expects a check to fail")
	}
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWithStdAndC(
			t,
			src,
			csrc,
			flags...,
		)
		var stderr strings.Builder
		cmd := exec.Command(bin)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("path failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
		}
		if string(out) != want {
			t.Errorf("path wrote\n%q\nwant\n%q", out, want)
		}
	})
}

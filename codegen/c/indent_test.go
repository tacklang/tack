package c_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// nestedIfs is a main that nests n ifs around one assignment.
func nestedIfs(n int) string {
	return "package main\n\nfunc main() {\n    mut x := 0\n" +
		strings.Repeat("if x == 0 {\n", n) + "x = 1\n" + strings.Repeat("}\n", n) + "}\n"
}

// TestDeepNestingIndentsEachLevelOnce nests ifs, type switches and range
// loops hundreds deep and checks the emitted C: the innermost if body sits
// one level per if right of the function's own level, no line goes more
// than one level right of the line before it, and no indent marker is left
// in the output.
func TestDeepNestingIndentsEachLevelOnce(t *testing.T) {
	const depth = 300
	out := generate(t, nestedIfs(depth))
	want := strings.Repeat("    ", depth+1) + "v_x = 1;"
	if !strings.Contains(out, "\n"+want+"\n") {
		t.Errorf("the innermost assignment is not at %d levels", depth+1)
	}

	var mixed strings.Builder
	mixed.WriteString("package main\n\nfunc f(a any, v []int64) {\n")
	for i := range depth {
		switch i % 3 {
		case 0:
			mixed.WriteString("switch a.(type) {\ncase int64:\n")
		case 1:
			fmt.Fprintf(&mixed, "for _, e%d := range v {\nif e%d == 0 {\n}\n", i, i)
		default:
			mixed.WriteString("if len(v) > 0 {\n")
		}
	}
	mixed.WriteString("print(\"x\")\n")
	for i := depth - 1; i >= 0; i-- {
		mixed.WriteString("}\n")
	}
	mixed.WriteString("}\n\nfunc main() {\n    arr := [1]int64{0}\n    f(1, arr)\n}\n")
	checkIndentSteps(t, generate(t, mixed.String()))
}

// checkIndentSteps fails t if any line of out holds a NUL byte, is indented
// by other than a multiple of four spaces, or sits more than one level right
// of the line before it.
func checkIndentSteps(t *testing.T, out string) {
	t.Helper()
	prev := 0
	for i, line := range strings.Split(out, "\n") {
		if strings.ContainsRune(line, 0) {
			t.Fatalf("line %d holds an indent marker: %q", i+1, line)
		}
		spaces := len(line) - len(strings.TrimLeft(line, " "))
		if spaces%4 != 0 {
			t.Fatalf("line %d is indented %d spaces: %q", i+1, spaces, line)
		}
		if spaces > prev+4 {
			t.Fatalf("line %d jumps from %d to %d spaces: %q", i+1, prev, spaces, line)
		}
		if strings.TrimSpace(line) != "" {
			prev = spaces
		}
	}
}

// TestDeepNestingGeneratesInQuadraticSpace checks that doubling the nesting
// depth costs the backend at most about four times the memory, as the
// output's own size grows: indenting each line once per level around it,
// as the backend once did, made it about eight.
func TestDeepNestingGeneratesInQuadraticSpace(t *testing.T) {
	small, large := allocated(t, 500), allocated(t, 1000)
	if ratio := float64(large) / float64(small); ratio > 6 {
		t.Errorf("doubling the depth multiplied allocation by %.1f, want about 4", ratio)
	}
}

// allocated reports the bytes the backend allocates generating nestedIfs(n).
func allocated(t *testing.T, n int) uint64 {
	t.Helper()
	files := ast.SingleFile(compile(t, nestedIfs(n)))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, diags := c.New().Generate(files, stdtest.MustBuiltins()); len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// elseIfChain is a func pick that returns i for x == i, chaining n branches
// with else if, and n when none matches.
func elseIfChain(n int) string {
	var b strings.Builder
	b.WriteString("func pick(x int64) int64 {\n    if x == 0 {\n        return 0\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "    } else if x == %d {\n        return %d\n", i, i)
	}
	fmt.Fprintf(&b, "    } else {\n        return %d\n    }\n}\n", n)
	return b.String()
}

// TestElseIfChainsStayFlat lowers a chain of a thousand else ifs and checks
// it stays at the function's own level in C, as a switch does, rather than
// nesting each branch inside the last: past maxChain branches each is an
// if of its own, which clang does not nest as it does else if, and a
// chain of 64 is still C's else if.
func TestElseIfChainsStayFlat(t *testing.T) {
	out := generate(t, inMainProgram(elseIfChain(1000), "print(\"\")"))
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, strings.Repeat(" ", 12)) {
			t.Fatalf("line %d is nested three levels deep: %q", i+1, line)
		}
	}
	if !strings.Contains(out, "\n    if (p_x == 999) {\n") || strings.Contains(out, "else if") {
		t.Errorf("the last branch is not an if of its own at the function's level:\n%s", out)
	}
	short := generate(t, inMainProgram(elseIfChain(64), "print(\"\")"))
	if !strings.Contains(short, "\n    } else if (p_x == 63) {\n") {
		t.Errorf("a chain of 64 is not C's else if:\n%s", short)
	}
}

// TestElseIfChainsRun runs else if chains, with defers in their branches and
// an else block that holds an if and more, which stays nested, and checks
// each takes the branch it should.
func TestElseIfChainsRun(t *testing.T) {
	decls := elseIfChain(200) + `
func branches(x int64) int64 {
    mut n := 0
    if x == 1 {
        defer {
            print("one\n")
        }
        n = 10
    } else if x == 2 {
        defer {
            print("two\n")
        }
        n = 20
    } else {
        if x == 3 {
            n = 30
        }
        n = n + 1
    }
    return n
}
`
	stdout, status := run(t, inMainProgram(decls,
		"if pick(0) != 0 || pick(137) != 137 || pick(199) != 199 || pick(-1) != 200 {",
		"    exit(1)",
		"}",
		"if branches(1) != 10 || branches(2) != 20 || branches(3) != 31 || branches(4) != 1 {",
		"    exit(2)",
		"}",
	))
	if status != 0 || stdout != "one\ntwo\n" {
		t.Errorf("got status %d and %q, want 0 and %q", status, stdout, "one\ntwo\n")
	}
}

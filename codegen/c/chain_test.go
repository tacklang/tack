package c_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// chainProgram writes a program of three chains of n ifs each, an if and
// n-1 else ifs, which the backend lowers flat past maxChain, and checks,
// each check printing its name and ok or FAIL, that:
//
//   - pick, whose every branch returns, returns from the branch that
//     holds, and its conditions, each counting itself, run in order and
//     only until one holds, then none at all after;
//   - classify, whose branches assign, takes exactly one branch, or its
//     else when none holds;
//   - a loop whose chain continues at 3 and breaks at 5 leaves as a loop
//     does.
func chainProgram(n int) string {
	var b strings.Builder
	b.WriteString("package main\n\nmut evals int64\n\n")
	b.WriteString(
		"func probe(v int64, want int64) bool {\n    evals +%= 1\n    return v == want\n}\n\n",
	)
	b.WriteString("func check(cond bool, name string) {\n    print(name)\n    if cond {\n" +
		"        print(\" ok\\n\")\n    } else {\n        print(\" FAIL\\n\")\n    }\n}\n\n")

	b.WriteString("func pick(x int64) int64 {\n    if probe(x, 0) {\n        return 0\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "    } else if probe(x, %d) {\n        return %d\n", i, i)
	}
	b.WriteString("    }\n    return -1\n}\n\n")

	b.WriteString(
		"func classify(x int64) int64 {\n    mut r int64 = -2\n    if x == 0 {\n        r = 0\n",
	)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "    } else if x == %d {\n        r = %d\n", i, i)
	}
	b.WriteString("    } else {\n        r = -1\n    }\n    return r\n}\n\n")

	writeWalk(&b, n)

	b.WriteString("func main() {\n")
	mid := n * 7 / 9
	fmt.Fprintf(
		&b,
		"    check(pick(%d) == %d && evals == %d, \"the branch that holds, its conditions in order\")\n",
		mid,
		mid,
		mid+1,
	)
	fmt.Fprintf(
		&b,
		"    evals = 0\n    check(pick(%d) == -1 && evals == %d, \"every condition once when none holds\")\n",
		n+5,
		n,
	)
	b.WriteString(
		"    evals = 0\n    check(pick(0) == 0 && evals == 1, \"the first branch, no other condition\")\n",
	)
	fmt.Fprintf(
		&b,
		"    check(classify(%d) == %d && classify(1) == 1, \"exactly one branch\")\n",
		n-1,
		n-1,
	)
	b.WriteString("    check(classify(-3) == -1, \"the else when none holds\")\n")
	b.WriteString("    check(walk() == 7, \"continue and break in a branch\")\n")
	b.WriteString("}\n")
	return b.String()
}

// writeWalk writes walk, a loop over 0 to 9 whose chain of n ifs adds
// each number to a sum, but continues at 3 and breaks at 5, so it
// returns 0 + 1 + 2 + 4.
func writeWalk(b *strings.Builder, n int) {
	b.WriteString("func walk() int64 {\n    mut sum int64 = 0\n    for i := range 10 {\n" +
		"        if i == 0 {\n            sum += 0\n")
	for i := 1; i < n; i++ {
		switch i {
		case 3:
			b.WriteString("        } else if i == 3 {\n            continue\n")
		case 5:
			b.WriteString("        } else if i == 5 {\n            break\n")
		default:
			fmt.Fprintf(b, "        } else if i == %d {\n            sum +%%= %d\n", i, i)
		}
	}
	b.WriteString("        }\n    }\n    return sum\n}\n\n")
}

// chainChecks is how many checks chainProgram prints.
const chainChecks = 6

// TestLongElseIfChainsRun builds chainProgram's program with chains of
// 10000 ifs, past where clang ran out of stack on else ifs, at -O0 and
// with the address and undefined behavior sanitizers, every finding fatal,
// and checks every check passes. TestLongElseIfChainsAreFlat checks a
// longer chain's C without building it.
func TestLongElseIfChainsRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds large programs")
	}
	for _, tc := range []struct {
		flags []string
		n     int
	}{
		{[]string{"-O0"}, 10000},
		{[]string{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, 10000},
	} {
		t.Run(fmt.Sprintf("%d %s", tc.n, strings.Join(tc.flags, " ")), func(t *testing.T) {
			prog := compileWithStd(t, chainProgram(tc.n))
			runChecks(t, buildProgram(t, prog, "", tc.flags...), chainChecks)
		})
	}
}

// TestLongElseIfChainsAreFlat checks a chain past maxChain is written as
// ifs one after another, with no else if and no nesting a level per
// branch, and one of maxChain branches as C's else if; and that clang
// takes the C of chains of 50000 ifs, warnings as errors, checking its
// syntax and types only, which is quick.
func TestLongElseIfChainsAreFlat(t *testing.T) {
	for _, tc := range []struct {
		n        int
		elseIfs  int
		maxDepth int
	}{
		{64, 3 * 63, 8},
		{65, 0, 8},
		{10000, 0, 8},
		{50000, 0, 8},
	} {
		prog := compileWithStd(t, chainProgram(tc.n))
		out, diags := c.New().Generate(prog, stdtest.MustBuiltins())
		if len(diags) > 0 {
			t.Fatalf("generate reported %+v", diags)
		}
		src := string(out)
		if got := strings.Count(src, "} else if "); got != tc.elseIfs {
			t.Errorf("%d branches: %d else ifs, want %d", tc.n, got, tc.elseIfs)
		}
		if depth, line := deepestBraces(src); depth > tc.maxDepth {
			t.Errorf("%d branches: C nests %d levels deep at line %d, want at most %d",
				tc.n, depth, line, tc.maxDepth)
		}
		if tc.n == 50000 {
			syntaxOnly(t, src)
		}
	}
}

// syntaxOnly checks clang takes src, C, with its warnings as errors,
// checking its syntax and types but generating no code.
func syntaxOnly(t *testing.T, src string) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	path := filepath.Join(t.TempDir(), "program.c")
	writeFile(t, path, src)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only",
		"-I", stdtest.IncludeDir(), path,
	}
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
}

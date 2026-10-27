package c_test

import (
	"regexp"
	"strings"
	"testing"
)

// loopRuntimeCases are programs with loops, and what each must print and
// exit with.
var loopRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"counts",
		"package main\n\nfunc main() {\n    mut i = 0\n    for i < 10 {\n        i = i + 1\n    }\n    code, fits := int32(i)\n    if fits { exit(code) }\n}\n",
		"",
		10,
	},
	{
		"false condition never runs the body",
		"package main\n\nfunc main() {\n    for false {\n        exit(1)\n    }\n    exit(2)\n}\n",
		"", 2,
	},
	{
		"break leaves forever",
		"package main\n\nfunc main() {\n    mut i = 0\n    for {\n        i = i + 1\n        if i == 7 {\n            break\n        }\n    }\n    code, fits := int32(i)\n    if fits { exit(code) }\n}\n",
		"",
		7,
	},
	{
		"continue skips the rest of the body",
		"package main\n\nfunc main() {\n    mut i = 0\n    mut odd = 0\n    for i < 10 {\n        i = i + 1\n        if i % 2 == 0 {\n            continue\n        }\n" +
			"        odd = odd + 1\n    }\n    code, fits := int32(odd)\n    if fits { exit(code) }\n}\n",
		"",
		5,
	},
	{
		"break leaves only the inner loop",
		"package main\n\nfunc main() {\n    mut n = 0\n    mut i = 0\n    for i < 3 {\n        i = i + 1\n        for {\n            n = n + 1\n            break\n        }\n" +
			"        n = n + 10\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		"",
		33,
	},
	{
		"returns from forever",
		"package main\n\nfunc first(over int64) int64 {\n    mut i = 0\n    for {\n        if i * i > over {\n            return i\n        }\n        i = i + 1\n    }\n}\n\n" +
			"func main() {\n    code, fits := int32(first(50))\n    if fits { exit(code) }\n}\n",
		"",
		8,
	},
	{
		"fresh variable each iteration",
		"package main\n\nfunc main() {\n    mut i = 0\n    mut sum = 0\n    for i < 4 {\n        sq := i * i\n        sum = sum + sq\n        i = i + 1\n    }\n    code, fits := int32(sum)\n    if fits { exit(code) }\n}\n",
		"",
		14,
	},
	{
		"condition re-evaluated each time",
		"package main\n\nmut calls int64\n\nfunc more() bool {\n    calls = calls + 1\n    print(\"check\")\n    return calls < 3\n}\n\n" +
			"func main() {\n    for more() {\n    }\n    code, fits := int32(calls)\n    if fits { exit(code) }\n}\n",
		"checkcheckcheck",
		3,
	},
	{
		"iterative fib",
		"package main\n\nfunc fib(n int64) int64 {\n    mut a = 0\n    mut b = 1\n    mut i = 0\n    for i < n {\n        next := a + b\n        a = b\n        b = next\n        i = i + 1\n    }\n    return a\n}\n\n" +
			"func main() {\n    code, fits := int32(fib(10))\n    if fits { exit(code) }\n}\n",
		"",
		55,
	},
	{
		"exit inside forever",
		"package main\n\nfunc main() {\n    for {\n        print(\"once\")\n        exit(4)\n    }\n}\n",
		"once",
		4,
	},
}

// Each of loopRuntimeCases, built with clang's warnings as errors (see
// run), prints and exits as the case says.
func TestLoopsAtRuntime(t *testing.T) {
	for _, tt := range loopRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			stdout, status := run(t, tt.src)
			if stdout != tt.stdout || status != tt.status {
				t.Errorf(
					"got stdout %q, status %d; want %q, %d",
					stdout,
					status,
					tt.stdout,
					tt.status,
				)
			}
		})
	}
}

// loopFormsSrc has every form of Tack loop, in three functions: a
// three-clause loop with all its parts, with no condition and with no
// post statement; a loop with a condition, a negated one included, and
// one with none; and a range over an integer, an array, a string, a
// view, a list and a map.
const loopFormsSrc = `package main

func clause() {
    for mut n := 0; n < 4; n++ {
        n = -5
    }
    for mut m := 0; ; m++ {
        if m > 3 {
            break
        }
    }
    for mut k := 0; k < 4; {
        k++
    }
}

func cond(i int64) {
    for i < 4 {
    }
    for !(i == 4) {
    }
    for {
    }
}

func ranges(v []int64) {
    for range 3 {
    }
    a := [2]int64{1, 2}
    for range a {
    }
    for range "ab" {
    }
    for range v {
    }
    mut l := make(list[int64], c_allocator)
    for range l {
    }
    mut m := make(map[int64]int64, c_allocator)
    for range m {
    }
}

func main() {
    clause()
    cond(0)
    one := [1]int64{1}
    ranges(one)
}
`

// forHeader matches the opening line of a C for, capturing what lies
// between its first two semicolons: its controlling expression.
var forHeader = regexp.MustCompile(`for \([^;]*;([^;]*);`)

// No loop lowered from Tack has a controlling expression that would let
// the C compiler assume the loop ends: every C loop of the functions the
// program declares is a for whose controlling expression is left out, so
// it is a constant, and none is a while. C11 6.8.5p6 lets a compiler
// assume that a loop whose controlling expression is not a constant, and
// whose body has no side effects, ends, and clang deletes one that never
// does.
func TestLoopsHaveNoControllingExpression(t *testing.T) {
	got := generate(t, loopFormsSrc)
	fors := 0
	for _, fn := range []string{"clause", "cond", "ranges"} {
		body := funcBody(t, got, fn)
		if strings.Contains(body, "while") {
			t.Errorf("%s lowers a loop to a while:\n%s", fn, body)
		}
		for line := range strings.SplitSeq(body, "\n") {
			if !strings.Contains(line, "for (") {
				continue
			}
			fors++
			m := forHeader.FindStringSubmatch(line)
			if m == nil || strings.TrimSpace(m[1]) != "" {
				t.Errorf("%s has a for with a controlling expression: %q", fn, line)
			}
		}
	}
	if fors != 12 {
		t.Errorf("found %d C fors, want one for each of the 12 loops:\n%s", fors, got)
	}
}

// funcBody returns the definition, from its first line to the line
// before its closing brace, of the function of the main package named
// name in the generated C src.
func funcBody(t *testing.T, src, name string) string {
	t.Helper()
	def := regexp.MustCompile(`(?m)^static void tack_4_main\d+_` + name + `\([^)]*\) \{$`)
	loc := def.FindStringIndex(src)
	if loc == nil {
		t.Fatalf("no definition of %s in:\n%s", name, src)
	}
	end := strings.Index(src[loc[0]:], "\n}\n")
	if end < 0 {
		t.Fatalf("the definition of %s has no end in:\n%s", name, src)
	}
	return src[loc[0] : loc[0]+end]
}

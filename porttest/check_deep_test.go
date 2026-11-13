package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkError is a statement each shape of TestCheckDeep ends main's body
// with, an error the type checker reports, so --check's output is the
// checker's alone and the backend, which runs only on a program with no
// error, decides nothing.
const checkError = "return 1\n"

// checkNests are the bodies TestCheckDeep nests n levels deep, each in
// main, ending in checkError: blocks of every statement that holds one,
// and expressions the checker recurses into a level at a time.
var checkNests = map[string]func(n int) string{
	"ifs":      func(n int) string { return rep("if true {\n", n) + checkError + rep("}\n", n) },
	"elses":    func(n int) string { return rep("if true {\n} else {\n", n) + checkError + rep("}\n", n) },
	"fors":     func(n int) string { return rep("for true {\n", n) + checkError + rep("}\n", n) },
	"ranges":   func(n int) string { return rep("for range 3 {\n", n) + checkError + rep("}\n", n) },
	"switches": func(n int) string { return rep("switch {\ncase true:\n", n) + checkError + rep("}\n", n) },
	"defers":   func(n int) string { return rep("defer {\n", n) + checkError + rep("}\n", n) },
	"parens":   func(n int) string { return "x := " + rep("(", n) + "1" + rep(")", n) + "\nreturn x\n" },
	"nots":     func(n int) string { return "b := " + rep("!(", n/2) + "true" + rep(")", n/2) + "\nreturn b\n" },
	"arraylits": func(n int) string {
		return "x := " + rep("[1]", n/2) + "int64" + rep("{", n/2) + "1" + rep("}", n/2) + "\nreturn x\n"
	},
}

// checkChains are the programs TestCheckDeep writes n links long: the
// chains a body can make as long as it likes, which the checker must walk
// without a frame a link.
var checkChains = map[string]func(n int) string{
	"elseifs": func(n int) string {
		return inMain("x := 1\nif x == 0 {\n}" + rep(" else if x == 1 {\n}", n) + "\n" + checkError)
	},
	"stmts": func(n int) string { return inMain(rep("if true {\n}\n", n) + checkError) },
	"ands":  func(n int) string { return inMain("b := " + rep("true && ", n) + "true\nreturn b\n") },
	"compares": func(n int) string {
		return inMain("b := " + rep("1 < 2 && ", n) + "true\nreturn b\n")
	},
	"locals": func(n int) string {
		var b strings.Builder
		b.WriteString("a0 := 1\n")
		for i := range n {
			fmt.Fprintf(&b, "a%d := a%d\n", i+1, i)
		}
		fmt.Fprintf(&b, "return a%d\n", n)
		return inMain(b.String())
	},
	// A function whose result an else if chain returns, each link's
	// branch, and the else, ending in a return, so whether it ends in one
	// walks the whole chain.
	"returns": func(n int) string {
		return "package main\n\nfunc f(x int64) int64 {\n    if x == 0 {\n        return 0\n    }" +
			rep(" else if x == 1 {\n        return 1\n    }", n) + " else {\n        return 2\n    }\n}\n\n" +
			"func main() {\n" + checkError + "}\n"
	},
	// Structs each holding the next by value, a local of the first, and a
	// selector down the whole chain.
	"fields": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\n")
		for i := range n {
			fmt.Fprintf(&b, "type S%d struct {\n    f S%d\n}\n", i, i+1)
		}
		fmt.Fprintf(&b, "type S%d struct {\n    v int64\n}\n\n", n)
		b.WriteString("func main() {\n    mut s S0\n    x := s" + rep(".f", n) + ".v\n    return x\n}\n")
		return b.String()
	},
}

// checkBadChains are chains of operators chainLength links long whose
// operands are invalid or untyped at the bottom, or whose every link is
// an error, each the value of a local in main ending in checkError:
// what the checker works out about such a chain, its value among it,
// gets nothing remembered for any link, and must still be worked out
// without a frame a link.
var checkBadChains = map[string]string{
	"known-muls": "a := 3\nx := a" + rep(" * a", chainLength),
	"strings":    "x := \"a\"" + rep(" + \"a\"", chainLength),
	"bool-ints":  "x := true" + rep(" + 1", chainLength),
	"undefined":  "x := nope" + rep(" + 1", chainLength),
	"zero-divs":  "x := 5" + rep(" / 0", chainLength),
	"shifts":     "x := 1" + rep(" << 1", chainLength),
	"int8-muls":  "x := int8(1)" + rep(" * 100", chainLength),
	"float-rems": "x := 1.5" + rep(" % 1", chainLength),
	"short-muls": "a := 3\nx := a" + rep(" * a", 20000),
}

// TestCheckDeep runs each of checkNests as deeply as the parser lets a
// body nest, and each of checkChains chainLength links long, through both
// compilers with --check --max-errors 0, compared as check-core compares
// its cases, the port's stack held to deepStack KB: a port whose body
// check recurses with a chain's length, or past its stack at the nesting
// limit, fails here, as does one that declines any of them once
// check-core is required. Each of checkBadChains is run with --port-types
// too, compared as TestTypes compares a case of check-core. It runs only
// as TestPort does.
func TestCheckDeep(t *testing.T) {
	ready(t)
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	shapes := make(map[string]string)
	for name, nest := range checkNests {
		// The function's braces and those around the nesting take a few
		// levels of the parser's count.
		shapes[name] = inMain(nest(deepLimit - 4))
	}
	for name, chain := range checkChains {
		shapes[name] = chain(chainLength)
	}
	bad := make(map[string]bool)
	for name, body := range checkBadChains {
		shapes[name] = inMain(body + "\n" + checkError)
		bad[name] = true
	}
	dir := t.TempDir()
	for name, src := range shapes {
		file := name + ".tck"
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			in := input{name: file, dir: dir, file: file}
			runCaseWith(t, *st, in, env.portRun)
			if bad[name] {
				checkTypes(t, in, env.portRun, false)
			}
		})
	}
}

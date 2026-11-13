package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deepStack is the stack, in KB, TestDeepNesting runs the port with: half
// of the 8 MB Linux gives a program by default, so the port's recursion
// is held to a margin at the nesting limit.
const deepStack = 4096

// deepLimit is the parser's nesting limit, maxNesting, which a program
// may reach and not pass.
const deepLimit = 1000

// ladder is an expression that climbs every precedence, which Go's
// parser recurses through once each, and the port's must not.
const ladder = "a || a && a == a + a * "

// deepShapes are the programs TestDeepNesting nests to deepLimit and one
// past it, each from its depth n: those that cost the parsers the most
// stack for each level of nesting, in expressions, statements and types,
// and in each place an expression is parsed from.
var deepShapes = map[string]func(n int) string{
	"parens":     func(n int) string { return inMain("x := " + rep("(", n) + "1" + rep(")", n)) },
	"calls":      func(n int) string { return inMain("x := " + rep("f(", n) + "1" + rep(")", n)) },
	"structlits": func(n int) string { return inMain("x := " + rep("S{s: ", n) + "1" + rep("}", n)) },
	"arraylits":  func(n int) string { return inMain("x := [1]int64" + rep("{", n) + "1" + rep("}", n)) },
	"indexes":    func(n int) string { return inMain("x := " + rep("t[", n) + "0" + rep("]", n)) },
	"methods":    func(n int) string { return inMain("x := " + rep("a.m(", n) + "1" + rep(")", n)) },
	"muts":       func(n int) string { return inMain("f(" + rep("mut ", n) + "x)") },
	"ladcall":    func(n int) string { return inMain("x := " + rep("f("+ladder, n) + "1" + rep(")", n)) },
	"ladparen":   func(n int) string { return inMain("x := " + rep("("+ladder, n) + "1" + rep(")", n)) },
	"ladlit":     func(n int) string { return inMain("x := " + rep("S{a: "+ladder, n) + "1" + rep("}", n)) },
	"ladindex":   func(n int) string { return inMain("x := " + rep("t["+ladder, n) + "1" + rep("]", n)) },
	"ladmethod":  func(n int) string { return inMain("x := " + rep("a.m("+ladder, n) + "1" + rep(")", n)) },
	"ladstmt":    func(n int) string { return inMain(rep("f("+ladder, n) + "1" + rep(")", n)) },
	"ladif":      func(n int) string { return inMain("if " + rep("f("+ladder, n) + "1" + rep(")", n) + " {\n    }") },
	"ladglobal": func(n int) string {
		return "package main\n\nvar x = " + rep("f("+ladder, n) + "1" + rep(")", n) + "\n"
	},
	"ladensures": func(n int) string {
		return "package main\n\nfunc f() int64 ensures " + rep("f("+ladder, n) + "1" + rep(")", n) + " {\n}\n"
	},
	"ladarraylen": func(n int) string {
		return "package main\n\nfunc f(a [" + rep("f("+ladder, n) + "1" + rep(")", n) + "]int64) {\n}\n"
	},
	"ifs":      func(n int) string { return inMain(rep("if b {\n", n) + rep("}\n", n)) },
	"switches": func(n int) string { return inMain(rep("switch x {\ncase 1:\n", n) + rep("}\n", n)) },
	"fors":     func(n int) string { return inMain(rep("for b {\n", n) + rep("}\n", n)) },
	"regions":  func(n int) string { return inMain(rep("region r {\n", n) + rep("}\n", n)) },
	"funclits": func(n int) string { return inMain(rep("x := func() {\n", n) + rep("}\n", n)) },
	"funclitargs": func(n int) string {
		return inMain("x := " + rep("f(func() {\n", n) + rep("})\n", n))
	},
	"mixed": func(n int) string {
		return inMain(rep("if b {\nx := func() {\n", n/2) + rep("}\n}\n", n/2))
	},
	"listtypes": func(n int) string {
		return "package main\n\nfunc f(a " + rep("list[", n) + "int64" + rep("]", n) + ") {\n}\n"
	},
	"functypes": func(n int) string {
		return "package main\n\nfunc f(a " + rep("func(", n) + "int64" + rep(")", n) + ") {\n}\n"
	},
}

// deepDumped are the shapes whose trees TestDeepNesting has both compilers
// dump at the limit; the others' dumps, which Go indents to hundreds of
// megabytes, are left out by a syntax error after the nesting, so the
// parse still goes all the way down and the diagnostics are compared.
var deepDumped = map[string]bool{"parens": true, "muts": true, "listtypes": true, "functypes": true}

// inMain returns a program whose main function's body is body.
func inMain(body string) string {
	return "package main\n\nfunc main() {\n    " + body + "\n}\n"
}

// rep returns s n times over.
func rep(s string, n int) string { return strings.Repeat(s, n) }

// stackWrapper writes, in dir, a shell script that runs the port with
// its stack held to deepStack KB, and returns its path, for the tests
// that hold the port to that margin.
func stackWrapper(t testing.TB, dir string) string {
	t.Helper()
	wrapper := filepath.Join(dir, "port-stack")
	script := fmt.Sprintf("#!/bin/sh\nulimit -s %d || exit 125\nexec %q \"$@\"\n", deepStack, env.portBin)
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil { //nolint:gosec // G306: a script it runs
		t.Fatal(err)
	}
	return wrapper
}

// TestDeepNesting runs each of deepShapes at the nesting limit and one
// past it through both compilers with --ast, compared as ast-single
// compares them, the port with its stack held to deepStack KB, so a port
// whose recursion costs more stack for each level of nesting than that
// allows fails here. The port runs as the harness builds it. It runs only
// as TestPort does, with TACK_PORT_TEST=1 and -count.
func TestDeepNesting(t *testing.T) {
	ready(t)
	st := stageNamed("ast-single")
	if st == nil {
		t.Fatal("no ast-single stage")
	}
	dir := t.TempDir()
	wrapper := stackWrapper(t, dir)
	for name, shape := range deepShapes {
		for _, n := range []int{deepLimit, deepLimit + 1} {
			src := shape(n)
			if n == deepLimit && !deepDumped[name] {
				src += ")\n"
			}
			file := fmt.Sprintf("%s.%d.tck", name, n)
			t.Run(file, func(t *testing.T) {
				t.Parallel()
				if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				runCaseWith(t, *st, input{name: file, dir: dir, file: file}, wrapper)
			})
		}
	}
}

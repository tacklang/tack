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
	// The shapes below cost the resolver more than the parser: each nests
	// a construct it resolves in a scope or a way of its own.
	"typeswitches": func(n int) string {
		return inMain("a := 1\n" + rep("switch v := a.(type) {\ncase int64:\n", n) + rep("}\n", n))
	},
	"defers":        func(n int) string { return inMain(rep("defer {\n", n) + rep("}\n", n)) },
	"ranges":        func(n int) string { return inMain("v := 1\n" + rep("for _, x := range v {\n", n) + rep("}\n", n)) },
	"elseblocks":    func(n int) string { return inMain(rep("if b {\n} else {\n", n) + rep("}\n", n)) },
	"fieldsloops":   func(n int) string { return inMain(rep("for f := fields(T) {\n", n) + rep("}\n", n)) },
	"switchdefault": func(n int) string { return inMain(rep("switch {\ndefault:\n", n) + rep("}\n", n)) },
	"maptypes": func(n int) string {
		return "package main\n\nfunc f(a " + rep("map[int64]", n) + "int64) {\n}\n"
	},
	"arraytypes": func(n int) string {
		return "package main\n\nfunc f(a " + rep("[1]", n) + "int64) {\n}\n"
	},
	"fieldtype": func(n int) string {
		return "package main\n\ntype S struct {\n    a " + listType(n) + "\n}\n"
	},
	"uniontype": func(n int) string {
		return "package main\n\ntype U union {\n    A " + listType(n) + "\n}\n"
	},
	"ifacetype": func(n int) string {
		return "package main\n\ntype I interface {\n    m(a " + listType(n) + ")\n}\n"
	},
	"globaltype":    func(n int) string { return "package main\n\nvar g " + listType(n) + "\n" },
	"resultlist":    func(n int) string { return "package main\n\nfunc f() " + listType(n) + " {\n}\n" },
	"localtype":     func(n int) string { return inMain("var x " + listType(n)) },
	"localfunctype": func(n int) string { return inMain("var x " + rep("func(", n) + "int64" + rep(")", n)) },
	"asserts":       func(n int) string { return inMain("a := 1\n    x := a.(" + listType(n) + ")") },
	"typeargs":      func(n int) string { return inMain("x := f[" + listType(n-1) + "](1)") },
	"make":          func(n int) string { return inMain("x := make(" + listType(n-1) + ", c_allocator)") },
	"litparams":     func(n int) string { return inMain("x := func(a " + listType(n-1) + ") {\n    }") },
	"closurecalls": func(n int) string {
		return inMain("x := " + rep("func() int64 { return ", n/2) + "1" + rep(" }()", n/2))
	},
	"deferlits": func(n int) string {
		return inMain(rep("defer {\nx := func() {\n", n/3) + rep("}\n}\n", n/3))
	},
	"kvlits":  func(n int) string { return inMain("x := " + rep("S{s: S{t: ", n/2) + "1" + rep("}}", n/2)) },
	"unaries": func(n int) string { return inMain("x := " + rep("- ", n) + "1") },
	"nots":    func(n int) string { return inMain("x := " + rep("!", n) + "b") },
}

// listType returns list[list[...int64...]], n lists deep.
func listType(n int) string { return rep("list[", n) + "int64" + rep("]", n) }

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

// writeStackWrapper writes, in dir, a shell script that runs the port
// with its stack held to deepStack KB, and returns its path, or why it
// could not write it. TestMain writes the one every run of the port goes
// through, env.portRun.
func writeStackWrapper(dir string) (string, error) {
	wrapper := filepath.Join(dir, "port-stack")
	script := fmt.Sprintf("#!/bin/sh\nulimit -s %d || exit 125\nexec %q \"$@\"\n", deepStack, env.portBin)
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil { //nolint:gosec // G306: a script it runs
		return "", fmt.Errorf("writing the stack wrapper: %w", err)
	}
	return wrapper, nil
}

// TestDeepNesting runs each of deepShapes at the nesting limit and one
// past it through both compilers with --ast and with --res, compared as
// ast-single and res-single compare them, the port with its stack held
// to deepStack KB, so a port whose recursion, the parser's or the
// resolver's, costs more stack for each level of nesting than that
// allows fails here. A shape left undumped ends in a syntax error, which
// the parser recovers from, so the resolver still walks the whole tree
// and only its diagnostics are compared. The port runs as the harness
// builds it. It runs only as TestPort does, with TACK_PORT_TEST=1 and
// -count.
func TestDeepNesting(t *testing.T) {
	ready(t)
	for _, stName := range []string{"ast-single", "res-single"} {
		st := stageNamed(stName)
		if st == nil {
			t.Fatalf("no %s stage", stName)
		}
		// Each stage writes its files in a directory of its own, so no
		// run reads a file another is writing.
		dir := t.TempDir()
		for name, shape := range deepShapes {
			for _, n := range []int{deepLimit, deepLimit + 1} {
				src := shape(n)
				if n == deepLimit && !deepDumped[name] {
					src += ")\n"
				}
				file := fmt.Sprintf("%s.%d.tck", name, n)
				t.Run(stName+"/"+file, func(t *testing.T) {
					t.Parallel()
					if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
						t.Fatal(err)
					}
					runCaseWith(t, *st, input{name: file, dir: dir, file: file}, env.portRun)
				})
			}
		}
	}
}

// chainLength is how many links TestDeepChains gives each chain.
const chainLength = 100000

// deepChains are the programs TestDeepChains runs: chains the parser
// builds with no nesting it counts, so a tree a node deeper for each
// link, however long the source makes it, each from its length n, and
// each naming something undefined, so the resolver reports it and no
// tree is dumped. The resolver walks each chain to its end, its first
// link last, which a recursion on the stack would need a frame a link
// for.
var deepChains = map[string]func(n int) string{
	"selectors":  func(n int) string { return inMain("x := a" + rep(".b", n)) },
	"binaries":   func(n int) string { return inMain("x := a" + rep(" + a", n)) },
	"indexes":    func(n int) string { return inMain("x := a" + rep("[0]", n)) },
	"slices":     func(n int) string { return inMain("x := a" + rep("[:]", n)) },
	"methods":    func(n int) string { return inMain("x := a" + rep(".m()", n)) },
	"asserts":    func(n int) string { return inMain("x := a" + rep(".(int64)", n)) },
	"questions":  func(n int) string { return inMain("x := f()" + rep("?", n)) },
	"qualified":  func(n int) string { return inMain("x := fmt.a" + rep(".b", n)) },
	"targets":    func(n int) string { return inMain("a" + rep(".b", n) + " = 1") },
	"targetidxs": func(n int) string { return inMain("a" + rep("[0]", n) + " = 1") },
	"elseifs":    func(n int) string { return inMain("if b {\n} " + rep("else if b {\n} ", n)) },
	"statements": func(n int) string { return inMain(rep("x := 1\n", n)) },
	"resolved": func(n int) string {
		return inMain("a := 1\n    x := a" + rep(" + a", n) + "\n    a = x")
	},
}

// TestDeepChains runs each of deepChains, chainLength links long, through
// both compilers with --res, compared as res-single compares them, the
// port with its stack held to deepStack KB. resolved has no error, and
// its tree nests too deeply for Go's encoder, which both refuse. It runs
// only as TestPort does, with TACK_PORT_TEST=1 and -count.
func TestDeepChains(t *testing.T) {
	ready(t)
	st := stageNamed("res-single")
	if st == nil {
		t.Fatal("no res-single stage")
	}
	dir := t.TempDir()
	for name, chain := range deepChains {
		file := name + ".tck"
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			if err := os.WriteFile(filepath.Join(dir, file), []byte(chain(chainLength)), 0o600); err != nil {
				t.Fatal(err)
			}
			runCaseWith(t, *st, input{name: file, dir: dir, file: file}, env.portRun)
		})
	}
}

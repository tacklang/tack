package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	// An if whose condition is an && chain, whose facts the checker keeps
	// for its branches.
	"if-ands": func(n int) string {
		return inMain("if " + rep("true && ", n) + "true {\n}\n" + checkError)
	},
	// Early exits each proving again what an index needs only part of,
	// each fact over the rest of the block, all of them in force at the
	// index, which is reported with its notes.
	"exit-facts": func(n int) string {
		return "package main\n\nfunc f(x int64, a [4]int64) int64 {\n" +
			rep("    if x < 0 {\n        return 0\n    }\n", n) + "    return a[x]\n}\n\n" +
			"func main() {\n" + checkError + "}\n"
	},
	// An else if chain each of whose branches returns, which gives the
	// statement after it what every condition being false says, each
	// link's facts held over a region of the rest of the chain, so Go's
	// checker takes time quadratic in it, and it is exitChainLength long.
	"exit-elseifs": func(int) string {
		return "package main\n\nfunc f(x int64, a [4]int64) int64 {\n    if x < 0 {\n        return 0\n    }" +
			rep(" else if x > 3 {\n        return 1\n    }", exitChainLength) + "\n    return a[x]\n}\n\n" +
			"func main() {\n" + checkError + "}\n"
	},
	// An else if chain each of whose conditions gives its else branch a
	// value ruled out, all of them in force at the last link, whose index
	// its own condition proves in range.
	"elseif-facts": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nfunc f(x int64, a [4]int64) int64 {\n    if x == 0 {\n        return a[0]\n    }")
		for i := range n {
			fmt.Fprintf(&b, " else if x == %d {\n        return a[1]\n    }", i+2)
		}
		b.WriteString(" else if x >= 0 && x < 4 {\n        return a[x]\n    }\n    return 0\n}\n\n" +
			"func main() {\n" + checkError + "}\n")
		return b.String()
	},
	// Sums n terms long, of a value whose bounds an early exit sets,
	// each link proven from the one before, and of one with none, whose
	// first link is reported and every later one rests on it; and chains
	// of wrapping additions, divisions, masks added to, and of an index
	// adding 0 to a value an early exit bounds.
	"sums": func(n int) string {
		return "package main\n\nfunc f(a int64) int64 {\n    if a < 0 || a > 10 {\n        return 0\n    }\n    x := a" +
			rep(" + a", n) + "\n    return x\n}\n\nfunc g(a int64) int64 {\n    x := a" + rep(" + a", n) +
			"\n    return x\n}\n\nfunc main() {\n" + checkError + "}\n"
	},
	"wraps": func(n int) string { return arithChain("int64", rep(" +% a", n)) },
	"divs":  func(n int) string { return arithChain("int64", rep(" / 2", n)) },
	"masks": func(n int) string { return arithChain("int64", rep(" & 255 + 1", n)) },
	"index-sums": func(n int) string {
		return "package main\n\nfunc f(i int64, s string) uint8 {\n    if i < 0 || i >= len(s) - 5 {\n        return 0\n    }\n" +
			"    return s[i" + rep(" + 0", n) + "]\n}\n\nfunc main() {\n" + checkError + "}\n"
	},
	// An unsigned subtraction chain, whose every link Go's checker reads
	// down to its bottom, the relations in force asked about each step,
	// taking time cubic in its length, so it is subChainLength long.
	"uint-subs": func(int) string { return arithChain("uint64", rep(" - 1", subChainLength)) },
	// Statements stepping a variable, each giving the next what it
	// stores, exitChainLength long: Go's checker takes time quadratic in
	// them, each step's proof asking every fact about the variable.
	"compounds": func(int) string { return stepChain("    x += 1\n") },
	"incs":      func(int) string { return stepChain("    x++\n") },
	"steps":     func(int) string { return stepChain("    x *= 2\n") },
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

// subChainLength is how long TestCheckDeep's unsigned subtraction chain
// is: Go's checker takes time cubic in it, and checks four hundred links
// in a second.
const subChainLength = 400

// arithChain returns a program whose f takes a, of type typ, and gives
// a followed by links, and whose main ends in checkError.
func arithChain(typ, links string) string {
	return "package main\n\nfunc f(a " + typ + ") " + typ + " {\n    x := a" + links +
		"\n    return x\n}\n\nfunc main() {\n" + checkError + "}\n"
}

// stepChain returns a program whose f steps a variable x by step
// exitChainLength times, and whose main ends in checkError.
func stepChain(step string) string {
	return "package main\n\nfunc f() int64 {\n    mut x int64 = 0\n" + rep(step, exitChainLength) +
		"    return x\n}\n\nfunc main() {\n" + checkError + "}\n"
}

// ownDecls are the declarations ownNests and ownChains use: mkl, which
// gives a new list, and S, a struct owning one.
const ownDecls = "func mkl() list[int64] {\n    return make(list[int64], c_allocator)\n}\n\n" +
	"type S struct {\n    items list[int64]\n}\n\n"

// ownProgram returns a program declaring decls and ownDecls, then h
// holding body, which reports nothing, so that the move check and the
// escape check follow it, and main holding body too, ending in
// checkError.
func ownProgram(decls, body string) string {
	return "package main\n\n" + ownDecls + decls + "func h() {\n" + body + "}\n\n" +
		"func main() {\n" + body + checkError + "}\n"
}

// ownNests are the bodies TestCheckDeep nests n levels deep in a
// function that reports nothing, of every statement that holds a block,
// each around a move of an owned local, whose moves the move check
// follows through every level and a loop's body twice, and regions,
// whose escapes the escape check walks.
var ownNests = map[string]func(n int) string{
	"own-ifs":      func(n int) string { return ownNest("if true {\n", "}\n", n) },
	"own-elses":    func(n int) string { return ownNest("if true {\n} else {\n", "}\n", n) },
	"own-fors":     func(n int) string { return ownNest("for true {\n", "}\n", n) },
	"own-ranges":   func(n int) string { return ownNest("for range 3 {\n", "}\n", n) },
	"own-lists":    func(n int) string { return ownNest("for _, e := range l {\n", "}\n", n) },
	"own-switches": func(n int) string { return ownNest("switch {\ncase true:\n", "}\n", n) },
	"own-defers":   func(n int) string { return ownNest("defer {\n", "}\n", n) },
	"own-regions": func(n int) string {
		return ownProgram("", "    mut l := mkl()\n"+rep("region r {\n", n)+
			"q := make(list[int64], r)\nif len(q) == 0 {\n}\nl = mkl()\n"+rep("}\n", n)+"    if len(l) == 0 {\n    }\n")
	},
}

// ownNest returns a program whose h and main hold a list l and n
// statements opened by open and closed by close around a move of a new
// list into a local and of that local into another.
func ownNest(open, close string, n int) string {
	return ownProgram("", "    l := mkl()\n"+rep(open, n)+"z := mkl()\ny := z\nif len(y) == len(l) {\n}\n"+
		rep(close, n)+"    if len(l) == 0 {\n    }\n")
}

// ownBranchLength is how long TestCheckDeep's else if chain moving a
// local in every branch is: Go's move check joins what every branch
// leaves, taking time that grows faster than the square of the chain's
// length, and checks this many in seconds.
const ownBranchLength = 2000

// ownChains are the programs TestCheckDeep writes n links long of
// owned values, which the move check must follow without a frame a
// link: locals each moved into the next, an else if chain moving a local
// in every branch, ownBranchLength long, and a field chain of structs
// each holding the next by value, the last a list, read, assigned and
// passed as mut.
var ownChains = map[string]func(n int) string{
	"own-moves": func(n int) string {
		var b strings.Builder
		b.WriteString("    a0 := mkl()\n")
		for i := range n {
			fmt.Fprintf(&b, "    a%d := a%d\n", i+1, i)
		}
		fmt.Fprintf(&b, "    if len(a%d) == 0 {\n    }\n", n)
		return ownProgram("", b.String())
	},
	"own-elseifs": func(n int) string {
		return ownProgram("", "    mut l := mkl()\n    x := 1\n    if x == 0 {\n        z := l\n        if len(z) == 0 {\n        }\n    }"+
			rep(" else if x == 1 {\n        z := l\n        if len(z) == 0 {\n        }\n    }", n)+"\n    l = mkl()\n    if len(l) == 0 {\n    }\n")
	},
	"own-fields": func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "type F%d struct {\n    f F%d\n}\n", i, i+1)
		}
		fmt.Fprintf(&b, "type F%d struct {\n    items list[int64]\n}\n\nfunc grow(mut l list[int64]) {\n}\n\n", n)
		path := "s" + rep(".f", n) + ".items"
		return ownProgram(b.String(), "    mut s F0\n    k := len("+path+")\n    if k == 0 {\n    }\n    "+
			path+" = mkl()\n    grow(mut "+path+")\n")
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

// exitChainLength is how long TestCheckDeep's else if chain of early
// exits is: Go's checker takes time quadratic in it, and checks two
// thousand links in seconds.
const exitChainLength = 2000

// methodChainLength is how long TestCheckDeep's chains of method calls
// are: Go's checker takes time quadratic in such a chain's length, and
// ten thousand links, which no checker that recurses for each link could
// check within deepStack KB, it checks in seconds.
const methodChainLength = 10000

// callProgram returns a program declaring decls, then main with body,
// which ends in checkError.
func callProgram(decls, body string) string {
	return "package main\n\n" + decls + "func main() {\n" + body + checkError + "}\n"
}

// The declarations the call shapes of TestCheckDeep call: f, which takes
// and gives an int64, g, a uint8, v, any number of int64s, and T, whose
// method m gives a T, whose method w takes it as mut, whose method a
// takes another T, and whose method mq gives a T and an error.
const (
	callF = "func f(x int64) int64 {\n    return x\n}\n\n"
	callG = "func g(x uint8) uint8 {\n    return x\n}\n\n"
	callV = "func v(xs ...int64) {\n}\n\n"
	callT = "type T struct {\n    n int64\n}\n\nfunc (t T) m() T {\n    return t\n}\n\n" +
		"func (mut t T) w() T {\n    return t\n}\n\nfunc (t T) a(u T) T {\n    return u\n}\n\n" +
		"func (t T) mq() (T, error) {\n    return t, noerror\n}\n\n"
)

// checkCalls are the programs TestCheckDeep writes of calls: calls nested
// in each other's argument n deep, the parser's nesting limit, as values,
// as a statement, and with an invalid or an untyped constant at the
// bottom, and method calls, calls of a method expression and struct
// literals nested so; chains of method calls each followed by a ?; method chains methodChainLength long, as a value, as a
// statement, on a name bound to nothing, with a missing method at the
// bottom, and, shorter, since each link's error quotes the chain below
// it, on a receiver passed as mut; an argument list chainLength long, of
// values and of values of the wrong type, each on a line of its own so
// that every error quotes one line; and a field chain chainLength long
// passed as mut and assigned. Most declare a function h holding the
// shape too, which reports nothing, so that what Go's checkFunc does
// after a body with no error, following its moves, follows the shape.
var checkCalls = map[string]func(n int) string{
	"calls": func(n int) string {
		return callProgram(callF+"func h() int64 {\n    return "+rep("f(", n)+"1"+rep(")", n)+"\n}\n\n", "    x := "+rep("f(", n)+"1"+rep(")", n)+"\n    if x == 1 {\n    }\n")
	},
	"call-stmts": func(n int) string { return callProgram(callF, "    "+rep("f(", n)+"1"+rep(")", n)+"\n") },
	"call-invalids": func(n int) string {
		return callProgram(callF, "    x := "+rep("f(", n)+"\"s\""+rep(")", n)+"\n    if x == 1 {\n    }\n")
	},
	"call-untypeds": func(n int) string {
		return callProgram(callG, "    x := "+rep("g(", n)+"300"+rep(")", n)+"\n    if x == 1 {\n    }\n")
	},
	"methods": func(int) string {
		return callProgram(callT+"func h(t T) T {\n    return t"+rep(".m()", methodChainLength)+"\n}\n\n", "    t := T{n: 1}\n    x := t"+rep(".m()", methodChainLength)+"\n    if x.n == 1 {\n    }\n")
	},
	"method-stmts": func(int) string {
		return callProgram(callT, "    t := T{n: 1}\n    t"+rep(".m()", methodChainLength)+"\n")
	},
	"method-invalids": func(int) string {
		return callProgram(callT, "    x := nope"+rep(".m()", methodChainLength)+"\n    if x.n == 1 {\n    }\n")
	},
	"method-missing": func(int) string {
		return callProgram(callT, "    t := T{n: 1}\n    x := t.nope()"+rep(".m()", methodChainLength)+"\n    if x.n == 1 {\n    }\n")
	},
	"question-chains": func(int) string {
		chain := rep(".mq()?", methodChainLength)
		return callProgram(callT+"func h(t T) (T, error) {\n    x := t"+chain+"\n    return x, noerror\n}\n\n"+
			"func h2() (T, error) {\n    x := nope"+chain+"\n    return x, noerror\n}\n\n", "")
	},
	"method-muts": func(int) string {
		return callProgram(callT, "    mut t := T{n: 1}\n    x := mut t"+rep(".w()", 500)+"\n    if x.n == 1 {\n    }\n")
	},
	"method-nests": func(n int) string {
		return callProgram(callT+"func h(t T) T {\n    return "+rep("t.a(", n)+"t"+rep(")", n)+"\n}\n\n",
			"    t := T{n: 1}\n    x := "+rep("t.a(", n)+"t"+rep(")", n)+"\n    if x.n == 1 {\n    }\n")
	},
	"method-expr-nests": func(n int) string {
		return callProgram(callT+"func h(t T) T {\n    return "+rep("T.a(t, ", n)+"t"+rep(")", n)+"\n}\n\n",
			"    t := T{n: 1}\n    x := "+rep("T.a(t, ", n)+"t"+rep(")", n)+"\n    if x.n == 1 {\n    }\n")
	},
	"literal-nests": func(n int) string {
		lits := rep("g(T{n: ", n/2) + "1" + rep("})", n/2)
		return callProgram(callT+"func g(t T) int64 {\n    return t.n\n}\n\nfunc h() int64 {\n    return "+lits+"\n}\n\n",
			"    x := "+lits+"\n    if x == 1 {\n    }\n")
	},
	"args": func(int) string {
		return callProgram(callV+"func h() {\n    v("+rep("1,\n", chainLength)+"1)\n}\n\n",
			"    v("+rep("1,\n", chainLength)+"1)\n")
	},
	"arg-invalids": func(int) string {
		return callProgram(callV, "    v("+rep("\"s\",\n", chainLength)+"\"s\")\n")
	},
	"mut-fields": func(int) string {
		return fieldChain() + "func p(mut x int64) {\n}\n\nfunc h() {\n    mut s S0\n    p(mut s" +
			rep(".f", chainLength) + ".v)\n}\n\nfunc main() {\n    mut s S0\n    p(mut s" +
			rep(".f", chainLength) + ".v)\n" + checkError + "}\n"
	},
	"assign-fields": func(int) string {
		return fieldChain() + "func h() {\n    mut s S0\n    s" + rep(".f", chainLength) + ".v = 1\n}\n\n" +
			"func main() {\n    mut s S0\n    s" + rep(".f", chainLength) + ".v = 1\n" +
			checkError + "}\n"
	},
}

// fieldChain returns the declarations of structs chainLength long, each
// holding the next by value, the last an int64 v.
func fieldChain() string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := range chainLength {
		fmt.Fprintf(&b, "type S%d struct {\n    f S%d\n}\n", i, i+1)
	}
	fmt.Fprintf(&b, "type S%d struct {\n    v int64\n}\n\n", chainLength)
	return b.String()
}

// printfStd is a std of one package, fmt, whose printf, which the type
// checker checks the format of, TestCheckDeep's printf shapes call: the
// repository's std/fmt has bodies the port's checker does not check yet.
const printfStd = "package fmt\n\npub func printf(format string, args ...any) {\n}\n"

// checkPrintfs are the programs TestCheckDeep calls printf in with a
// format of chainLength verbs and as many values, each on a line of its
// own, which take them, or which do not.
var checkPrintfs = map[string]string{
	"printf":      "%d",
	"printf-bads": "%s",
}

// TestCheckDeep runs each of checkNests as deeply as the parser lets a
// body nest, and each of checkChains chainLength links long, through both
// compilers with --check --max-errors 0, compared as check-core compares
// its cases, the port's stack held to deepStack KB: a port whose body
// check recurses with a chain's length, or past its stack at the nesting
// limit, fails here, as does one that declines any of them once
// check-core is required. Each of checkBadChains, ownNests and ownChains
// but the field chain is run with --port-types too, compared as TestTypes
// compares a case of check-core. It runs only as TestPort does.
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
	for name, call := range checkCalls {
		shapes[name] = call(deepLimit - 4)
	}
	bad := make(map[string]bool)
	for name, nest := range ownNests {
		shapes[name] = nest(deepLimit - 4)
		bad[name] = true
	}
	for name, chain := range ownChains {
		n := chainLength
		if name == "own-elseifs" {
			n = ownBranchLength
		}
		shapes[name] = chain(n)
		// The dump of a table of a hundred thousand structs, each with
		// every query the table answers, is TestTypesDeep's to time.
		bad[name] = name != "own-fields"
	}
	for name, body := range checkBadChains {
		shapes[name] = inMain(body + "\n" + checkError)
		bad[name] = true
	}
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, "src", "std", "fmt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "src", "std", "fmt", "fmt.tck"), []byte(printfStd), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, verb := range checkPrintfs {
		file := name + ".tck"
		src := "package main\n\nimport fmt from \"std/fmt\"\n\nfunc main() {\n    fmt.printf(\"" +
			rep(verb, chainLength) + "\",\n" + rep("1,\n", chainLength-1) + "1)\n" + checkError + "}\n"
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			in := input{name: file, dir: dir, file: file, env: []string{"TACK_HOME=" + home}}
			runCaseWith(t, *st, in, env.portRun)
		})
	}
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

// nestRoute is a construct that nests an expression in another,
// TestCheckDeepPairs's: how it wraps the expression it holds, a %s, and
// how many of the parser's nesting levels it takes, 0 for an operator
// whose right or left operand it is, which the parser counts no level
// for, so only nested in another route.
type nestRoute struct {
	wrap  string
	level int
}

// nestFamilies are the routes TestCheckDeepPairs nests, by the type of
// the value each gives and holds: each family's routes wrap a value of
// one type, bool or int64, and give one of it. Together they are every
// construct the parser lets an expression nest in that the port's
// checker checks with no later stage: a call's argument, a method call's
// argument, a method call's receiver, a call of a method expression,
// the left and right operands of a comparison and of && and ||, !, a
// parenthesized expression, an element of an array literal read back, a
// field of a struct literal read back, a union's payload, a ?, a value
// passed as any, a conversion, a narrowing one, arithmetic whose
// overflow the bounds of an & prove, + - * and unary -, a wrapping +,
// a division, shifts, a slice's bound, a function whose body tests the value
// in an if and a switch, the right side of an && and of an || whose left
// side gives it a fact, and an element of an array literal whose index
// the facts of the && around it prove in range; and, of values that own
// a list, a call and a method call that take one as own, a call taking
// another variable as own beside it, a struct literal's field, and
// replace, whose mut place is a variable of its own each time, @ in the
// route, so that what the move check follows is the nest and not a use
// after a move. swap and drop give no value, and so nest in nothing.
// Each family's bad is a value of the wrong type, and untyped an untyped
// constant of the family's type, none for the owned family.
var nestFamilies = []struct {
	decls, leaf, bad, untyped string
	routes                    map[string]nestRoute
	owned                     bool
}{
	{
		decls: "type T struct {\n    n int64\n}\n\nfunc (t T) c(x bool) bool {\n    return x\n}\n\n" +
			"type W struct {\n    v bool\n}\n\nfunc (w W) get() bool {\n    return w.v\n}\n\n" +
			"type B struct {\n    v bool\n}\n\ntype U union {\n    a bool\n    b\n}\n\n" +
			"func bb(x bool) bool {\n    return x\n}\n\nfunc wb(x bool) W {\n    return W{v: x}\n}\n\n" +
			"func ub(u U) bool {\n    return u == U.b\n}\n\n" +
			"func qb(x bool) (bool, error) {\n    return x, noerror\n}\n\nfunc ab(x any) bool {\n    return true\n}\n\n" +
			"func cb(x bool) bool {\n    if x {\n        return true\n    }\n    switch x {\n    case true:\n    }\n    return false\n}\n\n",
		leaf: "true", bad: "\"s\"", untyped: "1 < 2",
		routes: map[string]nestRoute{
			"call": {"bb(%s)", 1}, "method-arg": {"t.c(%s)", 1}, "receiver": {"wb(%s).get()", 1},
			"method-expr": {"T.c(t, %s)", 1}, "left": {"%s == true", 0}, "right": {"true == %s", 0},
			"and": {"%s && true", 0}, "or": {"false || %s", 0}, "not": {"!%s", 1}, "paren": {"(%s)", 1},
			"element": {"[1]bool{%s}[0]", 1}, "field": {"B{v: %s}.v", 1}, "payload": {"ub(U.a(%s))", 2},
			"question": {"qb(%s)?", 1}, "any": {"ab(%s)", 1}, "cond": {"cb(%s)", 1},
			"fact-and": {"(t.n > 0 && %s)", 1}, "fact-or": {"(t.n == 0 || %s)", 1},
			"guarded": {"(t.n >= 0 && t.n < 2 && [2]bool{%s, true}[t.n])", 2},
		},
	},
	{
		decls: "type T struct {\n    n int64\n}\n\ntype L struct {\n    items list[int64]\n}\n\n" +
			"func mkl() list[int64] {\n    return make(list[int64], c_allocator)\n}\n\n" +
			"func idl(own l list[int64]) list[int64] {\n    return l\n}\n\n" +
			"func ido(own a list[int64], own b list[int64]) list[int64] {\n    return b\n}\n\n" +
			"func li(own x L) list[int64] {\n    mut y := x\n    return replace(mut y.items, mkl())\n}\n\n" +
			"func (t T) ml(own l list[int64]) list[int64] {\n    return l\n}\n\n",
		leaf: "mkl()", bad: "\"s\"",
		routes: map[string]nestRoute{
			"own-call": {"idl(%s)", 1}, "own-arg": {"ido(own @, %s)", 1}, "owned-lit": {"li(L{items: %s})", 2},
			"replace": {"replace(mut @, %s)", 1}, "own-method": {"t.ml(%s)", 1},
		},
		owned: true,
	},
	{
		decls: "type T struct {\n    n int64\n}\n\nfunc (t T) c(x int64) int64 {\n    return x\n}\n\n" +
			"type B struct {\n    v int64\n}\n\nfunc fi(x int64) int64 {\n    return x\n}\n\n",
		leaf: "1", bad: "\"s\"", untyped: "2 * 3",
		routes: map[string]nestRoute{
			"int-call": {"fi(%s)", 1}, "int-method-arg": {"t.c(%s)", 1}, "conversion": {"int64(%s)", 1},
			"int-element": {"[1]int64{%s}[0]", 1}, "int-field": {"B{v: %s}.v", 1},
			"plus": {"((%s) & 255) + 1", 2}, "minus": {"((%s) & 255) - 255", 2},
			"times": {"((%s) & 255) * 2", 2}, "neg": {"-((%s) & 255)", 3}, "wrap": {"%s +%% t.n", 0}, "div": {"(%s / 2)", 1},
			"shr": {"(%s >> 1)", 1}, "shl": {"(%s << 0)", 1}, "narrow": {"int64(uint8(%s & 255))", 2},
			"slice": {"len(\"abcdef\"[%s:])", 2},
		},
	},
}

// reportingRoutes are the routes of nestFamilies whose every nest Go's
// checker reports, whatever it holds, so no pair holding one is a
// program Go accepts: a narrowing conversion and a slice's bound, which
// only a subject, a length or arithmetic on them is proven to fit, never
// the value of another route.
var reportingRoutes = map[string]bool{"narrow": true, "slice": true}

// shallowPairs are the ordered pairs of routes TestCheckDeepPairs nests
// only a quarter as deeply as the parser allows: an || whose facts are
// asked true, nested in an && and holding one, which Go's checker works
// out again at every level, taking time cubic in the depth, some thirty
// seconds a run at the limit. Each of the three routes is nested at the
// limit in every other pair it is part of.
var shallowPairs = map[[2]string]bool{
	{"and", "fact-or"}: true, {"fact-or", "and"}: true,
	{"fact-and", "fact-or"}: true, {"fact-or", "fact-and"}: true,
}

// pairNest returns the expression that nests routes a and b, alternating,
// a first from the outside, around leaf, as many times as limit, the
// parser's nesting limit less a margin for what holds them, allows.
func pairNest(a, b nestRoute, leaf string, limit int) string {
	e, levels := leaf, 0
	for i := 0; ; i++ {
		r := a
		if i%2 == 1 {
			r = b
		}
		if levels+r.level > limit || i > 4*deepLimit {
			break
		}
		levels += r.level
		e = fmt.Sprintf(r.wrap, e)
	}
	return e
}

// ownedVars returns nest with each @ in it replaced by a variable of its
// own, v0, v1 and on, and the declarations of those variables, each a
// mutable list, one a line.
func ownedVars(nest string) (string, string) {
	var b, decls strings.Builder
	n := 0
	for _, r := range nest {
		if r != '@' {
			b.WriteRune(r)
			continue
		}
		fmt.Fprintf(&b, "v%d", n)
		fmt.Fprintf(&decls, "    mut v%d := mkl()\n", n)
		n++
	}
	return b.String(), decls.String()
}

// TestCheckDeepPairs nests every ordered pair of the routes of each of
// nestFamilies, alternating, as deeply as the parser allows: a valid
// nest, one with a value of the wrong type at its bottom and one with an
// untyped constant there, each in a function h of a program whose main
// ends in checkError, run with --check --max-errors 0 and compared as
// check-core compares its cases, and with --port-types, compared as
// TestTypes compares a case of the type checker's, and, unless a route
// of the pair is one of reportingRoutes, a program Go accepts with the
// valid nest, compared as TestTypes compares an input
// of the corpus, in the phase "bodies". The port runs with its stack
// held to deepStack KB, as TestPort does, and each --check nest again at
// pairStack KB, so every level of every route must cost it less than
// pairStack KB over the parser's nesting limit, counting every frame the
// level takes. Two routes that
// take no nesting level, operators of each other's operand, are no pair:
// they chain without end, which TestCheckDeep checks. It runs only as
// TestPort does.
func TestCheckDeepPairs(t *testing.T) {
	ready(t)
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	dir := t.TempDir()
	for _, fam := range nestFamilies {
		for an, a := range fam.routes {
			for bn, b := range fam.routes {
				if a.level == 0 && b.level == 0 {
					continue
				}
				for _, variant := range []string{"valid", "invalid", "untyped", "clean"} {
					if variant == "untyped" && fam.untyped == "" {
						continue
					}
					if variant == "clean" && (reportingRoutes[an] || reportingRoutes[bn]) {
						continue
					}
					leaf := fam.leaf
					switch variant {
					case "invalid":
						leaf = fam.bad
					case "untyped":
						leaf = fam.untyped
					}
					ret := "bool"
					if fam.leaf == "1" {
						ret = "int64"
					}
					limit := deepLimit - 6
					if shallowPairs[[2]string{an, bn}] {
						limit /= 4
					}
					nest, vars := ownedVars(pairNest(a, b, leaf, limit))
					if fam.owned {
						ret = "list[int64]"
					}
					h := "func h(t T) (" + ret + ", error) {\n" + vars + "    x := " + nest +
						"\n    return x, noerror\n}\n\n"
					if fam.owned {
						// A list bound with an error could be read only once
						// the facts in force prove the error checked.
						h = "func h(t T) list[int64] {\n" + vars + "    x := " + nest + "\n    return x\n}\n\n"
					}
					main := "func main() {\n    " + checkError + "}\n"
					switch {
					case variant == "clean" && fam.owned:
						main = "func main() {\n    l := h(T{n: 1})\n    if len(l) == 0 {\n        return\n    }\n}\n"
					case variant == "clean":
						main = "func main() {\n    _, err := h(T{n: 1})\n    if err.code != 0 {\n        return\n    }\n}\n"
					}
					src := "package main\n\n" + fam.decls + h + main
					file := an + "-" + bn + "-" + variant + ".tck"
					t.Run(file, func(t *testing.T) {
						t.Parallel()
						if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
							t.Fatal(err)
						}
						in := input{name: file, dir: dir, file: file}
						if variant != "clean" {
							runCaseWith(t, *st, in, env.portRun)
							runCaseWith(t, *st, in, env.portPairs)
							checkTypes(t, in, env.portRun, false)
							return
						}
						compared, _, phase := checkTypes(t, in, env.portRun, true)
						if !compared || phase != "bodies" {
							t.Errorf("not compared in the phase bodies: compared %v, phase %q", compared, phase)
						}
					})
				}
			}
		}
	}
}

// stmtRoutes are the statements TestCheckDeepStmts nests, each around
// the statements it holds, a %s, each taking one of the parser's nesting
// levels: every statement that holds a block, an if, an else, an else
// if, a loop with a condition, a range over a count and over a list, a
// tagless switch, a switch on a value, on a union, binding its payload,
// with its other case and with a default, a type switch, a defer and a
// region. A # in one is a number of its own each time it is written, so
// that no name is declared twice. There are no labels and no bare
// blocks; a defer in a defer is reported, so a nest holding two defers
// is no program Go accepts.
var stmtRoutes = map[string]string{
	"if":            "if b {\n%s}\n",
	"else":          "if b {\n} else {\n%s}\n",
	"else-if":       "if b {\n} else if b {\n%s}\n",
	"for":           "for b {\n%s}\n",
	"range":         "for range 3 {\n%s}\n",
	"list-range":    "for _, e# := range l {\nif e# == 0 {\n}\n%s}\n",
	"tagless":       "switch {\ncase b:\n%s}\n",
	"value-switch":  "switch n {\ncase 1:\n%s}\n",
	"union-bind":    "switch u {\ncase a(p#):\nif p# {\n}\n%scase b:\n}\n",
	"union-default": "switch u {\ncase a(p#):\nif p# {\n}\ndefault:\n%s}\n",
	"type-switch":   "switch t# := y.(type) {\ncase int64:\nif t# == 0 {\n}\n%s}\n",
	"defer":         "defer {\n%s}\n",
	"region":        "region r# {\n%s}\n",
}

// stmtNest nests the routes a and b, alternating, a first from the
// outside, levels deep around inner, numbering each # in them.
func stmtNest(a, b string, levels int, inner string) string {
	var wraps []string
	for i := range levels {
		if i%2 == 0 {
			wraps = append(wraps, a)
		} else {
			wraps = append(wraps, b)
		}
	}
	e := inner
	for i := len(wraps) - 1; i >= 0; i-- {
		e = strings.ReplaceAll(fmt.Sprintf(wraps[i], e), "#", strconv.Itoa(i))
	}
	return e
}

// stmtProgram returns a program declaring what the bool routes of
// nestFamilies call, U among them, whose h holds body, given a T, a
// bool, an int64, a U, an any and a list, returning an error, so that a
// ? may be written in it, and whose main ends in checkError, or, when
// clean, calls h and reports nothing.
func stmtProgram(body string, clean bool) string {
	main := "func main() {\n    " + checkError + "}\n"
	if clean {
		main = "func main() {\n    l := make(list[int64], c_allocator)\n    e := h(T{n: 1}, true, 1, U.b, 1, l)\n" +
			"    if e.code != 0 {\n        return\n    }\n}\n"
	}
	return "package main\n\n" + nestFamilies[0].decls +
		"func h(t T, b bool, n int64, u U, y any, l list[int64]) error {\n" + body + "    return noerror\n}\n\n" + main
}

// TestCheckDeepStmts nests every ordered pair of stmtRoutes,
// alternating, as deeply as the parser allows around a local's
// declaration and an if on it; and every statement route nested half as
// deeply around a value nested the other half in each bool route of
// nestFamilies that takes a level. Each runs as a valid
// nest, one whose value is of the wrong type, and one in a program Go
// accepts, but for a nest holding two defers: the first two under
// --check, compared as check-core compares its cases, and --port-types,
// compared as TestTypes compares a case of check-core, and the last
// under --port-types, compared in the phase "bodies", each with the
// port's stack held to deepStack KB and again to pairStack KB. It runs
// only as TestPort does.
func TestCheckDeepStmts(t *testing.T) {
	ready(t)
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	dir := t.TempDir()
	type shape struct {
		name, body, leaf string
		defers           int
	}
	var shapes []shape
	for an, a := range stmtRoutes {
		for bn, b := range stmtRoutes {
			d := 0
			if an == "defer" || bn == "defer" {
				// Alternating, the nest holds a defer every other level.
				d = 2
			}
			shapes = append(shapes, shape{an + "-" + bn, stmtNest(a, b, deepLimit-6, "x := @\nif x == 1 {\n}\n"), "1", d})
		}
	}
	bools := nestFamilies[0].routes
	half := (deepLimit - 6) / 2
	for sn, s := range stmtRoutes {
		for en, e := range bools {
			if e.level == 0 {
				continue
			}
			d := 0
			if sn == "defer" {
				d = 2
			}
			value := pairNest(e, e, "@", half)
			shapes = append(shapes, shape{sn + "-" + en, stmtNest(s, s, half, "x := "+value+"\nif x {\n}\n"), "true", d})
		}
	}
	for _, sh := range shapes {
		for _, variant := range []string{"valid", "invalid", "clean"} {
			if variant == "clean" && sh.defers > 1 {
				continue
			}
			leaf := sh.leaf
			if variant == "invalid" {
				leaf = "\"s\""
			}
			src := stmtProgram(strings.ReplaceAll(sh.body, "@", leaf), variant == "clean")
			file := sh.name + "-" + variant + ".tck"
			t.Run(file, func(t *testing.T) {
				t.Parallel()
				if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				in := input{name: file, dir: dir, file: file}
				for _, port := range []string{env.portRun, env.portPairs} {
					if variant == "clean" {
						compared, _, phase := checkTypes(t, in, port, true)
						if !compared || phase != "bodies" {
							t.Errorf("not compared in the phase bodies: compared %v, phase %q", compared, phase)
						}
						continue
					}
					runCaseWith(t, *st, in, port)
					checkTypes(t, in, port, false)
				}
			})
		}
	}
}

// smallStack is the stack, in KB, TestCheckSliceChains holds the port to
// besides deepStack: a quarter of it, which a check taking a frame of
// even two hundred bytes a link of sliceChainLength links runs out of.
const smallStack = 1024

// sliceChainLength is how long TestCheckSliceChains's chains are: Go's
// checker takes time quadratic in each, and checks five thousand links
// in a second.
const sliceChainLength = 5000

// sliceChains are the programs TestCheckSliceChains writes n links long:
// a spine of slices with no high bound under len, whose length is the
// length of what each slices less its low bound, down the whole spine,
// and one indexed;
// and locals each slicing the one before, from a parameter, and from a
// string literal, whose value is known, down the whole chain.
var sliceChains = map[string]func(n int) string{
	"slice-spine": func(n int) string {
		return "package main\n\nfunc f(s string) int64 {\n    if len(s) < 10 {\n        return 0\n    }\n" +
			"    return len(s" + rep("[0:]", n) + ")\n}\n\nfunc main() {\n" + checkError + "}\n"
	},
	"index-spine": func(n int) string {
		return "package main\n\nfunc f(s string) uint8 {\n    if len(s) < 3 {\n        return 0\n    }\n" +
			"    return s" + rep("[0:]", n) + "[1]\n}\n\nfunc main() {\n" + checkError + "}\n"
	},
	"slice-locals": func(n int) string { return localSlices("s string", "s[0:]", n) },
	"slice-literal-locals": func(n int) string {
		return localSlices("", "\"abcdef\"[0:]", n)
	},
}

// localSlices returns a program whose f, taking params, declares t0 as
// first and each local after it slicing the one before, n of them, and
// gives the length of a slice of the last, and whose main ends in
// checkError.
func localSlices(params, first string, n int) string {
	var b strings.Builder
	b.WriteString("package main\n\nfunc f(" + params + ") int64 {\n    t0 := " + first + "\n")
	for i := range n {
		fmt.Fprintf(&b, "    t%d := t%d[0:]\n", i+1, i)
	}
	fmt.Fprintf(&b, "    return len(t%d[1:])\n}\n\nfunc main() {\n%s}\n", n, checkError)
	return b.String()
}

// TestCheckSliceChains runs each of sliceChains sliceChainLength links
// long through both compilers with --check --max-errors 0, compared as
// check-core compares its cases, the port's stack held to deepStack KB
// and again to smallStack KB: a port that works out a slice's length or
// a known string with a frame a link fails here. It runs only as
// TestPort does.
func TestCheckSliceChains(t *testing.T) {
	ready(t)
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	dir := t.TempDir()
	for name, chain := range sliceChains {
		file := name + ".tck"
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			if err := os.WriteFile(filepath.Join(dir, file), []byte(chain(sliceChainLength)), 0o600); err != nil {
				t.Fatal(err)
			}
			in := input{name: file, dir: dir, file: file}
			runCaseWith(t, *st, in, env.portRun)
			runCaseWith(t, *st, in, env.portSmall)
		})
	}
}

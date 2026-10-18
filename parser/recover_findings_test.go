package parser_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// findingCases are the inputs on which recovery once went wrong, each
// reduced to its smallest form: a line marked //E must get exactly one
// error and every other line none, and every name in keep must still be
// declared in the tree.
var findingCases = []struct {
	name, src string
	keep      []string
}{
	{"an expression cut off at a line's end keeps the type after it", `package main

var a = 1 +
type U struct { //E
    x int64
}

func f() {
}
`, []string{"U", "f"}},
	{"a call cut off at a line's end keeps the vars after it", `package main

var a = f(
var b = 1 //E
var c = 2
`, []string{"b", "c"}},
	{"a missing value keeps the var after it and the error after that", `package main

var a =
var b = 1 //E
c = 2     //E
var d = 3
`, []string{"b", "d"}},
	{"an unclosed brace in a var keeps the vars after it", `package main

var a = (1 > { ) //E
var b = 2
mut c = 3

func f() {
}
`, []string{"b", "c", "f"}},
	{"a mut method line does not end a damaged interface", `package main

type I interface {
    count() int64 int64 //E
    mut put(w string) error
}

mut g = 1
`, []string{"g"}},
	{"a damaged import still names its package", `package main

import geo "shapes/geo" //E

func main() {
    r := geo.Rect{min: 1}
    s := geo.Rect{min: 2}
}
`, []string{"main"}},
	{"a lookahead stays in its own statement", `package main

func check(cond bool, name string) {
    print [ (name) //E
    if cond {
        print("a")
    }
}

func g() {
    for  ]  := range 3 { //E
    }
}
`, []string{"check", "g"}},
	{"an extern's damaged line keeps the vars after it", `package main

extern("c_live") func live() } else { //E

mut seed uint64 = 1

mut flips int64 = 0

func check() {
}
`, []string{"seed", "flips", "check"}},
	{"an extern written with a body skips the body whole", `package main

extern("c_live") func live() int64 { //E
    mut x := 1
    var y = 2
    return x
}

mut flips int64 = 0
`, []string{"flips"}},
	{"a var with func for its keyword keeps the vars after it", `package main

func counts [lexer.MaxTokens]int64 //E

var corner = geom.Corners[1].x
var far = 2

func size() int64 {
    return 8
}
`, []string{"corner", "far", "size"}},
	{"a func line holding a composite literal is a statement", `package main

func main() {
    a := 1
     func (a == [4]int64{4, 3, 2, 1}, "x") //E
    mut s := 2
    mut b := 3
}

func g() {
}
`, []string{"main", "g"}},
	{"a func line that declares a method still ends an unclosed body", `package main

type P struct {
    x int64
}

func a() {
    x := 1
func (p P) m() int64 { //E
    return p.x
}

func g() {
}
`, []string{"P", "m", "g"}},
	{"a switch closed early reports only the brace", `package main

func f(a int64) {
    switch a {
    case 1:
        if a > 0 //E
            print("x")
        }
    case 2:
        print("b")
    default:
        print("c")
    }
}

func g() {
}
`, []string{"f", "g"}},
	{"a case outside any switch is still reported", `package main

func f() {
    case 1: //E
    print("x")
}
`, []string{"f"}},
	{"a for header split over two lines is one error", `package main

func f() {
    mut steps := 0
    for mut a, mut b //E
 := divmod(100, 1); a > 1; a, b = divmod(a, 2) {
        steps += 1 + b
    }
    x := ) //E
}
`, []string{"f"}},
	{"a closing brace in the middle of a line is part of the damage", `package main

func main() {
    check(a == 5 }  "x") //E
    check(b, "y")
    mut x int64 = 7
    check(x == 7, "z")
}

func g() {
}
`, []string{"main", "g"}},
	{"a body closed too soon spills no errors", `package main

func main() {
    big := 1
    if big != 0 //E
        print("x")
        return
    }
    mut b := 2
    print("y")
    mut part := 3
    var c = 4
}

func g() {
}
`, []string{"main", "g"}},
	{"a function whose body never opens reports only its line", `package main

func main()  } //E
    var size uint64 = 16
    check(size == 16, "size")
    mut n int64 = 4
    halve(mut n)
}

var g = 1

func h() {
}
`, []string{"g", "h"}},
	{"a doubled mut in an interface keeps the interface's lines in it", `package main

type Writer interface {
    mut mut write(b []uint8) error //E
    size() int64
}

func f() {
}
`, []string{"f"}},
	{"a func line that is no function skips no body", `package main

var a = 1
 func var high = 2 //E
var prefix = 3
var both = !(a   a) //E

func check() {
}
`, []string{"a", "prefix", "check"}},
	{"a function with its func left out keeps its statements in it", `package main

  main() { //E
    mut i = 0
    for {
        i = i + 1
    }
    mut sum = 0
    for mut n := 1; n <= 10; n++ {
        sum += n
    }
}

func g() {
}
`, []string{"g"}},
	{"a stray brace before a type keeps the vars after it", `package main

 { type Rect struct { //E
    w int64
}

var origin = Rect{w: 1}
var size int64 = 3
var greeting = } //E

func area(r Rect) int64 {
    return r.w
}
`, []string{"origin", "size", "area"}},
	{"an import with its keyword mistyped still names its package", `package main

 ! geo from "shapes/geo" //E

func main() {
    r := geo.Rect{min: 1}
}
`, []string{"main"}},
	{"a field line shaped like a declaration stays in its struct", `package geo

pub type Point struct {
    pub  type x int64 //E
    pub y int64
}

pub type Rect struct {
    pub min Point
}
`, []string{"Rect"}},
	{"a body after a function left without its brace spills no errors", `package main

func main()
    greeting := "variables" //E
    print(greeting)
    var base int64 = 3
    mut total := base
    if total == 3 {
        print("x")
    }
}

func g() {
}
`, []string{"main", "g"}},
	{"an extra closing brace spills a body without a cascade", `package main

func main() {
    if 1 < 2 {
        exit(7)
    } }
    if !(2 < 3) || 1 > 0 { //E
        exit(8)
    }
    mut e string
    if e != "" {
        exit(9)
    }
    mut x := 1
    var y = 2
}

func g() {
}
`, []string{"main", "g"}},
	{"an extra brace keeps the globals after its function", `package main

func unsigned(ok bool) int64 {
    if !ok {
        return 0
    } }
    cutoff := 3 //E
    return cutoff
}

var saw_start int64 = 0
var saw_digit int64 = 1

func g() {
}
`, []string{"unsigned", "saw_start", "saw_digit", "g"}},
	{"a closing brace for a missing value does not end the body", `package main

func main() {
    var u8 uint8 = 255
    var u16 uint16 = } //E
    var u32 uint32 = 7
    s := "a"
    check(s, "x")
    mut x int64 = 7
    if x > 0 { check(s, "y") }
}

func g() {
}
`, []string{"main", "g"}},
	{"an extra brace before a short declaration spills no errors", `package main

func main() {
    if 1 < 2 {
        exit(3)
    } }

    mut ml := 1 //E
    ml += 7
    mut b := 2
    g = 3
}

func g() {
}
`, []string{"main", "g"}},
	{"a header error with a whole body spills nothing", `package main

func check(cond bool,   string) { //E
    print("x")
}

mut g uint8 = 1

 defer  bump() int64 { //E
    g++
    return 9
}
`, []string{"g"}},
	{"pub before a short declaration does not end the body", `package main

func assigned() bool {
     pub mut l := count(3) //E
    mut i := 0
    v := next(mut i)
    return v
}

func g() {
}
`, []string{"assigned", "g"}},
	{"an error in a body that closes properly spills nothing", `package main

func unsigned(p int64) int64 {
    if p ==  import  { //E
        return 1
    }
    return 0
}

var saw_start int64 = 0

 else  underscores_ok(s []uint8) bool { //E
    return true
}
`, []string{"unsigned", "saw_start"}},
	{"a function with its func mistyped as mut keeps its statements", `package main

 mut  main() { //E
    var b bool = true
    var u8 uint8 = 255
    s := "a"
    check(s, "x")
}

func g() {
}
`, []string{"g"}},
	{"a stray brace after declarations is reported", `package main

func unsigned(p int64) int64 {
    if p ==  pub  { //E
        return 1
    }
    return 0
}

var saw_start int64 = 0

 } func underscores_ok(s []uint8) bool { //E
    return true
}
`, []string{"unsigned", "saw_start"}},
	{"an import that lost its name keeps its literals from errors", `package main

import  (  from "shapes/geo" //E

func main() {
    r := geo.Rect{min: geo.Point{x: 1, y: 2}}
}
`, []string{"main"}},
	{"a mistyped func ends a spilled body", `package main

func unsigned(ub uint64) int64 {
    if ub == 0  pub //E
        return 0
    }
    ubits := 3
    return ubits
}

 *  underscores_ok(s []uint8) bool { //E
    return true
}
`, []string{"unsigned"}},
	{"a spilled body ends at its brace", `package main

func unsigned(ub uint64) int64 {
    if ub == 0  pub //E
        return 0
    }
    ubits := 3
    return ubits
}

var saw_start int64 = 0

 if  underscores_ok(s []uint8) bool { //E
    return true
}
`, []string{"unsigned", "saw_start"}},
	{"a func line naming a call with a composite literal is a statement", `package main

func main() {
    cells := 1
     func check(cells == [4]int64{1, 1, 2, 2}, "x") //E
    mut k int64 = 1
    mut q := Point{x: 7, y: 0}
}

func g() {
}
`, []string{"main", "g"}},
	{"an unclosed index ending a var's line keeps the vars after it", `package main

var last = table[len(table) - 1 { //E
var known = table[1]
var byteT = word[0 0] //E
var widened = int64(small)
`, []string{"known", "widened"}},
	{"an import before a function keeps the function's statements in it", `package main

 import func main() { //E
    mut n int64 = 0
    fill(mut n, 42)
    mut p := Point{x: 2}
}

func g() {
}
`, []string{"g"}},
	{"braces in a damaged header do not hide the declarations after it", `package main

func unsigned } else { s []uint8) (uint64, int32) { //E
    if len(s) <= 0 {
        return 0, 1
    }
    return 1, 0
}

var saw_digit int64 = 1
var saw_other int64   3 //E

func g() {
}
`, []string{"saw_digit", "g"}},
	{"a function whose parameters are cut off keeps its statements in it", `package main

func moving(  { //E
    v, f := lookup(1)
    mut g2 := g
    mut h := take(mut g2)
}

func g() {
}
`, []string{"g"}},
	{"one-line functions still end an unclosed body", `package main

func count(mut n int64, depth int64) {
    if depth <= 0 {
        return
     x :=
    n++ //E
}

func relay(mut n int64) { relay2(mut n) } //E
func relay2(mut n int64) { n = n + 100 }

func next(mut n int64) int64 {
    return n
}
`, []string{"relay", "relay2", "next"}},
	{"an import without its keyword keeps the imports after it in the header", `package main

 bytes from "std/bytes" //E
import fmt from "std/fmt"
import text from "std/text"

func main() {
}
`, []string{"main"}},
	{"a function with its func mistyped as import keeps its statements", `package main

func check() {
}

 import main() { //E
    mut i = 0
    for {
        i = i + 1
    }
    for mut n := 1; n <= 10; n++ {
        i += n
    }
}

func g() {
}
`, []string{"check", "g"}},
	{"a stray line before the imports keeps them in the header", `package main

 ) //E

import fmt from "std/fmt"
import logs from "generics/logs"

func main() {
}
`, []string{"main"}},
	{"a keyword alone on a body line does not end the body", `package main

func main() {
     import //E
    a := "tack"
    if a != "x" {
        exit(1)
    }
    mut b := 2
}

func g() {
}
`, []string{"main", "g"}},
	{"import as a mistyped var in a body does not end the body", `package main

func main() {
     import  b bool = true //E
    var i8 int8 = -128
    s := "a"
    check(s, "x")
}

func g() {
}
`, []string{"main", "g"}},
	{"a semicolon before a body keeps the body's statements in it", `package main

func main()  ; { //E
    mut b int8 = 127
    b++
    var w uint16 = 65535
    sum := w
}

func g() {
}
`, []string{"g"}},
	{"an if mistyped as func is a statement", `package main

func main() {
     func  greeting(true) != "hello" || greeting(false) == "x" { //E
        exit(10)
    }
    mut x := 1
    if x > 0 {
        exit(2)
    }
}

func g() {
}
`, []string{"main", "g"}},
	{"a body brace on the next line keeps the body's statements in it", `package main

func main()
 { //E
    mut n int64 = 0
    fill(mut n, 42)
    mut p := Point{x: 2}
}

func g() {
}
`, []string{"main", "g"}},
	{"braces in a mistyped func keep the declarations after it", `package main

 } else {  check(cond bool, name string) { //E
    if cond {
        print("x")
    } else {
        print("y")
    }
}

mut g uint8 = 1

mut h uint8 = 3

func bump() int64 {
    return 9
}
`, []string{"h", "bump"}},
	{"braces in a damaged type line keep the mut declarations after it", `package main

type  } else {  struct { //E
    x int64
}

mut tagCalls int64
mut valueCalls int64

func f() {
}
`, []string{"tagCalls", "valueCalls", "f"}},
	{"braces a spilled body leaves open keep the declarations after it", `package main

func a0() {
0 //E
}
A}{{
    x := 1
}

type T1 struct {
    x int64
}

var v2 = 3
`, []string{"a0", "T1", "v2"}},
	{"a type line where a switch's case goes does not end the function", `package main

func main() {
    mut hit := 0
    switch {
     type  value(1) == 2: //E
        hit = 7
    default:
        hit = 9
    }
    mut sum := 0
    for i := range 10 {
        switch {
        case i == 2:
            continue
        }
        sum += i
    }
}

func value(n int64) int64 {
    return n
}
`, []string{"main", "value"}},
}

// TestRecoveryFindings checks each of findingCases.
func TestRecoveryFindings(t *testing.T) {
	for _, tc := range findingCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, diags := parse(t, tc.src)
			checkErrorLines(t, tc.src, diags)
			if tree == nil {
				t.Fatal("no tree")
			}
			found := declaredNames(tree)
			for _, name := range tc.keep {
				if !found[name] {
					t.Errorf("%s is not declared in the tree", name)
				}
			}
		})
	}
}

// TestBodiesClosedAfterAStatementParse checks valid programs whose block
// closes on the line of its last statement, which recovery's reading of a
// closing brace with more of its line before it must leave alone.
func TestBodiesClosedAfterAStatementParse(t *testing.T) {
	for _, src := range []string{
		"package lexer\n\npub func F() string { f(\"x\")\nreturn \"y\" }\n",
		"package main\n\nfunc main() {\n    if a {\n        b() }\n    c() }\n",
		"package main\n\nfunc main() {\n    switch x {\n    case 1:\n        y() }\n}\n",
	} {
		if _, diags := parse(t, src); len(diags) > 0 {
			t.Errorf("%q: %v", src, diags)
		}
	}
}

// TestLongDamagedLinesParseInLinearTime checks that one damaged line of
// tens of thousands of tokens, which recovery reads token by token, is
// parsed in time in proportion to its length, with no limit on errors:
// what recovery reads of a line is remembered rather than read again for
// each of its tokens.
func TestLongDamagedLinesParseInLinearTime(t *testing.T) {
	const n = 50000
	for _, line := range []string{
		"A[" + strings.Repeat(";", n),
		"x := " + strings.Repeat("}", n),
		"x := (" + strings.Repeat("} ", n) + ")",
		"A[A" + strings.Repeat("!0000.A00", n/10),
		"A.A.A>A.A." + strings.Repeat(";", n),
		"x := 1 " + strings.Repeat("; y", n) + " {",
	} {
		src := "package main\n\nfunc a0() {\n" + line + "\n}\n\nvar v = 1\n"
		f := &source.File{ID: "long", Filename: "long.tck", Data: []byte(src)}
		lex := lexer.New(f)
		if diags := lex.Lex(); len(diags) > 0 {
			t.Fatal(diags)
		}
		start := time.Now()
		_, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("parsing a line of %d bytes took %v", len(line), elapsed)
		}
		if len(diags) == 0 {
			t.Errorf("no error for %.20q...", line)
		}
	}
}

// TestUnclosedIndexesParseInLinearTime checks that statements each
// opening a bracket they never close, with no limit on errors, take time
// in proportion to their number: a lookahead for the closing bracket stops
// at the end of the statement rather than scanning to the end of the file
// for each one.
func TestUnclosedIndexesParseInLinearTime(t *testing.T) {
	const n = 50000
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n")
	for range n {
		b.WriteString("    x[ = 1\n")
	}
	b.WriteString("}\n")
	f := &source.File{ID: "lin", Filename: "lin.tck", Data: []byte(b.String())}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatal(diags)
	}
	start := time.Now()
	_, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("parsing %d unclosed indexes took %v", n, elapsed)
	}
	if len(diags) != n {
		t.Errorf("got %d errors, want %d", len(diags), n)
	}
}

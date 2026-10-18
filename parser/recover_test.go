package parser_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// recoverCases are files with several independent syntax errors. Each
// marks, with the comment //E on its line, every line one error must be
// reported on, and the parse must report exactly one error on each such
// line and none elsewhere.
var recoverCases = []struct {
	name, src string
}{
	{"two statements in one body", `package main

func main() {
    x := 1 +
    y := 2      //E
    z := * y    //E
    print("ok")
}
`},
	{"a statement in each of two functions", `package main

func f() {
    a := (1     //E
}

func g() {
    b := 2 2    //E
}
`},
	{"errors in nested blocks", `package main

func main() {
    if true {
        x := )      //E
        for i := range 3 {
            y := ]  //E
        }
    }
    z := 1
    w := ,          //E
}
`},
	{"bad declarations between good ones", `package main

func ok1() {
}

func (

func ok2() { //E
}

var = 3      //E

type T struct {
    x int64
}

type U struct {
    y          //E
}

func ok3() {
}
`},
	{"garbage between declarations", `package main

func a() {
}

+ - * / ;    //E

func b() {
}

) ) ]        //E

func c() {
}
`},
	{"an unclosed brace before the next function", `package main

func a() {
    x := 1

func b() {   //E
    y := 2 2 //E
}
`},
	{"an unclosed brace at the end of the file", `package main

func a() {
    z := 1 1 //E
    if true {
        y := 2 //E
`},
	{"errors in types", `package main

type A struct {
    x [3 int64      //E
}

func f(a list[) {   //E
}

func g(m map[int64) { //E
}
`},
	{"a bad switch clause and a good one", `package main

func main() {
    switch 1 {
    case 1:
        x := +   //E
    case 2:
        y := 1 1 //E
    default:
        print("d")
    }
}
`},
	{"a type declared in a closed function", `package main

func main() {
    type T struct { //E
        x int64
    }
    var y = 1 1     //E
}

func g() {
}
`},
	{"an error in an import", `package main

import from "x"    //E
import fmt from "std/fmt"

func main() {
    q := 1 1       //E
}
`},
}

// wantLines returns the 1-based lines of src marked //E.
func wantLines(src string) map[int]bool {
	want := make(map[int]bool)
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "//E") {
			want[i+1] = true
		}
	}
	return want
}

// lineOf returns the 1-based line of the offset at in src.
func lineOf(src string, at int) int {
	return strings.Count(src[:at], "\n") + 1
}

// TestRecoversToReportEveryError checks that the parser reports each of
// several independent syntax errors exactly once, on its line, with no
// error on any other line, and still gives a tree, in which the damage is
// marked by bad nodes.
func TestRecoversToReportEveryError(t *testing.T) {
	for _, tc := range recoverCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, diags := parse(t, tc.src)
			checkErrorLines(t, tc.src, diags)
			if tree == nil {
				t.Fatal("no tree")
			}
			if countBad(tree) == 0 {
				t.Error("the tree marks no damage with a bad node")
			}
		})
	}
}

// checkErrorLines checks that diags, the errors of src, are inside it,
// one on each line marked //E and none on any other.
func checkErrorLines(t *testing.T, src string, diags []diag.Diagnostic) {
	t.Helper()
	want := wantLines(src)
	got := make(map[int]int)
	for _, d := range diags {
		checkSpan(t, src, d)
		got[lineOf(src, d.Span.Start)]++
	}
	for line := range want {
		if got[line] != 1 {
			t.Errorf("line %d: %d errors, want 1", line, got[line])
		}
	}
	for line, n := range got {
		if !want[line] {
			t.Errorf("line %d: %d errors, want none", line, n)
		}
	}
	if t.Failed() {
		for _, d := range diags {
			t.Logf("%d: %s", lineOf(src, d.Span.Start), d.Message)
		}
	}
}

// countBad returns how many bad declarations and statements tree holds.
func countBad(tree *ast.File) int {
	n := 0
	for _, d := range tree.Decls {
		ast.Walk(d, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.BadDecl, *ast.BadStmt:
				n++
			}
			return true
		})
	}
	return n
}

// TestKeepsTheGoodDeclarations checks that the declarations around a bad
// one are in the tree, whole, beside it.
func TestKeepsTheGoodDeclarations(t *testing.T) {
	src := recoverCases[3].src
	tree, diags := parse(t, src)
	if len(diags) == 0 || tree == nil {
		t.Fatalf("tree %v, %d diagnostics", tree != nil, len(diags))
	}
	var names []string
	for _, d := range tree.Decls {
		switch x := d.(type) {
		case *ast.Func:
			names = append(names, x.Name)
		case *ast.TypeDecl:
			names = append(names, x.Name)
		case *ast.BadDecl:
			names = append(names, "bad")
		}
	}
	want := "ok1 bad ok2 bad T bad ok3"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("declarations %q, want %q", got, want)
	}
}

// TestValidFileHasNoBadNodes checks that a file without errors gives no
// bad node and no diagnostic.
func TestValidFileHasNoBadNodes(t *testing.T) {
	src := "package main\n\nfunc main() {\n    x := 1\n    if x > 0 {\n        print(\"y\")\n    }\n}\n"
	tree, diags := parse(t, src)
	if len(diags) > 0 || tree == nil || countBad(tree) > 0 {
		t.Fatalf("tree %v, %d diagnostics, %d bad nodes", tree != nil, len(diags), countBad(tree))
	}
}

// TestLimitStopsAtMaxErrors checks that a parser limited to n errors
// reports n + 1, the one past the limit saying there are more, and stops,
// and that a limit of 0 reports every error.
func TestLimitStopsAtMaxErrors(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n")
	for range 30 {
		b.WriteString("    x := 1 1\n")
	}
	b.WriteString("}\n")
	src := b.String()
	for _, tc := range []struct{ limit, want int }{{0, 30}, {10, 11}, {1, 2}, {29, 30}, {30, 30}} {
		f := &source.File{ID: "test", Filename: "case.tck", Data: []byte(src)}
		lex := lexer.New(f)
		if diags := lex.Lex(); len(diags) > 0 {
			t.Fatal(diags)
		}
		p := parser.NewParser(f.ID, lex.Tokens())
		p.Limit(tc.limit)
		_, diags := p.Parse()
		if len(diags) != tc.want {
			t.Errorf("limit %d: %d errors, want %d", tc.limit, len(diags), tc.want)
		}
	}
}

// validDecls are declarations each of which parses alone, for
// FuzzRecoversOtherDecls.
var validDecls = []string{
	"func a%d() {\n    x := 1\n    if x > 0 {\n        print(\"a\")\n    }\n}\n",
	"type T%d struct {\n    x int64\n    y [2]int64\n}\n",
	"var v%d = 3\n",
	"func b%d(n int64) int64 {\n    for i := range n {\n        n = n + i\n    }\n    return n\n}\n",
	"pub func c%d() {\n    switch {\n    case true:\n        print(\"c\")\n    }\n}\n",
	"type U%d union {\n    a\n    b int64\n}\n",
}

// declName returns the name a declaration of validDecls i, numbered k,
// declares.
func declName(i, k int) string {
	names := []string{"a", "T", "v", "b", "c", "U"}
	return fmt.Sprintf("%s%d", names[i], k)
}

// FuzzRecoversOtherDecls builds a file of declarations that each parse,
// inserts the fuzzer's bytes into one of them, and checks the parser
// terminates without panicking and, after the damaged one, still gives
// every other declaration whole. The bytes go in after the declaration's first line, as tokens:
// they cannot open a comment or a string that swallows the rest.
func FuzzRecoversOtherDecls(f *testing.F) {
	f.Add(uint8(0), uint8(1), "+ - ;")
	f.Add(uint8(3), uint8(2), ") ) ]")
	f.Add(uint8(2), uint8(0), "{ {")
	f.Add(uint8(5), uint8(4), "func (")
	f.Add(uint8(1), uint8(3), "} } }")
	f.Fuzz(func(t *testing.T, count, which uint8, garbage string) {
		if strings.ContainsAny(garbage, "\"'`/\r\x00") || !isTokens(garbage) {
			return
		}
		bad := int(which) % (2 + int(count)%6)
		src, good := garbled(2+int(count)%6, bad, garbage)
		tree := checkParse(t, src)
		if tree == nil {
			return // nested too deeply
		}
		found := declaredNames(tree)
		// The declaration right after the damaged one may be taken into
		// it when the garbage opens braces it never closes; every other
		// one must be there.
		for _, name := range good {
			if !found[name] && !afterBad(name, bad) {
				t.Errorf("%s is missing after garbage %q in declaration %d:\n%s",
					name, garbage, bad, src)
			}
		}
	})
}

// garbled returns a file of n declarations of validDecls, garbage written
// on a line of its own after the first line of the one numbered bad, and
// the names the others declare.
func garbled(n, bad int, garbage string) (string, []string) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	var good []string
	for k := range n {
		kind := k % len(validDecls)
		decl := fmt.Sprintf(validDecls[kind], k)
		if k == bad {
			first, rest, _ := strings.Cut(decl, "\n")
			decl = first + "\n" + garbage + "\n" + rest
		} else {
			good = append(good, declName(kind, k))
		}
		b.WriteString(decl + "\n")
	}
	return b.String(), good
}

// declaredNames returns the names tree's functions, types and variables
// declare.
func declaredNames(tree *ast.File) map[string]bool {
	found := make(map[string]bool)
	for _, d := range tree.Decls {
		switch x := d.(type) {
		case *ast.Func:
			found[x.Name] = true
		case *ast.TypeDecl:
			found[x.Name] = true
		case *ast.Var:
			found[x.Name] = true
		}
	}
	return found
}

// afterBad reports whether name, a declaration's, is numbered right after
// bad.
func afterBad(name string, bad int) bool {
	return strings.HasSuffix(name, fmt.Sprint(bad+1)) && len(name) == len(fmt.Sprint(bad+1))+1
}

// isTokens reports whether s lexes without error.
func isTokens(s string) bool {
	f := &source.File{ID: "g", Filename: "g.tck", Data: []byte(s)}
	return len(lexer.New(f).Lex()) == 0
}

// parseDeadline is how long checkParse lets one parse run before it calls
// the parser stuck: far longer than any input a fuzzer makes takes.
const parseDeadline = 10 * time.Second

// checkParse lexes src, and, when it lexes, parses it, checking what any
// input must give: the parse ends within parseDeadline and never panics,
// every error is inside the source, and the tree holds a bad node exactly
// when there is an error, with no tree only for a program nested too
// deeply. It returns the tree, or nil for a source that does not lex.
func checkParse(t *testing.T, src string) *ast.File {
	t.Helper()
	file := &source.File{ID: "fuzz", Filename: "fuzz.tck", Data: []byte(src)}
	lex := lexer.New(file)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil
	}
	tree, diags := parseWithin(t, file.ID, lex.Tokens(), src)
	for _, d := range diags {
		checkSpan(t, src, d)
	}
	switch {
	case tree == nil:
		if len(diags) == 0 || !strings.Contains(diags[len(diags)-1].Message, "nested too deeply") {
			t.Fatalf("no tree, and no error saying it nests too deeply: %v: %q", diags, src)
		}
	case (countBad(tree) > 0) != (len(diags) > 0):
		t.Fatalf("%d bad nodes with %d errors: %q", countBad(tree), len(diags), src)
	}
	return tree
}

// parseWithin parses tokens, of the file id whose text is src, failing
// the test when the parse panics or does not end within parseDeadline.
func parseWithin(
	t *testing.T,
	id source.FileID,
	tokens []lexer.Token,
	src string,
) (*ast.File, []diag.Diagnostic) {
	t.Helper()
	type result struct {
		tree     *ast.File
		panicked any
		diags    []diag.Diagnostic
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{panicked: r}
			}
		}()
		tree, diags := parser.NewParser(id, tokens).Parse()
		done <- result{tree: tree, diags: diags}
	}()
	select {
	case r := <-done:
		if r.panicked != nil {
			t.Fatalf("the parse panicked: %v: %q", r.panicked, src)
		}
		return r.tree, r.diags
	case <-time.After(parseDeadline):
		t.Fatalf("the parse did not end within %v: %q", parseDeadline, src)
		return nil, nil
	}
}

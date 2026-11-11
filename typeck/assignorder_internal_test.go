package typeck

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
)

// assignOrder returns a program whose main's body is lines, each on a
// line of its own. It declares Bag, a struct holding a list and an
// int64, and Row, one holding an array of 256 int64s; take, which frees
// a list and returns 3 as a uint8; takeBag and takeMap, which free a Bag
// and a map and return 3; read, which returns a list's length; and use,
// which reads an int64.
func assignOrder(lines ...string) string {
	return "package main\n\ntype Bag struct {\n    items list[int64]\n    n     int64\n}\n\n" +
		"func take(own l list[int64]) uint8 {\n    return 3\n}\n\n" +
		"func takeBag(own b Bag) int64 {\n    return 3\n}\n\n" +
		"func takeMap(own m map[int64]int64) int64 {\n    return 3\n}\n\n" +
		"func read(l list[int64]) int64 {\n    return len(l)\n}\n\n" +
		"func use(n int64) {\n}\n\n" +
		"type Row struct {\n    cells [256]int64\n}\n\n" +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// movesOf checks src, then follows the moves of its main again on their
// own and returns the diagnostics that gives. The checker skips the moves
// of a body it reported an error in, and a call in an index is reported
// as an index that may be out of range, so this is how a move in an
// assignment's target is followed at all.
func movesOf(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	f := &source.File{ID: "test", Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}
	prog := ast.SingleFile(file)
	if diags := resolver.New(prog, stdtest.MustBuiltins()).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}
	c := New(prog, f, stdtest.MustBuiltins())
	c.Check()
	for _, decl := range prog.Decls() {
		if fn, ok := decl.(*ast.Func); ok && fn.Name == "main" {
			c.diags = nil
			c.checkMoves(fn)
			return c.diags
		}
	}
	t.Fatal("the program has no main")
	return nil
}

// movedAt returns the diagnostics in diags that report a moved variable
// used, as "name is used after it was moved".
func movedAt(diags []diag.Diagnostic) []diag.Diagnostic {
	var moved []diag.Diagnostic
	for _, d := range diags {
		if strings.HasSuffix(d.Message, "is used after it was moved") {
			moved = append(moved, d)
		}
	}
	return moved
}

// TestAssignMovesFollowEvaluationOrder checks the move checker follows an
// assignment in the order the backend evaluates it: a plain assignment's
// indexes, but a map's key, before its value, then the store, which needs
// the variable it starts from usable; a compound assignment's target read
// before its value and stored after it. Each case is a program whose
// main, its moves followed on their own (see movesOf), uses a variable
// after a move once, at the name want, written once in the program, or
// never when want is empty. The cases with a call in an index are
// rejected as an index that may be out of range too, so no example can
// show them.
func TestAssignMovesFollowEvaluationOrder(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{
			"an index that moves before the value that reads",
			assignOrder(
				"mut a [256]int64",
				"l := make(list[int64], c_allocator)",
				"a[take(own l)] = read(l)",
				"use(a[0])",
			),
			"read(l)",
		},
		{
			"an inner index that moves before an outer one that reads",
			assignOrder(
				"mut grid [256][256]int64",
				"l := make(list[int64], c_allocator)",
				"grid[take(own l)][uint8(read(l))] = 1",
				"use(grid[0][0])",
			),
			"read(l)",
		},
		{
			"an outer index that moves before the value that reads",
			assignOrder(
				"mut grid [256][256]int64",
				"l := make(list[int64], c_allocator)",
				"grid[0][take(own l)] = read(l)",
				"use(grid[0][0])",
			),
			"read(l)",
		},
		{
			"a field's index that moves before the value that reads",
			assignOrder(
				"mut r Row",
				"l := make(list[int64], c_allocator)",
				"r.cells[take(own l)] = read(l)",
				"use(r.cells[0])",
			),
			"read(l)",
		},
		{
			"an index that reads before the value that moves",
			assignOrder(
				"mut a [256]int64",
				"l := make(list[int64], c_allocator)",
				"if len(l) < 256 {",
				"    a[len(l)] = int64(take(own l))",
				"}",
				"use(a[0])",
			),
			"",
		},
		{
			"a value that moves before the store into its list",
			assignOrder(
				"mut l := make(list[int64], c_allocator)",
				"if len(l) > 0 {",
				"    l[0] = int64(take(own l))",
				"}",
			),
			"l[0] =",
		},
		{
			"a compound value that moves before the store into its list",
			assignOrder(
				"mut l := make(list[int64], c_allocator)",
				"if len(l) > 0 {",
				"    l[0] +%= int64(take(own l))",
				"}",
			),
			"l[0] +%=",
		},
		{
			"a compound value that moves before the store into its map",
			assignOrder(
				"mut m := make(map[int64]int64, c_allocator)",
				"if has(m, 1) {",
				"    m[1] +%= takeMap(own m)",
				"}",
			),
			"m[1] +%=",
		},
		{
			"a compound value that moves before the store into its struct",
			assignOrder(
				"mut r := Bag{items: make(list[int64], c_allocator), n: 0}",
				"r.n +%= takeBag(own r)",
			),
			"r.n +%=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := movesOf(t, tt.src)
			moved := movedAt(diags)
			if tt.want == "" {
				if len(moved) > 0 {
					t.Fatalf("got %v, want no use after a move", moved)
				}
				return
			}
			at := strings.Index(tt.src, tt.want)
			if at < 0 || strings.Count(tt.src, tt.want) != 1 {
				t.Fatalf("%q is not written exactly once in the program", tt.want)
			}
			if len(moved) != 1 {
				t.Fatalf("got %v, want one use after a move, at %q", diags, tt.want)
			}
			// The name used is the first in want for a store, and the one
			// in parentheses for a call.
			name := at
			if open := strings.IndexByte(tt.want, '('); open >= 0 {
				name = at + open + 1
			}
			if moved[0].Span.Start != name {
				t.Errorf("use after a move reported at %d (%s), want %d, in %q",
					moved[0].Span.Start, moved[0].Message, name, tt.want)
			}
		})
	}
}

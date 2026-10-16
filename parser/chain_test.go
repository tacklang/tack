package parser_test

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// elseIfChain returns a program whose main holds one if followed by n-1
// else ifs and an else, flat in the source.
func elseIfChain(n int) string {
	var b strings.Builder
	b.WriteString(
		"package main\n\nfunc main() {\n    x := 1\n    if x == 0 {\n        print(\"0\")\n",
	)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "    } else if x == %d {\n        print(\"%d\")\n", i, i)
	}
	b.WriteString("    } else {\n        print(\"none\")\n    }\n}\n")
	return b.String()
}

// TestLongElseIfChainParsesInALoop parses a chain of 100000 else ifs with
// every goroutine's stack held to 1 MiB, which a parser recursing once per
// branch would run past, and checks the tree is the one a short chain
// gives: each else if the else branch of the if before it, holding that
// if alone, the last holding the else block, and every if spanning to the
// end of the chain.
func TestLongElseIfChainParsesInALoop(t *testing.T) {
	const n = 100000
	src := elseIfChain(n)
	old := debug.SetMaxStack(1 << 20)
	file, diags := parse(t, src)
	debug.SetMaxStack(old)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags[0])
	}
	main := file.Decls[0].(*ast.Func)
	first, ok := main.Body[1].(*ast.If)
	if !ok {
		t.Fatalf("main's second statement is %T, want an if", main.Body[1])
	}
	count, s := 1, first
	for len(s.Else) == 1 {
		next, isIf := s.Else[0].(*ast.If)
		if !isIf {
			break
		}
		if next.Span.End != first.Span.End {
			t.Fatalf(
				"if %d ends at %d, want the chain's end, %d",
				count,
				next.Span.End,
				first.Span.End,
			)
		}
		count, s = count+1, next
	}
	if count != n {
		t.Errorf("chain has %d ifs, want %d", count, n)
	}
	if len(s.Else) != 1 {
		t.Errorf("the last if's else holds %d statements, want the else block's 1", len(s.Else))
	}
	if want := strings.LastIndex(src, "}\n}"); first.Span.End != want+1 {
		t.Errorf("the chain ends at %d, want %d", first.Span.End, want+1)
	}
}

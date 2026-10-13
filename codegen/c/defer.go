package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// A defer emits nothing where it is written. Its statement is copied, in
// full, to every point where control leaves the block holding it after
// reaching it: the block's end, unless its last statement leaves it, and
// every return, break and continue inside the block that leaves it. Each
// copy is generated afresh, so its temporaries get names of their own.
//
// No flag records whether a defer was reached, since none is ever needed:
// Tack has no goto and no labels, so a statement of a block runs only after
// every statement before it in that block has finished, and a point that
// leaves the block from inside a later statement has, every time it runs,
// passed every defer written before that statement in the block. Which
// defers each exit runs is therefore known from where it is written: those
// before it in each block it leaves.
//
// A return evaluates its values first, into a const temporary of fn's
// result type, then runs the defers of every block of the function, and
// then returns the temporary, so a defer that changes a variable used in
// the returned expression does not change what is returned:
//
//	const int64_t tack_ret0 = v_x;
//	deferred statements, innermost block first, last first in each
//	return tack_ret0;
//
// A break or continue runs the defers of every block up to and including
// the body of the loop it acts on, then jumps. A C continue in a loop with
// a post statement jumps to the post statement, so the defers run before
// it, as the end of an iteration's would. A deferred block is wrapped in a
// C block, which scopes the variables it declares to that copy.

// deferScope is what the generator knows about one block while lowering
// it: the defers reached so far in it, in order, and whether it is a loop's
// body, which a break or continue leaves.
type deferScope struct {
	defers []*ast.Defer
	loop   bool
}

// pendingDefers reports whether a return from the statement being
// generated would leave any block with defers to run.
func (g *generator) pendingDefers() bool {
	for _, sc := range g.scopes {
		if len(sc.defers) > 0 {
			return true
		}
	}
	return false
}

// deferredReturn lowers a return that leaves blocks with defers to run.
// value is its rendered value, or "" for a bare return.
func (g *generator) deferredReturn(value string, fn *ast.Func) ([]string, []diag.Diagnostic) {
	var lines []string
	ret := "return;"
	if value != "" {
		tmp := g.temp("ret")
		lines = append(lines, fmt.Sprintf("const %s %s = %s;", g.resultType(fn), tmp, value))
		ret = fmt.Sprintf("return %s;", tmp)
	}
	run, diags := g.unwind(len(g.scopes), fn)
	if len(diags) > 0 {
		return nil, diags
	}
	return append(append(lines, run...), ret), nil
}

// generateJump lowers a break or continue, jump being its C statement:
// the defers of every block it leaves, up to and including the body of the
// innermost loop, then the jump.
func (g *generator) generateJump(jump string, fn *ast.Func) ([]string, []diag.Diagnostic) {
	n := len(g.scopes)
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if g.scopes[i].loop {
			n = len(g.scopes) - i
			break
		}
	}
	run, diags := g.unwind(n, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	return append(run, jump), nil
}

// unwind lowers the defers reached in the innermost n blocks, as leaving
// them runs them: innermost block first, and last reached first within
// each block.
func (g *generator) unwind(n int, fn *ast.Func) ([]string, []diag.Diagnostic) {
	var lines []string
	for i := len(g.scopes) - 1; i >= len(g.scopes)-n; i-- {
		defers := g.scopes[i].defers
		for j := len(defers) - 1; j >= 0; j-- {
			got, diags := g.deferred(defers[j], fn)
			if len(diags) > 0 {
				return nil, diags
			}
			lines = append(lines, got...)
		}
	}
	return lines, nil
}

// deferred lowers one copy of what d defers. It is generated with no
// enclosing blocks, since nothing in it leaves it, so its own defers run
// at its end and a break or continue in it finds the loop it is in. A
// deferred block becomes a C block.
func (g *generator) deferred(d *ast.Defer, fn *ast.Func) ([]string, []diag.Diagnostic) {
	outer := g.scopes
	g.scopes = nil
	body, diags := g.generateBlock(d.Body, fn)
	g.scopes = outer
	if len(diags) > 0 {
		return nil, diags
	}
	if !d.Block {
		return body, nil
	}
	return append(append([]string{"{"}, indent(body)...), "}"), nil
}

// endsInJump reports whether a block's last statement is a return, break
// or continue, which has run the block's defers itself, so its end needs
// no copy of them.
func endsInJump(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	switch body[len(body)-1].(type) {
	case *ast.Return, *ast.Break, *ast.Continue:
		return true
	default:
		return false
	}
}

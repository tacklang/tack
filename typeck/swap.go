package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// swap(mut l, i, j) swaps elements i and j of the list l in place: what
// was at i is at j, and what was at j at i, i equal to j leaving l as it
// was. It moves neither out of l, copies neither and frees nothing, so it
// takes elements of any type, owned ones and a type parameter's included,
// which is how generic code reorders them. Both indexes need proofs, as
// l[i] does, and there is no checked form; it changes no length, so it
// ends no fact about l (see mutWriter), but it passes l as mut, so no
// range over l and no binding into one of its elements can be alive
// across it (see move.go). It gives no value, and is only ever a call
// statement.

// isSwap reports whether call calls swap.
func isSwap(call *ast.Call) bool { return isBuiltinCall(call, "swap") }

// swapCall checks a call to swap, swap(mut l, i, j): its first argument
// is the list to swap two elements of, passed as mut as append's is, and
// its second and third the indexes, each usable as an int64 and proven
// in range for l, as l[i] is. A call anywhere but as a statement is
// reported, since it gives no value. It returns types.Invalid, the type
// of nothing.
func (c *Checker) swapCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "swap", 3) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	elem := c.listTarget(call.Args[0], "swap")
	c.checkArg(call.Args[1], param{name: "i", typ: types.Int64}, "swap")
	c.checkArg(call.Args[2], param{name: "j", typ: types.Int64}, "swap")
	if len(c.diags) > before || elem == types.Invalid {
		return types.Invalid
	}
	list := call.Args[0].(*ast.MutArg).X
	for _, i := range call.Args[1:] {
		c.requireSwapIndex(&ast.Index{X: list, Index: i, Span: i.Spanned(), Type: elem})
	}
	if c.droppedCall != call {
		c.errorf(call.Span, "swap returns no value")
	}
	return types.Invalid
}

// requireSwapIndex reports ix, standing for the element swap reads and
// writes at one of its indexes, when its index is not proven in range for
// its list: a known negative index always, and any other the facts in
// force do not prove, with what check would prove it.
func (c *Checker) requireSwapIndex(ix *ast.Index) {
	if v, known := c.knownValue(ix.Index); known && v.IsNegative() {
		c.reportOutside(ix, v)
		return
	}
	if !c.stringIndexProven(ix) {
		msg, notes := c.stringIndexError(ix, true)
		c.errorNotes(ix.Index.Spanned(), notes, "%s", msg)
	}
}

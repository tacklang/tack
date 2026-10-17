package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// swapStmt lowers swap(mut l, i, j), always a statement: the list's
// address, then i and then j, as int64s, each held in a const temporary
// in that order, then the two elements exchanged through a temporary of
// the element type, which, for an owned element, holds it for the moment
// both places do not, and frees nothing:
//
//	tack_list_9 *const tack_r0 = &v_l;
//	const int64_t tack_r1 = v_i;
//	const int64_t tack_r2 = v_j;
//	{
//	    const tack_list_4 tack_r3 = tack_r0->ptr[tack_r1];
//	    tack_r0->ptr[tack_r1] = tack_r0->ptr[tack_r2];
//	    tack_r0->ptr[tack_r2] = tack_r3;
//	}
//
// The checker proved both indexes in range, and an i equal to j writes
// the element back where it was.
func (g *generator) swapStmt(call *ast.Call) ([]string, []diag.Diagnostic) {
	lp, diags := g.takeList(call)
	if len(diags) > 0 {
		return nil, diags
	}
	src := multiSource{temps: []multiTemp{
		{typ: g.cType(ast.TypeOf(call.Args[0].(*ast.MutArg).X)), init: lp, mut: true},
	}}
	for _, index := range call.Args[1:] {
		x, diags := g.expr(index, types.Int64)
		if len(diags) > 0 {
			return nil, diags
		}
		src.temps = append(src.temps, multiTemp{typ: "int64_t", init: x})
	}
	names, lines := g.holdTemps(src)
	elem := g.cType(g.prog.Types.Elem(ast.TypeOf(call.Args[0].(*ast.MutArg).X)))
	at := func(k int) string { return fmt.Sprintf("%s->ptr[%s]", names[0], names[k]) }
	held := g.temp("r")
	return append(lines,
		"{",
		indentIn,
		fmt.Sprintf("const %s %s = %s;", elem, held, at(1)),
		fmt.Sprintf("%s = %s;", at(1), at(2)),
		fmt.Sprintf("%s = %s;", at(2), held),
		indentOut,
		"}",
	), nil
}

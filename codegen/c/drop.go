package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// dropStmt lowers drop(own x), always a statement: the free the end of
// x's block would emit (see frees), then x emptied, as a move empties
// it, so that free, still on the block's cleanup chain, frees nothing:
//
//	tack_list_free_(v_l.ptr, v_l.cap, sizeof (int64_t), _Alignof (int64_t), v_l.alloc);
//	v_l = (tack_list_3){};
//
// The checker allows only a local variable or an own parameter, which a
// closure has not captured, so x is its C variable itself. In an instance
// of a generic function whose type argument owns nothing, x holds
// nothing to free, and the drop is no statement at all, as a move of it
// is a copy (see moveValue).
func (g *generator) dropStmt(call *ast.Call) ([]string, []diag.Diagnostic) {
	var ref string
	id, _ := ast.ArgOperand(call.Args[0]).(*ast.Ident)
	if id != nil {
		switch d := id.Def.(type) {
		case *ast.Var:
			ref = g.varRef(d)
		case *ast.Param:
			ref = g.paramRef(d)
		}
	}
	if ref == "" {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "internal error: drop of something other than a variable",
			Span:     call.Spanned(),
		}}
	}
	typ := ast.TypeOf(id)
	if !g.owned(typ) {
		return nil, nil
	}
	return append(g.frees(typ, ref), fmt.Sprintf("%s = (%s){};", ref, g.cType(typ))), nil
}

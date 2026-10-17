package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// replaceValue renders old := replace(mut place, new) as a comma
// expression through temporaries declared at the function's top: the
// address of the place, taken first, with every index in it, then new,
// rendered as the place's type, a move when it is an owned name, then the
// place's old value, read only now, so new may read it, which is stored
// over by new and given as the result:
//
//	(tack_o1 = &v_s.f_frames, tack_o2 = v_l, tack_o3 = *tack_o1,
//	 *tack_o1 = tack_o2, tack_o3)
//
// Nothing is copied but the value itself, so an owned value moves: the old
// one to the result's owner, the new one into the place.
func (g *generator) replaceValue(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "replace's place must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	addr, diags := g.mutArg(m)
	if len(diags) > 0 {
		return "", diags
	}
	value, diags := g.expr(call.Args[1], call.Type)
	if len(diags) > 0 {
		return "", diags
	}
	t := g.cType(call.Type)
	p, n, o := g.temp("o"), g.temp("o"), g.temp("o")
	g.frame.temps = append(g.frame.temps,
		fmt.Sprintf("%s *%s;", t, p), fmt.Sprintf("%s %s;", t, n), fmt.Sprintf("%s %s;", t, o))
	return fmt.Sprintf(
		"(%s = %s, %s = %s, %s = *%s, *%s = %s, %s)",
		p,
		addr,
		n,
		value,
		o,
		p,
		p,
		n,
		o,
	), nil
}

// dropReplace lowers replace(mut place, new) written as a statement, an
// assignment of new to the place: the call, as replaceValue renders it,
// and, when the place's type is owned, a free of the old value it gives.
func (g *generator) dropReplace(call *ast.Call) ([]string, []diag.Diagnostic) {
	value, diags := g.replaceValue(call)
	if len(diags) > 0 {
		return nil, diags
	}
	if !g.owned(call.Type) {
		return []string{"(void)" + value + ";"}, nil
	}
	old := g.temp("r")
	return append([]string{fmt.Sprintf("const %s %s = %s;", g.cType(call.Type), old, value)},
		g.frees(call.Type, old)...), nil
}

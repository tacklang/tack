package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// anyIDs maps each type a value can be passed as any with to the macro of
// std's tack.h that names its type id, which a tack_any holds.
var anyIDs = map[types.Type]string{
	types.Bool:          "TACK_TYPE_BOOL",
	types.Int8:          "TACK_TYPE_INT8",
	types.Int16:         "TACK_TYPE_INT16",
	types.Int32:         "TACK_TYPE_INT32",
	types.Int64:         "TACK_TYPE_INT64",
	types.Uint8:         "TACK_TYPE_UINT8",
	types.Uint16:        "TACK_TYPE_UINT16",
	types.Uint32:        "TACK_TYPE_UINT32",
	types.Uint64:        "TACK_TYPE_UINT64",
	types.UnsafeSize:    "TACK_TYPE_UNSAFE_SIZE_T",
	types.UnsafeUintptr: "TACK_TYPE_UNSAFE_UINTPTR_T",
	types.String:        "TACK_TYPE_STRING",
}

// boxesAt reports whether call, to a function with parameters params,
// makes an any of its argument i: the argument is a value of a basic type
// passed to an any parameter, or as a value of an ...any one. An any
// passed on is one already, and a spread is a view.
func (g *generator) boxesAt(call *ast.Call, params []*ast.Param, i int) bool {
	if ast.TypeOf(call.Args[i]) == types.Any || g.viewParamAt(call, params, i) != nil {
		return false
	}
	var typ types.Type
	switch n := len(params); {
	case packs(call, params) && i >= n-1:
		typ = g.prog.Types.Elem(params[n-1].Type)
	case i < n:
		typ = params[i].Type
	}
	return typ == types.Any
}

// boxed renders arg, already rendered as op, as the tack_any a call passes
// for it: its type's id and the address of a copy of its value as it was
// when the argument was evaluated, which lives for the whole call. held
// says op is a temporary the call's order of evaluation already holds arg
// in (see sequenced), whose address is taken as it is. So is a name the
// call cannot change, one stable says is. Anything else, a mutable global,
// a variable passed as mut somewhere in the function, a mut parameter, a
// field, an element, a call or a constant, is first copied into a
// temporary declared at the top of the function, (tack_a0 = op,
// (tack_any){id, &tack_a0}), which a later call through the same line
// reuses once this one has returned.
func (g *generator) boxed(arg ast.Expr, op string, held bool) string {
	typ := ast.TypeOf(arg)
	id := anyIDs[typ]
	if held || g.stable(arg) {
		return fmt.Sprintf("(tack_any){%s, &%s}", id, op)
	}
	t := g.temp("a")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.cType(typ), t))
	return fmt.Sprintf("(%s = %s, (tack_any){%s, &%s})", t, op, id, t)
}

// stable reports whether arg names a variable or parameter whose value no
// call can change while the function runs, so an any may point at it
// directly: a plain parameter, an immutable local or global, or a mutable
// local the function never passes as mut. A call can change a local only
// through a mut argument, and a mutable global at any time.
func (g *generator) stable(arg ast.Expr) bool {
	id, ok := arg.(*ast.Ident)
	if !ok {
		return false
	}
	switch def := id.Def.(type) {
	case *ast.Param:
		return !def.Mut
	case *ast.Var:
		if g.globals[def] || !def.Mutable {
			return !def.Mutable
		}
		return g.frame == nil || !g.frame.mutated[def]
	default:
		return false
	}
}

package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// make for Data. make(T, alloc), with T written as a name, a Data type or
// a type parameter standing for one, lowers to the empty value of the
// type T stands for in the instance being lowered, as one C expression:
// 0 or false for a basic type, a list or a map holding alloc and no
// buffer, as make(list[T], alloc) and make(map[K]V, alloc) are, and for
// a struct a compound literal setting every field the same way, private
// ones included, at every depth. A struct whose fields hold no list or
// map is its zeroed value. No struct holds itself, through a field or a
// list or a map, so the walk over the type ends, and it is the whole of
// the work: nothing recurs at run time, and nothing is allocated, so the
// value cannot fail to be made. alloc is evaluated exactly once, before
// the value is built: in place, where the one list or map holding it is,
// or where each is when it is a name or c_allocator, which reading
// cannot change; held in a temporary at the function's top when it is
// any other expression and two or more hold it; and cast to void beside
// the value when none does.

// makeData renders call, make(T, alloc) with T a name, as the empty value
// of the type T stands for (see the comment at the top of the file).
func (g *generator) makeData(call *ast.Call) (string, []diag.Diagnostic) {
	alloc, diags := g.expr(call.Args[1], types.Allocator)
	if len(diags) > 0 {
		return "", diags
	}
	typ := g.prog.Types.Bound(call.Type)
	switch n := g.allocUses(typ); {
	case n == 0:
		return fmt.Sprintf("((void)(%s), %s)", alloc, g.zeroValue(typ)), nil
	case n > 1 && !readOnly(call.Args[1]):
		tmp := g.temp("a")
		g.frame.temps = append(g.frame.temps,
			fmt.Sprintf("%s %s;", g.cType(types.Allocator), tmp))
		return fmt.Sprintf("(%s = %s, %s)", tmp, alloc, g.emptyValue(typ, tmp)), nil
	default:
		return g.emptyValue(typ, alloc), nil
	}
}

// makeStmt lowers make(T, alloc) written as a statement, which the type
// checker allows only for a value that owns nothing, so nothing is to be
// freed: the value, its allocator evaluated once, cast to void.
func (g *generator) makeStmt(call *ast.Call) ([]string, []diag.Diagnostic) {
	v, diags := g.makeValue(call)
	if len(diags) > 0 {
		return nil, diags
	}
	return []string{fmt.Sprintf("(void)(%s);", v)}, nil
}

// readOnly reports whether alloc, an Allocator, is read without a side
// effect, so it may be rendered more than once: a name, or c_allocator.
func readOnly(alloc ast.Expr) bool {
	switch alloc.(type) {
	case *ast.Ident, *ast.CAllocatorLit:
		return true
	default:
		return false
	}
}

// allocUses returns how many lists and maps the empty value of typ, a
// Data type the binding in force has bound, holds, each holding the
// allocator it was made with: one for a list or a map, the sum over its
// fields for a struct, and none for a basic type.
func (g *generator) allocUses(typ types.Type) int {
	t := g.prog.Types
	typ = t.Bound(typ)
	switch {
	case t.IsList(typ), t.IsMap(typ):
		return 1
	case t.IsStruct(typ):
		n := 0
		for _, f := range t.Fields(typ) {
			n += g.allocUses(f.Type)
		}
		return n
	default:
		return 0
	}
}

// emptyValue renders the empty value of typ, a Data type the binding in
// force has bound, its lists and maps holding alloc, a C expression of
// type tack_allocator that each reads.
func (g *generator) emptyValue(typ types.Type, alloc string) string {
	t := g.prog.Types
	typ = t.Bound(typ)
	switch {
	case t.IsList(typ):
		return fmt.Sprintf("((%s){.alloc = %s})", g.listName(typ), alloc)
	case t.IsMap(typ):
		return fmt.Sprintf("((%s){.alloc = %s})", g.mapName(typ), alloc)
	case t.IsStruct(typ) && g.allocUses(typ) > 0:
		fields := t.Fields(typ)
		inits := make([]string, len(fields))
		for i, f := range fields {
			inits[i] = fmt.Sprintf(".%s = %s", memberName(typ, f.Name), g.emptyValue(f.Type, alloc))
		}
		return fmt.Sprintf("((%s){%s})", g.structName(typ), strings.Join(inits, ", "))
	default:
		return g.zeroValue(typ)
	}
}

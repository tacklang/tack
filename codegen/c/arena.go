package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// An arena, arena[T], lowers to a struct of its own per element type: a
// list of its elements but the sentinel, ids 1 to len - 1 at ptr[0] to
// ptr[len - 2], and the sentinel, id 0, held inline, so make allocates
// nothing and the sentinel never moves:
//
//	typedef struct {
//	    tack_list_8 elems;
//	    int64_t sentinel;
//	} tack_arena_9;
//
// An id, id[T], lowers to a uint32_t, and a span, span[T], to a uint64_t,
// its start in the low 32 bits and its length in the high 32, so the zero
// span is the empty one, and both compare with C's == and hash as an
// integer of their width does. Every sum of an id, a start and a length
// is taken in 64 bits, where no two 32-bit values overflow.
//
// Each arena type has helpers of its own, written once its element
// type's free is (see emitArenas): its at helper gives the address of the
// element an id names, clamped to the sentinel for any id not below the
// arena's length, tack_arena_at_9; its span helper the view of a span's
// elements, or the empty view for a span not wholly in the arena,
// tack_arena_span_9; its add, add_all and reserve helpers grow it through
// std's TACK_LIST_APPEND, TACK_LIST_APPEND_N and TACK_LIST_RESERVE, after
// tack_arena_room_ says the arena can hold that many more without passing
// 2^32 - 1 elements, its sentinel included, and give out of memory, the
// arena unchanged, when it cannot, freeing what add and add_all were
// given when the element type is owned; and its tack_free_ frees its
// elements, last first, then its sentinel. add's results are a
// tack_arena_added_, an id and an error, and add_all's a
// tack_arena_spanned_, a span and an error, which the runtime's tack.h
// defines, read field by field as a function's results struct is.

// arenaNameOf is the name of the C struct an arena type lowers to, made
// from its ID as a list's is.
func arenaNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_arena_%d", typ)
}

// arenaName is the name of the C struct of the arena type typ stands for.
func (g *generator) arenaName(typ types.Type) string {
	return arenaNameOf(g.prog.Types.Bound(typ))
}

// arenaHelper is the name of the helper what of the arena type typ stands
// for: at, span, add, add_all or reserve.
func (g *generator) arenaHelper(what string, typ types.Type) string {
	return fmt.Sprintf("tack_arena_%s_%d", what, g.prog.Types.Bound(typ))
}

// arenaList returns the list type of the elements of the arena type typ,
// which the Table added with it.
func (g *generator) arenaList(typ types.Type) types.Type {
	table := g.prog.Types
	return table.List(table.Elem(typ))
}

// arenaResults is the C struct a call to add, or, when all is set, to
// add_all, gives: its id or span, r0, and its error, r1.
func arenaResults(all bool) string {
	if all {
		return "tack_arena_spanned_"
	}
	return "tack_arena_added_"
}

// emitArenaDef defines the C struct an arena type lowers to, whose list
// and element types are defined already.
func (g *generator) emitArenaDef(typ types.Type) {
	fmt.Fprintf(&g.buf, "typedef struct {\n    %s elems;\n    %s sentinel;\n} %s;\n\n",
		g.listName(g.arenaList(typ)), g.cType(g.prog.Types.Elem(typ)), g.arenaName(typ))
}

// arenaTypes returns every arena type C has, in the order the checker
// added them.
func (g *generator) arenaTypes() []types.Type {
	var arenas []types.Type
	for _, typ := range g.emittedTypes() {
		if g.prog.Types.IsArena(typ) {
			arenas = append(arenas, typ)
		}
	}
	return arenas
}

// emitArenas writes the helpers of every arena type (see the comment at
// the top of this file), once every free they call is written.
func (g *generator) emitArenas() {
	for _, typ := range g.arenaTypes() {
		g.emitArenaLookups(typ)
		g.emitArenaAdds(typ)
	}
}

// emitArenaLookups writes the at and span helpers of the arena type typ.
// The at helper takes a const pointer and gives a pointer the caller may
// write through, as a map's at helper does: an arena is owned, so no
// variable holding one is const in C, and the checker lets only a
// mutable arena's element be written.
//
//	static int64_t *tack_arena_at_9(const tack_arena_9 *a, uint32_t id) {
//	    uint64_t i = (uint64_t)id - 1;
//	    return i < (uint64_t)a->elems.len ? &a->elems.ptr[i] : (int64_t *)&a->sentinel;
//	}
//
// id 0 makes i the largest uint64_t, which is never below the length, so
// it reads the sentinel, as an id past the end does; before the first add
// the list is empty, its pointer null, and every id reads the sentinel.
// A span is in the arena when its start is at least 1 and its start plus
// its length, less 1, is at most the list's length; one of length 0 is
// the empty view, whose null pointer nothing reads.
func (g *generator) emitArenaLookups(typ types.Type) {
	table := g.prog.Types
	a, elem := g.arenaName(typ), g.cType(table.Elem(typ))
	at := g.arenaHelper("at", typ)
	fmt.Fprintf(&g.buf, "%sstatic %s *%s(const %s *a, uint32_t id) {\n"+
		"    uint64_t i = (uint64_t)id - 1;\n"+
		"    return i < (uint64_t)a->elems.len ? &a->elems.ptr[i] : (%s *)&a->sentinel;\n}\n\n",
		g.maybeUnused(at), elem, at, a, elem)
	view := g.viewName(table.View(table.Elem(typ)))
	span := g.arenaHelper("span", typ)
	fmt.Fprintf(&g.buf, "%sstatic %s %s(const %s *a, uint64_t s) {\n"+
		"    uint64_t start = s & 0xffffffffu, n = s >> 32;\n"+
		"    if (n == 0 || start == 0 || start - 1 + n > (uint64_t)a->elems.len) {\n"+
		"        return (%s){0, 0};\n    }\n"+
		"    return (%s){a->elems.ptr + (start - 1), (int64_t)n};\n}\n\n",
		g.maybeUnused(span), view, span, a, view, view)
}

// emitArenaAdds writes the add, add_all and reserve helpers of the arena
// type typ. add takes its value by value and add_all a pointer to its
// values and their count, each moved in when the element type is owned,
// so each frees them, last first, when the arena cannot take them. add
// gives the new element's id, the list's new length, and add_all the
// span of its values, from the first one's id, or the zero span for no
// values, which allocates nothing and never fails.
//
//	static tack_arena_added_ tack_arena_add_9(tack_arena_9 *a, int64_t v) {
//	    if (tack_arena_room_(a->elems.len + 1, 1) && TACK_LIST_APPEND(&a->elems, v)) {
//	        return (tack_arena_added_){(uint32_t)a->elems.len, TACK_NO_ERROR};
//	    }
//	    return (tack_arena_added_){0, TACK_ERROR(1, "out of memory")};
//	}
func (g *generator) emitArenaAdds(typ types.Type) {
	table := g.prog.Types
	a, elemType := g.arenaName(typ), table.Elem(typ)
	elem := g.cType(elemType)
	oom := fmt.Sprintf("TACK_ERROR(%d, %q)", outOfMemoryCode, outOfMemory)

	add := g.arenaHelper("add", typ)
	fmt.Fprintf(&g.buf, "%sstatic tack_arena_added_ %s(%s *a, %s v) {\n"+
		"    if (tack_arena_room_(a->elems.len + 1, 1) && TACK_LIST_APPEND(&a->elems, v)) {\n"+
		"        return (tack_arena_added_){(uint32_t)a->elems.len, TACK_NO_ERROR};\n    }\n",
		g.maybeUnused(add), add, a, elem)
	for _, line := range g.frees(elemType, "v") {
		fmt.Fprintf(&g.buf, "    %s\n", line)
	}
	fmt.Fprintf(&g.buf, "    return (tack_arena_added_){0, %s};\n}\n\n", oom)

	all := g.arenaHelper("add_all", typ)
	fmt.Fprintf(&g.buf, "%sstatic tack_arena_spanned_ %s(%s *a, const %s *src, int64_t n) {\n"+
		"    if (n == 0) {\n        return (tack_arena_spanned_){0, TACK_NO_ERROR};\n    }\n"+
		"    int64_t start = a->elems.len + 1;\n"+
		"    if (tack_arena_room_(start, n) && TACK_LIST_APPEND_N(&a->elems, src, n)) {\n"+
		"        return (tack_arena_spanned_){(uint64_t)start | (uint64_t)n << 32, TACK_NO_ERROR};\n"+
		"    }\n",
		g.maybeUnused(all), all, a, elem)
	if frees := g.frees(elemType, "src[i - 1]"); len(frees) > 0 {
		g.buf.WriteString("    for (int64_t i = n; i > 0; i--) {\n")
		for _, line := range frees {
			fmt.Fprintf(&g.buf, "        %s\n", line)
		}
		g.buf.WriteString("    }\n")
	}
	fmt.Fprintf(&g.buf, "    return (tack_arena_spanned_){0, %s};\n}\n\n", oom)

	reserve := g.arenaHelper("reserve", typ)
	fmt.Fprintf(&g.buf, "%sstatic bool %s(%s *a, int64_t n) {\n"+
		"    return tack_arena_room_(a->elems.len + 1, n) && TACK_LIST_RESERVE(&a->elems, n);\n}\n\n",
		g.maybeUnused(reserve), reserve, a)
}

// emitArenaFree writes tack_free_ for the arena type typ: it frees the
// list of its elements, each element last first when they are owned, then
// its sentinel when it is owned. A moved arena is zeroed, its list empty
// and its sentinel zeroed, holding nothing, so freeing it does nothing.
//
//	static void tack_free_9(const tack_arena_9 *v) {
//	    tack_list_free_(v->elems.ptr, ...);
//	}
func (g *generator) emitArenaFree(typ types.Type) {
	free := g.freeName(typ)
	fmt.Fprintf(
		&g.buf,
		"%sstatic void %s(const %s *v) {\n",
		g.maybeUnused(free),
		free,
		g.arenaName(typ),
	)
	for _, line := range g.frees(g.arenaList(typ), "v->elems") {
		fmt.Fprintf(&g.buf, "    %s\n", line)
	}
	for _, line := range g.frees(g.prog.Types.Elem(typ), "v->sentinel") {
		fmt.Fprintf(&g.buf, "    %s\n", line)
	}
	g.buf.WriteString("}\n\n")
}

// makeArena renders make(arena[T], sentinel, alloc) as an arena of an
// empty list holding the allocator and the sentinel, which allocates
// nothing, the sentinel and the allocator evaluated in that order.
func (g *generator) makeArena(call *ast.Call) (string, []diag.Diagnostic) {
	elem := g.prog.Types.Elem(call.Type)
	sentinel, diags := g.expr(call.Args[1], elem)
	if len(diags) > 0 {
		return "", diags
	}
	alloc, diags := g.expr(call.Args[2], types.Allocator)
	if len(diags) > 0 {
		return "", diags
	}
	name := g.arenaName(call.Type)
	return g.sequenced(call.Args[1:], []string{sentinel, alloc}, func(ops []string) string {
		return fmt.Sprintf("((%s){.elems = {.alloc = %s}, .sentinel = %s})", name, ops[1], ops[0])
	}), nil
}

// arenaIndexExpr renders ix, an element of an arena or a span's view of
// one, read, the arena and the index evaluated in order: the element its
// at helper gives the address of, or the view its span helper gives.
func (g *generator) arenaIndexExpr(ix *ast.Index) (string, []diag.Diagnostic) {
	base, diags := g.expr(ix.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	index, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	typ := ast.TypeOf(ix.X)
	return g.sequenced([]ast.Expr{ix.X, ix.Index}, []string{"(" + base + ")", index},
		func(ops []string) string {
			if g.prog.Types.IsSpan(ast.TypeOf(ix.Index)) {
				return fmt.Sprintf("%s(&%s, %s)", g.arenaHelper("span", typ), ops[0], ops[1])
			}
			return fmt.Sprintf("(*%s(&%s, %s))", g.arenaHelper("at", typ), ops[0], ops[1])
		}), nil
}

// arenaElementRef renders ix, an element of an arena assigned or passed as
// mut, as a C lvalue, the element the arena's at helper gives the address
// of in base, the arena rendered as a target (see targetRef), index being
// its id rendered.
func (g *generator) arenaElementRef(ix *ast.Index, base, index string) string {
	return fmt.Sprintf("(*%s(&%s, %s))", g.arenaHelper("at", ast.TypeOf(ix.X)), base, index)
}

// addValues lowers e, err := add(mut a, v) and s, err := add_all(mut a,
// ...), the source of a MultiVar or a MultiAssign, or of a ?: the call to
// the arena's add or add_all helper, held as its results struct and read
// field by field (see addExpr).
func (g *generator) addValues(call *ast.Call) (multiSource, []diag.Diagnostic) {
	x, diags := g.addExpr(call)
	return multiSource{
		temps: []multiTemp{{typ: arenaResults(isBuiltin(call, "add_all")), init: x}},
		field: func(tmps []string, i int) string { return fmt.Sprintf("%s.r%d", tmps[0], i) },
	}, diags
}

// addExpr renders a call to add or add_all as a call to the arena's
// helper on the address of the arena's place, its operands evaluated in
// order, the place first, as append's are (see appendOperands): add's
// one value, or add_all's values as a compound literal array of them,
// (const T[n]){a, b, c}, or a null pointer for none, or a spread view,
// held in a temporary so it is evaluated once, and given as its pointer
// and its length.
func (g *generator) addExpr(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		id, _ := call.Fn.(*ast.Ident)
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  id.Name + "'s arena must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	rendered, ctypes, diags := g.appendOperands(call, m)
	if len(diags) > 0 {
		return "", diags
	}
	typ := ast.TypeOf(m.X)
	elem := g.prog.Types.Elem(typ)
	build := func(ops []string) string {
		ap, values := ops[0], ops[1:]
		if isBuiltin(call, "add") {
			return fmt.Sprintf("%s(%s, %s)", g.arenaHelper("add", typ), ap, values[0])
		}
		all := g.arenaHelper("add_all", typ)
		switch {
		case call.Spread:
			t := g.temp("v")
			view := g.prog.Types.View(elem)
			g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.viewName(view), t))
			return fmt.Sprintf("(%s = %s, %s(%s, %s.ptr, %s.len))", t, values[0], all, ap, t, t)
		case len(values) == 0:
			return fmt.Sprintf("%s(%s, 0, 0)", all, ap)
		default:
			return fmt.Sprintf("%s(%s, ((const %s[%d]){%s}), %d)", all, ap, g.cType(elem),
				len(values), strings.Join(values, ", "), len(values))
		}
	}
	return g.sequencedAs(call.Args, rendered, ctypes, build), nil
}

// arenaRange lowers a range loop over an arena, whose lowered address is
// addr, a place, since the checker gives a temporary arena no range, and
// lowered body body: every id from 0, the sentinel's, below the arena's
// length, each element read through the at helper, bound in place when
// its type is owned, and copied otherwise. The body cannot change the
// arena while the loop runs (see typeck's move.go), so its length is read
// before each pass from the arena itself:
//
//	{
//	    const tack_arena_9 *const tack_r0 = &v_a;
//	    for (int64_t tack_k1 = 0;; tack_k1++) {
//	        if (!(tack_k1 < tack_r0->elems.len + 1)) {
//	            break;
//	        }
//	        const uint32_t v_i = (uint32_t)tack_k1;
//	        const int64_t v_x = *tack_arena_at_9(tack_r0, (uint32_t)tack_k1);
//	        body
//	    }
//	}
func (g *generator) arenaRange(loop *ast.ForRange, addr string, body []string) []string {
	typ := ast.TypeOf(loop.X)
	r, k := g.temp("r"), g.temp("k")
	at := fmt.Sprintf("%s(%s, (uint32_t)%s)", g.arenaHelper("at", typ), r, k)
	var vars []string
	if loop.Var != nil && loop.Var.Name != ast.Blank {
		vars = append(vars, fmt.Sprintf("const uint32_t %s = (uint32_t)%s;", declName(loop.Var), k))
	}
	switch {
	case g.bindsInPlace(loop):
		vars = append(vars, fmt.Sprintf("const %s *const %s = %s;",
			g.cType(loop.Elem.Type), declName(loop.Elem), at))
	case bindsElem(loop):
		vars = append(vars, fmt.Sprintf("const %s %s = *%s;",
			g.cType(loop.Elem.Type), declName(loop.Elem), at))
	}
	eval := fmt.Sprintf("const %s *const %s = %s;", g.cType(typ), r, addr)
	return rangeBlock(eval, "int64_t", k, r+"->elems.len + 1", append(vars, body...))
}

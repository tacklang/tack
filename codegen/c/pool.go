package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A pool, pool[T], lowers to a struct of its own per element type: a
// buffer of slots, laid out first as a list's is, so std's
// TACK_LIST_RESERVE grows it, the number of live elements, the index plus
// one of the first free slot, 0 for none, and the pool's tag, which
// tack_pool_tag_ gives each make, never 0. Each slot holds its element,
// its generation, the index plus one of the next free slot while it is
// free, and whether it is live:
//
//	typedef struct {
//	    int64_t value;
//	    uint32_t gen;
//	    uint32_t next;
//	    bool live;
//	} tack_pool_slot_9;
//
//	typedef struct {
//	    tack_pool_slot_9 *ptr;
//	    int64_t len;
//	    int64_t cap;
//	    tack_allocator alloc;
//	    int64_t count;
//	    uint32_t free;
//	    uint32_t tag;
//	} tack_pool_9;
//
// A handle, handle[T], lowers to the runtime's tack_handle_, a slot's
// index, the generation it had when the element was inserted, and the
// pool's tag, compared with tack_handle_eq_; the zeroed handle's tag is
// 0, which no pool has.
//
// Each pool type has helpers of its own, written once its element type's
// free is (see emitPools): its find helper gives the slot a handle names
// only when the tag, the index against the slots' count, whether the
// slot is live and its generation all match, and a null pointer
// otherwise, so a stale or foreign handle never reads a slot; its at
// helper gives the address of the element of a handle the type checker
// proved live with has, a plain slot access; its insert helper takes the
// first free slot, or grows the buffer by one slot, starting at the
// generation tack_pool_gen0_ gives, after tack_pool_room_ says the pool
// may hold one more, and gives out of memory, the pool unchanged and the
// value freed, when it can do neither; and its take helper moves a live
// slot's element out, zeroes the slot and moves its generation on, unless
// the generation is the last, 2^32 - 1, which retires the slot for good
// instead of putting it on the free list, checked before the increment so
// no generation is ever issued twice. Its tack_free_ frees each live
// element, last first, then the buffer. insert's results are a
// tack_pool_inserted_, a handle and an error, read field by field as a
// function's results struct is.

// poolNameOf is the name of the C struct a pool type lowers to, made from
// its ID as a list's is.
func poolNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_pool_%d", typ)
}

// poolName is the name of the C struct of the pool type typ stands for.
func (g *generator) poolName(typ types.Type) string {
	return poolNameOf(g.prog.Types.Bound(typ))
}

// poolSlotName is the name of the C struct of one slot of the pool type
// typ stands for.
func (g *generator) poolSlotName(typ types.Type) string {
	return fmt.Sprintf("tack_pool_slot_%d", g.prog.Types.Bound(typ))
}

// poolHelper is the name of the helper what of the pool type typ stands
// for: find, at, insert or take.
func (g *generator) poolHelper(what string, typ types.Type) string {
	return fmt.Sprintf("tack_pool_%s_%d", what, g.prog.Types.Bound(typ))
}

// handleType is the C type every handle type lowers to.
const handleType = "tack_handle_"

// emitPoolDef defines the C structs a pool type lowers to, its slot's and
// its own, whose element type is defined already.
func (g *generator) emitPoolDef(typ types.Type) {
	slot := g.poolSlotName(typ)
	fmt.Fprintf(&g.buf, "typedef struct {\n    %s value;\n    uint32_t gen;\n    uint32_t next;\n"+
		"    bool live;\n} %s;\n\n", g.cType(g.prog.Types.Elem(typ)), slot)
	fmt.Fprintf(&g.buf, "typedef struct {\n    %s *ptr;\n    int64_t len;\n    int64_t cap;\n"+
		"    tack_allocator alloc;\n    int64_t count;\n    uint32_t free;\n    uint32_t tag;\n} %s;\n\n",
		slot, g.poolName(typ))
}

// poolTypes returns every pool type C has, in the order the checker added
// them.
func (g *generator) poolTypes() []types.Type {
	var pools []types.Type
	for _, typ := range g.emittedTypes() {
		if g.prog.Types.IsPool(typ) {
			pools = append(pools, typ)
		}
	}
	return pools
}

// emitPools writes the helpers of every pool type (see the comment at the
// top of this file), once every free they call is written.
func (g *generator) emitPools() {
	for _, typ := range g.poolTypes() {
		g.emitPoolLookups(typ)
		g.emitPoolInsert(typ)
		g.emitPoolTake(typ)
	}
}

// emitPoolLookups writes the find and at helpers of the pool type typ.
// Each takes a const pointer and gives a pointer the caller may write
// through, as a map's at helper does: a pool is owned, so no variable
// holding one is const in C, and the checker lets only a mutable pool's
// element be written.
//
//	static tack_pool_slot_9 *tack_pool_find_9(const tack_pool_9 *p, tack_handle_ h) {
//	    if (h.tag != p->tag || (int64_t)h.index >= p->len) {
//	        return 0;
//	    }
//	    tack_pool_slot_9 *s = &p->ptr[h.index];
//	    return s->live && s->gen == h.gen ? s : 0;
//	}
//
//	static int64_t *tack_pool_at_9(const tack_pool_9 *p, tack_handle_ h) {
//	    return &p->ptr[h.index].value;
//	}
//
// A pool before its first insert has no slots, so find reads no buffer.
func (g *generator) emitPoolLookups(typ types.Type) {
	p, slot, elem := g.poolName(typ), g.poolSlotName(typ), g.cType(g.prog.Types.Elem(typ))
	find, at := g.poolHelper("find", typ), g.poolHelper("at", typ)
	fmt.Fprintf(&g.buf, "%sstatic %s *%s(const %s *p, %s h) {\n"+
		"    if (h.tag != p->tag || (int64_t)h.index >= p->len) {\n        return 0;\n    }\n"+
		"    %s *s = &p->ptr[h.index];\n"+
		"    return s->live && s->gen == h.gen ? s : 0;\n}\n\n",
		g.maybeUnused(find), slot, find, p, handleType, slot)
	fmt.Fprintf(&g.buf, "%sstatic %s *%s(const %s *p, %s h) {\n"+
		"    return &p->ptr[h.index].value;\n}\n\n",
		g.maybeUnused(at), elem, at, p, handleType)
}

// emitPoolInsert writes the insert helper of the pool type typ, which
// takes its value by value, moved in when the element type is owned, and
// frees it when the pool can take it nowhere: no slot is free and the
// pool cannot grow, past its limit or out of memory. The pool is changed
// only once a slot is found, so a failed insert leaves it as it was.
//
//	static tack_pool_inserted_ tack_pool_insert_9(tack_pool_9 *p, int64_t v) {
//	    uint32_t i;
//	    if (p->free != 0) {
//	        i = p->free - 1;
//	        p->free = p->ptr[i].next;
//	    } else if (tack_pool_room_(p->len) && TACK_LIST_RESERVE(p, 1)) {
//	        i = (uint32_t)p->len;
//	        p->ptr[i].gen = tack_pool_gen0_();
//	        p->len++;
//	    } else {
//	        return (tack_pool_inserted_){{0, 0, 0}, TACK_ERROR(1, "out of memory")};
//	    }
//	    tack_pool_slot_9 *s = &p->ptr[i];
//	    s->value = v;
//	    s->next = 0;
//	    s->live = true;
//	    p->count++;
//	    return (tack_pool_inserted_){{i, s->gen, p->tag}, TACK_NO_ERROR};
//	}
func (g *generator) emitPoolInsert(typ types.Type) {
	elemType := g.prog.Types.Elem(typ)
	p, slot, elem := g.poolName(typ), g.poolSlotName(typ), g.cType(elemType)
	insert := g.poolHelper("insert", typ)
	fmt.Fprintf(&g.buf, "%sstatic tack_pool_inserted_ %s(%s *p, %s v) {\n"+
		"    uint32_t i;\n"+
		"    if (p->free != 0) {\n        i = p->free - 1;\n        p->free = p->ptr[i].next;\n"+
		"    } else if (tack_pool_room_(p->len) && TACK_LIST_RESERVE(p, 1)) {\n"+
		"        i = (uint32_t)p->len;\n        p->ptr[i].gen = tack_pool_gen0_();\n        p->len++;\n"+
		"    } else {\n",
		g.maybeUnused(insert), insert, p, elem)
	g.writeFrees("        ", elemType, "v")
	fmt.Fprintf(&g.buf, "        return (tack_pool_inserted_){{0, 0, 0}, TACK_ERROR(%d, %q)};\n    }\n",
		outOfMemoryCode, outOfMemory)
	fmt.Fprintf(&g.buf, "    %s *s = &p->ptr[i];\n    s->value = v;\n    s->next = 0;\n    s->live = true;\n"+
		"    p->count++;\n    return (tack_pool_inserted_){{i, s->gen, p->tag}, TACK_NO_ERROR};\n}\n\n",
		slot)
}

// emitPoolTake writes the take helper of the pool type typ, which moves
// the element of s, a live slot of p, out as its result, leaves the slot
// zeroed and not live, and puts it on the free list with its generation
// moved on; a slot at the last generation, 2^32 - 1, is retired instead,
// left off the free list, so its generation never wraps to one a handle
// already holds.
//
//	static int64_t tack_pool_take_9(tack_pool_9 *p, tack_pool_slot_9 *s) {
//	    int64_t v = s->value;
//	    s->value = 0;
//	    s->live = false;
//	    p->count--;
//	    if (s->gen != 4294967295u) {
//	        s->gen++;
//	        s->next = p->free;
//	        p->free = (uint32_t)(s - p->ptr) + 1;
//	    }
//	    return v;
//	}
func (g *generator) emitPoolTake(typ types.Type) {
	elemType := g.prog.Types.Elem(typ)
	p, slot, elem := g.poolName(typ), g.poolSlotName(typ), g.cType(elemType)
	take := g.poolHelper("take", typ)
	fmt.Fprintf(&g.buf, "%sstatic %s %s(%s *p, %s *s) {\n"+
		"    %s v = s->value;\n    s->value = %s;\n    s->live = false;\n    p->count--;\n"+
		"    if (s->gen != 4294967295u) {\n        s->gen++;\n        s->next = p->free;\n"+
		"        p->free = (uint32_t)(s - p->ptr) + 1;\n    }\n    return v;\n}\n\n",
		g.maybeUnused(take), elem, take, p, slot, elem, g.zeroValue(elemType))
}

// emitPoolFree writes tack_free_ for the pool type typ: it frees each live
// element, last first, when the element type is owned, then the slots'
// buffer. A moved pool is zeroed, with no slots, so freeing it does
// nothing.
//
//	static void tack_free_9(const tack_pool_9 *v) {
//	    tack_list_free_(v->ptr, v->cap, sizeof (tack_pool_slot_9), _Alignof (tack_pool_slot_9), v->alloc);
//	}
func (g *generator) emitPoolFree(typ types.Type) {
	free, slot := g.freeName(typ), g.poolSlotName(typ)
	elemType := g.prog.Types.Elem(typ)
	fmt.Fprintf(&g.buf, "%sstatic void %s(const %s *v) {\n", g.maybeUnused(free), free, g.poolName(typ))
	if g.owned(elemType) {
		g.buf.WriteString("    for (int64_t i = v->len; i > 0; i--) {\n" +
			"        if (v->ptr[i - 1].live) {\n")
		g.writeFrees("            ", elemType, "v->ptr[i - 1].value")
		g.buf.WriteString("        }\n    }\n")
	}
	fmt.Fprintf(&g.buf,
		"    tack_list_free_(v->ptr, v->cap, sizeof (%s), _Alignof (%s), v->alloc);\n}\n\n", slot, slot)
}

// makePool renders make(pool[T], alloc) as a pool with no slots holding
// the allocator and a tag of its own, which allocates nothing and cannot
// fail.
func (g *generator) makePool(call *ast.Call, alloc string) string {
	return fmt.Sprintf("((%s){.alloc = %s, .tag = tack_pool_tag_()})", g.poolName(call.Type), alloc)
}

// poolElementExpr lowers p[h], ix, read where h is proven live in p, p and
// h evaluated in order, to the element the pool type's at helper gives
// the address of (see emitPoolLookups).
func (g *generator) poolElementExpr(ix *ast.Index) (string, []diag.Diagnostic) {
	base, diags := g.expr(ix.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	h, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	at := g.poolHelper("at", ast.TypeOf(ix.X))
	return g.sequenced([]ast.Expr{ix.X, ix.Index}, []string{"(" + base + ")", h},
		func(ops []string) string {
			return fmt.Sprintf("(*%s(&%s, %s))", at, ops[0], ops[1])
		}), nil
}

// poolElementRef renders p[h], ix, assigned or passed as mut where h is
// proven live in p, as a C lvalue: the element the pool type's at helper
// gives the address of in base, the pool rendered as a target (see
// targetRef), index being its handle rendered.
func (g *generator) poolElementRef(ix *ast.Index, base, index string) string {
	return fmt.Sprintf("(*%s(&%s, %s))", g.poolHelper("at", ast.TypeOf(ix.X)), base, index)
}

// poolHandleSource returns the temporaries a lookup of the handle h in
// the pool p holds, in order: p's address, a pointer to it, to a mutable
// pool when mut is set; the handle; and the slot it names, or a null
// pointer, found once.
func (g *generator) poolHandleSource(p, h ast.Expr, mut bool) (multiSource, []diag.Diagnostic) {
	steps, place, diags := g.exprSteps(p, false)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	handle, diags := g.expr(h, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	typ := ast.TypeOf(p)
	find := g.poolHelper("find", typ)
	return multiSource{temps: []multiTemp{
		{typ: g.poolName(typ), init: withSteps(steps, "&"+place), ref: !mut, mut: mut},
		{typ: handleType, init: handle},
		{typ: g.poolSlotName(typ), mut: true, initFrom: func(r []string) string {
			return fmt.Sprintf("%s(%s, %s)", find, r[0], r[1])
		}},
	}}, nil
}

// poolLookupValues lowers v, ok := p[h], for a copyable element: the
// pool's address is held, then the handle, then its slot, and the element
// is read only when there is one, the element type's zero value given
// otherwise.
func (g *generator) poolLookupValues(ix *ast.Index) (multiSource, []diag.Diagnostic) {
	src, diags := g.poolHandleSource(ix.X, ix.Index, false)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	zero := g.zeroValue(ix.Type)
	src.field = func(tmps []string, i int) string {
		if i == 1 {
			return tmps[2] + " != 0"
		}
		return fmt.Sprintf("(%s != 0 ? %s->value : %s)", tmps[2], tmps[2], zero)
	}
	return src, nil
}

// poolTakeValues lowers v, ok := remove(mut p, h): the pool's address is
// held in a temporary, then the handle, then the slot it names, found
// once, and the element, taken out only when there is one, the element
// type's zero value otherwise, which, for a type with none, holds nothing
// to free and is never read; ok is whether there is one.
func (g *generator) poolTakeValues(call *ast.Call) (multiSource, []diag.Diagnostic) {
	p := call.Args[0].(*ast.MutArg).X
	src, diags := g.poolHandleSource(p, call.Args[1], true)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	take, zero := g.poolHelper("take", ast.TypeOf(p)), g.zeroValue(call.Type)
	// The element is taken into a temporary of its own, so the remove is
	// made even when it is bound to _ and never read.
	src.temps = append(src.temps, multiTemp{typ: g.cType(call.Type), initFrom: func(r []string) string {
		return fmt.Sprintf("%s != 0 ? %s(%s, %s) : %s", r[2], take, r[0], r[2], zero)
	}})
	src.field = func(tmps []string, i int) string {
		if i == 1 {
			// Read beside ok, so an element bound to _ is not an unused C
			// variable.
			return fmt.Sprintf("((void)%s, %s != 0)", tmps[3], tmps[2])
		}
		return tmps[3]
	}
	return src, nil
}

// dropPoolRemove lowers remove(mut p, h) written as a statement: the
// temporaries poolTakeValues holds, then, only when h names a live
// element, the element taken out and dropped, freed when owned.
func (g *generator) dropPoolRemove(call *ast.Call) ([]string, []diag.Diagnostic) {
	p := call.Args[0].(*ast.MutArg).X
	src, diags := g.poolHandleSource(p, call.Args[1], true)
	if len(diags) > 0 {
		return nil, diags
	}
	names, lines := g.holdTemps(src)
	take := fmt.Sprintf("%s(%s, %s)", g.poolHelper("take", ast.TypeOf(p)), names[0], names[2])
	body := []string{"(void)" + take + ";"}
	if g.owned(call.Type) {
		old := g.temp("r")
		body = append([]string{fmt.Sprintf("const %s %s = %s;", g.cType(call.Type), old, take)},
			g.frees(call.Type, old)...)
	}
	lines = append(lines, fmt.Sprintf("if (%s != 0) {", names[2]))
	return append(append(lines, indent(body)...), "}"), nil
}

// poolHasValue lowers has(p, h), p and h evaluated in order, to whether
// the pool type's find helper finds a slot for h in p, on p's address. The
// checker makes p a place, since a pool that is a temporary has no owner.
func (g *generator) poolHasValue(call *ast.Call) (string, []diag.Diagnostic) {
	p, h := call.Args[0], call.Args[1]
	base, diags := g.expr(p, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	handle, diags := g.expr(h, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	find := g.poolHelper("find", ast.TypeOf(p))
	return g.sequenced([]ast.Expr{p, h}, []string{"(" + base + ")", handle}, func(ops []string) string {
		return fmt.Sprintf("(%s(&%s, %s) != 0)", find, ops[0], ops[1])
	}), nil
}

// insertValues lowers h, err := insert(mut p, v), the source of a
// MultiVar or a MultiAssign, or of a ?: the call to the pool's insert
// helper, held as its results struct and read field by field (see
// insertExpr).
func (g *generator) insertValues(call *ast.Call) (multiSource, []diag.Diagnostic) {
	x, diags := g.insertExpr(call)
	return multiSource{
		temps: []multiTemp{{typ: "tack_pool_inserted_", init: x}},
		field: func(tmps []string, i int) string { return fmt.Sprintf("%s.r%d", tmps[0], i) },
	}, diags
}

// insertExpr renders a call to insert as a call to the pool's insert
// helper on the address of the pool's place and the value, evaluated in
// order, the place first, as append's operands are (see appendOperands).
func (g *generator) insertExpr(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "insert's pool must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	rendered, ctypes, diags := g.appendOperands(call, m)
	if len(diags) > 0 {
		return "", diags
	}
	insert := g.poolHelper("insert", ast.TypeOf(m.X))
	return g.sequencedAs(call.Args, rendered, ctypes, func(ops []string) string {
		return fmt.Sprintf("%s(%s, %s)", insert, ops[0], ops[1])
	}), nil
}

// poolRange lowers a range loop over a pool, whose lowered address is
// addr, a place, since the checker gives a temporary pool no range, and
// lowered body body: every slot in order, skipping those not live, each
// live one's handle made from its index, its generation and the pool's
// tag, and its element bound in place when its type is owned, and copied
// otherwise. The body cannot change the pool while the loop runs (see
// typeck's move.go), so neither its slots nor its count move:
//
//	{
//	    const tack_pool_9 *const tack_r0 = &v_p;
//	    for (int64_t tack_k1 = 0;; tack_k1++) {
//	        if (!(tack_k1 < tack_r0->len)) {
//	            break;
//	        }
//	        if (!tack_r0->ptr[tack_k1].live) {
//	            continue;
//	        }
//	        const tack_handle_ v_h = {(uint32_t)tack_k1, tack_r0->ptr[tack_k1].gen, tack_r0->tag};
//	        const int64_t v_x = tack_r0->ptr[tack_k1].value;
//	        body
//	    }
//	}
func (g *generator) poolRange(loop *ast.ForRange, addr string, body []string) []string {
	typ := ast.TypeOf(loop.X)
	r, k := g.temp("r"), g.temp("k")
	slot := fmt.Sprintf("%s->ptr[%s]", r, k)
	vars := []string{fmt.Sprintf("if (!%s.live) {", slot), "    continue;", "}"}
	if loop.Var != nil && loop.Var.Name != ast.Blank {
		vars = append(vars, fmt.Sprintf("const %s %s = {(uint32_t)%s, %s.gen, %s->tag};",
			handleType, declName(loop.Var), k, slot, r))
	}
	switch {
	case g.bindsInPlace(loop):
		vars = append(vars, fmt.Sprintf("const %s *const %s = &%s.value;",
			g.cType(loop.Elem.Type), declName(loop.Elem), slot))
	case bindsElem(loop):
		vars = append(vars, fmt.Sprintf("const %s %s = %s.value;",
			g.cType(loop.Elem.Type), declName(loop.Elem), slot))
	}
	eval := fmt.Sprintf("const %s *const %s = %s;", g.poolName(typ), r, addr)
	return rangeBlock(eval, "int64_t", k, r+"->len", append(vars, body...))
}

// isPoolRemove reports whether call calls remove on a pool, remove(mut p,
// h), rather than on a list.
func (g *generator) isPoolRemove(call *ast.Call) bool {
	if !isBuiltin(call, "remove") || len(call.Args) != 2 {
		return false
	}
	m, ok := call.Args[0].(*ast.MutArg)
	return ok && g.prog.Types.IsPool(ast.TypeOf(m.X))
}

// handleComparison renders == or != on two handles, l and r, as
// tack_handle_eq_, parenthesized as every comparison is.
func handleComparison(op ast.BinaryOp, l, r string) string {
	eq := fmt.Sprintf("tack_handle_eq_(%s, %s)", l, r)
	if op == ast.Ne {
		return "(!" + eq + ")"
	}
	return "(" + eq + ")"
}

package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A list, list[T], lowers to a struct of its own per element type, like a
// view's, holding a pointer to its buffer, its length, its capacity and
// the allocator it grows and is freed through:
//
//	typedef struct {
//	    int64_t *ptr;
//	    int64_t len;
//	    int64_t cap;
//	    tack_allocator alloc;
//	} tack_list_17;
//
// An empty list holds a null pointer, a length and a capacity of 0 and its
// allocator; make writes one, which allocates nothing. append lowers to
// std's TACK_LIST_APPEND, which grows the buffer through
// tack_list_grow_ when it is full, and gives false, the list unchanged,
// when the allocator cannot, which append turns into its error. An
// element is read and written as a view's is, x.ptr[i], and a view of a
// list is its pointer and its length.
//
// A list is freed by tack_list_free_, std's, given its buffer, capacity,
// element size and alignment and allocator; it does nothing for a null
// buffer. A struct holding lists frees each, last field first, at any
// depth, and a list whose elements are owned frees each element it holds,
// last first, through a tack_free_ of its own (see emitListFree), before
// its buffer. pop and remove take an element out, so the list no longer
// frees it, through std's TACK_LIST_POP and TACK_LIST_REMOVE. A local of an owned type is freed when its block ends, by an
// entry its declaration adds to the block's cleanup chain, beside its
// defers (see defer.go): so frees and defers run last first, by position,
// at every exit from the block, and at the end of every iteration of a
// loop's body. Assigning a variable of an owned type frees what it held
// once the new value has been computed, which may read the old one.
//
// A move, a name the type checker marked (ast.Ident.Move), takes the
// value and empties the variable, all of it zeroed, through a temporary,
// (tack_o3 = v_l, v_l = (tack_list_17){}, tack_o3), so a later free of
// it, at its block's end or when it is assigned, does nothing. That is
// what makes a conditional move free right on every path without a flag:
// the free at the end is unconditional, and finds either the list or
// nothing.

// emitListDef defines the C struct a list type lowers to.
func (g *generator) emitListDef(typ types.Type) {
	fmt.Fprintf(
		&g.buf,
		"typedef struct {\n    %s *ptr;\n    int64_t len;\n    int64_t cap;\n    tack_allocator alloc;\n} %s;\n\n",
		g.cType(g.prog.Types.Elem(typ)),
		g.listName(typ),
	)
}

// listName is the name of the C struct a list type lowers to, made from
// its ID as a view's is, tack_list_ and a digit.
func listNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_list_%d", typ)
}

// outOfMemory is the message of the error append and reserve give when
// the allocator cannot grow the list, or the length it would have
// overflows.
const outOfMemory = "out of memory"

// outOfMemoryCode is the code of that error.
const outOfMemoryCode = 1

// negativeCount is the message of the error reserve gives for a negative
// count.
const negativeCount = "negative count"

// negativeCountCode is the code of that error.
const negativeCountCode = 2

// listErrors returns the messages of the errors the program's calls to
// append, reserve and put can give, so they are pooled: out of memory for
// any, and negative count for a reserve whose count is not a constant,
// which the checker has made sure is not negative.
func (g *generator) listErrors() []string {
	grows, negative := false, false
	g.walk(func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok {
			reserves := isBuiltin(call, "reserve")
			grows = grows || reserves || isBuiltin(call, "append") || isBuiltin(call, "put")
			negative = negative || reserves && !g.constCount(call)
		}
		return !negative
	})
	switch {
	case negative:
		return []string{outOfMemory, negativeCount}
	case grows:
		return []string{outOfMemory}
	default:
		return nil
	}
}

// constCount reports whether the count of call, a call to reserve, is a
// constant, which the checker has made sure is not negative.
func (g *generator) constCount(call *ast.Call) bool {
	_, ok := g.literal(call.Args[1])
	return ok
}

// isBuiltin reports whether call calls the generic builtin named name,
// which, like every builtin, has a nil declaration.
func isBuiltin(call *ast.Call, name string) bool {
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != name || id.Pkg != "" {
		return false
	}
	fn, ok := id.Def.(*ast.Func)
	return ok && fn == nil
}

// makeValue renders make(list[T], alloc) as an empty list of its type
// holding the allocator, which allocates nothing, and make(map[K]V,
// alloc) as an empty map (see makeMap).
func (g *generator) makeValue(call *ast.Call) (string, []diag.Diagnostic) {
	alloc, diags := g.expr(call.Args[1], types.Allocator)
	if len(diags) > 0 {
		return "", diags
	}
	if g.prog.Types.IsMap(call.Type) {
		return g.makeMap(call, alloc), nil
	}
	return fmt.Sprintf("((%s){.alloc = %s})", g.listName(call.Type), alloc), nil
}

// listError renders the error code and message, a pooled string, make.
func (g *generator) listError(code int, msg string) string {
	label := g.strLabel(msg)
	return fmt.Sprintf("(tack_error){%d, {%s, TACK_C_STR_LEN(%s)}}", code, label, label)
}

// appendValue renders err := append(mut l, ...) as one of std's macros on
// the address of l's place, its result as an error: none, or code 1 with
// the message "out of memory" when the buffer could not grow, which
// leaves the list as it was. One value is TACK_LIST_APPEND(lp, v); any
// other number, none included, is TACK_LIST_APPEND_N(lp, src, n) of a
// compound literal array of them, (const T[n]){a, b, c}, or a null
// pointer for none; and a spread view is held in a temporary, so it is
// evaluated once, and TACK_LIST_APPEND_N given its pointer and length.
// The place and the values are operands evaluated left to right (see
// sequenced), the place first.
func (g *generator) appendValue(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "append's list must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	rendered, ctypes, diags := g.appendOperands(call, m)
	if len(diags) > 0 {
		return "", diags
	}
	elem := g.prog.Types.Elem(ast.TypeOf(m.X))
	if g.owned(elem) && len(call.Args) > 1 {
		return g.appendOwned(call, elem, rendered), nil
	}
	build := func(ops []string) string {
		return g.appendBuild(call, elem, ops)
	}
	return fmt.Sprintf("(%s ? %s : %s)",
		g.sequencedAs(call.Args, rendered, ctypes, build),
		g.zeroValue(types.Error), g.listError(outOfMemoryCode, outOfMemory)), nil
}

// appendOwned renders err := append(mut l, a, b) onto a list of the owned
// type elem, whose operands are rendered as rendered: the list's address,
// then each value, each moved, which a spread never is. Each value is
// held in a temporary of its own, in order after the list's address, so
// that when the buffer cannot grow, the values, which no variable holds
// any longer, are freed, last first, as a list's elements are, before the
// error is given:
//
//	(tack_o1 = &v_l, tack_o2 = <a>, TACK_LIST_APPEND(tack_o1, tack_o2)
//	    ? noerror : (tack_free_9(tack_o2), <out of memory>))
func (g *generator) appendOwned(call *ast.Call, elem types.Type, rendered []string) string {
	ctype := g.cType(elem)
	lp := rendered[0]
	steps := make([]string, 0, len(rendered))
	if !g.pure(call.Args[0]) {
		t := g.temp("o")
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.operandType(call.Args[0]), t))
		steps = append(steps, fmt.Sprintf("%s = %s", t, lp))
		lp = t
	}
	values := make([]string, 0, len(rendered)-1)
	for _, v := range rendered[1:] {
		t := g.temp("o")
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", ctype, t))
		steps = append(steps, fmt.Sprintf("%s = %s", t, v))
		values = append(values, t)
	}
	grow := fmt.Sprintf("TACK_LIST_APPEND(%s, %s)", lp, values[0])
	if len(values) > 1 {
		grow = fmt.Sprintf("TACK_LIST_APPEND_N(%s, ((const %s[%d]){%s}), %d)",
			lp, ctype, len(values), strings.Join(values, ", "), len(values))
	}
	var frees []string
	for i := len(values) - 1; i >= 0; i-- {
		for _, line := range g.frees(elem, values[i]) {
			frees = append(frees, strings.TrimSuffix(line, ";"))
		}
	}
	frees = append(frees, g.listError(outOfMemoryCode, outOfMemory))
	steps = append(steps,
		fmt.Sprintf("(%s ? %s : (%s))", grow, g.zeroValue(types.Error), strings.Join(frees, ", ")))
	return "(" + strings.Join(steps, ", ") + ")"
}

// appendOperands renders the arguments of call, a call to append whose
// list m is, and gives the C type of a temporary holding each: the
// list's address, then each value as the element type, or the view a
// spread gives (see viewArg).
func (g *generator) appendOperands(
	call *ast.Call,
	m *ast.MutArg,
) ([]string, []string, []diag.Diagnostic) {
	place, diags := g.mutArg(m)
	if len(diags) > 0 {
		return nil, nil, diags
	}
	rendered, ctypes := []string{place}, []string{g.operandType(m)}
	elem := g.prog.Types.Elem(ast.TypeOf(m.X))
	guard := g.guardOwned(call.Args)
	defer guard.done()
	for i, arg := range call.Args[1:] {
		var v, ctype string
		if call.Spread {
			view := g.prog.Types.View(elem)
			v, diags = g.viewArg(arg, view, call)
			ctype = g.viewName(view)
		} else {
			v, diags = g.expr(arg, elem)
			ctype = g.operandType(arg)
		}
		if len(diags) > 0 {
			return nil, nil, diags
		}
		rendered, ctypes = append(rendered, guard.hold(i+1, v)), append(ctypes, ctype)
	}
	return rendered, ctypes, nil
}

// appendBuild renders the macro call append lowers to, given its
// operands, ops, evaluated in order already where that shows: the list's
// address, then the values or the spread view, of a list of elem.
func (g *generator) appendBuild(call *ast.Call, elem types.Type, ops []string) string {
	lp, values := ops[0], ops[1:]
	switch {
	case call.Spread:
		t := g.temp("v")
		view := g.prog.Types.View(elem)
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.viewName(view), t))
		return fmt.Sprintf(
			"(%s = %s, TACK_LIST_APPEND_N(%s, %s.ptr, %s.len))",
			t,
			values[0],
			lp,
			t,
			t,
		)
	case len(values) == 1:
		return fmt.Sprintf("TACK_LIST_APPEND(%s, %s)", lp, values[0])
	case len(values) == 0:
		return fmt.Sprintf("TACK_LIST_APPEND_N(%s, 0, 0)", lp)
	default:
		return fmt.Sprintf("TACK_LIST_APPEND_N(%s, ((const %s[%d]){%s}), %d)",
			lp, g.cType(elem), len(values), strings.Join(values, ", "), len(values))
	}
}

// reserveValue renders err := reserve(mut l, n) as std's
// TACK_LIST_RESERVE on the address of l's place, after n is found not to
// be negative, which is the error code 2 with the message "negative
// count", and the list left as it was. Its result is otherwise an error
// as append's is: none, or code 1, "out of memory", when the buffer could
// not grow or len + n overflows, which leaves the list as it was. n is
// read twice, so it is held in a temporary first unless it is pure; a
// constant n, which the checker has made sure is not negative, is not
// tested.
func (g *generator) reserveValue(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "reserve's list must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	place, diags := g.mutArg(m)
	if len(diags) > 0 {
		return "", diags
	}
	n, diags := g.expr(call.Args[1], types.Int64)
	if len(diags) > 0 {
		return "", diags
	}
	none, oom := g.zeroValue(types.Error), g.listError(outOfMemoryCode, outOfMemory)
	build := func(ops []string) string {
		if g.constCount(call) {
			return fmt.Sprintf("(TACK_LIST_RESERVE(%s, %s) ? %s : %s)", ops[0], ops[1], none, oom)
		}
		count, hold := ops[1], ""
		if !g.pure(call.Args[1]) {
			count = g.temp("n")
			g.frame.temps = append(g.frame.temps, fmt.Sprintf("int64_t %s;", count))
			hold = fmt.Sprintf("%s = %s, ", count, ops[1])
		}
		return fmt.Sprintf("(%s%s < 0 ? %s : TACK_LIST_RESERVE(%s, %s) ? %s : %s)",
			hold, count, g.listError(negativeCountCode, negativeCount), ops[0], count, none, oom)
	}
	return g.sequencedAs(call.Args, []string{place, n},
		[]string{g.operandType(m), "int64_t"}, build), nil
}

// popValue renders x := pop(mut l), the plain form, as std's
// TACK_LIST_POP on the address of l's place: the last element, which it
// takes out of the list. The checker has proven the list holds one.
func (g *generator) popValue(call *ast.Call) (string, []diag.Diagnostic) {
	lp, diags := g.takeList(call)
	if len(diags) > 0 {
		return "", diags
	}
	return fmt.Sprintf("TACK_LIST_POP(%s)", lp), nil
}

// removeValue renders x := remove(mut l, i), the plain form, as std's
// TACK_LIST_REMOVE on the address of l's place and i, an int64: element
// i, which it takes out of the list, moving the rest down. The checker
// has proven i in range. The macro evaluates the place first, then i.
func (g *generator) removeValue(call *ast.Call) (string, []diag.Diagnostic) {
	lp, diags := g.takeList(call)
	if len(diags) > 0 {
		return "", diags
	}
	i, diags := g.expr(call.Args[1], types.Int64)
	if len(diags) > 0 {
		return "", diags
	}
	return fmt.Sprintf("TACK_LIST_REMOVE(%s, %s)", lp, i), nil
}

// takeList renders the address of the list call, a call to pop or
// remove, takes an element of, its first argument, a mut argument.
func (g *generator) takeList(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		id, _ := call.Fn.(*ast.Ident)
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  id.Name + "'s list must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	return g.mutArg(m)
}

// takeValues lowers x, ok := pop(mut l) or x, ok := remove(mut l, i), the
// checked form: the list's address is held in a temporary, then remove's
// index, as an int64, then whether the list holds what is taken, which
// is read before anything changes, then the element, taken only when it
// does, and the element type's zero value otherwise, which, for a type
// with none, holds nothing to free and is never read:
//
//	tack_list_9 *const tack_r0 = &v_l;
//	const bool tack_r1 = tack_r0->len > 0;
//	const int64_t tack_r2 = tack_r1 ? TACK_LIST_POP(tack_r0) : 0;
func (g *generator) takeValues(call *ast.Call) (multiSource, []diag.Diagnostic) {
	lp, diags := g.takeList(call)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	m, _ := call.Args[0].(*ast.MutArg)
	elem, zero := g.cType(call.Type), g.zeroValue(call.Type)
	src := multiSource{temps: []multiTemp{{typ: g.cType(ast.TypeOf(m.X)), init: lp, mut: true}}}
	if isBuiltin(call, "pop") {
		src.temps = append(src.temps,
			multiTemp{typ: "bool", initFrom: func(r []string) string {
				return r[0] + "->len > 0"
			}},
			multiTemp{typ: elem, initFrom: func(r []string) string {
				return fmt.Sprintf("%s ? TACK_LIST_POP(%s) : %s", r[1], r[0], zero)
			}})
		src.field = func(tmps []string, i int) string { return tmps[2-i] }
		return src, nil
	}
	i, diags := g.expr(call.Args[1], types.Int64)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	src.temps = append(src.temps,
		multiTemp{typ: "int64_t", init: i},
		multiTemp{typ: "bool", initFrom: func(r []string) string {
			return fmt.Sprintf("%s >= 0 && %s < %s->len", r[1], r[1], r[0])
		}},
		multiTemp{typ: elem, initFrom: func(r []string) string {
			return fmt.Sprintf("%s ? TACK_LIST_REMOVE(%s, %s) : %s", r[2], r[0], r[1], zero)
		}})
	src.field = func(tmps []string, i int) string { return tmps[3-i] }
	return src, nil
}

// dropTake lowers pop(mut l) or remove(mut l, i) written as a statement,
// which needs no proof: as the checked form does (see takeValues), it
// holds the list's address, then remove's index, then whether the list
// holds what is taken, and only when it does takes the element and drops
// it, freeing it when its type is owned:
//
//	tack_list_9 *const tack_r0 = &v_l;
//	const bool tack_r1 = tack_r0->len > 0;
//	if (tack_r1) {
//	    const tack_list_8 tack_r2 = TACK_LIST_POP(tack_r0);
//	    tack_list_free_(tack_r2.ptr, ...);
//	}
func (g *generator) dropTake(call *ast.Call) ([]string, []diag.Diagnostic) {
	src, diags := g.takeValues(call)
	if len(diags) > 0 {
		return nil, diags
	}
	// Every temporary but the last, the element, which is taken only
	// under the test.
	src.temps = src.temps[:len(src.temps)-1]
	names, lines := g.holdTemps(src)
	take := fmt.Sprintf("TACK_LIST_POP(%s)", names[0])
	if isBuiltin(call, "remove") {
		take = fmt.Sprintf("TACK_LIST_REMOVE(%s, %s)", names[0], names[1])
	}
	body := []string{"(void)" + take + ";"}
	if g.owned(call.Type) {
		elem := g.temp("r")
		body = append(
			[]string{fmt.Sprintf("const %s %s = %s;", g.cType(call.Type), elem, take)},
			g.frees(call.Type, elem)...)
	}
	lines = append(lines, fmt.Sprintf("if (%s) {", names[len(names)-1]))
	return append(append(lines, indent(body)...), "}"), nil
}

// moveValue renders a name the type checker marked as moved, whose
// variable or field place is ref, as its value taken through a temporary,
// with the variable emptied: (tack_o3 = ref, ref = (T){}, tack_o3). In
// an instance of a generic function, a value of a type parameter is
// moved, since the type checker cannot know whether it owns a list; when
// its type argument owns none, nothing frees it, so the move is a copy,
// ref itself, and the variable, which nothing reads again, is left as it
// is.
func (g *generator) moveValue(typ types.Type, ref string) string {
	if !g.owned(typ) {
		return ref
	}
	t := g.temp("o")
	ctype := g.cType(typ)
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", ctype, t))
	return fmt.Sprintf("(%s = %s, %s = (%s){}, %s)", t, ref, ref, ctype, t)
}

// frees returns the C statements, each ending in a semicolon, that free
// the value of the owned type typ held at the place ref: a list's buffer,
// through its allocator, after each element it holds when they are owned,
// through the list's tack_free_ (see emitListFree), each list a struct
// holds, last field first, at any depth, or what the case a union holds
// holds, through its tack_free_ (see union.go). Each is a call, an
// expression, so they can be joined into a loop's post statement.
func (g *generator) frees(typ types.Type, ref string) []string {
	table := g.prog.Types
	if table.IsUnion(typ) || table.IsMap(typ) || table.IsList(typ) && g.owned(table.Elem(typ)) {
		return []string{fmt.Sprintf("%s(&%s);", g.freeName(typ), ref)}
	}
	if table.IsList(typ) {
		elem := g.cType(table.Elem(typ))
		return []string{fmt.Sprintf(
			"tack_list_free_(%s.ptr, %s.cap, sizeof (%s), _Alignof (%s), %s.alloc);",
			ref, ref, elem, elem, ref)}
	}
	var lines []string
	fields := table.Fields(typ)
	for i := len(fields) - 1; i >= 0; i-- {
		f := fields[i]
		if table.Owned(f.Type) {
			lines = append(lines, g.frees(f.Type, ref+"."+memberName(typ, f.Name))...)
		}
	}
	return lines
}

// emitListFree writes tack_free_ for the list type typ, whose elements are
// owned: it frees each element the list holds, last first, as a struct's
// fields are freed last first, then the buffer. An element popped or
// removed is past the list's length, or moved down over, so it is never
// freed here; a moved list is zeroed, its length 0 and its buffer null,
// so freeing it does nothing.
//
//	static void tack_free_9(const tack_list_9 *v) {
//	    for (int64_t i = v->len; i > 0; i--) {
//	        tack_list_free_(v->ptr[i - 1].ptr, ...);
//	    }
//	    tack_list_free_(v->ptr, v->cap, sizeof (tack_list_8), _Alignof (tack_list_8), v->alloc);
//	}
func (g *generator) emitListFree(typ types.Type) {
	table := g.prog.Types
	elem := table.Elem(typ)
	free := g.freeName(typ)
	fmt.Fprintf(
		&g.buf,
		"%sstatic void %s(const %s *v) {\n",
		g.maybeUnused(free),
		free,
		g.listName(typ),
	)
	g.buf.WriteString("    for (int64_t i = v->len; i > 0; i--) {\n")
	for _, line := range g.frees(elem, "v->ptr[i - 1]") {
		fmt.Fprintf(&g.buf, "        %s\n", line)
	}
	ctype := g.cType(elem)
	fmt.Fprintf(
		&g.buf,
		"    }\n    tack_list_free_(v->ptr, v->cap, sizeof (%s), _Alignof (%s), v->alloc);\n}\n\n",
		ctype,
		ctype,
	)
}

// ownedAssign renders an assignment of value, already rendered, to target,
// a place of the owned type typ, rendered as ref: the value is computed
// first, into a temporary, since it may read the old one, then the old
// value is freed, then the place takes the new one. Each line is an
// expression ending in a semicolon, so a loop's post statement can join
// them.
func (g *generator) ownedAssign(typ types.Type, ref, value string) []string {
	t := g.temp("o")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.cType(typ), t))
	lines := []string{fmt.Sprintf("%s = %s;", t, value)}
	lines = append(lines, g.frees(typ, ref)...)
	return append(lines, fmt.Sprintf("%s = %s;", ref, t))
}

// freeEntry is one owned local variable, or own parameter, whose value
// its block frees when it ends, as a defer would: see defer.go. Exactly
// one of v and p is set.
type freeEntry struct {
	v *ast.Var
	p *ast.Param
}

// lower returns the statements that free the variable's or the
// parameter's value.
func (e freeEntry) lower(g *generator) []string {
	if e.p != nil {
		return g.frees(e.p.Type, g.paramRef(e.p))
	}
	return g.frees(e.v.Type, g.varRef(e.v))
}

// addParamFrees records, in the innermost block, the body of fn, a free
// for each own parameter of fn that holds an owned value, in order, ahead
// of every statement, so every exit from the body frees it, unless it was
// moved on first, which emptied it. In an instance of a generic function
// whose type argument owns nothing, there is none.
func (g *generator) addParamFrees(fn *ast.Func) {
	sc := g.scopes[len(g.scopes)-1]
	for _, p := range fn.Params {
		if p.Own && g.owned(p.Type) {
			sc.entries = append(sc.entries, cleanup{free: &freeEntry{p: p}})
		}
	}
}

// addFrees records, in the innermost block, a free for each variable of
// vars that holds an owned value, in order, after its declaration.
func (g *generator) addFrees(vars ...*ast.Var) {
	sc := g.scopes[len(g.scopes)-1]
	for _, v := range vars {
		if v.Name != ast.Blank && g.prog.Types.Owned(v.Type) {
			sc.entries = append(sc.entries, cleanup{free: &freeEntry{v: v}})
		}
	}
}

// owned reports whether typ is an owned type, a list or a struct
// holding one, whose variables are not const in C, since a move empties
// them and a free reads them at their block's end.
func (g *generator) owned(typ types.Type) bool {
	return g.prog.Types.Owned(typ)
}

// moves returns whether evaluating e moves a variable's value, which
// changes the variable, so the order it is evaluated in, among the
// operands around it, can show.
func moves(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Move {
			found = true
		}
		return !found
	})
	return found
}

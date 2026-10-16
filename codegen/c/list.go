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
// depth. A local of an owned type is freed when its block ends, by an
// entry its declaration adds to the block's cleanup chain, beside its
// defers (see defer.go): so frees and defers run last first, by position,
// at every exit from the block, and at the end of every iteration of a
// loop's body. Assigning a variable of an owned type frees what it held
// once the new value has been computed, which may read the old one.
//
// A move, a name the type checker marked (ast.Ident.Move), takes the
// value and empties the variable, all of it zeroed, through a temporary,
// (tack_o3 = v_l, v_l = (tack_list_17){0}, tack_o3), so a later free of
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
		listName(typ),
	)
}

// listName is the name of the C struct a list type lowers to, made from
// its ID as a view's is, tack_list_ and a digit.
func listName(typ types.Type) string {
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
// append and reserve can give, so they are pooled: out of memory for
// either, and negative count for a reserve whose count is not a constant,
// which the checker has made sure is not negative.
func (g *generator) listErrors() []string {
	grows, negative := false, false
	g.walk(func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok {
			reserves := isBuiltin(call, "reserve")
			grows = grows || reserves || isBuiltin(call, "append")
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
// holding the allocator, which allocates nothing.
func (g *generator) makeValue(call *ast.Call) (string, []diag.Diagnostic) {
	alloc, diags := g.expr(call.Args[1], types.Allocator)
	if len(diags) > 0 {
		return "", diags
	}
	return fmt.Sprintf("((%s){.alloc = %s})", listName(call.Type), alloc), nil
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
	build := func(ops []string) string {
		return g.appendBuild(call, elem, ops)
	}
	return fmt.Sprintf("(%s ? %s : %s)",
		g.sequencedAs(call.Args, rendered, ctypes, build),
		g.zeroValue(types.Error), g.listError(outOfMemoryCode, outOfMemory)), nil
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
	for _, arg := range call.Args[1:] {
		var v, ctype string
		if call.Spread {
			view := g.prog.Types.View(elem)
			v, diags = g.viewArg(arg, view, call)
			ctype = viewName(view)
		} else {
			v, diags = g.expr(arg, elem)
			ctype = g.operandType(arg)
		}
		if len(diags) > 0 {
			return nil, nil, diags
		}
		rendered, ctypes = append(rendered, v), append(ctypes, ctype)
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
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", viewName(view), t))
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

// moveValue renders a name the type checker marked as moved, whose
// variable or field place is ref, as its value taken through a temporary,
// with the variable emptied: (tack_o3 = ref, ref = (T){0}, tack_o3). In
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
	return fmt.Sprintf("(%s = %s, %s = (%s){0}, %s)", t, ref, ref, ctype, t)
}

// frees returns the C statements, each ending in a semicolon, that free
// the value of the owned type typ held at the place ref: a list's buffer,
// through its allocator, each list a struct holds, last field first, at
// any depth, or what the case a union holds holds, through its tack_free_
// (see union.go). Each is a call, an expression, so they can be joined into
// a loop's post statement.
func (g *generator) frees(typ types.Type, ref string) []string {
	table := g.prog.Types
	if table.IsUnion(typ) {
		return []string{fmt.Sprintf("%s(%s);", freeName(typ), ref)}
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

// freeEntry is one owned local variable whose value its block frees when
// it ends, as a defer would: see defer.go.
type freeEntry struct {
	v *ast.Var
}

// lower returns the statements that free the variable's value.
func (e freeEntry) lower(g *generator) []string {
	return g.frees(e.v.Type, g.varRef(e.v))
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

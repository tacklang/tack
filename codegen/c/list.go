package c

import (
	"fmt"

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

// outOfMemory is the message of the error append gives when the
// allocator cannot grow the list.
const outOfMemory = "out of memory"

// outOfMemoryCode is the code of that error.
const outOfMemoryCode = 1

// usesAppend reports whether the program calls append anywhere, so the
// message of its error is pooled.
func (g *generator) usesAppend() bool {
	found := false
	g.walk(func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok && isBuiltin(call, "append") {
			found = true
		}
		return !found
	})
	return found
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

// appendValue renders err := append(mut l, v) as std's TACK_LIST_APPEND
// on the address of l's place, which evaluates the place and then v once
// each, in that order, and its result as an error: none, or code 1 with
// the message "out of memory" when the buffer could not grow, which
// leaves the list as it was.
func (g *generator) appendValue(call *ast.Call) (string, []diag.Diagnostic) {
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "append's list must be passed as mut",
			Span:     call.Args[0].Spanned(),
		}}
	}
	place, diags := g.mutArg(m)
	if len(diags) > 0 {
		return "", diags
	}
	elem := g.prog.Types.Elem(ast.TypeOf(m.X))
	v, diags := g.expr(call.Args[1], elem)
	if len(diags) > 0 {
		return "", diags
	}
	label := g.strLabel(outOfMemory)
	failed := fmt.Sprintf(
		"(tack_error){%d, {%s, TACK_C_STR_LEN(%s)}}",
		outOfMemoryCode,
		label,
		label,
	)
	return fmt.Sprintf("(TACK_LIST_APPEND(%s, %s) ? %s : %s)",
		place, v, g.zeroValue(types.Error), failed), nil
}

// moveValue renders a name the type checker marked as moved, whose
// variable or field place is ref, as its value taken through a temporary,
// with the variable emptied: (tack_o3 = ref, ref = (T){0}, tack_o3).
func (g *generator) moveValue(typ types.Type, ref string) string {
	t := g.temp("o")
	ctype := g.cType(typ)
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", ctype, t))
	return fmt.Sprintf("(%s = %s, %s = (%s){0}, %s)", t, ref, ref, ctype, t)
}

// frees returns the C statements, each ending in a semicolon, that free
// the value of the owned type typ held at the place ref: a list's buffer,
// through its allocator, or each list a struct holds, last field first,
// at any depth. Each is a call, an expression, so they can be joined into
// a loop's post statement.
func (g *generator) frees(typ types.Type, ref string) []string {
	table := g.prog.Types
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

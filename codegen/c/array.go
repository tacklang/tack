package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// emitArrayDef defines the C struct an array type lowers to, holding its
// elements as a C array, e. A C array cannot be assigned, passed or
// returned, and a struct holding one can, so an array is a value as Tack
// defines it. An array of no elements still holds one, since C has no
// empty array, and is never indexed.
func (g *generator) emitArrayDef(typ types.Type) {
	table := g.prog.Types
	n := table.Len(typ)
	if n == 0 {
		n = 1
	}
	fmt.Fprintf(
		&g.buf,
		"typedef struct {\n    %s e[%d];\n} %s;\n\n",
		g.cType(table.Elem(typ)),
		n,
		arrayName(typ),
	)
}

// emitArrayEquality writes tack_eq_ for an array type, which compares two
// arrays element by element.
func (g *generator) emitArrayEquality(typ types.Type) {
	table := g.prog.Types
	name := arrayName(typ)
	differ := "a.e[i] != b.e[i]"
	if elem := table.Elem(typ); g.isComposite(elem) || elem == types.String {
		differ = "!" + g.equalExpr(elem, "a.e[i]", "b.e[i]")
	}
	fmt.Fprintf(&g.buf, "static bool %s(%s a, %s b) {\n", equalityName(typ), name, name)
	fmt.Fprintf(&g.buf, "    for (int64_t i = 0; i < %d; i++) {\n", table.Len(typ))
	fmt.Fprintf(&g.buf, "        if (%s) {\n            return false;\n        }\n    }\n", differ)
	g.buf.WriteString("    return true;\n}\n\n")
}

// equalityName is the name of the helper that compares two arrays, or two
// structs, of type typ.
func equalityName(typ types.Type) string {
	return fmt.Sprintf("tack_eq_%d", typ)
}

// compositeLit renders an array literal as a C compound literal of its
// struct, its elements in order inside the struct's array, and a struct
// literal as structLit does.
func (g *generator) compositeLit(lit *ast.CompositeLit) (string, []diag.Diagnostic) {
	if g.prog.Types.IsStruct(lit.Type) {
		return g.structLit(lit)
	}
	if len(lit.Elems) == 0 {
		return fmt.Sprintf("(%s){0}", arrayName(lit.Type)), nil
	}
	elems := make([]string, len(lit.Elems))
	for i, e := range lit.Elems {
		var (
			v     string
			diags []diag.Diagnostic
		)
		if inner, ok := e.(*ast.CompositeLit); ok {
			v, diags = g.compositeLit(inner)
		} else {
			v, diags = g.expr(e, types.Invalid)
		}
		if len(diags) > 0 {
			return "", diags
		}
		elems[i] = v
	}
	return g.sequenced(lit.Elems, elems, func(ops []string) string {
		return fmt.Sprintf("(%s){{%s}}", arrayName(lit.Type), strings.Join(ops, ", "))
	}), nil
}

// indexExpr renders an element read, x.e[i], or a string's byte or a
// view's element, x.ptr[i].
// The checker has proven the index in range. x is parenthesized unless it is
// a name, an element, a field or a call, which C's postfix operators
// already bind.
func (g *generator) indexExpr(ix *ast.Index) (string, []diag.Diagnostic) {
	base, diags := g.expr(ix.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	index, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	field := "e"
	if g.hasLen(ast.TypeOf(ix.X)) {
		field = "ptr"
	}
	switch ix.X.(type) {
	case *ast.Ident, *ast.Index, *ast.Selector, *ast.Call:
	default:
		base = "(" + base + ")"
	}
	operands := []ast.Expr{ix.X, ix.Index}
	return g.sequenced(operands, []string{base, index}, func(ops []string) string {
		return fmt.Sprintf("%s.%s[%s]", ops[0], field, ops[1])
	}), nil
}

// lenValue renders a len other than of a string literal: the length of a
// string, read from it, or the constant length of an array, written after
// the array itself, cast to void, so a call in it still runs and a variable
// in it still counts as used.
func (g *generator) lenValue(call *ast.Call) (string, []diag.Diagnostic) {
	arg := call.Args[0]
	v, diags := g.expr(arg, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	if typ := ast.TypeOf(arg); g.prog.Types.IsArray(typ) {
		return fmt.Sprintf("((void)%s, (int64_t)%d)", v, g.prog.Types.Len(typ)), nil
	}
	return fmt.Sprintf("(%s).len", v), nil
}

// arrayComparison renders == or != on two arrays, or two structs, as a call
// to the type's equality helper, parenthesized as every comparison is.
func (g *generator) arrayComparison(bin *ast.Binary, left, right string) string {
	eq := fmt.Sprintf("%s(%s, %s)", equalityName(ast.TypeOf(bin.X)), left, right)
	if bin.Op == ast.Ne {
		return "(!" + eq + ")"
	}
	return "(" + eq + ")"
}

// indexValues lowers v, ok := a[i]: the index is held in a temporary,
// and the element is read only when the index is in range, giving the
// element type's zero value otherwise. A string is held the same way, and
// its byte read only when the index is below its length, known only at
// runtime, giving 0 otherwise. What is indexed is evaluated before the
// index, as written; see heldIndexed for whether, and how, it is held.
func (g *generator) indexValues(ix *ast.Index) (multiSource, []diag.Diagnostic) {
	base, diags := g.expr(ix.X, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	index, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}

	arrayType, indexType := ast.TypeOf(ix.X), ast.TypeOf(ix.Index)
	var src multiSource
	held := g.heldIndexed(ix, base)
	if held != nil {
		src.temps = append(src.temps, *held)
	}
	src.temps = append(src.temps, multiTemp{typ: g.cType(indexType), init: index})

	zero := g.zeroValue(ix.Type)
	src.field = func(tmps []string, i int) string {
		x, at := base, tmps[0]
		if held != nil {
			x, at = tmps[0], tmps[1]
			if held.ref {
				x = "(*" + x + ")"
			}
		}
		in, read := g.indexIn(arrayType, indexType, x, at)
		if i == 1 {
			return in
		}
		return fmt.Sprintf("(%s ? %s : %s)", in, read, zero)
	}
	return src, nil
}

// heldIndexed returns the temporary that holds what ix indexes, base as
// lowered, for indexValues, or nil when it is read where it is, after the
// index. It needs no temporary when it calls no function and the order of
// it and the index cannot show (see orderShows), unless it is a string
// other than a name, which is read twice, for its length and its byte, and
// so is held once. Otherwise an array that
// is a place, a variable or a field or element of one, is held by its
// address, which reads its element without copying it, unless evaluating
// the index can change the array, which must then be read as it was
// before: it is copied then, as a string or a view always is, being two
// words, and
// as anything else is, such as a call's result.
func (g *generator) heldIndexed(ix *ast.Index, base string) *multiTemp {
	typ := ast.TypeOf(ix.X)
	_, isName := ix.X.(*ast.Ident)
	readAfter := !callsFunc(ix.X) && !g.orderShows([]ast.Expr{ix.X, ix.Index})
	if readAfter && (isName || !g.hasLen(typ)) {
		return nil
	}
	if g.hasLen(typ) || ast.Root(ix.X) == nil || g.indexChanges(ix) {
		return &multiTemp{typ: g.cType(typ), init: base}
	}
	return &multiTemp{typ: g.cType(typ), init: "&" + base, ref: true}
}

// indexChanges reports whether evaluating the index of ix can change the
// array it indexes, a place: the index calls a function, and the array is
// part of a mutable global, which any function can assign, or of a
// variable the index passes as mut. A call reaches a local or a parameter
// of the caller only through a mut argument of its own.
func (g *generator) indexChanges(ix *ast.Index) bool {
	if !callsFunc(ix.Index) {
		return false
	}
	root := ast.Root(ix.X)
	if v, isVar := root.Def.(*ast.Var); isVar && g.globals[v] && v.Mutable {
		return true
	}
	return passesAsMut(ix.Index, root.Def)
}

// passesAsMut reports whether n passes decl, or a part of it, as a mut
// argument anywhere within it.
func passesAsMut(n ast.Node, decl ast.Decl) bool {
	found := false
	ast.Walk(n, func(n ast.Node) bool {
		if m, ok := n.(*ast.MutArg); ok {
			if root := ast.Root(m.X); root != nil && root.Def == decl {
				found = true
			}
		}
		return !found
	})
	return found
}

// indexIn renders the C test of whether the index at, of type indexType, is
// in range for x, of type xType, and the read of the element it guards. For
// an array the bounds are constants, and only those the index's type can
// cross are tested. For a string or a view the upper bound is its length: 0 is
// tested only when the index's type can go below it, and the length
// always. C compares the two in a common type. An unsigned index that can
// be 64 bits wide, a uint64 or a target-width one, is compared with the
// length as a uint64_t, both cast to it, which keeps the length's value,
// since a length is never negative, and which C would otherwise do
// implicitly where the index is 64 bits wide, with a warning that the
// operands' signs differ, and not where it is 32. Any other index's type
// converts to int64_t, the length's, which holds its every value.
func (g *generator) indexIn(xType, indexType types.Type, x, at string) (in, read string) {
	if !g.hasLen(xType) {
		n := g.prog.Types.Len(xType)
		in = withinExpr(indexType, at, constant.Int{}, constant.FromInt64(n-1))
		return in, fmt.Sprintf("%s.e[%s]", x, at)
	}
	upper := fmt.Sprintf("%s < %s.len", at, x)
	if types.IsUnsigned(indexType) && types.MaxBits(indexType) == 64 {
		upper = fmt.Sprintf("(uint64_t)%s < (uint64_t)%s.len", at, x)
	}
	if lo, _ := bounds(indexType); lo.IsNegative() {
		in = fmt.Sprintf("(%s >= 0 && %s)", at, upper)
	} else {
		in = "(" + upper + ")"
	}
	return in, fmt.Sprintf("%s.ptr[%s]", x, at)
}

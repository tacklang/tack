package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// emitArrayTypes defines a C struct for every array type the program uses,
// holding its elements as a C array, e. A C array cannot be assigned,
// passed or returned, and a struct holding one can, so an array is a value
// as Tack defines it. The types are emitted in the order the checker met
// them, which puts an element type before any array of it. An array of no
// elements still holds one, since C has no empty array, and is never
// indexed. Each type that == compares then gets its helper.
func (g *generator) emitArrayTypes() {
	table := g.file.Types
	for i := range table.Count() {
		typ := types.Added(i)
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

	compared := g.comparedArrays()
	for i := range table.Count() {
		if typ := types.Added(i); compared[typ] {
			g.emitArrayEquality(typ)
		}
	}
}

// comparedArrays returns every array type the program compares with == or
// !=, and every array type of their elements, which their helpers compare
// in turn.
func (g *generator) comparedArrays() map[types.Type]bool {
	compared := make(map[types.Type]bool)
	ast.Walk(g.file, func(n ast.Node) bool {
		bin, ok := n.(*ast.Binary)
		if ok && (bin.Op == ast.Eq || bin.Op == ast.Ne) {
			for typ := ast.TypeOf(bin.X); g.file.Types.IsArray(typ); typ = g.file.Types.Elem(typ) {
				compared[typ] = true
			}
		}
		return true
	})
	return compared
}

// emitArrayEquality writes tack_eq_ for an array type, which compares two
// arrays element by element.
func (g *generator) emitArrayEquality(typ types.Type) {
	table := g.file.Types
	name := arrayName(typ)
	differ := "a.e[i] != b.e[i]"
	if elem := table.Elem(typ); table.IsArray(elem) {
		differ = fmt.Sprintf("!%s(a.e[i], b.e[i])", equalityName(elem))
	}
	fmt.Fprintf(&g.buf, "static bool %s(%s a, %s b) {\n", equalityName(typ), name, name)
	fmt.Fprintf(&g.buf, "    for (int64_t i = 0; i < %d; i++) {\n", table.Len(typ))
	fmt.Fprintf(&g.buf, "        if (%s) {\n            return false;\n        }\n    }\n", differ)
	g.buf.WriteString("    return true;\n}\n\n")
}

// equalityName is the name of the helper that compares two arrays of type
// typ.
func equalityName(typ types.Type) string {
	return fmt.Sprintf("tack_eq_%d", typ)
}

// compositeLit renders an array literal as a C compound literal of its
// struct, its elements in order inside the struct's array.
func (g *generator) compositeLit(lit *ast.CompositeLit) (string, []diag.Diagnostic) {
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
	return fmt.Sprintf("(%s){{%s}}", arrayName(lit.Type), strings.Join(elems, ", ")), nil
}

// indexExpr renders an element read, x.e[i], or a string's byte, x.ptr[i].
// The checker has proven the index in range. x is parenthesized unless it is
// a name, an element or a call, which C's postfix operators already bind.
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
	if ast.TypeOf(ix.X) == types.String {
		field = "ptr"
	}
	switch ix.X.(type) {
	case *ast.Ident, *ast.Index, *ast.Call:
		return fmt.Sprintf("%s.%s[%s]", base, field, index), nil
	default:
		return fmt.Sprintf("(%s).%s[%s]", base, field, index), nil
	}
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
	if typ := ast.TypeOf(arg); g.file.Types.IsArray(typ) {
		return fmt.Sprintf("((void)%s, (int64_t)%d)", v, g.file.Types.Len(typ)), nil
	}
	return fmt.Sprintf("(%s).len", v), nil
}

// arrayComparison renders == or != on two arrays as a call to the type's
// equality helper, parenthesized as every comparison is.
func (g *generator) arrayComparison(bin *ast.Binary, left, right string) string {
	eq := fmt.Sprintf("%s(%s, %s)", equalityName(ast.TypeOf(bin.X)), left, right)
	if bin.Op == ast.Ne {
		return "(!" + eq + ")"
	}
	return "(" + eq + ")"
}

// arrayConst renders a package-level array's computed elements as a C
// initializer, which a static variable needs instead of a compound literal.
func (g *generator) arrayConst(elems []any) (string, bool) {
	if len(elems) == 0 {
		return "{0}", true
	}
	parts := make([]string, len(elems))
	for i, e := range elems {
		v, ok := g.constValue(e)
		if !ok {
			return "", false
		}
		parts[i] = v
	}
	return "{{" + strings.Join(parts, ", ") + "}}", true
}

// indexValues lowers v, ok := a[i]: the index is held in a temporary, and
// so is the array unless it is a variable, which reading again cannot
// change, and the element is read only when the index is in range, giving
// the element type's zero value otherwise. A string is held the same way,
// and its byte read only when the index is below its length, known only at
// runtime, giving 0 otherwise.
func (g *generator) indexValues(ix *ast.Index) (multiSource, []diag.Diagnostic) {
	base, diags := g.expr(ix.X, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	index, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}

	// The array is evaluated before the index, as written.
	arrayType, indexType := ast.TypeOf(ix.X), ast.TypeOf(ix.Index)
	var src multiSource
	_, isName := ix.X.(*ast.Ident)
	if !isName {
		src.temps = append(src.temps, multiTemp{g.cType(arrayType), base})
	}
	src.temps = append(src.temps, multiTemp{g.cType(indexType), index})

	zero := g.zeroValue(ix.Type)
	src.field = func(tmps []string, i int) string {
		array, at := base, tmps[0]
		if !isName {
			array, at = tmps[0], tmps[1]
		}
		in, read := g.indexIn(arrayType, indexType, array, at)
		if i == 1 {
			return in
		}
		return fmt.Sprintf("(%s ? %s : %s)", in, read, zero)
	}
	return src, nil
}

// indexIn renders the C test of whether the index at, of type indexType, is
// in range for x, of type xType, and the read of the element it guards. For
// an array the bounds are constants, and only those the index's type can
// cross are tested. For a string the upper bound is its length: 0 is
// tested only when the index's type can go below it, and the length
// always. C compares the two in a common type, unsigned when the index is a
// uint64, which keeps the length's value, since a length is never negative.
func (g *generator) indexIn(xType, indexType types.Type, x, at string) (in, read string) {
	if xType != types.String {
		n := g.file.Types.Len(xType)
		in = withinExpr(indexType, at, constant.Int{}, constant.FromInt64(n-1))
		return in, fmt.Sprintf("%s.e[%s]", x, at)
	}
	upper := fmt.Sprintf("%s < %s.len", at, x)
	if lo, _ := bounds(indexType); lo.IsNegative() {
		in = fmt.Sprintf("(%s >= 0 && %s)", at, upper)
	} else {
		in = "(" + upper + ")"
	}
	return in, fmt.Sprintf("%s.ptr[%s]", x, at)
}

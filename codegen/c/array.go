package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
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

// indexExpr renders an element read, x.e[i]. The checker has proven the
// index in range. x is parenthesized unless it is a name, an element or a
// call, which C's postfix operators already bind.
func (g *generator) indexExpr(ix *ast.Index) (string, []diag.Diagnostic) {
	base, diags := g.expr(ix.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	index, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	switch ix.X.(type) {
	case *ast.Ident, *ast.Index, *ast.Call:
		return fmt.Sprintf("%s.e[%s]", base, index), nil
	default:
		return fmt.Sprintf("(%s).e[%s]", base, index), nil
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

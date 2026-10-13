package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// emitTypes defines a C struct for every array and struct type the program
// uses, then the string comparison helper if the program compares strings,
// directly or inside those types, then the equality helper of every one ==
// compares. A type is
// defined after every type it holds, since C needs a member's type complete
// before the struct that holds it; the checker has rejected a struct that
// holds itself, so this order exists. Each type is then emitted in the
// order the checker met it, unless something it holds came later.
func (g *generator) emitTypes() {
	table := g.prog.Types
	defined := make(map[types.Type]bool)
	for i := range table.Count() {
		g.emitTypeDef(types.Added(i), defined)
	}

	compared := g.comparedTypes()
	if g.comparesStrings(compared) {
		g.emitStringCompare()
	}
	emitted := make(map[types.Type]bool)
	for i := range table.Count() {
		if typ := types.Added(i); compared[typ] {
			g.emitEqualityOf(typ, emitted)
		}
	}
}

// isComposite reports whether typ is an array or a struct type, which
// lowers to a C struct of its own.
func (g *generator) isComposite(typ types.Type) bool {
	return g.prog.Types.IsArray(typ) || g.prog.Types.IsStruct(typ)
}

// parts returns the types a value of type typ is made of: an array's
// element type, or a struct's field types, in order.
func (g *generator) parts(typ types.Type) []types.Type {
	table := g.prog.Types
	if table.IsArray(typ) {
		return []types.Type{table.Elem(typ)}
	}
	fields := table.Fields(typ)
	out := make([]types.Type, len(fields))
	for i, f := range fields {
		out[i] = f.Type
	}
	return out
}

// emitTypeDef defines the C struct for typ, after those of the types it is
// made of, unless defined says it has been already. typ is marked before
// its parts are visited, so even a type that held itself could not loop.
func (g *generator) emitTypeDef(typ types.Type, defined map[types.Type]bool) {
	if defined[typ] || !g.isComposite(typ) {
		return
	}
	defined[typ] = true
	for _, part := range g.parts(typ) {
		g.emitTypeDef(part, defined)
	}
	if g.prog.Types.IsArray(typ) {
		g.emitArrayDef(typ)
		return
	}
	g.emitStructDef(typ)
}

// emitStructDef defines the C struct a Tack struct type lowers to: one
// member for each field, in order, named by fieldName. A struct with no
// fields still holds one member, since C has no empty struct, and nothing
// reads it.
func (g *generator) emitStructDef(typ types.Type) {
	g.buf.WriteString("typedef struct {\n")
	fields := g.prog.Types.Fields(typ)
	if len(fields) == 0 {
		g.buf.WriteString("    char unused;\n")
	}
	for _, f := range fields {
		fmt.Fprintf(&g.buf, "    %s %s;\n", g.cType(f.Type), fieldName(f.Name))
	}
	fmt.Fprintf(&g.buf, "} %s;\n\n", g.structName(typ))
}

// comparedTypes returns every array and struct type the program compares
// with == or !=, and every array and struct type they are made of, which
// their helpers compare in turn.
func (g *generator) comparedTypes() map[types.Type]bool {
	compared := make(map[types.Type]bool)
	var mark func(typ types.Type)
	mark = func(typ types.Type) {
		if compared[typ] || !g.isComposite(typ) {
			return
		}
		compared[typ] = true
		for _, part := range g.parts(typ) {
			mark(part)
		}
	}
	g.walk(func(n ast.Node) bool {
		if bin, ok := n.(*ast.Binary); ok && (bin.Op == ast.Eq || bin.Op == ast.Ne) {
			mark(ast.TypeOf(bin.X))
		}
		if typ, ok := switchCompares(n); ok {
			mark(typ)
		}
		return true
	})
	return compared
}

// emitEqualityOf writes the equality helper for typ, after those of the
// types it is made of, which it calls, unless emitted says it has been
// written already.
func (g *generator) emitEqualityOf(typ types.Type, emitted map[types.Type]bool) {
	if emitted[typ] || !g.isComposite(typ) {
		return
	}
	emitted[typ] = true
	for _, part := range g.parts(typ) {
		g.emitEqualityOf(part, emitted)
	}
	if g.prog.Types.IsArray(typ) {
		g.emitArrayEquality(typ)
		return
	}
	g.emitStructEquality(typ)
}

// emitStructEquality writes tack_eq_ for a struct type, which compares two
// structs field by field, in order. A struct with no fields has nothing to
// compare, and its two of them are always equal.
func (g *generator) emitStructEquality(typ types.Type) {
	name := g.structName(typ)
	fmt.Fprintf(&g.buf, "static bool %s(%s a, %s b) {\n", equalityName(typ), name, name)
	fields := g.prog.Types.Fields(typ)
	if len(fields) == 0 {
		g.buf.WriteString("    (void)a;\n    (void)b;\n    return true;\n}\n\n")
		return
	}
	tests := make([]string, len(fields))
	for i, f := range fields {
		member := "." + fieldName(f.Name)
		tests[i] = g.equalExpr(f.Type, "a"+member, "b"+member)
	}
	fmt.Fprintf(&g.buf, "    return %s;\n}\n\n", strings.Join(tests, " && "))
}

// equalExpr renders a C test of whether l and r, two values of type typ,
// are equal: the type's equality helper for an array or a struct, the
// string comparison helper giving zero for a string, and C's == otherwise.
func (g *generator) equalExpr(typ types.Type, l, r string) string {
	if g.isComposite(typ) {
		return fmt.Sprintf("%s(%s, %s)", equalityName(typ), l, r)
	}
	if typ == types.String {
		return stringComparison(ast.Eq, l, r)
	}
	return fmt.Sprintf("%s == %s", l, r)
}

// structName is the name of the C struct a struct type lowers to: the
// struct's name, mangled as a package-level name is. A struct shares one
// namespace with functions and package-level variables, so the name is
// never another declaration's.
func (g *generator) structName(typ types.Type) string {
	return g.mangle(g.prog.Types.Name(typ))
}

// fieldName keeps a field's C member clear of C's keywords, for the same
// reason paramName keeps a parameter's name clear: a Tack field may be
// called int or while.
func fieldName(name string) string { return "f_" + name }

// structLit renders a struct literal as a C compound literal of its
// struct, each field set by a designated initializer in the order the
// literal names them. A struct with no fields gets its one unused member
// zeroed.
func (g *generator) structLit(lit *ast.CompositeLit) (string, []diag.Diagnostic) {
	if len(lit.Elems) == 0 {
		return fmt.Sprintf("(%s){0}", g.structName(lit.Type)), nil
	}
	inits := make([]string, len(lit.Elems))
	values := make([]ast.Expr, len(lit.Elems))
	fields := make([]string, len(lit.Elems))
	for i, e := range lit.Elems {
		kv, isKV := e.(*ast.KeyValue)
		key, isName := keyName(kv, isKV)
		if !isName {
			return "", []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "struct literal element without a field name",
				Span:     e.Spanned(),
			}}
		}
		v, diags := g.expr(kv.Value, types.Invalid)
		if len(diags) > 0 {
			return "", diags
		}
		values[i], fields[i], inits[i] = kv.Value, fieldName(key.Name), v
	}
	return g.sequenced(values, inits, func(ops []string) string {
		for i, v := range ops {
			ops[i] = fmt.Sprintf(".%s = %s", fields[i], v)
		}
		return fmt.Sprintf("(%s){%s}", g.structName(lit.Type), strings.Join(ops, ", "))
	}), nil
}

// keyName returns the name kv's key is, if kv is a key and a value, isKV,
// and its key a name.
func keyName(kv *ast.KeyValue, isKV bool) (*ast.Ident, bool) {
	if !isKV {
		return nil, false
	}
	key, ok := kv.Key.(*ast.Ident)
	return key, ok
}

// selectorExpr renders a field read, x.f_name. x is parenthesized unless it
// is a name, an element, a field or a call, which C's postfix operators
// already bind.
func (g *generator) selectorExpr(sel *ast.Selector) (string, []diag.Diagnostic) {
	base, diags := g.expr(sel.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	switch sel.X.(type) {
	case *ast.Ident, *ast.Index, *ast.Selector, *ast.Call:
		return base + "." + fieldName(sel.Name), nil
	default:
		return "(" + base + ")." + fieldName(sel.Name), nil
	}
}

// constOf renders a computed constant of type typ as a C constant
// expression, usable to initialize a static variable: an array's or a
// struct's as a brace-enclosed initializer, which a static variable needs
// instead of a compound literal, built from its elements or fields in
// order. No elements or fields is a zeroed initializer, which also fills
// the one member an empty array or struct holds.
func (g *generator) constOf(typ types.Type, c any) (string, bool) {
	values, isList := c.([]any)
	if !isList || !g.isComposite(typ) {
		return g.constValue(c)
	}
	if len(values) == 0 {
		return "{0}", true
	}
	parts, layout := g.parts(typ), "{%s}"
	if g.prog.Types.IsArray(typ) {
		layout = "{{%s}}"
	}
	out := make([]string, len(values))
	for i, v := range values {
		part := parts[0]
		if !g.prog.Types.IsArray(typ) {
			if i >= len(parts) {
				return "", false
			}
			part = parts[i]
		}
		rendered, ok := g.constOf(part, v)
		if !ok {
			return "", false
		}
		out[i] = rendered
	}
	return fmt.Sprintf(layout, strings.Join(out, ", ")), true
}

package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// resolveParamSwitch resolves s, a switch on a type parameter, nested in
// sc: its tag as a type, where the switch is, and each case as a block
// of its own nested in sc, in source order, the default's included. A
// case's values are types, resolved where the switch is, but for the
// name list[E] writes for its element type and map[K]V for its value
// type, which each declares a type parameter of the case's own, in its
// block, so the case's body can name it and nothing else can (see
// declareCaseParam).
func (r *pkgResolver) resolveParamSwitch(s *ast.Switch, sc *scope) []diag.Diagnostic {
	diags := r.resolveType(s.Tag, sc)
	for _, cl := range s.Cases {
		block := newScope(sc)
		for _, v := range cl.Values {
			diags = append(diags, r.resolveTypeCase(v, block, sc)...)
		}
		for _, stmt := range cl.Body {
			diags = append(diags, r.resolveStmt(stmt, block)...)
		}
		diags = append(diags, r.unused(block)...)
	}
	return diags
}

// resolveTypeCase resolves v, a value of a case of a switch on a type
// parameter whose body is block, nested in sc, where the switch is:
// struct names nothing, list[E] and map[K]V declare E and V in block,
// K resolved in sc, and any other value is a type resolved in sc.
func (r *pkgResolver) resolveTypeCase(v ast.Expr, block, sc *scope) []diag.Diagnostic {
	switch x := v.(type) {
	case *ast.StructCase:
		return nil
	case *ast.ListType:
		return r.declareCaseParam(x.Elem, "the element type of a list", "list[E]", block)
	case *ast.MapType:
		diags := r.resolveType(x.Key, sc)
		return append(diags,
			r.declareCaseParam(x.Value, "the value type of a map", "map[text.Text]V", block)...)
	default:
		return r.resolveType(v, sc)
	}
}

// declareCaseParam declares part, the type what names in a value of a
// case of a switch on a type parameter, as a type parameter of its own in
// block, the case's body: part must be a name, unqualified, that names
// nothing visible there and is not reserved, or the case declares none,
// and example shows how one is written. A name declared twice in one case
// is reported at the second, as any name declared twice in one block is.
func (r *pkgResolver) declareCaseParam(
	part ast.Expr,
	what, example string,
	block *scope,
) []diag.Diagnostic {
	id, ok := part.(*ast.Ident)
	if !ok || id.Pkg != "" || namesType(id, block) {
		prefix := ""
		if ok {
			prefix = id.Qualified() + " is a type; "
		}
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message: fmt.Sprintf("%sa case of a switch on a type parameter names %s "+
				"with a new name, as in case %s", prefix, what, example),
			Span: part.Spanned(),
		}}
	}
	if reserved := reservedName(id.Name, id.Span); reserved != nil {
		return reserved
	}
	if existing, visible := block.lookup(id.Name); visible {
		block.reject(id.Name)
		id.Def = ast.Unbound
		return []diag.Diagnostic{redeclared(id.Name, id.Span, existing)}
	}
	tp := &ast.TypeParam{Name: id.Name, NameSpan: id.Span, Span: id.Span}
	block.names[id.Name] = tp
	id.Def = tp
	return nil
}

// namesType reports whether id, an unqualified name, names a type where
// sc is: a builtin type, or a type declaration or type parameter visible
// there.
func namesType(id *ast.Ident, sc *scope) bool {
	if types.Reserved(id.Name) {
		return true
	}
	switch decl, _ := sc.lookup(id.Name); decl.(type) {
	case *ast.TypeDecl, *ast.TypeParam:
		return true
	}
	return false
}

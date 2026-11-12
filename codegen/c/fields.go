package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Fields loops. for f := range fields(T) { body } is unrolled: its body is
// lowered once for each field the type checker recorded the loop lists
// for the type T stands for (see ast.FieldsOf.Unrolled), in order, each
// time in a C block of its own, with the loop's type parameter,
// FieldsOf.Field, bound to that field's type (see types.Table.BindMore),
// so every value of it is lowered as a value of the field's type, and
// with the field remembered for the loop's variable, so v.[f] lowers to
// the field's member, v.f_name, f.name to the field's name, a pooled
// string, and f.index to its position among the fields listed, a
// literal. Each block is an ordinary block, with its own cleanup chain,
// so the locals it declares are freed and its defers run at its end, and
// a return or a ? in it leaves through the blocks around it as from any
// block; the checker allows no break or continue in it. A type with no
// fields, or none the loop's package sees, lowers to nothing. So the
// code is what the fields written out by hand, one block each, would
// give, moves and frees included.
//
// A closure in the body is lowered after the function holding it, once
// for each block it is in: it keeps the binding and the fields in force
// where it is met, and is lowered under them (see closureValue).

// reflectField is the field a fields loop's variable stands for in the
// block being lowered: the struct type, the field's position among its
// fields, and its position among the fields the loop lists.
type reflectField struct {
	typ   types.Type
	field int
	index int
}

// fieldsLoop lowers loop, a fields loop of fn ranging over fo, as one C
// block for each field it lists for the type its T stands for.
func (g *generator) fieldsLoop(
	loop *ast.ForRange,
	fo *ast.FieldsOf,
	fn *ast.Func,
) ([]string, []diag.Diagnostic) {
	typ := g.prog.Types.Bound(fo.Type)
	fields, ok := fo.FieldsFor(typ)
	if !ok {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a fields loop must have its fields recorded before code is generated for it",
			Span:     fo.Span,
		}}
	}
	all := g.prog.Types.Fields(typ)
	lines := []string{}
	for n, i := range fields {
		restore := g.prog.Types.BindMore(map[types.Type]types.Type{fo.Field: all[i].Type})
		if loop.Var != nil {
			g.reflect[loop.Var] = reflectField{typ: typ, field: i, index: n}
		}
		body, diags := g.block(loop.Body, fn, false, nil)
		if loop.Var != nil {
			delete(g.reflect, loop.Var)
		}
		restore()
		if len(diags) > 0 {
			return nil, diags
		}
		lines = append(lines, "{")
		lines = append(lines, indent(body)...)
		lines = append(lines, "}")
	}
	return lines, nil
}

// reflectOf returns the field id, a fields loop's variable, stands for in
// the block being lowered, and whether it is one.
func (g *generator) reflectOf(id *ast.Ident) (reflectField, bool) {
	if id == nil {
		return reflectField{}, false
	}
	v, ok := id.Def.(*ast.Var)
	if !ok {
		return reflectField{}, false
	}
	r, ok := g.reflect[v]
	return r, ok
}

// fieldName returns the name of the field sel selects, the field its
// fields loop's variable stands for when sel is v.[f], and the name
// written for any other.
func (g *generator) fieldName(sel *ast.Selector) string {
	if r, ok := g.reflectOf(sel.Field); ok {
		return g.prog.Types.Fields(r.typ)[r.field].Name
	}
	return sel.Name
}

// member returns the C member sel, a field selected from a struct, is.
func (g *generator) member(sel *ast.Selector) string {
	return memberName(g.prog.Types.Bound(ast.TypeOf(sel.X)), g.fieldName(sel))
}

// fieldMember renders sel, f.name or f.index, when f, sel.X, is a fields
// loop's variable, and reports whether it is: the field's name, a pooled
// string, or its position among the fields the loop lists, an int64.
func (g *generator) fieldMember(sel *ast.Selector) (string, bool, []diag.Diagnostic) {
	id, ok := sel.X.(*ast.Ident)
	if !ok || sel.Field != nil {
		return "", false, nil
	}
	r, ok := g.reflectOf(id)
	if !ok {
		return "", false, nil
	}
	switch sel.Name {
	case "name":
		return g.stringValue(g.prog.Types.Fields(r.typ)[r.field].Name), true, nil
	case "index":
		return intLiteral(constant.FromInt64(int64(r.index))), true, nil
	default:
		return "", true, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("a field of a fields loop has no %s", sel.Name),
			Span:     sel.NameSpan,
		}}
	}
}

// poolFieldNames pools the names of the fields fo lists for every type it
// was recorded for, when its body reads f.name.
func (g *generator) poolFieldNames(fo *ast.FieldsOf) {
	if !fo.Names {
		return
	}
	for _, u := range fo.Unrolled {
		all := g.prog.Types.Fields(u.Type)
		for _, i := range u.Fields {
			g.addStr(all[i].Name)
		}
	}
}

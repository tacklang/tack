package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// fieldVis returns the visibility the i'th field of the struct type typ has
// in effect: the field's own, capped by its struct's.
func (c *Checker) fieldVis(typ types.Type, i int) ast.Visibility {
	td := c.structs[typ]
	st, ok := td.TypeExpr.(*ast.StructType)
	if !ok || i >= len(st.Fields) {
		return ast.Private
	}
	return st.Fields[i].Vis.Capped(td.Vis)
}

// hiddenField returns why the code being checked may not use the i'th
// field of the struct type typ, named name, by name, or "" when it may:
// the field is not visible from the package being checked. A struct with
// no package, error, has every field visible.
func (c *Checker) hiddenField(typ types.Type, i int, name string) string {
	owner := c.structPkg[typ]
	if owner == nil || c.pkg == nil {
		return ""
	}
	vis := c.fieldVis(typ, i)
	if ast.Visible(vis, owner, c.pkg) {
		return ""
	}
	if vis == ast.Lib {
		return ast.LibMessage(name, owner, c.pkg)
	}
	return fmt.Sprintf("%s is not pub in %s", name, c.typeName(typ))
}

// checkLiteralFields reports a literal of the struct type typ, at span,
// written in a package that cannot see every one of its fields: a literal
// names every field, so it needs them all.
func (c *Checker) checkLiteralFields(typ types.Type, span ast.Span) bool {
	var hidden []string
	for i, f := range c.table.Fields(typ) {
		if c.hiddenField(typ, i, f.Name) != "" {
			hidden = append(hidden, f.Name)
		}
	}
	if len(hidden) == 0 {
		return true
	}
	verb := "is"
	if len(hidden) > 1 {
		verb = "are"
	}
	c.errorf(span, "cannot write a %s literal outside package %s: %s %s %s not pub",
		c.typeName(typ), c.structPkg[typ].Name, plural(len(hidden), "field"),
		strings.Join(hidden, ", "), verb)
	return false
}

// checkExposure reports every type a pub or lib declaration exposes that is
// less visible than it: a type named in a function's signature, a global's
// type, a field's type, where the field's visibility is capped by its
// struct's, or a union case's payload's type, a case being as visible as
// its union. A pub one may expose only pub types; a lib one, lib or pub
// ones. A client of the declaration can then name every type it gets.
func (c *Checker) checkExposure(decls []ast.Decl) {
	for _, decl := range decls {
		restore := c.enter(decl)
		switch d := decl.(type) {
		case *ast.Func:
			c.funcExposure(d)
		case *ast.Var:
			// A mut global cannot be pub or lib, which the resolver
			// reported.
			if d.Vis != ast.Private && !d.Mutable {
				at := d.NameSpan
				if d.TypeExpr != nil {
					at = d.TypeExpr.Spanned()
				}
				c.exposure(d.Vis, "variable "+d.Name, d.Type, at)
			}
		case *ast.TypeDecl:
			c.typeExposure(d)
		}
		restore()
	}
}

// typeExposure reports every type td, a type declaration, exposes that is
// less visible than it: an interface's methods' types, a union's
// payloads' types, or a struct's fields' types.
func (c *Checker) typeExposure(td *ast.TypeDecl) {
	switch {
	case td.Interface() != nil:
		c.ifaceExposure(td)
	case td.Union() != nil:
		c.unionExposure(td)
	default:
		c.fieldExposure(td)
	}
}

// funcExposure reports every type the signature of fn, if pub or lib,
// names that is less visible than fn: for a method, its receiver's type
// included, with its visibility capped by that type's.
func (c *Checker) funcExposure(fn *ast.Func) {
	vis, what := fn.Vis, "function "+fn.Name
	if fn.Method {
		// A method is never more visible than its type, as a field is
		// not; its receiver, of that type, is then never less visible.
		if fn.RecvType != nil {
			vis = vis.Capped(fn.RecvType.Vis)
		}
		what = "method " + c.displayName(fn)
	}
	if vis == ast.Private {
		return
	}
	c.constraintExposure(vis, what, fn)
	for _, p := range fn.Params {
		c.exposure(vis, what, p.Type, p.TypeExpr.Spanned())
	}
	for i, r := range fn.Results {
		if i < len(fn.ResultTypes) {
			c.exposure(vis, what, fn.ResultTypes[i], r.Spanned())
		}
	}
}

// fieldExposure reports every type a field of td, visible beyond its
// package, has that is less visible than the field.
func (c *Checker) fieldExposure(td *ast.TypeDecl) {
	st, ok := td.TypeExpr.(*ast.StructType)
	if !ok {
		return
	}
	fields := c.table.Fields(td.Type)
	for i, f := range st.Fields {
		if vis := f.Vis.Capped(td.Vis); vis != ast.Private && i < len(fields) {
			c.exposure(vis, "field "+f.Name+" of "+td.Name, fields[i].Type, f.TypeExpr.Spanned())
		}
	}
}

// exposure reports typ, written at at in the declaration what, whose
// visibility is vis, if it is or holds a struct or union type less
// visible than vis, as an array's, a view's, a list's, a pool's or a
// handle's element, a map's key or value, or a function type's parameter
// or result.
func (c *Checker) exposure(vis ast.Visibility, what string, typ types.Type, at ast.Span) {
	for c.table.IsArray(typ) || c.table.IsView(typ) || c.table.IsList(typ) || c.table.IsPool(typ) ||
		c.table.IsHandle(typ) {
		typ = c.table.Elem(typ)
	}
	if c.table.IsMap(typ) || c.table.IsFunc(typ) {
		for _, part := range c.funcParts(typ) {
			c.exposure(vis, what, part, at)
		}
		return
	}
	td := c.typeDecl(typ)
	if td == nil || td.Vis >= vis {
		return
	}
	need := "pub"
	if vis == ast.Lib {
		need = "lib or pub"
	}
	c.errorf(at, "%s %s exposes %s, which is not %s", vis, what, c.typeName(typ), need)
}

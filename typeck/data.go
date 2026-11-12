package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/types"
)

// Data and fields loops. Data is a builtin constraint, closed: a type
// satisfies it when it is plain data, a bool, an integer or a float, a
// list of Data elements, a map from text.Text to Data values, or a struct
// whose fields are all Data, private ones included, so a type is Data or
// not whichever package asks, which text.Text is, holding a list[uint8].
// Nothing else is: no string, which is only ever a
// literal, no union, array, view, any, id, span, handle, arena, pool,
// function value or Allocator, and no type parameter constrained by an
// interface. A type parameter constrained by Data has no method and no
// field, so its values are borrowed, moved and freed as any other type
// parameter's are, and nothing more; whether one satisfies an interface
// is decided as for any type parameter, so only an interface that asks
// for no method takes one.
//
// for f := range fields(T) { ... } ranges over the fields of T, a type
// parameter constrained by Data or a struct type that satisfies Data
// (see ast.FieldsOf). Its body is checked once, as the body of a loop
// that may run any number of times, against nothing but Data: v.[f], a
// field of a value v of type T, has the loop's own type parameter,
// FieldsOf.Field, constrained by Data, which every v of type T shares for
// one f, and is a place as any field is, read, assigned when v can be,
// and passed as mut when v is. So what is reported never depends on the
// type arguments of an instance, and a body that checked is right for
// every field of every one. f itself is no value: f.name is the field's
// name, a string, and f.index its position among the fields listed, an
// int64, and anything else written with it is reported. A break or a
// continue that would leave the loop is reported, since its body runs
// once for each field, in place. Once every instance is found, the fields
// each loop lists for each type it is lowered for are recorded (see
// recordFields): those whose own mark lets the package declaring the
// loop's function see them, uncapped by the struct's visibility, since
// the loop gets the type only from a caller that can name it (see
// visibleFields).

// isDataName reports whether e, a type parameter's constraint as written,
// names the builtin constraint Data, which nothing can declare a name of.
func isDataName(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Pkg == "" && id.Def == nil && builtin.IsConstraint(id.Name)
}

// satisfiesData reports whether got, a type argument passed as at for a
// type parameter constrained by Data, satisfies Data, and reports at at
// why it does not.
func (c *Checker) satisfiesData(at ast.Expr, got types.Type) bool {
	why := c.notData(got, make(map[types.Type]bool))
	if why == "" {
		return true
	}
	c.errorf(at.Spanned(), "%s does not satisfy Data: %s", c.typeName(got), why)
	return false
}

// notData returns why typ is not Data, naming the field, element or value
// type deepest in it that is not, or "" when it is. seen holds the
// structs being looked at further up, which a struct reached again
// through a list or a map counts as Data, since it is Data exactly when
// it is once every other part is. A type parameter is Data when its
// constraint is, and one whose constraint was reported is taken as Data,
// so nothing more is reported of it.
func (c *Checker) notData(typ types.Type, seen map[types.Type]bool) string {
	switch {
	case typ == types.Invalid, typ == types.Bool, types.IsInteger(typ), types.IsFloat(typ):
		return ""
	case c.table.IsTypeParam(typ):
		return c.paramNotData(typ)
	case c.table.IsList(typ):
		return c.partNotData(c.table.Elem(typ), "the element type of "+c.typeName(typ), seen)
	case c.table.IsMap(typ):
		if key := c.table.Key(typ); !c.isText(key) {
			return fmt.Sprintf("the key type of %s is %s, and a Data map's key is text.Text",
				c.typeName(typ), c.typeName(key))
		}
		return c.partNotData(c.table.Elem(typ), "the value type of "+c.typeName(typ), seen)
	case c.table.IsStruct(typ):
		return c.structNotData(typ, seen)
	default:
		return c.kindName(typ) + " is not Data"
	}
}

// kindName names the kind of typ, a type that is not Data and is made of
// no Data parts, in a diagnostic: a string, an Allocator, an any, an
// array, a view, a union, a function value, an arena, a pool, an id, a
// span or a handle.
func (c *Checker) kindName(typ types.Type) string {
	switch typ {
	case types.String:
		return "a string"
	case types.Allocator:
		return "an Allocator"
	case types.Any:
		return "an any"
	}
	kinds := []struct {
		is   func(types.Type) bool
		name string
	}{
		{c.table.IsArray, "an array"},
		{c.table.IsView, "a view"},
		{c.table.IsUnion, "a union"},
		{c.table.IsFunc, "a function value"},
		{c.table.IsArena, "an arena"},
		{c.table.IsPool, "a pool"},
		{c.table.IsID, "an id"},
		{c.table.IsSpan, "a span"},
		{c.table.IsHandle, "a handle"},
	}
	for _, k := range kinds {
		if k.is(typ) {
			return k.name
		}
	}
	return c.typeName(typ)
}

// paramNotData returns why typ, a type parameter, is not Data: it is
// constrained by an interface. One constrained by Data, or whose
// constraint was reported, is Data, and gives "".
func (c *Checker) paramNotData(typ types.Type) string {
	tp := c.typeParams[typ]
	if tp == nil || tp.Data || tp.Iface == nil {
		return ""
	}
	return fmt.Sprintf("%s is constrained by %s, not Data",
		c.typeName(typ), c.ifaceName(tp.Iface))
}

// partNotData returns why part, the type of a part of a type that what
// names, is not Data, or "" when it is: its own reason, when it is made of
// parts of its own, or that what is part, which is not.
func (c *Checker) partNotData(part types.Type, what string, seen map[types.Type]bool) string {
	why := c.notData(part, seen)
	switch {
	case why == "":
		return ""
	case c.table.IsList(part), c.table.IsMap(part), c.table.IsStruct(part),
		c.table.IsTypeParam(part):
		return why
	default:
		return fmt.Sprintf("%s is %s, which is not Data", what, c.typeName(part))
	}
}

// structNotData returns why typ, a struct type, is not Data: the first of
// its fields, in declaration order, that is not, private ones included,
// so a type is Data or not whichever package asks, or "" when every one
// is.
func (c *Checker) structNotData(typ types.Type, seen map[types.Type]bool) string {
	if seen[typ] {
		return ""
	}
	seen[typ] = true
	if !c.resolveFields(typ, ast.Span{}) {
		return "" // reported where the struct is declared
	}
	for _, f := range c.table.Fields(typ) {
		what := fmt.Sprintf("field %s of %s", f.Name, c.typeName(typ))
		if why := c.partNotData(f.Type, what, seen); why != "" {
			return why
		}
	}
	return ""
}

// textPath is the import path of std/text, whose Text is the key type of
// every Data map.
const textPath = "std/text"

// isText reports whether typ is text.Text, std/text's Text.
func (c *Checker) isText(typ types.Type) bool {
	td := c.structs[typ]
	pkg := c.structPkg[typ]
	return td != nil && pkg != nil && pkg.Path == textPath && td.Name == "Text"
}

// checkFieldsLoop checks loop, a fields loop ranging over fo (see the
// comment at the top of the file): T, the loop's variable, which only
// f.name, f.index and v.[f] can use, and its body, once, as a range
// loop's is, with no facts of its own, its break and continue reported.
func (c *Checker) checkFieldsLoop(loop *ast.ForRange, fo *ast.FieldsOf, fn *ast.Func) {
	if loop.Elem != nil {
		c.errorf(
			ast.Span{File: loop.Var.Span.File, Start: loop.Var.Span.Start, End: fo.Span.End},
			"assignment mismatch: 2 variables but range over %s gives 1 value",
			c.text(fo.Span),
		)
	}
	fo.Type = c.fieldsType(fo)
	if fo.Field == types.Invalid {
		name := c.text(fo.TypeExpr.Spanned()) + ".[_]"
		if loop.Var != nil {
			name = c.text(fo.TypeExpr.Spanned()) + ".[" + loop.Var.Name + "]"
		}
		fo.Field = c.table.TypeParam(name)
		c.typeParams[fo.Field] = &ast.TypeParam{
			Name: name, NameSpan: fo.Span, Span: fo.Span, Type: fo.Field, Data: true,
		}
	}
	if loop.Var != nil {
		loop.Var.Type = types.Invalid
		c.bindings[loop.Var] = rangeVar
		c.fieldLoops[loop.Var] = fo
	}
	header := ast.Span{File: loop.Span.File, Start: loop.Span.Start, End: fo.Span.End}
	m := c.mark()
	leave := c.enterLoop(loop)
	c.loops++
	outer := c.fieldLoop
	c.fieldLoop = c.loops
	c.checkGuarded(loop.Body, &guard{span: header, kind: guardRange}, nil, fn)
	c.fieldLoop = outer
	c.loops--
	leave()
	c.release(m)
	markUnrolled(loop.Body)
}

// markUnrolled marks each local named in body, a fields loop's, and
// declared outside it, as ast.Var.Unrolled: the backend lowers body once
// for each field, so for a type with none the variable may be named
// nowhere else in C.
func markUnrolled(body []ast.Stmt) {
	inside := make(map[*ast.Var]bool)
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if v, ok := n.(*ast.Var); ok {
				inside[v] = true
			}
			return true
		})
	}
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, isVar := id.Def.(*ast.Var); isVar && !inside[v] {
					v.Unrolled = true
				}
			}
			return true
		})
	}
}

// fieldsType returns the type fo ranges over the fields of, reporting
// one that is neither a type parameter constrained by Data nor a struct
// type that satisfies Data, for which it returns types.Invalid.
func (c *Checker) fieldsType(fo *ast.FieldsOf) types.Type {
	typ := c.declaredType(fo.TypeExpr)
	switch {
	case typ == types.Invalid:
		return types.Invalid
	case c.table.IsTypeParam(typ):
		tp := c.typeParams[typ]
		if tp == nil || tp.Data {
			return typ
		}
		if tp.Iface != nil {
			c.errorf(fo.TypeExpr.Spanned(),
				"cannot range over the fields of %s: it is constrained by %s, not Data",
				c.typeName(typ), c.ifaceName(tp.Iface))
		}
		return types.Invalid
	case !c.table.IsStruct(typ):
		c.errorf(fo.TypeExpr.Spanned(), "cannot range over the fields of %s: it is not a struct",
			c.typeName(typ))
		return types.Invalid
	}
	if why := c.notData(typ, make(map[types.Type]bool)); why != "" {
		c.errorf(fo.TypeExpr.Spanned(), "%s does not satisfy Data: %s", c.typeName(typ), why)
		return types.Invalid
	}
	return typ
}

// fieldsElsewhere reports fo, fields(T) written anywhere but as a range
// loop's X, the one place it can be.
func (c *Checker) fieldsElsewhere(fo *ast.FieldsOf) types.Type {
	c.errorf(fo.Span, "%s can only be ranged over: for f := range %s { ... }",
		c.text(fo.Span), c.text(fo.Span))
	return types.Invalid
}

// fieldVar returns what the fields loop whose variable id names ranges
// over, or nil when id names anything else.
func (c *Checker) fieldVar(id *ast.Ident) *ast.FieldsOf {
	v, ok := id.Def.(*ast.Var)
	if !ok {
		return nil
	}
	return c.fieldLoops[v]
}

// fieldVarValue reports id, a fields loop's variable used as a value:
// stored, passed, returned or anything else but f.name, f.index or v.[f].
func (c *Checker) fieldVarValue(id *ast.Ident) types.Type {
	c.errorf(id.Span,
		"%s is a field of a fields loop, not a value; use %s.name, %s.index or v.[%s]",
		id.Name, id.Name, id.Name, id.Name)
	return types.Invalid
}

// fieldMember checks sel, f.name or f.index, where f, sel.X, is the
// variable of a fields loop ranging over fo, and returns its type: a
// string for the field's name, which the backend pools, and an int64 for
// its position among the fields the loop lists. Any other name is
// reported.
func (c *Checker) fieldMember(sel *ast.Selector, fo *ast.FieldsOf) types.Type {
	switch sel.Name {
	case "name":
		fo.Names = true
		sel.Type = types.String
	case "index":
		sel.Type = types.Int64
	default:
		c.errorf(sel.NameSpan, "%s has no %s: a field of a fields loop has only name and index",
			c.text(sel.X.Spanned()), sel.Name)
		sel.Type = types.Invalid
	}
	return sel.Type
}

// reflectField returns the type of sel, v.[f], a field of a value of type
// base, which is the type parameter the fields loop f is the variable of
// gives its fields, FieldsOf.Field. f must be such a variable, and base
// the type the loop ranges over the fields of; anything else is
// reported, and gives types.Invalid.
func (c *Checker) reflectField(sel *ast.Selector, base types.Type) types.Type {
	if c.unbound(sel.Field) {
		return types.Invalid
	}
	fo := c.fieldVar(sel.Field)
	if fo == nil {
		if sel.Field.Def != nil {
			c.errorf(sel.Field.Span,
				"%s is not a fields loop's variable; v.[f] takes the f of for f := range fields(T)",
				sel.Field.Name)
		}
		return types.Invalid
	}
	if base == types.Invalid || fo.Type == types.Invalid {
		return types.Invalid
	}
	if base != fo.Type {
		c.errorf(sel.Span, "%s is %s, and %s is a field of %s", c.text(sel.X.Spanned()),
			c.typeName(base), sel.Field.Name, c.typeName(fo.Type))
		return types.Invalid
	}
	return fo.Field
}

// visibleFields returns the positions of the fields of typ, in
// declaration order, that a fields loop of pkg lists: those whose own mark
// lets pkg see them, every field of a struct of pkg, the pub ones of
// another package's, and the lib ones too when it is of pkg's module.
// The struct's own visibility does not cap the mark, as it does for a
// field named (see fieldVis): the loop is given the type by a caller that
// can name it. Any other type has none. A struct with no package, error,
// has every field listed.
func (c *Checker) visibleFields(typ types.Type, pkg *ast.Package) []int {
	if !c.table.IsStruct(typ) {
		return nil
	}
	owner := c.structPkg[typ]
	var list []int
	for i := range c.table.Fields(typ) {
		if owner == nil || pkg == nil || ast.Visible(c.ownFieldVis(typ, i), owner, pkg) {
			list = append(list, i)
		}
	}
	return list
}

// ownFieldVis returns the visibility the i'th field of the struct type typ
// is marked with, uncapped by its struct's.
func (c *Checker) ownFieldVis(typ types.Type, i int) ast.Visibility {
	st, ok := c.structs[typ].TypeExpr.(*ast.StructType)
	if !ok || i >= len(st.Fields) {
		return ast.Private
	}
	return st.Fields[i].Vis
}

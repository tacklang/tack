package typeck

import (
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// progress is how far checking has got with something that is checked when
// first needed, a struct's fields or a package-level variable, so that
// needing it again while checking it is noticed rather than looped on.
type progress int

// The zero value of progress means not looked at yet.
const (
	// checking means it is being checked, further up the call stack.
	checking progress = iota + 1
	// checked means it is done, and its results are recorded.
	checked
)

// declareStructs gives every type declaration in the program its struct
// or union type, with no fields or cases yet, so that any type, a field's
// or a payload's type included, can name any struct or union whatever
// the order of declarations. Both are listed in structOrder, in source
// order.
func (c *Checker) declareStructs() {
	for _, decl := range c.prog.Decls() {
		td, ok := decl.(*ast.TypeDecl)
		if !ok || td.Interface() != nil {
			continue
		}
		if td.Union() != nil {
			td.Type = c.table.Union(td.Name)
			c.unions[td.Type] = td
		} else {
			td.Type = c.table.Struct(td.Name)
			c.structs[td.Type] = td
		}
		c.structPkg[td.Type] = c.declPkg[td]
		c.structOrder = append(c.structOrder, td.Type)
	}
}

// checkStructs resolves the fields of every struct type, and the cases of
// every union type, not already resolved, then reports every struct or
// union that contains itself by value.
func (c *Checker) checkStructs() {
	for _, typ := range c.structOrder {
		if td := c.unions[typ]; td != nil {
			c.resolveCases(typ, td.NameSpan)
			continue
		}
		c.resolveFields(typ, c.structs[typ].NameSpan)
	}
	c.reportCycles()
}

// resolveFields records the fields of the struct type typ in the table the
// first time they are needed: a global's initializer, or a field's array
// length, may need them before their turn. A field's array length names no
// variable (see arrayLength), but it may still write a literal of a struct,
// so needing the fields again while they are being resolved, at, means the
// struct's fields depend on themselves, which is reported, and gives false.
// error's fields are fixed, and need no resolving.
func (c *Checker) resolveFields(typ types.Type, at ast.Span) bool {
	if typ == types.Error {
		return true
	}
	switch c.structState[typ] {
	case checked:
		return true
	case checking:
		c.errorf(at, "invalid recursive type %s", c.typeName(typ))
		return false
	}

	c.structState[typ] = checking
	defer c.enter(c.structs[typ])()
	st, _ := c.structs[typ].TypeExpr.(*ast.StructType)
	fields := make([]types.Field, 0)
	if st != nil {
		outer, outerUnion := c.inFields, c.inUnion
		c.inFields, c.inUnion = true, false
		for _, f := range st.Fields {
			fields = append(fields, types.Field{Name: f.Name, Type: c.declaredType(f.TypeExpr)})
		}
		c.inFields, c.inUnion = outer, outerUnion
	}
	c.table.SetFields(typ, fields)
	c.structState[typ] = checked
	return true
}

// cycleEdge is one step of a struct or union containing another, by
// value or through a list or a map: the type from, through its field or
// case written at span, whose type reaches the other through via, "a
// list" or "a map", when it is set.
type cycleEdge struct {
	via  string
	span ast.Span
	from types.Type
}

// reportCycles reports every struct or union type that contains itself by
// value, directly, through another struct or union, or through an array,
// which would make it infinitely large, or through a list or a map, which
// a type cannot do either, though its size would be finite: a list may hold an
// owned type, but how a type holding itself would be built, freed and
// lowered is not settled. Each cycle is reported once, at the type it
// was found from, with a note for every field or case on the way round.
func (c *Checker) reportCycles() {
	state := make(map[types.Type]progress)
	for _, typ := range c.structOrder {
		c.visitStruct(typ, state, nil)
	}
}

// visitStruct walks the structs and unions typ, a struct or a union,
// contains by value, depth first, path being the fields and cases
// followed to reach it, and reports a cycle on reaching a type still on
// path.
func (c *Checker) visitStruct(typ types.Type, state map[types.Type]progress, path []cycleEdge) {
	switch state[typ] {
	case checked:
		return
	case checking:
		c.reportCycle(typ, path)
		return
	}
	state[typ] = checking
	for _, part := range c.partsOf(typ) {
		for _, inner := range c.structsIn(part.typ, "") {
			c.visitStruct(
				inner.typ,
				state,
				append(path, cycleEdge{span: part.span, from: typ, via: inner.via}),
			)
		}
	}
	state[typ] = checked
}

// part is one field of a struct, or one payload of a union: its type and
// where it is written.
type part struct {
	span ast.Span
	typ  types.Type
}

// partsOf returns the fields of the struct typ, or the payloads of the
// union typ, each with where it is written.
func (c *Checker) partsOf(typ types.Type) []part {
	var parts []part
	if td := c.unions[typ]; td != nil {
		cases := c.table.Cases(typ)
		for i, uc := range td.Union().Cases {
			if i < len(cases) && cases[i].Payload {
				parts = append(parts, part{span: uc.Span, typ: cases[i].Type})
			}
		}
		return parts
	}
	st, _ := c.structs[typ].TypeExpr.(*ast.StructType)
	for i, f := range c.table.Fields(typ) {
		if st != nil && i < len(st.Fields) {
			parts = append(parts, part{span: st.Fields[i].Span, typ: f.Type})
		}
	}
	return parts
}

// core is a struct or a union type a value holds at its core, and what it
// is held through, "a list" or "a map", or "" when it is held by value.
type core struct {
	via string
	typ types.Type
}

// structsIn returns the struct or union types a value of type typ holds
// at its core: typ itself if it is a struct or a union, or the element
// type of an array or a list, or the key and value types of a map, at
// any depth, of an array of lists of maps included, that is one; via
// says what was on the way already, and each core keeps the first list
// or map on its way. error is a struct no declaration gives, which holds
// no other struct, so it is not one here.
func (c *Checker) structsIn(typ types.Type, via string) []core {
	switch {
	case c.table.IsMap(typ):
		if via == "" {
			via = "a map"
		}
		return append(c.structsIn(c.table.Key(typ), via), c.structsIn(c.table.Elem(typ), via)...)
	case c.table.IsList(typ):
		if via == "" {
			via = "a list"
		}
		return c.structsIn(c.table.Elem(typ), via)
	case c.table.IsArray(typ):
		return c.structsIn(c.table.Elem(typ), via)
	case (c.table.IsStruct(typ) || c.table.IsUnion(typ)) && typ != types.Error:
		return []core{{typ: typ, via: via}}
	default:
		return nil
	}
}

// reportCycle reports the cycle that path, the fields followed so far, closes
// by reaching typ again.
func (c *Checker) reportCycle(typ types.Type, path []cycleEdge) {
	defer c.enter(c.typeDecl(typ))()
	start := 0
	for i, e := range path {
		if e.from == typ {
			start = i
			break
		}
	}
	notes := make([]diag.Diagnostic, 0, len(path)-start)
	msg := "invalid recursive type " + c.typeName(typ)
	for _, e := range path[start:] {
		if e.via != "" {
			msg += ": a type cannot hold itself, even through " + e.via
			break
		}
	}
	for i, e := range path[start:] {
		to := typ
		if next := start + i + 1; next < len(path) {
			to = path[next].from
		}
		notes = append(notes, diag.Diagnostic{
			Severity: diag.Note,
			Message:  c.typeName(e.from) + " refers to " + c.typeName(to),
			Span:     e.span,
		})
	}
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  msg,
		Span:     c.typeDecl(typ).NameSpan,
		Related:  notes,
	})
}

// selector checks a field read as a value, x.f, and returns its type, or
// a union built from a case with no payload, Expr.eof (see construction).
func (c *Checker) selector(sel *ast.Selector) types.Type {
	if _, _, _, built := ast.Construction(sel); built {
		return c.construction(sel)
	}
	sel.Type = c.field(sel, c.valueOf(sel.X, c.expr(sel.X)))
	return sel.Type
}

// field returns the type of the field sel names of a value of type base,
// which must be a struct with a field of that name, visible from the
// package being checked. A method of base named without a call is
// reported as such: there are no method values.
func (c *Checker) field(sel *ast.Selector, base types.Type) types.Type {
	if base == types.Invalid {
		return types.Invalid
	}
	if c.table.IsStruct(base) {
		if !c.resolveFields(base, sel.Span) {
			return types.Invalid
		}
	}
	f, i, ok := c.table.Field(base, sel.Name)
	if !ok && c.table.IsTypeParam(base) {
		c.typeParamField(sel, base)
		return types.Invalid
	}
	if !ok {
		if td := c.typeDecl(base); td != nil && td.Method(sel.Name) != nil {
			c.errorf(sel.NameSpan, "%s is a method of %s, not a field; a method can only be called",
				sel.Name, c.typeName(base))
			return types.Invalid
		}
		c.errorf(sel.NameSpan, "%s has no field %s", c.typeName(base), sel.Name)
		return types.Invalid
	}
	if msg := c.hiddenField(base, i, sel.Name); msg != "" {
		c.errorf(sel.NameSpan, "%s", msg)
		return types.Invalid
	}
	return f.Type
}

// fieldTarget checks a field being assigned, x.f: x must be a mutable
// variable, or an element or field of one, since a struct follows its
// variable as an array does. It returns the field's type.
func (c *Checker) fieldTarget(sel *ast.Selector) (types.Type, bool) {
	base, ok := c.containerTarget(sel.X, "field")
	if !ok {
		return types.Invalid, false
	}
	sel.Type = c.field(sel, base)
	return sel.Type, sel.Type != types.Invalid
}

// structLit checks the elements of a struct literal of the struct type typ:
// each a field's name and a value usable as that field's type, every field
// named exactly once. Every field must be visible where the literal is
// written; if one is not, only the values are checked. An element with no name, or with a key that is not a
// name, is reported, and then a missing field is not, since which field the
// element meant cannot be known.
func (c *Checker) structLit(lit *ast.CompositeLit, typ types.Type) types.Type {
	name := c.typeName(typ)
	if !c.resolveFields(typ, lit.Span) {
		c.discardElems(lit)
		lit.Type = types.Invalid
		return types.Invalid
	}

	at := lit.Span
	if lit.TypeExpr != nil {
		at = lit.TypeExpr.Spanned()
	}
	if !c.checkLiteralFields(typ, at) {
		c.discardElems(lit)
		return typ
	}

	set := make(map[string]bool)
	unnamed := false
	for _, e := range lit.Elems {
		kv, ok := e.(*ast.KeyValue)
		if !ok {
			c.errorf(e.Spanned(), "missing field name in %s literal", name)
			c.discard(e)
			unnamed = true
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || kv.KeyParens || key.Pkg != "" {
			c.errorf(kv.Key.Spanned(), "invalid field name in %s literal", name)
			c.discard(kv.Value)
			unnamed = true
			continue
		}
		c.fieldValue(kv, key, typ, set)
	}
	if !unnamed {
		c.reportMissing(lit, typ, set)
	}
	return typ
}

// fieldValue checks one element of a struct literal of type typ, key: value,
// recording the field in set, which holds the fields named before it.
func (c *Checker) fieldValue(
	kv *ast.KeyValue,
	key *ast.Ident,
	typ types.Type,
	set map[string]bool,
) {
	name := c.typeName(typ)
	f, _, ok := c.table.Field(typ, key.Name)
	switch {
	case !ok:
		c.errorf(key.Span, "%s has no field %s", name, key.Name)
		c.discard(kv.Value)
	case set[key.Name]:
		c.errorf(key.Span, "duplicate field %s in %s literal", key.Name, name)
		c.discard(kv.Value)
	default:
		set[key.Name] = true
		c.use(kv.Value, c.expr(kv.Value), f.Type, "in field "+key.Name+" of "+name+" literal")
	}
}

// reportMissing reports the fields of typ a literal of it leaves out, set
// being those it names, in one error listing them in declaration order.
func (c *Checker) reportMissing(lit *ast.CompositeLit, typ types.Type, set map[string]bool) {
	var missing []string
	for _, f := range c.table.Fields(typ) {
		if !set[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		c.errorf(lit.Span, "%s literal is missing %s %s",
			c.typeName(typ), plural(len(missing), "field"), strings.Join(missing, ", "))
	}
}

// discardElems checks the elements of a literal whose type is unknown, for
// errors of their own, keys aside, which name nothing.
func (c *Checker) discardElems(lit *ast.CompositeLit) {
	for _, e := range lit.Elems {
		if kv, ok := e.(*ast.KeyValue); ok {
			e = kv.Value
		}
		c.discard(e)
	}
}

// foldStruct computes a constant struct literal, as a []any of its fields'
// values in declaration order, whatever order the literal names them in.
func (c *Checker) foldStruct(lit *ast.CompositeLit) (any, bool) {
	values := make([]any, len(c.table.Fields(lit.Type)))
	for _, e := range lit.Elems {
		kv, ok := e.(*ast.KeyValue)
		if !ok {
			return nil, false
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return nil, false
		}
		_, i, found := c.table.Field(lit.Type, key.Name)
		v, folded := c.fold(kv.Value)
		if !found || !folded {
			return nil, false
		}
		values[i] = v
	}
	return values, true
}

// ensureGlobal checks the package-level variable v, unless it has been
// already. Globals are checked in order, and nothing checked before them
// names one, since a struct's field lengths name no variable, but a
// global's type may name any global of its package, declared before or
// after it. Checking on first need keeps the order of declarations from
// mattering; needing v while it is being checked, at, means it depends on
// itself, which is reported.
func (c *Checker) ensureGlobal(v *ast.Var, at ast.Span) {
	switch c.globalState[v] {
	case checked:
		return
	case checking:
		c.errorf(at, "initialization cycle: %s", c.initCycle(v))
		return
	}
	c.globalState[v] = checking
	c.initStack = append(c.initStack, v)
	restore := c.enter(v)
	c.checkGlobal(v)
	restore()
	c.initStack = c.initStack[:len(c.initStack)-1]
	c.globalState[v] = checked
}

// initCycle describes the cycle that needing v, which is being checked,
// closes: "a refers to itself", or every global from v round to v again,
// "a refers to b refers to a". A cycle never leaves a package, since
// imports have none, so its globals are named unqualified.
func (c *Checker) initCycle(v *ast.Var) string {
	start := 0
	for i, w := range c.initStack {
		if w == v {
			start = i
		}
	}
	if start == len(c.initStack)-1 {
		return v.Name + " refers to itself"
	}
	names := make([]string, 0, len(c.initStack)-start+1)
	for _, w := range c.initStack[start:] {
		names = append(names, w.Name)
	}
	return strings.Join(append(names, v.Name), " refers to ")
}

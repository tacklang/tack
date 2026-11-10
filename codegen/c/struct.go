package c

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// emitTypes defines a C struct for every array, struct and union type the
// program uses, after tack_fnptr, which any of them may hold, and before
// the pointer type of every function type (see funcvalue.go), then the
// string comparison helper if the program compares
// strings, directly or inside those types, then the equality helper of
// every one == compares, then the free helper of every union holding a
// list and of every list of owned elements. A type is
// defined after every type it holds, since C needs a member's type complete
// before the struct that holds it; the checker has rejected a struct that
// holds itself, so this order exists. Each type is then emitted in the
// order the checker met it, unless something it holds came later.
func (g *generator) emitTypes() {
	defined := make(map[types.Type]bool)
	g.emitFuncPtr()
	for _, typ := range g.emittedTypes() {
		g.emitTypeDef(typ, defined)
	}
	g.emitFuncTypes()

	compared := g.comparedTypes()
	for _, key := range g.keyTypes() {
		g.markCompared(key, compared)
	}
	if g.comparesStrings(compared) {
		g.emitStringCompare()
	}
	emitted := make(map[types.Type]bool)
	// error is no type the Table added, so it is not among them, though
	// == may compare two.
	if compared[types.Error] {
		g.emitEqualityOf(types.Error, emitted)
	}
	for _, typ := range g.emittedTypes() {
		if compared[typ] {
			g.emitEqualityOf(typ, emitted)
		}
	}
	g.emitFrees()
	g.emitMaps()
}

// emittedTypes returns every type the Table added that C has, in the order
// they were added: every one but those made of a type parameter, list[T]
// or map[string]T in a generic function's body, which C has only as the
// types they stand for in each instance, each added to the Table as its
// instance is generated, which is before any type is written.
func (g *generator) emittedTypes() []types.Type {
	table := g.prog.Types
	out := make([]types.Type, 0, table.Count())
	for i := range table.Count() {
		if typ := types.Added(i); !table.HasTypeParam(typ) {
			out = append(out, typ)
		}
	}
	return out
}

// isComposite reports whether typ is an array, a struct or a union type,
// which lowers to a C struct of its own.
func (g *generator) isComposite(typ types.Type) bool {
	table := g.prog.Types
	return table.IsArray(typ) || table.IsStruct(typ) || table.IsUnion(typ)
}

// hasEqualityHelper reports whether two values of type typ are compared
// through an equality helper, tack_eq_ (see equalityName): an array, a
// struct, a union or a list. Every other comparable type compares with
// C's == or the string comparison helper.
func (g *generator) hasEqualityHelper(typ types.Type) bool {
	return g.isComposite(typ) || g.prog.Types.IsList(typ)
}

// parts returns the types a value of type typ is made of: an array's, a
// view's or a list's element type, a map's key and value types, a
// struct's field types, or a union's payloads' types, in order.
func (g *generator) parts(typ types.Type) []types.Type {
	table := g.prog.Types
	if table.IsArray(typ) || table.IsView(typ) || table.IsList(typ) {
		return []types.Type{table.Elem(typ)}
	}
	if table.IsMap(typ) {
		return []types.Type{table.Key(typ), table.Elem(typ)}
	}
	if table.IsUnion(typ) {
		return g.payloads(typ)
	}
	fields := table.Fields(typ)
	out := make([]types.Type, len(fields))
	for i, f := range fields {
		out[i] = f.Type
	}
	return out
}

// emitTypeDef defines the C struct for typ, an array, a view, a list, a
// struct or a union,
// after those of the types it is made of, unless defined says it has been already. typ is marked before
// its parts are visited, so even a type that held itself could not loop.
// error's struct, tack_error, is the runtime's tack.h's, so it is never defined
// here.
func (g *generator) emitTypeDef(typ types.Type, defined map[types.Type]bool) {
	table := g.prog.Types
	if defined[typ] || typ == types.Error ||
		!g.isComposite(typ) && !table.IsView(typ) && !table.IsList(typ) && !table.IsMap(typ) {
		return
	}
	defined[typ] = true
	for _, part := range g.parts(typ) {
		g.emitTypeDef(part, defined)
	}
	g.emitDef(typ)
}

// emitDef defines the C struct for typ, an array, a view, a list, a
// struct or a union, whose parts are defined already.
func (g *generator) emitDef(typ types.Type) {
	table := g.prog.Types
	switch {
	case table.IsView(typ):
		g.emitViewDef(typ)
	case table.IsList(typ):
		g.emitListDef(typ)
	case table.IsMap(typ):
		g.emitMapDef(typ)
	case table.IsArray(typ):
		g.emitArrayDef(typ)
	case table.IsUnion(typ):
		g.emitUnionDef(typ)
	default:
		g.emitStructDef(typ)
	}
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
		fmt.Fprintf(&g.buf, "    %s %s;\n", g.cType(f.Type), memberName(typ, f.Name))
	}
	fmt.Fprintf(&g.buf, "} %s;\n\n", g.structName(typ))
}

// comparedTypes returns every array, struct, union and list type the
// program compares with == or !=, and every such type they are made of,
// which their helpers compare in turn. emitTypes adds the key types of
// maps, whose helpers compare keys.
func (g *generator) comparedTypes() map[types.Type]bool {
	compared := make(map[types.Type]bool)
	g.walk(func(n ast.Node) bool {
		if bin, ok := n.(*ast.Binary); ok && (bin.Op == ast.Eq || bin.Op == ast.Ne) {
			g.markCompared(ast.TypeOf(bin.X), compared)
		}
		if typ, ok := switchCompares(n); ok && !g.isUnionSwitch(n.(*ast.Switch)) {
			g.markCompared(typ, compared)
		}
		return true
	})
	return compared
}

// markCompared records in compared that values of typ, and of every type
// it is made of, are compared, when it has an equality helper.
func (g *generator) markCompared(typ types.Type, compared map[types.Type]bool) {
	if compared[typ] || !g.hasEqualityHelper(typ) {
		return
	}
	compared[typ] = true
	for _, part := range g.parts(typ) {
		g.markCompared(part, compared)
	}
}

// emitEqualityOf writes the equality helper for typ, after those of the
// types it is made of, which it calls, unless emitted says it has been
// written already.
func (g *generator) emitEqualityOf(typ types.Type, emitted map[types.Type]bool) {
	if emitted[typ] || !g.hasEqualityHelper(typ) {
		return
	}
	emitted[typ] = true
	for _, part := range g.parts(typ) {
		g.emitEqualityOf(part, emitted)
	}
	if g.prog.Types.IsArray(typ) || g.prog.Types.IsList(typ) {
		g.emitArrayEquality(typ)
		return
	}
	if g.prog.Types.IsUnion(typ) {
		g.emitUnionEquality(typ)
		return
	}
	g.emitStructEquality(typ)
}

// emitStructEquality writes tack_eq_ for a struct type, which compares two
// structs field by field, in order, through pointers to them (see
// equalityName). A struct with no fields has nothing to compare, and its
// two of them are always equal.
func (g *generator) emitStructEquality(typ types.Type) {
	name := g.structName(typ)
	fmt.Fprintf(
		&g.buf,
		"static bool %s(const %s *a, const %s *b) {\n",
		g.equalityName(typ),
		name,
		name,
	)
	fields := g.prog.Types.Fields(typ)
	if len(fields) == 0 {
		g.buf.WriteString("    (void)a;\n    (void)b;\n    return true;\n}\n\n")
		return
	}
	tests := make([]string, len(fields))
	for i, f := range fields {
		member := memberName(typ, f.Name)
		tests[i] = g.equalExpr(f.Type, "a->"+member, "b->"+member)
	}
	fmt.Fprintf(&g.buf, "    return %s;\n}\n\n", strings.Join(tests, " && "))
}

// equalExpr renders a C test of whether l and r, two values of type typ,
// each a place an equality helper reads through its pointers, are equal:
// the type's equality helper, given their addresses, for an array, a
// struct, a union or a list, the string comparison helper giving zero for
// a string, and C's == otherwise.
func (g *generator) equalExpr(typ types.Type, l, r string) string {
	if g.hasEqualityHelper(typ) {
		return fmt.Sprintf("%s(&%s, &%s)", g.equalityName(typ), l, r)
	}
	if typ == types.String {
		return stringComparison(ast.Eq, l, r)
	}
	return fmt.Sprintf("%s == %s", l, r)
}

// structName is the name of the C struct a struct type lowers to: the
// struct's name, mangled as a package-level name is. A struct shares one
// namespace with functions and package-level variables, so the name is
// never another declaration's. error's is tack_error, which the runtime's tack.h
// defines.
func (g *generator) structName(typ types.Type) string {
	typ = g.prog.Types.Bound(typ)
	if typ == types.Error {
		return basicCType(typ)
	}
	if td := g.structs[typ]; td != nil {
		return g.mangle(td, td.Name)
	}
	return g.mangle(nil, g.prog.Types.Name(typ))
}

// memberName is the C member a field called name of the struct type typ
// lowers to. A declared struct's keeps clear of C's keywords, for the
// same reason paramName keeps a parameter's name clear: a Tack field may
// be called int or while, so it is f_ and the name. error's fields are
// tack_error's members, code and msg, as the runtime's tack.h names them.
func memberName(typ types.Type, name string) string {
	if typ == types.Error {
		return name
	}
	return "f_" + name
}

// structLit renders a struct literal as a C compound literal of its
// struct, each field set by a designated initializer in the order the
// literal names them. A struct with no fields gets its one unused member
// zeroed.
func (g *generator) structLit(lit *ast.CompositeLit) (string, []diag.Diagnostic) {
	if len(lit.Elems) == 0 {
		return fmt.Sprintf("(%s){}", g.structName(lit.Type)), nil
	}
	inits := make([]string, len(lit.Elems))
	values := make([]ast.Expr, len(lit.Elems))
	fields := make([]string, len(lit.Elems))
	for i, e := range lit.Elems {
		if kv, isKV := e.(*ast.KeyValue); isKV {
			values[i] = kv.Value
		}
	}
	guard := g.guardOwned(values)
	defer guard.done()
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
		values[i], fields[i], inits[i] = kv.Value, memberName(lit.Type, key.Name), guard.hold(i, v)
	}
	return g.sequencedLit(values, inits, func(ops []string) string {
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
// already bind. A union built of a case with no payload, Expr.eof, is a
// compound literal of the union (see constructionValue).
func (g *generator) selectorExpr(sel *ast.Selector) (string, []diag.Diagnostic) {
	if _, _, _, built := ast.Construction(sel); built {
		return g.constructionValue(sel)
	}
	base, diags := g.expr(sel.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	member := memberName(ast.TypeOf(sel.X), sel.Name)
	switch sel.X.(type) {
	case *ast.Ident, *ast.Index, *ast.Selector, *ast.Call:
		return base + "." + member, nil
	default:
		return "(" + base + ")." + member, nil
	}
}

// constOf renders a computed constant of type typ as a C constant
// expression, usable to initialize a static variable: an array's or a
// struct's as a brace-enclosed initializer, which a static variable needs
// instead of a compound literal, built from its elements or fields in
// order. c_allocator, folded to its literal, is tack_allocator's index 0
// and generation 0, as TACK_C_ALLOCATOR is. No elements or fields is a zeroed initializer, which also fills
// the one member an empty array or struct holds, and so is an error with
// code 0 and an empty message, noerror, whose message has no string in
// the pool to point at.
func (g *generator) constOf(typ types.Type, c any) (string, bool) {
	if rendered, ok, special := g.specialConst(typ, c); special {
		return rendered, ok
	}
	values, isList := c.([]any)
	if !isList || !g.isComposite(typ) {
		return g.constValue(c)
	}
	if zeroed(typ, values) {
		return "{}", true
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

// deepGlobal adds to g.globalInits the C statements that give v, a
// global whose initializer would nest deeper than maxDepth, its computed
// value, and reports whether it has one. clang checks an initializer one
// level of subobject at a time, recursively, and runs out of stack past a
// few hundred, even when the initializer names each scalar by its whole
// path rather than nesting braces; and a static variable has no function
// to hold its parts in temporaries, as an expression's are (see
// exprSteps). So v starts zeroed, and C's main gives it its value before
// Tack's main runs, in a block of its own that builds the value as
// constOf writes it, but with each part nested deeper than maxDepth held
// in a local temporary first (see heldConst), so no statement nests
// deep and the C grows only as the value does. Every other global's
// initializer was computed by the checker, so none reads v before then,
// and Tack code cannot.
func (g *generator) deepGlobal(v *ast.Var) bool {
	var steps []string
	value, ok := g.heldConst(v.Type, v.Const, &steps)
	if !ok {
		return false
	}
	g.globalInits = append(g.globalInits, "{")
	for _, step := range steps {
		g.globalInits = append(g.globalInits, "    "+step)
	}
	g.globalInits = append(g.globalInits,
		fmt.Sprintf("    %s = %s;", g.mangle(v, v.Name), compound(g.cType(v.Type), value)), "}")
	return true
}

// heldConst renders c, the computed value of type typ, as constOf does,
// but appends to steps a declaration of a local temporary initialized
// with each part whose C nests deeper than half of maxDepth, in the
// order the parts are found, inner ones first, and writes the temporary
// in the part's place. Each temporary is declared before the one that
// reads it, so the steps run in order.
func (g *generator) heldConst(typ types.Type, c any, steps *[]string) (string, bool) {
	rendered, ok := g.heldParts(typ, c, steps)
	if !ok || cDepth(rendered) <= maxDepth/2 {
		return rendered, ok
	}
	t := g.temp("g")
	*steps = append(*steps, fmt.Sprintf("%s %s = %s;", g.cType(typ), t, rendered))
	return t, true
}

// heldParts renders c, of type typ, as constOf does, its parts each
// rendered by heldConst, so a deep one is a temporary.
func (g *generator) heldParts(typ types.Type, c any, steps *[]string) (string, bool) {
	if u, isUnion := c.(ast.UnionConst); isUnion {
		return g.heldUnion(typ, u, steps)
	}
	values, isList := c.([]any)
	if !isList || !g.isComposite(typ) || zeroed(typ, values) {
		return g.constOf(typ, c)
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
		rendered, ok := g.heldConst(part, v, steps)
		if !ok {
			return "", false
		}
		out[i] = rendered
	}
	return fmt.Sprintf(layout, strings.Join(out, ", ")), true
}

// heldUnion renders u, a union's computed value of type typ, as
// unionConst does, its payload rendered by heldConst.
func (g *generator) heldUnion(typ types.Type, u ast.UnionConst, steps *[]string) (string, bool) {
	cases := g.prog.Types.Cases(typ)
	if u.Case < 0 || u.Case >= len(cases) {
		return "", false
	}
	uc := cases[u.Case]
	if !uc.Payload || u.Payload == nil {
		return fmt.Sprintf("{.tag = %d}", u.Case), true
	}
	payload, ok := g.heldConst(uc.Type, u.Payload, steps)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("{.tag = %d, .u.%s = %s}", u.Case, caseMember(uc.Name), payload), true
}

// compound returns value, an initializer of the C type ctype or an
// expression of it, as an expression of it: a compound literal for an
// initializer, which starts with a brace, and value itself otherwise.
func compound(ctype, value string) string {
	if strings.HasPrefix(value, "{") {
		return "(" + ctype + ")" + value
	}
	return value
}

// specialConst renders c, the computed value of a static variable of type
// typ, when it is c_allocator's, TACK_C_ALLOCATOR's value written as an
// initializer, which a static variable needs instead of std's compound
// literal, or a union's (see unionConst), and reports whether it was one.
func (g *generator) specialConst(typ types.Type, c any) (rendered string, ok, special bool) {
	switch v := c.(type) {
	case *ast.CAllocatorLit:
		return "{0, 0}", true, true
	case ast.UnionConst:
		rendered, ok = g.unionConst(typ, v)
		return rendered, ok, true
	default:
		return "", false, false
	}
}

// zeroed reports whether values, the computed elements or fields of a
// value of type typ, are written as a zeroed initializer: there are none,
// or typ is error and they are noerror's.
func zeroed(typ types.Type, values []any) bool {
	return len(values) == 0 || typ == types.Error && isNoError(values)
}

// isNoError reports whether values, an error's computed fields, are
// noerror's: code 0 and the empty message.
func isNoError(values []any) bool {
	if len(values) != 2 {
		return false
	}
	code, isInt := values[0].(constant.Int)
	msg, isString := values[1].(string)
	return isInt && code.IsZero() && isString && msg == ""
}

// addressOf renders the address of the value of e, of the type typ,
// rendered as rendered, for an equality helper to read through: rendered
// itself, when it is a place C can take the address of, a variable, a
// temporary, a field or an element of one, or a compound literal, and
// otherwise a temporary of typ assigned rendered, which the comparison's
// own order already evaluates where e is evaluated. An expression that
// is a place in Tack is not one in C once its rendering runs steps before
// it, as a literal holding two calls does, (o0 = f(1), o1 = f(2),
// (S){...}), whose value is a comma expression's (see notPlace).
func (g *generator) addressOf(e ast.Expr, typ types.Type, rendered string) string {
	if isCName(rendered) || addressable(e) && !notPlace(rendered) {
		return "&" + rendered
	}
	t := g.temp("q")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.cType(typ), t))
	return fmt.Sprintf("(%s = %s, &%s)", t, rendered, t)
}

// addressable reports whether the C rendered from e is a place C can take
// the address of: a variable or parameter, a field or an element of one,
// at any depth, an element of a list or a view, which is always one, or
// a struct or array literal, noerror or a union's construction, which are
// compound literals.
func addressable(e ast.Expr) bool {
	if _, _, _, built := ast.Construction(e); built {
		return true
	}
	switch x := e.(type) {
	case *ast.CompositeLit, *ast.NoErrorLit:
		return true
	case *ast.Ident:
		return true
	case *ast.Selector:
		return addressable(x.X)
	case *ast.Index:
		return addressable(x.X)
	default:
		return false
	}
}

// notPlace reports whether rendered, the C of an expression addressable
// reports a place, gives its value through an operator whose result C
// does not count as a place: a comma, a conditional or an assignment,
// outside every call's arguments, index and initializer, where one is
// only part of what the place is built from. Grouping parentheses do not
// hide one, since (a, b) and ((a, b)).f are no more places than a, b is.
// A character or string literal is skipped whole.
func notPlace(rendered string) bool {
	// open holds, for every bracket open around i, innermost last, 1 when
	// it is not a grouping parenthesis and 0 when it is; enclosed is
	// their sum, how many of them are not.
	enclosed, open := 0, []int(nil)
	for i := 0; i < len(rendered); i++ {
		switch rendered[i] {
		case '\'', '"':
			i = closingQuote(rendered, i)
		case '(', '[', '{':
			open = append(open, encloses(rendered, i))
			enclosed += open[len(open)-1]
		case ')', ']', '}':
			if n := len(open); n > 0 {
				enclosed -= open[n-1]
				open = open[:n-1]
			}
		case ',', '?', '=':
			if enclosed == 0 {
				return true
			}
		}
	}
	return false
}

// encloses is 1 when the bracket at i in s opens a call's arguments, an
// index or an initializer, and 0 when it is a parenthesis that groups or
// casts: one right after a name or a closing bracket opens a call's
// arguments, and any other groups or casts.
func encloses(s string, i int) int {
	if s[i] != '(' || i > 0 && (isIdentByte(s[i-1]) || s[i-1] == ')' || s[i-1] == ']') {
		return 1
	}
	return 0
}

// closingQuote returns the index in s of the quote that ends the
// character or string literal whose opening quote is at i, past every
// escaped character, or the last index of s when none does.
func closingQuote(s string, i int) int {
	quote := s[i]
	for i++; i < len(s) && s[i] != quote; i++ {
		if s[i] == '\\' {
			i++
		}
	}
	return min(i, len(s)-1)
}

// isCName reports whether s is a C identifier, such as the name of a
// temporary an operand is held in.
func isCName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c != '_' && !unicode.IsLetter(c) && (i == 0 || !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

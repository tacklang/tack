package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A union type lowers to a C struct of its own, named as a struct type is
// (see structName), holding its tag, the position of the case it holds
// among the union's cases, counting from 0, and a C union of its cases'
// payloads, one member each, c_ and the case's name, which keeps it clear
// of C's keywords as a field's f_ does:
//
//	typedef struct {
//	    uint8_t tag;
//	    union {
//	        int64_t c_int;
//	        tack_string c_name;
//	    } u;
//	} tack_4_main4_Expr;
//
// A case with no payload has no member, and a union none of whose cases
// has one holds one unused member, since C has no empty union. The tag is
// the narrowest of uint8_t, uint16_t and uint32_t that holds every case's
// position. A union is built as a compound literal setting the tag and
// the payload, (T){.tag = 0, .u.c_int = 5}, or the tag alone.
//
// Two unions compare with tack_eq_, which compares the tags and then, for
// the case both hold, the payloads, as == compares a value of the
// payload's type. A union holding a list is freed by tack_free_, which
// frees what the case it holds holds, as a struct's lists are freed: a
// moved union is zeroed, so its tag is 0 and every list in its payload
// empty, and freeing it does nothing.
//
// A switch on a union holds a pointer to the union in a temporary: to the
// switched place itself when it is a place whose address cannot change
// while the switch runs, and to a copy of the value otherwise, a call's
// result, or a mutable global's, which a call in a clause could assign.
// Each clause tests the tag, and a clause binding a payload declares its
// variable as a pointer to the payload, read as (*v_n), so it refers to
// the payload in place, which nothing can change while the clause runs
// (see typeck). A switch whose clauses name every case lowers its last
// clause as the final else, so C sees no path through the switch that
// runs no clause, and one on a union with no cases, which has no value
// and so is never reached, as __builtin_unreachable():
//
//	{
//	    const tack_4_main4_Expr *const tack_s0 = &v_e;
//	    if (tack_s0->tag == 0) {
//	        const int64_t *const v_n = &tack_s0->u.c_int;
//	        body
//	    } else {
//	        body
//	    }
//	}

// unionTagType returns the C type a union's tag is held in, for a union of
// n cases: the narrowest unsigned type that holds every position.
func unionTagType(n int) string {
	switch {
	case n <= 1<<8:
		return "uint8_t"
	case n <= 1<<16:
		return "uint16_t"
	default:
		return "uint32_t"
	}
}

// caseMember is the C member of a union's payload union that the payload
// of the case called name is held in.
func caseMember(name string) string { return "c_" + name }

// emitUnionDef defines the C struct a union type lowers to (see above).
func (g *generator) emitUnionDef(typ types.Type) {
	cases := g.prog.Types.Cases(typ)
	fmt.Fprintf(&g.buf, "typedef struct {\n    %s tag;\n    union {\n", unionTagType(len(cases)))
	members := 0
	for _, uc := range cases {
		if uc.Payload {
			fmt.Fprintf(&g.buf, "        %s %s;\n", g.cType(uc.Type), caseMember(uc.Name))
			members++
		}
	}
	if members == 0 {
		g.buf.WriteString("        char unused;\n")
	}
	fmt.Fprintf(&g.buf, "    } u;\n} %s;\n\n", g.structName(typ))
}

// payloads returns the types of the payloads of the union type typ, in
// order, a case with no payload having none.
func (g *generator) payloads(typ types.Type) []types.Type {
	var out []types.Type
	for _, uc := range g.prog.Types.Cases(typ) {
		if uc.Payload {
			out = append(out, uc.Type)
		}
	}
	return out
}

// emitUnionEquality writes tack_eq_ for a union type: two unions are
// equal when they hold the same case and, for a case with a payload,
// equal payloads. It reads them through pointers (see equalityName).
func (g *generator) emitUnionEquality(typ types.Type) {
	name := g.structName(typ)
	fmt.Fprintf(
		&g.buf,
		"static bool %s(const %s *a, const %s *b) {\n",
		g.equalityName(typ),
		name,
		name,
	)
	g.buf.WriteString("    if (a->tag != b->tag) {\n        return false;\n    }\n")
	g.buf.WriteString("    switch (a->tag) {\n")
	for i, uc := range g.prog.Types.Cases(typ) {
		if !uc.Payload {
			continue
		}
		member := "->u." + caseMember(uc.Name)
		fmt.Fprintf(&g.buf, "    case %d:\n        return %s;\n", i,
			g.equalExpr(uc.Type, "a"+member, "b"+member))
	}
	g.buf.WriteString("    default:\n        return true;\n    }\n}\n\n")
}

// freeName is the name of the helper that frees what a union of type typ,
// which holds a list, holds, or a list of type typ, whose elements are
// owned, and its elements. It takes a const pointer to the value, as an
// equality helper does (see equalityName), so a chain of types nested
// one in another frees in frames of one small size.
func freeNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_free_%d", typ)
}

// emitFrees writes tack_free_ for every union type that holds a list,
// every list type whose elements are owned, every map type, every arena
// type and every pool type, each after those of the
// types its payloads or elements, and the fields of those, hold, which
// it calls.
func (g *generator) emitFrees() {
	table := g.prog.Types
	emitted := make(map[types.Type]bool)
	var emit func(typ types.Type)
	emit = func(typ types.Type) {
		if emitted[typ] || !g.owned(typ) {
			return
		}
		emitted[typ] = true
		for _, part := range g.parts(typ) {
			emit(part)
		}
		switch {
		case table.IsUnion(typ):
			g.emitUnionFree(typ)
		case table.IsList(typ) && g.owned(table.Elem(typ)):
			g.emitListFree(typ)
		case table.IsMap(typ):
			g.emitMapFree(typ)
		case table.IsArena(typ):
			g.emitArenaFree(typ)
		case table.IsPool(typ):
			g.emitPoolFree(typ)
		}
	}
	for _, typ := range g.emittedTypes() {
		emit(typ)
	}
}

// emitUnionFree writes tack_free_ for the union type typ, which holds a
// list: it frees the lists the payload of the case the union holds holds,
// and nothing for a case whose payload holds none.
func (g *generator) emitUnionFree(typ types.Type) {
	free := g.freeName(typ)
	fmt.Fprintf(
		&g.buf,
		"%sstatic void %s(const %s *v) {\n    switch (v->tag) {\n",
		g.maybeUnused(free),
		free,
		g.structName(typ),
	)
	for i, uc := range g.prog.Types.Cases(typ) {
		if !uc.Payload || !g.owned(uc.Type) {
			continue
		}
		fmt.Fprintf(&g.buf, "    case %d:\n", i)
		for _, line := range g.frees(uc.Type, "v->u."+caseMember(uc.Name)) {
			fmt.Fprintf(&g.buf, "        %s\n", line)
		}
		g.buf.WriteString("        break;\n")
	}
	g.buf.WriteString("    default:\n        break;\n    }\n}\n\n")
}

// constructionValue renders e, a union built of a case and its payload,
// or of a case with no payload, as a compound literal of its C struct.
func (g *generator) constructionValue(e ast.Expr) (string, []diag.Diagnostic) {
	td, sel, payload, _ := ast.Construction(e)
	uc, i, ok := g.prog.Types.Case(td.Type, sel.Name)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s has no case %s", td.Name, sel.Name),
			Span:     sel.NameSpan,
		}}
	}
	name := g.structName(td.Type)
	if !uc.Payload || payload == nil {
		return fmt.Sprintf("((%s){.tag = %d})", name, i), nil
	}
	v, diags := g.expr(payload, uc.Type)
	if len(diags) > 0 {
		return "", diags
	}
	return fmt.Sprintf("((%s){.tag = %d, .u.%s = %s})", name, i, caseMember(uc.Name), v), nil
}

// unionConst renders c, the computed value of a package-level union of
// type typ, as a static initializer: its tag, and its payload as constOf
// renders a value of the payload's type.
func (g *generator) unionConst(typ types.Type, c ast.UnionConst) (string, bool) {
	cases := g.prog.Types.Cases(typ)
	if c.Case < 0 || c.Case >= len(cases) {
		return "", false
	}
	uc := cases[c.Case]
	if !uc.Payload || c.Payload == nil {
		return fmt.Sprintf("{.tag = %d}", c.Case), true
	}
	payload, ok := g.constOf(uc.Type, c.Payload)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("{.tag = %d, .u.%s = %s}", c.Case, caseMember(uc.Name), payload), true
}

// isUnionSwitch reports whether s is a switch on a union, which matches
// its cases rather than comparing its tag with values.
func (g *generator) isUnionSwitch(s *ast.Switch) bool {
	return s.Tag != nil && !s.IsTypeSwitch() && g.prog.Types.IsUnion(ast.TypeOf(s.Tag))
}

// generateUnionSwitch lowers s, a switch on a union, in fn (see above).
// A switch whose clauses test nothing and bind nothing, its only clause a
// default or the one case of its union, holds no pointer, which nothing
// would read: its tag is evaluated, cast to void, for any call in it to
// run.
func (g *generator) generateUnionSwitch(s *ast.Switch, fn *ast.Func) ([]string, []diag.Diagnostic) {
	typ := ast.TypeOf(s.Tag)
	steps, place, diags := g.exprSteps(s.Tag, false)
	if len(diags) > 0 {
		return nil, diags
	}
	tag, addr := withSteps(steps, place), withSteps(steps, "&"+place)
	ref := g.temp("s")
	lines, used, diags := g.unionChain(s, typ, ref, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	eval := []string{fmt.Sprintf("(void)(%s);", tag)}
	if used {
		eval = g.holdUnion(s.Tag, tag, addr, ref)
	}
	out := append([]string{"{"}, indent(append(eval, lines...))...)
	return append(out, "}"), nil
}

// unionChain lowers the clauses of s, a switch on the union typ held
// through the pointer ref, in fn, to a chain of ifs, and reports whether
// anything in it reads ref: a test, or a binding.
func (g *generator) unionChain(
	s *ast.Switch,
	typ types.Type,
	ref string,
	fn *ast.Func,
) ([]string, bool, []diag.Diagnostic) {
	var lines []string
	used := false
	clauses := tryOrder(s)
	for i, cl := range clauses {
		last := i == len(clauses)-1 && (cl.Default || s.Exhaustive)
		body, bound, diags := g.unionClauseBody(typ, cl, ref, fn)
		if len(diags) > 0 {
			return nil, false, diags
		}
		header := "{"
		if !last {
			header = fmt.Sprintf("if (%s) {", g.unionTest(typ, cl, ref))
		}
		used = used || bound || !last
		if len(lines) > 0 {
			header = "} else " + header
		}
		lines = append(append(lines, header), indent(body)...)
	}
	return closeChain(lines, s), used, nil
}

// closeChain ends lines, the chain of ifs lowering the clauses of s, a
// switch on a union, with the closing brace of its last clause. A switch
// with no clause on a union with no cases, which has no value, is never
// reached, which C is told, as a function ending in it returns nothing.
func closeChain(lines []string, s *ast.Switch) []string {
	switch {
	case len(lines) > 0:
		return append(lines, "}")
	case s.Exhaustive:
		return []string{"__builtin_unreachable();"}
	default:
		return lines
	}
}

// holdUnion returns the declarations that hold the union tag, rendered as
// rendered, with its address addr, through the pointer ref: a pointer to
// the place itself when it can be matched in place (see matchesInPlace),
// and otherwise to a copy.
func (g *generator) holdUnion(tag ast.Expr, rendered, addr, ref string) []string {
	ctype := g.cType(ast.TypeOf(tag))
	if g.matchesInPlace(tag) {
		return []string{fmt.Sprintf("const %s *const %s = %s;", ctype, ref, addr)}
	}
	value := g.temp("u")
	return []string{
		fmt.Sprintf("const %s %s = %s;", ctype, value, rendered),
		fmt.Sprintf("const %s *const %s = &%s;", ctype, ref, value),
	}
}

// tryOrder returns a switch's cases in the order they are tried: every
// case with values in source order, then the default, if there is one.
func tryOrder(s *ast.Switch) []*ast.CaseClause {
	order := make([]*ast.CaseClause, 0, len(s.Cases))
	for _, cl := range s.Cases {
		if !cl.Default {
			order = append(order, cl)
		}
	}
	if def := s.Default(); def != nil {
		order = append(order, def)
	}
	return order
}

// matchesInPlace reports whether a switch on a union can hold a pointer
// to tag, the place it switches on, rather than to a copy: tag is a
// variable or a parameter, or a field or an element of one, rendered as
// a C lvalue, since it calls nothing and moves nothing, and its variable
// is no mutable global, which a call in a clause could assign.
func (g *generator) matchesInPlace(tag ast.Expr) bool {
	root := ast.Root(tag)
	if root == nil || callsFunc(tag) || moves(tag) {
		return false
	}
	v, isVar := root.Def.(*ast.Var)
	return !isVar || !g.globals[v] || !v.Mutable
}

// unionTest renders the C test of cl, a clause of a switch on the union
// typ held through the pointer ref: whether the tag is any of the cases
// it names.
func (g *generator) unionTest(typ types.Type, cl *ast.CaseClause, ref string) string {
	tests := make([]string, 0, len(cl.Values))
	for _, v := range cl.Values {
		name, _, _ := ast.CasePattern(v)
		_, i, _ := g.prog.Types.Case(typ, name.Name)
		tests = append(tests, fmt.Sprintf("%s->tag == %d", ref, i))
	}
	if len(tests) == 1 {
		return tests[0]
	}
	return "(" + strings.Join(tests, ") || (") + ")"
}

// unionClauseBody lowers the body of cl, a clause of a switch on the union
// typ held through the pointer ref, in fn, after the declaration of the
// variable it binds a payload to, when the body reads it: a pointer to
// the payload, through which every use of the variable reads it. It
// reports whether it declared one.
func (g *generator) unionClauseBody(
	typ types.Type,
	cl *ast.CaseClause,
	ref string,
	fn *ast.Func,
) ([]string, bool, []diag.Diagnostic) {
	bound := cl.Var != nil && len(cl.Values) == 1 && g.frame.read[cl.Var]
	if bound {
		g.inPlace[cl.Var] = true
	}
	body, diags := g.generateBlock(cl.Body, fn)
	if len(diags) > 0 || !bound {
		return body, false, diags
	}
	name, _, _ := ast.CasePattern(cl.Values[0])
	uc, _, _ := g.prog.Types.Case(typ, name.Name)
	decl := fmt.Sprintf("const %s *const %s = &%s->u.%s;",
		g.cType(uc.Type), declName(cl.Var), ref, caseMember(uc.Name))
	return append([]string{decl}, body...), true, nil
}

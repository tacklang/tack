package c

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// generateSwitch lowers a switch to a chain of C ifs, never to a C switch:
// Tack's break and continue in a case act on the enclosing loop, as they do
// in a C if, where in a C switch break would leave the switch, and a tag
// may be a string, an array or a struct, which a C switch cannot take. An
// expression switch evaluates its tag once, into a const temporary, in a C
// block that scopes it to the switch, and each case tests the temporary
// against its values with ||, which evaluates them left to right and stops
// at the first match:
//
//	{
//	    const int64_t tack_s0 = tag;
//	    if ((tack_s0 == 1) || (tack_s0 == 2)) {
//	        body
//	    } else if (tack_s0 == 3) {
//	        body
//	    } else {
//	        default body
//	    }
//	}
//
// A tagless switch tests its values themselves, with no block around the
// chain. The default, wherever it is written, is the final else, and a
// switch with only a default is its body in a C block of its own. A tag no
// case tests, and no binding reads, is still evaluated, cast to void, for
// any call in it to run.
//
// A type switch, switch v := x.(type), holds x, a tack_any, in the
// temporary, and a case tests its type id against each type's, as
// TACK_ANY_IS does. Its variable is declared first in the case's body:
// for a case naming one type T, a const T read through the any's pointer,
// which the id just tested says points at a T, and for any other case, and
// the default, a const copy of the tack_any. A case whose body does not
// read its variable declares none:
//
//	{
//	    const tack_any tack_s0 = p_a;
//	    if (tack_s0.type == TACK_TYPE_INT64) {
//	        const int64_t v_v = *(const int64_t *)tack_s0.ptr;
//	        body
//	    } else if ((tack_s0.type == TACK_TYPE_INT8) || (tack_s0.type == TACK_TYPE_BOOL)) {
//	        const tack_any v_v = tack_s0;
//	        body
//	    }
//	}
//
// A switch with a case ending in fallthrough is lowered by fallChain
// instead, its tag held the same way.
//
// A switch on a union is lowered by generateUnionSwitch.
func (g *generator) generateSwitch(s *ast.Switch, fn *ast.Func) ([]string, []diag.Diagnostic) {
	if g.isUnionSwitch(s) {
		return g.generateUnionSwitch(s, fn)
	}
	if s.Tag == nil {
		return g.caseChain(s, "", fn)
	}

	// A type switch's tag is x.(type), whose value is x's, an any.
	tagExpr := s.Tag
	if ta, ok := s.Tag.(*ast.TypeAssert); ok && s.IsTypeSwitch() {
		tagExpr = ta.X
	}
	tag, diags := g.expr(tagExpr, types.Invalid)
	if len(diags) > 0 {
		return nil, diags
	}
	tagRef := ""
	eval := fmt.Sprintf("(void)(%s);", tag)
	if hasValues(s) || s.Bind != nil {
		tagRef = g.temp("s")
		eval = fmt.Sprintf("const %s %s = %s;", g.cType(ast.TypeOf(tagExpr)), tagRef, tag)
	}

	chain, diags := g.caseChain(s, tagRef, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	lines := append([]string{"{"}, indent(append([]string{eval}, chain...))...)
	return append(lines, "}"), nil
}

// hasValues reports whether any case of a switch has a value, so that the
// tag is compared with something.
func hasValues(s *ast.Switch) bool {
	for _, cl := range s.Cases {
		if len(cl.Values) > 0 {
			return true
		}
	}
	return false
}

// caseChain lowers a switch's cases to an if, else if and else chain, the
// cases with values in source order and the default as the final else. In
// an expression switch, tagRef names the temporary holding the tag.
func (g *generator) caseChain(
	s *ast.Switch,
	tagRef string,
	fn *ast.Func,
) ([]string, []diag.Diagnostic) {
	if hasFallthrough(s) {
		return g.fallChain(s, tagRef, fn)
	}
	var lines []string
	for _, cl := range s.Cases {
		if cl.Default {
			continue
		}
		test, diags := g.caseTest(s, tagRef, cl)
		if len(diags) > 0 {
			return nil, diags
		}
		body, diags := g.caseBody(cl, tagRef, fn)
		if len(diags) > 0 {
			return nil, diags
		}
		header := fmt.Sprintf("if (%s) {", test)
		if len(lines) > 0 {
			header = "} else " + header
		}
		lines = append(append(lines, header), indent(body)...)
	}

	def := s.Default()
	if def == nil {
		if len(lines) > 0 {
			lines = append(lines, "}")
		}
		return lines, nil
	}
	body, diags := g.caseBody(def, tagRef, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	open := "{"
	if len(lines) > 0 {
		open = "} else {"
	}
	lines = append(append(lines, open), indent(body)...)
	return append(lines, "}"), nil
}

// hasFallthrough reports whether a case of s ends in a fallthrough into
// the next case.
func hasFallthrough(s *ast.Switch) bool {
	for _, cl := range s.Cases {
		if ast.FallsInto(cl.Body) != nil {
			return true
		}
	}
	return false
}

// fallChain lowers the cases of s, a switch with a case ending in
// fallthrough, as gotos to their bodies: first a test of each case with
// values, in source order, each jumping to its case's label when it
// matches, then a jump to the default's label, or past every body when
// there is none; then each body, in source order, in a C block of its own
// after its label, so it runs its defers and frees its locals as it ends.
// A body that ends in fallthrough then falls into the next one's label,
// and any other jumps past the rest, to a label after the last, which is
// written only when some jump goes to it. Every goto jumps forward, to a
// label of the chain itself, never into a block, so it skips no
// declaration a body makes. In an expression switch, tagRef names the
// temporary holding the tag:
//
//	if (tack_s0 == 1) goto tack_case1;
//	goto tack_case2;
//	tack_case1: ;
//	{
//	    body
//	}
//	tack_case2: ;
//	{
//	    default body
//	}
func (g *generator) fallChain(
	s *ast.Switch,
	tagRef string,
	fn *ast.Func,
) ([]string, []diag.Diagnostic) {
	labels := make([]string, len(s.Cases))
	for i := range s.Cases {
		labels[i] = g.temp("case")
	}
	end := g.temp("end")
	endUsed := false
	var lines []string
	for i, cl := range s.Cases {
		if cl.Default {
			continue
		}
		test, diags := g.caseTest(s, tagRef, cl)
		if len(diags) > 0 {
			return nil, diags
		}
		lines = append(lines, fmt.Sprintf("if (%s) goto %s;", test, labels[i]))
	}
	if i := slices.IndexFunc(s.Cases, func(cl *ast.CaseClause) bool { return cl.Default }); i >= 0 {
		lines = append(lines, "goto "+labels[i]+";")
	} else {
		lines, endUsed = append(lines, "goto "+end+";"), true
	}
	for i, cl := range s.Cases {
		body, diags := g.caseBody(cl, tagRef, fn)
		if len(diags) > 0 {
			return nil, diags
		}
		lines = append(lines, labels[i]+": ;", "{")
		lines = append(append(lines, indent(body)...), "}")
		if i < len(s.Cases)-1 && ast.FallsInto(cl.Body) == nil {
			lines, endUsed = append(lines, "goto "+end+";"), true
		}
	}
	if endUsed {
		lines = append(lines, end+": ;")
	}
	return lines, nil
}

// caseBody lowers the body of cl, a case of a switch whose tag tagRef
// holds, in fn, after the declaration of the variable a type switch's case
// declares, when the body reads it (see generateSwitch); a case that does
// not read it declares none, which C would warn about.
func (g *generator) caseBody(
	cl *ast.CaseClause,
	tagRef string,
	fn *ast.Func,
) ([]string, []diag.Diagnostic) {
	body, diags := g.generateBlock(cl.Body, fn)
	if len(diags) > 0 || cl.Var == nil || !g.frame.read[cl.Var] {
		return body, diags
	}
	value := tagRef
	if typ := cl.Var.Type; typ != types.Any {
		value = fmt.Sprintf("*(const %s *)%s.ptr", g.cType(typ), tagRef)
	}
	decl := fmt.Sprintf("const %s %s = %s;", g.cType(cl.Var.Type), declName(cl.Var), value)
	return append([]string{decl}, body...), nil
}

// caseVarsRead returns the variables of the cases of the type switches in
// body, at any depth, that a name in body reads, found in one walk.
func caseVarsRead(body []ast.Stmt) map[*ast.Var]bool {
	vars := make(map[*ast.Var]bool)
	read := make(map[*ast.Var]bool)
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Switch:
				for _, cl := range x.Cases {
					if cl.Var != nil {
						vars[cl.Var] = true
					}
				}
			case *ast.Ident:
				if v, ok := x.Def.(*ast.Var); ok && vars[v] {
					read[v] = true
				}
			}
			return true
		})
	}
	return read
}

// caseTest renders the C test of a case: each value's test, joined with ||
// and each parenthesized when there are several. A type switch's value is
// a type, tested against the id the tag's tack_any holds. A single test is left
// bare, since the if adds the parentheses and a second pair around an
// equality makes clang warn.
func (g *generator) caseTest(
	s *ast.Switch,
	tagRef string,
	cl *ast.CaseClause,
) (string, []diag.Diagnostic) {
	tests := make([]string, len(cl.Values))
	for i, v := range cl.Values {
		if s.IsTypeSwitch() {
			tests[i] = fmt.Sprintf("%s.type == %s", tagRef, g.anyID(cl.Types[i]))
			continue
		}
		test, diags := g.valueTest(s, tagRef, v)
		if len(diags) > 0 {
			return "", diags
		}
		tests[i] = test
	}
	if len(tests) == 1 {
		return tests[0], nil
	}
	return "(" + strings.Join(tests, ") || (") + ")", nil
}

// valueTest renders, without parentheses around the whole, the C test of
// one case value v: the condition itself in a tagless switch, and whether
// the tag, held in tagRef, equals v in an expression switch, as == compares
// them: two integers by value (see intTest).
func (g *generator) valueTest(
	s *ast.Switch,
	tagRef string,
	v ast.Expr,
) (string, []diag.Diagnostic) {
	if s.Tag == nil {
		cond, diags := g.expr(v, types.Bool)
		if len(diags) > 0 {
			return "", diags
		}
		return stripParens(v, cond), nil
	}
	value, diags := g.expr(v, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	switch typ := ast.TypeOf(s.Tag); {
	case g.isComposite(typ):
		return fmt.Sprintf(
			"%s(&%s, %s)",
			g.equalityName(typ),
			tagRef,
			g.addressOf(v, typ, value),
		), nil
	case typ == types.String:
		return fmt.Sprintf("%s(%s, %s) == 0", stringCompareName, tagRef, value), nil
	case types.IsInteger(typ):
		return g.intTest(typ, tagRef, v, value), nil
	default:
		return fmt.Sprintf("%s == %s", tagRef, value), nil
	}
}

// intTest renders, without parentheses around the whole, whether the
// integer tag of type typ, held in tagRef, equals the case value v,
// rendered as value, by value, as intComparison compares them. When that
// reads v more than once, or may skip it, a v that is not pure is held in
// a temporary first; the tag's temporary is pure.
func (g *generator) intTest(typ types.Type, tagRef string, v ast.Expr, value string) string {
	vt := ast.TypeOf(v)
	test := func(ops []string) string {
		return intComparison(ast.Eq, typ, vt, tagRef, ops[0])
	}
	var rendered string
	if needsOnce(typ, vt) {
		operands := []ast.Expr{v}
		rendered = g.hoist(operands, []string{value}, g.operandTypes(operands), test)
	} else {
		rendered = test([]string{value})
	}
	return rendered[1 : len(rendered)-1]
}

// switchCompares returns the type of the tag when n is an expression
// switch that compares its tag with at least one value, with ==, so that
// the type's equality helper, or the string comparison helper, is
// emitted. A type switch compares type ids, and needs neither.
func switchCompares(n ast.Node) (types.Type, bool) {
	s, ok := n.(*ast.Switch)
	if !ok || s.Tag == nil || !hasValues(s) || s.IsTypeSwitch() {
		return types.Invalid, false
	}
	return ast.TypeOf(s.Tag), true
}

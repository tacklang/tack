package c

import (
	"fmt"
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
// case tests is still evaluated, cast to void, for any call in it to run.
func (g *generator) generateSwitch(s *ast.Switch, fn *ast.Func) ([]string, []diag.Diagnostic) {
	if s.Tag == nil {
		return g.caseChain(s, "", fn)
	}

	tag, diags := g.expr(s.Tag, types.Invalid)
	if len(diags) > 0 {
		return nil, diags
	}
	tagRef := ""
	eval := fmt.Sprintf("(void)(%s);", tag)
	if hasValues(s) {
		tagRef = g.temp("s")
		eval = fmt.Sprintf("const %s %s = %s;", g.cType(ast.TypeOf(s.Tag)), tagRef, tag)
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
	var lines []string
	for _, cl := range s.Cases {
		if cl.Default {
			continue
		}
		test, diags := g.caseTest(s, tagRef, cl)
		if len(diags) > 0 {
			return nil, diags
		}
		body, diags := g.generateBlock(cl.Body, fn)
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
	body, diags := g.generateBlock(def.Body, fn)
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

// caseTest renders the C test of a case: each value's test, joined with ||
// and each parenthesized when there are several. A single test is left
// bare, since the if adds the parentheses and a second pair around an
// equality makes clang warn.
func (g *generator) caseTest(
	s *ast.Switch,
	tagRef string,
	cl *ast.CaseClause,
) (string, []diag.Diagnostic) {
	tests := make([]string, len(cl.Values))
	for i, v := range cl.Values {
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
// the tag, held in tagRef, equals v in an expression switch, compared in
// the type one widens to, as == compares them.
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
		return fmt.Sprintf("%s(%s, %s)", equalityName(typ), tagRef, value), nil
	case typ == types.String:
		return fmt.Sprintf("%s(%s, %s) == 0", stringCompareName, tagRef, value), nil
	default:
		return fmt.Sprintf("%s == %s", tagRef, value), nil
	}
}

// switchCompares returns the type of the tag when n is an expression
// switch that compares its tag with at least one value, with ==, so that
// the type's equality helper, or the string comparison helper, is
// emitted.
func switchCompares(n ast.Node) (types.Type, bool) {
	s, ok := n.(*ast.Switch)
	if !ok || s.Tag == nil || !hasValues(s) {
		return types.Invalid, false
	}
	return ast.TypeOf(s.Tag), true
}

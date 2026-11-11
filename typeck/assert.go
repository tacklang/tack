package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Asking an any which type it holds. A type assertion, x.(T), asks
// whether the any x holds a T, exactly: an int8 is not an int64, and an
// untyped constant, passed as an int64, is one. It is a checked form, like
// q, ok := a / b, v, ok := a[i] and v, ok := T(x), and has no other: it is
// only ever bound with ok, v, ok := x.(T) or v, ok = x.(T), giving T's
// zero value and false on a mismatch, and anywhere else it is reported,
// since there is no value to give on a mismatch and Tack has no panics. A
// type switch, switch v := x.(type), tries its cases' types the same way,
// top to bottom, and binds v in each case: to the value, as a T, in a
// case naming one type T, and to the any itself in a case naming several
// and in the default. x.(type) is a type switch's tag and nothing else.
//
// T, and every type a case names, must be one an any can hold: one of the
// twelve types InAny accepts, or a view of one of them. Any other, an
// array, a struct, a view of another type, a list, error, Allocator or
// any itself, is reported, since asking would always give false. A view
// is named only by a type switch's case, whose v is then that view, under
// every rule a view parameter follows: it refers to the caller's
// elements, which nothing changes during the call, for no longer than
// the any does, and is immutable, so it is never assigned, stored or
// returned. A type assertion to a view is reported: it would declare
// a variable of a view type that a mismatch leaves empty, and that
// v, ok = a.([]T) would need a mutable one to assign. A type named twice in one type switch, in one case
// or two, is reported, since the second could never match. The operand
// must be an any. Nothing about what an any holds is ever known at compile
// time, so a type switch gives no facts, and an assertion proves nothing.
//
// The syntax is meant to serve tagged unions too, whose type switch will
// try the union's variants; the checks here are written for an any's
// operand, and a union's would sit beside them.

// assertedType returns the type expr names, written in a type assertion
// or as a type switch's case, reporting one an any can never hold. It is
// types.Invalid for one reported here or where the type itself was.
func (c *Checker) assertedType(expr ast.Expr) types.Type {
	var typ types.Type
	switch {
	case isAnyName(expr):
		typ = types.Any
	default:
		if vt, ok := expr.(*ast.ViewType); ok {
			typ = c.viewType(vt)
		} else {
			typ = c.declaredType(expr)
		}
	}
	if typ == types.Invalid || c.table.InAny(typ) {
		return typ
	}
	c.errorf(expr.Spanned(), "an any never holds %s: %s", c.typeName(typ), anyHolds)
	return types.Invalid
}

// assertOperand checks x, the operand of a type assertion or of a type
// switch's tag, which must be an any, and reports whether it is one. A
// value of any other type is reported, and one already reported is not.
func (c *Checker) assertOperand(x ast.Expr) bool {
	got := c.valueOf(x, c.expr(x))
	if got == types.Invalid {
		return false
	}
	if got != types.Any {
		c.errorf(
			x.Spanned(),
			"cannot assert on %s: it is %s, not any",
			c.text(x.Spanned()),
			c.typeName(got),
		)
		return false
	}
	return true
}

// typeAssert checks a, a type assertion, and returns the type of its
// value, which it records on a: x.(T) gives T when it is checkedAssert,
// being bound with ok, and is reported anywhere else, as is x.(type)
// anywhere but a type switch's tag, which checkTypeSwitch checks without
// coming here, and a T that is a view, which only a type switch's case
// may name.
func (c *Checker) typeAssert(a *ast.TypeAssert) types.Type {
	a.Type = types.Invalid
	if a.TypeExpr == nil {
		c.expr(a.X)
		c.errorf(a.Span, ".(type) can only be the tag of a switch")
		return types.Invalid
	}
	isAny := c.assertOperand(a.X)
	typ := c.assertedType(a.TypeExpr)
	if !isAny || typ == types.Invalid {
		return types.Invalid
	}
	if c.table.IsView(typ) {
		c.errorf(a.TypeExpr.Spanned(), "a type assertion cannot give %s, a view; "+
			"ask with a type switch, as in switch v := %s.(type)", c.typeName(typ), c.text(a.X.Spanned()))
		return types.Invalid
	}
	if a != c.checkedAssert {
		c.errorf(a.Span, "a type assertion needs two results; write v, ok := %s", c.text(a.Span))
		return types.Invalid
	}
	a.Type = typ
	return typ
}

// assertResults checks a type assertion bound to want names, which must be
// two: the value, or the asserted type's zero value on a mismatch, and
// whether it matched.
func (c *Checker) assertResults(a *ast.TypeAssert, want int, span ast.Span) ([]types.Type, bool) {
	c.checkedAssert = a
	typ := c.typeAssert(a)
	c.checkedAssert = nil
	if typ == types.Invalid {
		return nil, false
	}
	if want != 2 {
		c.errorf(
			span,
			"assignment mismatch: %d variables but a type assertion gives 2 values",
			want,
		)
		return nil, false
	}
	return []types.Type{typ, types.Bool}, true
}

// checkTypeSwitch checks a type switch, whose tag is x.(type), in fn: x,
// which must be an any, then each case in the order tried, the types it
// names and its body, with the case's variable, if the switch binds one,
// of the case's one type, or of x's type, any, in a case naming several
// and in the default. A type switch gives no facts: what an any holds is
// never known at compile time. A type named twice in the switch is
// reported at the second, with a note at the first.
func (c *Checker) checkTypeSwitch(s *ast.Switch, fn *ast.Func) {
	tag, _ := s.Tag.(*ast.TypeAssert)
	isAny := c.assertOperand(tag.X)
	seen := make(map[types.Type]ast.Expr)
	for _, cl := range tryOrder(s) {
		c.checkCaseTypes(cl, seen)
		if cl.Var != nil {
			cl.Var.Type = caseVarType(cl, isAny)
			c.bindings[cl.Var] = typeSwitchVar
		}
		c.inClause(cl, typeSwitch, func() {
			c.checkGuarded(cl.Body, &guard{span: caseSpan(cl), kind: guardCase}, nil, fn)
		})
	}
}

// checkCaseTypes checks and records the types a case of a type switch
// names, reporting one seen earlier in the switch, which seen records with
// where it was named.
func (c *Checker) checkCaseTypes(cl *ast.CaseClause, seen map[types.Type]ast.Expr) {
	cl.Types = make([]types.Type, len(cl.Values))
	for i, v := range cl.Values {
		typ := c.assertedType(v)
		cl.Types[i] = typ
		if typ == types.Invalid {
			continue
		}
		prev, dup := seen[typ]
		if !dup {
			seen[typ] = v
			continue
		}
		c.diags = append(c.diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s appears twice in this type switch", c.typeName(typ)),
			Span:     v.Spanned(),
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  "previous case",
				Span:     prev.Spanned(),
			}},
		})
	}
}

// caseVarType returns the type of the variable a case of a type switch
// declares: the case's type, when it names one, and otherwise any, the
// type of the tag's operand, or types.Invalid when that operand, isAny
// says, was reported, or the case is a bad clause, whose types are
// unknown.
func caseVarType(cl *ast.CaseClause, isAny bool) types.Type {
	switch {
	case !isAny || cl.Bad:
		return types.Invalid
	case !cl.Default && len(cl.Types) == 1:
		return cl.Types[0]
	default:
		return types.Any
	}
}

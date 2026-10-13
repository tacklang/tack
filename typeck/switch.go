package typeck

import (
	"fmt"
	"strconv"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A switch is checked as the chain of ifs it means. A tagless switch,
//
//	switch {
//	case c1: A
//	case c2, c3: B
//	default: D
//	}
//
// means if c1 { A } else if c2 || c3 { B } else { D }, wherever the default
// is written, and an expression switch on tag means the same with each
// value v standing for tag == v, except that tag is evaluated once. So a
// case's body gets the facts of its condition being true, every later case,
// its values and the default included, gets those of it being false, a
// value gets those of the values before it in its case being false, and the
// statements after the switch get what an if in that chain's place would
// give them. Facts about the tag come from the comparisons it stands in,
// so they exist only where the same comparison written in an if would
// give one, as when the tag is a variable.

// checkSwitch checks a switch in fn: its tag, then each case, its values and
// its body, in the order they are tried, the default last.
func (c *Checker) checkSwitch(s *ast.Switch, fn *ast.Func) {
	tagType := types.Invalid
	if s.Tag != nil {
		tagType = c.valueOf(s.Tag, c.expr(s.Tag))
	}

	order := tryOrder(s)
	// Every body after a case, in the order tried, is the region its
	// condition being false holds in; rest[i] is where that region starts
	// for the case at order[i].
	var later []ast.Stmt
	rest := make([]int, len(order))
	for i, cl := range order {
		later = append(later, cl.Body...)
		rest[i] = len(later)
	}

	seen := make(map[caseKey]ast.Expr)
	m := c.mark()
	for i, cl := range order {
		c.checkCaseValues(s, tagType, cl, seen)
		span := caseSpan(cl)
		c.checkGuarded(cl.Body, &guard{span: span, kind: guardCase}, c.caseFacts(s, cl, true), fn)
		c.assume(
			&guard{span: span, kind: guardLaterCase},
			c.caseFacts(s, cl, false),
			later[rest[i]:],
		)
	}
	c.release(m)
}

// tryOrder returns a switch's cases in the order they are tried: every case
// with values in source order, then the default, if there is one.
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

// checkCaseValues checks each value of a case, left to right, each with the
// facts of those before it being false in force, as on the right of ||. In
// a tagless switch each value must be a bool; in an expression switch each
// must compare with the tag, and must not repeat a constant value seen
// earlier in the switch, which seen records.
func (c *Checker) checkCaseValues(
	s *ast.Switch,
	tagType types.Type,
	cl *ast.CaseClause,
	seen map[caseKey]ast.Expr,
) {
	m := c.mark()
	for _, v := range cl.Values {
		switch {
		case s.Tag == nil:
			c.checkCond(v)
		case c.checkCaseValue(tagType, v):
			c.checkDuplicate(v, seen)
		}
		cond := caseCond(s, v)
		c.assume(
			&guard{cond: cond, span: v.Spanned(), kind: guardCase},
			c.factsFrom(cond, false),
			nil,
		)
	}
	c.release(m)
}

// checkCaseValue checks a value of an expression switch whose tag has type
// tagType, under the rules of ==: an untyped constant takes the tag's type
// if it is an integer type, and the two types must then have a common
// type, one widening to the other, that == is defined on. It reports
// whether the value can be compared with the tag.
func (c *Checker) checkCaseValue(tagType types.Type, v ast.Expr) bool {
	got := c.expr(v)
	if tagType == types.Invalid || got == types.Invalid {
		return false
	}
	got = c.untypedAs(v, got, tagType)
	if got == types.Invalid {
		return false
	}
	common, ok := commonType(tagType, got)
	if !ok {
		c.errorf(
			v.Spanned(),
			"mismatched types %s and %s in switch case",
			c.typeName(tagType),
			c.typeName(got),
		)
		return false
	}
	// Every type is comparable today; this keeps a type that is not from
	// reaching the backend should one be added.
	if !c.supports(common, ast.Eq) {
		c.errorf(v.Spanned(), "operator == is not defined on %s", c.typeName(common))
		return false
	}
	return true
}

// caseKey identifies a constant case value for finding duplicates: an
// integer's known value, a bool constant's, true or false, or a string
// literal's bytes, which are told apart by str. An integer's text is
// digits, so it is never a bool's.
type caseKey struct {
	text string
	str  bool
}

// checkDuplicate reports v, a case value of an expression switch, when it
// is a constant equal to one seen earlier in the switch, pointing at that
// one, and otherwise records it in seen. An integer is a constant when its
// value is known, a bool when it is a constant expression, as fold computes
// it, and a string when it is a literal; any other value is never a
// duplicate, since its value is known only at run time.
func (c *Checker) checkDuplicate(v ast.Expr, seen map[caseKey]ast.Expr) {
	key, ok := c.caseKeyOf(v)
	if !ok {
		return
	}

	prev, dup := seen[key]
	if !dup {
		seen[key] = v
		return
	}
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("duplicate case %s in switch", key.text),
		Span:     v.Spanned(),
		Related: []diag.Diagnostic{{
			Severity: diag.Note,
			Message:  "previous case",
			Span:     prev.Spanned(),
		}},
	})
}

// caseKeyOf returns the key of the case value v, if it is a constant.
func (c *Checker) caseKeyOf(v ast.Expr) (caseKey, bool) {
	if lit, ok := v.(*ast.StrLit); ok {
		return caseKey{text: strconv.Quote(lit.Value), str: true}, true
	}
	if ast.TypeOf(v) == types.Bool {
		b, ok := c.fold(v)
		if known, isBool := b.(bool); ok && isBool {
			return caseKey{text: strconv.FormatBool(known)}, true
		}
		return caseKey{}, false
	}
	if k, ok := c.knownValue(v); ok {
		return caseKey{text: k.String()}, true
	}
	return caseKey{}, false
}

// caseCond returns the condition a case value v of switch s stands for: v
// itself in a tagless switch, and tag == v in an expression switch. The
// comparison is built only to derive facts from; it is never checked or
// lowered.
func caseCond(s *ast.Switch, v ast.Expr) ast.Expr {
	if s.Tag == nil {
		return v
	}
	return &ast.Binary{Op: ast.Eq, X: s.Tag, Y: v, Span: v.Spanned()}
}

// caseFacts returns the facts a case of switch s gives where its condition,
// the || of its values' conditions, evaluated to holds: as for ||, what
// every value's facts of being true agree on when true (see union), and
// every value's facts of being false when false. The default has no
// condition, and gives nothing.
func (c *Checker) caseFacts(s *ast.Switch, cl *ast.CaseClause, holds bool) []fact {
	if cl.Default || len(cl.Values) == 0 {
		return nil
	}
	if holds {
		facts := c.factsFrom(caseCond(s, cl.Values[0]), true)
		for _, v := range cl.Values[1:] {
			facts = union(facts, c.factsFrom(caseCond(s, v), true))
		}
		return facts
	}
	var facts []fact
	for _, v := range cl.Values {
		facts = append(facts, c.factsFrom(caseCond(s, v), false)...)
	}
	return facts
}

// switchAfterFactsBy returns the facts a switch gives the statements after
// it in its block, as the chain of ifs it means does: its first case tried
// is the if, and the rest of the chain its else. The facts come from that
// first case, as an early exit, the guard returned, when one side of the
// chain leaves and the other does not; a call to exit counts as leaving
// when exits is set (see leavesBy).
func (c *Checker) switchAfterFactsBy(s *ast.Switch, exits bool) (*guard, []fact) {
	order := tryOrder(s)
	if len(order) == 0 || order[0].Default {
		return nil, nil
	}
	firstLeaves, restLeaves := leavesBy(order[0].Body, exits), chainLeavesBy(order[1:], exits)
	if firstLeaves == restLeaves {
		return nil, nil
	}
	facts := exitFacts(firstLeaves, restLeaves, func(holds bool) []fact {
		return c.caseFacts(s, order[0], holds)
	})
	return &guard{span: caseSpan(order[0]), kind: guardExit}, facts
}

// caseSpan returns what a note about a case underlines: its keyword
// through its last value, or the whole clause for a default.
func caseSpan(cl *ast.CaseClause) ast.Span {
	span := cl.Span
	if len(cl.Values) > 0 {
		span.End = cl.Values[len(cl.Values)-1].Spanned().End
	}
	return span
}

// chainLeavesBy reports whether a chain of cases, in the order tried,
// always leaves the code around it: it ends in a default, and every case's
// body leaves, a call to exit counting as leaving when exits is set (see
// leavesBy).
func chainLeavesBy(order []*ast.CaseClause, exits bool) bool {
	if len(order) == 0 || !order[len(order)-1].Default {
		return false
	}
	for _, cl := range order {
		if !leavesBy(cl.Body, exits) {
			return false
		}
	}
	return true
}

// switchTerminates reports whether a switch is a terminating statement: it
// has a default, and every case's body ends in a terminating statement, as
// an if must have an else and both branches terminate.
func switchTerminates(s *ast.Switch) bool {
	if s.Default() == nil {
		return false
	}
	for _, cl := range s.Cases {
		if !blockTerminates(cl.Body) {
			return false
		}
	}
	return true
}

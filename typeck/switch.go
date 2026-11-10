package typeck

import (
	"fmt"
	"slices"
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
//
// A case whose body ends in a fallthrough goes on into the body of the
// case written after it, which is then reached two ways: by matching, with
// the facts above, or by the fallthrough, with the facts in force where it
// is, after the falling case's defers have run, and none from the entered
// case's values, which are not tested on that path. Its body gets what
// both ways agree on, as || joins its sides (see joinFacts). The bodies
// entered by a fallthrough are checked once every case's values are, in
// source order, each after the case that falls into it, since a default
// written before other cases, tried last, may fall into one of them.

// checkSwitch checks a switch in fn: its tag, then each case, its values and
// its body, in the order they are tried, the default last. A type switch
// is checked by checkTypeSwitch, and a switch on a union, which matches
// its cases, by checkUnionSwitch. A tag of any other type
// with no ==, a view, an any, a list, an Allocator, or a struct or array
// holding one, is reported, since a switch compares its tag with ==.
func (c *Checker) checkSwitch(s *ast.Switch, fn *ast.Func) {
	if s.IsTypeSwitch() {
		c.checkTypeSwitch(s, fn)
		return
	}
	tagType := types.Invalid
	if s.Tag != nil {
		tagType = c.valueOf(s.Tag, c.expr(s.Tag))
	}
	if c.table.IsUnion(tagType) {
		c.checkUnionSwitch(s, tagType, fn)
		return
	}
	if tagType != types.Invalid {
		tagType = c.switchTagType(s.Tag, tagType)
	}

	order := tryOrder(s)
	// Every case after a case, in the order tried, its values and its
	// body, is the region its condition being false holds in; rest[i] is
	// where that region starts for the case at order[i].
	var later []ast.Node
	rest := make([]int, len(order))
	for i, cl := range order {
		for _, v := range cl.Values {
			later = append(later, v)
		}
		later = append(later, stmtNodes(cl.Body)...)
		rest[i] = len(later)
	}

	seen := make(map[caseKey]ast.Expr)
	entered := fallenInto(s)
	late := lateBodies(s)
	// earlier holds the facts of every case tried so far not matching,
	// kept only when some body is entered by a fallthrough, for the facts
	// matching gives that body (see checkFallenInto).
	var earlier, atSwitch []fact
	if len(entered) > 0 {
		atSwitch = c.factsAtSwitch(s)
	}
	matched := make(map[*ast.CaseClause][]fact)
	m := c.mark()
	for i, cl := range order {
		c.checkCaseValues(s, tagType, cl, seen)
		span := caseSpan(cl)
		if entered[cl] {
			matched[cl] = slices.Concat(atSwitch, c.caseFacts(s, cl, true), earlier)
		} else {
			g := &guard{span: span, kind: guardCase}
			c.checkCaseBody(s, cl, late[cl], g, c.caseFacts(s, cl, true), fn)
		}
		notMatched := c.caseFacts(s, cl, false)
		c.assume(&guard{span: span, kind: guardLaterCase}, notMatched, later[rest[i]:])
		if len(entered) > 0 {
			earlier = append(earlier, notMatched...)
		}
	}
	c.release(m)
	for i, cl := range s.Cases {
		if entered[cl] {
			from, _ := lastFallthrough(s.Cases[i-1].Body)
			c.checkFallenInto(s, cl, from, late[cl], matched[cl], fn)
		}
	}
}

// lateBodies returns the cases of s whose body may run after every case's
// values are tried: a default written before another case, which runs
// only once every value has failed to match, and each case it falls into,
// directly or down a chain of fallthroughs, which then runs later still.
// It is nil when the default is written last, or there is none.
func lateBodies(s *ast.Switch) map[*ast.CaseClause]bool {
	i := slices.IndexFunc(s.Cases, func(cl *ast.CaseClause) bool { return cl.Default })
	if i < 0 || i == len(s.Cases)-1 {
		return nil
	}
	late := map[*ast.CaseClause]bool{s.Cases[i]: true}
	for next := ast.FallsInto(s.Cases[i].Body); next != nil; next = ast.FallsInto(next.Body) {
		late[next] = true
	}
	return late
}

// fallenInto returns the cases of s whose body a fallthrough enters: each
// written after a case whose body ends in one.
func fallenInto(s *ast.Switch) map[*ast.CaseClause]bool {
	var entered map[*ast.CaseClause]bool
	for _, cl := range s.Cases {
		if next := ast.FallsInto(cl.Body); next != nil {
			if entered == nil {
				entered = make(map[*ast.CaseClause]bool)
			}
			entered[next] = true
		}
	}
	return entered
}

// checkFallenInto checks the body of cl, a case of the expression switch
// s that the fallthrough from enters, in fn, with the facts both ways into
// it agree on in force: matched, which matching it gives, the facts in
// force where s starts, its values' and those of the cases tried before
// it not matching, and the facts in force at from (see checkFallthrough).
// The facts in force where s starts still hold in the body on their own,
// up to the first write that can reach it, as anywhere in s, which, for a
// body late says runs after every case's values, a case a default
// written before others falls into, is any write in a value of s too (see
// lateBodies). Every value
// of s is tried before cl's body runs, on the path that matches, so a
// fact about a variable one of them assigns is left out of matched; most
// switches have no such value (see tagWriteOf). The facts are those of a
// join, which no note describes.
func (c *Checker) checkFallenInto(
	s *ast.Switch,
	cl *ast.CaseClause,
	from *ast.Fallthrough,
	late bool,
	matched []fact,
	fn *ast.Func,
) {
	if w := c.tagWriteOf(s); w.first < len(w.pos) {
		var values []ast.Node
		for _, other := range s.Cases {
			values = append(values, valueNodes(other.Values)...)
		}
		matched = c.dropAssigned(matched, values)
	}
	fell := c.fellFacts[from]
	delete(c.fellFacts, from)
	g := &guard{span: caseSpan(cl), kind: guardFallthrough}
	c.checkCaseBody(s, cl, late, g, joinFacts(fell, matched), fn)
}

// lastFallthrough returns the fallthrough body ends in, if it ends in one.
func lastFallthrough(body []ast.Stmt) (*ast.Fallthrough, bool) {
	if len(body) == 0 {
		return nil, false
	}
	f, ok := body[len(body)-1].(*ast.Fallthrough)
	return f, ok
}

// checkCaseBody checks the body of cl, a case of the expression switch s,
// in fn, with facts, which g gives, in force: those of its condition
// being true, or, for a body a fallthrough enters, what that and the
// fallthrough agree on. A body late says may run after every case's
// values are tried, a default written before another case or a case it
// falls into (see lateBodies), is checked with c.lateDefaults recording it,
// so a write in any of those values reaches it (see reaches).
func (c *Checker) checkCaseBody(
	s *ast.Switch,
	cl *ast.CaseClause,
	late bool,
	g *guard,
	facts []fact,
	fn *ast.Func,
) {
	if late {
		c.lateDefaults = append(c.lateDefaults, lateDefault{whole: s.Span, body: clauseSpan(cl)})
	}
	c.inClause(cl, plainSwitch, func() { c.checkGuarded(cl.Body, g, facts, fn) })
	if late {
		c.lateDefaults = c.lateDefaults[:len(c.lateDefaults)-1]
	}
}

// switchKind is what kind of switch a case belongs to, which decides
// whether a fallthrough may end it.
type switchKind int

const (
	// plainSwitch is an expression switch, tagged or with no tag, whose
	// cases a fallthrough may end.
	plainSwitch switchKind = iota
	// typeSwitch is a type switch, switch x.(type).
	typeSwitch
	// unionSwitch is a switch on a union, which matches its cases.
	unionSwitch
)

// clauseAt is a switch case whose body is being checked: cl, of a switch
// of kind kind, with deferred defers around it (see Checker.deferred).
type clauseAt struct {
	cl       *ast.CaseClause
	kind     switchKind
	deferred int
}

// inClause runs check, which checks the body of cl, a case of a switch of
// kind kind, with cl recorded as the innermost case being checked.
func (c *Checker) inClause(cl *ast.CaseClause, kind switchKind, check func()) {
	c.clauses = append(c.clauses, clauseAt{cl: cl, kind: kind, deferred: c.deferred})
	check()
	c.clauses = c.clauses[:len(c.clauses)-1]
}

// checkFallthrough checks f, which must be the last statement of the
// body of a case of an expression switch that is not the switch's last
// case, outside any defer inside that case, and records the facts in
// force there for the next case's body (see checkFallenInto). Those
// facts hold once the case's body is left, but for any its defers, which
// run then, assign the variable of.
func (c *Checker) checkFallthrough(f *ast.Fallthrough) {
	if len(c.clauses) == 0 {
		c.errorf(f.Span, "fallthrough is not in a switch")
		return
	}
	in := c.clauses[len(c.clauses)-1]
	last, isLast := lastFallthrough(in.cl.Body)
	switch {
	case c.deferred > in.deferred:
		c.errorf(f.Span, "cannot fall through out of a defer")
	case in.kind == typeSwitch:
		c.errorf(f.Span, "cannot fall through in a type switch")
	case in.kind == unionSwitch:
		c.errorf(f.Span, "cannot fall through in a switch on a union")
	case !isLast || last != f:
		c.errorf(f.Span, "fallthrough must be the last statement of a case")
	case f.Next == nil:
		c.errorf(f.Span, "cannot fall through from the last case of a switch")
	default:
		if c.fellFacts == nil {
			c.fellFacts = make(map[*ast.Fallthrough][]fact)
		}
		c.fellFacts[f] = c.factsAtFallthrough(in.cl.Body)
	}
}

// factsAtFallthrough returns the facts in force where checking has
// reached, a fallthrough ending body (see factsHere), with each whose
// variable a defer of body assigns marked dropped: the defers run as body
// is left, before the next case's body.
func (c *Checker) factsAtFallthrough(body []ast.Stmt) []fact {
	facts := c.factsHere()
	var defers []ast.Node
	for _, stmt := range body {
		if d, ok := stmt.(*ast.Defer); ok {
			defers = append(defers, d)
		}
	}
	if len(defers) > 0 {
		facts = c.dropAssigned(facts, defers)
	}
	return facts
}

// factsAtSwitch returns the facts in force where s starts, once its tag
// is evaluated and before any value is tried (see factsHere): just past
// the tag, so a write the tag makes, which takes effect at its end, ends
// what it writes.
func (c *Checker) factsAtSwitch(s *ast.Switch) []fact {
	outer := c.at
	c.at = s.Span.Start + len("switch")
	if s.Tag != nil {
		c.at = s.Tag.Spanned().End + 1
	}
	facts := c.factsHere()
	c.at = outer
	return facts
}

// factsHere returns a copy of every fact in force that holds where
// checking has reached, c.at, but those that hold nothing a join can
// carry: a relation, and the sides of a condition. Each copy holds
// without a write to end it, for the code that joins it.
func (c *Checker) factsHere() []fact {
	var facts []fact
	for _, f := range c.active {
		if f.empty() || f.relational() || f.either != nil || !c.holds(f) {
			continue
		}
		f.ends = nil
		facts = append(facts, f)
	}
	return facts
}

// lateDefault is a body of a switch that runs only after every case's
// values have been tried: a default written before some of its cases, or
// a case it falls into (see lateBodies). whole is the switch, and body the
// case, its keyword through its last statement. A write in the switch
// outside the case may run before its body although it is written after
// it (see reaches).
type lateDefault struct {
	whole, body ast.Span
}

// clauseSpan returns the span of a case clause, its keyword through the
// end of its last statement.
func clauseSpan(cl *ast.CaseClause) ast.Span {
	span := cl.Span
	if len(cl.Body) > 0 {
		span.End = cl.Body[len(cl.Body)-1].Spanned().End
	}
	return span
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
// facts of those before it being false in force, as on the right of ||,
// each dropped when a value after it assigns its variable. In
// a tagless switch each value must be a bool; in an expression switch each
// must compare with the tag, and must not repeat a constant value seen
// earlier in the switch, which seen records. A value the resolver bound
// as a union's pattern, which the tag, no union, makes none, is reported
// as resolving it as a value would have (see unresolvedCase).
func (c *Checker) checkCaseValues(
	s *ast.Switch,
	tagType types.Type,
	cl *ast.CaseClause,
	seen map[caseKey]ast.Expr,
) {
	m := c.mark()
	for i, v := range cl.Values {
		switch {
		case s.Tag == nil:
			c.checkCond(v)
		case c.unresolvedCase(v, tagType):
		case c.checkCaseValue(tagType, v):
			c.checkDuplicate(v, seen)
		}
		cond := caseCond(s, v)
		c.assume(
			&guard{cond: cond, span: v.Spanned(), kind: guardCase},
			c.tagFacts(s, cl, c.factsFrom(cond, false)),
			valueNodes(cl.Values[i+1:]),
		)
	}
	c.release(m)
}

// checkCaseValue checks a value of an expression switch whose tag has type
// tagType, under the rules of ==: an untyped constant takes the tag's type
// if it is an integer type, which it must fit; an integer value of any
// type then compares with an integer tag by value, and any other value
// must have a common type with the tag, one widening to the other, that ==
// is defined on. It reports whether the value can be compared with the
// tag.
func (c *Checker) checkCaseValue(tagType types.Type, v ast.Expr) bool {
	got := c.expr(v)
	if tagType == types.Invalid || got == types.Invalid {
		return false
	}
	got = c.untypedAs(v, got, tagType)
	if got == types.Invalid {
		return false
	}
	if types.IsInteger(tagType) && types.IsInteger(got) {
		return true
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
// every value's facts of being false when false, each dropped when a value
// tried after it assigns its variable. The default has no
// condition, and gives nothing, and neither does a type switch's case,
// whose values are types, nor a union switch's, whose values are
// patterns.
func (c *Checker) caseFacts(s *ast.Switch, cl *ast.CaseClause, holds bool) []fact {
	if cl.Default || len(cl.Values) == 0 || s.IsTypeSwitch() ||
		c.table.IsUnion(ast.TypeOf(s.Tag)) {
		return nil
	}
	if holds {
		facts := c.factsFrom(caseCond(s, cl.Values[0]), true)
		for _, v := range cl.Values[1:] {
			facts = union(facts, c.factsFrom(caseCond(s, v), true))
		}
		return c.tagFacts(s, cl, facts)
	}
	var facts []fact
	for i, v := range cl.Values {
		own := c.factsFrom(caseCond(s, v), false)
		facts = append(facts, c.dropAssigned(own, valueNodes(cl.Values[i+1:]))...)
	}
	return c.tagFacts(s, cl, facts)
}

// tagFacts returns facts, which comparing the tag of switch s with values
// of the case cl gives, with every one that a value tried up to and
// including cl's assigns the variable of marked dropped. The tag is
// evaluated once, before any value, and every value is compared with
// what it was then, so a fact about the tag's variable holds for the
// variable only if no value tried since has assigned it. A tagless
// switch has no tag, and its facts are returned as they are.
//
// Most switches pass nothing as mut in their values, and a switch with
// thousands of cases asks about every one, so where a value first may
// assign anything is found once per switch (see tagWriteOf), and a case
// tried before that point keeps its facts without a search.
func (c *Checker) tagFacts(s *ast.Switch, cl *ast.CaseClause, facts []fact) []fact {
	if s.Tag == nil {
		return facts
	}
	if w := c.tagWriteOf(s); w.pos[cl] < w.first {
		return facts
	}
	var tried []ast.Node
	for _, other := range tryOrder(s) {
		tried = append(tried, valueNodes(other.Values)...)
		if other == cl {
			break
		}
	}
	return c.dropAssigned(facts, tried)
}

// tagWrite is where, in the order a switch's cases are tried, its values
// first may assign a variable: first is the position of the first case
// with a value that passes anything as mut, the only way a value can
// assign, or the number of cases when none does, and pos gives each
// case's position.
type tagWrite struct {
	pos   map[*ast.CaseClause]int
	first int
}

// tagWriteOf returns where the values of switch s first may assign a
// variable, working it out the first time it is asked.
func (c *Checker) tagWriteOf(s *ast.Switch) tagWrite {
	if w, ok := c.tagWrites[s]; ok {
		return w
	}
	order := tryOrder(s)
	w := tagWrite{first: len(order), pos: make(map[*ast.CaseClause]int, len(order))}
	for i, cl := range order {
		w.pos[cl] = i
		if w.first == len(order) && passesMut(cl.Values) {
			w.first = i
		}
	}
	c.tagWrites[s] = w
	return w
}

// passesMut reports whether any of values, at any depth, passes something
// as mut, or holds a closure that may write a capture, which the call it
// is passed to may run.
func passesMut(values []ast.Expr) bool {
	found := false
	for _, v := range values {
		ast.Walk(v, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.MutArg:
				found = true
			case *ast.FuncLit:
				for _, cp := range x.Captures {
					found = found || cp.Write
				}
				return false
			}
			return !found
		})
	}
	return found
}

// valueNodes returns a case's values as a region assume and dropAssigned
// search: the values tried after one that did not match.
func valueNodes(values []ast.Expr) []ast.Node {
	nodes := make([]ast.Node, len(values))
	for i, v := range values {
		nodes[i] = v
	}
	return nodes
}

// switchAfterFactsBy returns the facts a switch gives the statements after
// it in its block, as the chain of ifs it means does: its first case tried
// is the if, and the rest of the chain its else. The facts come from that
// first case, as an early exit, the guard returned, when one side of the
// chain leaves and the other does not.
func (c *Checker) switchAfterFacts(s *ast.Switch) (*guard, []fact) {
	order := tryOrder(s)
	if len(order) == 0 || order[0].Default {
		return nil, nil
	}
	firstLeaves, restLeaves := leaves(order[0].Body), chainLeaves(order[1:])
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

// switchLeaves reports whether s always leaves the code around it: its
// chain of cases does (see chainLeaves), or s is a switch on a union
// whose clauses name every case and whose every clause's body leaves.
func switchLeaves(s *ast.Switch) bool {
	if !s.Exhaustive {
		return chainLeaves(tryOrder(s))
	}
	for _, cl := range s.Cases {
		if !leaves(cl.Body) {
			return false
		}
	}
	return true
}

// chainLeaves reports whether a chain of cases, in the order tried,
// always leaves the code around it: it ends in a default, and every case's
// body leaves (see leaves).
func chainLeaves(order []*ast.CaseClause) bool {
	return chainLeavesBy(order, leaves)
}

// chainLeavesBy is chainLeaves with bodyLeaves deciding whether each
// case's body leaves, as leaves does, which a caller remembering what it
// found for a long chain passes (see keepWalk).
func chainLeavesBy(order []*ast.CaseClause, bodyLeaves func([]ast.Stmt) bool) bool {
	if len(order) == 0 || !order[len(order)-1].Default {
		return false
	}
	for _, cl := range order {
		if !bodyLeaves(cl.Body) {
			return false
		}
	}
	return true
}

// switchTerminates reports whether a switch is a terminating statement: it
// has a default, or is a switch on a union whose clauses name every case,
// and every case's body ends in a terminating statement, as an if must
// have an else and both branches terminate. A fallthrough into the next
// case counts as one, as in Go: the last case, which none may end, must
// terminate itself.
func switchTerminates(s *ast.Switch) bool {
	if s.Default() == nil && !s.Exhaustive {
		return false
	}
	for _, cl := range s.Cases {
		if !blockTerminates(cl.Body) {
			return false
		}
	}
	return true
}

// switchTagType returns tagType, the type of tag, a switch's tag other
// than a union, when a switch can compare it with its cases, and Invalid
// otherwise, having reported why: a type without ==, or one holding a
// list. == reads a list in place, which a switch, holding its tag while
// it evaluates each case, does not promise, so a value holding a list is
// compared in an if instead.
func (c *Checker) switchTagType(tag ast.Expr, tagType types.Type) types.Type {
	if !c.comparable(tagType) {
		c.errorf(tag.Spanned(), "operator == is not defined on %s", c.typeName(tagType))
		return types.Invalid
	}
	if c.table.Owned(tagType) {
		c.errorf(
			tag.Spanned(),
			"cannot switch on %s, which holds a list; compare it with == in an if instead",
			c.typeName(tagType),
		)
		return types.Invalid
	}
	return tagType
}

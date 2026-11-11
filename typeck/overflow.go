package typeck

import (
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// Integer arithmetic never wraps unless it is written to. Plain + - and *
// on integers, unary -, ++ and --, and += -= and *= on an integer target
// compile only where the checker proves the exact result is a value of
// the operation's type, below 0 included for an unsigned one; anything
// else is an error that suggests the checked form, v, ok := a + b, or
// the wrapping operator, a +% b. Arithmetic on known values that does
// not fit is the error knownOverflow reports instead, and a target-width
// type's result must fit on a 32-bit target, the narrowest, so that it
// fits on every target (see portableBounds).
//
// A proof (see overflowProven) takes the bounds of each operand and works
// out the bounds of the exact result from them (see exactRange); an
// operand's bounds are its known value, a subject's interval from its
// type and the facts in force, a length's range, the exact bounds of
// arithmetic inside it, or, for anything else, its type's. That last is
// sound for arithmetic inside an operand too: a plain operation is
// itself proven, or the program is rejected, so in a program that
// compiles every value an operand has is a value of its type, a wrapping
// operator's and a call's alike. A conversion keeps its operand's value,
// so its operand's bounds are its own. Where those bounds fall short, the
// relations in force may still prove it: a known amount added to a node,
// i + 1 under i < n, through its steps' conditions (see linear), and a
// difference that cannot go below 0, a - b under b <= a. Those queries
// count each node's type's bounds too (see Checker.typedRelations), so
// that n's type bounds i + 1 under i < n.
//
// A loop's post statement runs after its body, where the condition held
// when the body began. It is checked before the body, as written, but an
// operation in it that the facts there do not prove is tried again after
// the body, with the condition's facts in force, each dropped when the
// body writes anything it is about (see recheckPost).

// overflowCheck is an operation whose result must be proven to fit its
// type: e, plain + - or * on two integers or unary - on one, of type typ,
// checked with the checker at at. stmt is the increment, decrement or
// compound assignment e stands for, when there is one, which the error
// names instead of e.
type overflowCheck struct {
	e    ast.Expr
	stmt ast.Stmt
	typ  types.Type
	at   int
}

// arithMemo is what checking an operation found: the bounds operandBounds
// gives it and whether it was proven, under the facts of generation gen
// in the statement stmt counted (see Checker.arithMemo).
type arithMemo struct {
	bounds    interval
	proven    bool
	gen, stmt int
}

// memoFor returns what checking e found, when it was found in the
// statement being checked under the facts in force now.
func (c *Checker) memoFor(e ast.Expr) (arithMemo, bool) {
	m, ok := c.arithMemo[e]
	return m, ok && m.gen == c.factGen && m.stmt == c.stmtGen
}

// isPlainArith reports whether e is an operation that must be proven not
// to overflow when its type is an integer one: plain + - or * on two
// operands, or unary - on one.
func isPlainArith(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Binary:
		return x.Op == ast.Add || x.Op == ast.Sub || x.Op == ast.Mul
	case *ast.Unary:
		return x.Op == ast.Neg
	default:
		return false
	}
}

// requireNoOverflow reports e, plain + - * or unary - of the typed
// integer type typ, which it records as e's, unless it is the operation a checked form binds,
// whose ok reports an overflow, or it is proven not to overflow (see
// checkOverflow).
func (c *Checker) requireNoOverflow(e ast.Expr, typ types.Type) {
	if c.checkedArith == e || !types.IsInteger(typ) || typ == types.UntypedInt {
		return
	}
	// The proof reads e's type, which its check records only once it
	// returns it.
	switch x := e.(type) {
	case *ast.Binary:
		x.Type = typ
	case *ast.Unary:
		x.Type = typ
	}
	c.checkOverflow(overflowCheck{e: e, typ: typ, at: c.at})
}

// requireStepNoOverflow reports stmt, an increment, a decrement, or a
// compound assignment by + - or *, of a target of the integer type typ,
// unless the operation it applies is proven not to overflow.
func (c *Checker) requireStepNoOverflow(stmt ast.Stmt, typ types.Type) {
	if !types.IsInteger(typ) {
		return
	}
	bin := &ast.Binary{Type: typ}
	switch s := stmt.(type) {
	case *ast.IncDec:
		bin.X, bin.Op, bin.Span = s.Target, ast.Add, s.Span
		bin.Y = &ast.IntLit{Value: constant.FromInt64(1), Span: s.Span}
		if s.Dec {
			bin.Op = ast.Sub
		}
	case *ast.Assign:
		bin.X, bin.Y, bin.Op, bin.Span = s.Target, s.Value, s.Op, s.Span
	default:
		return
	}
	c.checkOverflow(overflowCheck{e: bin, stmt: stmt, typ: typ, at: c.at})
}

// checkOverflow reports chk unless its operation is proven not to
// overflow, or an operand of it is not proven either, which is reported on
// its own. While a loop's post statement is checked, every one not proven
// is kept for recheckPost instead, an operation over an unproven operand
// included.
func (c *Checker) checkOverflow(chk overflowCheck) {
	proven := c.overflowProven(chk.e, chk.typ)
	if chk.stmt == nil {
		c.remember(chk.e, chk.typ, proven)
	}
	if proven {
		return
	}
	// In a loop's post, an operation is kept for recheckPost even when an
	// operand of its own is not proven: that operand may be proven there,
	// and the operation must then be proven too.
	if c.deferredOverflow != nil {
		*c.deferredOverflow = append(*c.deferredOverflow, chk)
		return
	}
	if !c.operandUnproven(chk.e) {
		c.reportOverflow(chk)
	}
}

// remember records what checking e, an operation of type typ, found (see
// memoFor): the bounds of its exact result when they fit typ, and its
// type's otherwise.
func (c *Checker) remember(e ast.Expr, typ types.Type, proven bool) {
	bounds := typeInterval(e)
	if known := c.exactRange(e); known.hasLo && known.hasHi {
		if fits, ok := fitted(known, typ); ok {
			bounds = fits
		}
	}
	c.arithMemo[e] = arithMemo{bounds: bounds, proven: proven, gen: c.factGen, stmt: c.stmtGen}
}

// operandUnproven reports whether an operand of e, an operation, is
// itself plain + - * or unary - on integers not proven not to overflow,
// which is reported on its own: e is not reported as well, since its
// bounds rest on that operand's, and whether e can overflow is asked
// again once the operand is fixed.
func (c *Checker) operandUnproven(e ast.Expr) bool {
	var operands []ast.Expr
	switch x := e.(type) {
	case *ast.Binary:
		operands = []ast.Expr{x.X, x.Y}
	case *ast.Unary:
		operands = []ast.Expr{x.X}
	}
	for _, x := range operands {
		typ := ast.TypeOf(x)
		if !isPlainArith(x) || !types.IsInteger(typ) || typ == types.UntypedInt {
			continue
		}
		if m, ok := c.memoFor(x); ok {
			if !m.proven {
				return true
			}
		} else if !c.overflowProven(x, typ) {
			return true
		}
	}
	return false
}

// recheckPost tries again each operation of loop's post statement that
// the facts where it is written did not prove, pending, with the facts
// the loop's condition gives when it holds also in force: the post runs
// only after a pass of the body, which the condition let in. A fact about
// anything the body writes, or passes as mut, may not hold by the time
// the post runs, and is dropped. One still not proven is reported.
func (c *Checker) recheckPost(loop *ast.For, pending []overflowCheck) {
	if len(pending) == 0 {
		return
	}
	m := c.mark()
	if loop.Cond != nil {
		facts := c.dropAssigned(c.factsFrom(loop.Cond, true), stmtNodes(loop.Body))
		c.assume(&guard{cond: loop.Cond, span: loop.Cond.Spanned(), kind: guardCond}, facts, nil)
	}
	outer := c.at
	for _, chk := range pending {
		c.at = chk.at
		if !c.overflowProven(chk.e, chk.typ) && !c.operandUnproven(chk.e) {
			c.reportOverflow(chk)
		}
	}
	c.at = outer
	c.release(m)
}

// overflowProven reports whether e, plain + - or * on two integers or
// unary - on one, of type typ, is proven to give a value of typ: it is a
// known value; the bounds of its operands keep its exact result within
// typ's, on every target (see exactRange); or the relations in force do,
// for a known amount added to a node (see linearProven), or for a
// difference whose bounds keep it at or below typ's largest value and the
// relations keep at or above 0 (see orderedSub).
func (c *Checker) overflowProven(e ast.Expr, typ types.Type) bool {
	if _, ok := c.knownValue(e); ok {
		return true
	}
	lo, hi := portableBounds(typ)
	known := c.exactRange(e)
	lowOK := known.hasLo && known.lo.Cmp(lo) >= 0
	highOK := known.hasHi && known.hi.Cmp(hi) <= 0
	if lowOK && highOK {
		return true
	}
	bin, isBin := e.(*ast.Binary)
	if !isBin {
		return false
	}
	highOK = highOK || c.noLarger(bin)
	if lowOK && highOK {
		return true
	}
	if c.linearProven(bin) {
		return true
	}
	return bin.Op == ast.Sub && highOK && c.orderedSub(bin)
}

// noLarger reports whether bin, + or - on integers, gives no more than
// one of its operands, whose value is one of its own type, which widens to
// bin's: a - b with b never negative, or a + b with one operand never
// positive. Its result then cannot pass its type's largest value on any
// target, which the bounds of a target-width type's operands do not show,
// since its largest value is the target's.
func (c *Checker) noLarger(bin *ast.Binary) bool {
	switch bin.Op {
	case ast.Sub:
		b := c.operandBounds(bin.Y)
		return !b.lo.IsNegative()
	case ast.Add:
		a, b := c.operandBounds(bin.X), c.operandBounds(bin.Y)
		notPositive := func(v constant.Int) bool { return v.IsNegative() || v.IsZero() }
		return notPositive(a.hi) || notPositive(b.hi)
	default:
		return false
	}
}

// exactRange returns the bounds of the exact result of e, plain + - * on
// two integers or unary - on one, from its operands' bounds (see
// operandBounds). An end past what a constant holds, which is past every
// type's bounds too, is left unset; a product sets both or neither.
func (c *Checker) exactRange(e ast.Expr) interval {
	var known interval
	switch x := e.(type) {
	case *ast.Binary:
		a, b := c.operandBounds(x.X), c.operandBounds(x.Y)
		switch x.Op {
		case ast.Add:
			known.lo, known.hasLo = a.lo.Add(b.lo)
			known.hi, known.hasHi = a.hi.Add(b.hi)
		case ast.Sub:
			known.lo, known.hasLo = a.lo.Sub(b.hi)
			known.hi, known.hasHi = a.hi.Sub(b.lo)
		default: // ast.Mul
			known.lo, known.hi, known.hasLo = products(a, b)
			known.hasHi = known.hasLo
		}
	case *ast.Unary:
		a := c.operandBounds(x.X)
		known.lo, known.hasLo = a.hi.Neg()
		known.hi, known.hasHi = a.lo.Neg()
	}
	return known
}

// operandBounds returns bounds every value the integer operand e can have
// where it is used lies within: its known value, or a subject's or a
// length's bound (see leafRange); for plain + - * or unary - on integers,
// the exact bounds of its result when they are within its type's on
// every target, so that it is proven, and otherwise its type's, since one
// not proven is reported; for a conversion between integer types, its
// operand's bounds, since a conversion keeps the value, within its type's;
// for a division, a remainder, a bitwise and or a right shift, the bounds
// its operands' give it (see quotientBounds); and for anything else, its
// type's bounds.
func (c *Checker) operandBounds(e ast.Expr) interval {
	if known, ok := c.leafRange(e); ok {
		return known
	}
	typ := ast.TypeOf(e)
	if m, ok := c.memoFor(e); ok {
		return m.bounds
	}
	if isPlainArith(e) && types.IsInteger(typ) {
		if known := c.exactRange(e); known.hasLo && known.hasHi {
			if fits, ok := fitted(known, typ); ok {
				return fits
			}
		}
	}
	if call, ok := e.(*ast.Call); ok && call.Conv != types.Invalid && types.IsInteger(call.Conv) &&
		len(call.Args) == 1 && types.IsInteger(ast.TypeOf(call.Args[0])) {
		return c.operandBounds(call.Args[0]).within(typeInterval(e))
	}
	if bin, ok := e.(*ast.Binary); ok && types.IsInteger(typ) && typ != types.UntypedInt {
		if known, ok := c.quotientBounds(bin); ok {
			return known.within(typeInterval(e))
		}
	}
	return typeInterval(e)
}

// quotientBounds returns bounds bin, an integer operation that only ever
// gives a value no further from 0 than an operand's, lies within, from
// its operands' bounds, and false for any other operation:
//
//   - a / k, for a known k above 0, from a's bounds each divided by k,
//     since truncating toward zero keeps their order;
//   - a / b otherwise, from -m to m, m the larger of a's bounds' sizes:
//     the quotient is never larger in size than a, since a plain
//     division is proven not to be the smallest value divided by -1, and
//     a checked one gives 0 there;
//   - a % b, between 0 and a's bound on each side, since the remainder has
//     a's sign and is no larger in size, and, for a known b, less in size
//     than b;
//   - a & b, with one operand never negative, from 0 to that operand's
//     largest value, since the result's bits are among that operand's;
//   - a >> k, for a known k, from a's bounds each shifted, since shifting
//     right rounds down and keeps their order.
func (c *Checker) quotientBounds(bin *ast.Binary) (interval, bool) {
	a := c.operandBounds(bin.X)
	known := interval{hasLo: true, hasHi: true}
	k, isKnown := c.knownValue(bin.Y)
	switch bin.Op {
	case ast.Div:
		if isKnown && !k.IsZero() && !k.IsNegative() {
			known.lo, _ = a.lo.Quo(k)
			known.hi, _ = a.hi.Quo(k)
			return known, true
		}
		m := magnitude(a)
		known.hi = m
		known.lo, _ = m.Neg()
		return known, true
	case ast.Rem:
		if a.lo.IsNegative() {
			known.lo = a.lo
		}
		if !a.hi.IsNegative() {
			known.hi = a.hi
		}
		if isKnown && !k.IsZero() {
			size := magnitude(interval{lo: k, hi: k})
			below, _ := size.Sub(constant.FromInt64(1))
			neg, _ := below.Neg()
			known = known.within(interval{lo: neg, hi: below})
		}
		return known, true
	case ast.BitAnd:
		b := c.operandBounds(bin.Y)
		switch {
		case !a.lo.IsNegative() && (b.lo.IsNegative() || a.hi.Cmp(b.hi) <= 0):
			known.hi = a.hi
		case !b.lo.IsNegative():
			known.hi = b.hi
		default:
			return interval{}, false
		}
		return known, true
	case ast.Shr:
		if !isKnown || k.IsNegative() {
			return interval{}, false
		}
		known.lo, _ = a.lo.Shr(k.Uint64())
		known.hi, _ = a.hi.Shr(k.Uint64())
		return known, true
	default:
		return interval{}, false
	}
}

// magnitude returns the larger of the sizes of known's two ends, which a
// constant holds: a size is at most 2^63 for a negative end.
func magnitude(known interval) constant.Int {
	size := func(v constant.Int) constant.Int {
		if n, ok := v.Neg(); ok && v.IsNegative() {
			return n
		}
		return v
	}
	lo, hi := size(known.lo), size(known.hi)
	if lo.Cmp(hi) > 0 {
		return lo
	}
	return hi
}

// within returns known narrowed to other's ends, both set: the bounds a
// value both describe lies within.
func (known interval) within(other interval) interval {
	if known.lo.Cmp(other.lo) < 0 {
		known.lo = other.lo
	}
	if known.hi.Cmp(other.hi) > 0 {
		known.hi = other.hi
	}
	return known
}

// linearProven reports whether bin is a node plus or minus a known amount
// whose every step the relations in force keep within its type, counting
// each node's type's bounds (see linear and Checker.typedRelations).
func (c *Checker) linearProven(bin *ast.Binary) bool {
	if bin.Op == ast.Mul {
		return false
	}
	t, ok := c.linear(bin)
	if !ok || t.node.isZero() {
		return false
	}
	if len(t.conds) == 0 {
		return true
	}
	c.typedRelations = true
	defer func() { c.typedRelations = false }()
	g := c.relationGraph(t.node, factKey{}, t.isLen, false)
	return g != nil && g.holdsAll(t.conds)
}

// orderedSub reports whether the relations in force, counting each node's
// type's bounds, prove bin, x - y, at least 0: that y is at most x.
func (c *Checker) orderedSub(bin *ast.Binary) bool {
	x, okX := c.queryTerm(bin.X)
	y, okY := c.queryTerm(bin.Y)
	if !okX || !okY {
		return false
	}
	c.typedRelations = true
	defer func() { c.typedRelations = false }()
	return c.relates(y, x, constant.Int{})
}

// reportOverflow reports chk, an operation not proven to fit its type,
// with the checks that would prove it, where one would, the checked
// form, and the wrapping operator.
func (c *Checker) reportOverflow(chk overflowCheck) {
	if chk.stmt != nil {
		c.errorf(chk.stmt.Spanned(), "%s", c.stepOverflowMessage(chk))
		return
	}
	c.errorf(chk.e.Spanned(), "%s", c.overflowMessage(chk.e, chk.typ))
}

// overflowMessage returns the error for e, plain + - * or unary - of the
// integer type typ, not proven to fit it: what may happen, the check that
// would prove it when its operands are subjects, then the checked form and
// the wrapping operator, each written out with e's own operands.
func (c *Checker) overflowMessage(e ast.Expr, typ types.Type) string {
	text := c.text(e.Spanned())
	tail := "use v, ok := " + text + ", or " + c.wrapText(e) + " to wrap"
	name := c.typeName(typ)
	switch x := e.(type) {
	case *ast.Unary:
		if c.checkSubject(x.X) {
			lo, _ := typeBounds(typ)
			return text + " may overflow " + name + "; check " + c.nameOf(x.X) + " > " + lo.String() +
				" first, or " + tail
		}
	case *ast.Binary:
		if x.Op == ast.Sub && types.IsUnsigned(typ) {
			msg := text + " may go below 0 in " + name + "; "
			if c.checkable(x.X) && c.checkable(x.Y) {
				msg += "check " + c.checkText(x.X) + " >= " + c.checkText(x.Y) + " first, or "
			}
			return msg + tail
		}
		if names := c.boundable(e, nil); len(names) > 0 {
			return text + " may overflow " + name + "; bound " + andList(names) + " first, or " + tail
		}
	}
	return text + " may overflow " + name + "; " + tail
}

// stepOverflowMessage returns the error for chk, an increment, a
// decrement or a compound assignment not proven to fit its target's type:
// what may happen, for an unsigned target going below 0 the check that
// would prove it when the target and the amount are subjects or known,
// then the wrapping compound assignment and the checked form, followed by
// storing its value when its ok is true.
func (c *Checker) stepOverflowMessage(chk overflowCheck) string {
	bin := chk.e.(*ast.Binary)
	target := c.text(bin.X.Spanned())
	amount, operand := "1", "1"
	// The statement is written out from its parts, since its own span
	// leaves out a parenthesis closing its value.
	var stmt string
	if s, ok := chk.stmt.(*ast.IncDec); ok {
		stmt = target + s.Op()
	}
	if a, ok := chk.stmt.(*ast.Assign); ok {
		amount = c.valueText(a)
		operand = amount
		if _, isBin := a.Value.(*ast.Binary); isBin && amount == c.text(a.Value.Spanned()) {
			operand = "(" + amount + ")"
		}
		stmt = target + " " + a.Op.String() + "= " + amount
	}
	name := c.typeName(chk.typ)
	op := bin.Op.String()
	tail := "use " + target + " " + op + "%= " + amount + " to wrap, or v, ok := " + target + " " + op +
		" " + operand + " and " + target + " = v when ok"
	if bin.Op != ast.Sub || !types.IsUnsigned(chk.typ) {
		return stmt + " may overflow " + name + "; " + tail
	}
	msg := stmt + " may go below 0 in " + name + "; "
	if c.checkable(bin.X) {
		if _, isInc := chk.stmt.(*ast.IncDec); isInc {
			msg += "check " + c.nameOf(bin.X) + " > 0 first, "
		} else if c.checkable(bin.Y) {
			msg += "check " + c.nameOf(bin.X) + " >= " + c.checkText(bin.Y) + " first, "
		}
	}
	return msg + tail
}

// valueText returns a compound assignment's value as written, with the
// parentheses around it, which its own span leaves out: one for each
// opened between the assignment's operator and the value.
func (c *Checker) valueText(a *ast.Assign) string {
	vs := a.Value.Spanned()
	gap := c.text(ast.Span{File: vs.File, Start: a.Target.Spanned().End, End: vs.Start})
	if at := strings.IndexByte(gap, '='); at >= 0 {
		gap = gap[at+1:]
	}
	n := strings.Count(gap, "(")
	return strings.Repeat("(", n) + c.text(vs) + strings.Repeat(")", n)
}

// checkable reports whether a check on e, compared in an if, gives a fact
// that bounds it: e is a subject or a known value.
func (c *Checker) checkable(e ast.Expr) bool {
	if _, ok := c.knownValue(e); ok {
		return true
	}
	return c.checkSubject(e)
}

// checkSubject reports whether a check on e gives a fact about it: e is a
// subject, and not a mutable global, which no fact is ever about.
func (c *Checker) checkSubject(e ast.Expr) bool {
	if _, global := c.mutableGlobal(e); global {
		return false
	}
	return c.isSubject(e)
}

// checkText returns how a check names e, a subject or a known value: by
// its name, or as written.
func (c *Checker) checkText(e ast.Expr) string {
	if c.isSubject(e) {
		return c.nameOf(e)
	}
	return c.text(e.Spanned())
}

// boundable returns the names of the subjects among the leaves of e that
// exactRange reads through, plain + - * and unary - on integers and
// conversions between them, each once, in source order, appended to
// names: the values a bound would have to be put on for its operands'
// bounds to prove e.
func (c *Checker) boundable(e ast.Expr, names []string) []string {
	switch x := e.(type) {
	case *ast.Binary:
		if isPlainArith(x) {
			return c.boundable(x.Y, c.boundable(x.X, names))
		}
	case *ast.Unary:
		if isPlainArith(x) {
			return c.boundable(x.X, names)
		}
	case *ast.Call:
		if x.Conv != types.Invalid && len(x.Args) == 1 {
			return c.boundable(x.Args[0], names)
		}
	}
	if _, known := c.knownValue(e); known || !c.checkSubject(e) {
		return names
	}
	name := c.nameOf(e)
	for _, n := range names {
		if n == name {
			return names
		}
	}
	return append(names, name)
}

// andList joins names as a sentence lists them: a, b and c.
func andList(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// wrapText returns e, plain + - * or unary -, as written, with its
// operator replaced by the wrapping one: a +% b for a + b, -%x for -x. A
// binary operator is found between its operands, past any parenthesis
// and line comment there.
func (c *Checker) wrapText(e ast.Expr) string {
	text := c.text(e.Spanned())
	switch x := e.(type) {
	case *ast.Unary:
		return "-%" + text[1:]
	case *ast.Binary:
		start := x.Span.Start
		gap := c.text(ast.Span{File: x.Span.File, Start: x.X.Spanned().End, End: x.Y.Spanned().Start})
		at := operatorIn(gap, x.Op.String()[0])
		if at < 0 {
			return text
		}
		cut := x.X.Spanned().End - start + at + 1
		return text[:cut] + "%" + text[cut:]
	default:
		return text
	}
}

// operatorIn returns where op, an operator's one byte, is in gap, the
// source between a binary expression's operands, skipping line comments,
// or -1 when it is not there.
func operatorIn(gap string, op byte) int {
	for i := 0; i < len(gap); i++ {
		switch {
		case strings.HasPrefix(gap[i:], "//"):
			end := strings.IndexByte(gap[i:], '\n')
			if end < 0 {
				return -1
			}
			i += end
		case gap[i] == op:
			return i
		}
	}
	return -1
}

// wrapping checks a wrapping operator, +% -% or *%, bin, on operands of
// types x and y, and returns the type it works in: both must be integers,
// combined as for + - and *, an untyped constant taking the other's type
// (see typedArithmetic). Two untyped constants have no width to wrap at,
// and are reported. Nothing needs proving: the result wraps at the type's
// width, and on known values it is computed so (see wrapStep).
func (c *Checker) wrapping(bin *ast.Binary, x, y types.Type) types.Type {
	if !c.integerOperands(bin, x, y) {
		return types.Invalid
	}
	if x == types.UntypedInt && y == types.UntypedInt {
		c.untypedWrap(bin)
		return types.Invalid
	}
	return c.typedArithmetic(bin, x, y)
}

// untypedWrap reports bin, a wrapping operator on two untyped constants,
// which have no width to wrap at, suggesting a conversion of its left
// operand to a type both operands are values of, when there is one.
func (c *Checker) untypedWrap(bin *ast.Binary) {
	a, _ := constValue(bin.X)
	b, _ := constValue(bin.Y)
	whole := c.text(bin.Span)
	msg := whole + " wraps at its type's width, but both operands are untyped constants; give one a type"
	if typ, ok := holdingType(a, b); ok {
		xs := bin.X.Spanned()
		left, right := whole[:xs.Start-bin.Span.Start], whole[xs.End-bin.Span.Start:]
		msg += ", as in " + left + c.typeName(typ) + "(" + c.text(xs) + ")" + right
	}
	c.errorf(bin.Span, "%s", msg)
}

// wrapNegation checks -%x, u, x of type x, and returns its type: x's,
// signed or unsigned. An untyped constant has no width to wrap at, and is
// reported, suggesting a conversion to a type it is a value of.
func (c *Checker) wrapNegation(u *ast.Unary, x types.Type) types.Type {
	switch {
	case x == types.Invalid:
		return types.Invalid
	case x == types.UntypedInt:
		v, _ := constValue(u.X)
		msg := c.text(u.Span) + " wraps at its type's width, but its operand is an untyped constant; give it a type"
		if typ, ok := holdingType(v, v); ok {
			msg += ", as in -%" + c.typeName(typ) + "(" + c.text(u.X.Spanned()) + ")"
		}
		c.errorf(u.Span, "%s", msg)
		return types.Invalid
	case types.IsInteger(x):
		return x
	default:
		if x = c.valueOf(u.X, x); x != types.Invalid {
			c.errorf(u.X.Spanned(), "operator -%% is not defined on %s", c.typeName(x))
		}
		return types.Invalid
	}
}

// holdingType returns int64 when a and b are both values of it, and
// otherwise uint64 when they are both values of that, the type a
// suggestion converts an untyped constant to.
func holdingType(a, b constant.Int) (types.Type, bool) {
	switch {
	case a.Fits(64, true) && b.Fits(64, true):
		return types.Int64, true
	case a.Fits(64, false) && b.Fits(64, false):
		return types.Uint64, true
	default:
		return types.Invalid, false
	}
}

// wrapStep computes a op b for op, a wrapping operator, on the two's
// complement forms of a and b, values of the integer type typ, keeping
// the low bits of typ's width: the value the program computes. -%x is
// 0 -% x. A target-width type's width depends on the target, so its
// result is not known.
func wrapStep(op ast.BinaryOp, a, b constant.Int, typ types.Type) (constant.Int, bool) {
	if !types.IsInteger(typ) || typ == types.UntypedInt || types.TargetWidth(typ) {
		return constant.Int{}, false
	}
	x, y := a.Uint64(), b.Uint64()
	var r uint64
	switch op.Unwrapped() {
	case ast.Add:
		r = x + y
	case ast.Sub:
		r = x - y
	default: // ast.Mul
		r = x * y
	}
	shift := 64 - types.MaxBits(typ)
	if types.IsUnsigned(typ) {
		return constant.FromUint64(r << shift >> shift), true
	}
	return constant.FromInt64(int64(r<<shift) >> shift), true //nolint:gosec // G115: two's complement on purpose
}

// arithResults checks value bound to want names when it is the checked
// form of arithmetic, a + b, a - b, a * b or -a on integers, giving the
// result, or 0 when it overflows, and whether it fit, and reports whether
// it was one. The operation itself needs no proof, and is allowed where
// one exists, when ok is always true; arithmetic on known values that
// overflows is still an error, and any operation inside it needs its own
// proof. A float's arithmetic cannot fail and has no checked form, and
// nor has a wrapping operator, which is reported too.
func (c *Checker) arithResults(
	value ast.Expr,
	want int,
	span ast.Span,
) (results []types.Type, ok, isChecked bool) {
	if wrapped, isWrap := wrapForm(value); isWrap {
		if c.expr(value) != types.Invalid {
			c.errorf(span, "%s wraps and cannot fail, so it has no checked form; use %s, or v, ok := %s",
				wrapped[0], wrapped[0], wrapped[1])
		}
		return nil, false, true
	}
	if !isPlainArith(value) {
		return nil, false, false
	}
	c.checkedArith = value
	typ := c.expr(value)
	c.checkedArith = nil
	if typ == types.Invalid {
		return nil, false, true
	}
	verb, form := arithWords(value)
	if types.IsFloat(typ) {
		c.errorf(span, "%s floats cannot fail, so it has no checked form; use %s", verb, form)
		return nil, false, true
	}
	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but %s gives 2 values", want, arithNoun(value))
		return nil, false, true
	}
	return []types.Type{typ, types.Bool}, true, true
}

// wrapForm reports whether value is a wrapping operator, and returns how
// an error writes it and the checked form of the operator it wraps, a +%
// b and a + b, or -%a and -a.
func wrapForm(value ast.Expr) ([2]string, bool) {
	switch x := value.(type) {
	case *ast.Binary:
		if x.Op.IsWrapping() {
			return [2]string{"a " + x.Op.String() + " b", "a " + x.Op.Unwrapped().String() + " b"}, true
		}
	case *ast.Unary:
		if x.Op == ast.NegWrap {
			return [2]string{"-%a", "-a"}, true
		}
	}
	return [2]string{}, false
}

// arithWords returns how an error on the checked form of value, plain
// + - * or unary -, names the operation done on floats, and the plain
// form it suggests instead.
func arithWords(value ast.Expr) (verb, form string) {
	if bin, ok := value.(*ast.Binary); ok {
		switch bin.Op {
		case ast.Add:
			return "adding", "a + b"
		case ast.Sub:
			return "subtracting", "a - b"
		default:
			return "multiplying", "a * b"
		}
	}
	return "negating", "-a"
}

// arithNoun returns how an error names value, plain + - * or unary -: an
// addition, a subtraction, a multiplication or a negation.
func arithNoun(value ast.Expr) string {
	if bin, ok := value.(*ast.Binary); ok {
		switch bin.Op {
		case ast.Add:
			return "an addition"
		case ast.Sub:
			return "a subtraction"
		default:
			return "a multiplication"
		}
	}
	return "a negation"
}

// A signed division can overflow too, at one quotient: the smallest value
// of its type divided by -1 is one past its largest. So a / b and x /= b
// on a signed integer type compile only where the checker proves, besides
// a divisor that is not zero, that the division is not that one: the
// divisor is not -1, or the dividend is not the smallest value (see
// quotientProven). Anything else is an error that suggests the check that
// would prove it, and the checked form, q, ok := a / b, which gives 0 and
// false at that quotient as for a zero divisor. A division whose divisor
// is not proven not zero is reported for that alone, as an operation is
// whose operand is not proven (see operandUnproven), since a check that
// fixes it, b > 0, may fix both. An unsigned division cannot overflow,
// and nor can a remainder: the smallest value % -1 is 0.

// isSigned reports whether typ is a typed signed integer type, one whose
// division can overflow.
func isSigned(typ types.Type) bool {
	return types.IsInteger(typ) && !types.IsUnsigned(typ) && typ != types.UntypedInt
}

// requireNoQuotientOverflow reports bin, a / b of the integer type typ,
// unless typ is unsigned, the division is proven not to be the smallest
// value of typ divided by -1 (see quotientProven), or an operand of it is
// plain arithmetic not proven, which is reported on its own. stmt is the
// compound assignment bin stands for, x /= b, when there is one, which
// the error names instead.
func (c *Checker) requireNoQuotientOverflow(bin *ast.Binary, typ types.Type, stmt *ast.Assign) {
	if !isSigned(typ) || c.quotientProven(bin, typ) || c.operandUnproven(bin) {
		return
	}
	if stmt != nil {
		c.errorf(stmt.Span, "%s", c.quotientMessage(bin, typ, stmt))
		return
	}
	c.errorf(bin.Span, "%s", c.quotientMessage(bin, typ, nil))
}

// quotientProven reports whether bin, a / b of the signed integer type
// typ, is proven not to be typ's smallest value divided by -1: the
// divisor is ruled out being -1, or the dividend being that smallest
// value (see excludes).
func (c *Checker) quotientProven(bin *ast.Binary, typ types.Type) bool {
	least, _ := typeBounds(typ)
	return c.excludes(bin.Y, constant.FromInt64(-1)) || c.excludes(bin.X, least)
}

// excludes reports whether e, an integer operand, is known not to be v
// where it is used: its known value is another, its bounds leave v out
// (see operandBounds), or, for a subject, a fact in force rules v out,
// e != v.
func (c *Checker) excludes(e ast.Expr, v constant.Int) bool {
	if k, ok := c.knownValue(e); ok {
		return k.Cmp(v) != 0
	}
	known := c.operandBounds(e)
	if known.lo.Cmp(v) > 0 || known.hi.Cmp(v) < 0 {
		return true
	}
	k, ok := c.subjectKey(e)
	if !ok {
		return false
	}
	for f := range c.heldFactsAbout(k) {
		if f.hasNe && f.ne.Cmp(v) == 0 && c.holds(f) {
			return true
		}
	}
	return false
}

// quotientMessage returns the error for bin, a / b of the signed integer
// type typ, not proven not to overflow: what may happen, the check that
// would prove it when the divisor, or else the dividend, is a subject
// whose value is not known, b != -1 or a > the smallest value, then the
// checked form. For stmt, x /= b, it names the statement and suggests the
// checked form of x / b, followed by storing its value when ok is true.
func (c *Checker) quotientMessage(bin *ast.Binary, typ types.Type, stmt *ast.Assign) string {
	text := c.text(bin.Span)
	tail := "use q, ok := " + text
	if stmt != nil {
		target := c.text(bin.X.Spanned())
		amount := c.valueText(stmt)
		operand := amount
		if _, isBin := stmt.Value.(*ast.Binary); isBin && amount == c.text(stmt.Value.Spanned()) {
			operand = "(" + amount + ")"
		}
		text = target + " /= " + amount
		tail = "use q, ok := " + target + " / " + operand + " and " + target + " = q when ok"
	}
	msg := text + " may overflow " + c.typeName(typ) + "; "
	switch {
	case c.unknownSubject(bin.Y):
		msg += "check " + c.nameOf(bin.Y) + " != -1 first, or "
	case c.unknownSubject(bin.X):
		least, _ := typeBounds(typ)
		msg += "check " + c.nameOf(bin.X) + " > " + least.String() + " first, or "
	}
	return msg + tail
}

// unknownSubject reports whether e is a subject a check gives a fact
// about (see checkSubject) whose value is not known, so that a check on
// it can prove something.
func (c *Checker) unknownSubject(e ast.Expr) bool {
	if _, known := c.knownValue(e); known {
		return false
	}
	return c.checkSubject(e)
}

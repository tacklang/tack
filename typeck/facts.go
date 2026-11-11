package typeck

import (
	"math"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// The checker proves a divisor is not zero, an index in range, and a
// conversion's operand a value of its type (see convfacts.go), from
// facts: what is known about a variable's value within some region of the
// program, that it is not zero, or bounds on it. A fact comes from a
// condition that has just been tested, or from a range loop, whose index
// runs from 0 to its count, or the length of what it ranges over, less one.
// It holds inside an if's branches, a loop's body, and the right side of &&
// and ||, according to whether the condition was true there, and in the
// rest of a block after an if whose branch leaves it. The operand proven
// may also be arithmetic on subjects, bounded from what the facts say of
// each (see arith.go). A switch's cases are that if's chain; see
// switch.go. Deferred code is checked with the facts
// in force at its defer, since the region a fact holds in takes in the rest
// of the defer's block, where the code runs; see defer.go. The rules are
// syntactic, so any reader can follow them:
//
//   - a fact is about a parameter, a local variable, or an immutable global,
//     or a field or element of an immutable variable or parameter (see
//     subject.go); a mutable global can be changed by any call, so it never
//     has one, and a field of a mutable variable never has one either;
//   - a fact holds in its region up to the first write to the variable
//     that can reach the use (see reaches): one that takes effect before
//     the use, a call's mut argument at the end of the call and an
//     assignment's target at the end of the statement, so the call or
//     assignment the fact is for does not end it; one in a loop inside the
//     region that holds the use too; but neither when it is in a branch or
//     a case that leaves (one ending in a switch only when the switch has
//     a default, see leavesBody), and the use is outside that if or
//     switch, before it or after it, and checked at or before the point a
//     break or continue out of the branch can reach code past: the end of
//     the loop in the region it breaks out of, when that is the outermost
//     one, and otherwise the start of the outermost loop in the region
//     holding the if or switch, whose next pass it can reach (see keep and
//     leftBefore); and, for a use in deferred
//     code, or in a switch's default written before other cases, one that
//     runs before that code although written after it. A counting loop's start,
//     and a range loop's bounds, which hold of what the loop began with,
//     end at any write in their region. The region is everything that can
//     run after the comparison and before the use: the rest of the
//     condition the comparison is part of, the branch that falls through
//     past an early exit, later case values,
//     every value tried since a switch's tag was evaluated, for a fact
//     about the tag, and a counting loop's condition for the start it
//     counts from;
//   - a condition gives facts through && (when true), || (when false), !,
//     and comparisons of a variable with a known value: b != 0, i >= 0,
//     i < len(a), i == 3; a variable's type bounds it too, so an unsigned
//     one is never negative. Integers of any two types compare by their
//     values, so the known value, whatever its type, bounds the variable's
//     value, which no comparison wraps: after i < uint64(3), an int64 i
//     is at most 2 and may still be negative. b != k for a known k that
//     is -1 or a signed width's smallest value rules that one value out,
//     which only a signed division's proof asks about (see
//     quotientProven, divisionEdge), and no note describes; no
//     other proof counts it, so after b >= -1 && b != -1, b is not known
//     to be at or above 0. || when true and && when false give what
//     both sides agree on, the union of their facts, for each variable
//     both sides constrain, a value ruled out not among them;
//   - has(m, k) true gives the relation that k is in the map m, for a map
//     that is a subject and a key that is a constant or a name nothing
//     assigns, or a path below one (see memberOf); it ends at any write
//     to m but not at one to a value m holds, which changes no key (see
//     writes), and only a map's element asks about it;
//   - the length of a string parameter or mutable variable has no known
//     value (an immutable one holding a known string has; see
//     knownString), so two more comparisons give facts: len(s) with a known value bounds the length,
//     as len(s) > 0 does, and i < len(s) bounds i by it, as a range loop
//     over len(s), or over s itself, does its index, whatever i's integer
//     type, a uint64 or a target-width one included. A fact that names s
//     holds only up to a write to either variable that can reach the use;
//     s may be mutable, since nothing then changes it before the use;
//   - for a slice's bounds, which may reach a length but not pass it, and
//     must be in order, two relations give facts too: j <= len(s) bounds
//     j by the length itself, and i <= j, or i < j, puts i at or below j,
//     for integer variables i and j of any types. These relations are
//     only ever asked about by a slice's proof, and are left out of what
//     explains an index's or a divisor's missing proof, and of what || and
//     && unite, as a bound by a length is.

// fact is something known about a subject's value in a region, about: that
// it is not zero, or that it is not ne, -1 or a signed width's smallest
// value (see divisionEdge), or that
// it lies at or above lo, or at or below hi, or
// below the length of the string subject lenOf, or at or below the length
// of the string subject atMostLen, or at or below the integer subject
// atMost, below it when strict, or that it is a key in the map subject
// inMap, which a key whose value is known is by known instead, about being
// zero then (see memberFact), or a difference bound between two values,
// diff (see relations.go). The last four are relations (see relational).
// A subject is a variable or
// a path below an immutable one (see subject.go). For a string subject,
// the fact can be about its length instead, which about then names.
//
// out says ne is a value the fact rules out, which every b != k sets,
// whatever k, and which, for a fact that holds nothing else, only the
// proof of an unreachable place counts (see proofOnly). either carries
// the sides of a true || or a false && on a fact that holds nothing
// else, which only that proof counts too (see eitherFacts).
//
// A fact also records where it came from, for the notes that explain a
// missing proof: src, the comparison, or the && or || whose sides it
// unites, that gave it, when there is one, and guard, the condition, loop,
// case or early exit whose region it was put in force for. drop is set on a
// fact that does not hold where it is: one the region drops because it
// assigns the variable, and one that would hold but for how its region is
// written (see explain.go). Such a fact is kept in force only to explain an
// error, and every query that proves something skips it: see holds. ends
// holds every write in the fact's region that could end it, each of which
// ends it only for the code it can reach (see reaches).
type fact struct {
	src          ast.Expr
	guard        *guard
	drop         *drop
	about        factKey
	lenOf        factKey
	atMostLen    factKey
	atMost       factKey
	inMap        factKey
	diff         *diffBound
	known        string
	ends         *endings
	lo, hi, ne   constant.Int
	hasLo, hasHi bool
	hasNe        bool
	nonzero      bool
	strict       bool
	out          bool
	either       *eitherFacts
}

// holds reports whether f is in force where checking has reached, c.at,
// and so may prove something: it was not dropped, and no write in its
// region that can reach c.at ends it (see dropOf).
func (c *Checker) holds(f fact) bool {
	return c.dropOf(f) == nil
}

// dropOf returns why f does not hold at c.at: the drop it was put in force
// with, or else the first write in its region that reaches c.at, or nil
// when it holds there (see firstReach).
func (c *Checker) dropOf(f fact) *drop {
	if f.drop != nil {
		return f.drop
	}
	return c.firstReach(f.ends)
}

// empty reports whether f says nothing about its variable: no bound, not
// nonzero, no other value ruled out, no length bounding it, and no
// relation.
func (f fact) empty() bool {
	return !f.hasLo && !f.hasHi && !f.nonzero && !f.hasNe && f.lenOf.isZero() && !f.relational()
}

// relational reports whether f is a relation: one a slice's bounds need,
// its subject at or below a string's or a view's length, or at or below
// another subject, or a key being in a map, which only a map's element
// needs, or a difference bound, which only a query of the relations in
// force asks about (see relates). Such a fact gives no bound of its own.
func (f fact) relational() bool {
	return !f.atMostLen.isZero() || !f.atMost.isZero() || !f.inMap.isZero() || f.diff != nil
}

// interval is what all the facts in force say about a variable's value: at
// or above lo when hasLo, at or below hi when hasHi, and not zero when
// nonzero.
type interval struct {
	lo, hi       constant.Int
	hasLo, hasHi bool
	nonzero      bool
}

// nonzero reports whether e is known not to be zero where it is checked: a
// constant other than zero, an immutable variable whose value is one, a
// subject, a variable or a path below an immutable one, the facts in force
// keep away from zero, len(s) of a string, list or view subject whose
// length they keep at 1 or more (see minLen), or arithmetic whose bound
// keeps it away from zero (see rangeOf).
func (c *Checker) nonzero(e ast.Expr) bool {
	if v, ok := c.knownValue(e); ok {
		return !v.IsZero()
	}
	if _, ok := c.stringLen(e); ok {
		return c.minLen(e.(*ast.Call).Args[0]).Cmp(constant.FromInt64(1)) >= 0
	}
	known, ok := c.operandRange(e)
	return ok && known.excludesZero()
}

// inRange reports whether an index that is a subject, a variable or a
// path below an immutable one, or arithmetic (see isArithmetic), is known
// to lie in [0, n) where it is used, by what its type and the facts in
// force say of it (see operandRange), or, for a bound that does not give,
// by the relations in force (see relates).
func (c *Checker) inRange(index ast.Expr, n int64) bool {
	known, ok := c.operandRange(index)
	if !ok || n == 0 {
		return false
	}
	last := constant.FromInt64(n - 1)
	return (known.hasLo && !known.lo.IsNegative() || c.atLeast(index)) &&
		(known.hasHi && known.hi.Cmp(last) <= 0 || c.atMostBy(index, nil, last))
}

// intervalOf returns what is known about the value of e, a subject: its
// type's bounds, narrowed by every fact in force about it.
func (c *Checker) intervalOf(e ast.Expr) interval {
	known := typeInterval(e)
	k, ok := c.subjectKey(e)
	if !ok {
		return known
	}
	for f := range c.heldFactsAbout(k) {
		if c.holds(f) {
			known = known.narrow(f)
		}
	}
	return known
}

// narrow returns what is known once the fact f holds as well: the higher of
// the two lower bounds, the lower of the two upper bounds.
func (known interval) narrow(f fact) interval {
	known.nonzero = known.nonzero || f.nonzero
	if f.hasLo && (!known.hasLo || f.lo.Cmp(known.lo) > 0) {
		known.lo, known.hasLo = f.lo, true
	}
	if f.hasHi && (!known.hasHi || f.hi.Cmp(known.hi) < 0) {
		known.hi, known.hasHi = f.hi, true
	}
	return known
}

// typeBounds returns the smallest and largest values of a sized integer
// type. A target-width type's largest is the largest it can have on any
// target, a 64-bit one's, so no value it holds anywhere lies outside them.
func typeBounds(typ types.Type) (lo, hi constant.Int) {
	n := types.MaxBits(typ)
	if types.IsUnsigned(typ) {
		return constant.Int{}, constant.FromUint64(math.MaxUint64 >> (64 - n))
	}
	return constant.FromInt64(
			math.MinInt64 >> (64 - n),
		), constant.FromInt64(
			math.MaxInt64 >> (64 - n),
		)
}

// knownValue returns the value of e when the checker knows it: a constant
// expression, len of an array or of a known string (see knownString), an
// immutable variable initialized with one, at package level or in a
// function, a field or an element of a known struct or array, such as a
// field of an immutable global, or a byte of a known string, at a known
// index (see foldPart), a conversion of a known value that is a value of
// its type, or arithmetic, a bitwise operator, a shift, a negation or a
// complement on known values whose exact result is a value of its type,
// as in len(a) - 1 or k << 2. A float's value is never known this way:
// floats carry no facts.
func (c *Checker) knownValue(e ast.Expr) (constant.Int, bool) {
	if types.IsFloat(ast.TypeOf(e)) {
		return constant.Int{}, false
	}
	if v, ok := constValue(e); ok {
		return v, true
	}
	switch x := e.(type) {
	case *ast.Binary:
		return c.knownArithmetic(x)
	case *ast.Call:
		return c.knownCall(x)
	case *ast.Selector, *ast.Index:
		v, ok := c.foldPart(x)
		k, isInt := v.(constant.Int)
		return k, ok && isInt
	case *ast.Unary:
		return c.knownUnary(x)
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return constant.Int{}, false
	}
	return c.knownVar(id)
}

// knownUnary returns the value of -x or ^x, u, when x's value is known and
// the result is a value of u's type on every target. ^x of a signed type
// is -x - 1, and of an unsigned one x with every bit of its width flipped,
// which a target-width type's width leaves unknown.
func (c *Checker) knownUnary(u *ast.Unary) (constant.Int, bool) {
	v, ok := c.knownValue(u.X)
	switch {
	case !ok:
		return constant.Int{}, false
	case u.Op == ast.Neg:
		return fitting(v.Neg())(u.Type)
	case u.Op == ast.NegWrap:
		return wrapStep(ast.SubWrap, constant.Int{}, v, u.Type)
	case u.Op != ast.Complement || types.TargetWidth(u.Type):
		return constant.Int{}, false
	case types.IsUnsigned(u.Type):
		mask := uint64(math.MaxUint64) >> (64 - types.MaxBits(u.Type))
		return fitting(v.Xor(constant.FromUint64(mask)))(u.Type)
	default:
		return fitting(v.Not())(u.Type)
	}
}

// knownVar returns the value of the variable id names, when it is
// immutable and its value is known. A local's answer is remembered, since
// its initializer, already checked, never changes, and computing it again
// for every use would take time exponential in a chain of variables each
// used twice by the next.
func (c *Checker) knownVar(id *ast.Ident) (constant.Int, bool) {
	v, ok := id.Def.(*ast.Var)
	if !ok || v.Mutable {
		return constant.Int{}, false
	}
	if k, ok := v.Const.(constant.Int); ok {
		return k, true
	}
	// A global's value is the Const its check computes, which is done
	// before anything reads it: a global with none has no known value,
	// and following its initializer could go round an initialization
	// cycle forever.
	if v.Value == nil || c.globals[v] {
		return constant.Int{}, false
	}
	if r, seen := c.knownVars[v]; seen {
		return r.value, r.ok
	}
	c.work.locals++
	value, known := c.knownValue(v.Value)
	c.knownVars[v] = knownResult{value, known}
	return value, known
}

// knownCall returns the value of a call when it is known: a conversion of
// a known value, or len of a known string.
func (c *Checker) knownCall(call *ast.Call) (constant.Int, bool) {
	if call.Conv != types.Invalid {
		return c.knownConversion(call)
	}
	return c.knownStringLen(call)
}

// knownConversion returns the value of a conversion T(x) of a known value
// x that is a value of the integer type T on every target, which is that
// value. One that is not has no value, and was reported when the
// conversion was checked, unless it fits a target-width T on some targets
// only, which is checked at run time.
func (c *Checker) knownConversion(call *ast.Call) (constant.Int, bool) {
	if len(call.Args) != 1 || !types.IsInteger(call.Conv) {
		return constant.Int{}, false
	}
	v, ok := c.knownValue(call.Args[0])
	if !ok || !fitsPortably(v, call.Conv) {
		return constant.Int{}, false
	}
	return v, true
}

// knownStringLen returns the length of the string call takes the len of,
// when call is len(s) and s is a known string, or of the slice it takes
// the len of, when its bounds are known and it calls nothing (see
// sliceExtent). len of a literal is already a constant, recorded on the
// call; this adds an immutable variable holding one.
func (c *Checker) knownStringLen(call *ast.Call) (constant.Int, bool) {
	if !isLen(call) || len(call.Args) != 1 {
		return constant.Int{}, false
	}
	if n, ok := c.knownLen(call.Args[0]); ok {
		return constant.FromInt64(n), true
	}
	if s, ok := call.Args[0].(*ast.Slice); ok && !hasCall(s) {
		if n, ok := c.sliceExtent(s); ok {
			return constant.FromInt64(n), true
		}
	}
	return constant.Int{}, false
}

// knownString returns the value of e when it is a string the checker
// knows: a literal, an immutable variable, local or package-level,
// initialized with a known string, a field or an element of a known
// struct or array (see foldPart), or a slice of a known string at known
// bounds (see knownSlice). Such a variable always holds that
// string, since nothing can assign it. A mutable variable, a parameter and
// a call are never known.
func (c *Checker) knownString(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.StrLit:
		return x.Value, true
	case *ast.Selector, *ast.Index:
		v, ok := c.foldPart(x)
		s, isString := v.(string)
		return s, ok && isString
	case *ast.Slice:
		return c.knownSlice(x)
	case *ast.Ident:
		return c.knownStringVar(x)
	default:
		return "", false
	}
}

// knownStringVar returns the string the variable id names holds, when it
// is immutable and initialized with a known string.
func (c *Checker) knownStringVar(id *ast.Ident) (string, bool) {
	v, ok := id.Def.(*ast.Var)
	if !ok || v.Mutable || v.Type != types.String {
		return "", false
	}
	if s, ok := v.Const.(string); ok {
		return s, true
	}
	if v.Value == nil {
		return "", false
	}
	return c.knownString(v.Value)
}

// knownLen returns the length of s, in bytes, when s is a known string.
func (c *Checker) knownLen(s ast.Expr) (int64, bool) {
	str, ok := c.knownString(s)
	return int64(len(str)), ok
}

// knownResult is what knownVar found out about a variable: its value, and
// whether it is known.
type knownResult struct {
	value constant.Int
	ok    bool
}

// knownBinary is what knownArithmetic found out about a binary, and the
// binary's type when it did.
type knownBinary struct {
	result knownResult
	typ    types.Type
}

// knownArithmetic computes arithmetic on two known values, exactly, and
// keeps the result only if it is a value of the expression's type, so that
// it is what the program computes too.
//
// The answer for a binary that has its type is remembered, for as long as
// it keeps that type: an operator asks about its operands as it is
// checked, so each operator of a chain would otherwise compute the whole
// chain below it again, in time growing with the square of its length.
// Nothing else it depends on changes once its operands are checked. A
// body checked twice (see checkBody) forgets every answer between the two.
func (c *Checker) knownArithmetic(bin *ast.Binary) (constant.Int, bool) {
	if !bin.Op.IsInteger() {
		return constant.Int{}, false
	}
	typed := bin.Type != types.Invalid && bin.Type != types.UntypedInt
	if known, seen := c.knownBinaries[bin]; typed && seen && known.typ == bin.Type {
		return known.result.value, known.result.ok
	}
	value, ok := c.computeArithmetic(bin)
	if typed {
		c.knownBinaries[bin] = knownBinary{knownResult{value, ok}, bin.Type}
	}
	return value, ok
}

// computeArithmetic is knownArithmetic, with no answer remembered: the
// known values of bin's operands, and the operation on them.
func (c *Checker) computeArithmetic(bin *ast.Binary) (constant.Int, bool) {
	c.work.arithmetic++
	a, okA := c.knownValue(bin.X)
	b, okB := c.knownValue(bin.Y)
	if !okA || !okB {
		return constant.Int{}, false
	}
	return knownStep(bin.Op, a, b, bin.Type)
}

// knownStep computes a op b, exactly, for the arithmetic, bitwise or
// shift operator op, and keeps the result only if it is a value of the
// type typ the operation works in. A division or remainder by zero has no
// value. A bitwise operator on two values of typ always gives one, and a
// left shift is exact, so one that shifts a set bit out of typ gives none.
func knownStep(op ast.BinaryOp, a, b constant.Int, typ types.Type) (constant.Int, bool) {
	switch {
	case op == ast.Add:
		return fitting(a.Add(b))(typ)
	case op == ast.Sub:
		return fitting(a.Sub(b))(typ)
	case op == ast.Mul:
		return fitting(a.Mul(b))(typ)
	case op.IsBitwise():
		return fitting(bitwiseStep(op, a, b))(typ)
	case op.IsShift():
		return fitting(shiftStep(op, a, b))(typ)
	case op.IsWrapping():
		return wrapStep(op, a, b, typ)
	}
	if b.IsZero() {
		return constant.Int{}, false
	}
	if op == ast.Div {
		return fitting(a.Quo(b))(typ)
	}
	return fitting(a.Rem(b))(typ)
}

// bitwiseStep computes a op b, exactly, for the bitwise operator op, on
// the values' two's complement forms (see constant.Int.And), and reports
// whether the result is in the range constants lie in.
func bitwiseStep(op ast.BinaryOp, a, b constant.Int) (constant.Int, bool) {
	switch op {
	case ast.BitAnd:
		return a.And(b)
	case ast.BitOr:
		return a.Or(b)
	case ast.BitXor:
		return a.Xor(b)
	default: // ast.AndNot
		return a.AndNot(b)
	}
}

// shiftStep computes a op n, exactly, for the shift operator op: a << n
// is a times 2^n, and a >> n a divided by 2^n, rounded down. It reports
// whether the result is in the range constants lie in; a negative count,
// which the checker reports, gives none.
func shiftStep(op ast.BinaryOp, a, n constant.Int) (constant.Int, bool) {
	if n.IsNegative() {
		return constant.Int{}, false
	}
	if op == ast.Shl {
		return a.Shl(n.Uint64())
	}
	return a.Shr(n.Uint64())
}

// fitting turns the result of a constant operation into a known value of
// the type typ: it is known only if it was in range and is a value of typ
// on every target, so a target-width type's is not known past
// types.PortableMax, where the program may wrap or not.
func fitting(v constant.Int, ok bool) func(typ types.Type) (constant.Int, bool) {
	return func(typ types.Type) (constant.Int, bool) {
		if !ok || !types.IsInteger(typ) || typ == types.UntypedInt {
			return v, ok && typ == types.UntypedInt
		}
		return v, fitsPortably(v, typ)
	}
}

// factsFrom returns the facts cond gives where it evaluated to holds. &&
// gives both sides' facts when true, and || both sides' facts of being
// false when false. When one side or the other holds, && false or || true,
// only what both sides agree on holds: see union. ! swaps; a comparison
// of a variable with a known value gives one fact.
//
// The right side runs after the left, so a fact the left side gives is
// dropped when the right side assigns its variable, a call passing it as
// mut included: the right side may undo it before the condition's value
// is known. A left side that decides the value on its own, && false or
// || true, runs nothing after it, and the right side's own facts come
// after every part of the condition that runs.
//
// The facts of the last && or || binary checked are kept (see joinFacts),
// and asked about that binary, factsFrom returns a copy of them rather
// than walking its left side again.
func (c *Checker) factsFrom(cond ast.Expr, holds bool) []fact {
	if facts, ok := c.chain.of(cond, holds); ok {
		return facts
	}
	c.work.conditions++
	switch x := cond.(type) {
	case *ast.Ident:
		return c.boolFact(x, holds)
	case *ast.Call:
		if holds && isHas(x) {
			return c.memberFact(x)
		}
	case *ast.Unary:
		if x.Op == ast.Not {
			return c.factsFrom(x.X, !holds)
		}
	case *ast.Binary:
		return c.binaryFacts(x, holds)
	}
	return nil
}

// binaryFacts returns the facts x, a binary condition, gives when its
// value is holds, as factsFrom does: a comparison's, both sides' of an &&
// that holds or an || that does not, the left side's dropped where the
// right side assigns their variables, or what either side's allow, of an
// && that does not hold or an || that does, followed by a fact carrying
// each side's own, for the proof of an unreachable place (see eitherFact).
func (c *Checker) binaryFacts(x *ast.Binary, holds bool) []fact {
	switch {
	case x.Op == ast.And && holds, x.Op == ast.Or && !holds:
		left := c.dropAssigned(c.factsFrom(x.X, holds), []ast.Node{x.Y})
		return append(left, c.factsFrom(x.Y, holds)...)
	case x.Op == ast.And, x.Op == ast.Or:
		left, right := c.factsFrom(x.X, holds), c.factsFrom(x.Y, holds)
		return append(from(union(left, right), x), eitherFact(left, right))
	case x.Op.IsComparison():
		return c.comparisonFact(x, holds)
	}
	return nil
}

// chainFacts is the facts of one && or || binary, as factsFrom gives them
// when its value is the one that lets its right side run, true for && and
// false for ||. They are the facts of its left side, each dropped where
// its right side assigns its variable, then its right side's.
type chainFacts struct {
	bin   *ast.Binary
	facts []fact
}

// of returns a copy of the facts held, when they are cond's when its
// value is holds, and whether they are.
func (ch chainFacts) of(cond ast.Expr, holds bool) ([]fact, bool) {
	if ch.bin == nil || cond != ch.bin || (ch.bin.Op == ast.And) != holds {
		return nil, false
	}
	return slices.Clone(ch.facts), true
}

// takeChain returns the facts factsFrom gives for cond when its value is
// holds, handing over the facts of the last && or || binary checked, when
// cond is that binary, rather than a copy of them, and forgetting them.
// binary asks for its left side's facts this way: that left side is
// asked about once, by the binary it is the left side of, which then
// records facts of its own (see joinFacts), so no copy is needed, and a
// chain of n operators copies none of its facts at each one.
func (c *Checker) takeChain(cond ast.Expr, holds bool) []fact {
	ch := c.chain
	if ch.bin == nil || cond != ch.bin || (ch.bin.Op == ast.And) != holds {
		return c.factsFrom(cond, holds)
	}
	c.chain = chainFacts{}
	return ch.facts
}

// joinFacts records the facts of bin, a checked && or || binary, whose
// left side gives left, as chainFacts holds them: left, each dropped
// where bin's right side assigns its variable, then the right side's
// facts. factsFrom gives the same for bin, by walking its left side
// again; a chain of n operators would walk n of them at each one. left
// is the caller's to give up: factsFrom returns a slice of its own, and
// assume copies each fact it puts in force, so joinFacts marks and
// extends left in place rather than copying it again.
func (c *Checker) joinFacts(bin *ast.Binary, left []fact) {
	holds := bin.Op == ast.And
	joined := c.dropAssigned(left, []ast.Node{bin.Y})
	joined = append(joined, c.factsFrom(bin.Y, holds)...)
	c.chain = chainFacts{bin: bin, facts: joined}
}

// boolFact returns the fact a bool variable, id, tested on its own as a
// condition gives, when its value is holds: that it is true, held as 1,
// or false, held as 0, when it can have facts. Only the ok of a checked
// pop or remove is asked about one (see okTrue).
func (c *Checker) boolFact(id *ast.Ident, holds bool) []fact {
	if ast.TypeOf(id) != types.Bool {
		return nil
	}
	about, ok := c.keyOf(id)
	if !ok {
		return nil
	}
	v := constant.FromInt64(0)
	if holds {
		v = constant.FromInt64(1)
	}
	return from([]fact{bound(about, ast.Eq, v)}, id)
}

// from records src as where each of facts came from, and returns them.
func from(facts []fact, src ast.Expr) []fact {
	for i := range facts {
		facts[i].src = src
	}
	return facts
}

// union returns the facts that hold when the facts a hold or the facts b
// do, not knowing which, leaving out any fact either side has dropped:
// for each variable both say something about, the smallest interval
// holding what each side allows, and not zero when neither side allows
// zero. So b < 0 || b > 0 gives b != 0, and
// i == 0 || i == 1 gives 0 <= i <= 1. A variable only one side constrains
// gets nothing, since the other side may be the one that holds. A bound by
// a string's length, i < len(s), is not carried over.
func union(a, b []fact) []fact {
	var out []fact
	seen := make(map[factKey]bool)
	for _, f := range a {
		if seen[f.about] || f.relational() || f.drop != nil || f.either != nil {
			continue
		}
		seen[f.about] = true
		other, ok := sideInterval(b, f.about)
		if !ok {
			continue
		}
		mine, _ := sideInterval(a, f.about)
		out = append(out, mine.hull(other).fact(f.about))
	}
	return out
}

// joinFacts returns what union does for a and b, the facts at two
// points control reaches one place from, looking each side's facts up by
// subject once rather than once for each subject: both sides may hold a
// fact for each case tried before them in a switch of thousands.
func joinFacts(a, b []fact) []fact {
	order, mine := factIntervals(a)
	_, other := factIntervals(b)
	var out []fact
	for _, k := range order {
		if theirs, ok := other[k]; ok {
			out = append(out, mine[k].hull(theirs).fact(k))
		}
	}
	return out
}

// factIntervals returns what facts say about each subject they are about,
// as sideInterval gives it, and the subjects in the order facts first
// name them.
func factIntervals(facts []fact) ([]factKey, map[factKey]interval) {
	var order []factKey
	known := make(map[factKey]interval)
	for _, f := range facts {
		if f.relational() || f.drop != nil || f.either != nil {
			continue
		}
		v, seen := known[f.about]
		if !seen {
			order = append(order, f.about)
		}
		known[f.about] = v.narrow(f)
	}
	return order, known
}

// sideInterval returns what the facts in force of one side say about the
// subject k, and whether any of them is about it.
func sideInterval(facts []fact, k factKey) (interval, bool) {
	var known interval
	found := false
	for _, f := range facts {
		if f.about == k && !f.relational() && f.drop == nil && f.either == nil {
			known, found = known.narrow(f), true
		}
	}
	return known, found
}

// hull returns the smallest interval that holds every value known or other
// allows: the lower of the lower bounds and the higher of the upper ones,
// each only when both have it, and not zero when neither allows zero.
func (known interval) hull(other interval) interval {
	h := interval{nonzero: known.excludesZero() && other.excludesZero()}
	if known.hasLo && other.hasLo {
		h.lo, h.hasLo = known.lo, true
		if other.lo.Cmp(known.lo) < 0 {
			h.lo = other.lo
		}
	}
	if known.hasHi && other.hasHi {
		h.hi, h.hasHi = known.hi, true
		if other.hi.Cmp(known.hi) > 0 {
			h.hi = other.hi
		}
	}
	return h
}

// excludesZero reports whether known rules out zero: it says so, or its
// bounds lie wholly above or below it.
func (known interval) excludesZero() bool {
	zero := constant.Int{}
	return known.nonzero || known.hasLo && known.lo.Cmp(zero) > 0 ||
		known.hasHi && known.hi.Cmp(zero) < 0
}

// fact returns the fact that the subject k lies in known.
func (known interval) fact(k factKey) fact {
	return fact{
		about:   k,
		lo:      known.lo,
		hi:      known.hi,
		hasLo:   known.hasLo,
		hasHi:   known.hasHi,
		nonzero: known.nonzero,
	}
}

// comparisonFact returns the fact a comparison gives, when its value is
// holds: about a variable compared with a known value, the length of a
// string variable compared with one, or a variable below a string
// variable's length. Either side may be the one the fact is about. A
// comparison that gives none of those may give difference bounds (see
// linearFacts).
func (c *Checker) comparisonFact(cmp *ast.Binary, holds bool) []fact {
	op := cmp.Op
	if !holds {
		op = negate(op)
	}
	if f, ok := c.boolRuledOut(cmp.X, op, cmp.Y); ok {
		return from([]fact{f}, cmp)
	}
	if f, ok := c.orientedFact(cmp.X, op, cmp.Y); ok {
		return from([]fact{f}, cmp)
	}
	if f, ok := c.orientedFact(cmp.Y, flip(op), cmp.X); ok {
		return from([]fact{f}, cmp)
	}
	return from(c.linearFacts(cmp.X, op, cmp.Y), cmp)
}

// boolRuledOut returns the fact a comparison of a bool subject with true
// or false gives, either side being the literal, when it holds with op
// != : that the subject is not the literal's value, 1 for true and 0 for
// false, as boolFact holds a bool. Such a fact is only a value ruled out
// (see ruledOut), which only the proof of an unreachable place counts, so
// a switch on a bool naming both values leaves its default no value. A
// comparison that holds with == gives nothing.
func (c *Checker) boolRuledOut(x ast.Expr, op ast.BinaryOp, y ast.Expr) (fact, bool) {
	lit, ok := y.(*ast.BoolLit)
	if !ok {
		lit, ok = x.(*ast.BoolLit)
		x = y
	}
	if !ok || op != ast.Ne || ast.TypeOf(x) != types.Bool {
		return fact{}, false
	}
	about, ok := c.keyOf(x)
	if !ok {
		return fact{}, false
	}
	v := constant.FromInt64(0)
	if lit.Value {
		v = constant.FromInt64(1)
	}
	return fact{about: about, ne: v, out: true}, true
}

// orientedFact returns the fact that left op right gives about left: a
// subject, or len of a string subject, compared with a known value, a
// subject less than len of a string subject, or one of the relations a
// slice needs (relation).
func (c *Checker) orientedFact(left ast.Expr, op ast.BinaryOp, right ast.Expr) (fact, bool) {
	about, ok := c.subject(left)
	if !ok {
		return fact{}, false
	}
	if k, known := c.knownValue(right); known {
		return bound(about, op, k), true
	}
	if s, ok := c.stringLen(right); ok && !isLenCall(left) && op == ast.Lt {
		return fact{about: about, lenOf: s}, true
	}
	return c.relation(left, about, op, right)
}

// relation returns the relation left op right gives about left, an
// integer subject whose key is about, other than a length: at or below
// len(s), for left <= len(s), or at or below right, an integer subject,
// for left <= right, or below it, strictly, for left < right.
func (c *Checker) relation(
	left ast.Expr,
	about factKey,
	op ast.BinaryOp,
	right ast.Expr,
) (fact, bool) {
	if (op != ast.Le && op != ast.Lt) || isLenCall(left) || !types.IsInteger(ast.TypeOf(left)) {
		return fact{}, false
	}
	if s, ok := c.stringLen(right); ok {
		return fact{about: about, atMostLen: s}, op == ast.Le
	}
	if other, ok := c.keyOf(right); ok && types.IsInteger(ast.TypeOf(right)) {
		return fact{about: about, atMost: other, strict: op == ast.Lt}, true
	}
	return fact{}, false
}

// subject returns the subject a fact about e would be about: the variable
// e names, or the path below an immutable one e is, when it can have
// facts, the code of an error variable, mutable or not (errorCodeKey), or
// the string subject whose length e is, len(s).
func (c *Checker) subject(e ast.Expr) (factKey, bool) {
	if k, ok := c.keyOf(e); ok {
		return k, true
	}
	if k, ok := c.errorCodeKey(e); ok {
		return k, true
	}
	return c.stringLen(e)
}

// stringLen returns the string subject s when e is len(s) and s can have
// facts.
func (c *Checker) stringLen(e ast.Expr) (factKey, bool) {
	call, ok := e.(*ast.Call)
	if !ok || !isLen(call) || len(call.Args) != 1 {
		return factKey{}, false
	}
	return c.stringVar(call.Args[0])
}

// stringVar returns the string subject e is, a variable or a path below an
// immutable one, when it can have facts. A view parameter is one too: its
// length bounds an index as a string's does; and so is a list, whose
// length only assigning it, passing it as mut or moving it changes.
func (c *Checker) stringVar(e ast.Expr) (factKey, bool) {
	if typ := ast.TypeOf(e); typ != types.String && !c.table.IsView(typ) && !c.table.IsList(typ) {
		return factKey{}, false
	}
	return c.keyOf(e)
}

// bound returns the fact that the subject about compares with k as op
// says.
func bound(about factKey, op ast.BinaryOp, k constant.Int) fact {
	f := fact{about: about}
	one := constant.FromInt64(1)
	switch op {
	case ast.Eq:
		f.lo, f.hi, f.hasLo, f.hasHi = k, k, true, true
	case ast.Ne:
		f.nonzero = k.IsZero()
		f.ne, f.hasNe, f.out = k, divisionEdge(k), true
	case ast.Lt:
		f.hi, f.hasHi = k.Sub(one)
	case ast.Le:
		f.hi, f.hasHi = k, true
	case ast.Gt:
		f.lo, f.hasLo = k.Add(one)
	default: // ast.Ge
		f.lo, f.hasLo = k, true
	}
	return f
}

// divisionEdge reports whether k is a value a signed division's proof
// asks to rule out (see quotientProven): -1, or the smallest value of a
// signed width. A comparison b != k with any other k but 0 gives
// no fact, as it never proves anything, so a long chain of them, as an
// else-if chain over many values makes, puts nothing in force.
func divisionEdge(k constant.Int) bool {
	if k.Cmp(constant.FromInt64(-1)) == 0 {
		return true
	}
	for _, bits := range []uint{8, 16, 32, 64} {
		if k.Cmp(constant.FromInt64(math.MinInt64>>(64-bits))) == 0 {
			return true
		}
	}
	return false
}

// canHoldFact reports whether decl can have a fact: a parameter, or a
// variable other than a mutable global.
func (c *Checker) canHoldFact(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.Param:
		return true
	case *ast.Var:
		return !d.Mutable || !c.globals[d]
	default:
		return false
	}
}

// flip returns the operator that compares the same way with its operands
// swapped: a < b is b > a.
func flip(op ast.BinaryOp) ast.BinaryOp {
	switch op {
	case ast.Lt:
		return ast.Gt
	case ast.Le:
		return ast.Ge
	case ast.Gt:
		return ast.Lt
	case ast.Ge:
		return ast.Le
	default:
		return op
	}
}

// negate returns the comparison that holds exactly when op does not.
func negate(op ast.BinaryOp) ast.BinaryOp {
	switch op {
	case ast.Eq:
		return ast.Ne
	case ast.Ne:
		return ast.Eq
	case ast.Lt:
		return ast.Ge
	case ast.Le:
		return ast.Gt
	case ast.Gt:
		return ast.Le
	default: // ast.Ge
		return ast.Lt
	}
}

// assume puts facts in force for the region about to be checked, coming
// from g. Each fact keeps every write in the region to its variable, or
// to the string whose length bounds it, or the subject a relation bounds
// it by (see writeIndex.endings): the fact holds for code no such write can reach
// (see reaches), and is dropped, marked with the first write that does,
// everywhere else, so it proves nothing there but can explain an error. A
// fact that says nothing, such as the one b != 3 gives, is left out
// before the region is searched, which a long chain of cases or ifs
// would otherwise do once per link; one that carries a value ruled out
// or the sides of a condition is kept apart instead, for the proof of an
// unreachable place alone (see keepApart). g itself is put in scope too.
// Everything assume puts in force stays until a release to a mark taken
// before it.
//
// region is everything that can run after facts became true and before,
// or while, the code they guard runs: statements, and the parts of a
// condition or of a switch's cases still to run, in the order they run.
// It must be entered only through g: no path from a write in it may come
// back to code in it without passing g again, but through a loop inside
// it. A loop whose body is the region, and whose condition is g, meets
// that, while a loop counting from a start, whose start is not tried
// again, does not, and its facts are dropped by any write in their region
// before they are put in force (see dropAssigned). A fact that already
// carries a drop, such as one a later part of its own condition ends (see
// factsFrom), keeps it.
func (c *Checker) assume(g *guard, facts []fact, nodes []ast.Node) {
	c.assumeIn(g, facts, region{head: nodes})
}

// assumeIn is assume for a region that may end in the rest of a block,
// shared with the other regions over it (see region.go).
func (c *Checker) assumeIn(g *guard, facts []fact, r region) {
	c.guards = append(c.guards, g)
	c.factGen++
	var idx *regionIndex
	for _, f := range facts {
		c.noteRelated(f)
		if f.empty() {
			c.keepApart(g, f, r)
			continue
		}
		f.guard = g
		if f.drop == nil {
			if idx == nil {
				idx = c.indexRegion(r)
			}
			f.ends = idx.endings(f)
		}
		c.active = append(c.active, f)
		c.indexFact()
	}
}

// mark records how many facts and guards are in force, and how many
// known values the probe remembers (see recordStart), for release.
type mark struct {
	facts, guards, starts, proven int
}

// mark returns how many facts and guards are in force now, and how many
// known values the probe remembers.
func (c *Checker) mark() mark {
	return mark{len(c.active), len(c.guards), len(c.starts), len(c.proven)}
}

// dropAssigned returns facts with every one that region assigns the
// variable of, as writeIndex.assigned says, marked dropped by that
// assignment, a value ruled out included, and each side's facts of one
// carrying the sides of a condition. A fact already dropped keeps its
// drop. The region is walked
// once for all the facts, not once for each (see writeIndex).
func (c *Checker) dropAssigned(facts []fact, region []ast.Node) []fact {
	var idx writeIndex
	for i := range facts {
		if facts[i].either != nil {
			facts[i].either = c.dropSides(facts[i].either, region)
			continue
		}
		if facts[i].drop == nil && (!facts[i].empty() || facts[i].out) {
			if idx == nil {
				idx = c.indexWrites(region)
			}
			facts[i].drop = idx.assigned(facts[i])
		}
	}
	return facts
}

// stmtNodes returns the statements of body as a region assume and
// dropAssigned search.
func stmtNodes(body []ast.Stmt) []ast.Node {
	nodes := make([]ast.Node, len(body))
	for i, stmt := range body {
		nodes[i] = stmt
	}
	return nodes
}

// release takes out of force every fact and guard assumed since m was
// taken, and forgets every known value the probe remembered since.
func (c *Checker) release(m mark) {
	if len(c.active) != m.facts {
		c.factGen++
	}
	c.unindexFacts(m.facts)
	c.active = c.active[:m.facts]
	c.guards = c.guards[:m.guards]
	c.starts = c.starts[:m.starts]
	c.proven = c.proven[:m.proven]
}

// assigned returns why the region idx indexes drops f: the first write
// in it, in source order, to f's variable, or else to the string whose
// length bounds it, or the subject a relation bounds it by, or the map it
// is a key of, or either node of a difference bound, a call passing any
// of them as mut included. It returns nil when the region writes none of
// them.
func (idx writeIndex) assigned(f fact) *drop {
	if len(idx) == 0 {
		return nil
	}
	var buf [7]factKey
	for _, k := range f.allKeys(&buf) {
		if k.isZero() {
			continue
		}
		if ws := idx[k.decl]; len(ws) > 0 {
			return ws[0].drop
		}
	}
	return nil
}

// keep is a branch of an if, or a case's body, in a fact's region, that
// leaves (see leaves): end is where its last statement ends, and whole
// is the if or switch it is part of. A write in it cannot fall out of it:
// every path on from the write ends at a return or a call to exit, which
// reach nothing after (deferred code aside, see reaches), or at a break
// or continue that leaves it. One whose loop is outside the region leaves
// the region too, and comes back to it only through the guard that put
// the fact in force, which proves it again (see assume). One whose loop
// is in the region lands there, and from that point on the write can
// reach any code past resume: the end of that loop, after a break out of
// the outermost loop in the region holding the if, and otherwise the
// start of that outermost loop, whose next pass the landing point can
// reach. resume is math.MaxInt when no break or continue in it lands in
// the region. So a write in it reaches no code outside whole whose check
// point is at or before resume (see leftBefore).
//
// The keeps holding a node form a chain through parent, innermost first,
// which every write in the same branch shares rather than copies. Each
// keep's whole holds the wholes of the keeps inside it, since an inner
// if or switch is in the outer one's branch.
//
// skip is the nearest keep out along parent whose resume is greater than
// this one's, or nil. leftBefore steps from one keep to its skip, so a
// walk takes one step for each distinct resume on the chain, not one for
// each keep (see leftBefore).
type keep struct {
	parent, skip *keep
	whole        ast.Span
	end, resume  int
}

// link puts k in force inside parent, the keep around it or nil, setting
// k's skip (see keep): out from parent, each keep whose resume is at most
// k's is passed, with the run its own skip passes, until one with a
// greater resume. Each step lands on a greater resume than the last, so
// it takes one step for each distinct resume out from parent, each counted
// in work (see proverWork).
func (k *keep) link(parent *keep, work *int) {
	k.parent = parent
	s := parent
	for s != nil && s.resume <= k.resume {
		*work++
		s = s.skip
	}
	k.skip = s
}

// keepWalk finds, as indexWrites walks a region, the chain of keeps that
// holds each node (see keep). An if or a switch records each of its
// branches or cases that leaves in pending, under the branch's first
// statement; the walk pushes the keep when it reaches that statement,
// and pops it once past the branch's end. leaves and jumps remember what
// each if, switch and defer in the region gives, and fallen what each
// case a fallthrough enters gives, so a chain of else ifs, each the last
// statement of the one before, or of cases each falling into the next,
// is looked at once in all, not once for every link. work counts each
// statement looked at to find them (see proverWork).
type keepWalk struct {
	top     *keep
	pending map[ast.Stmt]keep
	leaves  map[ast.Stmt]bool
	jumps   map[ast.Stmt]jumpsOut
	fallen  map[*ast.CaseClause]jumpsOut
	work    *int

	// lits holds the closures the walk is in, innermost last. A return in
	// a closure leaves only the closure, and the call it is passed to goes
	// on, so no branch in one is a keep.
	lits []ast.Span
}

// jumpsOut says whether a statement holds a break, and whether it holds
// a continue, that acts on a loop around it: one at any depth but inside
// a loop in it, which acts on that loop. A break or continue in a switch
// acts on the loop around the switch, so a switch is searched.
type jumpsOut struct {
	breaks, continues bool
}

// newKeepWalk returns a keepWalk with nothing in force, counting its work
// in work.
func newKeepWalk(work *int) *keepWalk {
	return &keepWalk{
		pending: make(map[ast.Stmt]keep),
		leaves:  make(map[ast.Stmt]bool),
		jumps:   make(map[ast.Stmt]jumpsOut),
		fallen:  make(map[*ast.CaseClause]jumpsOut),
		work:    work,
	}
}

// visit moves the walk to n, whose span is span, held by loops, every
// loop in the region holding it, outermost first, and returns the chain
// of keeps holding n: it pops every keep whose branch ends before n,
// pushes the one whose branch n starts, if any, and records the branches
// of n, an if, or the cases of n, a switch, that leave. A keep is pushed
// only once the walk is in its branch, so every keep in the chain holds
// n. Inside a closure it records none, and pushes none, since none is
// recorded there (see lits).
func (w *keepWalk) visit(n ast.Node, span ast.Span, loops []ast.Span) *keep {
	for w.top != nil && span.Start >= w.top.end {
		w.top = w.top.parent
	}
	for len(w.lits) > 0 && span.Start >= w.lits[len(w.lits)-1].End {
		w.lits = w.lits[:len(w.lits)-1]
	}
	if _, ok := n.(*ast.FuncLit); ok {
		w.lits = append(w.lits, span)
	}
	if len(w.lits) > 0 {
		return w.top
	}
	if stmt, ok := n.(ast.Stmt); ok {
		if k, ok := w.pending[stmt]; ok {
			delete(w.pending, stmt)
			k.link(w.top, w.work)
			w.top = &k
		}
	}
	switch s := n.(type) {
	case *ast.If:
		w.record(s, s.Then, loops)
		w.record(s, s.Else, loops)
	case *ast.Switch:
		for _, cl := range s.Cases {
			w.record(s, cl.Body, loops)
		}
	}
	return w.top
}

// record makes body, a branch or a case of n held by loops, pending as a
// keep when it leaves, to be pushed when the walk reaches its first
// statement.
func (w *keepWalk) record(n ast.Stmt, body []ast.Stmt, loops []ast.Span) {
	if !w.leavesBody(body) {
		return
	}
	w.pending[body[0]] = keep{
		whole:  n.Spanned(),
		end:    body[len(body)-1].Spanned().End,
		resume: w.resumeFrom(body, loops),
	}
}

// leavesBody reports what leaves does for body, remembering it for each
// if and switch it looks at, whose own branches it asks about in turn,
// with one difference: a switch leaves only as a chain of cases ending
// in a default does (see chainLeaves), never as a switch on a union
// naming every case. Whether a switch names every case is known only once
// it is checked (see Switch.Exhaustive), and a region is indexed before
// the code in it is checked, except when a function is checked twice (see
// checkBody), so asking would make whether a program compiles depend on
// that. Without a default, a switch is taken to fall out of the bottom,
// which only ever counts a write as reaching more code than it can, so it
// rejects programs but accepts none it should not. A body ending in a
// fallthrough leaves as the body it enters does, asked the same way.
func (w *keepWalk) leavesBody(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	last := body[len(body)-1]
	switch last.(type) {
	case *ast.If, *ast.Switch, *ast.Fallthrough:
	default:
		return leaves(body)
	}
	if v, ok := w.leaves[last]; ok {
		return v
	}
	*w.work++
	var v bool
	switch s := last.(type) {
	case *ast.If:
		v = w.leavesBody(s.Then) && w.leavesBody(s.Else)
	case *ast.Switch:
		v = chainLeavesBy(tryOrder(s), w.leavesBody)
	case *ast.Fallthrough:
		v = s.Next != nil && w.leavesBody(s.Next.Body)
	}
	w.leaves[last] = v
	return v
}

// resumeFrom returns a keep's resume for body, a branch or case that
// leaves, held by loops, every loop in the region around it, outermost
// first: math.MaxInt when no loop in the region holds body, or no break
// or continue leaves body; the end of the outermost loop when every one
// that does is a break and that loop is the only one; and its start
// otherwise. A case ending in a fallthrough leaves through the cases it
// falls into, so a break or continue in any of them counts.
func (w *keepWalk) resumeFrom(body []ast.Stmt, loops []ast.Span) int {
	if len(loops) == 0 {
		return math.MaxInt
	}
	j := w.jumpsIn(body)
	if next := ast.FallsInto(body); next != nil {
		j = j.or(w.fallenJumps(next))
	}
	switch {
	case j.continues || j.breaks && len(loops) > 1:
		return loops[0].Start
	case j.breaks:
		return loops[0].End
	default:
		return math.MaxInt
	}
}

// fallenJumps returns the jumpsOut of the body of cl, a case a
// fallthrough enters, together with those of every case it falls into in
// turn, remembering each case's, so a long chain of fallthroughs is
// looked at once in all.
func (w *keepWalk) fallenJumps(cl *ast.CaseClause) jumpsOut {
	if j, ok := w.fallen[cl]; ok {
		return j
	}
	j := w.jumpsIn(cl.Body)
	if next := ast.FallsInto(cl.Body); next != nil {
		j = j.or(w.fallenJumps(next))
	}
	w.fallen[cl] = j
	return j
}

// or returns the jumpsOut of two statements together.
func (j jumpsOut) or(k jumpsOut) jumpsOut {
	return jumpsOut{breaks: j.breaks || k.breaks, continues: j.continues || k.continues}
}

// jumpsIn returns the jumpsOut of the statements of body together.
func (w *keepWalk) jumpsIn(body []ast.Stmt) jumpsOut {
	var j jumpsOut
	for _, stmt := range body {
		j = j.or(w.jumpsOf(stmt))
	}
	return j
}

// jumpsOf returns the jumpsOut of stmt, remembering it for an if, a
// switch or a defer, the statements that hold others a break or continue
// acting outside them may be in (see heldJumps). A loop holds none that
// act outside it.
func (w *keepWalk) jumpsOf(stmt ast.Stmt) jumpsOut {
	*w.work++
	switch stmt.(type) {
	case *ast.Break:
		return jumpsOut{breaks: true}
	case *ast.Continue:
		return jumpsOut{continues: true}
	case *ast.If, *ast.Switch, *ast.Defer:
		if j, ok := w.jumps[stmt]; ok {
			return j
		}
		j := w.heldJumps(stmt)
		w.jumps[stmt] = j
		return j
	default:
		return jumpsOut{}
	}
}

// heldJumps returns the jumpsOut of the statements stmt, an if, a switch
// or a defer, holds: an if's branches, a switch's cases' bodies, or a
// defer's body.
func (w *keepWalk) heldJumps(stmt ast.Stmt) jumpsOut {
	switch s := stmt.(type) {
	case *ast.If:
		return w.jumpsIn(s.Then).or(w.jumpsIn(s.Else))
	case *ast.Switch:
		var j jumpsOut
		for _, cl := range s.Cases {
			j = j.or(w.jumpsIn(cl.Body))
		}
		return j
	case *ast.Defer:
		return w.jumpsIn(s.Body)
	default:
		return jumpsOut{}
	}
}

// leftBefore reports whether e cannot reach c.at by falling through: a
// branch or case that holds it and leaves does not hold c.at, nor does
// the if or switch it is part of, and c.at comes before the point any
// break or continue out of it can reach code from (see keep).
//
// c.at at a keep's resume counts as before it, so that whether a program
// compiles does not depend on where it puts a brace: a return's contract
// is checked at the return's end plus one (see checkReturn), which is a
// loop's end exactly when the return is written right against the loop's
// closing brace. That is sound. A resume is math.MaxInt, which c.at never
// is, or a loop's start or end. A write in a loop reaches this test only
// for a c.at past its own position or inside the outermost loop in the
// region holding it (see reaches), and that loop's start is neither, so
// c.at is never a loop's start here. A loop's end is a resume only when
// every break or continue out of the branch is a break out of that loop,
// the outermost in the region, so the write can reach only what runs
// after the loop. Every node after the loop ends past its end, and so do
// a return after it, plus one, and the function's closing brace, one past
// it at the least, when written right against the loop's (so a resume of
// one past the end would not be sound, see leave-write-ensures-end-braces
// in examples/invalid.txtar); every
// statement holding the loop ends at a closing brace of its own past the
// loop's. So a c.at at the loop's end belongs to the loop itself or to
// code inside it, none of which runs again after a break out of it.
//
// Asked keep by keep, innermost first, the answer is no at the first
// keep whose whole holds c.at, since every keep outside it then holds
// c.at too and the write may reach it; yes at the first, before that,
// that c.at is at or before the resume of; and no when no keep is
// either. The walk asks the same in fewer steps. At a keep k that is
// neither, c.at is past k.resume, so past the resume of every keep k.skip
// passes, each at most k's: none of them can answer yes, so each of them
// either answers no, when its whole holds c.at, or passes the question
// on. When k.skip is a keep, its whole holds every whole the walk
// skipped, since they nest, so a skipped keep answering no means k.skip
// answers no too, and asking k.skip next gives the same answer. When
// k.skip is nil, the skipped keeps can only answer no or pass the
// question to the end of the chain, which answers no. So the walk asks
// k and goes on at k.skip, whose resume is greater. Along one chain the
// outermost loop in the region around each if or switch is the same
// loop, or none further out, so there are at most three distinct
// resumes, its start, its end and math.MaxInt, and at most three steps.
// Each keep looked at counts as work (see proverWork).
func (c *Checker) leftBefore(e ending) bool {
	for k := e.kept; k != nil; k = k.skip {
		c.work.judged++
		if inSpan(c.at, k.whole) {
			return false
		}
		if c.at <= k.resume {
			return true
		}
	}
	return false
}

// ending is a write in a fact's region that ends the fact for the code it
// can reach: drop says what it is, and at where it takes effect, the end
// of the call it passes the variable as mut to, or of the assignment
// statement whose target it is. Either runs after everything written
// before that end: a call's arguments, and the callee's own check of
// them, come before it assigns its mut arguments, and an assignment's
// value, and every check in it, before its target is stored to. loop is
// the outermost loop in the region that holds the write, through whose
// back edge it can reach code before it, or the zero span. kept is the
// chain of every branch or case in the region that holds the write and
// leaves (see keep), each of which bounds the code it can reach, or nil.
type ending struct {
	drop *drop
	kept *keep
	loop ast.Span
	at   int
}

// endings returns every write in the region idx indexes that may end
// f, in the order the region holds them: each assignment to f's
// variable, or to the string whose length bounds it, or the subject a
// relation bounds it by, or the map it is a key of, or either node of a
// difference bound, a call passing any of them as mut included. Each
// knows the outermost loop in the region that holds it. It returns nil
// when there are none.
func (idx writeIndex) endings(f fact) *endings {
	if len(idx) == 0 {
		return nil
	}
	var ends []ending
	var buf [7]factKey
	var seenBuf [7]ast.Decl
	seen := seenBuf[:0]
	for _, k := range f.allKeys(&buf) {
		if k.isZero() || slices.Contains(seen, k.decl) {
			continue
		}
		seen = append(seen, k.decl)
		for _, w := range idx[k.decl] {
			ends = append(ends, endingOf(w))
		}
	}
	if len(ends) == 0 {
		return nil
	}
	return &endings{list: ends}
}

// writeIndex holds every write in a region, by the declaration it writes,
// each declaration's in the order the region holds them, as eachWrite
// finds them. It is built for one use, since whether a target writes
// through a list depends on types the checker may not have recorded yet
// (see throughList): assume and dropAssigned each build one and ask it
// about every fact they take, rather than walking the region once a fact.
type writeIndex map[ast.Decl][]write

// indexWrites returns the writeIndex of region, from one walk of it:
// every write eachWrite would find for each declaration, in the same
// order, with the same loops.
func (c *Checker) indexWrites(region []ast.Node) writeIndex {
	idx := make(writeIndex)
	c.walkWrites(region, func(_ int, n ast.Node, at write) {
		c.indexNode(idx, n, at)
	})
	return idx
}

// walkWrites walks each of nodes on its own, the loops and the keeps
// around a node found within the one walked from alone, and calls each
// with the position in nodes of the one walked from, every node met, and
// a write holding the loops around that node, the keeps holding it, and
// where its writes take effect, for indexNode to add its writes to.
// Each node met counts as walked (see proverWork).
func (c *Checker) walkWrites(nodes []ast.Node, each func(i int, n ast.Node, at write)) {
	var loops []ast.Span
	keeps := newKeepWalk(&c.work.leaving)
	var i int
	visit := func(n ast.Node) bool {
		c.work.walked++
		span := n.Spanned()
		for len(loops) > 0 && span.Start >= loops[len(loops)-1].End {
			loops = loops[:len(loops)-1]
		}
		if isLoop(n) {
			loops = append(loops[:len(loops):len(loops)], span)
		}
		kept := keeps.visit(n, span, loops)
		each(i, n, write{kept: kept, loops: loops, at: span.End})
		return true
	}
	for i = range nodes {
		loops, keeps.top, keeps.lits = nil, nil, nil
		ast.Walk(nodes[i], visit)
	}
}

// indexNode adds to idx the writes the node n makes, as eachWrite finds
// them: each target it assigns, then, for a call, the first argument
// passing each declaration as mut, as mutWriter finds it. at holds the
// loops around n and where its writes take effect.
func (c *Checker) indexNode(idx writeIndex, n ast.Node, at write) {
	add := func(w write, decl ast.Decl) {
		w.drop = c.writeDrop(w, decl)
		idx[decl] = append(idx[decl], w)
	}
	for _, target := range assignedTargets(n) {
		if decl := c.writtenDecl(target); decl != nil {
			w := at
			w.shown = target
			add(w, decl)
		}
	}
	call, ok := n.(*ast.Call)
	if !ok || isSwap(call) {
		return
	}
	var done []ast.Decl
	for _, arg := range call.Operands() {
		m, ok := arg.(*ast.MutArg)
		if !ok {
			continue
		}
		if decl := c.writtenDecl(m.X); decl != nil && !slices.Contains(done, decl) {
			done = append(done, decl)
			w := at
			w.shown, w.call = m, call
			add(w, decl)
		}
	}
}

// writtenDecl returns the declaration assigning to target changes, as
// writes decides it, or nil for none.
func (c *Checker) writtenDecl(target ast.Expr) ast.Decl {
	root := ast.Root(target)
	if root == nil || root.Def == nil || c.throughList(target) {
		return nil
	}
	return root.Def
}

// writeDrop returns why w, a write to decl, ends a fact: an assignment
// to decl, or a call passing it as mut.
func (c *Checker) writeDrop(w write, decl ast.Decl) *drop {
	if w.call != nil {
		return &drop{
			kind: dropMut, at: w.shown.Spanned(), name: declName(decl), text: c.factCallee(w.call),
		}
	}
	return &drop{kind: dropAssigned, at: w.shown.Spanned(), name: declName(decl)}
}

// write is a write to a variable that eachWrite found: shown, what a note
// about it underlines, the target or the mut argument; call, the call it
// is a mut argument of, or nil; at, where it takes effect, as an
// ending's at; loops, every loop the walk passed that holds it,
// outermost first; and, in a writeIndex, kept, as an ending's kept.
type write struct {
	shown ast.Node
	call  *ast.Call
	drop  *drop // why the write ends a fact, set only in a writeIndex
	kept  *keep // the branches that leave holding it, set only in a writeIndex
	loops []ast.Span
	at    int
}

// eachWrite calls found with every write to decl in nodes, however deeply
// nested, in the order the walk meets them: an assignment's target, a
// compound assignment's, an increment's or decrement's, one of an
// assignment of several values, or a mut argument, which a call may
// assign through. Each node is walked on its own, so a write's loops are
// those inside the node that hold it, found as the walk passes them: a
// node is in a loop passed before it exactly when it starts before the
// loop ends.
func (c *Checker) eachWrite(nodes []ast.Node, decl ast.Decl, found func(w write)) {
	var loops []ast.Span
	visit := func(n ast.Node) bool {
		span := n.Spanned()
		for len(loops) > 0 && span.Start >= loops[len(loops)-1].End {
			loops = loops[:len(loops)-1]
		}
		if isLoop(n) {
			loops = append(loops[:len(loops):len(loops)], span)
		}
		for _, target := range assignedTargets(n) {
			if c.writes(target, decl) {
				found(write{shown: target, loops: loops, at: span.End})
			}
		}
		if call, ok := n.(*ast.Call); ok {
			if m := c.mutWriter(call, decl); m != nil {
				found(write{shown: m, call: call, loops: loops, at: span.End})
			}
		}
		return true
	}
	for _, n := range nodes {
		loops = nil
		ast.Walk(n, visit)
	}
}

// assignedTargets returns the places n, a statement, assigns: an
// assignment's target, a compound assignment's, an increment's or
// decrement's, or each of an assignment of several values; or nil for
// any other node.
func assignedTargets(n ast.Node) []ast.Expr {
	switch x := n.(type) {
	case *ast.Assign:
		return []ast.Expr{x.Target}
	case *ast.IncDec:
		return []ast.Expr{x.Target}
	case *ast.MultiAssign:
		return x.Targets
	default:
		return nil
	}
}

// isLoop reports whether n is a loop, of either kind, or a closure, which
// is one as far as a write in it reaching a use goes: the call it is
// passed to may run it any number of times, so a write in its body can
// reach code before it in the body, on a later run, as a write in a loop
// can on a later pass.
func isLoop(n ast.Node) bool {
	switch n.(type) {
	case *ast.For, *ast.ForRange, *ast.FuncLit:
		return true
	default:
		return false
	}
}

// reaches reports whether e, a write in the region of a fact, can run
// after the fact became true and before the check at c.at, the end of
// the expression or statement being checked, without the fact being
// proved again: c.at is in deferred code, which runs when its block is
// left, by a return too, and e is outside it; or c.at is in a switch's
// default written before cases whose values are tried before it runs,
// or in a case that default falls into, and e is in that switch outside
// that case (see lateDefault); or c.at
// is in the targets of an assignment and e is anywhere in that
// assignment, its value or its targets (see storing); or else e takes
// effect before c.at, or is in a loop inside the region that c.at is in
// too, whose next pass reaches c.at, and in either case is not in a
// branch or case that leaves before reaching c.at (see leftBefore).
func (c *Checker) reaches(e ending) bool {
	switch {
	case c.deferSpan != nil && !inSpan(e.at, *c.deferSpan):
		return true
	case c.storing != nil && inSpan(e.at, *c.storing):
		return true
	case c.beforeLateDefault(e):
		return true
	case e.at < c.at, e.loop.Start < c.at && c.at <= e.loop.End:
		return !c.leftBefore(e)
	default:
		return false
	}
}

// beforeLateDefault reports whether e is in a switch whose default,
// written before some of its cases, or a case that default falls into, is
// being checked, and outside that case: every case's values are tried
// before the default runs, and so before any case it falls into.
func (c *Checker) beforeLateDefault(e ending) bool {
	for _, d := range c.lateDefaults {
		if inSpan(e.at, d.whole) && !inSpan(e.at, d.body) {
			return true
		}
	}
	return false
}

// inSpan reports whether the position at, the end of a node, lies in
// span: after its start and at or before its end.
func inSpan(at int, span ast.Span) bool {
	return span.Start < at && at <= span.End
}

// factCallee returns the name call calls, as calleeName gives it, or "a
// function" for a call it cannot name.
func (c *Checker) factCallee(call *ast.Call) string {
	if name, ok := c.calleeName(call); ok {
		return name
	}
	return "a function"
}

// mutWriter returns the first of call's arguments, its receiver first,
// that passes decl, or a part of it, as mut, or nil when none does. A
// call to swap writes only two elements of its list, which no fact is
// about, and changes no length, so it writes nothing a fact holds.
func (c *Checker) mutWriter(call *ast.Call, decl ast.Decl) *ast.MutArg {
	if isSwap(call) {
		return nil
	}
	for _, arg := range call.Operands() {
		if m, ok := arg.(*ast.MutArg); ok && c.writes(m.X, decl) {
			return m
		}
	}
	return nil
}

// declName returns the name a parameter or variable is declared with.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.Param:
		return d.Name
	case *ast.Var:
		return d.Name
	default:
		return ""
	}
}

// writes reports whether assigning to target changes decl: target is decl
// itself, or an element or a field of it, but not an element of a list or
// a value of a map, or a part of one, which changes no variable a fact can
// be about: a list's element and a map's value are never subjects, and
// writing one changes no list's length and no map's keys.
func (c *Checker) writes(target ast.Expr, decl ast.Decl) bool {
	root := ast.Root(target)
	return root != nil && root.Def == decl && !c.throughList(target)
}

// throughList reports whether target, a place, is an element of a list or
// a value of a map, or a field or an element of one, at any depth.
func (c *Checker) throughList(target ast.Expr) bool {
	for {
		switch x := target.(type) {
		case *ast.Index:
			if typ := ast.TypeOf(x.X); c.table.IsList(typ) || c.table.IsMap(typ) {
				return true
			}
			target = x.X
		case *ast.Selector:
			target = x.X
		default:
			return false
		}
	}
}

// leaves reports whether a block always leaves the code around it before
// its end: its last statement is a return, a break, a continue, a call
// to exit, which never returns, an unreachable, which is proven never
// reached, an if whose branches both leave, a
// switch with a default, or on a union with every case named, whose cases
// all leave, or a fallthrough into a case whose body leaves, which is
// where the fallthrough goes on to. Like the terminating rule, it is
// syntactic. The checker asks through Checker.leaves, which remembers
// the answers (see leavememo.go).
func leaves(body []ast.Stmt) bool {
	return leavesBy(body, leaves)
}

// leavesBy is leaves with bodyLeaves deciding whether each block inside
// body's last statement leaves, as leaves does, which the checker,
// remembering what it found, passes (see Checker.leaves).
func leavesBy(body []ast.Stmt, bodyLeaves func([]ast.Stmt) bool) bool {
	if len(body) == 0 {
		return false
	}
	switch s := body[len(body)-1].(type) {
	case *ast.Return, *ast.Break, *ast.Continue, *ast.Unreachable:
		return true
	case *ast.Fallthrough:
		return s.Next != nil && bodyLeaves(s.Next.Body)
	case *ast.If:
		return bodyLeaves(s.Then) && bodyLeaves(s.Else)
	case *ast.Switch:
		return switchLeavesBy(s, bodyLeaves)
	case *ast.ExprStmt:
		return isExitStmt(s)
	default:
		return false
	}
}

// isExitStmt reports whether s is a call to the builtin exit, which never
// returns.
func isExitStmt(s *ast.ExprStmt) bool {
	call, ok := s.X.(*ast.Call)
	return ok && isExit(call)
}

// afterFacts returns the facts an if, or a switch, gives the statements
// after it in its block, and the guard they come from, when it is an early
// exit, one of whose branches leaves; see exitFacts and switchAfterFacts.
// An early exit is a guard whether or not it gives facts.
func (c *Checker) afterFacts(stmt ast.Stmt) (*guard, []fact) {
	switch s := stmt.(type) {
	case *ast.If:
		thenLeaves, elseLeaves := c.leaves(s.Then), c.leaves(s.Else)
		if thenLeaves == elseLeaves {
			return nil, nil
		}
		return ifGuard(s), exitFacts(thenLeaves, elseLeaves, func(holds bool) []fact {
			return c.factsFrom(s.Cond, holds)
		})
	case *ast.Switch:
		return c.switchAfterFacts(s)
	default:
		return nil, nil
	}
}

// fallsThrough returns what of stmt, an if or a switch that gives the
// statements after it facts as an early exit (see afterFacts), runs after
// its condition is known and before those statements: the branch of an if
// that does not leave; and of a switch whose first case leaves, every
// later case's values, tried in turn, and every later body that does not
// leave, or, when the rest of the chain leaves, the first case's body.
// The region the facts hold in starts with it. A branch that leaves never
// reaches the statements after, so what it assigns does not matter there.
func (c *Checker) fallsThrough(stmt ast.Stmt) []ast.Node {
	switch s := stmt.(type) {
	case *ast.If:
		if c.leaves(s.Then) {
			return stmtNodes(s.Else)
		}
		return stmtNodes(s.Then)
	case *ast.Switch:
		order := tryOrder(s)
		if len(order) == 0 {
			return nil
		}
		if !c.leaves(order[0].Body) {
			return stmtNodes(order[0].Body)
		}
		var nodes []ast.Node
		for _, cl := range order[1:] {
			for _, v := range cl.Values {
				nodes = append(nodes, v)
			}
			if !c.leaves(cl.Body) {
				nodes = append(nodes, stmtNodes(cl.Body)...)
			}
		}
		return nodes
	default:
		return nil
	}
}

// ifGuard returns the guard an if is as an early exit: its keyword
// through its condition, which a note underlines.
func ifGuard(s *ast.If) *guard {
	span := s.Span
	span.End = s.Cond.Spanned().End
	return &guard{cond: s.Cond, span: span, kind: guardExit}
}

// exitFacts returns the facts a two-way branch gives the statements after
// it: those of its condition being false when only its then branch leaves,
// thenLeaves, and of it being true when only its else branch does,
// elseLeaves. facts gives the condition's facts when it evaluated to holds.
func exitFacts(thenLeaves, elseLeaves bool, facts func(holds bool) []fact) []fact {
	switch {
	case thenLeaves && !elseLeaves:
		return facts(false)
	case elseLeaves && !thenLeaves:
		return facts(true)
	default:
		return nil
	}
}

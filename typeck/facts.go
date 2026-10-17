package typeck

import (
	"math"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// The checker proves a divisor is not zero, and an index in range, from
// facts: what is known about a variable's value within some region of the
// program, that it is not zero, or bounds on it. A fact comes from a
// condition that has just been tested, or from a range loop, whose index
// runs from 0 to its count, or the length of what it ranges over, less one.
// It holds inside an if's branches, a loop's body, and the right side of &&
// and ||, according to whether the condition was true there, and in the
// rest of a block after an if whose branch leaves it. A switch's cases are
// that if's chain; see switch.go. Deferred code is checked with the facts
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
//     region that holds the use too; and, for a use in deferred code, or
//     in a switch's default written before other cases, one that runs
//     before that code although written after it. A counting loop's start,
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
//     is at most 2 and may still be negative. || when true and && when false give what
//     both sides agree on, the union of their facts, for each variable
//     both sides constrain;
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
// it is not zero, or that it lies at or above lo, or at or below hi, or
// below the length of the string subject lenOf, or at or below the length
// of the string subject atMostLen, or at or below the integer subject
// atMost, or that it is a key in the map subject inMap, which a key whose
// value is known is by known instead, about being zero then (see
// memberFact). The last three are relations (see relational). A subject is a variable or
// a path below an immutable one (see subject.go). For a string subject,
// the fact can be about its length instead, which about then names.
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
	known        string
	ends         []ending
	lo, hi       constant.Int
	hasLo, hasHi bool
	nonzero      bool
}

// holds reports whether f is in force where checking has reached, c.at,
// and so may prove something: it was not dropped, and no write in its
// region that can reach c.at ends it (see dropOf).
func (c *Checker) holds(f fact) bool {
	return c.dropOf(f) == nil
}

// dropOf returns why f does not hold at c.at: the drop it was put in force
// with, or else the first write in its region that reaches c.at, or nil
// when it holds there.
func (c *Checker) dropOf(f fact) *drop {
	if f.drop != nil {
		return f.drop
	}
	for _, e := range f.ends {
		if c.reaches(e) {
			return e.drop
		}
	}
	return nil
}

// empty reports whether f says nothing about its variable: no bound, not
// nonzero, no length bounding it, and no relation.
func (f fact) empty() bool {
	return !f.hasLo && !f.hasHi && !f.nonzero && f.lenOf.isZero() && !f.relational()
}

// relational reports whether f is a relation: one a slice's bounds need,
// its subject at or below a string's or a view's length, or at or below
// another subject, or a key being in a map, which only a map's element
// needs. Such a fact gives no bound of its own, and only those proofs ask
// about it.
func (f fact) relational() bool {
	return !f.atMostLen.isZero() || !f.atMost.isZero() || !f.inMap.isZero()
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
// keep away from zero, or len(s) of a string, list or view subject whose
// length they keep at 1 or more (see minLen).
func (c *Checker) nonzero(e ast.Expr) bool {
	if v, ok := c.knownValue(e); ok {
		return !v.IsZero()
	}
	if _, ok := c.stringLen(e); ok {
		return c.minLen(e.(*ast.Call).Args[0]).Cmp(constant.FromInt64(1)) >= 0
	}
	if !c.isSubject(e) {
		return false
	}
	return c.intervalOf(e).excludesZero()
}

// inRange reports whether a subject used as an index, a variable or a path
// below an immutable one, is known to lie in [0, n) where it is used, by
// its type and the facts in force.
func (c *Checker) inRange(index ast.Expr, n int64) bool {
	if !c.isSubject(index) || n == 0 {
		return false
	}
	known := c.intervalOf(index)
	return known.hasLo && !known.lo.IsNegative() &&
		known.hasHi && known.hi.Cmp(constant.FromInt64(n-1)) <= 0
}

// intervalOf returns what is known about the value of e, a subject: its
// type's bounds, narrowed by every fact in force about it.
func (c *Checker) intervalOf(e ast.Expr) interval {
	known := typeInterval(e)
	k, ok := c.subjectKey(e)
	if !ok {
		return known
	}
	for _, f := range c.active {
		if f.about == k && c.holds(f) {
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
// as in len(a) - 1 or k << 2.
func (c *Checker) knownValue(e ast.Expr) (constant.Int, bool) {
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
// && that does not hold or an || that does.
func (c *Checker) binaryFacts(x *ast.Binary, holds bool) []fact {
	switch {
	case x.Op == ast.And && holds, x.Op == ast.Or && !holds:
		left := c.dropAssigned(c.factsFrom(x.X, holds), []ast.Node{x.Y})
		return append(left, c.factsFrom(x.Y, holds)...)
	case x.Op == ast.And, x.Op == ast.Or:
		return from(union(c.factsFrom(x.X, holds), c.factsFrom(x.Y, holds)), x)
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

// joinFacts records the facts of bin, a checked && or || binary, whose
// left side gives left, as chainFacts holds them: left, each dropped
// where bin's right side assigns its variable, then the right side's
// facts. factsFrom gives the same for bin, by walking its left side
// again; a chain of n operators would walk n of them at each one.
func (c *Checker) joinFacts(bin *ast.Binary, left []fact) {
	holds := bin.Op == ast.And
	joined := c.dropAssigned(slices.Clone(left), []ast.Node{bin.Y})
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
		if seen[f.about] || f.relational() || f.drop != nil {
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

// sideInterval returns what the facts in force of one side say about the
// subject k, and whether any of them is about it.
func sideInterval(facts []fact, k factKey) (interval, bool) {
	var known interval
	found := false
	for _, f := range facts {
		if f.about == k && !f.relational() && f.drop == nil {
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
// variable's length. Either side may be the one the fact is about.
func (c *Checker) comparisonFact(cmp *ast.Binary, holds bool) []fact {
	op := cmp.Op
	if !holds {
		op = negate(op)
	}
	if f, ok := c.orientedFact(cmp.X, op, cmp.Y); ok {
		return from([]fact{f}, cmp)
	}
	if f, ok := c.orientedFact(cmp.Y, flip(op), cmp.X); ok {
		return from([]fact{f}, cmp)
	}
	return nil
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
// for left <= right or left < right.
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
		return fact{about: about, atMost: other}, true
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
// it by (see endingsIn): the fact holds for code no such write can reach
// (see reaches), and is dropped, marked with the first write that does,
// everywhere else, so it proves nothing there but can explain an error. A
// fact that says nothing, such as the one b != 3 gives, is left out
// before the region is searched, which a long chain of cases or ifs
// would otherwise do once per link. g itself is put in scope too.
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
func (c *Checker) assume(g *guard, facts []fact, region []ast.Node) {
	c.guards = append(c.guards, g)
	for _, f := range facts {
		if f.empty() {
			continue
		}
		f.guard = g
		if f.drop == nil {
			f.ends = c.endingsIn(region, f)
		}
		c.active = append(c.active, f)
	}
}

// mark records how many facts and guards are in force, for release.
type mark struct {
	facts, guards int
}

// mark returns how many facts and guards are in force now.
func (c *Checker) mark() mark {
	return mark{len(c.active), len(c.guards)}
}

// dropAssigned returns facts with every one that region assigns the
// variable of, as assignedIn says, marked dropped by that assignment. A
// fact already dropped keeps its drop.
func (c *Checker) dropAssigned(facts []fact, region []ast.Node) []fact {
	for i := range facts {
		if facts[i].drop == nil && !facts[i].empty() {
			facts[i].drop = c.assignedIn(region, facts[i])
		}
	}
	return facts
}

// stmtNodes returns the statements of body as a region assume and
// assignedIn search.
func stmtNodes(body []ast.Stmt) []ast.Node {
	nodes := make([]ast.Node, len(body))
	for i, stmt := range body {
		nodes[i] = stmt
	}
	return nodes
}

// release takes out of force every fact and guard assumed since m was
// taken.
func (c *Checker) release(m mark) {
	c.active = c.active[:m.facts]
	c.guards = c.guards[:m.guards]
}

// assignedIn returns why region drops f: the first assignment in it, in
// source order, to f's variable, or else to the string whose length bounds
// it, or the subject a relation bounds it by, or the map it is a key of, a
// call passing any of them as mut included. It returns nil when region
// assigns none of them.
func (c *Checker) assignedIn(region []ast.Node, f fact) *drop {
	if !f.about.isZero() {
		if d := c.assignedDrop(region, f.about); d != nil {
			return d
		}
	}
	for _, other := range []factKey{f.lenOf, f.atMostLen, f.atMost, f.inMap} {
		if other.isZero() {
			continue
		}
		if d := c.assignedDrop(region, other); d != nil {
			return d
		}
	}
	return nil
}

// ending is a write in a fact's region that ends the fact for the code it
// can reach: drop says what it is, and at where it takes effect, the end
// of the call it passes the variable as mut to, or of the assignment
// statement whose target it is. Either runs after everything written
// before that end: a call's arguments, and the callee's own check of
// them, come before it assigns its mut arguments, and an assignment's
// value, and every check in it, before its target is stored to. loop is
// the outermost loop in the region that holds the write, through whose
// back edge it can reach code before it, or the zero span.
type ending struct {
	drop *drop
	loop ast.Span
	at   int
}

// endingsIn returns every write in region that may end f, in the order
// region holds them: each assignment to f's variable, or to the string
// whose length bounds it, or the subject a relation bounds it by, or the
// map it is a key of, a call passing any of them as mut included.
func (c *Checker) endingsIn(region []ast.Node, f fact) []ending {
	var ends []ending
	for _, k := range []factKey{f.about, f.lenOf, f.atMostLen, f.atMost, f.inMap} {
		if !k.isZero() {
			ends = append(ends, c.writesIn(region, k.decl)...)
		}
	}
	return ends
}

// writesIn returns every write to decl in region, however deeply nested,
// as an ending (see eachWrite), each knowing the outermost loop in region
// that holds it.
func (c *Checker) writesIn(region []ast.Node, decl ast.Decl) []ending {
	var ends []ending
	c.eachWrite(region, decl, func(w write) {
		d := &drop{kind: dropAssigned, at: w.shown.Spanned(), name: declName(decl)}
		if w.call != nil {
			d.kind, d.text = dropMut, c.factCallee(w.call)
		}
		var loop ast.Span
		if len(w.loops) > 0 {
			loop = w.loops[0]
		}
		ends = append(ends, ending{drop: d, loop: loop, at: w.at})
	})
	return ends
}

// write is a write to a variable that eachWrite found: shown, what a note
// about it underlines, the target or the mut argument; call, the call it
// is a mut argument of, or nil; at, where it takes effect, as an
// ending's at; and loops, every loop the walk passed that holds it,
// outermost first.
type write struct {
	shown ast.Node
	call  *ast.Call
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

// isLoop reports whether n is a loop, of either kind.
func isLoop(n ast.Node) bool {
	switch n.(type) {
	case *ast.For, *ast.ForRange:
		return true
	default:
		return false
	}
}

// reaches reports whether e, a write in the region of a fact, can run
// after the fact became true and before the check at c.at, the end of
// the expression or statement being checked, without the fact being
// proved again: it takes effect before c.at; or it is in a loop inside
// the region that c.at is in too, whose next pass reaches c.at; or c.at
// is in deferred code, which runs when its block is left, and e is
// outside it; or c.at is in a switch's default written before cases
// whose values are tried before it runs, and e is in that switch outside
// the default (see lateDefault); or c.at is in the targets of an
// assignment and e is anywhere in that assignment, its value or its
// targets (see storing).
func (c *Checker) reaches(e ending) bool {
	switch {
	case e.at < c.at:
		return true
	case e.loop.Start < c.at && c.at <= e.loop.End:
		return true
	case c.deferSpan != nil && !inSpan(e.at, *c.deferSpan):
		return true
	case c.storing != nil && inSpan(e.at, *c.storing):
		return true
	}
	return c.beforeLateDefault(e)
}

// beforeLateDefault reports whether e is in a switch whose default,
// written before some of its cases, is being checked, and outside that
// default: every case's values are tried before the default runs.
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

// assignedDrop returns the drop for the first assignment in region to the
// variable the subject k starts from, which names the call when it is a
// mut argument, or nil when there is none. A path's variable is immutable,
// so nothing assigns it.
func (c *Checker) assignedDrop(region []ast.Node, k factKey) *drop {
	target, call := c.assignment(region, k.decl)
	switch {
	case target == nil:
		return nil
	case call != nil:
		return &drop{
			kind: dropMut,
			at:   target.Spanned(),
			name: declName(k.decl),
			text: c.factCallee(call),
		}
	default:
		return &drop{kind: dropAssigned, at: target.Spanned(), name: declName(k.decl)}
	}
}

// factCallee returns the name call calls, as calleeName gives it, or "a
// function" for a call it cannot name.
func (c *Checker) factCallee(call *ast.Call) string {
	if name, ok := c.calleeName(call); ok {
		return name
	}
	return "a function"
}

// assignment returns the first target, in source order, through which any
// statement or expression in region, however deeply nested, assigns to
// decl: an assignment's target, a compound assignment's, an increment's
// or decrement's, one of an assignment of several values, or a mut
// argument, which a call may assign through, with the call. It returns
// nil when there is none.
func (c *Checker) assignment(region []ast.Node, decl ast.Decl) (ast.Expr, *ast.Call) {
	var found ast.Expr
	var by *ast.Call
	visit := func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Assign:
			found = c.writer(found, x.Target, decl)
		case *ast.IncDec:
			found = c.writer(found, x.Target, decl)
		case *ast.MultiAssign:
			for _, target := range x.Targets {
				found = c.writer(found, target, decl)
			}
		case *ast.Call:
			if m := c.mutWriter(x, decl); found == nil && m != nil {
				found, by = m, x
			}
		}
		return found == nil
	}
	for _, n := range region {
		if ast.Walk(n, visit); found != nil {
			return found, by
		}
	}
	return nil, nil
}

// mutWriter returns the first of call's arguments, its receiver first,
// that passes decl, or a part of it, as mut, or nil when none does.
func (c *Checker) mutWriter(call *ast.Call, decl ast.Decl) *ast.MutArg {
	for _, arg := range call.Operands() {
		if m, ok := arg.(*ast.MutArg); ok && c.writes(m.X, decl) {
			return m
		}
	}
	return nil
}

// writer returns found, the target already found to assign decl, if any,
// and otherwise target when assigning to it changes decl.
func (c *Checker) writer(found, target ast.Expr, decl ast.Decl) ast.Expr {
	if found == nil && c.writes(target, decl) {
		return target
	}
	return found
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
// its end: its last statement is a return, a break or a continue, an if
// whose branches both leave, or a switch with a default, or on a union
// with every case named, whose cases all leave. Like the terminating rule, it is syntactic, so a call to exit does
// not count.
func leaves(body []ast.Stmt) bool {
	return leavesBy(body, false)
}

// leavesBy is leaves, except that a call to exit counts as leaving when
// exits is set, which only explaining a missing proof asks for.
func leavesBy(body []ast.Stmt, exits bool) bool {
	if len(body) == 0 {
		return false
	}
	switch s := body[len(body)-1].(type) {
	case *ast.Return, *ast.Break, *ast.Continue:
		return true
	case *ast.If:
		return leavesBy(s.Then, exits) && leavesBy(s.Else, exits)
	case *ast.Switch:
		return switchLeavesBy(s, exits)
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.Call)
		return exits && ok && isExit(call)
	default:
		return false
	}
}

// afterFacts returns the facts an if, or a switch, gives the statements
// after it in its block, and the guard they come from, when it is an early
// exit, one of whose branches leaves; see exitFacts and switchAfterFacts.
// An early exit is a guard whether or not it gives facts.
func (c *Checker) afterFacts(stmt ast.Stmt) (*guard, []fact) {
	switch s := stmt.(type) {
	case *ast.If:
		thenLeaves, elseLeaves := leaves(s.Then), leaves(s.Else)
		if thenLeaves == elseLeaves {
			return nil, nil
		}
		return ifGuard(s), exitFacts(thenLeaves, elseLeaves, func(holds bool) []fact {
			return c.factsFrom(s.Cond, holds)
		})
	case *ast.Switch:
		return c.switchAfterFactsBy(s, false)
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
func fallsThrough(stmt ast.Stmt) []ast.Node {
	switch s := stmt.(type) {
	case *ast.If:
		if leaves(s.Then) {
			return stmtNodes(s.Else)
		}
		return stmtNodes(s.Then)
	case *ast.Switch:
		order := tryOrder(s)
		if len(order) == 0 {
			return nil
		}
		if !leaves(order[0].Body) {
			return stmtNodes(order[0].Body)
		}
		var nodes []ast.Node
		for _, cl := range order[1:] {
			for _, v := range cl.Values {
				nodes = append(nodes, v)
			}
			if !leaves(cl.Body) {
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

package typeck

import (
	"math"

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
//   - a fact is about a parameter, a local variable, or an immutable global;
//     a mutable global can be changed by any call, so it never has one;
//   - a fact holds in a region only if the region never assigns the
//     variable, anywhere, so nothing in it can make the fact false;
//   - a condition gives facts through && (when true), || (when false), !,
//     and comparisons of a variable with a known value: b != 0, i >= 0,
//     i < len(a), i == 3; a variable's type bounds it too, so an unsigned
//     one is never negative. || when true and && when false give what
//     both sides agree on, the union of their facts, for each variable
//     both sides constrain;
//   - the length of a string parameter or mutable variable has no known
//     value (an immutable one holding a known string has; see
//     knownString), so two more comparisons give facts: len(s) with a known value bounds the length,
//     as len(s) > 0 does, and i < len(s) bounds i by it, as a range loop
//     over len(s), or over s itself, does its index. A fact that names s
//     holds only in a region that assigns neither variable; s may be
//     mutable, since the region then never changes it.

// fact is something known about a variable's value in a region: that it is
// not zero, or that it lies at or above lo, or at or below hi, or below the
// length of the string variable lenOf. For a string variable, the fact is
// about its length instead.
//
// A fact also records where it came from, for the notes that explain a
// missing proof: src, the comparison, or the && or || whose sides it
// unites, that gave it, when there is one, and guard, the condition, loop,
// case or early exit whose region it was put in force for. drop is set on a
// fact that does not hold where it is: one the region drops because it
// assigns the variable, and one that would hold but for how its region is
// written (see explain.go). Such a fact is kept in force only to explain an
// error, and every query that proves something skips it: see inForce.
type fact struct {
	decl         ast.Decl
	lenOf        ast.Decl
	src          ast.Expr
	guard        *guard
	drop         *drop
	lo, hi       constant.Int
	hasLo, hasHi bool
	nonzero      bool
}

// inForce reports whether f is in force where it is, and so may prove
// something: it was not dropped.
func (f fact) inForce() bool {
	return f.drop == nil
}

// empty reports whether f says nothing about its variable: no bound, not
// nonzero, and no length bounding it.
func (f fact) empty() bool {
	return !f.hasLo && !f.hasHi && !f.nonzero && f.lenOf == nil
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
// constant other than zero, an immutable variable whose value is one, or a
// variable the facts in force keep away from zero.
func (c *Checker) nonzero(e ast.Expr) bool {
	if v, ok := c.knownValue(e); ok {
		return !v.IsZero()
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	return c.intervalOf(id).excludesZero()
}

// inRange reports whether a variable used as an index is known to lie in
// [0, n) where it is used, by its type and the facts in force.
func (c *Checker) inRange(index ast.Expr, n int64) bool {
	id, ok := index.(*ast.Ident)
	if !ok || n == 0 {
		return false
	}
	known := c.intervalOf(id)
	return known.hasLo && !known.lo.IsNegative() &&
		known.hasHi && known.hi.Cmp(constant.FromInt64(n-1)) <= 0
}

// intervalOf returns what is known about the value of the variable id names:
// its type's bounds, narrowed by every fact in force about it.
func (c *Checker) intervalOf(id *ast.Ident) interval {
	known := typeInterval(id)
	for _, f := range c.active {
		if f.decl == id.Def && f.inForce() {
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
// type.
func typeBounds(typ types.Type) (lo, hi constant.Int) {
	n := types.Bits(typ)
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
// its type, or arithmetic on known values whose exact result is a value of
// its type, as in len(a) - 1.
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
		v, ok := c.knownValue(x.X)
		if !ok || x.Op != ast.Neg {
			return constant.Int{}, false
		}
		return fitting(v.Neg())(x.Type)
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return constant.Int{}, false
	}
	return c.knownVar(id)
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
// x that is a value of the integer type T, which is that value. One that
// is not has no value, and was reported when the conversion was checked.
func (c *Checker) knownConversion(call *ast.Call) (constant.Int, bool) {
	if len(call.Args) != 1 || !types.IsInteger(call.Conv) {
		return constant.Int{}, false
	}
	v, ok := c.knownValue(call.Args[0])
	if !ok || !v.Fits(types.Bits(call.Conv), !types.IsUnsigned(call.Conv)) {
		return constant.Int{}, false
	}
	return v, true
}

// knownStringLen returns the length of the string call takes the len of,
// when call is len(s) and s is a known string. len of a literal is already
// a constant, recorded on the call; this adds an immutable variable
// holding one.
func (c *Checker) knownStringLen(call *ast.Call) (constant.Int, bool) {
	if !isLen(call) || len(call.Args) != 1 {
		return constant.Int{}, false
	}
	if n, ok := c.knownLen(call.Args[0]); ok {
		return constant.FromInt64(n), true
	}
	return constant.Int{}, false
}

// knownString returns the value of e when it is a string the checker
// knows: a literal, an immutable variable, local or package-level,
// initialized with a known string, or a field or an element of a known
// struct or array (see foldPart). Such a variable always holds that
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

// knownArithmetic computes arithmetic on two known values, exactly, and
// keeps the result only if it is a value of the expression's type, so that
// it is what the program computes too.
func (c *Checker) knownArithmetic(bin *ast.Binary) (constant.Int, bool) {
	if !bin.Op.IsArithmetic() {
		return constant.Int{}, false
	}
	a, okA := c.knownValue(bin.X)
	b, okB := c.knownValue(bin.Y)
	if !okA || !okB {
		return constant.Int{}, false
	}
	return knownStep(bin.Op, a, b, bin.Type)
}

// knownStep computes a op b, exactly, for the arithmetic operator op, and
// keeps the result only if it is a value of the type typ the operation
// works in. A division or remainder by zero has no value.
func knownStep(op ast.BinaryOp, a, b constant.Int, typ types.Type) (constant.Int, bool) {
	switch op {
	case ast.Add:
		return fitting(a.Add(b))(typ)
	case ast.Sub:
		return fitting(a.Sub(b))(typ)
	case ast.Mul:
		return fitting(a.Mul(b))(typ)
	}
	if b.IsZero() {
		return constant.Int{}, false
	}
	if op == ast.Div {
		return fitting(a.Quo(b))(typ)
	}
	return fitting(a.Rem(b))(typ)
}

// fitting turns the result of a constant operation into a known value of
// the type typ: it is known only if it was in range and is a value of typ.
func fitting(v constant.Int, ok bool) func(typ types.Type) (constant.Int, bool) {
	return func(typ types.Type) (constant.Int, bool) {
		if !ok || !types.IsInteger(typ) || typ == types.UntypedInt {
			return v, ok && typ == types.UntypedInt
		}
		return v, v.Fits(types.Bits(typ), !types.IsUnsigned(typ))
	}
}

// factsFrom returns the facts cond gives where it evaluated to holds. &&
// gives both sides' facts when true, and || both sides' facts of being
// false when false. When one side or the other holds, && false or || true,
// only what both sides agree on holds: see union. ! swaps; a comparison
// of a variable with a known value gives one fact.
func (c *Checker) factsFrom(cond ast.Expr, holds bool) []fact {
	switch x := cond.(type) {
	case *ast.Unary:
		if x.Op == ast.Not {
			return c.factsFrom(x.X, !holds)
		}
	case *ast.Binary:
		switch {
		case x.Op == ast.And && holds, x.Op == ast.Or && !holds:
			return append(c.factsFrom(x.X, holds), c.factsFrom(x.Y, holds)...)
		case x.Op == ast.And, x.Op == ast.Or:
			return from(union(c.factsFrom(x.X, holds), c.factsFrom(x.Y, holds)), x)
		case x.Op.IsComparison():
			return c.comparisonFact(x, holds)
		}
	}
	return nil
}

// from records src as where each of facts came from, and returns them.
func from(facts []fact, src ast.Expr) []fact {
	for i := range facts {
		facts[i].src = src
	}
	return facts
}

// union returns the facts that hold when the facts a hold or the facts b
// do, not knowing which: for each variable both say something about, the
// smallest interval holding what each side allows, and not zero when
// neither side allows zero. So b < 0 || b > 0 gives b != 0, and
// i == 0 || i == 1 gives 0 <= i <= 1. A variable only one side constrains
// gets nothing, since the other side may be the one that holds. A bound by
// a string's length, i < len(s), is not carried over.
func union(a, b []fact) []fact {
	var out []fact
	seen := make(map[ast.Decl]bool)
	for _, f := range a {
		if seen[f.decl] {
			continue
		}
		seen[f.decl] = true
		other, ok := sideInterval(b, f.decl)
		if !ok {
			continue
		}
		mine, _ := sideInterval(a, f.decl)
		out = append(out, mine.hull(other).fact(f.decl))
	}
	return out
}

// sideInterval returns what the facts of one side say about decl, and
// whether any of them is about it.
func sideInterval(facts []fact, decl ast.Decl) (interval, bool) {
	var known interval
	found := false
	for _, f := range facts {
		if f.decl == decl {
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

// fact returns the fact that decl lies in known.
func (known interval) fact(decl ast.Decl) fact {
	return fact{
		decl:    decl,
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
// variable, or len of a string variable, compared with a known value, or a
// variable less than len of a string variable.
func (c *Checker) orientedFact(left ast.Expr, op ast.BinaryOp, right ast.Expr) (fact, bool) {
	decl, ok := c.subject(left)
	if !ok {
		return fact{}, false
	}
	if k, known := c.knownValue(right); known {
		return bound(decl, op, k), true
	}
	_, isName := left.(*ast.Ident)
	if s, ok := c.stringLen(right); ok && isName && op == ast.Lt {
		return fact{decl: decl, lenOf: s}, true
	}
	return fact{}, false
}

// subject returns the declaration a fact about e would be about: the
// variable e names, or the string variable whose length e is, len(s), when
// that variable can have facts.
func (c *Checker) subject(e ast.Expr) (ast.Decl, bool) {
	if id, ok := e.(*ast.Ident); ok && c.canHoldFact(id.Def) {
		return id.Def, true
	}
	return c.stringLen(e)
}

// stringLen returns the string variable s when e is len(s) and s can have
// facts.
func (c *Checker) stringLen(e ast.Expr) (ast.Decl, bool) {
	call, ok := e.(*ast.Call)
	if !ok || !isLen(call) || len(call.Args) != 1 {
		return nil, false
	}
	return c.stringVar(call.Args[0])
}

// stringVar returns the string variable e names, when it can have facts.
func (c *Checker) stringVar(e ast.Expr) (ast.Decl, bool) {
	id, ok := e.(*ast.Ident)
	if !ok || ast.TypeOf(id) != types.String || !c.canHoldFact(id.Def) {
		return nil, false
	}
	return id.Def, true
}

// bound returns the fact that the variable decl compares with k as op says.
func bound(decl ast.Decl, op ast.BinaryOp, k constant.Int) fact {
	f := fact{decl: decl}
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
// from g. A fact about a variable the region assigns, or about the string
// whose length bounds one, is put in force dropped, marked with the
// assignment, so it proves nothing but can explain an error. A fact that
// says nothing, such as the one b != 3 gives, is left out before the region
// is searched, which a long chain of cases or ifs would otherwise do once
// per link. g itself is put in scope too. Everything assume puts in force
// stays until a release to a mark taken before it.
func (c *Checker) assume(g *guard, facts []fact, region []ast.Stmt) {
	c.guards = append(c.guards, g)
	for _, f := range facts {
		if f.empty() {
			continue
		}
		f.guard = g
		if f.drop == nil {
			f.drop = assignedIn(region, f)
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

// release takes out of force every fact and guard assumed since m was
// taken.
func (c *Checker) release(m mark) {
	c.active = c.active[:m.facts]
	c.guards = c.guards[:m.guards]
}

// assignedIn returns why region drops f: the first assignment in it, in
// source order, to f's variable, or else to the string whose length bounds
// it. It returns nil when region assigns neither.
func assignedIn(region []ast.Stmt, f fact) *drop {
	if target := assignment(region, f.decl); target != nil {
		return &drop{kind: dropAssigned, at: target.Spanned(), name: declName(f.decl)}
	}
	if f.lenOf == nil {
		return nil
	}
	if target := assignment(region, f.lenOf); target != nil {
		return &drop{kind: dropAssigned, at: target.Spanned(), name: declName(f.lenOf)}
	}
	return nil
}

// assignment returns the first target, in source order, through which any
// statement in region, however deeply nested, assigns to decl: an
// assignment's target, a compound assignment's, an increment's or
// decrement's, or one of an assignment of several values. It returns nil
// when there is none.
func assignment(region []ast.Stmt, decl ast.Decl) ast.Expr {
	var found ast.Expr
	visit := func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Assign:
			found = writer(found, x.Target, decl)
		case *ast.IncDec:
			found = writer(found, x.Target, decl)
		case *ast.MultiAssign:
			for _, target := range x.Targets {
				found = writer(found, target, decl)
			}
		}
		return found == nil
	}
	for _, stmt := range region {
		if ast.Walk(stmt, visit); found != nil {
			return found
		}
	}
	return nil
}

// writer returns found, the target already found to assign decl, if any,
// and otherwise target when assigning to it changes decl.
func writer(found, target ast.Expr, decl ast.Decl) ast.Expr {
	if found == nil && writes(target, decl) {
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
// itself, or an element of it.
func writes(target ast.Expr, decl ast.Decl) bool {
	root := ast.Root(target)
	return root != nil && root.Def == decl
}

// leaves reports whether a block always leaves the code around it before
// its end: its last statement is a return, a break or a continue, an if
// whose branches both leave, or a switch with a default whose cases all
// leave. Like the terminating rule, it is syntactic, so a call to exit does
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
		return chainLeavesBy(tryOrder(s), exits)
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

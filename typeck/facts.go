package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// The checker proves a divisor is not zero from facts: that a variable is
// known not to be zero within some region of the program. A fact comes from
// a condition that has just been tested. It holds inside an if's branches,
// a loop's body, and the right side of && and ||, according to whether the
// condition was true there, and in the rest of a block after an if whose
// branch leaves it. The rules are syntactic, so any reader can follow them:
//
//   - a fact is about a parameter, a local variable, or an immutable global;
//     a mutable global can be changed by any call, so it never has one;
//   - a fact holds in a region only if the region never assigns the
//     variable, anywhere, so nothing in it can make the fact false;
//   - a condition gives facts through && (when true), || (when false), !,
//     and comparisons of a variable with a constant that exclude zero, such
//     as b != 0, b > 0, or b == 3.

// nonzero reports whether e is known not to be zero where it is checked: a
// constant other than zero, an immutable variable whose value is one, or a
// variable a fact covers.
func (c *Checker) nonzero(e ast.Expr) bool {
	if v, ok := c.knownValue(e); ok {
		return !v.IsZero()
	}
	id, ok := e.(*ast.Ident)
	return ok && c.facts[id.Def] > 0
}

// knownValue returns the value of e when the checker knows it: a constant
// expression, len of an array or a string literal, an immutable variable
// initialized with one, at package level or in a function, or arithmetic
// on known values whose exact result is a value of its type, as in
// len(a) - 1.
func (c *Checker) knownValue(e ast.Expr) (constant.Int, bool) {
	if v, ok := constValue(e); ok {
		return v, true
	}
	switch x := e.(type) {
	case *ast.Binary:
		return c.knownArithmetic(x)
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
// immutable and its value is known.
func (c *Checker) knownVar(id *ast.Ident) (constant.Int, bool) {
	v, ok := id.Def.(*ast.Var)
	if !ok || v.Mutable {
		return constant.Int{}, false
	}
	if k, ok := v.Const.(constant.Int); ok {
		return k, true
	}
	if v.Value == nil {
		return constant.Int{}, false
	}
	return c.knownValue(v.Value)
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
	switch bin.Op {
	case ast.Add:
		return fitting(a.Add(b))(bin.Type)
	case ast.Sub:
		return fitting(a.Sub(b))(bin.Type)
	case ast.Mul:
		return fitting(a.Mul(b))(bin.Type)
	}
	if b.IsZero() {
		return constant.Int{}, false
	}
	if bin.Op == ast.Div {
		return fitting(a.Quo(b))(bin.Type)
	}
	return fitting(a.Rem(b))(bin.Type)
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

// factsFrom returns the variables cond proves not zero where it evaluated
// to holds. && gives both sides' facts when true and none when false; || the
// reverse; ! swaps; a comparison of a variable with a known value gives a
// fact when the comparison, or its negation, excludes zero.
func (c *Checker) factsFrom(cond ast.Expr, holds bool) []ast.Decl {
	switch x := cond.(type) {
	case *ast.Unary:
		if x.Op == ast.Not {
			return c.factsFrom(x.X, !holds)
		}
	case *ast.Binary:
		switch {
		case x.Op == ast.And && holds, x.Op == ast.Or && !holds:
			return append(c.factsFrom(x.X, holds), c.factsFrom(x.Y, holds)...)
		case x.Op.IsComparison():
			return c.comparisonFact(x, holds)
		}
	}
	return nil
}

// comparisonFact returns the variable a comparison of it with a known value
// proves not zero, when the comparison's value is holds, if it proves that.
func (c *Checker) comparisonFact(cmp *ast.Binary, holds bool) []ast.Decl {
	op := cmp.Op
	variable, value := cmp.X, cmp.Y
	if _, isConst := c.knownValue(cmp.X); isConst {
		variable, value, op = cmp.Y, cmp.X, flip(op)
	}

	id, ok := variable.(*ast.Ident)
	if !ok || !c.canHoldFact(id.Def) {
		return nil
	}
	k, ok := c.knownValue(value)
	if !ok {
		return nil
	}
	if !holds {
		op = negate(op)
	}
	if excludesZero(op, k) {
		return []ast.Decl{id.Def}
	}
	return nil
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

// excludesZero reports whether v op k can only hold for a v other than 0.
func excludesZero(op ast.BinaryOp, k constant.Int) bool {
	sign := k.Cmp(constant.Int{})
	switch op {
	case ast.Eq:
		return sign != 0
	case ast.Ne:
		return sign == 0
	case ast.Lt:
		return sign <= 0
	case ast.Le:
		return sign < 0
	case ast.Gt:
		return sign >= 0
	default: // ast.Ge
		return sign > 0
	}
}

// assume records facts for the region about to be checked, keeping only
// those about variables the region never assigns. It returns what it
// recorded, for forget to remove when the region ends.
func (c *Checker) assume(facts []ast.Decl, region []ast.Stmt) []ast.Decl {
	var kept []ast.Decl
	for _, decl := range facts {
		if !assigns(region, decl) {
			c.facts[decl]++
			kept = append(kept, decl)
		}
	}
	return kept
}

// forget removes facts assume recorded.
func (c *Checker) forget(facts []ast.Decl) {
	for _, decl := range facts {
		c.facts[decl]--
	}
}

// assigns reports whether any statement in region, however deeply nested,
// assigns to decl: as an assignment's target, a compound assignment's, an
// increment's or decrement's, or one of an assignment of several values.
func assigns(region []ast.Stmt, decl ast.Decl) bool {
	found := false
	for _, stmt := range region {
		ast.Walk(stmt, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Assign:
				found = found || writes(x.Target, decl)
			case *ast.IncDec:
				found = found || writes(x.Target, decl)
			case *ast.MultiAssign:
				for _, target := range x.Targets {
					found = found || writes(target, decl)
				}
			}
			return !found
		})
	}
	return found
}

// writes reports whether assigning to target changes decl: target is decl
// itself, or an element of it.
func writes(target ast.Expr, decl ast.Decl) bool {
	root := ast.Root(target)
	return root != nil && root.Def == decl
}

// leaves reports whether a block always leaves the code around it before
// its end: its last statement is a return, a break or a continue, or an if
// whose branches both leave. Like the terminating rule, it is syntactic, so
// a call to exit does not count.
func leaves(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	switch s := body[len(body)-1].(type) {
	case *ast.Return, *ast.Break, *ast.Continue:
		return true
	case *ast.If:
		return leaves(s.Then) && leaves(s.Else)
	default:
		return false
	}
}

// afterFacts returns the facts an if gives the statements after it in its
// block: those of its condition being false when its then branch leaves,
// and of it being true when its else branch does.
func (c *Checker) afterFacts(stmt ast.Stmt) []ast.Decl {
	s, ok := stmt.(*ast.If)
	if !ok {
		return nil
	}
	switch {
	case leaves(s.Then) && !leaves(s.Else):
		return c.factsFrom(s.Cond, false)
	case leaves(s.Else) && !leaves(s.Then):
		return c.factsFrom(s.Cond, true)
	default:
		return nil
	}
}

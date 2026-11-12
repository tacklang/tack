package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// The postfix ? operator, f(...)?, passes the error of the call before it
// on. It applies to a call whose last result is an error: of a function,
// a method, a function value, or a builtin that returns one, append,
// reserve and put. Anything else before it, a variable, a stored error
// among them, a literal, a conversion, a union built or another ?, is
// reported (questionCall), as is a call whose last result is not an
// error. It is written only in a function whose last result is an error:
// one in main, in another function, in deferred code, which cannot
// return, or in a package-level variable's value is reported
// (questionPlace).
//
// When the call's error is set, its code not 0, the function holding the
// ? returns it, as a return there would, so every fact its contract
// ensures must hold at the ?, as at a return (proveContract). Otherwise
// the ? gives the call's other results, recorded in Question.Results: one
// is a value, Question.Type, which the ? may be used as wherever a value
// is; none make it a statement only; and several are bound together, a,
// b := f()? or a, b = f()?. The error is never one of them, so nothing
// the ? gives is held by an error, and no check is needed before reading
// it. A ? written as a statement drops what it gives, under the rules of
// a call statement: a value of an owned type cannot be discarded (see
// move.go).

// question checks q, a ? whose one value is used, and returns that
// value's type. A ? that gives none, or several, is reported.
func (c *Checker) question(q *ast.Question) types.Type {
	results, ok := c.checkQuestion(q)
	switch {
	case !ok:
		return types.Invalid
	case len(results) == 0:
		c.errorf(q.Span, "%s gives no value", c.text(q.Span))
		return types.Invalid
	case len(results) > 1:
		c.errorf(q.Span, "%s gives %d values", c.text(q.Span), len(results))
		return types.Invalid
	}
	return results[0]
}

// questionResults checks q, the value of a binding of want names, and
// returns the type of each value it gives, in order, and whether it gives
// exactly want of them. A wrong count is one error covering span, the
// whole statement.
func (c *Checker) questionResults(q *ast.Question, want int, span ast.Span) ([]types.Type, bool) {
	results, ok := c.checkQuestion(q)
	if !ok {
		return nil, false
	}
	if got := len(results); got != want {
		gives := "no value"
		if got > 0 {
			gives = fmt.Sprintf("%d %s", got, plural(got, "value"))
		}
		c.errorf(span, "assignment mismatch: %d %s but %s gives %s",
			want, plural(want, "variable"), c.text(q.Span), gives)
		return nil, false
	}
	return results, true
}

// checkQuestion checks q and the call it follows, and returns the call's
// results before its error, which it records on q, and whether q is one
// to give them: it follows a call whose last result is an error, and it
// is written where it can return. A problem with either is reported, the
// call's own first, and gives false.
func (c *Checker) checkQuestion(q *ast.Question) ([]types.Type, bool) {
	results, ok := c.questionCall(q)
	if !c.questionPlace(q) || !ok {
		return nil, false
	}
	q.Results = results
	if len(results) == 1 {
		q.Type = results[0]
	}
	c.proveQuestionContract(q)
	return results, true
}

// questionCall checks what q follows, which must be a call whose last
// result is an error, and returns the call's results before the error.
// It reports anything else, and returns false then, and when the call's
// own problems were reported.
func (c *Checker) questionCall(q *ast.Question) ([]types.Type, bool) {
	call, isCall := q.X.(*ast.Call)
	if !isCall || notCalling(call) {
		before := len(c.diags)
		c.discard(q.X)
		if len(c.diags) == before {
			c.errorf(q.Mark(), "cannot use ? on %s, which is not a call", c.text(q.X.Spanned()))
		}
		return nil, false
	}
	// The call is checked where it ends, as any operand is (see expr),
	// so what it writes through a mut argument is judged after it.
	outer := c.at
	c.at = call.Spanned().End
	results, name, ok := c.questionedResults(call)
	c.at = outer
	if !ok {
		return nil, false
	}
	switch {
	case len(results) == 0:
		c.errorf(q.Mark(), "cannot use ? on a call to %s, which returns no error", name)
		return nil, false
	case results[len(results)-1] != types.Error:
		c.errorf(q.Mark(), "cannot use ? on a call to %s, whose last result is %s, not error",
			name, c.typeName(results[len(results)-1]))
		return nil, false
	}
	return results[:len(results)-1], true
}

// notCalling reports whether call, as written, calls no function: it
// builds a union, Expr.int(5), or converts a value, int8(x) or
// []uint8(s).
func notCalling(call *ast.Call) bool {
	if _, _, _, built := ast.Construction(call); built {
		return true
	}
	return call.Conv != types.Invalid || isViewConversion(call)
}

// questionedResults checks call, the call a ? follows, and returns every
// result it gives, the error included, and its callee's name, as
// calleeName gives it: a function's or a method's results, a function
// value's type's, or a builtin's. A generic builtin's value is its one
// result, an error for append, reserve and put; any other builtin
// returns nothing. It returns false when the call's own problems were
// reported, which leave its results unknown.
func (c *Checker) questionedResults(call *ast.Call) ([]types.Type, string, bool) {
	if c.unboundCall(call) {
		return nil, "", false
	}
	if generic := c.genericCall(call); generic != nil {
		before := len(c.diags)
		// swap and drop, the generic builtins with no value, report a use
		// as one unless it is a statement's call; a ? on either is
		// reported below, as on any call that returns no error.
		outer := c.droppedCall
		if givesNoValue(call) {
			c.droppedCall = call
		}
		typ := generic(call)
		c.droppedCall = outer
		name, _ := c.calleeName(call)
		switch {
		case len(c.diags) > before:
			return nil, "", false
		case typ == types.Invalid:
			return nil, name, true
		}
		return []types.Type{typ}, name, true
	}
	c.checkCall(call)
	name, ok := c.calleeName(call)
	if !ok || isValueCall(call) && call.Dynamic == nil {
		return nil, "", false
	}
	// Any other builtin's Def is a nil *Func, and it returns nothing.
	if fn := ast.Callee(call); fn != nil {
		return ast.CallResults(call), name, true
	}
	return nil, name, true
}

// questionPlace reports whether q is written where a ? can return: in
// the body of a function whose last result is an error, outside deferred
// code. It reports where it cannot.
func (c *Checker) questionPlace(q *ast.Question) bool {
	fn := c.fn
	switch {
	case fn == nil:
		c.errorf(q.Mark(), "cannot use ? outside a function")
	case c.deferred > 0:
		c.errorf(q.Mark(), "cannot return from a defer with ?")
	case len(fn.ResultTypes) == 0:
		c.errorf(q.Mark(), "cannot use ? in %s, which returns no error", c.displayName(fn))
	case fn.ResultTypes[len(fn.ResultTypes)-1] != types.Error:
		last := fn.ResultTypes[len(fn.ResultTypes)-1]
		if last == types.Invalid {
			// The result's type was reported where it is written.
			return false
		}
		c.errorf(q.Mark(), "cannot use ? in %s, whose last result is %s, not error",
			c.displayName(fn), c.typeName(last))
	default:
		return true
	}
	return false
}

// proveQuestionContract reports each fact the contract of the function
// holding q ensures that the facts in force do not prove at q, which
// returns from it when its call's error is set. The call is done by
// then, so a write it makes through a mut argument counts.
func (c *Checker) proveQuestionContract(q *ast.Question) {
	outer := c.at
	c.at = q.Span.End + 1
	c.proveContractWhere(c.fn, q.Mark(), nil, "at this ?")
	c.at = outer
}

// questionsIn returns every ? in e, at any depth, in source order, but
// those in the body of a closure, which return from the closure.
func questionsIn(e ast.Expr) []*ast.Question {
	var found []*ast.Question
	ast.Walk(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Question:
			found = append(found, x)
		case *ast.FuncLit:
			return false
		}
		return true
	})
	return found
}

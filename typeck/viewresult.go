package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A view may be a function's or a method's result, when it is its only
// one: func (t Text) bytes() []uint8. The view it returns points into
// what the caller passed it, so, like a view parameter, it is
// second-class: a call returning one is used only inside the expression
// that consumes it, wherever a view slice may be, passed to a view
// parameter, a variadic one by a spread included, given to len, or
// sliced, and also indexed, x, ok := t.bytes()[i] (an index without ok is
// never proven, since no fact is about a call), ranged over, and returned
// from a function that itself returns one. A call returning a view
// anywhere else, stored, compared, printed or passed as any, is
// reported; written as a statement, it drops the view, as any call's
// result may be dropped, borrowing nothing past the statement. An extern func cannot return a view, and a
// view among several results is reported.
//
// What the view may point into is everything its call was passed but as
// mut that holds data, a list, an array, a struct, a union, a value of a
// type parameter or a view, the receiver included: the call borrows each
// of those places, and the view returned is read in place, as a list
// borrowed by a plain argument is. So a call passing it as an argument
// cannot also pass, at any depth, a place that may overlap one of them as
// mut (checkViewBorrows), and a range over it cannot change one of them
// while it runs (see move.go).
//
// A function may return as a view only what it was passed and does not
// own or change: a view parameter, a list or an array that is a parameter
// that is neither mut nor own, which the function frees when it returns,
// or a field or element of one, a slice of any of these,
// or a call returning a view whose borrowed arguments are all such
// places. A plain parameter is the function's own copy, so an array is
// returned only when it is reached through a list's or a view's element,
// which live in the caller's data; a list's elements always do. A union
// pattern's binding and a range loop's element variable over owned
// elements refer in place into what they match or range over, and are
// rooted where that is (see referent). A local,
// a global, a mut parameter, a temporary, a string's bytes, and values a
// variadic call packs are reported (returnedView).

// resultType returns the type result, the declared type of one of n
// results of a function or an interface's method, denotes: a view,
// written []T, when it is the only result and the function is not an
// extern func, whose name extern is, or "" for any other, and any other
// type as declaredType gives it. A view anywhere else is reported, and
// gives types.Invalid.
func (c *Checker) resultType(result ast.Expr, n int, extern string) types.Type {
	vt, ok := result.(*ast.ViewType)
	if !ok {
		return c.declaredType(result)
	}
	switch {
	case extern != "":
		c.errorf(
			vt.Span,
			"extern func %s cannot return a view: nothing tells the checker what C's view points into",
			extern,
		)
	case n > 1:
		c.errorf(vt.Span, "a view can only be a function's only result")
	default:
		return c.viewType(vt)
	}
	return types.Invalid
}

// externName returns fn's name when it is an extern func, and "" when it
// is not.
func externName(fn *ast.Func) string {
	if fn.Extern {
		return fn.Name
	}
	return ""
}

// isViewCall reports whether e is a call, not a conversion, whose one
// result is a view, once it has been checked.
func (c *Checker) isViewCall(e ast.Expr) bool {
	call, ok := e.(*ast.Call)
	return ok && !isViewConversion(call) && call.Conv == types.Invalid &&
		c.table.IsView(ast.TypeOf(call))
}

// viewCallValue checks typ, the one result of call, used as a value, and
// returns it, or reports it and returns types.Invalid when it is a view
// and allowed says call is not where a view may be (see viewCall).
func (c *Checker) viewCallValue(call *ast.Call, typ types.Type, allowed bool) types.Type {
	if allowed || !c.table.IsView(typ) {
		return typ
	}
	c.errorf(call.Span, "%s is a view, %s, which can only be passed to a view parameter, "+
		"given to len, indexed, sliced, ranged over or returned",
		c.text(call.Span), c.typeName(typ))
	return types.Invalid
}

// allowViewCall marks e, when it is a call, as written where a view it
// returns may be: what an index indexes, or what a range ranges over; and
// e, when it is an element, as written where a span's view of an arena
// may be, which is the same place.
func (c *Checker) allowViewCall(e ast.Expr) {
	switch x := e.(type) {
	case *ast.Call:
		c.viewCall = x
	case *ast.Index:
		c.viewSpan = x
	}
}

// returnView checks value, returned from fn, whose result is the view
// type want: a view, or a list or an array of exactly its element type,
// that points only into what a parameter that is not mut holds (see
// returnedView). A string's bytes, []uint8(s), and a string are
// reported, and any other value is used as want under the usual rules,
// which report it.
func (c *Checker) returnView(value ast.Expr, want types.Type, fn *ast.Func) {
	if conv, ok := value.(*ast.Call); ok && isViewConversion(conv) {
		c.returnBytes(conv)
		return
	}
	c.allowView(value)
	got := c.valueOf(value, c.expr(value))
	switch {
	case got == types.Invalid:
	case got == types.String && c.table.Elem(want) == types.Uint8:
		c.errorf(
			value.Spanned(),
			"cannot return string %s as %s: a returned view cannot point into a string's bytes",
			c.text(value.Spanned()),
			c.typeName(want),
		)
	case (c.viewable(got) || c.table.IsView(got)) && c.table.Elem(got) == c.table.Elem(want):
		c.reportReturnedView(value)
	default:
		c.use(value, got, want, "in return from "+c.displayName(fn))
	}
}

// returnBytes reports conv, a string's bytes, []uint8(s), returned as a
// view, once it checks as a conversion: a returned view cannot point into
// a string's bytes.
func (c *Checker) returnBytes(conv *ast.Call) {
	if c.viewConversion(conv) == types.Invalid {
		return
	}
	c.errorf(conv.Span, "cannot return %s: a returned view cannot point into a string's bytes",
		c.text(conv.Span))
}

// viewPlaceRule ends an error about a view that cannot be returned, saying
// what one may point into.
const viewPlaceRule = "a returned view can only point into what a parameter that is not mut holds"

// arrayPlaceRule ends an error about an array viewed that the function
// holds its own copy of, saying when an array can be returned as a view.
const arrayPlaceRule = "an array is returned as a view only when it is reached through an element of a list or a view"

// viewProblem is why a view cannot be returned: what, the text naming
// what it may point into, at the place it is written, at, cannot be
// pointed into by a returned view, as why says; copied says it is an
// array the function holds its own copy of.
type viewProblem struct {
	at     ast.Expr
	what   string
	why    string
	copied bool
}

// reportReturnedView reports value, a view returned, when it may point
// into anything but what a parameter that is not mut holds (see
// returnedView): as a view of that place when value is the place or a
// slice of it, and otherwise as a call whose view may point into it.
func (c *Checker) reportReturnedView(value ast.Expr) {
	p := c.returnedView(value)
	if p == nil {
		return
	}
	rule := viewPlaceRule
	if p.copied {
		rule = arrayPlaceRule
	}
	if p.at == unsliced(value) {
		c.errorf(value.Spanned(), "cannot return a view of %s: %s; %s",
			c.text(value.Spanned()), p.why, rule)
		return
	}
	c.errorf(value.Spanned(), "cannot return %s: the view it returns may point into %s, and %s; %s",
		c.text(value.Spanned()), p.what, p.why, rule)
}

// unsliced returns what e slices, at any depth of slices, or e when it is
// not a slice.
func unsliced(e ast.Expr) ast.Expr {
	for {
		s, ok := e.(*ast.Slice)
		if !ok {
			return e
		}
		e = s.X
	}
}

// returnedView returns why e, a view returned or what a view returned may
// point into, may point into what a returned view cannot, or nil when it
// can be returned: a view parameter, a list or an array that is a
// parameter that is not mut, or a field or an element of one, an array
// only when its storage is the caller's (callerMemory), a slice of one,
// or a call returning a view that borrows only such places
// (callBorrowsParams). The slices of a slice are followed to what they
// slice, so what is reported is the place itself.
func (c *Checker) returnedView(e ast.Expr) *viewProblem {
	e = unsliced(e)
	problem := func(why string) *viewProblem {
		return &viewProblem{at: e, what: c.text(e.Spanned()), why: why}
	}
	if call, ok := e.(*ast.Call); ok {
		switch {
		case isViewConversion(call):
			return problem("it is a string's bytes")
		case c.isViewCall(call):
			return c.callBorrowsParams(call)
		default:
			return problem("it is a temporary")
		}
	}
	if why, ok := c.paramRoot(e); !ok {
		return problem(why)
	}
	if c.table.IsArray(ast.TypeOf(e)) && !c.callerMemory(e) {
		p := problem("the function holds its own copy of " + c.ownerName(e) +
			", and so of the array, which ends when it returns")
		p.copied = true
		return p
	}
	return nil
}

// paramRoot reports whether e is a place rooted in a parameter that is
// not mut, a parameter or a field or an element of one, and, when it is
// not, says why. A variable that refers in place into something else, a
// union pattern's binding or a range loop's element over an owned one,
// is rooted where what it refers into is, which must be what a returned
// view can point into (see referent).
func (c *Checker) paramRoot(e ast.Expr) (string, bool) {
	root := ast.Root(e)
	if root == nil {
		return "it is a temporary", false
	}
	if v, place, ok := c.referent(root); ok {
		if p := c.returnedView(place); p != nil {
			return v.Name + " refers into " + p.what + ", and " + p.why, false
		}
		return "", true
	}
	switch def := root.Def.(type) {
	case *ast.Param:
		if def.Mut {
			return root.Name + " is a mut parameter", false
		}
		if def.Own {
			return root.Name + " is an own parameter, which is freed when the function returns", false
		}
		return "", true
	case *ast.Var:
		if c.globals[def] {
			return root.Name + " is a global", false
		}
		return root.Name + " is a local variable", false
	default:
		return "it is a temporary", false
	}
}

// callerMemory reports whether the array place e, rooted in a parameter,
// is stored in its caller's data: reached through an element of a list, a
// pool or a view, whose elements its caller holds, a range loop's element over an
// owned one being one, but for one over an arena, which is in the
// caller's data only where its arena is (see boundIn). An arena's element,
// a[e], does not count by itself: an id may name the sentinel, which the
// arena holds inline, so it is in the caller's data only where the arena
// is. A parameter of any other type is its function's
// own copy, and so is every field and array element inside it, and a
// union pattern's binding is a payload of what its switch matches, which
// is followed in its place.
func (c *Checker) callerMemory(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.Index:
			if base := ast.TypeOf(x.X); c.table.IsList(base) || c.table.IsView(base) ||
				c.table.IsPool(base) {
				return true
			}
			e = x.X
		case *ast.Selector:
			e = x.X
		case *ast.Ident:
			next, inCaller := c.boundIn(x)
			if next == nil {
				return inCaller
			}
			e = next
		default:
			return false
		}
	}
}

// boundIn says, for id, the root of an array place, where the place's
// storage is found: in the caller's data, inCaller, when id is a range
// loop's element in place, an element of a list, a pool or a view; in next, what
// its switch matches, when it is a union pattern's binding, or the arena
// a range loop over one walks, when it is that loop's element in place;
// and in the function's own copy otherwise, when it returns neither. An
// arena's range gives its sentinel first, which the arena holds inline,
// so an element of one is in the caller's data only where the arena
// itself is, an element of a list or a view: a plain arena parameter, or
// a field of one, is the function's own copy, sentinel and all.
func (c *Checker) boundIn(id *ast.Ident) (next ast.Expr, inCaller bool) {
	v, isVar := id.Def.(*ast.Var)
	if !isVar {
		return nil, false
	}
	if x, inPlace := c.rangeBindings[v]; inPlace {
		if c.table.IsArena(ast.TypeOf(x)) {
			return x, false
		}
		return nil, true
	}
	return c.unionBindings[v], false
}

// ownerName returns the name of the parameter e, a place, is rooted in,
// through any union pattern's binding to what its switch matches, and any
// range loop's element over an arena to the arena (see boundIn).
func (c *Checker) ownerName(e ast.Expr) string {
	root := ast.Root(e)
	for {
		v, ok := root.Def.(*ast.Var)
		if !ok {
			return root.Name
		}
		in := c.unionBindings[v]
		if x, inPlace := c.rangeBindings[v]; inPlace && c.table.IsArena(ast.TypeOf(x)) {
			in = x
		}
		if in == nil {
			return root.Name
		}
		next := ast.Root(in)
		if next == nil {
			return root.Name
		}
		root = next
	}
}

// referent returns the variable root names and what it refers into in
// place, when it is one that does: a union pattern's binding, which
// refers into what its switch matches, or a range loop's element variable
// over a list or a view of an owned type, which refers into an element of
// what the loop ranges over. Neither is ever assigned, and what it refers
// into cannot change while it is in scope (see move.go), so a view of it
// points where a view of what it refers into would.
func (c *Checker) referent(root *ast.Ident) (*ast.Var, ast.Expr, bool) {
	v, ok := root.Def.(*ast.Var)
	if !ok {
		return nil, nil, false
	}
	if tag := c.unionBindings[v]; tag != nil {
		return v, tag, true
	}
	if x, inPlace := c.rangeBindings[v]; inPlace {
		return v, x, true
	}
	return nil, nil, false
}

// callBorrowsParams returns why call, a call returning a view being
// returned, may return a view of what a returned view cannot point into,
// for its first argument that does, or nil when none does. An argument to
// a view parameter is followed as returnedView follows the value
// returned; one of any other type holding data, passed by value, is the
// callee's own copy, which it can only return a view into the lists of,
// so it needs only to be rooted in a parameter that is not mut. An
// argument to an own parameter is the callee's to free, so no view it
// returns points into it, and it is passed over. A call
// that packs values for a variadic parameter packs them into a temporary,
// which is reported.
func (c *Checker) callBorrowsParams(call *ast.Call) *viewProblem {
	fn := ast.Callee(call)
	if fn == nil {
		return &viewProblem{at: call, what: c.text(call.Span), why: "it is a temporary"}
	}
	for i, arg := range call.Operands() {
		if _, isMut := arg.(*ast.MutArg); isMut || i >= len(fn.Params) || fn.Params[i].Own {
			continue
		}
		if problem := c.borrowedArg(call, fn.Params[i], arg); problem != nil {
			return problem
		}
	}
	return nil
}

// borrowedArg returns why arg, passed to p in call, a call returning a
// view being returned, may hold what a returned view cannot point into,
// as callBorrowsParams says, or nil when it does not.
func (c *Checker) borrowedArg(call *ast.Call, p *ast.Param, arg ast.Expr) *viewProblem {
	switch {
	case p.Variadic && !call.Spread:
		return &viewProblem{
			at: arg, what: "the values for " + p.Name,
			why: "they are packed into a temporary",
		}
	case c.table.IsView(p.Type):
		return c.returnedView(arg)
	case c.holdsData(ast.TypeOf(arg)):
		if why, ok := c.paramRoot(arg); !ok {
			return &viewProblem{at: arg, what: c.text(arg.Spanned()), why: why}
		}
	}
	return nil
}

// holdsData reports whether a value of type typ may hold data a view can
// point into, which a call returning a view borrows: a view, a list, a
// map, an arena, a pool, an array, a struct but error, a union, or a type
// parameter, which may stand for any of them.
func (c *Checker) holdsData(typ types.Type) bool {
	switch {
	case typ == types.Error:
		return false
	case c.table.IsView(typ), c.table.IsList(typ), c.table.IsMap(typ), c.table.IsArena(typ),
		c.table.IsPool(typ), c.table.IsArray(typ), c.table.IsStruct(typ), c.table.IsUnion(typ),
		c.table.IsTypeParam(typ):
		return true
	default:
		return false
	}
}

// viewCallOf returns the call returning a view that e is, or slices at any
// depth, or nil when it is neither.
func (c *Checker) viewCallOf(e ast.Expr) *ast.Call {
	e = unsliced(e)
	if !c.isViewCall(e) {
		return nil
	}
	return e.(*ast.Call)
}

// viewBorrows returns the places the view call returns may point into:
// each of its operands, the receiver among them, that is neither mut nor
// own, which leaves the caller for the call, holds
// data (holdsData) and is a place, or what a slice of one slices, and,
// for one that is itself a call returning a view, the places that call
// borrows, at any depth. An operand that is not a place, a temporary or
// a string's bytes, is no place a caller could change while the view is
// read.
func (c *Checker) viewBorrows(call *ast.Call) []ast.Expr {
	var places []ast.Expr
	for _, arg := range call.Operands() {
		switch arg.(type) {
		case *ast.MutArg, *ast.OwnArg:
			continue
		}
		if inner := c.viewCallOf(arg); inner != nil {
			places = append(places, c.viewBorrows(inner)...)
			continue
		}
		arg = unsliced(arg)
		if ast.Root(arg) != nil && c.holdsData(ast.TypeOf(arg)) {
			places = append(places, arg)
		}
	}
	return places
}

// checkViewBorrows reports each operand of call, a method call's receiver
// among them, that is a call returning a view, or a slice of one, while
// another operand of call, at any depth, passes as mut a place that may
// overlap one the view may point into (viewBorrows), or holds a closure
// that may write one (see writersIn), with a note at that mut argument or
// write. The view is read in place during the call, as a list
// borrowed by a plain argument is (see checkListBorrows), so the place
// passed as mut would be reachable another way. A mut argument inside the
// view's own call is that call's to check: it runs before the view is
// made.
func (c *Checker) checkViewBorrows(call *ast.Call) {
	operands := call.Operands()
	for i, arg := range operands {
		inner := c.viewCallOf(arg)
		if inner == nil || appendedValue(call, i) {
			continue
		}
		ws := writersOutside(operands, i)
		if len(ws) == 0 {
			continue
		}
	places:
		for _, place := range c.viewBorrows(inner) {
			for _, w := range ws {
				if !placesMayOverlap(place, w.place) {
					continue
				}
				c.errorNotes(
					arg.Spanned(),
					[]diag.Diagnostic{w.note()},
					"cannot pass %s alongside %s in this call: the view it returns may point into %s, "+
						"and %s during the call",
					c.text(arg.Spanned()),
					w.what(c),
					c.text(place.Spanned()),
					w.only(c),
				)
				break places
			}
		}
	}
}

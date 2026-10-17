package typeck

import (
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A list, list[T], is an owned, growable list of elements of type T,
// stored by value. make(list[T], alloc) makes an empty one, which
// allocates nothing until append grows it through the allocator alloc,
// an Allocator; c_allocator is the one Allocator there is today. A list
// owns its buffer: it moves rather than copies, and is freed when the
// block of the variable owning it ends (see move.go, and codegen/c).
//
// A list reads as a view does: len, an int64 with the facts a string
// variable's length has, which hold up to a write that can reach the use:
// assigning the list, passing it as mut or moving it, and which a
// successful append adds to (see appended.go); an element, l[i], with an index
// proven in range or the checked form; range, which walks the list
// itself; slicing, l[i:j], to a view; and passing it, whole, to a view
// parameter of its element type. Unlike a view it can be written: an
// element, l[i] = v or l[i].f = v, of a list that is mutable, a mut local
// or a mut parameter, at a proven index; grown, err := append(mut l, a,
// b) or err := append(mut l, v...), as a variadic takes values, whose
// error says the allocator could not grow the buffer, which is then left
// as it was, all of it; and given room ahead, err := reserve(mut l, n),
// for n more elements, a count no constant may make negative. Writing an
// element never changes the list's length, so it ends no fact about it
// (see writes); append and reserve pass the list as mut, which does.
//
// A list's element type may be owned, a list or a struct, an array or a
// union holding one, nested to any depth, but not a type parameter. An
// element of an owned type is never copied out of its list: it is a
// place inside the list, as a struct's list field is, read in place, l[i]
// borrowed by a plain parameter or a method's plain receiver, passed as
// mut l[i], grown, ranged over, matched and indexed in place, and never
// moved out of it but by pop or remove (see move.go): x := l[i], return
// l[i] and the like are reported there, and the checked form, which
// copies, here (see indexResults). Assigning an element, l[i] = v, moves
// v in and frees the element it replaces; append moves its values in. A
// view's element type follows the same rule, a view being a borrow
// already, while an array's and a variadic's elements are copied when
// read, and stay copyable (see requireCopyable).
//
// A list has no zero value, so a variable of one is declared with a
// value, and no package-level variable may hold one, since make is not a
// constant expression and a global would need one. Lists have no ==.
//
// pop(mut l) and remove(mut l, i) move an element out of a list, the last
// or the one at i, moving the rest down to keep their order, and give it;
// neither allocates, so neither returns an error. The plain form needs
// len(l) > 0, or i in range for l, proven, as an index does; the checked
// form, x, ok := pop(mut l), is allowed anywhere, and x then holds T's
// zero value when ok is false, or, for a T with none, cannot be read until
// ok is checked true (see okHeld). Written as a statement, pop(mut l) and
// remove(mut l, i) need no proof: they drop the element when there is one,
// and the backend frees it when it is owned.

// listType returns the list type a written list[T] denotes, adding it to
// the table. Its element type may be any type but a type parameter (see
// requireElement); one that is, or was reported, gives types.Invalid.
func (c *Checker) listType(lt *ast.ListType) types.Type {
	elem := c.declaredType(lt.Elem)
	if elem == types.Invalid {
		return types.Invalid
	}
	if !c.requireElement("a list", elem, lt.Span) {
		return types.Invalid
	}
	return c.table.List(elem)
}

// requireElement reports elem, the element type of holder, a list or a
// view written at at, when it is a type parameter, which neither may
// hold, and returns whether elem may be an element. An owned type may: an
// element of a list or a view is read in place, never copied.
func (c *Checker) requireElement(holder string, elem types.Type, at ast.Span) bool {
	if !c.table.IsTypeParam(elem) {
		return true
	}
	what := "a list's element"
	switch holder {
	case "a view":
		what = "a view's element"
	case "a map":
		what = "a map's value"
	}
	c.errorf(at, "%s cannot hold %s: %s cannot be a type parameter",
		holder, c.typeName(elem), what)
	return false
}

// pendingElem is an element type whose copyability could not be judged
// where it was written, in a struct's field while struct fields are still
// being resolved: what holds it, as a diagnostic names it, the type and
// where it was written.
type pendingElem struct {
	holder string
	at     ast.Span
	elem   types.Type
}

// requireCopyable reports elem, the element type of holder, an array or
// a variadic parameter written at at, when it is owned, a list or a
// struct, an array or a union holding one, or a type parameter, which
// may hold one, which an element cannot be: reading an element copies it,
// and an owned value moves. It returns whether elem may be an
// element. While struct fields are being resolved a struct's fields may
// not be known yet, so the check waits until they are (see
// checkPendingElems) and elem is allowed for now.
func (c *Checker) requireCopyable(holder string, elem types.Type, at ast.Span) bool {
	if c.inFields {
		c.pendingElems = append(c.pendingElems, pendingElem{holder: holder, at: at, elem: elem})
		return true
	}
	if !c.table.Owned(elem) {
		return true
	}
	c.notCopyable(holder, elem, at)
	return false
}

// checkPendingElems reports every element type requireCopyable put off
// that turned out to be owned, once every struct's fields are known.
func (c *Checker) checkPendingElems() {
	for _, p := range c.pendingElems {
		if c.table.Owned(p.elem) {
			c.notCopyable(p.holder, p.elem, p.at)
		}
	}
	c.pendingElems = nil
}

// notCopyable reports elem, an owned type, as the element type of holder,
// written at at.
func (c *Checker) notCopyable(holder string, elem types.Type, at ast.Span) {
	c.errorf(at, "%s cannot hold %s: its elements are copied, and %s cannot be",
		holder, c.typeName(elem), c.ownedPhrase(elem))
}

// ownedPhrase names what makes typ, an owned type, owned, for a
// diagnostic: "a list" for a list, and "T, which holds a list," for an
// array or a struct holding one.
func (c *Checker) ownedPhrase(typ types.Type) string {
	if c.table.IsList(typ) {
		return "a list"
	}
	if c.table.IsTypeParam(typ) {
		return c.typeName(typ) + ", a type parameter, which may hold a list,"
	}
	return c.typeName(typ) + ", which holds a list,"
}

// isBuiltinCall reports whether call calls the generic builtin named
// name, which, like every builtin, has a nil declaration.
func isBuiltinCall(call *ast.Call, name string) bool {
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != name || id.Pkg != "" {
		return false
	}
	fn, ok := id.Def.(*ast.Func)
	return ok && fn == nil
}

// isMake reports whether call calls the builtin make.
func isMake(call *ast.Call) bool { return isBuiltinCall(call, "make") }

// isAppend reports whether call calls the builtin append.
func isAppend(call *ast.Call) bool { return isBuiltinCall(call, "append") }

// isReserve reports whether call calls the builtin reserve.
func isReserve(call *ast.Call) bool { return isBuiltinCall(call, "reserve") }

// returnsListError reports whether call calls append, reserve or put, the
// generic builtins that return an error.
func returnsListError(
	call *ast.Call,
) bool {
	return isAppend(call) || isReserve(call) || isPut(call)
}

// isTake reports whether call calls pop or remove, the generic builtins
// that move an element out of a list, or delete, which moves a value out
// of a map: each gives what it took and, in its checked form, whether
// there was one.
func isTake(
	call *ast.Call,
) bool {
	return isBuiltinCall(call, "pop") || isBuiltinCall(call, "remove") || isDelete(call)
}

// makeCall checks a call to make, make(list[T], alloc) or make(map[K]V,
// alloc), records the type it makes on the call and returns it. It takes
// the list or map type, as written, and an Allocator to grow it through; a capacity is not taken
// yet. Anything else is reported, and gives types.Invalid.
func (c *Checker) makeCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "make", 2) {
		for _, arg := range call.Args {
			if madeType(arg) {
				c.declaredType(arg)
				continue
			}
			c.argExpr(arg)
		}
		return types.Invalid
	}
	if !madeType(call.Args[0]) {
		c.argExpr(call.Args[0])
		c.errorf(call.Args[0].Spanned(),
			"make takes a list or a map type, as in make(list[int64], c_allocator)")
		c.argExpr(call.Args[1])
		return types.Invalid
	}
	typ := c.declaredType(call.Args[0])
	alloc := call.Args[1]
	if !c.use(alloc, c.expr(alloc), types.Allocator, "in argument to make") ||
		typ == types.Invalid {
		return types.Invalid
	}
	call.Type = typ
	return typ
}

// madeType reports whether e is a type make can make: a list type or a
// map type, as written.
func madeType(e ast.Expr) bool {
	switch e.(type) {
	case *ast.ListType, *ast.MapType:
		return true
	default:
		return false
	}
}

// appendCall checks a call to append, err := append(mut l, a, b) or err
// := append(mut l, v...), records its type, error, on the call and
// returns it. append is variadic, append(mut l list[T], elems ...T), and
// is checked as a call to such a function is (see variadic.go): its first
// argument is the list to grow, passed as mut, a place of a list type;
// after it come any number of values, none included, each usable as T,
// or a view of T spread whole, what a []T parameter takes. The call
// copies them onto the list's end, or, for an owned T, moves them there
// (see move.go), so a view of an owned T, whose elements would be
// copied, cannot be spread. Anything else is reported, and gives
// types.Invalid. The call is a call like any other otherwise: its mut
// argument is a place no other argument may reach (see exclusive.go), a
// spread list included (see checkListBorrows), and its error cannot be
// dropped (see errors.go).
func (c *Checker) appendCall(call *ast.Call) types.Type {
	if !c.checkVariadicArity(call, "append", []param{{name: "l"}}, param{name: "elems"}) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	elem := c.listTarget(call.Args[0], "append")
	if call.Spread {
		c.appendSpread(call.Args[1], elem)
		if len(c.diags) == before && c.table.Owned(elem) {
			c.errorf(
				call.Args[1].Spanned(),
				"cannot spread %s into append: its elements would be copied, and %s cannot be",
				c.text(call.Args[1].Spanned()),
				c.ownedPhrase(elem),
			)
		}
	} else {
		value := param{name: "elems", typ: elem}
		for _, arg := range call.Args[1:] {
			c.checkArg(arg, value, "append")
		}
	}
	c.checkListBorrows(call)
	if len(c.diags) > before {
		return types.Invalid
	}
	call.Type = types.Error
	return types.Error
}

// appendSpread checks arg, the view a call to append spreads, v..., into
// its variadic parameter, elems, of type []T for the list's element type
// elem, as a spread into any variadic is checked; only a spread adds the
// view type []T to the table. When elem is unknown, which was reported,
// arg is checked only for errors of its own.
func (c *Checker) appendSpread(arg ast.Expr, elem types.Type) {
	if elem != types.Invalid {
		c.checkArg(arg, param{name: "elems", typ: c.table.View(elem), variadic: true}, "append")
		return
	}
	if conv, ok := arg.(*ast.Call); ok && isViewConversion(conv) {
		c.viewConversion(conv)
		return
	}
	c.argExpr(arg)
}

// reserveCall checks a call to reserve, err := reserve(mut l, n), records
// its type, error, on the call and returns it. Its first argument is the
// list to make room in, passed as mut as append's is, and its second the
// number of elements to make room for, usable as an int64, which must not
// be negative when it is known: a constant, or a value the checker knows.
// Anything else is reported, and gives types.Invalid. Like append's, its
// error cannot be dropped.
func (c *Checker) reserveCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "reserve", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	c.listTarget(call.Args[0], "reserve")
	n := call.Args[1]
	c.checkArg(n, param{name: "n", typ: types.Int64}, "reserve")
	if len(c.diags) > before {
		return types.Invalid
	}
	if v, known := c.knownValue(n); known && v.IsNegative() {
		c.errorf(n.Spanned(), "reserve count must not be negative, got %s", v)
		return types.Invalid
	}
	call.Type = types.Error
	return types.Error
}

// popCall checks a call to pop, x := pop(mut l) or x, ok := pop(mut l),
// records the type of the element it gives on the call and returns it.
// Its one argument is the list to take the last element of, passed as mut
// as append's is. The plain form needs len(l) > 0 proven (see
// requireNotEmpty); the checked form, the call c.checkedTake is, needs
// nothing, and nor does a call written as a statement, the call
// c.droppedCall is, which drops the last element when there is one, and
// frees it when it is owned. Anything else is reported, and gives
// types.Invalid.
func (c *Checker) popCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "pop", 1) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	elem := c.listTarget(call.Args[0], "pop")
	if elem == types.Invalid {
		return types.Invalid
	}
	call.Type = elem
	if c.checkedTake != call && c.droppedCall != call {
		c.requireNotEmpty(call, call.Args[0].(*ast.MutArg).X)
	}
	return elem
}

// removeCall checks a call to remove, x := remove(mut l, i) or x, ok :=
// remove(mut l, i), records the type of the element it gives on the call
// and returns it. Its first argument is the list to take element i of,
// passed as mut as append's is, and its second the index, usable as an
// int64, as reserve's count is. The plain form needs i proven in range
// for l, as l[i] does; the checked form, the call c.checkedTake is, needs
// nothing, and nor does a call written as a statement, the call
// c.droppedCall is, which drops element i when it is in range, and frees
// it when it is owned, though in either a negative constant i, never in
// range, is still reported, as an index is. Anything else is reported,
// and gives types.Invalid.
func (c *Checker) removeCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "remove", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	elem := c.listTarget(call.Args[0], "remove")
	i := call.Args[1]
	c.checkArg(i, param{name: "i", typ: types.Int64}, "remove")
	if len(c.diags) > before || elem == types.Invalid {
		return types.Invalid
	}
	call.Type = elem
	// The element remove reads, as the index l[i] would, which the proofs
	// of an index are asked about.
	ix := &ast.Index{X: call.Args[0].(*ast.MutArg).X, Index: i, Span: call.Span, Type: elem}
	if v, known := c.knownValue(i); known && v.IsNegative() {
		c.reportOutside(ix, v)
		return types.Invalid
	}
	if c.checkedTake != call && c.droppedCall != call && !c.stringIndexProven(ix) {
		msg, notes := c.stringIndexError(ix, true)
		msg += orUse("x, ok := "+c.text(call.Span), strings.HasSuffix(msg, " first"))
		c.errorNotes(i.Spanned(), notes, "%s", msg)
	}
	return elem
}

// requireNotEmpty reports l, the list a plain pop takes the last element
// of, at call, when the facts in force do not prove it holds one: len(l)
// is at least 1. The error says what check would prove it, when l is a
// subject, a variable or a path below an immutable one, and the checked
// form, with notes on a check that falls short.
func (c *Checker) requireNotEmpty(call *ast.Call, l ast.Expr) {
	one := constant.FromInt64(1)
	proven := func() bool { return c.minLen(l).Cmp(one) >= 0 }
	if proven() {
		return
	}
	name := c.text(l.Spanned())
	msg := "pop may find " + name + " empty"
	if bound := c.stringBound(l); bound != "" {
		msg += "; check " + bound + " > 0 first"
	}
	msg += orUse("x, ok := pop(mut "+name+")", strings.HasSuffix(msg, " first"))
	var notes []diag.Diagnostic
	if c.isSubject(l) {
		notes = c.explain(c.lengthProof(l, proven))
	}
	notes = append(notes, c.indexedNotes(l)...)
	c.errorNotes(call.Span, notes, "%s", msg)
}

// takeResults checks a call to pop or remove bound to want names, the
// checked form, which gives two: the element, or, when the list is empty
// or the index out of range, T's zero value, or nothing to read for a T
// with none (see okHeld); and whether it gave one. It needs no proof.
func (c *Checker) takeResults(call *ast.Call, want int, span ast.Span) ([]types.Type, bool) {
	c.checkedTake = call
	elem := c.expr(call)
	c.checkedTake = nil
	if elem == types.Invalid {
		return nil, false
	}
	if want != 2 {
		id, _ := call.Fn.(*ast.Ident)
		c.errorf(span, "assignment mismatch: %d variables but %s gives 2 values", want, id.Name)
		return nil, false
	}
	return []types.Type{elem, types.Bool}, true
}

// listTarget checks arg, the list a call to builtin, append, reserve, pop
// or remove, grows or takes from, which must be passed as mut, mut l, and
// be a place of a list type, and returns the list's element type,
// types.Invalid when anything was reported.
func (c *Checker) listTarget(arg ast.Expr, builtin string) types.Type {
	m, isMut := arg.(*ast.MutArg)
	if !isMut {
		c.expr(arg)
		c.missingMut(arg, param{name: "l"}, builtin)
		return types.Invalid
	}
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case !c.table.IsList(got):
		c.errorf(m.X.Spanned(), "%s expects a list, got %s", builtin, c.typeName(got))
		return types.Invalid
	default:
		return c.table.Elem(got)
	}
}

// genericCall returns the checker of the generic builtin call calls, len,
// make, append, reserve, pop, remove, replace, put or delete, each of which checks its own
// arguments and gives one value, or nil when call calls none of them.
func (c *Checker) genericCall(call *ast.Call) func(*ast.Call) types.Type {
	switch {
	case isLen(call):
		return c.lenCall
	case isMake(call):
		return c.makeCall
	case isReplace(call):
		return c.replaceCall
	case isPut(call):
		return c.putCall
	case isDelete(call):
		return c.deleteCall
	default:
		return c.listCall(call)
	}
}

// listCall returns the checker of the generic builtin call calls when it
// is one that changes a list, append, reserve, pop or remove, or nil.
func (c *Checker) listCall(call *ast.Call) func(*ast.Call) types.Type {
	switch {
	case isBuiltinCall(call, "pop"):
		return c.popCall
	case isBuiltinCall(call, "remove"):
		return c.removeCall
	case isAppend(call):
		return c.appendCall
	case isReserve(call):
		return c.reserveCall
	default:
		return nil
	}
}

// listTypeValue reports a list type written where a value is wanted,
// anywhere but as make's first argument, after checking it for errors of
// its own, and returns types.Invalid.
func (c *Checker) listTypeValue(lt *ast.ListType) types.Type {
	if typ := c.listType(lt); typ != types.Invalid {
		c.errorf(lt.Span, "cannot use %s as a value; it is a type", c.typeName(typ))
	}
	return types.Invalid
}

// noZeroValue reports v, a variable of type typ declared without a value,
// when typ has no zero value to start it from: a list, an Allocator, or
// an array or a struct holding one. It returns whether it did.
func (c *Checker) noZeroValue(v *ast.Var, typ types.Type) bool {
	if typ == types.Invalid || c.table.HasZero(typ) {
		return false
	}
	c.errorf(v.NameSpan, "%s needs a value: %s has no zero value", v.Name, c.typeName(typ))
	return true
}

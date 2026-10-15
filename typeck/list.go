package typeck

import (
	"github.com/tacklang/tack/ast"
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
// variable's length has, which hold in a region that never assigns the
// list, passes it as mut or moves it; an element, l[i], with an index
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
// A list's element type must be copyable, since reading an element
// copies it: no list, and no array or struct holding one. Neither may an
// array's, nor a view's. A list has no zero value, so a variable of one
// is declared with a value, and no package-level variable may hold one,
// since make is not a constant expression and a global would need one.
// Lists have no ==.

// listType returns the list type a written list[T] denotes, adding it to
// the table. Its element type must be a type a list can hold (see
// requireCopyable); one that cannot be, or was reported, gives
// types.Invalid.
func (c *Checker) listType(lt *ast.ListType) types.Type {
	elem := c.declaredType(lt.Elem)
	if elem == types.Invalid {
		return types.Invalid
	}
	if !c.requireCopyable("a list", elem, lt.Span) {
		return types.Invalid
	}
	return c.table.List(elem)
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

// requireCopyable reports elem, the element type of holder, a list, an
// array or a view written at at, when it is owned, a list or a struct or
// array holding one, which an element cannot be: reading an element
// copies it, and an owned value moves. It returns whether elem may be an
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

// returnsListError reports whether call calls append or reserve, the
// generic builtins that return an error.
func returnsListError(call *ast.Call) bool { return isAppend(call) || isReserve(call) }

// makeCall checks a call to make, make(list[T], alloc), records the list
// type it makes on the call and returns it. It takes the list type, as
// written, and an Allocator to grow it through; a capacity is not taken
// yet. Anything else is reported, and gives types.Invalid.
func (c *Checker) makeCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "make", 2) {
		for _, arg := range call.Args {
			if lt, isType := arg.(*ast.ListType); isType {
				c.listType(lt)
				continue
			}
			c.argExpr(arg)
		}
		return types.Invalid
	}
	lt, ok := call.Args[0].(*ast.ListType)
	if !ok {
		c.argExpr(call.Args[0])
		c.errorf(call.Args[0].Spanned(),
			"make takes a list type, as in make(list[int64], c_allocator)")
		c.argExpr(call.Args[1])
		return types.Invalid
	}
	typ := c.listType(lt)
	alloc := call.Args[1]
	if !c.use(alloc, c.expr(alloc), types.Allocator, "in argument to make") ||
		typ == types.Invalid {
		return types.Invalid
	}
	call.Type = typ
	return typ
}

// appendCall checks a call to append, err := append(mut l, a, b) or err
// := append(mut l, v...), records its type, error, on the call and
// returns it. append is variadic, append(mut l list[T], elems ...T), and
// is checked as a call to such a function is (see variadic.go): its first
// argument is the list to grow, passed as mut, a place of a list type;
// after it come any number of values, none included, each usable as T,
// or a view of T spread whole, what a []T parameter takes. The call
// copies them onto the list's end. Anything else is reported, and gives
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

// listTarget checks arg, the list a call to builtin, append or reserve,
// grows, which must be passed as mut, mut l, and be a place of a list
// type, and returns the list's element type, types.Invalid when anything
// was reported.
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
// make, append or reserve, each of which checks its own arguments and
// gives one value, or nil when call calls none of them.
func (c *Checker) genericCall(call *ast.Call) func(*ast.Call) types.Type {
	switch {
	case isLen(call):
		return c.lenCall
	case isMake(call):
		return c.makeCall
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

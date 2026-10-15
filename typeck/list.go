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
// or a mut parameter, at a proven index; and grown, err := append(mut l,
// v), whose error says the allocator could not grow the buffer, which is
// then left as it was. Writing an element never changes the list's
// length, so it ends no fact about it (see writes).
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

// appendCall checks a call to append, err := append(mut l, v), records
// its type, error, on the call and returns it. Its first argument is the
// list to grow, passed as mut, a place of a list type, and its second a
// value usable as the list's element type, which the call copies onto
// its end. Anything else is reported, and gives types.Invalid. The call
// is a call like any other otherwise: its mut argument is a place no
// other argument may reach (see exclusive.go), and its error cannot be
// dropped (see errors.go).
func (c *Checker) appendCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "append", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	elem := c.appendTarget(call.Args[0])
	value := call.Args[1]
	if m, isMut := value.(*ast.MutArg); isMut {
		c.expr(m.X)
		text := c.text(m.X.Spanned())
		c.errorf(m.Span, "append does not take v as mut; pass %s", text)
		return types.Invalid
	}
	if !c.use(value, c.expr(value), elem, "in argument to append") {
		return types.Invalid
	}
	call.Type = types.Error
	return types.Error
}

// appendTarget checks arg, the list an append grows, which must be passed
// as mut, mut l, and be a place of a list type, and returns the list's
// element type, types.Invalid when anything was reported.
func (c *Checker) appendTarget(arg ast.Expr) types.Type {
	m, isMut := arg.(*ast.MutArg)
	if !isMut {
		c.expr(arg)
		c.missingMut(arg, param{name: "l"}, "append")
		return types.Invalid
	}
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case !c.table.IsList(got):
		c.errorf(m.X.Spanned(), "append expects a list, got %s", c.typeName(got))
		return types.Invalid
	default:
		return c.table.Elem(got)
	}
}

// genericCall returns the checker of the generic builtin call calls, len,
// make or append, each of which checks its own arguments and gives one
// value, or nil when call calls none of them.
func (c *Checker) genericCall(call *ast.Call) func(*ast.Call) types.Type {
	switch {
	case isLen(call):
		return c.lenCall
	case isMake(call):
		return c.makeCall
	case isAppend(call):
		return c.appendCall
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

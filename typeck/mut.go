package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A parameter written mut p T is a mutable reference to the caller's
// variable for the duration of a call: the callee reads and writes the
// caller's variable through p, p.x or p[i]. The call passes it as
// mut x, and only to a mut parameter: a mut parameter takes nothing else,
// and a plain parameter never takes mut x. What follows mut must be a
// place the caller may change: a mutable local, a mut parameter, a field
// of one, or an element of one at an index proven in range, nested in any
// combination. A mutable global is reachable from every function, so it is
// never passed as mut; an immutable variable, a plain parameter, a
// string's byte and a computed value are not places that can change. The
// argument's type is the parameter's exactly: a reference to an int32
// cannot stand for one to an int64, as widening a value could. A mut
// written anywhere but before a call's argument is an error.
//
// A reference exists only as a parameter. Nothing can store one: q := p
// copies the caller's value, and return p returns a copy. Passing p on as
// mut p to another call is how a reference reaches further.

// param is what checking an argument needs to know about the parameter
// it is passed to: its name, its type, and whether it is mut, own, or
// variadic, the last parameter, args ...T, whose type is the view []T.
type param struct {
	name     string
	typ      types.Type
	mut      bool
	own      bool
	scope    bool
	variadic bool

	// sig is whether it is a parameter of a function's signature, a
	// method's or a function type's, and not one of a builtin, and extern
	// whether that function is an extern func; a message about a scope
	// allocator passed to it says so (see scopeAllocToPlain).
	sig, extern bool
}

// calleeParams returns the parameters of the function or builtin id
// names, none when it names neither.
func (c *Checker) calleeParams(id *ast.Ident) []param {
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil {
		params := c.funcParams(fn.Params)
		for i := range params {
			params[i].extern = fn.Extern
		}
		return params
	}
	fn, _ := c.builtins.Lookup(id.Name)
	params := make([]param, len(fn.Params))
	for i, typ := range fn.Params {
		params[i] = param{typ: typ}
	}
	return params
}

// checkArg checks arg, passed to p in a call to callee: a mut argument to
// a mut parameter must name a place of exactly its type, and any other
// argument must be usable as its plain parameter's type, or be what
// viewArg allows a view parameter, or anyArg an any parameter, to take. A
// mut argument to a plain parameter, or a plain one to a mut parameter, is
// reported, and what it holds is still checked for errors of its own; own
// is checked first, as ownArg says. It returns the mut
// argument when arg is one passed to a mut parameter with nothing
// reported, for the exclusivity check, and nil otherwise.
func (c *Checker) checkArg(arg ast.Expr, p param, callee string) *ast.MutArg {
	if p.scope {
		c.scopeArg(arg, p, callee)
		return nil
	}
	if lit, ok := arg.(*ast.FuncLit); ok {
		c.funcLitToPlain(lit, p, callee)
		return nil
	}
	if id, ok := arg.(*ast.Ident); ok && p.sig && c.isScopeAlloc(id) {
		c.scopeAllocToPlain(id, p, callee)
		return nil
	}
	if c.inference.mentionsAny(c, p.typ) {
		return c.inferArg(arg, p, callee)
	}
	arg, ok := c.ownArg(arg, p, callee)
	if !ok {
		return nil
	}
	m, isMut := arg.(*ast.MutArg)
	switch {
	case p.mut && isMut:
		c.checkErrorMut(m)
		return c.mutToMut(m, p, callee)
	case p.mut:
		c.expr(arg)
		c.missingMut(arg, p, callee)
	case isMut:
		c.expr(m.X)
		text := c.text(m.X.Spanned())
		c.errorf(m.Span, "%s does not take %s as mut; pass %s", callee, text, text)
	case c.table.IsView(p.typ):
		c.viewArg(arg, p.typ, callee)
	case p.typ == types.Any:
		c.anyArg(arg)
	default:
		c.use(arg, c.expr(arg), p.typ, "in argument to "+callee)
	}
	return nil
}

// mutToMut checks m, a mut argument passed to the mut parameter p of
// callee: its place must be one of exactly p's type. It returns m when
// nothing was reported, and nil otherwise.
func (c *Checker) mutToMut(m *ast.MutArg, p param, callee string) *ast.MutArg {
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return nil
	case p.typ != types.Invalid && got != p.typ:
		c.errorf(m.X.Spanned(), "cannot use %s as %s in argument to %s",
			c.typeName(got), c.typeName(p.typ), callee)
		return nil
	default:
		return m
	}
}

// missingMut reports arg, a plain argument passed to the mut parameter p
// of callee: the fix is mut before it when it is a mutable variable, a
// field or an element, what argFix says when it is not, and otherwise a
// place in its stead.
func (c *Checker) missingMut(arg ast.Expr, p param, callee string) {
	if !namesVariable(arg) {
		c.errorf(arg.Spanned(),
			"%s takes %s as mut; only a variable, a field or an element can be passed as mut",
			callee, p.name)
		return
	}
	fix := c.argFix(arg)
	head := fmt.Sprintf("%s takes %s as mut", callee, p.name)
	cure := "pass mut " + c.text(arg.Spanned())
	c.errorNotes(arg.Spanned(), fix.notes, "%s", fix.message(head, cure))
}

// The bindings no declaration can make mut, as bindings records them:
// each is what a message says of one, given its name.
const (
	rangeVar       = "%s is a range loop variable, which cannot be mut"
	typeSwitchVar  = "%s is a type switch variable, which cannot be mut"
	payloadBinding = "%s binds a union's payload, which cannot be mut"
)

// stringByte is what a message about passing a string's byte as mut says
// of it: strings are immutable, whatever holds the string, so nothing
// makes the byte a place.
var stringByte = rootFix{problem: "strings are immutable"}

// argFix returns what a message about passing e as mut, written with mut
// or without, says of it: a string's byte is never a place, whatever its
// root, and otherwise mutRootFix says it of the variable at e's root.
func (c *Checker) argFix(e ast.Expr) rootFix {
	if x, ok := e.(*ast.Index); ok && c.placeType(x.X) == types.String {
		return stringByte
	}
	return c.mutRootFix(ast.Root(e))
}

// rootFix is what a message about an argument written without mut, where
// mut is needed, says of the variable at its root. problem is why the
// variable cannot be passed as mut, "" when it can; fix is what lets it,
// "" when nothing can; and notes, set only when the fix is to declare the
// variable with mut, after which mut still goes before the argument, is
// a note at its declaration.
type rootFix struct {
	problem string
	fix     string
	notes   []diag.Diagnostic
}

// mutRootFix returns what a message about passing an argument as mut,
// written with mut (placeName) or without (missingMut and
// receiverWithoutMut), says of root, the variable or parameter at the
// argument's root, so both say the same of it: paramFix says it of a
// parameter and varFix of a variable. A name left unresolved, whose error
// is already reported, can be passed as mut.
func (c *Checker) mutRootFix(root *ast.Ident) rootFix {
	switch def := root.Def.(type) {
	case *ast.Param:
		return c.paramFix(root, def)
	case *ast.Var:
		return c.varFix(root, def)
	default:
		return rootFix{}
	}
}

// paramFix returns what mutRootFix says of def, the parameter root names:
// a mut parameter can be passed as mut, and a plain one once declared
// with mut in its signature, unless it is a closure's, whose signature
// the call fixes, which is copied into a mutable local instead; an own
// parameter is moved into a mutable variable first; a view parameter
// cannot be passed as mut at all.
func (c *Checker) paramFix(root *ast.Ident, def *ast.Param) rootFix {
	switch {
	case def.Mut:
		return rootFix{}
	case def.Own:
		return rootFix{
			problem: root.Name + " is an own parameter, which is immutable",
			fix:     "move it into a mutable variable with mut x := " + root.Name,
		}
	case c.table.IsView(def.Type):
		return rootFix{problem: root.Name + " is a read-only view"}
	case c.closureParams[def]:
		return rootFix{
			problem: root.Name + " is a closure parameter, whose type the call fixes",
			fix:     "copy it into a mutable local",
		}
	default:
		return rootFix{
			problem: root.Name + " is immutable",
			fix:     "declare it with mut in the signature",
			notes:   declaredHere(root.Name, def.NameSpan),
		}
	}
}

// varFix returns what mutRootFix says of def, the variable root names: a
// mutable local can be passed as mut, and an immutable one once declared
// with mut; a global is copied into a mutable local first, a mutable one
// because any function can reach it; a binding no declaration can make mut (see
// bindings) cannot be passed as mut at all.
func (c *Checker) varFix(root *ast.Ident, def *ast.Var) rootFix {
	kind := c.bindings[def]
	switch {
	case def.Mutable && c.globals[def]:
		return rootFix{
			problem: root.Qualified() + " is a mutable global, which any function can reach",
			fix:     "copy it into a mutable local",
		}
	case def.Mutable:
		return rootFix{}
	case c.globals[def]:
		return rootFix{
			problem: root.Qualified() + " is immutable",
			fix:     "copy it into a mutable local",
		}
	case kind != "":
		return rootFix{problem: fmt.Sprintf(kind, root.Name)}
	default:
		return rootFix{
			problem: root.Name + " is immutable",
			fix:     "declare it with mut",
			notes:   declaredHere(root.Name, def.NameSpan),
		}
	}
}

// placeMessage returns the message about text, a mut argument whose
// place f says cannot be passed as mut: the problem, and its fix when
// there is one.
func (f rootFix) placeMessage(text string) string {
	if f.fix == "" {
		return "cannot pass " + text + ": " + f.problem
	}
	return "cannot pass " + text + ": " + f.problem + "; " + f.fix
}

// message returns the message about an argument written without mut,
// where mut is needed, that starts with head, what takes it as mut, and
// whose fix, when the root can be passed as mut, is cure, mut written
// before it: cure alone when the root can be; the root's problem and its
// fix, followed by cure when the fix is to declare the root with mut; and
// the problem alone when nothing can make the root a place.
func (f rootFix) message(head, cure string) string {
	switch {
	case f.problem == "":
		return head + "; " + cure
	case f.fix == "":
		return head + ", but " + f.problem
	case f.notes != nil:
		return head + ", but " + f.problem + "; " + f.fix + ", then " + cure
	default:
		return head + ", but " + f.problem + "; " + f.fix
	}
}

// declaredHere returns a note that the variable or parameter name is
// declared at span, its name in its declaration.
func declaredHere(name string, span ast.Span) []diag.Diagnostic {
	return []diag.Diagnostic{note(span, name+" is declared here")}
}

// namesVariable reports whether e is a variable or a parameter, or a field
// or an element of one, written as a name, a field or an element is. Only
// such an argument can take mut, which it does once the variable at its
// root is a mutable local or a mut parameter; mutRootFix says what makes
// it one, if anything can. A union built in place, Expr.eof, a method
// named through its type, Pair.first, and a function named are not,
// though each is written as a name or a field is: the name at their root
// is a type or a function. A name left unresolved, whose error is already
// reported, counts as a variable.
func namesVariable(e ast.Expr) bool {
	root := ast.Root(e)
	if root == nil {
		return false
	}
	switch root.Def.(type) {
	case *ast.Var, *ast.Param, *ast.BadDecl, nil:
		return true
	default:
		return false
	}
}

// argExpr checks an argument of a call whose argument count is wrong, or
// whose callee was reported, which is matched with no parameter: only for
// errors of its own, so a mut argument is not reported as passed to a
// plain parameter, a closure, whose place is unknown, not at all, and a
// scope allocator, a region's or a scope Allocator parameter, not as
// passed where none may go, since where it goes is unknown too.
func (c *Checker) argExpr(arg ast.Expr) {
	switch x := arg.(type) {
	case *ast.Ident:
		if c.isScopeAlloc(x) {
			c.scopeUse = x
			c.expr(x)
			c.scopeUse = nil
			return
		}
		c.expr(arg)
	case *ast.FuncLit:
		// With no parameter to pass it to, whether it may be passed, and
		// what type its signature must have, is unknown.
	case *ast.MutArg:
		c.expr(x.X)
	case *ast.OwnArg:
		c.expr(x.X)
	default:
		c.expr(arg)
	}
}

// place checks e, the place the mut argument m names, or part of it, and
// returns its type: a mutable local or a mut parameter, a field of a
// place, or an element of a place whose index is proven in range. Anything
// else, a slice, a union built in place, Expr.eof, or a method named
// through its type, Pair.first, each written as a field is, included, is
// reported, and gives types.Invalid.
func (c *Checker) place(e ast.Expr, m *ast.MutArg) types.Type {
	_, isMethodExpr := ast.MethodExpr(e)
	if _, _, _, built := ast.Construction(e); built || isMethodExpr {
		if c.expr(e) != types.Invalid {
			c.errorf(m.Span, "only a variable, a field or an element can be passed as mut")
		}
		return types.Invalid
	}
	switch x := e.(type) {
	case *ast.Ident:
		return c.placeName(x, m)
	case *ast.Selector:
		base := c.place(x.X, m)
		if base == types.Invalid {
			return types.Invalid
		}
		x.Type = c.field(x, base)
		return x.Type
	case *ast.Index:
		return c.placeElement(x, m)
	case *ast.Slice:
		c.notPlace(m.Span, "pass "+c.text(m.Span))
		return types.Invalid
	default:
		// Something already reported has no type, and is not reported
		// again as not being a place.
		if c.expr(e) != types.Invalid {
			c.errorf(m.Span, "only a variable, a field or an element can be passed as mut")
		}
		return types.Invalid
	}
}

// placeElement checks x, an element of the place the mut argument m
// names, or of part of it, as place does, and returns its type: an
// element of a place, whose index is proven in range. A string's byte is
// reported, as strings are immutable, before anything about the string's
// root, which no fix to the root could change; one whose string has no
// type its declarations give is reported once its string is checked.
func (c *Checker) placeElement(x *ast.Index, m *ast.MutArg) types.Type {
	if c.placeType(x.X) == types.String {
		c.expr(x.X)
		c.expr(x.Index)
		c.errorf(m.Span, "%s", stringByte.placeMessage(c.text(m.Span)))
		return types.Invalid
	}
	base := c.place(x.X, m)
	switch base {
	case types.Invalid:
		c.expr(x.Index)
		return types.Invalid
	case types.String:
		c.expr(x.Index)
		c.errorf(m.Span, "%s", stringByte.placeMessage(c.text(m.Span)))
		return types.Invalid
	}
	x.Type = c.element(x, base, true)
	return x.Type
}

// placeName checks the variable or parameter id names, where a mut
// argument m starts: a mutable local or a mut parameter, whose type it
// returns. Any other variable or parameter is reported as mutRootFix
// says, and anything else as no place; each gives types.Invalid.
func (c *Checker) placeName(id *ast.Ident, m *ast.MutArg) types.Type {
	typ := c.ident(id)
	switch id.Def.(type) {
	case *ast.Param, *ast.Var:
		fix := c.mutRootFix(id)
		if fix.problem == "" {
			return typ
		}
		c.errorNotes(m.Span, fix.notes, "%s", fix.placeMessage(c.text(m.Span)))
	default:
		if typ != types.Invalid {
			c.errorf(m.Span, "only a variable, a field or an element can be passed as mut")
		}
	}
	return types.Invalid
}

// mutElsewhere reports a mut argument written anywhere but as a call's
// argument, after checking what it holds for errors of its own. mut
// written several times in a row, mut mut x, is one error.
func (c *Checker) mutElsewhere(m *ast.MutArg) types.Type {
	x := m.X
	for {
		inner, ok := x.(*ast.MutArg)
		if !ok {
			break
		}
		x = inner.X
	}
	c.expr(x)
	c.errorf(m.Span, "%s can only be passed to a mut parameter", c.text(m.Span))
	return types.Invalid
}

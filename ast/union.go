package ast

// UnionType is a union type as written, union { int int64; name string;
// eof }: its cases in the order they are declared, each a name and the
// type of its payload, or no payload. A value of the union holds exactly
// one of its cases, with that case's payload. It appears only in a
// TypeDecl; anonymous union types are not supported.
type UnionType struct {
	Cases []*UnionCase
	Span  Span // the 'union' keyword through the closing brace
}

var _ Expr = (*UnionType)(nil)

func (u *UnionType) exprKey() exprKey { return _exprKey }

func (u *UnionType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'union' keyword through the
// closing brace.
func (u *UnionType) Spanned() Span { return u.Span }

// Case returns the case of the union named name, and its position among
// the cases, or nil and -1 when it has none.
func (u *UnionType) Case(name string) (*UnionCase, int) {
	for i, c := range u.Cases {
		if c.Name == name {
			return c, i
		}
	}
	return nil, -1
}

// UnionCase is one case of a union type: its name and the type of its
// payload as written, or nil for a case with no payload. Like a struct's
// Field it is part of its UnionType rather than a node of its own; Walk
// visits its type. A case has no visibility of its own: it is exactly as
// visible as its union.
type UnionCase struct {
	TypeExpr Expr // the payload's type as written; nil for a case with no payload
	Name     string
	NameSpan Span // just the identifier
	Span     Span // the name through the end of the payload's type, if any
}

// Union returns the union type d declares, or nil when d declares a
// struct type or an interface.
func (d *TypeDecl) Union() *UnionType {
	u, _ := d.TypeExpr.(*UnionType)
	return u
}

// Construction reports whether e builds a value of a union, and returns
// the parts it is built from: the union's declaration, the selector
// naming the case, Expr.int, and the payload, or nil for a case named
// without parentheses, Expr.eof. e is either a call whose callee is that
// selector, Expr.int(5), or the selector alone. The selector's operand,
// a name, plain or qualified, is bound to the union's TypeDecl by the
// resolver, which binds a name written there to nothing but a union. A
// call holding anything but one argument, Expr.eof() or Expr.int(1, 2),
// is a construction too, with a nil payload; the type checker reports it.
func Construction(e Expr) (td *TypeDecl, sel *Selector, payload Expr, ok bool) {
	var call *Call
	switch x := e.(type) {
	case *Call:
		call = x
		sel, _ = x.Fn.(*Selector)
	case *Selector:
		sel = x
	}
	if sel == nil {
		return nil, nil, nil, false
	}
	id, isName := sel.X.(*Ident)
	if !isName {
		return nil, nil, nil, false
	}
	td, isType := id.Def.(*TypeDecl)
	if !isType || td == nil {
		return nil, nil, nil, false
	}
	if call != nil && len(call.Args) == 1 {
		payload = call.Args[0]
	}
	return td, sel, payload, true
}

// CasePattern reads e, a value of a case of a switch, as a pattern naming
// a case of a union: a name alone, eof, which names the case and binds
// nothing, or a name and one name in parentheses, int(n), which names the
// case and binds its payload to n, or binds nothing for int(_). It
// returns the case's name and the binding's, nil for none, and false when
// e has neither shape. Whether a switch's values are patterns is known
// only from its tag's type, which the type checker finds; the resolver
// binds a value of this shape as a pattern where it cannot be a value,
// its binding's name visible nowhere (see resolver).
func CasePattern(e Expr) (name, bind *Ident, ok bool) {
	switch x := e.(type) {
	case *Ident:
		if x.Pkg != "" || x.Name == Blank {
			return nil, nil, false
		}
		return x, nil, true
	case *Call:
		return callPattern(x)
	default:
		return nil, nil, false
	}
}

// callPattern is CasePattern for a call, which is a pattern when it is a
// name and one name in parentheses, neither qualified, with no spread.
func callPattern(call *Call) (name, bind *Ident, ok bool) {
	fn, isName := call.Fn.(*Ident)
	if !isName || fn.Pkg != "" || len(call.Args) != 1 || call.Spread {
		return nil, nil, false
	}
	arg, isArg := call.Args[0].(*Ident)
	if !isArg || arg.Pkg != "" {
		return nil, nil, false
	}
	return fn, arg, true
}

// UnionConst is the computed value of a package-level variable of a union
// type, as Var.Const holds it: the position of its case among the
// union's cases, which is its tag, and its payload's computed value, as
// Var.Const would hold a variable of the payload's type, or nil for a case
// with no payload.
type UnionConst struct {
	Payload any
	Case    int
}

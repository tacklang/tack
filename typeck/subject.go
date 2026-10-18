package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A fact is about a subject: a variable or a parameter, or a path below an
// immutable one, a field, p.d, an element at an index that cannot change,
// p.a[1] or p.a[k], or any chain of those, p.rows[k].d. A path below an
// immutable variable, an immutable parameter or an immutable global names
// a value nothing can change while the variable is in scope, so a fact
// about it holds for the whole of any region, under the same rules as a
// fact about a variable. A path below anything mutable, a mutable variable
// or a mut parameter, is never a subject: a field of one never carries a
// fact. An index in a path must be a known value or a name of an immutable
// variable or parameter, so that the path names one element wherever it is
// written; no name may be reused while another of that name is in scope,
// so within the region of a fact a name in its path names one variable.

// factKey identifies a fact's subject: the variable or parameter decl it
// starts from, and the fields and elements after it, as written by nameOf
// without the variable's name, ".d", ".a[1]" or ".a[k]", or "" for the
// variable itself.
type factKey struct {
	decl ast.Decl
	path string
}

// declKey returns the key of the variable or parameter decl itself.
func declKey(decl ast.Decl) factKey {
	return factKey{decl: decl}
}

// isZero reports whether k is the zero key, which names nothing.
func (k factKey) isZero() bool {
	return k.decl == nil
}

// name returns how a note names the subject k: the variable's name and its
// path, p.d.
func (k factKey) name() string {
	return declName(k.decl) + k.path
}

// keyOf returns the subject e is, when a fact can be about it: a variable
// that can hold a fact (canHoldFact), or a path below an immutable one. A
// float never is: floats carry no facts.
func (c *Checker) keyOf(e ast.Expr) (factKey, bool) {
	id, ok := e.(*ast.Ident)
	switch {
	case types.IsFloat(ast.TypeOf(e)):
		return factKey{}, false
	case !ok:
		return c.pathKey(e)
	case c.canHoldFact(id.Def):
		return declKey(id.Def), true
	default:
		return factKey{}, false
	}
}

// subjectKey returns the subject facts about e would be about: the
// variable any name names, whether or not it can hold a fact, so that its
// type still bounds it, or a path below an immutable variable. A float is
// no subject: floats carry no facts.
func (c *Checker) subjectKey(e ast.Expr) (factKey, bool) {
	if types.IsFloat(ast.TypeOf(e)) {
		return factKey{}, false
	}
	if id, ok := e.(*ast.Ident); ok {
		return declKey(id.Def), true
	}
	return c.pathKey(e)
}

// pathKey returns the key of e, a field or an element, when it is a path
// below an immutable variable or parameter whose every index is a known
// value or an immutable name, and the zero key and false otherwise.
func (c *Checker) pathKey(e ast.Expr) (factKey, bool) {
	var k factKey
	ok := false
	switch x := e.(type) {
	case *ast.Selector:
		k, ok = c.pathBase(x.X)
		k.path += "." + x.Name
	case *ast.Index:
		var step string
		k, ok = c.pathBase(x.X)
		step, stepOK := c.indexStep(x.Index)
		k.path += "[" + step + "]"
		ok = ok && stepOK
	}
	if !ok {
		return factKey{}, false
	}
	return k, true
}

// pathBase returns the key of e, the start of a longer path: an immutable
// variable or parameter, or a path below one.
func (c *Checker) pathBase(e ast.Expr) (factKey, bool) {
	if id, ok := e.(*ast.Ident); ok {
		return declKey(id.Def), immutableName(id)
	}
	return c.pathKey(e)
}

// immutableName reports whether id names a variable, local or global, or
// a parameter that nothing can assign: one declared without mut.
func immutableName(id *ast.Ident) bool {
	switch d := id.Def.(type) {
	case *ast.Var:
		return !d.Mutable
	case *ast.Param:
		return !d.Mut
	default:
		return false
	}
}

// indexStep returns how an index in a path is written in its key: its
// value when it is known, or the name of the immutable variable or
// parameter it is, and false for any other index.
func (c *Checker) indexStep(index ast.Expr) (string, bool) {
	if v, ok := c.knownValue(index); ok {
		return v.String(), true
	}
	if id, ok := index.(*ast.Ident); ok && immutableName(id) {
		return id.Qualified(), true
	}
	return "", false
}

// nameOf returns how a message names e, a subject: a name as written, or
// a path below one, its indexes written as indexStep writes them, p.a[1].
// Anything else is ..., as operandName writes it.
func (c *Checker) nameOf(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Qualified()
	case *ast.Selector:
		return c.nameOf(x.X) + "." + x.Name
	case *ast.Index:
		if step, ok := c.indexStep(x.Index); ok {
			return c.nameOf(x.X) + "[" + step + "]"
		}
	}
	return "..."
}

// isSubject reports whether e names a subject facts can be about, a name
// or a path below an immutable one, which a message names and a check can
// prove.
func (c *Checker) isSubject(e ast.Expr) bool {
	_, ok := c.subjectKey(e)
	return ok
}

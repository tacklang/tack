package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Regions and scope allocators. A region, region r { ... }, opens an
// allocator, r, that lives for its block: r is an immutable variable of
// type Allocator, declared at the start of the block, and what is made
// through it is given back, in one step, when the block is left, however
// it is left. The block is checked as any block of its function is, a
// return, a ? and a break or continue leaving it included, and it
// terminates exactly when its body does, since the body runs once, in
// place.
//
// r is second class, as a scope parameter of a function type is: it may
// be given to make as its allocator, passed to a parameter marked scope
// whose type is Allocator, and captured by a closure, which can only go
// to a scope parameter itself, and nothing else. Copied into a local,
// assigned, returned, put in a field, an element, a list, a map or a
// global, passed to any other parameter, an extern func's among them, it
// is reported (see scopeAllocElsewhere and scopeAllocToPlain), each with
// a note at the region. A parameter written scope a Allocator is under
// the same rule, in the function that declares it, so no callee can keep
// a region's allocator past the call it was given it for: such a
// parameter takes any Allocator, c_allocator included, and an extern
// func cannot declare one (see checkExternScope), so r never reaches C.
// r and a scope Allocator parameter are scope allocators (see
// isScopeAlloc).
//
// What is made through r cannot outlive the block either; that is the
// escape rule, checked per function once its body is checked (see
// escape.go).

// regionAlloc is what a message about making a region's allocator mut
// says of it, as bindings records it.
const regionAlloc = "%s is a region's allocator, which cannot be mut"

// checkRegion checks s, a region of fn: its name is an immutable variable
// of type Allocator, recorded as a region's allocator (see isScopeAlloc),
// and its body is a block of fn.
func (c *Checker) checkRegion(s *ast.Region, fn *ast.Func) {
	s.Var.Type = types.Allocator
	c.regions[s.Var] = s
	c.bindings[s.Var] = regionAlloc
	c.checkBlock(s.Body, fn)
}

// isScopeAlloc reports whether id names a scope allocator: a region's
// allocator, or a parameter marked scope, which scope fits, of type
// Allocator.
func (c *Checker) isScopeAlloc(id *ast.Ident) bool {
	switch d := id.Def.(type) {
	case *ast.Var:
		_, ok := c.regions[d]
		return ok
	case *ast.Param:
		return c.scopeFits(d) && d.Type == types.Allocator
	default:
		return false
	}
}

// regionNotes returns the note naming s, the region whose allocator a
// message is about, at its keyword and name.
func (c *Checker) regionNotes(s *ast.Region) []diag.Diagnostic {
	if s == nil {
		return nil
	}
	span := s.Span
	span.End = s.Var.NameSpan.End
	return []diag.Diagnostic{note(span, fmt.Sprintf(
		"region %s is opened here, and everything made through %s is given back when its block ends",
		s.Var.Name,
		s.Var.Name,
	))}
}

// allocRegion returns the region whose allocator id names, or nil when id
// names a scope Allocator parameter.
func (c *Checker) allocRegion(id *ast.Ident) *ast.Region {
	if v, ok := id.Def.(*ast.Var); ok {
		return c.regions[v]
	}
	return nil
}

// scopeAllocElsewhere reports id, a name of a scope allocator, used
// anywhere but as make's allocator, an argument to a scope parameter or a
// capture, where its value could outlive the region or the call it is
// for.
func (c *Checker) scopeAllocElsewhere(id *ast.Ident) {
	if s := c.allocRegion(id); s != nil {
		c.errorNotes(
			id.Span,
			c.regionNotes(s),
			"%s is a region's allocator, which can only be given to make or passed to a scope Allocator parameter",
			id.Name,
		)
		return
	}
	c.errorf(
		id.Span,
		"%s is a scope Allocator parameter, which can only be given to make or passed to another scope parameter",
		id.Name,
	)
}

// scopeAllocToPlain reports id, a name of a scope allocator, passed to
// p, a parameter of callee's signature that is not scope: an extern
// func's, which none can be, or any other function's.
func (c *Checker) scopeAllocToPlain(id *ast.Ident, p param, callee string) {
	what := "a scope Allocator parameter"
	if c.allocRegion(id) != nil {
		what = "a region's allocator"
	}
	if p.extern {
		c.errorNotes(id.Span, c.regionNotes(c.allocRegion(id)),
			"%s is %s, which never crosses to C, and %s is an extern func", id.Name, what, callee)
		return
	}
	c.errorNotes(id.Span, c.regionNotes(c.allocRegion(id)),
		"%s does not take %s as scope, and %s can only be passed to a scope parameter",
		callee, p.name, what)
}

// allocArg checks alloc, the allocator make is given, and returns its
// type: a scope allocator may be one.
func (c *Checker) allocArg(alloc ast.Expr) types.Type {
	if id, ok := alloc.(*ast.Ident); ok && c.isScopeAlloc(id) {
		c.scopeUse = id
		defer func() { c.scopeUse = nil }()
	}
	return c.expr(alloc)
}

// scopeAllocArg checks arg, passed to p, a scope Allocator parameter of
// callee: any Allocator, a scope allocator among them, which may be
// passed on to it. A mut or an own argument is reported as for a plain
// parameter, and a closure, which is no Allocator, as a mismatch.
func (c *Checker) scopeAllocArg(arg ast.Expr, p param, callee string) {
	switch x := arg.(type) {
	case *ast.MutArg, *ast.OwnArg:
		c.checkArg(arg, param{name: p.name, typ: p.typ}, callee)
		return
	case *ast.FuncLit:
		c.errorf(x.Func.NameSpan, "cannot use a closure as Allocator in argument to %s", callee)
		return
	case *ast.Ident:
		if c.isScopeAlloc(x) {
			c.scopeUse = x
			defer func() { c.scopeUse = nil }()
		}
	}
	c.use(arg, c.expr(arg), types.Allocator, "in argument to "+callee)
}

package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Invalid names and damaged source. The checker runs even when the stages
// before it reported errors, so the tree it is given may hold names the
// resolver bound to ast.Unbound, because what they name was reported or
// may have been declared by source the parser could not parse, and bad
// nodes, ast.BadDecl and ast.BadStmt, where that source was, and bad
// clauses, a switch's clauses whose header could not be parsed, each
// starting its body with a BadStmt (see ast.CaseClause). A name bound
// to ast.Unbound has types.Invalid, which every check passes over as
// already reported, so a use of it reports nothing. A bad node is skipped.
//
// What a bad statement hides cannot be known: it may have declared a
// name, assigned a variable, returned, or established what a later check
// needs proved. So the body of a function that holds one is checked, but
// only what is reported before the first bad statement in it is kept, and
// the checks that need the whole body, that it ends in a return and that
// no moved value is used, are not made. A bad declaration may have been
// a method, so a method its package does not declare, of the name it got
// to or of any name when it got to none, is not reported missing.
//
// None of this is ever trusted to make a program valid: it applies only
// when an earlier stage reported an error (prior), and code is never
// generated from a program with one (see codegen.Refuse). Without one, a
// name bound to ast.Unbound is reported as undefined, and a bad node as a
// syntax error, as if nothing had explained them.

// unbound reports whether id is bound to ast.Unbound, which gives it no
// type and reports nothing more, unless no earlier stage reported an
// error, when it is reported as undefined.
func (c *Checker) unbound(id *ast.Ident) bool {
	if id.Def != ast.Unbound {
		return false
	}
	if c.prior == 0 {
		c.errorf(id.Span, "undefined: %s", id.Qualified())
	}
	return true
}

// unboundCall reports whether call calls a name bound to ast.Unbound (see
// unbound), having checked each argument on its own, since what it is
// passed to is unknown.
func (c *Checker) unboundCall(call *ast.Call) bool {
	id, ok := call.Fn.(*ast.Ident)
	if !ok || !c.unbound(id) {
		return false
	}
	for _, arg := range call.Args {
		if conv, ok := arg.(*ast.Call); ok && isViewConversion(conv) {
			c.viewConversion(conv)
			continue
		}
		c.allowView(arg)
		c.argExpr(arg)
	}
	return true
}

// checkDamagedBody checks the body of fn, which holds bad statements, the
// first of them first, dropping what is reported in fn after it (see the
// comment at the top of the file). What checking the body reports outside
// fn, about a declaration it needed, is kept.
func (c *Checker) checkDamagedBody(fn *ast.Func, first *ast.BadStmt) {
	before := len(c.diags)
	c.checkBody(fn)
	kept := c.diags[:before]
	for _, d := range c.diags[before:] {
		if d.Span.File != first.Span.File || d.Span.Start < first.Span.Start ||
			d.Span.Start >= fn.Span.End {
			kept = append(kept, d)
		}
	}
	c.diags = kept
	c.reportBad(first.Span)
}

// findLostMethods records the methods each package's bad declarations may
// have declared (see lostMethods).
func (c *Checker) findLostMethods() {
	c.lostMethods = make(map[*ast.Package]map[string]bool)
	c.lostAll = make(map[*ast.Package]bool)
	for _, pkg := range c.prog.Packages {
		for _, decl := range pkg.Decls() {
			bad, ok := decl.(*ast.BadDecl)
			switch {
			case !ok:
			case bad.Name == "":
				c.lostAll[pkg] = true
			case bad.Method:
				if c.lostMethods[pkg] == nil {
					c.lostMethods[pkg] = make(map[string]bool)
				}
				c.lostMethods[pkg][bad.Name] = true
			}
		}
	}
}

// lostMethod reports whether a bad declaration of pkg, a package whose
// type is said to have no method name, may have been that method, after
// an earlier stage reported an error.
func (c *Checker) lostMethod(pkg *ast.Package, name string) bool {
	return c.prior > 0 && pkg != nil && (c.lostAll[pkg] || c.lostMethods[pkg][name])
}

// lostMethodOf reports whether typ, a struct or union type said to have
// no method name, may have had it declared by a bad declaration of its
// package (see lostMethod).
func (c *Checker) lostMethodOf(typ types.Type, name string) bool {
	td := c.typeDecl(typ)
	if td == nil || td.Method(name) != nil {
		return false
	}
	return c.lostMethod(c.declPkg[td], name)
}

// checkBadDecls reports every bad declaration of the program as a syntax
// error when no earlier stage reported an error, which the parser always
// did for one (see reportBad).
func (c *Checker) checkBadDecls() {
	for _, decl := range c.prog.Decls() {
		if bad, ok := decl.(*ast.BadDecl); ok {
			c.reportBad(bad.Span)
		}
	}
}

// reportBad reports a bad node, at span, as a syntax error when no earlier
// stage reported an error: the parser reports the error that made every
// bad node, so one with none reported was given to the checker by mistake,
// and must not pass as valid.
func (c *Checker) reportBad(span ast.Span) {
	if c.prior == 0 {
		c.errorf(span, "syntax error: this code could not be parsed")
	}
}

// hasBadClause reports whether s has a clause the parser could not parse,
// which may have been any case or the default (see ast.CaseClause).
func hasBadClause(s *ast.Switch) bool {
	for _, cl := range s.Cases {
		if cl.Bad {
			return true
		}
	}
	return false
}

package resolver

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// Invalid names. The resolver goes on past every error it reports, and runs
// on trees the parser recovered from errors in (see package parser), so a
// name may name nothing because of an error already known: its
// declaration was reported, its import could not be loaded, or the
// declaration or statement that would have declared it could not be
// parsed. Such a name is bound to ast.Unbound and not reported, so each
// error is reported once, at its cause, and the type checker reports
// nothing more about the name either.
//
// Damaged source hides uses as well as declarations. A variable a bad
// statement may have read, an import or a global a bad declaration or
// statement may have used, is not reported as unused, and a missing main
// that a bad declaration may have been is not reported either.

// damage is what the bad nodes of one package hide (see the comment
// above).
type damage struct {
	// lost holds the names the package's bad declarations were declaring,
	// and lostAll is whether one of them could have declared any name,
	// having got no name before its error.
	lost map[string]bool

	// files holds each file of the package with a bad node.
	files map[source.FileID]bool

	// from and last are where the first and the last bad statement of the
	// function whose body is being resolved start, from -1 in a function
	// with none: a name used after the first that names nothing may have
	// been declared by it, and a variable declared before the last may
	// have been read by it.
	from, last int

	lostAll bool // see lost
}

// newDamage returns what the bad nodes of pkg hide.
func newDamage(pkg *ast.Package) damage {
	d := damage{
		lost:  make(map[string]bool),
		files: make(map[source.FileID]bool),
		from:  -1,
		last:  -1,
	}
	for _, f := range pkg.Files {
		for _, decl := range f.Decls {
			switch x := decl.(type) {
			case *ast.BadDecl:
				d.files[f.Span.File] = true
				switch {
				case x.Name == "":
					d.lostAll = true
				case !x.Method:
					d.lost[x.Name] = true
				}
			case *ast.Func:
				if len(ast.BadStmts(x.Body)) > 0 {
					d.files[f.Span.File] = true
				}
			}
		}
	}
	return d
}

// damaged reports whether the package has a bad node.
func (d *damage) damaged() bool {
	return len(d.files) > 0
}

// enterBody records where the bad statements of body, a function's body
// being resolved, are.
func (d *damage) enterBody(body []ast.Stmt) {
	d.from, d.last = -1, -1
	if bad := ast.BadStmts(body); len(bad) > 0 {
		d.from, d.last = bad[0].Span.Start, bad[len(bad)-1].Span.Start
	}
}

// mayDeclare reports whether damaged source may have declared name where
// it is used, at span: a bad declaration of the package, or a bad
// statement of the function before it.
func (d *damage) mayDeclare(name string, span ast.Span) bool {
	return d.lostAll || d.lost[name] || d.from >= 0 && span.Start > d.from
}

// mayRead reports whether a bad statement of the function being resolved
// may have read v, a local variable: it is declared before the last one.
func (d *damage) mayRead(v *ast.Var) bool {
	return d.last >= 0 && v.NameSpan.Start < d.last
}

// settle finishes the binding of id, a name used in sc, which found diags:
// a name that names nothing usable is bound to ast.Unbound, and its
// problems are dropped when what they say follows from an error already
// known (see the comment at the top of the file). A name a declaration
// visible here declares, which was reported, is one; so is one no
// declaration visible here declares, which damaged source may have
// declared.
func (r *pkgResolver) settle(id *ast.Ident, diags []diag.Diagnostic, sc *scope) []diag.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	if id.Def == nil {
		id.Def = ast.Unbound
	}
	if id.Pkg != "" {
		if r.lostQualified(id, sc) {
			return nil
		}
		return diags
	}
	if sc.isRejected(id.Name) {
		return nil
	}
	if r.unknown(id.Name, sc) && r.damage.mayDeclare(id.Name, id.Span) {
		return nil
	}
	return diags
}

// unknown reports whether name, used in sc, is one no declaration visible
// there declares, and no function or builtin of the package, nor _ or a
// basic type's name, which nothing declares.
func (r *pkgResolver) unknown(name string, sc *scope) bool {
	if _, visible := sc.lookup(name); visible {
		return false
	}
	if _, isFunc := r.seen(name); isFunc {
		return false
	}
	if _, isBuiltin := r.builtinSeen(name); isBuiltin {
		return false
	}
	_, isBasic := types.Lookup(name)
	return name != ast.Blank && !isBasic
}

// lostQualified reports whether the qualified name id, pkg.name, used in sc,
// names nothing because of damaged source: no import of pkg is visible,
// and damaged source may have declared one, or the package imported has
// damaged declarations that may have declared name.
func (r *pkgResolver) lostQualified(id *ast.Ident, sc *scope) bool {
	decl, visible := sc.lookup(id.Pkg)
	if !visible {
		return r.damage.mayDeclare(id.Pkg, id.PkgSpan)
	}
	imp, ok := decl.(*ast.Import)
	if !ok {
		return false
	}
	target := r.packages[imp.Package]
	if target == nil {
		return false
	}
	if _, declared := target.member(id.Name); declared {
		return false
	}
	return target.damage.lostAll || target.damage.lost[id.Name]
}

// rejectedUse binds id, a name used as a value where a declaration of it
// visible there, decl, if any, was reported, or a declaration between the
// use and decl was, to ast.Unbound: which of them the use means is
// unknown. decl, a variable, counts as read, since the use may mean it.
func (r *pkgResolver) rejectedUse(id *ast.Ident, decl ast.Decl) []diag.Diagnostic {
	id.Def = ast.Unbound
	if v, ok := decl.(*ast.Var); ok {
		r.read[v] = true
	}
	return nil
}

// lostType binds id, a name written as a type that no declaration visible
// here declares, to ast.Unbound, and reports whether it did: damaged source
// may have declared it. A name it does not bind is left for the type
// checker to report as an unknown type.
func (r *pkgResolver) lostType(id *ast.Ident) bool {
	if !r.damage.mayDeclare(id.Name, id.Span) {
		return false
	}
	id.Def = ast.Unbound
	return true
}

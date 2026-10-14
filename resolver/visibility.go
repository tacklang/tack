package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// visibilityOf returns the visibility of decl, a package-level declaration,
// and the keyword's span that gave it.
func visibilityOf(decl ast.Decl) (ast.Visibility, ast.Span) {
	switch d := decl.(type) {
	case *ast.Func:
		return d.Vis, d.VisSpan
	case *ast.Var:
		return d.Vis, d.VisSpan
	case *ast.TypeDecl:
		return d.Vis, d.VisSpan
	default:
		return ast.Private, ast.Span{}
	}
}

// checkVisibility reports every pub or lib the package cannot have: any at
// all in package main, which no package can import, so that the keyword
// would mean nothing, one on a mut global, which only its own package may
// change, and one on an extern func, which its package wraps instead.
func (r *pkgResolver) checkVisibility() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		vis, span := visibilityOf(decl)
		if vis != ast.Private {
			diags = append(diags, r.visibilityProblems(decl, vis, span)...)
		}
		if td, ok := decl.(*ast.TypeDecl); ok && r.pkg.Name == mainPackage {
			diags = append(diags, fieldsVisibleInMain(td)...)
		}
	}
	return diags
}

// visibilityProblems reports decl, written with the pub or lib that gives
// vis at span, if it cannot have it: nothing in package main can, nor an
// extern func, nor a mut global.
func (r *pkgResolver) visibilityProblems(
	decl ast.Decl,
	vis ast.Visibility,
	span ast.Span,
) []diag.Diagnostic {
	var msg string
	v, isVar := decl.(*ast.Var)
	switch {
	case r.pkg.Name == mainPackage:
		return []diag.Diagnostic{visibleInMain(vis, span)}
	case isExtern(decl):
		msg = fmt.Sprintf("an extern func cannot be %s; export a Tack function that calls it", vis)
	case isVar && v.Mutable:
		msg = fmt.Sprintf(
			"a mut variable cannot be %s; export a function that reads or changes it",
			vis,
		)
	default:
		return nil
	}
	return []diag.Diagnostic{{Severity: diag.Error, Message: msg, Span: span}}
}

// isExtern reports whether decl is an extern func.
func isExtern(decl ast.Decl) bool {
	fn, ok := decl.(*ast.Func)
	return ok && fn.Extern
}

// fieldsVisibleInMain reports every field of td, a type of package main,
// that is pub or lib.
func fieldsVisibleInMain(td *ast.TypeDecl) []diag.Diagnostic {
	st, ok := td.TypeExpr.(*ast.StructType)
	if !ok {
		return nil
	}
	var diags []diag.Diagnostic
	for _, f := range st.Fields {
		if f.Vis != ast.Private {
			diags = append(diags, visibleInMain(f.Vis, f.VisSpan))
		}
	}
	return diags
}

// visibleInMain reports the keyword giving vis, at span, in package main.
func visibleInMain(vis ast.Visibility, span ast.Span) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("%s has no effect in package main, which no package can import", vis),
		Span:     span,
	}
}

// access reports why the package being resolved cannot use member, a
// package-level declaration of owner named by the qualified name id, or
// returns nil when it can.
func (r *pkgResolver) access(member ast.Decl, owner *ast.Package, id *ast.Ident) []diag.Diagnostic {
	vis, _ := visibilityOf(member)
	if ast.Visible(vis, owner, r.pkg) {
		return nil
	}
	msg := fmt.Sprintf("%s is not pub in package %s", id.Name, owner.Path)
	if vis == ast.Lib {
		msg = fmt.Sprintf("%s is lib in module %s and cannot be used from module %s",
			id.Name, owner.Module, r.pkg.Module)
	}
	return []diag.Diagnostic{{Severity: diag.Error, Message: msg, Span: id.Span}}
}

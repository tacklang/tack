package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// typeParamScope returns the scope of fn's type parameters, nested in
// file, the scope of fn's file, in which fn's signature is resolved and
// which encloses its parameters; for a function with no type parameters
// it is file itself. A type parameter follows a parameter's naming rules:
// no reserved name, no name of the package's or of an import, and no
// other type parameter's; each one reported is rejected, so a use of it
// is not reported again. The constraints are resolved once every type
// parameter is declared, in their scope, so a constraint naming a type
// parameter is bound to it, and the type checker reports it as no
// interface.
func (r *pkgResolver) typeParamScope(fn *ast.Func, file *scope) (*scope, []diag.Diagnostic) {
	if !fn.Generic() {
		return file, nil
	}
	tps := newScope(file)
	var diags []diag.Diagnostic
	for _, tp := range fn.TypeParams {
		if clash := typeParamClash(tp, tps); clash != nil {
			diags = append(diags, clash...)
			tps.reject(tp.Name)
			continue
		}
		tps.names[tp.Name] = tp
	}
	for _, tp := range fn.TypeParams {
		diags = append(diags, r.resolveType(tp.Constraint, tps)...)
	}
	return tps, diags
}

// typeParamClash reports why tp cannot be declared in tps, the scope of
// its function's type parameters: its name is reserved, a package-level
// name's or an import's, or another type parameter's. It returns nil when
// the name is free.
func typeParamClash(tp *ast.TypeParam, tps *scope) []diag.Diagnostic {
	if reserved := reservedName(tp.Name, tp.NameSpan); reserved != nil {
		return reserved
	}
	if outer, clash := tps.parent.lookup(tp.Name); clash {
		return []diag.Diagnostic{redeclared(tp.Name, tp.NameSpan, outer)}
	}
	if existing, dup := tps.names[tp.Name]; dup {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "duplicate type parameter name",
			Span:     tp.NameSpan,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("previous declaration of %s", tp.Name),
				Span:     nameSpan(existing),
			}},
		}}
	}
	return nil
}

// resolveInterfaces resolves the methods of every interface type the
// package declares: each method's parameter and result types, in the
// scope of the interface's file, and its name, which is in its
// interface's namespace, so it may share one with anything outside it but
// not with another method of the interface, and follows a field's naming
// rule. A method's parameters are named as a function's are, no two
// alike and none reserved; since a method of an interface has no body,
// nothing they could shadow is ever in scope, so they may share a name
// with anything else.
func (r *pkgResolver) resolveInterfaces() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		td, ok := decl.(*ast.TypeDecl)
		if !ok || td.Interface() == nil {
			continue
		}
		sc := r.fileScope(td.Span.File)
		seen := make(map[string]*ast.IfaceMethod)
		for _, m := range td.Interface().Methods {
			diags = append(diags, r.resolveIfaceMethod(m, sc)...)
			if reserved := reservedName(m.Name, m.NameSpan); reserved != nil {
				diags = append(diags, reserved...)
				continue
			}
			if prev, dup := seen[m.Name]; dup {
				name := td.Name + "." + m.Name
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Message:  fmt.Sprintf("%s is already declared", name),
					Span:     m.NameSpan,
					Related: []diag.Diagnostic{{
						Severity: diag.Note,
						Message:  fmt.Sprintf("previous declaration of %s", name),
						Span:     prev.NameSpan,
					}},
				})
				continue
			}
			seen[m.Name] = m
		}
	}
	return diags
}

// resolveIfaceMethod resolves the types of m, a method of an interface,
// in sc, and reports a parameter name that is reserved or another
// parameter's.
func (r *pkgResolver) resolveIfaceMethod(m *ast.IfaceMethod, sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	names := make(map[string]*ast.Param, len(m.Params))
	for _, p := range m.Params {
		diags = append(diags, r.resolveType(p.TypeExpr, sc)...)
		if reserved := reservedName(p.Name, p.NameSpan); reserved != nil {
			diags = append(diags, reserved...)
			continue
		}
		if prev, dup := names[p.Name]; dup {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  "duplicate parameter name",
				Span:     p.NameSpan,
				Related: []diag.Diagnostic{{
					Severity: diag.Note,
					Message:  fmt.Sprintf("previous declaration of %s", p.Name),
					Span:     prev.NameSpan,
				}},
			})
			continue
		}
		names[p.Name] = p
	}
	for _, result := range m.Results {
		diags = append(diags, r.resolveType(result, sc)...)
	}
	return diags
}

// isTypeName reports whether decl declares a name that stands for a type:
// a type declaration or a type parameter.
func isTypeName(decl ast.Decl) bool {
	switch decl.(type) {
	case *ast.TypeDecl, *ast.TypeParam:
		return true
	default:
		return false
	}
}

package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
)

// fileScope returns the scope of the file id: its imports, enclosed by the
// global scope. A file with no scope of its own, which declareImports gives
// every file, gets the global scope.
func (r *pkgResolver) fileScope(id source.FileID) *scope {
	if sc, ok := r.files[id]; ok {
		return sc
	}
	return r.globals
}

// declareImports gives every file of the package a scope holding its
// imports, enclosed by the global scope, and reports every import that
// cannot be declared: one whose name is reserved, a builtin's, any
// package-level name of the package, in any file, or another import's of
// the same file, and one of a package the file already imports under
// another name. Imports are per file: one file's are not visible in
// another.
func (r *pkgResolver) declareImports() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, f := range r.pkg.Files {
		sc := newScope(r.globals)
		r.files[f.Span.File] = sc
		paths := make(map[string]*ast.Import)
		for _, imp := range f.Imports {
			if clash := r.importClash(imp, sc, paths); clash != nil {
				diags = append(diags, clash...)
				r.rejected[imp] = true
				sc.reject(imp.Name)
				continue
			}
			sc.names[imp.Name] = imp
			paths[imp.Path] = imp
			if imp.Package == nil {
				// The loader reported why the package could not be
				// loaded; what the import would name follows from that.
				r.rejected[imp] = true
				sc.reject(imp.Name)
			}
		}
	}
	return diags
}

// importClash reports why imp cannot be declared in sc, its file's scope,
// where paths holds the file's imports declared so far by path. It returns
// nil when it can.
func (r *pkgResolver) importClash(
	imp *ast.Import,
	sc *scope,
	paths map[string]*ast.Import,
) []diag.Diagnostic {
	if reserved := reservedName(imp.Name, imp.NameSpan); reserved != nil {
		return reserved
	}
	if _, isBuiltin := r.builtinSeen(imp.Name); isBuiltin {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is a builtin and cannot be redeclared", imp.Name),
			Span:     imp.NameSpan,
		}}
	}
	if existing, ok := sc.lookup(imp.Name); ok {
		return []diag.Diagnostic{alreadyDeclared(imp.Name, imp.NameSpan, existing)}
	}
	if fn, isFunc := r.seen(imp.Name); isFunc {
		return []diag.Diagnostic{alreadyDeclared(imp.Name, imp.NameSpan, fn)}
	}
	if prev, dup := paths[imp.Path]; dup {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is already imported as %s", imp.Path, prev.Name),
			Span:     imp.PathSpan,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("previous import of %s", imp.Path),
				Span:     prev.PathSpan,
			}},
		}}
	}
	return nil
}

// unusedImports reports every import of every file that no qualified name
// in the file used, unless its declaration was reported, or its package
// could not be loaded, or the file has damaged source, which may have
// used it (see invalid.go).
func (r *pkgResolver) unusedImports() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, f := range r.pkg.Files {
		if r.damage.files[f.Span.File] {
			continue
		}
		for _, imp := range f.Imports {
			if !r.used[imp] && !r.rejected[imp] {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Warning,
					Message:  fmt.Sprintf("imported and not used: %s", imp.Name),
					Span:     imp.NameSpan,
					Unused:   true,
				})
			}
		}
	}
	return diags
}

// mainOutsideMain reports every package-level declaration named main in a
// package other than main: the name is the main package's. A method is
// not one, since its name is in its type's namespace.
func (r *pkgResolver) mainOutsideMain() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		if keyword := mainKeyword(decl); keyword != "" {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  fmt.Sprintf("%s main is allowed only in package main", keyword),
				Span:     nameSpan(decl),
			})
		}
	}
	return diags
}

// mainKeyword returns the keyword that declares decl, a package-level
// declaration, when it is named main, and "" otherwise. A method named
// main is not one: its name is in its type's namespace.
func mainKeyword(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.Func:
		if d.Name == mainFunc && !d.Method {
			return "func"
		}
	case *ast.TypeDecl:
		if d.Name == mainFunc {
			return "type"
		}
	case *ast.Var:
		if d.Name == mainFunc && d.Mutable {
			return "mut"
		}
		if d.Name == mainFunc {
			return "var"
		}
	}
	return ""
}

// member returns the package-level declaration of the package named name:
// a function, a type, or a global, whatever its visibility, which access
// checks.
func (r *pkgResolver) member(name string) (ast.Decl, bool) {
	if fn, ok := r.seen(name); ok && fn != nil {
		return fn, true
	}
	decl, ok := r.globals.names[name]
	return decl, ok
}

// qualified returns the declaration the qualified name id, pkg.name, names
// in the package the import pkg stands for, marking the import used, or
// why it names none: pkg is not an import visible in sc, the package
// declares no name, or the name is not visible from this package.
func (r *pkgResolver) qualified(id *ast.Ident, sc *scope) (ast.Decl, []diag.Diagnostic) {
	if r.rejectedImport(id.Pkg, sc) {
		// The import's declaration was reported, or its package could not
		// be loaded; what it would have named follows from that.
		id.Def = ast.Unbound
		return nil, nil
	}
	decl, visible := sc.lookup(id.Pkg)
	if !visible {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("undefined: %s", id.Pkg),
			Span:     id.PkgSpan,
		}}
	}
	imp, ok := decl.(*ast.Import)
	if !ok {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is not a package", id.Pkg),
			Span:     id.PkgSpan,
		}}
	}
	r.used[imp] = true
	if target := r.packages[imp.Package]; target != nil {
		if member, ok := target.member(id.Name); ok {
			if diags := r.access(member, target.pkg, id); diags != nil {
				return nil, diags
			}
			return member, nil
		}
	}
	return nil, []diag.Diagnostic{undefined(id)}
}

// bindQualifiedValue binds the qualified name id, used as a value, to the
// global it names, marking the global read, or to the function it names,
// whose value it is.
func (r *pkgResolver) bindQualifiedValue(id *ast.Ident, sc *scope) []diag.Diagnostic {
	decl, diags := r.qualified(id, sc)
	switch d := decl.(type) {
	case nil:
		return diags
	case *ast.TypeDecl:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot use %s as a value; it is a type", id.Qualified()),
			Span:     id.Span,
		}}
	case *ast.Var:
		id.Def = d
		r.read[d] = true
		return nil
	case *ast.Func:
		// The function's value; whether it can be one is the type
		// checker's to say.
		id.Def = d
		return nil
	default:
		return []diag.Diagnostic{undefined(id)}
	}
}

// bindQualifiedType binds the qualified name id, written as a type, to the
// type it names.
func (r *pkgResolver) bindQualifiedType(id *ast.Ident, sc *scope) []diag.Diagnostic {
	decl, diags := r.qualified(id, sc)
	switch d := decl.(type) {
	case nil:
		return diags
	case *ast.TypeDecl:
		id.Def = d
		return nil
	default:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is not a type", id.Qualified()),
			Span:     id.Span,
		}}
	}
}

// bindQualifiedCallee binds the qualified name id, called, to the function
// it names.
func (r *pkgResolver) bindQualifiedCallee(id *ast.Ident, sc *scope) []diag.Diagnostic {
	decl, diags := r.qualified(id, sc)
	switch d := decl.(type) {
	case nil:
		return diags
	case *ast.Func:
		id.Def = d
		return nil
	case *ast.TypeDecl:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot call %s; it is a type", id.Qualified()),
			Span:     id.Span,
		}}
	default:
		return []diag.Diagnostic{undefined(id)}
	}
}

// bindQualifiedTarget binds the qualified name id, assigned, to the global
// it names; whether it may be assigned is the type checker's to say.
func (r *pkgResolver) bindQualifiedTarget(id *ast.Ident, sc *scope) []diag.Diagnostic {
	decl, diags := r.qualified(id, sc)
	switch d := decl.(type) {
	case nil:
		return diags
	case *ast.Var:
		id.Def = d
		return nil
	case *ast.TypeDecl:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot assign to %s; it is a type", id.Qualified()),
			Span:     id.Span,
		}}
	default:
		return []diag.Diagnostic{undefined(id)}
	}
}

// undefined reports id as naming nothing.
func undefined(id *ast.Ident) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("undefined: %s", id.Qualified()),
		Span:     id.Span,
	}
}

// notA reports id, an import's name, used where a what is wanted: a value,
// a type, a function or a variable.
func notA(id *ast.Ident, what string) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("%s is a package, not a %s", id.Name, what),
		Span:     id.Span,
	}
}

// redeclared reports a local or parameter named name, at span, that reuses
// the name of existing, visible where it is declared: an import's, which
// is reported as such, or any other's, as declared already.
func redeclared(name string, span ast.Span, existing ast.Decl) diag.Diagnostic {
	if _, ok := existing.(*ast.Import); ok {
		return diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is an imported package and cannot be redeclared", name),
			Span:     span,
		}
	}
	return alreadyDeclared(name, span, existing)
}

// isImport reports whether decl is an import.
func isImport(decl ast.Decl) bool {
	_, ok := decl.(*ast.Import)
	return ok
}

// useImport marks the import the qualified name id is qualified by used,
// if it names one visible in sc, without binding id.
func (r *pkgResolver) useImport(id *ast.Ident, sc *scope) {
	if decl, ok := sc.lookup(id.Pkg); ok {
		if imp, ok := decl.(*ast.Import); ok {
			r.used[imp] = true
		}
	}
}

// rejectedImport reports whether name is the name of an import of the file
// whose scope encloses sc, and that import's declaration was reported.
func (r *pkgResolver) rejectedImport(name string, sc *scope) bool {
	for s := sc; s != nil; s = s.parent {
		if s.parent == r.globals {
			return s.rejected[name]
		}
	}
	return false
}

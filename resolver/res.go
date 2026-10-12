package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
)

const (
	mainPackage    = "main"
	builtinPackage = "builtin"
	mainFunc       = "main"
)

// Resolver binds names in one file. It is single-use: create one with New and
// call Resolve once.
type Resolver struct {
	file     *ast.File
	funcRefs map[string]*ast.Func
}

// New returns a Resolver for file.
func New(file *ast.File) *Resolver {
	return &Resolver{file: file, funcRefs: make(map[string]*ast.Func)}
}

// Resolve checks the file's top-level shape and binds every name in it,
// returning every problem found. It panics if the file is nil, which is a
// compiler bug rather than bad input.
func (r *Resolver) Resolve() []diag.Diagnostic {
	if r.file == nil {
		panic("file is nil")
	}

	if r.file.Package != mainPackage {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "main package not found",
			Span:     ast.Span{Start: 0, End: 0, File: r.file.Span.File},
		}}
	}

	for _, fn := range builtin.Funcs() {
		r.funcRefs[r.refLabel(builtinPackage, fn.Name)] = nil
	}

	diags := r.declareFuncs()
	diags = append(diags, r.checkMain()...)

	// Resolution is per function body: a parameter is in scope inside the
	// function that declares it and nowhere else, which the file-wide Walk
	// this replaced could not express.
	for _, decl := range r.file.Decls {
		fn, ok := decl.(*ast.Func)
		if !ok {
			continue
		}

		scope, scopeDiags := r.paramScope(fn)
		diags = append(diags, scopeDiags...)

		for _, stmt := range fn.Body {
			diags = append(diags, r.resolveStmt(stmt, scope)...)
		}
	}

	return diags
}

// declareFuncs records every top-level function in the reference table,
// reporting a name declared twice or one that reuses a builtin's name.
func (r *Resolver) declareFuncs() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.file.Decls {
		if fn, ok := decl.(*ast.Func); ok {
			if existing, seen := r.seen(fn.Name); seen {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Message:  "duplicate function declaration",
					Span:     fn.NameSpan,
					Related: []diag.Diagnostic{{
						Severity: diag.Note,
						Message:  fmt.Sprintf("previous declaration of %s", fn.Name),
						Span:     existing.NameSpan,
					}},
				})
				continue
			}

			if _, seen := r.builtinSeen(fn.Name); seen {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Message:  "duplicate declaration of builtin function",
					Span:     fn.NameSpan,
				})
				continue
			}

			r.funcRefs[r.refLabel(r.file.Package, fn.Name)] = fn
		}
	}

	return diags
}

// checkMain reports a missing main function, or one with any other signature
// than taking nothing and returning nothing. Parameters and a result are
// reported separately, so a main with both gets both diagnostics.
func (r *Resolver) checkMain() []diag.Diagnostic {
	mainFn, mainDefined := r.funcRefs[r.refLabel(mainFunc, mainFunc)]
	if !mainDefined {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "main function not found",
			Span:     ast.Span{Start: 0, End: 0, File: r.file.Span.File},
		}}
	}
	if mainFn == nil {
		return nil
	}

	var diags []diag.Diagnostic
	if len(mainFn.Params) > 0 {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  "main function must not declare parameters",
			Span:     mainFn.Params[0].Spanned(),
		})
	}
	if mainFn.Result != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  "main function must not declare a result",
			Span:     mainFn.Result.Spanned(),
		})
	}
	return diags
}

// paramScope indexes a function's parameters by name, reporting any name
// declared twice.
func (r *Resolver) paramScope(fn *ast.Func) (map[string]*ast.Param, []diag.Diagnostic) {
	scope := make(map[string]*ast.Param, len(fn.Params))

	var diags []diag.Diagnostic
	for _, param := range fn.Params {
		if existing, dup := scope[param.Name]; dup {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  "duplicate parameter name",
				Span:     param.NameSpan,
				Related: []diag.Diagnostic{{
					Severity: diag.Note,
					Message:  fmt.Sprintf("previous declaration of %s", param.Name),
					Span:     existing.NameSpan,
				}},
			})
			continue
		}
		scope[param.Name] = param
	}

	return scope, diags
}

// resolveStmt binds every name a statement mentions.
func (r *Resolver) resolveStmt(stmt ast.Stmt, scope map[string]*ast.Param) []diag.Diagnostic {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return r.resolveExpr(s.X, scope)
	case *ast.Return:
		if s.Value == nil {
			return nil
		}
		return r.resolveExpr(s.Value, scope)
	case *ast.If:
		diags := r.resolveExpr(s.Cond, scope)
		for _, inner := range s.Then {
			diags = append(diags, r.resolveStmt(inner, scope)...)
		}
		for _, inner := range s.Else {
			diags = append(diags, r.resolveStmt(inner, scope)...)
		}
		return diags
	default:
		return nil
	}
}

// resolveExpr binds a call's callee to its declaration and an identifier to
// the parameter it names, recursing so that a call nested in an argument is
// bound like any other.
func (r *Resolver) resolveExpr(e ast.Expr, scope map[string]*ast.Param) []diag.Diagnostic {
	switch x := e.(type) {
	case *ast.Call:
		ident, ok := x.Fn.(*ast.Ident)
		if !ok {
			panic("expected ident")
		}

		var diags []diag.Diagnostic
		if def, seen := r.seen(ident.Name); seen {
			ident.Def = def
		} else if def, seen := r.builtinSeen(ident.Name); seen {
			ident.Def = def
		} else {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  fmt.Sprintf("undefined: %s", ident.Name),
				Span:     ident.Span,
			})
		}

		for _, arg := range x.Args {
			diags = append(diags, r.resolveExpr(arg, scope)...)
		}
		return diags

	case *ast.Binary:
		return append(r.resolveExpr(x.X, scope), r.resolveExpr(x.Y, scope)...)

	case *ast.Ident:
		param, inScope := scope[x.Name]
		if !inScope {
			return []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  fmt.Sprintf("undefined: %s", x.Name),
				Span:     x.Span,
			}}
		}
		x.Def = param
		return nil

	default:
		return nil
	}
}

func (r *Resolver) seen(ref string) (*ast.Func, bool) {
	fn, seen := r.funcRefs[r.refLabel(r.file.Package, ref)]
	return fn, seen
}

func (r *Resolver) builtinSeen(ref string) (*ast.Func, bool) {
	fn, seen := r.funcRefs[r.refLabel(builtinPackage, ref)]
	return fn, seen
}

func (r *Resolver) refLabel(pkg, ref string) string {
	return fmt.Sprintf("_%s.%s", pkg, ref)
}

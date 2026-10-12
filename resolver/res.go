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
	read     map[*ast.Var]bool // variables read at least once
}

// New returns a Resolver for file.
func New(file *ast.File) *Resolver {
	return &Resolver{
		file:     file,
		funcRefs: make(map[string]*ast.Func),
		read:     make(map[*ast.Var]bool),
	}
}

// scope holds the names one block declares, and links to the block that
// encloses it. A function's parameters are its outermost scope.
type scope struct {
	parent *scope
	names  map[string]ast.Decl // *ast.Param or *ast.Var
	vars   []*ast.Var          // this block's variables, in declaration order
}

// newScope returns an empty scope nested in parent, which is nil for a
// function's outermost scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, names: make(map[string]ast.Decl)}
}

// lookup finds name in this scope or any enclosing one.
func (s *scope) lookup(name string) (ast.Decl, bool) {
	for sc := s; sc != nil; sc = sc.parent {
		if decl, ok := sc.names[name]; ok {
			return decl, true
		}
	}
	return nil, false
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

		params, paramDiags := r.paramScope(fn)
		diags = append(diags, paramDiags...)
		diags = append(diags, r.resolveBlock(fn.Body, params)...)
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

// paramScope returns the outermost scope of a function, holding its
// parameters, and reports any parameter name declared twice.
func (r *Resolver) paramScope(fn *ast.Func) (*scope, []diag.Diagnostic) {
	params := newScope(nil)

	var diags []diag.Diagnostic
	for _, param := range fn.Params {
		if existing, dup := params.names[param.Name]; dup {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  "duplicate parameter name",
				Span:     param.NameSpan,
				Related: []diag.Diagnostic{{
					Severity: diag.Note,
					Message:  fmt.Sprintf("previous declaration of %s", param.Name),
					Span:     nameSpan(existing),
				}},
			})
			continue
		}
		params.names[param.Name] = param
	}

	return params, diags
}

// resolveBlock resolves a block's statements in a new scope nested in parent,
// then reports every variable the block declared and never read.
func (r *Resolver) resolveBlock(body []ast.Stmt, parent *scope) []diag.Diagnostic {
	block := newScope(parent)

	var diags []diag.Diagnostic
	for _, stmt := range body {
		diags = append(diags, r.resolveStmt(stmt, block)...)
	}

	for _, v := range block.vars {
		if !r.read[v] {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  fmt.Sprintf("declared and not used: %s", v.Name),
				Span:     v.NameSpan,
			})
		}
	}
	return diags
}

// declareVar adds a variable to the current scope. Its value is resolved
// first, so a variable cannot refer to itself. No variable may reuse a name
// already visible from here, whether a parameter or a variable of this block
// or an enclosing one.
func (r *Resolver) declareVar(v *ast.Var, sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	if v.Value != nil {
		diags = r.resolveExpr(v.Value, sc)
	}

	if existing, visible := sc.lookup(v.Name); visible {
		return append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is already declared", v.Name),
			Span:     v.NameSpan,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("previous declaration of %s", v.Name),
				Span:     nameSpan(existing),
			}},
		})
	}

	sc.names[v.Name] = v
	sc.vars = append(sc.vars, v)
	return diags
}

// resolveAssign binds an assignment's target to the variable it names and
// resolves its value. Assigning does not count as reading the variable.
func (r *Resolver) resolveAssign(a *ast.Assign, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(a.Value, sc)

	decl, visible := sc.lookup(a.Target.Name)
	if !visible {
		return append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("undefined: %s", a.Target.Name),
			Span:     a.Target.Span,
		})
	}
	a.Target.Def = decl
	return diags
}

// nameSpan returns the span of the name a parameter or variable declares.
func nameSpan(decl ast.Decl) ast.Span {
	switch d := decl.(type) {
	case *ast.Param:
		return d.NameSpan
	case *ast.Var:
		return d.NameSpan
	default:
		return decl.Spanned()
	}
}

// resolveStmt binds every name a statement mentions, and declares any
// variable it introduces in sc. Each branch of an if is a block of its own.
func (r *Resolver) resolveStmt(stmt ast.Stmt, sc *scope) []diag.Diagnostic {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return r.resolveExpr(s.X, sc)
	case *ast.Return:
		if s.Value == nil {
			return nil
		}
		return r.resolveExpr(s.Value, sc)
	case *ast.If:
		diags := r.resolveExpr(s.Cond, sc)
		diags = append(diags, r.resolveBlock(s.Then, sc)...)
		return append(diags, r.resolveBlock(s.Else, sc)...)
	case *ast.Var:
		return r.declareVar(s, sc)
	case *ast.Assign:
		return r.resolveAssign(s, sc)
	default:
		return nil
	}
}

// resolveExpr binds a call's callee to its declaration and an identifier to
// the parameter or variable it names, marking a variable as read, recursing so that a call nested in an argument is
// bound like any other.
func (r *Resolver) resolveExpr(e ast.Expr, sc *scope) []diag.Diagnostic {
	switch x := e.(type) {
	case *ast.Call:
		return r.resolveCall(x, sc)
	case *ast.Binary:
		return append(r.resolveExpr(x.X, sc), r.resolveExpr(x.Y, sc)...)
	case *ast.Ident:
		return r.resolveIdent(x, sc)
	default:
		return nil
	}
}

// resolveCall binds a call's callee to the function or builtin it names, and
// resolves its arguments.
func (r *Resolver) resolveCall(call *ast.Call, sc *scope) []diag.Diagnostic {
	ident, ok := call.Fn.(*ast.Ident)
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

	for _, arg := range call.Args {
		diags = append(diags, r.resolveExpr(arg, sc)...)
	}
	return diags
}

// resolveIdent binds a name used as a value to the parameter or variable it
// names, and marks a variable as read.
func (r *Resolver) resolveIdent(id *ast.Ident, sc *scope) []diag.Diagnostic {
	decl, visible := sc.lookup(id.Name)
	if !visible {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("undefined: %s", id.Name),
			Span:     id.Span,
		}}
	}
	id.Def = decl
	if v, ok := decl.(*ast.Var); ok {
		r.read[v] = true
	}
	return nil
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

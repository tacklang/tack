package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
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
	globals  *scope            // package-level variables, enclosing every function
}

// New returns a Resolver for file.
func New(file *ast.File) *Resolver {
	return &Resolver{
		file:     file,
		funcRefs: make(map[string]*ast.Func),
		read:     make(map[*ast.Var]bool),
		globals:  newScope(nil),
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

	for _, name := range builtin.Names() {
		r.funcRefs[r.refLabel(builtinPackage, name)] = nil
	}

	diags := r.declareFuncs()
	diags = append(diags, r.declareGlobals()...)
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

	return append(diags, r.unused(r.globals)...)
}

// declareGlobals adds every package-level variable to the global scope, which
// encloses every function, so a global can be used from any function whatever
// the order of declarations. Functions were declared first, so a global that
// shares a name with a function, another global, or a builtin is the one
// reported. Initializers are resolved too, so a name in one is bound and the
// type checker can say why it is not allowed there.
func (r *Resolver) declareGlobals() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.file.Decls {
		v, ok := decl.(*ast.Var)
		if !ok {
			continue
		}

		if v.TypeExpr != nil {
			diags = append(diags, r.resolveType(v.TypeExpr, r.globals)...)
		}
		if v.Value != nil {
			diags = append(diags, r.resolveExpr(v.Value, r.globals)...)
		}

		if reserved := reservedName(v.Name, v.NameSpan); reserved != nil {
			diags = append(diags, reserved...)
			continue
		}

		if _, isBuiltin := r.builtinSeen(v.Name); isBuiltin {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  fmt.Sprintf("%s is a builtin and cannot be redeclared", v.Name),
				Span:     v.NameSpan,
			})
			continue
		}

		existing, clash := r.globals.lookup(v.Name)
		if fn, isFunc := r.seen(v.Name); isFunc {
			existing, clash = fn, true
		}
		if clash {
			diags = append(diags, alreadyDeclared(v.Name, v.NameSpan, existing))
			continue
		}

		r.globals.names[v.Name] = v
		r.globals.vars = append(r.globals.vars, v)
	}
	return diags
}

// blankName reports _ used to name a declaration, which it cannot: it only
// discards a result in a binding of several.
func blankName(span ast.Span) diag.Diagnostic {
	return diag.Diagnostic{Severity: diag.Error, Message: "cannot use _ as a name", Span: span}
}

// reservedName reports, if name is not one a declaration may have, why not:
// _ only discards a result, and a type name is reserved like a keyword, for
// writing types and conversions. It returns nil for any other name.
func reservedName(name string, span ast.Span) []diag.Diagnostic {
	if name == ast.Blank {
		return []diag.Diagnostic{blankName(span)}
	}
	if _, isType := types.Lookup(name); isType {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot use %s as a name; it is a type", name),
			Span:     span,
		}}
	}
	return nil
}

// alreadyDeclared reports name, declared at span, as clashing with an
// earlier declaration, and points at that one.
func alreadyDeclared(name string, span ast.Span, existing ast.Decl) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("%s is already declared", name),
		Span:     span,
		Related: []diag.Diagnostic{{
			Severity: diag.Note,
			Message:  fmt.Sprintf("previous declaration of %s", name),
			Span:     nameSpan(existing),
		}},
	}
}

// declareFuncs records every top-level function in the reference table,
// reporting a name declared twice or one that reuses a builtin's name.
func (r *Resolver) declareFuncs() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.file.Decls {
		if fn, ok := decl.(*ast.Func); ok {
			if reserved := reservedName(fn.Name, fn.NameSpan); reserved != nil {
				diags = append(diags, reserved...)
				continue
			}
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
	if n := len(mainFn.Results); n > 0 {
		span := mainFn.Results[0].Spanned()
		span.End = mainFn.Results[n-1].Spanned().End
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  "main function must not declare a result",
			Span:     span,
		})
	}
	return diags
}

// paramScope returns the outermost scope of a function, holding its
// parameters, and reports any parameter name declared twice.
func (r *Resolver) paramScope(fn *ast.Func) (*scope, []diag.Diagnostic) {
	params := newScope(r.globals)

	// The types in a signature can name only package-level constants, so
	// they are resolved where no parameter is visible.
	var diags []diag.Diagnostic
	for _, param := range fn.Params {
		diags = append(diags, r.resolveType(param.TypeExpr, r.globals)...)
	}
	for _, result := range fn.Results {
		diags = append(diags, r.resolveType(result, r.globals)...)
	}
	for _, param := range fn.Params {
		if reserved := reservedName(param.Name, param.NameSpan); reserved != nil {
			diags = append(diags, reserved...)
			continue
		}
		if global, clash := r.globals.lookup(param.Name); clash {
			diags = append(diags, alreadyDeclared(param.Name, param.NameSpan, global))
			continue
		}
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
	return append(diags, r.unused(block)...)
}

// unused reports every variable a scope declared and never read.
func (r *Resolver) unused(sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, v := range sc.vars {
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
	if v.TypeExpr != nil {
		diags = r.resolveType(v.TypeExpr, sc)
	}
	if v.Value != nil {
		diags = append(diags, r.resolveExpr(v.Value, sc)...)
	}

	if reserved := reservedName(v.Name, v.NameSpan); reserved != nil {
		return append(diags, reserved...)
	}

	if existing, visible := sc.lookup(v.Name); visible {
		return append(diags, alreadyDeclared(v.Name, v.NameSpan, existing))
	}

	sc.names[v.Name] = v
	sc.vars = append(sc.vars, v)
	return diags
}

// declareMultiVar resolves the value of a declaration of several variables,
// then declares each of them in order, so none of them can be used in the
// call that gives them their values, and a name listed twice is reported as
// declared twice.
//
// A _ in the list declares nothing, and may not be mut. A list of nothing
// but _ binds nothing at all, which is reported: the call can be made on
// its own instead.
func (r *Resolver) declareMultiVar(m *ast.MultiVar, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(m.Value, sc)

	blanks := 0
	for _, v := range m.Vars {
		if v.Name != ast.Blank {
			diags = append(diags, r.declareVar(v, sc)...)
			continue
		}
		blanks++
		if v.Mutable {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  "cannot use mut on _",
				Span:     v.Span,
			})
		}
	}
	if blanks == len(m.Vars) {
		diags = append(diags, nothingBound(m.Span))
	}
	return diags
}

// resolveMultiAssign resolves the value of an assignment of several results,
// then binds each target other than _, which discards its result. A list of
// nothing but _ is reported, as in declareMultiVar.
func (r *Resolver) resolveMultiAssign(m *ast.MultiAssign, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(m.Value, sc)

	blanks := 0
	for _, target := range m.Targets {
		if ast.IsBlank(target) {
			blanks++
			continue
		}
		diags = append(diags, r.resolveTarget(target, sc)...)
	}
	if blanks == len(m.Targets) {
		diags = append(diags, nothingBound(m.Span))
	}
	return diags
}

// nothingBound reports a binding of several results whose every name is _.
func nothingBound(span ast.Span) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  "every name is _, so nothing is bound; make the call on its own",
		Span:     span,
	}
}

// resolveAssign binds an assignment's target to the variable it names and
// resolves its value. Assigning does not count as reading the variable.
func (r *Resolver) resolveAssign(a *ast.Assign, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(a.Value, sc)
	return append(diags, r.resolveTarget(a.Target, sc)...)
}

// resolveTarget binds the target of an assignment, increment or decrement:
// a variable, or an element of one, a[i], whose index is resolved like any
// value. Writing to a variable, or to an element of it, does not count as
// reading it.
func (r *Resolver) resolveTarget(target ast.Expr, sc *scope) []diag.Diagnostic {
	switch t := target.(type) {
	case *ast.Ident:
		return r.resolveTargetName(t, sc)
	case *ast.Index:
		diags := r.resolveTarget(t.X, sc)
		return append(diags, r.resolveExpr(t.Index, sc)...)
	default:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "cannot assign to this expression",
			Span:     target.Spanned(),
		}}
	}
}

// resolveTargetName binds the variable an assignment target names.
func (r *Resolver) resolveTargetName(target *ast.Ident, sc *scope) []diag.Diagnostic {
	if target.Name == ast.Blank {
		return []diag.Diagnostic{
			{Severity: diag.Error, Message: "cannot assign to _", Span: target.Span},
		}
	}
	if _, isType := types.Lookup(target.Name); isType {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot assign to %s; it is a type", target.Name),
			Span:     target.Span,
		}}
	}
	decl, visible := sc.lookup(target.Name)
	if !visible {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("undefined: %s", target.Name),
			Span:     target.Span,
		}}
	}
	target.Def = decl
	return nil
}

// nameSpan returns the span of the name a declaration introduces.
func nameSpan(decl ast.Decl) ast.Span {
	switch d := decl.(type) {
	case *ast.Func:
		return d.NameSpan
	case *ast.Param:
		return d.NameSpan
	case *ast.Var:
		return d.NameSpan
	default:
		return decl.Spanned()
	}
}

// resolveStmt binds every name a statement mentions, and declares any
// variable it introduces in sc. Each branch of an if, and a loop's body, is a
// block of its own.
func (r *Resolver) resolveStmt(stmt ast.Stmt, sc *scope) []diag.Diagnostic {
	switch s := stmt.(type) {
	case *ast.Return:
		var diags []diag.Diagnostic
		for _, v := range s.Values {
			diags = append(diags, r.resolveExpr(v, sc)...)
		}
		return diags
	case *ast.If:
		diags := r.resolveExpr(s.Cond, sc)
		diags = append(diags, r.resolveBlock(s.Then, sc)...)
		return append(diags, r.resolveBlock(s.Else, sc)...)
	case *ast.For:
		return r.resolveFor(s, sc)
	case *ast.ForRange:
		return r.resolveForRange(s, sc)
	default:
		return r.resolveSimpleStmt(stmt, sc)
	}
}

// resolveFor resolves a loop in a scope of its own, nested in sc, which holds
// any variable its init statement declares; the body is a block nested in
// that. Everything is resolved in source order, so the post statement comes
// before the body and cannot see what the body declares.
func (r *Resolver) resolveFor(loop *ast.For, sc *scope) []diag.Diagnostic {
	inner := newScope(sc)

	var diags []diag.Diagnostic
	if loop.Init != nil {
		diags = append(diags, r.resolveSimpleStmt(loop.Init, inner)...)
	}
	if loop.Cond != nil {
		diags = append(diags, r.resolveExpr(loop.Cond, inner)...)
	}
	if loop.Post != nil {
		diags = append(diags, r.resolveSimpleStmt(loop.Post, inner)...)
	}
	diags = append(diags, r.resolveBlock(loop.Body, inner)...)
	return append(diags, r.unused(inner)...)
}

// resolveForRange resolves a range loop. Its count is resolved where the
// loop is, before the loop variable exists, so it cannot refer to it. The
// variable, if any, is declared in a scope of the loop's own, with the body
// nested in that.
func (r *Resolver) resolveForRange(loop *ast.ForRange, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(loop.Count, sc)

	inner := newScope(sc)
	if loop.Var != nil {
		diags = append(diags, r.declareVar(loop.Var, inner)...)
	}
	diags = append(diags, r.resolveBlock(loop.Body, inner)...)
	return append(diags, r.unused(inner)...)
}

// resolveSimpleStmt resolves a statement that holds no other statement: a
// declaration, an assignment, an increment or decrement, or an expression.
func (r *Resolver) resolveSimpleStmt(stmt ast.Stmt, sc *scope) []diag.Diagnostic {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return r.resolveExpr(s.X, sc)
	case *ast.Var:
		return r.declareVar(s, sc)
	case *ast.Assign:
		return r.resolveAssign(s, sc)
	case *ast.IncDec:
		return r.resolveTarget(s.Target, sc)
	case *ast.MultiVar:
		return r.declareMultiVar(s, sc)
	case *ast.MultiAssign:
		return r.resolveMultiAssign(s, sc)
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
	case *ast.Unary:
		return r.resolveExpr(x.X, sc)
	case *ast.Ident:
		return r.resolveIdent(x, sc)
	case *ast.Index:
		return append(r.resolveExpr(x.X, sc), r.resolveExpr(x.Index, sc)...)
	case *ast.CompositeLit:
		var diags []diag.Diagnostic
		if x.TypeExpr != nil {
			diags = r.resolveType(x.TypeExpr, sc)
		}
		for _, e := range x.Elems {
			diags = append(diags, r.resolveExpr(e, sc)...)
		}
		return diags
	default:
		return nil
	}
}

// resolveType binds the names in a type as written: the length of an array
// type, which may name a constant variable, through every level of
// nesting. A type name itself needs no binding.
func (r *Resolver) resolveType(typ ast.Expr, sc *scope) []diag.Diagnostic {
	a, ok := typ.(*ast.ArrayType)
	if !ok {
		return nil
	}
	var diags []diag.Diagnostic
	if a.Len != nil {
		diags = r.resolveExpr(a.Len, sc)
	}
	return append(diags, r.resolveType(a.Elem, sc)...)
}

// resolveCall binds a call's callee to the function or builtin it names, or
// records the type a conversion converts to, and resolves its arguments.
func (r *Resolver) resolveCall(call *ast.Call, sc *scope) []diag.Diagnostic {
	ident, ok := call.Fn.(*ast.Ident)
	if !ok {
		panic("expected ident")
	}

	var diags []diag.Diagnostic
	if typ, isType := types.Lookup(ident.Name); isType {
		// A type name called like a function converts its argument. It
		// binds to no declaration; the type checker reads Conv instead.
		call.Conv = typ
	} else if def, seen := r.seen(ident.Name); seen {
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
	if id.Name == ast.Blank {
		return []diag.Diagnostic{
			{Severity: diag.Error, Message: "cannot use _ as a value", Span: id.Span},
		}
	}
	if _, isType := types.Lookup(id.Name); isType {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot use %s as a value; it is a type", id.Name),
			Span:     id.Span,
		}}
	}
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

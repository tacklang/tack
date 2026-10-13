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
	globals  *scope            // package-level variables and types, enclosing every function
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
	names  map[string]ast.Decl // *ast.Param, *ast.Var, or *ast.TypeDecl
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
	diags = append(diags, r.declareTypes()...)
	diags = append(diags, r.declareGlobals()...)
	diags = append(diags, r.resolveStructs()...)
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
// the order of declarations. Functions and types were declared first, so a
// global that shares a name with a function, a type, another global, or a
// builtin is the one reported. Initializers are resolved too, so a name in one is bound and the
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

		if clash := r.packageNameClash(v.Name, v.NameSpan); clash != nil {
			diags = append(diags, clash...)
			continue
		}

		r.globals.names[v.Name] = v
		r.globals.vars = append(r.globals.vars, v)
	}
	return diags
}

// declareTypes adds every type declaration to the global scope, so a type
// can be used anywhere in the file, before its declaration included, and
// shares one namespace with functions, package-level variables and
// builtins. Functions were declared first, so a type that shares a name
// with one is the one reported, as a global would be.
func (r *Resolver) declareTypes() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.file.Decls {
		td, ok := decl.(*ast.TypeDecl)
		if !ok {
			continue
		}
		if clash := r.packageNameClash(td.Name, td.NameSpan); clash != nil {
			diags = append(diags, clash...)
			continue
		}
		r.globals.names[td.Name] = td
	}
	return diags
}

// packageNameClash reports why a package-level declaration cannot be named
// name, written at span: the name is reserved, a builtin's, or already
// declared by a function or in the global scope. It returns nil when the
// name is free.
func (r *Resolver) packageNameClash(name string, span ast.Span) []diag.Diagnostic {
	if reserved := reservedName(name, span); reserved != nil {
		return reserved
	}
	if _, isBuiltin := r.builtinSeen(name); isBuiltin {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is a builtin and cannot be redeclared", name),
			Span:     span,
		}}
	}
	existing, clash := r.globals.lookup(name)
	if fn, isFunc := r.seen(name); isFunc {
		existing, clash = fn, true
	}
	if clash {
		return []diag.Diagnostic{alreadyDeclared(name, span, existing)}
	}
	return nil
}

// resolveStructs resolves the fields of every struct type declared: each
// field's type, which may name any type or package-level constant, and its
// name, which must not be reserved or be another field's. Field names are a
// namespace of their own, one per struct, so a field may share a name with
// anything outside it.
func (r *Resolver) resolveStructs() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.file.Decls {
		td, ok := decl.(*ast.TypeDecl)
		if !ok {
			continue
		}
		st, ok := td.TypeExpr.(*ast.StructType)
		if !ok {
			continue
		}
		diags = append(diags, r.resolveFields(st)...)
	}
	return diags
}

// resolveFields resolves the fields of one struct type.
func (r *Resolver) resolveFields(st *ast.StructType) []diag.Diagnostic {
	var diags []diag.Diagnostic
	seen := make(map[string]*ast.Field, len(st.Fields))
	for _, f := range st.Fields {
		diags = append(diags, r.resolveType(f.TypeExpr, r.globals)...)
		if reserved := reservedName(f.Name, f.NameSpan); reserved != nil {
			diags = append(diags, reserved...)
			continue
		}
		if prev, dup := seen[f.Name]; dup {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  "duplicate field name",
				Span:     f.NameSpan,
				Related: []diag.Diagnostic{{
					Severity: diag.Note,
					Message:  fmt.Sprintf("previous declaration of %s", f.Name),
					Span:     prev.NameSpan,
				}},
			})
			continue
		}
		seen[f.Name] = f
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
// value, or a field of one, p.x, whose name the type checker looks up.
// Writing to a variable, or to an element or field of it, does not count as
// reading it.
func (r *Resolver) resolveTarget(target ast.Expr, sc *scope) []diag.Diagnostic {
	switch t := target.(type) {
	case *ast.Ident:
		return r.resolveTargetName(t, sc)
	case *ast.Index:
		diags := r.resolveTarget(t.X, sc)
		return append(diags, r.resolveExpr(t.Index, sc)...)
	case *ast.Selector:
		return r.resolveTarget(t.X, sc)
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
	if _, isType := decl.(*ast.TypeDecl); isType {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot assign to %s; it is a type", target.Name),
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
	case *ast.TypeDecl:
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

// resolveForRange resolves a range loop. Its ranged expression is resolved
// where the loop is, before the loop's variables exist, so it cannot refer
// to them. The variables, if any, are declared in a scope of the loop's own,
// with the body nested in that.
func (r *Resolver) resolveForRange(loop *ast.ForRange, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(loop.X, sc)

	inner := newScope(sc)
	diags = append(diags, r.declareRangeVars(loop, inner)...)
	diags = append(diags, r.resolveBlock(loop.Body, inner)...)
	return append(diags, r.unused(inner)...)
}

// declareRangeVars declares a range loop's variables in sc, the index first.
// With two names, a _ declares nothing, and both being _ is reported, since
// the loop then binds nothing and needs no names. A lone name is declared
// like any variable, so a lone _ is reported as a name.
func (r *Resolver) declareRangeVars(loop *ast.ForRange, sc *scope) []diag.Diagnostic {
	if loop.Elem == nil {
		if loop.Var == nil {
			return nil
		}
		return r.declareVar(loop.Var, sc)
	}
	if loop.Var.Name == ast.Blank && loop.Elem.Name == ast.Blank {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "every name is _, so nothing is bound; drop the names and :=",
			Span: ast.Span{
				File:  loop.Var.Span.File,
				Start: loop.Var.Span.Start,
				End:   loop.Elem.Span.End,
			},
		}}
	}
	var diags []diag.Diagnostic
	for _, v := range []*ast.Var{loop.Var, loop.Elem} {
		if v.Name != ast.Blank {
			diags = append(diags, r.declareVar(v, sc)...)
		}
	}
	return diags
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
	case *ast.Selector:
		return r.resolveExpr(x.X, sc)
	case *ast.CompositeLit:
		return r.resolveLit(x, sc)
	case *ast.KeyValue:
		// A key names a field, which is not looked up among variables; the
		// type checker finds it in the literal's type.
		return r.resolveExpr(x.Value, sc)
	default:
		return nil
	}
}

// resolveLit binds the names in a composite literal: its type, if written,
// and its elements.
func (r *Resolver) resolveLit(lit *ast.CompositeLit, sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	if lit.TypeExpr != nil {
		diags = r.resolveType(lit.TypeExpr, sc)
	}
	for _, e := range lit.Elems {
		diags = append(diags, r.resolveExpr(e, sc)...)
	}
	return diags
}

// resolveType binds the names in a type as written: a type name to the
// struct type it names, and the length of an array type, which may name a
// constant variable, through every level of nesting. A builtin type's name
// needs no binding, and a name bound to nothing is left for the type
// checker to report as an unknown type. A name bound to anything other than
// a type is reported here.
func (r *Resolver) resolveType(typ ast.Expr, sc *scope) []diag.Diagnostic {
	switch t := typ.(type) {
	case *ast.Ident:
		return r.resolveTypeName(t, sc)
	case *ast.ArrayType:
		var diags []diag.Diagnostic
		if t.Len != nil {
			diags = r.resolveExpr(t.Len, sc)
		}
		return append(diags, r.resolveType(t.Elem, sc)...)
	default:
		return nil
	}
}

// resolveTypeName binds a name written as a type to the type declaration it
// names.
func (r *Resolver) resolveTypeName(id *ast.Ident, sc *scope) []diag.Diagnostic {
	if _, isBasic := types.Lookup(id.Name); isBasic {
		return nil
	}
	decl, visible := sc.lookup(id.Name)
	if !visible {
		return nil
	}
	if _, isType := decl.(*ast.TypeDecl); !isType {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is not a type", id.Name),
			Span:     id.Span,
		}}
	}
	id.Def = decl
	return nil
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
	} else if decl, ok := r.globals.lookup(ident.Name); ok && isTypeDecl(decl) {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot call %s; it is a type", ident.Name),
			Span:     ident.Span,
		})
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
	if isTypeDecl(decl) {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot use %s as a value; it is a type", id.Name),
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

// isTypeDecl reports whether decl declares a type.
func isTypeDecl(decl ast.Decl) bool {
	_, ok := decl.(*ast.TypeDecl)
	return ok
}

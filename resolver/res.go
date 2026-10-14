package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

const (
	mainPackage    = "main"
	builtinPackage = "builtin"
	mainFunc       = "main"
)

// Resolver binds names in a program. It is single-use: create one with New
// and call Resolve once.
type Resolver struct {
	prog    *ast.Program
	read    map[*ast.Var]bool // variables read at least once
	excused map[*ast.Var]bool // variables never reported unused: see excuse

	// packages holds each package's resolver once its package-level names
	// are declared, which is before any package importing it is resolved.
	packages map[*ast.Package]*pkgResolver
}

// New returns a Resolver for prog.
func New(prog *ast.Program) *Resolver {
	return &Resolver{
		prog:     prog,
		read:     make(map[*ast.Var]bool),
		excused:  make(map[*ast.Var]bool),
		packages: make(map[*ast.Package]*pkgResolver),
	}
}

// pkgResolver binds the names in one package of the program. It shares
// what it learns about variables with the Resolver it belongs to.
type pkgResolver struct {
	*Resolver

	pkg      *ast.Package
	funcRefs map[string]*ast.Func
	globals  *scope // package-level variables and types, enclosing every file's scope

	// files holds each file's scope, by file: its imports, enclosed by
	// the global scope and enclosing every function of the file.
	files map[source.FileID]*scope
	// used holds the imports a qualified name has used, and rejected
	// those whose declaration was reported, which are not reported unused.
	used     map[*ast.Import]bool
	rejected map[*ast.Import]bool
}

// newPkgResolver returns a pkgResolver for pkg, a package of r's program.
func (r *Resolver) newPkgResolver(pkg *ast.Package) *pkgResolver {
	return &pkgResolver{
		Resolver: r,
		pkg:      pkg,
		funcRefs: make(map[string]*ast.Func),
		globals:  newScope(nil),
		files:    make(map[source.FileID]*scope),
		used:     make(map[*ast.Import]bool),
		rejected: make(map[*ast.Import]bool),
	}
}

// scope holds the names one block declares, and links to the block that
// encloses it. A function's parameters are its outermost scope.
type scope struct {
	parent   *scope
	names    map[string]ast.Decl // *ast.Param, *ast.Var, or *ast.TypeDecl
	rejected map[string]bool     // names whose declaration here was reported
	vars     []*ast.Var          // this block's variables, in declaration order
}

// newScope returns an empty scope nested in parent, which is nil for a
// function's outermost scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, names: make(map[string]ast.Decl), rejected: make(map[string]bool)}
}

// reject records that a declaration of name in this scope was reported, so
// that a use of name from here on, which refers to it, is not reported
// again (see unrejected).
func (s *scope) reject(name string) {
	s.rejected[name] = true
}

// isRejected reports whether a declaration of name was reported in this
// scope or an enclosing one.
func (s *scope) isRejected(name string) bool {
	for sc := s; sc != nil; sc = sc.parent {
		if sc.rejected[name] {
			return true
		}
	}
	return false
}

// unrejected returns diags, the problems found with a use of name, unless
// a declaration of name visible here was reported: the use refers to that
// declaration, so what is wrong with it follows from what was reported.
func unrejected(diags []diag.Diagnostic, name string, sc *scope) []diag.Diagnostic {
	if len(diags) > 0 && sc.isRejected(name) {
		return nil
	}
	return diags
}

// excuse records that the variable v is not to be reported as unused,
// since its declaration, or a use of it, was reported already.
func (r *pkgResolver) excuse(v *ast.Var) {
	r.excused[v] = true
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

// Resolve checks the program's top-level shape and binds every name in it,
// returning every problem found. It panics if the program is nil or has no
// package, which is a compiler bug rather than bad input.
func (r *Resolver) Resolve() []diag.Diagnostic {
	if r.prog == nil || r.prog.Main() == nil {
		panic("program is nil or empty")
	}

	main := r.prog.Main()
	if main.Name != mainPackage {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "main package not found",
			Span:     ast.Span{Start: 0, End: 0, File: firstFile(main)},
		}}
	}

	var diags []diag.Diagnostic
	resolvers := make([]*pkgResolver, 0, len(r.prog.Packages))
	for _, pkg := range r.prog.Packages {
		pr := r.newPkgResolver(pkg)
		r.packages[pkg] = pr
		resolvers = append(resolvers, pr)
		diags = append(diags, pr.resolve()...)
	}

	// A global is reported unused only once every package has been
	// resolved, so a read from any of them counts. One that is pub or lib
	// is there for other packages, which may not read it, so it is never
	// reported.
	for _, pr := range resolvers {
		for _, v := range pr.globals.vars {
			if v.Vis != ast.Private {
				pr.excuse(v)
			}
		}
		diags = append(diags, pr.unused(pr.globals)...)
	}
	return diags
}

// firstFile returns the ID of the first file of pkg, where a diagnostic
// about the package as a whole points, or the zero ID for a package with
// no files.
func firstFile(pkg *ast.Package) source.FileID {
	if len(pkg.Files) == 0 {
		return ""
	}
	return pkg.Files[0].Span.File
}

// resolve declares every package-level name of the package, from every one
// of its files, and then binds every name in it, returning every problem
// found but the unused globals, which Resolve reports.
func (r *pkgResolver) resolve() []diag.Diagnostic {
	for _, name := range builtin.Names() {
		r.funcRefs[r.refLabel(builtinPackage, name)] = nil
	}

	diags := r.declareFuncs()
	diags = append(diags, r.checkBodies()...)
	diags = append(diags, r.declareTypes()...)
	globals, globalDiags := r.declareGlobals()
	diags = append(diags, globalDiags...)
	diags = append(diags, r.declareImports()...)
	diags = append(diags, r.resolveGlobals(globals)...)
	diags = append(diags, r.resolveStructs()...)
	diags = append(diags, r.checkVisibility()...)
	if r.pkg.Name == mainPackage {
		diags = append(diags, r.checkMain()...)
	} else {
		diags = append(diags, r.mainOutsideMain()...)
	}

	// Resolution is per function body: a parameter is in scope inside the
	// function that declares it and nowhere else, which the file-wide Walk
	// this replaced could not express.
	for _, decl := range r.pkg.Decls() {
		fn, ok := decl.(*ast.Func)
		if !ok {
			continue
		}

		params, paramDiags := r.paramScope(fn)
		diags = append(diags, paramDiags...)
		diags = append(diags, r.resolveBlock(fn.Body, params)...)
	}
	return append(diags, r.unusedImports()...)
}

// declareGlobals adds every package-level variable of every file of the
// package to the global scope, which encloses every file's scope, so a
// global can be used from any function whatever the order of declarations
// and of files. Functions and types were declared first, so a global that
// shares a name with a function, a type, another global, or a builtin is
// the one reported. It returns every global, declared or not, for
// resolveGlobals.
func (r *pkgResolver) declareGlobals() ([]*ast.Var, []diag.Diagnostic) {
	var diags []diag.Diagnostic
	var globals []*ast.Var
	for _, decl := range r.pkg.Decls() {
		v, ok := decl.(*ast.Var)
		if !ok {
			continue
		}
		globals = append(globals, v)
		if clash := r.packageNameClash(v.Name, v.NameSpan); clash != nil {
			diags = append(diags, clash...)
			r.globals.reject(v.Name)
			continue
		}

		r.globals.names[v.Name] = v
		r.globals.vars = append(r.globals.vars, v)
	}
	return globals, diags
}

// resolveGlobals resolves the type and initializer of each of globals in
// the scope of its file. Every global and every import is declared by
// then, so either may name a global declared after it, in the same file
// or another, or a name another package declares: the type checker then
// says whether the use is allowed there.
func (r *pkgResolver) resolveGlobals(globals []*ast.Var) []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, v := range globals {
		sc := r.fileScope(v.Span.File)
		if v.TypeExpr != nil {
			diags = append(diags, r.resolveType(v.TypeExpr, sc)...)
		}
		if v.Value != nil {
			diags = append(diags, r.resolveExpr(v.Value, sc)...)
		}
	}
	return diags
}

// declareTypes adds every type declaration to the global scope, so a type
// can be used anywhere in the file, before its declaration included, and
// shares one namespace with functions, package-level variables and
// builtins. Functions were declared first, so a type that shares a name
// with one is the one reported, as a global would be.
func (r *pkgResolver) declareTypes() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
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
func (r *pkgResolver) packageNameClash(name string, span ast.Span) []diag.Diagnostic {
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
func (r *pkgResolver) resolveStructs() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		td, ok := decl.(*ast.TypeDecl)
		if !ok {
			continue
		}
		st, ok := td.TypeExpr.(*ast.StructType)
		if !ok {
			continue
		}
		diags = append(diags, r.resolveFields(st, r.fileScope(td.Span.File))...)
	}
	return diags
}

// resolveFields resolves the fields of one struct type, in sc, the scope
// of the file declaring it.
func (r *pkgResolver) resolveFields(st *ast.StructType, sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	seen := make(map[string]*ast.Field, len(st.Fields))
	for _, f := range st.Fields {
		diags = append(diags, r.resolveType(f.TypeExpr, sc)...)
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

// declareFuncs records every top-level function of every file of the
// package in the reference table, reporting a name declared twice, in one
// file or in two, or one that reuses a builtin's name.
func (r *pkgResolver) declareFuncs() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		if fn, ok := decl.(*ast.Func); ok {
			if reserved := reservedName(fn.Name, fn.NameSpan); reserved != nil {
				diags = append(diags, reserved...)
				continue
			}
			if existing, seen := r.seen(fn.Name); seen {
				diags = append(diags, alreadyDeclared(fn.Name, fn.NameSpan, existing))
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

			r.funcRefs[r.refLabel(r.pkg.Name, fn.Name)] = fn
		}
	}

	return diags
}

// checkBodies reports every function of the package declared without a
// body that is not an extern func, whose body is in C: a function without
// a body has nothing to run.
func (r *pkgResolver) checkBodies() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		if fn, ok := decl.(*ast.Func); ok && fn.NoBody && !fn.Extern {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message: fmt.Sprintf(
					"function %s has no body; only an extern func is declared without one",
					fn.Name,
				),
				Span: fn.NameSpan,
			})
		}
	}
	return diags
}

// checkMain reports a missing main function, or one with any other signature
// than taking nothing and returning nothing. Parameters and a result are
// reported separately, so a main with both gets both diagnostics.
func (r *pkgResolver) checkMain() []diag.Diagnostic {
	mainFn, mainDefined := r.funcRefs[r.refLabel(mainFunc, mainFunc)]
	if !mainDefined {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "main function not found",
			Span:     ast.Span{Start: 0, End: 0, File: firstFile(r.pkg)},
		}}
	}
	if mainFn == nil {
		return nil
	}

	var diags []diag.Diagnostic
	if mainFn.Extern {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  "main function must not be extern",
			Span:     mainFn.ExternSpan,
		})
	}
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
func (r *pkgResolver) paramScope(fn *ast.Func) (*scope, []diag.Diagnostic) {
	file := r.fileScope(fn.Span.File)
	params := newScope(file)

	// The types in a signature can name only package-level constants, and
	// what the file imports, so they are resolved where no parameter is
	// visible.
	var diags []diag.Diagnostic
	for _, param := range fn.Params {
		diags = append(diags, r.resolveType(param.TypeExpr, file)...)
	}
	for _, result := range fn.Results {
		diags = append(diags, r.resolveType(result, file)...)
	}
	for _, param := range fn.Params {
		if clash := r.paramClash(param, params); clash != nil {
			diags = append(diags, clash...)
			params.reject(param.Name)
			continue
		}
		params.names[param.Name] = param
	}

	return params, diags
}

// paramClash reports why param cannot be declared in params, the scope of
// its function's parameters: its name is reserved, a package-level name's,
// or another parameter's. It returns nil when the name is free.
func (r *pkgResolver) paramClash(param *ast.Param, params *scope) []diag.Diagnostic {
	if reserved := reservedName(param.Name, param.NameSpan); reserved != nil {
		return reserved
	}
	if global, clash := params.parent.lookup(param.Name); clash {
		return []diag.Diagnostic{redeclared(param.Name, param.NameSpan, global)}
	}
	if existing, dup := params.names[param.Name]; dup {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "duplicate parameter name",
			Span:     param.NameSpan,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("previous declaration of %s", param.Name),
				Span:     nameSpan(existing),
			}},
		}}
	}
	return nil
}

// resolveBlock resolves a block's statements in a new scope nested in parent,
// then reports every variable the block declared and never read.
func (r *pkgResolver) resolveBlock(body []ast.Stmt, parent *scope) []diag.Diagnostic {
	block := newScope(parent)

	var diags []diag.Diagnostic
	for _, stmt := range body {
		diags = append(diags, r.resolveStmt(stmt, block)...)
	}
	return append(diags, r.unused(block)...)
}

// unused reports every variable a scope declared and never read, unless it
// is excused.
func (r *pkgResolver) unused(sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, v := range sc.vars {
		if !r.read[v] && !r.excused[v] {
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
func (r *pkgResolver) declareVar(v *ast.Var, sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	if v.TypeExpr != nil {
		diags = r.resolveType(v.TypeExpr, sc)
	}
	if v.Value != nil {
		diags = append(diags, r.resolveExpr(v.Value, sc)...)
	}

	if reserved := reservedName(v.Name, v.NameSpan); reserved != nil {
		sc.reject(v.Name)
		return append(diags, reserved...)
	}

	if existing, visible := sc.lookup(v.Name); visible {
		sc.reject(v.Name)
		return append(diags, redeclared(v.Name, v.NameSpan, existing))
	}

	if len(diags) > 0 {
		r.excuse(v)
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
func (r *pkgResolver) declareMultiVar(m *ast.MultiVar, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveMultiAssign(m *ast.MultiAssign, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveAssign(a *ast.Assign, sc *scope) []diag.Diagnostic {
	diags := r.resolveExpr(a.Value, sc)
	return append(diags, r.resolveTarget(a.Target, sc)...)
}

// resolveTarget binds the target of an assignment, increment or decrement:
// a variable, or an element of one, a[i], whose index is resolved like any
// value, or a field of one, p.x, whose name the type checker looks up.
// Writing to a variable, or to an element or field of it, does not count as
// reading it.
func (r *pkgResolver) resolveTarget(target ast.Expr, sc *scope) []diag.Diagnostic {
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

// resolveTargetName binds the variable an assignment target names. A name
// whose declaration was rejected is not reported again (unrejected).
func (r *pkgResolver) resolveTargetName(target *ast.Ident, sc *scope) []diag.Diagnostic {
	return unrejected(r.bindTargetName(target, sc), target.Qualified(), sc)
}

// bindTargetName does resolveTargetName's work, reporting every problem
// with the target.
func (r *pkgResolver) bindTargetName(target *ast.Ident, sc *scope) []diag.Diagnostic {
	if target.Pkg != "" {
		return r.bindQualifiedTarget(target, sc)
	}
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
	switch decl.(type) {
	case *ast.TypeDecl:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot assign to %s; it is a type", target.Name),
			Span:     target.Span,
		}}
	case *ast.Import:
		return []diag.Diagnostic{notA(target, "variable")}
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
	case *ast.Import:
		return d.NameSpan
	default:
		return decl.Spanned()
	}
}

// resolveStmt binds every name a statement mentions, and declares any
// variable it introduces in sc. Each branch of an if, each case of a
// switch, a loop's body, and what a defer defers, is a block of its own.
func (r *pkgResolver) resolveStmt(stmt ast.Stmt, sc *scope) []diag.Diagnostic {
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
	case *ast.Switch:
		return r.resolveSwitch(s, sc)
	case *ast.Defer:
		return r.resolveBlock(s.Body, sc)
	default:
		return r.resolveSimpleStmt(stmt, sc)
	}
}

// resolveSwitch resolves a switch: its tag and every case value where the
// switch is, and each case's body as a block of its own nested in sc, in
// source order, the default's included.
func (r *pkgResolver) resolveSwitch(s *ast.Switch, sc *scope) []diag.Diagnostic {
	var diags []diag.Diagnostic
	if s.Tag != nil {
		diags = r.resolveExpr(s.Tag, sc)
	}
	for _, cl := range s.Cases {
		for _, v := range cl.Values {
			diags = append(diags, r.resolveExpr(v, sc)...)
		}
		diags = append(diags, r.resolveBlock(cl.Body, sc)...)
	}
	return diags
}

// resolveFor resolves a loop in a scope of its own, nested in sc, which holds
// any variable its init statement declares; the body is a block nested in
// that. Everything is resolved in source order, so the post statement comes
// before the body and cannot see what the body declares.
func (r *pkgResolver) resolveFor(loop *ast.For, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveForRange(loop *ast.ForRange, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) declareRangeVars(loop *ast.ForRange, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveSimpleStmt(stmt ast.Stmt, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveExpr(e ast.Expr, sc *scope) []diag.Diagnostic {
	switch x := e.(type) {
	case *ast.Call:
		return r.resolveCall(x, sc)
	case *ast.Binary:
		return append(r.resolveExpr(x.X, sc), r.resolveExpr(x.Y, sc)...)
	case *ast.Unary, *ast.MutArg:
		return r.resolveExpr(operand(x), sc)
	case *ast.Ident:
		return r.resolveIdent(x, sc)
	case *ast.Index:
		return append(r.resolveExpr(x.X, sc), r.resolveExpr(x.Index, sc)...)
	case *ast.Selector:
		return r.resolveExpr(x.X, sc)
	case *ast.CompositeLit:
		return r.resolveLit(x, sc)
	case *ast.KeyValue:
		return r.resolveKeyValue(x, sc)
	default:
		return nil
	}
}

// operand returns the operand of e, a prefix operator or a mut argument.
func operand(e ast.Expr) ast.Expr {
	if m, ok := e.(*ast.MutArg); ok {
		return m.X
	}
	return e.(*ast.Unary).X
}

// resolveKeyValue resolves an element of a struct literal, key: value. A
// key names a field, which is not looked up among variables; the type
// checker finds it in the literal's type, and reports one written
// qualified. Its import counts as used, so that is the one error reported.
func (r *pkgResolver) resolveKeyValue(kv *ast.KeyValue, sc *scope) []diag.Diagnostic {
	if key, ok := kv.Key.(*ast.Ident); ok && key.Pkg != "" {
		r.useImport(key, sc)
	}
	return r.resolveExpr(kv.Value, sc)
}

// resolveLit binds the names in a composite literal: its type, if written,
// and its elements.
func (r *pkgResolver) resolveLit(lit *ast.CompositeLit, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveType(typ ast.Expr, sc *scope) []diag.Diagnostic {
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
func (r *pkgResolver) resolveTypeName(id *ast.Ident, sc *scope) []diag.Diagnostic {
	if id.Pkg != "" {
		return r.bindQualifiedType(id, sc)
	}
	if _, isBasic := types.Lookup(id.Name); isBasic {
		return nil
	}
	decl, visible := sc.lookup(id.Name)
	if !visible {
		return nil
	}
	if _, isImport := decl.(*ast.Import); isImport {
		return []diag.Diagnostic{notA(id, "type")}
	}
	if _, isType := decl.(*ast.TypeDecl); !isType {
		if v, isVar := decl.(*ast.Var); isVar {
			r.excuse(v)
		}
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
func (r *pkgResolver) resolveCall(call *ast.Call, sc *scope) []diag.Diagnostic {
	ident, ok := call.Fn.(*ast.Ident)
	if !ok {
		panic("expected ident")
	}

	diags := r.bindCallee(ident, call, sc)
	for _, arg := range call.Args {
		diags = append(diags, r.resolveExpr(arg, sc)...)
	}
	return diags
}

// bindCallee binds ident, the callee of call, to the function or builtin
// it names, or records the type a conversion converts to.
func (r *pkgResolver) bindCallee(ident *ast.Ident, call *ast.Call, sc *scope) []diag.Diagnostic {
	if ident.Pkg != "" {
		return r.bindQualifiedCallee(ident, sc)
	}
	if typ, isType := types.Lookup(ident.Name); isType {
		// A type name called like a function converts its argument. It
		// binds to no declaration; the type checker reads Conv instead.
		call.Conv = typ
		return nil
	}
	if def, seen := r.seen(ident.Name); seen {
		ident.Def = def
		return nil
	}
	if def, seen := r.builtinSeen(ident.Name); seen {
		ident.Def = def
		return nil
	}
	if decl, ok := sc.lookup(ident.Name); ok && isImport(decl) {
		return []diag.Diagnostic{notA(ident, "function")}
	}
	if decl, ok := r.globals.lookup(ident.Name); ok && isTypeDecl(decl) {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("cannot call %s; it is a type", ident.Name),
			Span:     ident.Span,
		}}
	}
	return []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  fmt.Sprintf("undefined: %s", ident.Name),
		Span:     ident.Span,
	}}
}

// resolveIdent binds a name used as a value to the parameter or variable it
// names, and marks a variable as read. A name whose declaration was
// rejected is not reported again (unrejected).
func (r *pkgResolver) resolveIdent(id *ast.Ident, sc *scope) []diag.Diagnostic {
	return unrejected(r.bindIdent(id, sc), id.Qualified(), sc)
}

// bindIdent does resolveIdent's work, reporting every problem with the use.
func (r *pkgResolver) bindIdent(id *ast.Ident, sc *scope) []diag.Diagnostic {
	if id.Pkg != "" {
		return r.bindQualifiedValue(id, sc)
	}
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
	if isImport(decl) {
		return []diag.Diagnostic{notA(id, "value")}
	}
	id.Def = decl
	if v, ok := decl.(*ast.Var); ok {
		r.read[v] = true
	}
	return nil
}

func (r *pkgResolver) seen(ref string) (*ast.Func, bool) {
	fn, seen := r.funcRefs[r.refLabel(r.pkg.Name, ref)]
	return fn, seen
}

func (r *pkgResolver) builtinSeen(ref string) (*ast.Func, bool) {
	fn, seen := r.funcRefs[r.refLabel(builtinPackage, ref)]
	return fn, seen
}

func (r *pkgResolver) refLabel(pkg, ref string) string {
	return fmt.Sprintf("_%s.%s", pkg, ref)
}

// isTypeDecl reports whether decl declares a type.
func isTypeDecl(decl ast.Decl) bool {
	_, ok := decl.(*ast.TypeDecl)
	return ok
}

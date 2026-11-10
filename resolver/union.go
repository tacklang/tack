package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// resolveUnions resolves the cases of every union type declared: each
// payload's type, which may name any type, and each case's name, which
// follows a field's rule: it is not reserved, and not another case's of
// the union, but may share a name with anything outside it, since case
// names are a namespace of their own, one per union.
func (r *pkgResolver) resolveUnions() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		td, ok := decl.(*ast.TypeDecl)
		if !ok || td.Union() == nil {
			continue
		}
		sc := r.fileScope(td.Span.File)
		seen := make(map[string]*ast.UnionCase)
		for _, c := range td.Union().Cases {
			if c.TypeExpr != nil {
				diags = append(diags, r.resolveType(c.TypeExpr, sc)...)
			}
			if reserved := reservedName(c.Name, c.NameSpan); reserved != nil {
				diags = append(diags, reserved...)
				continue
			}
			if prev, dup := seen[c.Name]; dup {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Message:  "duplicate case name",
					Span:     c.NameSpan,
					Related: []diag.Diagnostic{{
						Severity: diag.Note,
						Message:  fmt.Sprintf("previous declaration of %s", c.Name),
						Span:     prev.NameSpan,
					}},
				})
				continue
			}
			seen[c.Name] = c
		}
	}
	return diags
}

// bindTypeOperand binds x, the operand of a selector, when it names a
// type, plain or qualified: a union, as Expr in Expr.int(5) or ast.Expr
// in ast.Expr.eof, which name one of its cases (see ast.Construction), or
// any type, as Pair in Pair.first or lexer.Token in lexer.Token.Kind,
// which name a method of it as a value, or called (see ast.MethodExpr).
// It binds the name to the type's declaration, or a type parameter's,
// leaves a basic type's name, which nothing declares, bound to nothing,
// marks the import of a qualified one used, and reports whether x names
// a type, with any problem with the qualified name, a type another
// package cannot see among them. Whether the type has a case or a method
// of the name is the type checker's to say. Any other operand is
// resolved as a value by the caller.
func (r *pkgResolver) bindTypeOperand(x ast.Expr, sc *scope) (bool, []diag.Diagnostic) {
	id, ok := x.(*ast.Ident)
	if !ok {
		return false, nil
	}
	if id.Pkg == "" {
		if _, isBasic := types.Lookup(id.Name); isBasic {
			return true, nil
		}
		decl, visible := sc.lookup(id.Name)
		if visible && isTypeName(decl) {
			id.Def = decl
			return true, nil
		}
		return false, nil
	}
	if !r.qualifiedType(id, sc) {
		return false, nil
	}
	decl, diags := r.qualified(id, sc)
	if td, isType := decl.(*ast.TypeDecl); isType {
		id.Def = td
	}
	return true, r.settle(id, diags, sc)
}

// qualifiedType reports whether the qualified name id names a type of the
// package its import stands for, whether or not this package may use it,
// without reporting anything or marking the import used.
func (r *pkgResolver) qualifiedType(id *ast.Ident, sc *scope) bool {
	decl, visible := sc.lookup(id.Pkg)
	imp, isImport := decl.(*ast.Import)
	if !visible || !isImport {
		return false
	}
	target := r.packages[imp.Package]
	if target == nil {
		return false
	}
	member, ok := target.member(id.Name)
	_, isType := member.(*ast.TypeDecl)
	return ok && isType
}

// Patterns. A value of a case of an expression switch, written as a name,
// eof, or a name and one name in parentheses, int(n), may be a value, as
// in any switch, or a pattern naming a case of a union, when the switch's
// tag is one (see ast.CasePattern), which only the type checker learns.
// What it is to the resolver follows from no name being declared twice:
// a pattern's binding, n, must be a name visible nowhere in the switch,
// which a value's argument must be, and a pattern's case name is found in
// the union, not where the switch is, while a value's callee must name a
// function, a builtin or a type to convert to. So:
//
//   - a name alone that is a variable or a parameter visible there is
//     resolved as a value, and any other is left as a pattern;
//   - in a call written with a name, f(...), the callee is bound to what
//     it names, if anything, without reporting it when it names nothing
//     a call can call, and its argument, when it is one name that is no
//     variable or parameter visible there, is left as a pattern's
//     binding, and is otherwise resolved as a value, as every argument of
//     any other call there is.
//
// A name left as a pattern's is bound to the type or import it names, if
// any, and to nothing otherwise, and nothing is reported. The type
// checker then reports, if the tag turns out not to be a union, what
// resolving it as a value would have: an undefined name, or a type,
// package or function where a value is wanted; and if it is one, a
// binding that is a name visible at the switch as already declared.
//
// A clause of one value whose binding is a name visible nowhere has a
// variable of its own under that name, its CaseClause's Var, declared at
// the start of its body; in a clause of several values a binding declares
// nothing, and its name is rejected in the body, so a use of it is not
// reported as well as the binding (see the type checker).

// casePatterns resolves the values of cl, a clause of an expression
// switch nested in sc, as values or patterns (see Patterns), and returns
// the variable the clause's one pattern binds, or nil, and the names
// the body must not report as undefined, since a binding of them was
// reported or will be.
func (r *pkgResolver) casePatterns(
	cl *ast.CaseClause,
	sc *scope,
) (*ast.Var, []string, []diag.Diagnostic) {
	var (
		bound    *ast.Var
		rejected []string
		diags    []diag.Diagnostic
	)
	for _, v := range cl.Values {
		bind, isPattern, valueDiags := r.patternValue(v, sc)
		diags = append(diags, valueDiags...)
		if !isPattern || bind == nil || bind.Name == ast.Blank ||
			reservedName(bind.Name, bind.Span) != nil {
			continue
		}
		if _, visible := sc.lookup(bind.Name); visible || len(cl.Values) > 1 {
			rejected = append(rejected, bind.Name)
			continue
		}
		bound = &ast.Var{Name: bind.Name, NameSpan: bind.Span, Span: bind.Span}
	}
	return bound, rejected, diags
}

// patternValue resolves v, a value of a case of an expression switch, in
// sc, as Patterns describes, and returns the name a pattern of it binds,
// nil for none, whether any of it was left as a pattern, and what
// resolving the rest as values reported.
func (r *pkgResolver) patternValue(v ast.Expr, sc *scope) (*ast.Ident, bool, []diag.Diagnostic) {
	switch x := v.(type) {
	case *ast.Ident:
		if x.Pkg != "" || x.Name == ast.Blank || isValueName(x, sc) {
			return nil, false, r.resolveExpr(v, sc)
		}
		r.bindPatternName(x, sc)
		return nil, true, nil
	case *ast.Call:
		return r.callPattern(x, sc)
	default:
		return nil, false, r.resolveExpr(v, sc)
	}
}

// callPattern is patternValue for call.
func (r *pkgResolver) callPattern(call *ast.Call, sc *scope) (*ast.Ident, bool, []diag.Diagnostic) {
	fn, isName := call.Fn.(*ast.Ident)
	if !isName || fn.Pkg != "" || call.Spread {
		return nil, false, r.resolveExpr(call, sc)
	}
	r.bindPatternCallee(fn, call, sc)
	bind, isBind := soleName(call)
	if isBind && !isValueName(bind, sc) {
		r.bindPatternName(bind, sc)
		return bind, true, nil
	}
	var diags []diag.Diagnostic
	for _, arg := range call.Args {
		diags = append(diags, r.resolveExpr(arg, sc)...)
	}
	return nil, !r.callable(fn, call), diags
}

// soleName returns the argument of call when it has one, a name that is
// not qualified.
func soleName(call *ast.Call) (*ast.Ident, bool) {
	if len(call.Args) != 1 {
		return nil, false
	}
	id, ok := call.Args[0].(*ast.Ident)
	return id, ok && id.Pkg == ""
}

// callable reports whether fn, the callee of call, names what a call can
// call: a function, a builtin, or a type a conversion converts to.
func (r *pkgResolver) callable(fn *ast.Ident, call *ast.Call) bool {
	_, isFunc := fn.Def.(*ast.Func)
	return isFunc || call.Conv != types.Invalid
}

// isValueName reports whether id, a name, is visible in sc as a variable
// or a parameter, a value a case could compare its tag with.
func isValueName(id *ast.Ident, sc *scope) bool {
	if id == nil {
		return false
	}
	decl, visible := sc.lookup(id.Name)
	switch decl.(type) {
	case *ast.Var, *ast.Param:
		return visible
	default:
		return false
	}
}

// bindPatternName binds id, a name of a pattern, to the type or import it
// names where it is visible in sc, if it names one, and to nothing
// otherwise: a variable or a parameter would have made the value no
// pattern.
func (r *pkgResolver) bindPatternName(id *ast.Ident, sc *scope) {
	if decl, visible := sc.lookup(id.Name); visible {
		id.Def = decl
	}
}

// bindPatternCallee binds id, the case name of a pattern written as a
// call, call, to the function or builtin it would call, or records the
// type a conversion would convert to, or binds it to the type or import
// it names, reporting nothing (see bindPattern).
func (r *pkgResolver) bindPatternCallee(id *ast.Ident, call *ast.Call, sc *scope) {
	if typ, isType := types.Lookup(id.Name); isType {
		call.Conv = typ
		return
	}
	if def, seen := r.seen(id.Name); seen {
		id.Def = def
		return
	}
	if def, seen := r.builtinSeen(id.Name); seen {
		id.Def = def
		return
	}
	r.bindPatternName(id, sc)
}

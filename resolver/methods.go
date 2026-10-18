package resolver

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// declareMethods records every method of the package on the struct or
// union type its receiver has, in TypeDecl.Methods, once every receiver's
// type is bound. A method's name is in its type's namespace, not the
// package's: it may share one with a function, a global or a builtin, but
// not with a field of its struct, a case of its union or another method
// of its type, and it follows a field's naming rule, so neither _ nor a
// type's name. Its receiver must be a struct or union type declared in
// the package; any other type, a type of another package, an interface, a
// basic type, an array, a view or a list, is reported. An extern method
// is reported too: its body would be C's, which a method leaves to an
// extern func it calls; it is still recorded on its type, so that a call
// to it is not reported as a call to no method, and the type checker
// checks nothing more of it. A method whose receiver type is unknown has been,
// or will be, reported where the type is written.
func (r *pkgResolver) declareMethods() []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, decl := range r.pkg.Decls() {
		fn, ok := decl.(*ast.Func)
		if !ok || !fn.Method {
			continue
		}
		if fn.Extern {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  "a method cannot be extern; declare an extern func and call it from the method",
				Span:     fn.ExternSpan,
			})
		}
		td, recvDiags := r.receiverType(fn)
		diags = append(diags, recvDiags...)
		if td == nil {
			continue
		}
		if clash := methodClash(fn, td); clash != nil {
			diags = append(diags, clash...)
			continue
		}
		fn.RecvType = td
		td.Methods = append(td.Methods, fn)
	}
	return diags
}

// receiverType returns the declaration of the struct or union type fn's
// receiver has, when it is one declared in the package, and reports any
// other type written there. It returns nil, and nothing to report, for a
// type name bound to nothing, which the type checker reports as unknown,
// or to something that is not a type, which resolveType reported.
func (r *pkgResolver) receiverType(fn *ast.Func) (*ast.TypeDecl, []diag.Diagnostic) {
	recv := fn.Recv()
	if recv == nil {
		return nil, nil
	}
	if id, ok := recv.TypeExpr.(*ast.Ident); ok {
		td, isType := id.Def.(*ast.TypeDecl)
		switch {
		case isType && id.Pkg == "" && canHaveMethods(td):
			return td, nil
		case id.Def == nil && (id.Pkg != "" || !isBasicName(id.Name)):
			// Bound to nothing: reported where it was bound, or by the
			// type checker as an unknown type.
			return nil, nil
		}
	}
	return nil, []diag.Diagnostic{{
		Severity: diag.Error,
		Message: fmt.Sprintf(
			"cannot declare a method on %s: only a struct or union type declared in this package can have methods",
			typeText(recv.TypeExpr),
		),
		Span: recv.TypeExpr.Spanned(),
	}}
}

// methodClash reports why fn, a method of td, cannot be declared: its name
// is reserved, as a field's would be, or is already a field's of td, a
// struct, a case's of td, a union, or another method's of td. It returns
// nil when the name is free.
func methodClash(fn *ast.Func, td *ast.TypeDecl) []diag.Diagnostic {
	if reserved := reservedName(fn.Name, fn.NameSpan); reserved != nil {
		return reserved
	}
	if st, ok := td.TypeExpr.(*ast.StructType); ok {
		for _, f := range st.Fields {
			if f.Name == fn.Name {
				return memberClash(fn, td, "a field", f.NameSpan)
			}
		}
	}
	if u := td.Union(); u != nil {
		if uc, _ := u.Case(fn.Name); uc != nil {
			return memberClash(fn, td, "a case", uc.NameSpan)
		}
	}
	if prev := td.Method(fn.Name); prev != nil {
		name := td.Name + "." + fn.Name
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is already declared", name),
			Span:     fn.NameSpan,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("previous declaration of %s", name),
				Span:     prev.NameSpan,
			}},
		}}
	}
	return nil
}

// memberClash reports fn, a method of td, named like a member of td
// declared at prev, what naming the member's kind, a field or a case.
func memberClash(fn *ast.Func, td *ast.TypeDecl, what string, prev ast.Span) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  fmt.Sprintf("%s is already declared as %s of %s", fn.Name, what, td.Name),
		Span:     fn.NameSpan,
		Related: []diag.Diagnostic{{
			Severity: diag.Note,
			Message:  fmt.Sprintf("previous declaration of %s", fn.Name),
			Span:     prev,
		}},
	}}
}

// typeText returns a type written as a receiver's, for a diagnostic: a
// name as written, qualified or not, and an array, a view, a list, a map
// or a function type by its kind.
func typeText(typ ast.Expr) string {
	switch t := typ.(type) {
	case *ast.Ident:
		return t.Qualified()
	case *ast.ArrayType:
		return "an array type"
	case *ast.ViewType:
		return "a view type"
	case *ast.ListType:
		return "a list type"
	case *ast.MapType:
		return "a map type"
	case *ast.FuncType:
		return "a function type"
	default:
		return "this type"
	}
}

// isBasicName reports whether name is a builtin type's: int64, string,
// error and any among them.
func isBasicName(name string) bool {
	_, ok := types.Lookup(name)
	return ok
}

// canHaveMethods reports whether td declares a type that can have
// methods: a struct type or a union type, not an interface.
func canHaveMethods(td *ast.TypeDecl) bool {
	switch td.TypeExpr.(type) {
	case *ast.StructType, *ast.UnionType:
		return true
	default:
		return false
	}
}

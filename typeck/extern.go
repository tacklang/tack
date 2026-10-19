package typeck

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/cname"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// stdPath is the import path of the standard library, whose packages, and
// only they, may give an extern func a C name starting tack_.
const stdPath = "std"

// checkExtern checks the declaration of fn, an extern func, whose body is
// written in C and so is not checked here: its C name must be one C can
// define, an identifier that is not a keyword; not main, which the
// generated C defines; and, outside the standard library, not start
// tack_, which the generated C's own names do. Every type can cross to C,
// and a mut parameter crosses as a pointer, except a list, or a struct or
// an array holding one, which crosses only by mut or as a view (see
// checkExternLists), and a map, which never does (see checkExternMaps);
// and no parameter is own (see checkExternOwn). A float32 crosses as C's
// float and a float64 as its double.
func (c *Checker) checkExtern(fn *ast.Func) {
	maps := c.checkExternMaps(fn)
	if c.checkExternOwn(fn) && c.checkExternFuncs(fn) && maps {
		c.checkExternLists(fn)
		c.checkExternUnions(fn)
		c.checkExternOwnedElems(fn)
	}
	c.checkCName(fn)
}

// checkCName reports the C name of fn, an extern func, when C cannot
// declare it, or the generated C or std uses it (see checkExtern).
func (c *Checker) checkCName(fn *ast.Func) {
	at := fn.NameSpan
	if fn.CNameSpan != (ast.Span{}) {
		at = fn.CNameSpan
	}
	name := fn.CName
	switch {
	case !cname.IsIdent(name):
		c.errorf(at, "extern func %s cannot be named %s in C: it is not a C identifier",
			fn.Name, strconv.Quote(name))
	case cname.IsKeyword(name):
		c.errorf(at, "extern func %s cannot be named %s in C: it is a C keyword", fn.Name, name)
	case name == "main" && !c.externMain(fn):
		c.errorf(
			at,
			"extern func %s cannot be named main in C: the program's C main is the one that runs Tack's",
			fn.Name,
		)
	case strings.HasPrefix(name, "tack_") && !c.inStd(fn):
		c.errorf(
			at,
			"extern func %s cannot be named %s in C: names starting tack_ are reserved for the standard library",
			fn.Name,
			name,
		)
	}
}

// checkExternMaps reports each parameter and each result of fn, an extern
// func, whose type is or holds a map, at any depth, through a struct, an
// array, a union, a list or a view, and returns whether there was none:
// how a map is laid out for C is not settled, so none crosses.
func (c *Checker) checkExternMaps(fn *ast.Func) bool {
	clean := true
	for _, p := range fn.Params {
		if c.holdsMap(p.Type, make(map[types.Type]bool)) {
			c.errorf(
				p.TypeExpr.Spanned(),
				"parameter %s of extern func %s cannot be %s: a map does not cross to C",
				p.Name,
				fn.Name,
				c.typeName(p.Type),
			)
			clean = false
		}
	}
	for i, typ := range fn.ResultTypes {
		if c.holdsMap(typ, make(map[types.Type]bool)) {
			c.errorf(
				fn.Results[i].Spanned(),
				"extern func %s cannot return %s: a map does not cross to C",
				fn.Name,
				c.typeName(typ),
			)
			clean = false
		}
	}
	return clean
}

// holdsMap reports whether typ is a map type, or holds one, at any depth,
// through a struct, an array, a union, a list or a view. seen holds the
// structs and unions already being looked at.
func (c *Checker) holdsMap(typ types.Type, seen map[types.Type]bool) bool {
	switch {
	case c.table.IsMap(typ):
		return true
	case c.table.IsArray(typ), c.table.IsList(typ), c.table.IsView(typ):
		return c.holdsMap(c.table.Elem(typ), seen)
	case (c.table.IsStruct(typ) || c.table.IsUnion(typ)) && !seen[typ]:
		seen[typ] = true
		for _, part := range c.partTypes(typ) {
			if c.holdsMap(part, seen) {
				return true
			}
		}
	}
	return false
}

// checkExternUnions reports each parameter and each result of fn, an
// extern func, whose type is or holds a union, through a struct, an
// array, a view or a list: how a union is laid out for C is not settled,
// so none crosses yet.
func (c *Checker) checkExternUnions(fn *ast.Func) {
	for _, p := range fn.Params {
		c.externUnion(p.Type, p.TypeExpr.Spanned(),
			fmt.Sprintf("parameter %s of extern func %s cannot be", p.Name, fn.Name))
	}
	for i, typ := range fn.ResultTypes {
		c.externUnion(typ, fn.Results[i].Spanned(),
			fmt.Sprintf("extern func %s cannot return", fn.Name))
	}
}

// listCrossing is what an error about a list crossing to C by value says
// it may cross as instead.
const listCrossing = "a list crosses to C only as a mut parameter, or as a view"

// checkExternLists reports each plain parameter and each result of fn, an
// extern func, of an owned type, a list or a struct or array holding one.
// A list crosses to C as a mut parameter, through which C may grow it
// with std's macros, or as a view of its elements. Passing one by value
// would move it into C, and returning one would move one out, and who
// then frees it, and through which allocator, is not settled. A type
// holding a union is reported as one instead (see checkExternUnions).
func (c *Checker) checkExternLists(fn *ast.Func) {
	for _, p := range fn.Params {
		if !p.Mut && c.table.Owned(p.Type) && !c.crossesUnion(p.Type) {
			c.errorf(p.TypeExpr.Spanned(), "parameter %s of extern func %s cannot be %s: %s",
				p.Name, fn.Name, c.ownedTypeName(p.Type), listCrossing)
		}
	}
	for i, typ := range fn.ResultTypes {
		if c.table.Owned(typ) && !c.crossesUnion(typ) {
			c.errorf(fn.Results[i].Spanned(), "extern func %s cannot return %s: %s",
				fn.Name, c.ownedTypeName(typ), listCrossing)
		}
	}
}

// checkExternOwnedElems reports each parameter of fn, an extern func,
// that would cross to C, as a mut parameter or a view, a list or a view
// whose elements are owned, directly or through a struct or an array:
// how C would grow, take from and free such a list, and what std's macros
// would do with its elements, is not settled, so none crosses yet. A
// plain parameter of an owned type, and any type holding a union, is
// reported as one instead.
func (c *Checker) checkExternOwnedElems(fn *ast.Func) {
	for _, p := range fn.Params {
		if !p.Mut && c.table.Owned(p.Type) || c.crossesUnion(p.Type) ||
			!c.holdsOwnedElems(p.Type, make(map[types.Type]bool)) {
			continue
		}
		name := c.typeName(p.Type)
		if !c.table.IsList(p.Type) && !c.table.IsView(p.Type) {
			name += ", which holds one"
		}
		c.errorf(
			p.TypeExpr.Spanned(),
			"parameter %s of extern func %s cannot be %s: a list or a view whose elements hold lists cannot cross to C yet",
			p.Name,
			fn.Name,
			name,
		)
	}
}

// holdsOwnedElems reports whether typ is a list or a view whose element
// type is owned, or holds one, at any depth, through a struct, an array,
// a list or a view. seen holds the structs already being looked at.
func (c *Checker) holdsOwnedElems(typ types.Type, seen map[types.Type]bool) bool {
	switch {
	case c.table.IsView(typ), c.table.IsList(typ):
		elem := c.table.Elem(typ)
		return c.table.Owned(elem) || c.holdsOwnedElems(elem, seen)
	case c.table.IsArray(typ):
		return c.holdsOwnedElems(c.table.Elem(typ), seen)
	case c.table.IsStruct(typ) && typ != types.Error && !seen[typ]:
		seen[typ] = true
		for _, f := range c.table.Fields(typ) {
			if c.holdsOwnedElems(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

// ownedTypeName names typ, an owned type, as an error about it does: a
// list by its type, and anything holding one by its type and what it
// holds, "Stack, which holds a list".
func (c *Checker) ownedTypeName(typ types.Type) string {
	if c.table.IsList(typ) {
		return c.typeName(typ)
	}
	return c.typeName(typ) + ", which holds a list"
}

// inStd reports whether fn is declared in a package of the standard
// library.
func (c *Checker) inStd(fn *ast.Func) bool {
	pkg := c.declPkg[fn]
	return pkg != nil && (pkg.Path == stdPath || strings.HasPrefix(pkg.Path, stdPath+"/"))
}

// checkCNames reports every extern func of decls, the program's
// declarations, whose C name an earlier one has: one C symbol can have one
// definition. The error is at the later one, with a note at the first.
func (c *Checker) checkCNames(decls []ast.Decl) {
	first := make(map[string]*ast.Func)
	for _, decl := range decls {
		fn, ok := decl.(*ast.Func)
		if !ok || !fn.Extern {
			continue
		}
		prev, dup := first[fn.CName]
		if !dup {
			first[fn.CName] = fn
			continue
		}
		if c.declaredTwice(prev, fn) {
			continue
		}
		at := fn.NameSpan
		if fn.CNameSpan != (ast.Span{}) {
			at = fn.CNameSpan
		}
		c.errorNotes(at, []diag.Diagnostic{{
			Severity: diag.Note,
			Message:  fmt.Sprintf("extern func %s is declared here", c.funcName(prev)),
			Span:     prev.NameSpan,
		}}, "extern funcs %s and %s both have the C name %s", c.funcName(fn), c.funcName(prev), fn.CName)
	}
}

// declaredTwice reports whether fn and prev, two extern funcs of one C
// name, are two functions of one name in one package, which the resolver
// reported as declared twice, after it has run, which an earlier error
// says.
func (c *Checker) declaredTwice(prev, fn *ast.Func) bool {
	return c.prior > 0 && prev.Name == fn.Name && !prev.Method && !fn.Method &&
		c.declPkg[prev] == c.declPkg[fn]
}

// externMain reports whether fn is the main function of package main,
// declared extern, which the resolver reported, after it has run, which
// an earlier error says.
func (c *Checker) externMain(fn *ast.Func) bool {
	pkg := c.declPkg[fn]
	return c.prior > 0 && fn.Name == "main" && !fn.Method && pkg != nil && pkg.Name == "main"
}

// funcName names fn in a diagnostic about extern funcs that may be in
// several packages: qualified by its package's name when that is not the
// package being checked's, as typeName qualifies a type.
func (c *Checker) funcName(fn *ast.Func) string {
	if pkg := c.declPkg[fn]; pkg != nil && pkg != c.prog.Main() {
		return pkg.Name + "." + fn.Name
	}
	return fn.Name
}

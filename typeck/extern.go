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
// checkExternLists).
func (c *Checker) checkExtern(fn *ast.Func) {
	c.checkExternLists(fn)
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
	case name == "main":
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

// listCrossing is what an error about a list crossing to C by value says
// it may cross as instead.
const listCrossing = "a list crosses to C only as a mut parameter, or as a view"

// checkExternLists reports each plain parameter and each result of fn, an
// extern func, of an owned type, a list or a struct or array holding one.
// A list crosses to C as a mut parameter, through which C may grow it
// with std's macros, or as a view of its elements. Passing one by value
// would move it into C, and returning one would move one out, and who
// then frees it, and through which allocator, is not settled.
func (c *Checker) checkExternLists(fn *ast.Func) {
	for _, p := range fn.Params {
		if !p.Mut && c.table.Owned(p.Type) {
			c.errorf(p.TypeExpr.Spanned(), "parameter %s of extern func %s cannot be %s: %s",
				p.Name, fn.Name, c.ownedTypeName(p.Type), listCrossing)
		}
	}
	for i, typ := range fn.ResultTypes {
		if c.table.Owned(typ) {
			c.errorf(fn.Results[i].Spanned(), "extern func %s cannot return %s: %s",
				fn.Name, c.ownedTypeName(typ), listCrossing)
		}
	}
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

// funcName names fn in a diagnostic about extern funcs that may be in
// several packages: qualified by its package's name when that is not the
// package being checked's, as typeName qualifies a type.
func (c *Checker) funcName(fn *ast.Func) string {
	if pkg := c.declPkg[fn]; pkg != nil && pkg != c.prog.Main() {
		return pkg.Name + "." + fn.Name
	}
	return fn.Name
}

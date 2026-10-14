package typeck

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/cname"
	"github.com/tacklang/tack/diag"
)

// stdPath is the import path of the standard library, whose packages, and
// only they, may give an extern func a C name starting tack_.
const stdPath = "std"

// checkExtern checks the declaration of fn, an extern func, whose body is
// written in C and so is not checked here: its C name must be one C can
// define, an identifier that is not a keyword; not main, which the
// generated C defines; and, outside the standard library, not start
// tack_, which the generated C's own names do. Every type can cross to C,
// so its signature needs no check of its own.
func (c *Checker) checkExtern(fn *ast.Func) {
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

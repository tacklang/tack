package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/cname"
	"github.com/tacklang/tack/types"
)

// checkExtern checks the declaration of fn, an extern func, whose body is
// written in C and so is not checked here: its C name must be one C can
// define, not a keyword, and not main, which the generated C defines; and
// every type in its signature one that can cross to C, an integer or a
// bool, with at most one result.
func (c *Checker) checkExtern(fn *ast.Func) {
	switch {
	case cname.IsKeyword(fn.CName):
		c.errorf(fn.NameSpan, "extern func %s cannot be named %s in C: it is a C keyword",
			fn.Name, fn.CName)
	case fn.CName == "main":
		c.errorf(
			fn.NameSpan,
			"extern func %s cannot be named main in C: the program's C main is the one that runs Tack's",
			fn.Name,
		)
	}
	for _, p := range fn.Params {
		if !crossesToC(p.Type) {
			c.errorf(p.TypeExpr.Spanned(), "extern func %s cannot pass %s to C yet",
				fn.Name, c.typeName(p.Type))
		}
	}
	if len(fn.Results) > 1 {
		c.errorf(spanOf(fn.Results), "extern func %s cannot return several results from C yet",
			fn.Name)
		return
	}
	for i, r := range fn.Results {
		if typ := fn.ResultTypes[i]; !crossesToC(typ) {
			c.errorf(r.Spanned(), "extern func %s cannot return %s from C yet",
				fn.Name, c.typeName(typ))
		}
	}
}

// crossesToC reports whether a value of type typ can be passed to C or
// returned from it: an integer or a bool. An invalid type, already
// reported, is let through so it is not reported again.
func crossesToC(typ types.Type) bool {
	return typ == types.Invalid || typ == types.Bool || types.IsInteger(typ)
}

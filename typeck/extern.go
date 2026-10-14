package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/cname"
)

// checkExtern checks the declaration of fn, an extern func, whose body is
// written in C and so is not checked here: its C name must be one C can
// define, not a keyword, and not main, which the generated C defines.
// Every type can cross to C, so its signature needs no check of its own.
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
}

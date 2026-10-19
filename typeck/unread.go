package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// Unread errors and oks. An error, like an ok, cannot be dropped: binding
// it to _ is an error (see checkErrorBinding and checkOkDropped), and so
// is binding it to a variable nothing ever reads, which drops it as
// surely. The resolver finds which variables are never read; one a call
// or a binding of several names gave a value it leaves to the checker
// (see ast.Var.UnusedDeferred), which knows what it holds. One that is
// the error a call returns, err := f(), v, err := f(), err = f(), or the
// error append, reserve or put returns, or the ok a checked form gives,
// an index, a map read, a slice, a division, a remainder, a conversion, a
// type assertion, pop, remove or delete, is an error at its name. Any
// other is declared and not used, a warning, as the resolver reports the
// rest. Only a variable never read at all is reported: one assigned again
// before its first value was read is not, which only a flow analysis of
// the reads between assignments could tell.

// noteHolder records v, a variable a binding of value with n names gave
// its last value to, as holding what value gives there, an error or an
// ok, when the resolver left it to be reported (see reportUnread).
func (c *Checker) noteHolder(v ast.Decl, value ast.Expr, n int) {
	if v, ok := v.(*ast.Var); ok && v.UnusedDeferred {
		if what, holds := c.holderText(value, n); holds {
			c.holders[v] = what
		}
	}
}

// holderText says what the last of n names bound to value holds, when it
// holds an error or an ok: the error f returns, the error append returns,
// the ok pop returns or the ok l[i] gives.
func (c *Checker) holderText(value ast.Expr, n int) (string, bool) {
	if call, ok := value.(*ast.Call); ok {
		if n == 1 && returnsListError(call) {
			id, _ := call.Fn.(*ast.Ident)
			return "the error " + id.Name + " returns", true
		}
		if name, ok := c.errorCall(call, n); ok {
			return "the error " + name + " returns", true
		}
		if n == 2 && isTake(call) {
			id, _ := call.Fn.(*ast.Ident)
			return "the ok " + id.Name + " returns", true
		}
	}
	if n == 2 && c.isCheckedForm(value) {
		return "the ok " + c.text(value.Spanned()) + " gives", true
	}
	return "", false
}

// noteTargetHolder is noteHolder for target, the last target of an
// assignment of value with n names, when it is a variable's name.
func (c *Checker) noteTargetHolder(target ast.Expr, value ast.Expr, n int) {
	if id, ok := target.(*ast.Ident); ok {
		c.noteHolder(id.Def, value, n)
	}
}

// reportUnread reports each variable of body the resolver left to be
// reported (see the comment at the top of the file): an error when it
// holds an error or an ok, never checked, and declared and not used, a
// warning, otherwise, with the resolver's note at a call by its name that
// passed over it.
func (c *Checker) reportUnread(body []ast.Stmt) {
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			v, ok := n.(*ast.Var)
			if !ok || !v.UnusedDeferred {
				return true
			}
			if what := c.holders[v]; what != "" {
				c.errorf(v.NameSpan, "%s is never checked: %s is not read", what, v.Name)
				return true
			}
			c.warnUnusedVar(v)
			return true
		})
	}
}

// warnUnusedVar warns that v is declared and not used, as the resolver
// does, with a note at the call by its name that passed over it, if one
// did.
func (c *Checker) warnUnusedVar(v *ast.Var) {
	d := diag.Diagnostic{
		Severity: diag.Warning,
		Message:  fmt.Sprintf("declared and not used: %s", v.Name),
		Span:     v.NameSpan,
		Unused:   true,
	}
	if callee := v.HiddenCall; callee != nil {
		kind := "function"
		if fn, ok := callee.Def.(*ast.Func); ok && fn == nil {
			kind = "builtin"
		}
		d.Related = []diag.Diagnostic{{
			Severity: diag.Note,
			Message: fmt.Sprintf(
				"this call calls the %s %s, not the variable; rename one of them", kind, v.Name),
			Span: callee.Span,
		}}
	}
	c.warnings = append(c.warnings, d)
}

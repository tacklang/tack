package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// builtinPrototype is the C prototype of fn, a builtin extern func, under
// its C name: void, since no builtin returns a value, and its parameters'
// C types, unnamed. Its body is in the runtime, compiled and linked by the
// driver, so the generated C only declares it. exit is declared
// _Noreturn, as its body is: the type checker counts a call to it as
// leaving, so a C function with a result may end in one, which clang
// accepts with no -Wreturn-type warning only when it knows the call never
// returns.
func (g *generator) builtinPrototype(fn builtin.Func) string {
	params := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = g.cType(p)
	}
	if len(params) == 0 {
		params = []string{"void"}
	}
	noReturn := ""
	if fn.Name == "exit" {
		noReturn = "_Noreturn "
	}
	return fmt.Sprintf("%svoid %s(%s);", noReturn, fn.CName, strings.Join(params, ", "))
}

// builtinsTake reports whether any of fns has a parameter of type typ.
func builtinsTake(fns []builtin.Func, typ types.Type) bool {
	for _, fn := range fns {
		for _, p := range fn.Params {
			if p == typ {
				return true
			}
		}
	}
	return false
}

// builtinCall lowers a call to fn, a builtin extern func, as a call to its
// C function, each argument converted to its parameter's type.
func (g *generator) builtinCall(fn builtin.Func, call *ast.Call) (string, []diag.Diagnostic) {
	if len(call.Args) != len(fn.Params) {
		return "", []diag.Diagnostic{
			{
				Severity: diag.Error,
				Message: fmt.Sprintf(
					"%s takes %d argument(s), got %d",
					fn.Name,
					len(fn.Params),
					len(call.Args),
				),
				Span: call.Spanned(),
			},
		}
	}
	args := make([]string, len(call.Args))
	for i, a := range call.Args {
		expr, diags := g.expr(a, fn.Params[i])
		if len(diags) > 0 {
			return "", diags
		}
		args[i] = expr
	}
	return g.sequenced(call.Args, args, func(ops []string) string {
		return fmt.Sprintf("%s(%s)", fn.CName, strings.Join(ops, ", "))
	}), nil
}

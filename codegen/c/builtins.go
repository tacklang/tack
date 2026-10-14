package c

import (
	"embed"
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
)

// builtinFS holds the C body of each builtin extern func, one file per
// builtin, named after it: builtin/print.c defines tack_builtin_print. A
// file is its #include lines, then the definition, a static function under
// the builtin's C name.
//
//go:embed builtin/*.c
var builtinFS embed.FS

// cBody is a builtin's C body, split as the generated C uses it.
type cBody struct {
	def      string   // all but its #include lines: its definition, with no surrounding blank lines
	includes []string // its #include lines, which go in the preamble
}

// builtinBody returns the C body of fn, read from builtinFS.
func builtinBody(fn builtin.Func) (cBody, error) {
	data, err := builtinFS.ReadFile("builtin/" + fn.Name + ".c")
	if err != nil {
		return cBody{}, fmt.Errorf("the C backend has no body for builtin %s: %w", fn.Name, err)
	}
	var body cBody
	var rest []string
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(line, "#include ") {
			body.includes = append(body.includes, strings.TrimSpace(line))
			continue
		}
		rest = append(rest, line)
	}
	body.def = strings.TrimSpace(strings.Join(rest, ""))
	return body, nil
}

// isBuiltinCall returns the builtin extern func call calls, and whether it
// calls one: its callee is a builtin's name, which the resolver binds to a
// nil *ast.Func.
func isBuiltinCall(call *ast.Call) (builtin.Func, bool) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return builtin.Func{}, false
	}
	if fn, isFunc := id.Def.(*ast.Func); !isFunc || fn != nil {
		return builtin.Func{}, false
	}
	return builtin.Lookup(id.Name)
}

// usedBuiltins returns the C body of every builtin the program calls, in
// the order builtin.Funcs lists them, so the output is the same every
// time. A builtin with no body is an error at its first call: the backend
// cannot lower it.
func (g *generator) usedBuiltins() ([]cBody, []diag.Diagnostic) {
	first := make(map[string]*ast.Call)
	g.walk(func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok {
			if fn, ok := isBuiltinCall(call); ok && first[fn.Name] == nil {
				first[fn.Name] = call
			}
		}
		return true
	})
	var bodies []cBody
	for _, fn := range builtin.Funcs() {
		call := first[fn.Name]
		if call == nil {
			continue
		}
		body, err := builtinBody(fn)
		if err != nil {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error, Message: err.Error(), Span: call.Spanned(),
			}}
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

// builtinIncludes returns the #include lines bodies need, each once, in
// the order the bodies first name them.
func builtinIncludes(bodies []cBody) []string {
	var includes []string
	for _, b := range bodies {
		for _, inc := range b.includes {
			if !slices.Contains(includes, inc) {
				includes = append(includes, inc)
			}
		}
	}
	return includes
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

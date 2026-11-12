package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Closures and scope parameters. A scope parameter's value is a
// tack_scope, a pair of a function pointer, as a tack_fnptr, and an
// environment, a void *, which the function is called with first, before
// the parameters of its function type: a call through a scope parameter
// f of the function type typ is ((tack_sfn_ID)f.fn)(f.env, args), the
// pointer converted back to typ's scope pointer type, tack_sfn_ and typ's
// ID, before it is called, as a call through a plain value converts its
// pointer back to tack_fn_ID (see funcvalue.go).
//
// A closure lowers to a static C function of its own, tack_closureN, N
// counting the closures lowered so far, taking the environment and then
// its parameters, and returning what a function of its function type
// returns through a value: nothing, its one result, or its type's results
// struct, tack_fnres_ID. Its environment is a struct of its own,
// tack_envN, holding a pointer to each of its captures, in the order of
// ast.FuncLit.Captures, c0, c1 and so on: the variable's address in the
// frame of the function or closure it is written in, a pointer it already
// is, for a mut parameter or a binding referring to its value in place, or
// the pointer the enclosing closure's own environment holds, for a
// capture of a capture. A capture the closure never writes is a pointer
// to const. In the closure's body a capture is read and written through
// its pointer, (*tack_env->cN), so every run sees and makes the changes
// the caller sees. The value passed for a scope parameter is then
// (tack_scope){f, &(tack_envN){...}}: the environment is a compound
// literal on the caller's stack, which lives to the end of the block
// holding the call, longer than the call, and a closure never outlives
// that call (see typeck's closure.go), so no environment is read after
// its frame ends. A closure with no captures has a null environment, which
// its body never reads.
//
// A plain function value passed to a scope parameter is called through an
// adapter, tack_adapt_ID for its function type, which takes the
// environment, a pointer to the value, and calls the value with the rest
// of its parameters: (tack_scope){tack_adapt_ID, &(tack_fnptr){v}}, the
// value held in a compound literal as a closure's environment is.
//
// The closures a function body holds are lowered once the body is, each
// as a frame of its own, with its own defers, its own owned locals freed
// at each of its exits and its own own parameters, and the closures they
// hold in turn, so that each is lowered once for each body that holds it,
// a generic function's once for each of its instances, with the same type
// arguments bound. Every closure's environment struct and prototype is
// written after every function's prototype, and its definition before the
// first function's.

// scopeType is the C type of a scope parameter's value.
const scopeType = "tack_scope"

// scopeFnName is the C pointer type a call through a scope value of the
// function type typ converts the value's function to: tack_sfn_ and the
// ID of the type typ stands for.
func (g *generator) scopeFnName(typ types.Type) string {
	return fmt.Sprintf("tack_sfn_%d", g.prog.Types.Bound(typ))
}

// adapterName is the adapter a plain function value of the function type
// typ is called through as a scope value: tack_adapt_ and the ID of the
// type typ stands for.
func (g *generator) adapterName(typ types.Type) string {
	return fmt.Sprintf("tack_adapt_%d", g.prog.Types.Bound(typ))
}

// closure is a closure met while lowering a body, to be lowered once that
// body is: lit, its C name and its environment struct's name, the C type
// of each capture's pointer, in order, and typ, its function type as the
// type arguments bound where it was met make it; bound and reflect, the
// binding in force and the fields the fields loops around it stand for
// where it was met, which its body is lowered under; and, once its body
// is lowered (see generateClosure), sig, its C function's signature, and
// lines, that function's body.
type closure struct {
	lit     *ast.FuncLit
	name    string
	env     string
	ptrs    []string
	typ     types.Type
	bound   map[types.Type]types.Type
	reflect map[*ast.Var]reflectField
	sig     string
	lines   []string
}

// usesScope reports whether the program has a scope parameter, of a
// function or of a function type, which tack_scope is the C type of.
func (g *generator) usesScope() bool {
	if g.scopeUsed {
		return true
	}
	table := g.prog.Types
	for _, typ := range g.emittedTypes() {
		if !table.IsFunc(typ) {
			continue
		}
		for _, p := range table.FuncParams(typ) {
			if p.Scope && table.IsFunc(p.Type) {
				return true
			}
		}
	}
	return false
}

// scopeFunc reports whether p is a scope parameter of a function type,
// which is passed as a tack_scope; a scope Allocator is passed as any
// Allocator is.
func (g *generator) scopeFunc(p *ast.Param) bool {
	return p.Scope && g.prog.Types.IsFunc(p.Type)
}

// emitScopeType defines tack_scope, after tack_fnptr, when the program
// has a scope parameter.
func (g *generator) emitScopeType() {
	if g.usesScope() {
		g.buf.WriteString("typedef struct {\n    " + funcPtr + " fn;\n    void *env;\n} " +
			scopeType + ";\n\n")
	}
}

// scopeFnParams returns the C parameter list of the function a scope
// value of the function type typ calls: the environment, then typ's
// parameters, each as funcParamType gives it.
func (g *generator) scopeFnParams(typ types.Type) []string {
	params := []string{"void *"}
	for _, p := range g.prog.Types.FuncParams(typ) {
		params = append(params, g.funcParamType(p))
	}
	return params
}

// funcParamType returns the C type a parameter p of a function type is
// passed as: a scope value for a scope one of a function type, a pointer
// to its type's for a mut one, and its type's otherwise, a scope
// Allocator's among them.
func (g *generator) funcParamType(p types.FuncParam) string {
	switch {
	case p.Scope && g.prog.Types.IsFunc(p.Type):
		return scopeType
	case p.Mut:
		return g.cType(p.Type) + " *"
	default:
		return g.cType(p.Type)
	}
}

// emitScopeFn defines the scope pointer type of the function type typ,
// tack_sfn_ID, when a scope value of typ is called, made or adapted.
func (g *generator) emitScopeFn(typ types.Type) {
	if !g.scopeFns[typ] {
		return
	}
	fmt.Fprintf(&g.buf, "typedef %s (*%s)(%s);\n\n",
		g.funcTypeResult(typ), g.scopeFnName(typ), strings.Join(g.scopeFnParams(typ), ", "))
}

// noteScopeFn records that a scope value of the function type typ is
// called, made or adapted, so its scope pointer type is defined.
func (g *generator) noteScopeFn(typ types.Type) {
	if g.scopeFns == nil {
		g.scopeFns = make(map[types.Type]bool)
	}
	g.scopeFns[g.prog.Types.Bound(typ)] = true
}

// scopeArg renders arg, passed to a scope parameter of the function type
// typ: a closure as its function and environment (see closureValue), a
// scope parameter passed on as the value it holds, and any other value,
// a plain function value, through its function type's adapter, with a
// pointer to a copy of the value as its environment.
func (g *generator) scopeArg(arg ast.Expr, typ types.Type) (string, []diag.Diagnostic) {
	if lit, ok := arg.(*ast.FuncLit); ok {
		return g.closureValue(lit), nil
	}
	if id, ok := arg.(*ast.Ident); ok {
		if p, isParam := id.Def.(*ast.Param); isParam && g.scopeFunc(p) {
			return g.identExpr(id)
		}
	}
	v, diags := g.expr(arg, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	bound := g.prog.Types.Bound(typ)
	g.noteScopeFn(bound)
	if g.adapters == nil {
		g.adapters = make(map[types.Type]bool)
	}
	if !g.adapters[bound] {
		g.adapters[bound] = true
		g.adapterOrder = append(g.adapterOrder, bound)
	}
	return fmt.Sprintf("((%s){(%s)(%s){%s}, &(%s){%s}})",
		scopeType, funcPtr, g.scopeFnName(bound), g.adapterName(bound), funcPtr, v), nil
}

// closureValue renders lit, a closure passed to a scope parameter, as a
// tack_scope of its function, which it records to be lowered once the
// body being lowered is (see flushClosures), and its environment, a
// compound literal of pointers to its captures, or a null pointer when it
// has none.
func (g *generator) closureValue(lit *ast.FuncLit) string {
	n := len(g.closures)
	cl := &closure{
		lit:     lit,
		name:    fmt.Sprintf("tack_closure%d", n),
		env:     fmt.Sprintf("tack_env%d", n),
		typ:     g.prog.Types.Bound(lit.Type),
		bound:   g.prog.Types.Binding(),
		reflect: make(map[*ast.Var]reflectField, len(g.reflect)),
	}
	for v, r := range g.reflect {
		cl.reflect[v] = r
	}
	g.closures = append(g.closures, cl)
	g.pendingClosures = append(g.pendingClosures, cl)
	g.noteScopeFn(cl.typ)
	env := "0"
	if len(lit.Captures) > 0 {
		inits := make([]string, len(lit.Captures))
		for i, cp := range lit.Captures {
			cl.ptrs = append(cl.ptrs, g.capturePtrType(cp))
			inits[i] = g.captureAddr(cp.Decl)
		}
		env = fmt.Sprintf("&(%s){%s}", cl.env, strings.Join(inits, ", "))
	}
	return fmt.Sprintf("((%s){(%s)(%s){%s}, %s})",
		scopeType, funcPtr, g.scopeFnName(cl.typ), cl.name, env)
}

// capturePtrType returns the C type of the pointer a closure's
// environment holds for cp: a pointer to the C type of the variable's
// value, to const when the closure never writes it.
func (g *generator) capturePtrType(cp *ast.Capture) string {
	var ctype string
	switch d := cp.Decl.(type) {
	case *ast.Param:
		ctype = g.cType(d.Type)
		if g.scopeFunc(d) {
			ctype = scopeType
		}
	case *ast.Var:
		ctype = g.cType(d.Type)
	}
	if cp.Write {
		return ctype + " *"
	}
	return "const " + ctype + " *"
}

// captureAddr returns the address of the variable or parameter decl, as
// the frame being lowered reaches it: the pointer the environment of the
// closure being lowered holds for it, when it is one of that closure's
// captures; the pointer a mut parameter, or a variable referring to its
// value in place, already is; and the address of the variable or the
// parameter otherwise.
func (g *generator) captureAddr(decl ast.Decl) string {
	if i, ok := g.captured[decl]; ok {
		return fmt.Sprintf("tack_env->c%d", i)
	}
	switch d := decl.(type) {
	case *ast.Param:
		if d.Mut {
			return paramName(d.Name)
		}
		return "&" + paramName(d.Name)
	case *ast.Var:
		if g.inPlace[d] {
			return varName(d.Name)
		}
		return "&" + varName(d.Name)
	}
	return "0"
}

// captureRef returns how the body of the closure being lowered reads and
// writes decl, when it is one of its captures: through the pointer its
// environment holds, (*tack_env->cN).
func (g *generator) captureRef(decl ast.Decl) (string, bool) {
	i, ok := g.captured[decl]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("(*tack_env->c%d)", i), true
}

// flushClosures lowers every closure recorded since the last flush, and
// the closures their bodies hold in turn, each as a frame of its own (see
// generateClosure), with the type arguments bound that are bound now.
func (g *generator) flushClosures() []diag.Diagnostic {
	for len(g.pendingClosures) > 0 {
		cl := g.pendingClosures[0]
		g.pendingClosures = g.pendingClosures[1:]
		if diags := g.generateClosure(cl); len(diags) > 0 {
			return diags
		}
	}
	return nil
}

// generateClosure lowers the body of cl as the body of its C function,
// under the binding and the fields loops' fields in force where cl was
// met, with each capture read through the environment: a closure with
// captures first converts the environment it is passed to a pointer to
// its environment struct, tack_env, and one with none casts it to void,
// as it does each parameter its body never names (see unusedParams). It
// records the function's signature and lines, and what the body names
// (see noteNames).
func (g *generator) generateClosure(cl *closure) []diag.Diagnostic {
	fn := cl.lit.Func
	outer, outerReflect := g.captured, g.reflect
	g.captured = make(map[ast.Decl]int, len(cl.lit.Captures))
	for i, cp := range cl.lit.Captures {
		g.captured[cp.Decl] = i
	}
	g.reflect = cl.reflect
	restore := g.prog.Types.BindMore(cl.bound)
	defer func() {
		restore()
		g.captured, g.reflect = outer, outerReflect
	}()
	body, diags := g.funcFrame(fn)
	if len(diags) > 0 {
		return diags
	}
	head := []string{"(void)tack_envp;"}
	if len(cl.lit.Captures) > 0 {
		head = []string{fmt.Sprintf("%s *const tack_env = tack_envp;", cl.env)}
	}
	body = append(append(head, unusedParams(fn, body)...), body...)
	g.noteNames(cl.name, body)
	params := []string{"void *tack_envp"}
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s %s", g.paramType(p), paramName(p.Name)))
	}
	cl.sig = fmt.Sprintf("static %s %s(%s)", g.funcTypeResult(cl.typ), cl.name, strings.Join(params, ", "))
	cl.lines = body
	return nil
}

// emitClosureDecls writes, after every function's prototype, the
// environment struct of each closure that has captures and each
// closure's prototype, then the adapter of each function type whose
// plain values are passed to a scope parameter.
func (g *generator) emitClosureDecls() {
	for _, cl := range g.closures {
		if len(cl.ptrs) == 0 {
			continue
		}
		g.buf.WriteString("typedef struct {\n")
		for i, ptr := range cl.ptrs {
			fmt.Fprintf(&g.buf, "    %sc%d;\n", ptr, i)
		}
		fmt.Fprintf(&g.buf, "} %s;\n\n", cl.env)
	}
	for _, cl := range g.closures {
		g.buf.WriteString(g.maybeUnused(cl.name) + cl.sig + ";\n")
	}
	if len(g.closures) > 0 {
		g.buf.WriteString("\n")
	}
	for _, typ := range g.adapterOrder {
		g.emitAdapter(typ)
	}
}

// emitAdapter writes the adapter of the function type typ (see
// adapterName): it reads the plain function value its environment points
// to, converts it back to typ's pointer type, and calls it with the rest
// of its parameters, returning what it returns.
func (g *generator) emitAdapter(typ types.Type) {
	ps := g.prog.Types.FuncParams(typ)
	params := []string{"void *tack_envp"}
	args := make([]string, len(ps))
	for i, p := range ps {
		args[i] = fmt.Sprintf("p%d", i)
		params = append(params, g.funcParamType(p)+" "+args[i])
	}
	call := fmt.Sprintf("((%s)*(const %s *)tack_envp)(%s)",
		g.funcPtrName(typ), funcPtr, strings.Join(args, ", "))
	ret := "return "
	if len(g.prog.Types.FuncResults(typ)) == 0 {
		ret = ""
	}
	fmt.Fprintf(&g.buf, "static %s %s(%s) {\n    %s%s;\n}\n\n",
		g.funcTypeResult(typ), g.adapterName(typ), strings.Join(params, ", "), ret, call)
}

// emitClosures writes every closure's definition.
func (g *generator) emitClosures() {
	for _, cl := range g.closures {
		g.emitFunc(cl.sig, cl.lines)
	}
}

// scopeCallee returns the C callee of a call through rendered, the value
// of a scope parameter of the function type typ, and the environment it
// is called with first.
func (g *generator) scopeCallee(typ types.Type, rendered string) (string, string) {
	g.noteScopeFn(typ)
	return fmt.Sprintf("((%s)%s.fn)", g.scopeFnName(typ), rendered), rendered + ".env"
}

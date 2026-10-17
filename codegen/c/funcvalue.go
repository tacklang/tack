package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A function value lowers to a C function pointer. Every function type
// shares one C type, tack_fnptr, a pointer to a function taking and
// returning nothing, which a variable, a parameter, a field, an element
// or a payload holding one has: a struct may then hold a function value
// whose parameter is that struct, which C could not declare were the
// pointer's type the function's own, since the struct's typedef names an
// anonymous struct that cannot be declared ahead of it. A function's name
// used as a value is first a pointer of its function type's pointer type,
// tack_fn_ and the type's ID, a compound literal the function initializes,
// (tack_fn_3){f}, so C itself checks the function has exactly that type,
// since an initializer converts only a compatible function pointer
// implicitly, and only then converted to tack_fnptr. A call through a
// value converts it back to the pointer type of its function type before
// calling it. C allows a function pointer converted to another function
// pointer type and back, and the value is converted back to the very type
// it was converted from, since the checker has given it that function
// type, so the call is defined.
//
// A function of several results returns its own results struct (see
// emitResultStructs), which two functions of one signature do not share,
// so such a function used as a value is a thunk of its own, tack_value_
// and what follows tack_ in its C name, that calls it and copies its
// results into the struct of its function type, tack_fnres_ and the
// type's ID, which every call through a value of that type reads.

// funcPtr is the C type every function value has.
const funcPtr = "tack_fnptr"

// funcPtrName is the C pointer type of the function type typ, which a call
// through a value of it converts the value to.
func funcPtrName(typ types.Type) string {
	return fmt.Sprintf("tack_fn_%d", typ)
}

// funcResultsName is the C struct a function of the function type typ,
// with several results, returns them in, when called through a value.
func funcResultsName(typ types.Type) string {
	return fmt.Sprintf("tack_fnres_%d", typ)
}

// usesFuncTypes reports whether the program has a function type.
func (g *generator) usesFuncTypes() bool {
	table := g.prog.Types
	for i := range table.Count() {
		if table.IsFunc(types.Added(i)) {
			return true
		}
	}
	return false
}

// emitFuncPtr defines tack_fnptr, before any type that may hold one, when
// the program has a function type.
func (g *generator) emitFuncPtr() {
	if g.usesFuncTypes() {
		g.buf.WriteString("typedef void (*" + funcPtr + ")(void);\n\n")
	}
}

// emitFuncTypes defines, for every function type, after every other type,
// which its parameters and results may be, the results struct of one with
// several results and the pointer type a call through a value of it
// converts the value to.
func (g *generator) emitFuncTypes() {
	table := g.prog.Types
	for i := range table.Count() {
		typ := types.Added(i)
		if !table.IsFunc(typ) {
			continue
		}
		results := table.FuncResults(typ)
		if len(results) > 1 {
			g.buf.WriteString("typedef struct {\n")
			for j, r := range results {
				fmt.Fprintf(&g.buf, "    %s r%d;\n", g.cType(r), j)
			}
			fmt.Fprintf(&g.buf, "} %s;\n\n", funcResultsName(typ))
		}
		params := make([]string, len(table.FuncParams(typ)))
		for j, p := range table.FuncParams(typ) {
			params[j] = g.cType(p.Type)
			if p.Mut {
				params[j] += " *"
			}
		}
		list := strings.Join(params, ", ")
		if len(params) == 0 {
			list = "void"
		}
		fmt.Fprintf(
			&g.buf,
			"typedef %s (*%s)(%s);\n\n",
			g.funcTypeResult(typ),
			funcPtrName(typ),
			list,
		)
	}
}

// funcTypeResult returns the C type a function of the function type typ
// returns, called through a value: void, its one result's type, or its
// type's results struct.
func (g *generator) funcTypeResult(typ types.Type) string {
	results := g.prog.Types.FuncResults(typ)
	switch len(results) {
	case 0:
		return "void"
	case 1:
		return g.cType(results[0])
	default:
		return funcResultsName(typ)
	}
}

// funcValueRef renders fn, a function named as a value, as a tack_fnptr:
// the function itself, or for one with several results its thunk, which
// it records for emitThunks, as a pointer of its function type's pointer
// type first, which C checks it has.
func (g *generator) funcValueRef(fn *ast.Func) string {
	name := g.funcCName(fn)
	if len(fn.ResultTypes) > 1 {
		if !g.thunked[fn] {
			g.thunked[fn] = true
			g.thunks = append(g.thunks, fn)
		}
		name = thunkName(name)
	}
	return "((" + funcPtr + ")(" + funcPtrName(fn.Value) + "){" + name + "})"
}

// thunkName returns the name of the thunk of the function whose C name is
// cname (see funcValueRef): tack_value_ and what follows tack_ in cname,
// told apart from every other name as resultsOf's are.
func thunkName(cname string) string {
	return "tack_value_" + strings.TrimPrefix(cname, "tack_")
}

// emitThunks writes the thunk of every function of several results named
// as a value, after every function's prototype: it takes what the
// function takes, calls it, and returns its results in the results struct
// of its function type, member by member.
func (g *generator) emitThunks() {
	for _, fn := range g.thunks {
		cname := g.funcCName(fn)
		params := make([]string, len(fn.Params))
		args := make([]string, len(fn.Params))
		for i, p := range fn.Params {
			args[i] = fmt.Sprintf("p%d", i)
			params[i] = g.paramType(p) + " " + args[i]
		}
		list := strings.Join(params, ", ")
		if len(params) == 0 {
			list = "void"
		}
		results := funcResultsName(fn.Value)
		fmt.Fprintf(&g.buf, "static %s %s(%s) {\n", results, thunkName(cname), list)
		fmt.Fprintf(
			&g.buf,
			"    const %s r = %s(%s);\n",
			resultsOf(cname),
			cname,
			strings.Join(args, ", "),
		)
		fmt.Fprintf(&g.buf, "    %s out;\n", results)
		for i := range fn.ResultTypes {
			fmt.Fprintf(&g.buf, "    out.r%d = r.r%d;\n", i, i)
		}
		g.buf.WriteString("    return out;\n}\n\n")
	}
}

// dynamicCallee returns the C callee of a call through rendered, a
// function value of the function type typ, converted back to the pointer
// type of its function.
func dynamicCallee(typ types.Type, rendered string) string {
	return "((" + funcPtrName(typ) + ")" + rendered + ")"
}

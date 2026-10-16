package c

import (
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Generic functions. The type checker checks a generic function's body
// once and records every instance of it the program needs, a struct type
// for each type parameter, in ast.Program.Instances. Each instance is
// lowered as a function of its own: its body is generated once for it,
// with the type table binding each type parameter to its type argument
// (see types.Table.Bind), so every type the generator asks about, and
// every C type it writes, is the type argument's, and a call of an
// interface method on a value of a type parameter calls that struct
// type's method of the name directly. An instance's C name is its
// function's, with the type arguments written after it (see
// instanceCName), so each is defined once, whichever calls reach it.

// withInstance runs f with inst's type parameters bound to its type
// arguments, then unbinds them.
func (g *generator) withInstance(inst *ast.Instance, f func()) {
	bound := make(map[types.Type]types.Type, len(inst.TypeArgs))
	for i, tp := range inst.Func.TypeParams {
		bound[tp.Type] = inst.TypeArgs[i]
	}
	g.prog.Types.Bind(bound)
	defer g.prog.Types.Unbind()
	f()
}

// generateInstances generates the body of every instance of a generic
// function the program needs, in the order the type checker recorded
// them, and returns each one's signature and body.
func (g *generator) generateInstances() ([]string, [][]string, []diag.Diagnostic) {
	var (
		sigs   []string
		bodies [][]string
		diags  []diag.Diagnostic
	)
	for _, inst := range g.prog.Instances {
		g.withInstance(inst, func() {
			var body []string
			body, diags = g.generateFuncBody(inst.Func)
			sigs = append(sigs, g.signature(inst.Func))
			bodies = append(bodies, body)
		})
		if len(diags) > 0 {
			return nil, nil, diags
		}
	}
	return sigs, bodies, nil
}

// target returns the function a call to fn, made with the type arguments
// typeArgs, calls, and its C name: fn itself for a function or a method;
// for the stand-in of an interface method called on a value of a type
// parameter, the method of that name of the struct type the type
// parameter is bound to; and for a generic function, the instance of it
// for typeArgs, as the type parameters among them are bound.
func (g *generator) target(fn *ast.Func, typeArgs []types.Type) (*ast.Func, string) {
	if m := g.boundMethod(fn); m != nil {
		return m, g.funcCName(m)
	}
	if fn.Generic() {
		return fn, g.instanceCName(fn, typeArgs)
	}
	return fn, g.funcCName(fn)
}

// boundMethod returns, for fn, the stand-in of an interface method called
// on a value of a type parameter, the method of that name of the struct
// type the type parameter is bound to, which the checker has made sure
// it has. It returns nil for any other function.
func (g *generator) boundMethod(fn *ast.Func) *ast.Func {
	recv := fn.Recv()
	if fn.Interface == nil || recv == nil {
		return nil
	}
	td := g.structs[g.prog.Types.Bound(recv.Type)]
	if td == nil {
		return nil
	}
	return td.Method(fn.Name)
}

// instanceCName returns the C name of the instance of fn, a generic
// function, whose type arguments are args, each a type parameter bound to
// a struct type or a struct type: fn's name followed by the type
// arguments in brackets, separated by commas, each its package's import
// path, a period and its name, mangled as one name in fn's package, so
// render[main.File] for render with File of a program of one file. No
// name of a function, a type, a global or a method holds a bracket, so
// this is no other declaration's name, and since an import path holds no
// comma or bracket, and a type's name no period, the brackets can be read
// back into exactly fn's name and the arguments, each into its package's
// path and its name: two instances never share a C name.
func (g *generator) instanceCName(fn *ast.Func, args []types.Type) string {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = g.typeArgName(g.prog.Types.Bound(a))
	}
	return g.mangle(fn, instanceName(fn.Name, names))
}

// instanceName joins name, a generic function's, and its type arguments'
// names as instanceCName writes them: name[a,b].
func instanceName(name string, args []string) string {
	return name + "[" + strings.Join(args, ",") + "]"
}

// typeArgName writes typ, a struct type, as an instance's C name holds
// it: its package's import path, a period and its name.
func (g *generator) typeArgName(typ types.Type) string {
	td := g.structs[typ]
	if td == nil {
		return g.prog.Types.Name(typ)
	}
	path := g.prog.Main().Path
	if pkg := g.pkgOf[td]; pkg != nil {
		path = pkg.Path
	}
	return path + "." + td.Name
}

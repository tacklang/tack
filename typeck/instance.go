package typeck

import (
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// MaxInstances is the most instances of generic functions a program may
// need. Each instance is a function of its own in the generated code, so
// a few generic functions whose calls of one another permute several type
// arguments could otherwise need more than any compiler could produce in
// a reasonable time: n struct types and k type parameters allow n^k
// instances of one function. The limit is far beyond what a program
// written by hand needs.
const MaxInstances = 10000

// instance is an instance of a generic function being found: the
// function and its type arguments, struct types all.
type instance struct {
	fn   *ast.Func
	args []types.Type
}

// instances is the search for the instances a program needs: those found
// so far, in the order they were found, and each one's key.
type instances struct {
	c     *Checker
	seen  map[instanceKey]bool
	found []instance
}

// instantiate records on the program, in Program.Instances, and on every
// file, every instance of a generic function its code calls: those called
// from the body of a function that is not generic, a method's included,
// and, in turn, those called from the body of each instance found, with
// the instance's type arguments in place of its type parameters. A
// generic function no call reaches has no instance. Each instance is
// recorded once, in the order it is first found, however many calls need
// it. Since no type a program writes is generic, every type argument is a
// struct type the program declares, so the instances are finite; past
// MaxInstances the call that would add another is reported, and nothing
// more is looked for. Nothing is instantiated in a program with errors,
// which is not compiled.
func (c *Checker) instantiate() {
	if len(c.diags) > 0 {
		return
	}
	s := &instances{c: c, seen: make(map[instanceKey]bool)}
	for _, decl := range c.prog.Decls() {
		if fn, ok := decl.(*ast.Func); ok && !fn.Generic() && !s.walk(fn, nil) {
			return
		}
	}
	for i := 0; i < len(s.found); i++ {
		inst := s.found[i]
		bound := make(map[types.Type]types.Type, len(inst.args))
		for j, tp := range inst.fn.TypeParams {
			bound[tp.Type] = inst.args[j]
		}
		if !s.walk(inst.fn, bound) {
			return
		}
	}
	s.record()
}

// record records every instance found on the program and on each of its
// files.
func (s *instances) record() {
	prog := s.c.prog
	prog.Instances = make([]*ast.Instance, len(s.found))
	for i, inst := range s.found {
		prog.Instances[i] = &ast.Instance{Func: inst.fn, TypeArgs: inst.args}
	}
	for _, f := range prog.Files() {
		f.Instances = prog.Instances
	}
}

// walk adds the instance every call to a generic function in fn's body
// needs, at any depth, deferred code included, with bound, the type
// arguments fn's type parameters stand for in the instance being walked,
// nil for a function that is not generic. It stops, and returns false, as
// soon as one would be one too many.
func (s *instances) walk(fn *ast.Func, bound map[types.Type]types.Type) bool {
	ok := true
	for _, stmt := range fn.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if call, isCall := n.(*ast.Call); isCall && call.TypeArgs != nil && ok {
				ok = s.add(call, bound)
			}
			return ok
		})
	}
	return ok
}

// add records the instance call needs, its type arguments under bound,
// the type arguments of the instance whose body holds it, unless it was
// recorded already. It reports, and returns false, when there is no room
// for another.
func (s *instances) add(call *ast.Call, bound map[types.Type]types.Type) bool {
	fn := ast.Callee(call)
	args := make([]types.Type, len(call.TypeArgs))
	for i, a := range call.TypeArgs {
		if to, ok := bound[a]; ok {
			a = to
		}
		args[i] = a
	}
	key := keyOfInstance(fn, args)
	if s.seen[key] {
		return true
	}
	if len(s.found) == MaxInstances {
		s.c.errorf(call.Spanned(), "instantiating %s with %s makes more than %d instances "+
			"of generic functions, the most a program may have",
			fn.Name, s.c.typeNames(args), MaxInstances)
		return false
	}
	s.seen[key] = true
	s.found = append(s.found, instance{fn: fn, args: args})
	return true
}

// instanceKey is what an instance is found by: its function, and the IDs
// of its type arguments, written out.
type instanceKey struct {
	fn   *ast.Func
	args string
}

// keyOfInstance returns the key of the instance of fn with args.
func keyOfInstance(fn *ast.Func, args []types.Type) instanceKey {
	ids := make([]string, len(args))
	for i, a := range args {
		ids[i] = strconv.Itoa(int(a))
	}
	return instanceKey{fn: fn, args: strings.Join(ids, ",")}
}

// typeNames names each of typs, joined by commas, for a diagnostic.
func (c *Checker) typeNames(typs []types.Type) string {
	names := make([]string, len(typs))
	for i, t := range typs {
		names[i] = c.typeName(t)
	}
	return strings.Join(names, ", ")
}

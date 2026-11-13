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
// a reasonable time: n struct or union types and k type parameters allow
// n^k instances of one function. The limit is far beyond what a program
// written by hand needs.
const MaxInstances = 10000

// MaxUnrolled is the most bodies of fields loops a program's lowered code
// may hold, counted as each instance unrolls each loop, once for each
// field it lists, a loop nested in another once for each field of each:
// k loops nested over a struct of n fields are n^k bodies, which no
// instance limit bounds, so a few lines could otherwise make more code
// than any compiler could produce in a reasonable time. Like
// MaxInstances, it is far beyond what a program written by hand needs.
const MaxUnrolled = 10000

// instance is an instance of a generic function being found: the
// function and its type arguments, struct or union types all.
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

	// unrolled counts the fields loop bodies walked so far, each one the
	// backend lowers (see MaxUnrolled).
	unrolled int
}

// instantiate records on the program, in Program.Instances, and on every
// file, every instance of a generic function its code calls: those called
// from the body of a function that is not generic, a method's included,
// and, in turn, those called from the body of each instance found, with
// the instance's type arguments in place of its type parameters. A
// generic function no call reaches has no instance. Each instance is
// recorded once, in the order it is first found, however many calls need
// it. A fields loop's body is walked once for each field it lists, with
// its type parameter standing for the field's type, and the fields are
// recorded on the loop (see unroll). A type parameter is passed on only
// whole, never inside a type made of it (see notTypeArg), so every type
// argument is a type some code that is not generic writes, or a field's
// type of one, and the instances are finite;
// past MaxInstances the call that would add another is reported, and
// nothing more is looked for. Nothing is instantiated in a program with
// errors, which is not compiled.
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
	return s.walkStmts(fn, fn.Body, bound)
}

// walkStmts is walk for stmts, statements of fn's body. A fields loop
// among them, at any depth, has the fields it lists for the type it
// ranges over under bound recorded (see recordFields), and its body is
// walked once for each of them, with the loop's type parameter bound to
// the field's type, as the backend lowers it.
func (s *instances) walkStmts(
	fn *ast.Func,
	stmts []ast.Stmt,
	bound map[types.Type]types.Type,
) bool {
	ok := true
	for _, stmt := range stmts {
		ast.Walk(stmt, func(n ast.Node) bool {
			if !ok {
				return false
			}
			if loop, fo, isFields := ast.FieldsLoop(n); isFields {
				ok = s.unroll(fn, loop, fo, bound)
				return false
			}
			if sw, isSwitch := n.(*ast.Switch); isSwitch && sw.OnType {
				ok = s.walkArm(fn, sw, bound)
				return false
			}
			if call, isCall := n.(*ast.Call); isCall && call.TypeArgs != nil {
				ok = s.add(call, bound)
			}
			return ok
		})
	}
	return ok
}

// unroll records the fields loop, a fields loop of fn ranging over fo,
// lists for the type it ranges over under bound, and walks its body once
// for each, with fo.Field bound to the field's type, in a copy of bound.
// It reports, and returns false, when a body would be one too many for
// MaxUnrolled, or a call in the body one instance too many.
func (s *instances) unroll(
	fn *ast.Func,
	loop *ast.ForRange,
	fo *ast.FieldsOf,
	bound map[types.Type]types.Type,
) bool {
	typ := fo.Type
	if to, ok := bound[typ]; ok {
		typ = to
	}
	fields := s.c.recordFields(fn, fo, typ)
	all := s.c.table.Fields(typ)
	for _, i := range fields {
		if s.unrolled == MaxUnrolled {
			s.c.errorf(fo.Span, "unrolling the fields loop over %s makes more than %d bodies "+
				"of fields loops, the most a program may have",
				s.c.typeName(typ), MaxUnrolled)
			return false
		}
		s.unrolled++
		inner := make(map[types.Type]types.Type, len(bound)+1)
		for k, v := range bound {
			inner[k] = v
		}
		inner[fo.Field] = all[i].Type
		if !s.walkStmts(fn, loop.Body, inner) {
			return false
		}
	}
	return true
}

// walkArm walks the one case of sw, a switch on a type parameter in fn,
// that runs when its type parameter stands for what bound binds it to,
// with each type parameter that case declares bound to what it stands
// for then, in a copy of bound, as the backend lowers it; no other case
// is lowered, so no call in one needs an instance. One case is walked
// for each time the switch is, so a switch adds no bodies to what an
// instance or a fields loop holds, and needs no bound of its own beside
// MaxInstances and MaxUnrolled. A switch whose type parameter bound does
// not bind has no case walked; the backend reports it.
func (s *instances) walkArm(fn *ast.Func, sw *ast.Switch, bound map[types.Type]types.Type) bool {
	id, _ := sw.Tag.(*ast.Ident)
	var tp *ast.TypeParam
	if id != nil {
		tp, _ = id.Def.(*ast.TypeParam)
	}
	if tp == nil {
		return true
	}
	typ, ok := bound[tp.Type]
	if !ok {
		return true
	}
	arm, binds := sw.ArmFor(s.c.table, typ)
	if arm == nil {
		return true
	}
	inner := make(map[types.Type]types.Type, len(bound)+len(binds))
	for k, v := range bound {
		inner[k] = v
	}
	for k, v := range binds {
		inner[k] = v
	}
	return s.walkStmts(fn, arm.Body, inner)
}

// recordFields records on fo, a fields loop of fn, the fields it lists
// for typ, a type its T stands for, and returns them: those of typ's
// fields whose own mark lets the package declaring fn see them (see
// visibleFields), in declaration order, and none
// when typ is no struct.
func (c *Checker) recordFields(fn *ast.Func, fo *ast.FieldsOf, typ types.Type) []int {
	if fields, ok := fo.FieldsFor(typ); ok {
		return fields
	}
	fields := c.visibleFields(typ, c.declPkg[fn])
	fo.Record(typ, fields)
	return fields
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

package typeck

import "github.com/tacklang/tack/ast"

// Unused declarations. A private function or method, a private type, and
// a parameter that nothing uses is warned of, declared and not used, as
// an unused variable is by the resolver. Only a program with no error is
// looked at: what an error left unchecked, a bad statement or a call the
// checker could not resolve, may have used anything.
//
// A function is used when a name outside its own body names it, called
// or as a value. A method is used when a call outside its own body calls
// it, or a method expression outside it names it, Pair.first, or when an instance of a generic function binds its type to a type
// parameter whose constraint asks for a method of its name (see
// bindInstances): the instance calls it. A type is used when a name
// outside its own declaration and its methods names it, in a signature,
// a variable's type, a field of another type, a literal, a union built, a
// conversion or a constraint; a type argument's type is used too. Uses
// are not followed further: a function only an unused one calls is used.
//
// Exempt: anything pub or lib, which another package may use; func main
// of package main; and a parameter of a function whose signature is not
// its own to change: an extern func's, whose body is C, a pub or lib
// function's or method's, another package's to call, a function used as
// a value, a method by a method expression included, whose type the
// value's must match, and a method a constraint asks for, whose
// signature the interface's must match. A method's
// receiver is never warned of. The parameters of a function that is
// itself unused are not warned of apart from it.

// usage is what checkUnusedDecls finds is used: the functions, methods
// and types, the parameters read, the functions used as values, and the
// methods a constraint asks for at an instance.
type usage struct {
	used    map[ast.Decl]bool
	read    map[*ast.Param]bool
	value   map[*ast.Func]bool
	bound   map[*ast.Func]bool
	callees map[*ast.Ident]bool
}

// checkUnusedDecls warns of every private function, method and type, and
// every parameter, nothing uses, as the comment at the top of the file
// says, when the program has no error.
func (c *Checker) checkUnusedDecls() {
	if c.prior > 0 || len(c.diags) > 0 {
		return
	}
	u := &usage{
		used:    make(map[ast.Decl]bool),
		read:    make(map[*ast.Param]bool),
		value:   make(map[*ast.Func]bool),
		bound:   make(map[*ast.Func]bool),
		callees: make(map[*ast.Ident]bool),
	}
	for _, decl := range c.prog.Decls() {
		u.walkDecl(decl)
	}
	c.bindInstances(u)
	for _, pkg := range c.prog.Packages {
		for _, decl := range pkg.Decls() {
			c.warnUnused(pkg, decl, u)
		}
	}
}

// walkDecl records what decl uses, leaving out its uses of itself: a
// function's or a method's of itself, and a type's, or one of its
// methods', of that type.
func (u *usage) walkDecl(decl ast.Decl) {
	var ownFunc *ast.Func
	var ownType *ast.TypeDecl
	switch d := decl.(type) {
	case *ast.Func:
		ownFunc, ownType = d, d.RecvType
	case *ast.TypeDecl:
		ownType = d
	}
	visit := func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Call:
			u.call(x, ownFunc)
		case *ast.Ident:
			u.ident(x, ownFunc, ownType)
		case *ast.Selector:
			u.methodValue(x, ownFunc)
		}
		return true
	}
	ast.Walk(decl, visit)
	if fn, ok := decl.(*ast.Func); ok && fn.Ensures != nil {
		ast.Walk(fn.Ensures, visit)
	}
}

// call records the method call calls as used, unless it is ownFunc, the
// function the call is in, or the stand-in for a method of a type
// parameter's constraint, and notes a name it calls by as a callee, not
// a value. A call is visited before its callee's name is.
func (u *usage) call(call *ast.Call, ownFunc *ast.Func) {
	if id, ok := call.Fn.(*ast.Ident); ok {
		u.callees[id] = true
	}
	if m := call.Method; m != nil && m.Interface == nil && m != ownFunc {
		u.used[m] = true
	}
}

// methodValue records the method sel names as a value, Pair.first, when
// it is one, as used, unless it is ownFunc, the method sel is in, and as
// used as a value, whose type the value's must match.
func (u *usage) methodValue(sel *ast.Selector, ownFunc *ast.Func) {
	m := sel.Method
	if m == nil {
		return
	}
	if m != ownFunc {
		u.used[m] = true
	}
	u.value[m] = true
}

// ident records what id names as used, unless it is ownFunc or ownType,
// the declaration id is in: a function, and whether as a value, a type,
// or a parameter, read.
func (u *usage) ident(id *ast.Ident, ownFunc *ast.Func, ownType *ast.TypeDecl) {
	switch def := id.Def.(type) {
	case *ast.Func:
		if def != ownFunc {
			u.used[def] = true
		}
		if !u.callees[id] {
			u.value[def] = true
		}
	case *ast.TypeDecl:
		if def != ownType {
			u.used[def] = true
		}
	case *ast.Param:
		u.read[def] = true
	}
}

// bindInstances records, for every instance of a generic function, each
// type argument's type as used, and each of its methods that the
// constraint of the type parameter it is bound to asks for as used and
// bound: the instance calls it.
func (c *Checker) bindInstances(u *usage) {
	for _, inst := range c.prog.Instances {
		for i, tp := range inst.Func.TypeParams {
			if i >= len(inst.TypeArgs) {
				break
			}
			td := c.typeDecl(inst.TypeArgs[i])
			if td == nil {
				continue
			}
			u.used[td] = true
			if tp.Iface == nil {
				continue
			}
			for _, want := range tp.Iface.Interface().Methods {
				if m := td.Method(want.Name); m != nil {
					u.used[m], u.bound[m] = true, true
				}
			}
		}
	}
}

// warnUnused warns of decl, a declaration of pkg, when it is private and
// unused, or of each of its parameters nothing reads when it is a
// function whose signature is its own (see the comment at the top of the
// file).
func (c *Checker) warnUnused(pkg *ast.Package, decl ast.Decl, u *usage) {
	switch d := decl.(type) {
	case *ast.Func:
		if d.Vis != ast.Private {
			return
		}
		if !u.used[d] && !isMainFunc(pkg, d) {
			c.unusedf(d.NameSpan, "declared and not used: %s", c.displayName(d))
			return
		}
		c.warnUnusedParams(d, u)
	case *ast.TypeDecl:
		if d.Vis == ast.Private && !u.used[d] {
			c.unusedf(d.NameSpan, "declared and not used: %s", d.Name)
		}
	}
}

// warnUnusedParams warns of each parameter of fn, a private function
// that is used, that nothing reads, but its receiver, unless fn's
// signature is not its own: it is used as a value, a constraint asks for
// it, or it has no body.
func (c *Checker) warnUnusedParams(fn *ast.Func, u *usage) {
	if u.value[fn] || u.bound[fn] || fn.NoBody {
		return
	}
	params := fn.Params
	if fn.Method && len(params) > 0 {
		params = params[1:]
	}
	for _, p := range params {
		if !u.read[p] {
			c.unusedf(p.NameSpan, "declared and not used: %s", p.Name)
		}
	}
}

// isMainFunc reports whether fn, a function of pkg, is the main function
// of package main.
func isMainFunc(pkg *ast.Package, fn *ast.Func) bool {
	return pkg.Name == "main" && fn.Name == "main" && !fn.Method
}

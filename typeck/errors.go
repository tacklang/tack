package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// An error is a returned value, of type error, types.Error: a struct of a
// code, an int32, and a message, a string, whose code is 0 when there is no
// error. Three rules hold for a function that returns one:
//
//   - the error is its last result: an error anywhere else in a result
//     list is reported where it is written (checkErrorLast);
//   - a call's error cannot be discarded: a call statement that returns
//     one, and a binding that puts _ where the error goes, are reported
//     (droppedError, checkErrorBinding);
//   - the other results a binding takes beside the error, v in
//     v, err := f(), are held by the error's variable, err, and cannot be
//     read until err is checked: where the facts in force prove err.code
//     == 0 (requireChecked).
//
// A check is a fact, as a divisor's proof is, so it comes from the
// conditions facts come from, an if's branches, the rest of a block after
// an early exit, a loop's body, the right of && and ||, a switch's cases,
// and holds only up to a write to err that can reach the read. Its subject is
// err.code, which carries facts whether err is mutable or not
// (errorCodeKey), though a field of a mutable variable carries none for
// any other proof. A check is proven when those facts bound err.code to
// exactly 0: err.code == 0 being true, err.code != 0 being false, or
// anything else that pins it there.
//
// A return may pass v on unchecked, return v, err, when err is the
// function's error result and v goes out beside it whole: the caller then
// holds v beside the same error, and checks it before reading v (passOn).
//
// The proof is about err's value, and v is read on the strength of it, so
// err must still hold the error of the call that gave v. So a binding that
// holds v binds the error and v to local variables, never to a global, a
// parameter, a field or an element (checkErrorTargets), and err is then
// assigned only by a binding that assigns every variable it holds, so
// that whenever err changes, what it holds changes with it
// (checkErrorAssign). Which variables an error holds is collected from
// the whole function before its body is checked (collectHolds), since a
// loop can bind them after a read that comes first in the source.

// holding is what one function's bindings of errors tie together: for
// each variable bound beside an error, the error variables that hold it,
// and for each error variable, the variables it holds, each in source
// order, and for each pair, the first binding that tied them. The ok of
// a checked pop or remove holds the element bound beside it in the same
// way, when its type has no zero value (see okHeld): oks holds each such
// ok variable, for the name of the builtin that bound it.
type holding struct {
	heldBy map[ast.Decl][]ast.Decl
	holds  map[ast.Decl][]ast.Decl
	first  map[tie]ast.Span
	oks    map[ast.Decl]string
}

// tie is an error variable, err, and a variable it holds, v.
type tie struct {
	err, v ast.Decl
}

// returnsError returns the function call calls, when it is one whose last
// result is an error. A method call calls one only once it has been
// checked, which finds its method.
func returnsError(call *ast.Call) (*ast.Func, bool) {
	if call.Conv != types.Invalid {
		return nil, false
	}
	fn := ast.Callee(call)
	if fn == nil || len(fn.ResultTypes) == 0 {
		return nil, false
	}
	return fn, fn.ResultTypes[len(fn.ResultTypes)-1] == types.Error
}

// errorCall returns the name, as calleeName gives it, of the function
// value calls, when value is a call to a function or a method whose last
// result is an error and which returns want results, or to add or
// add_all, which return an id or a span and an error.
func (c *Checker) errorCall(value ast.Expr, want int) (string, bool) {
	call, ok := value.(*ast.Call)
	if !ok {
		return "", false
	}
	if addsToArena(call) {
		// Known before the call is checked, as a function's results are,
		// so a binding of it holds its id or span from the start.
		id, _ := call.Fn.(*ast.Ident)
		return id.Name, want == 2
	}
	fn, ok := returnsError(call)
	if !ok || len(fn.ResultTypes) != want {
		return "", false
	}
	return c.calleeName(call)
}

// holderCall returns the name, as calleeName gives it, of what value
// calls, when a binding of want names to it ties its last name to the
// others: a call to a function or a method whose last result is an error,
// and which returns want results, or a checked pop or remove, two names,
// whose ok holds the element (see okHeld), which isOk says.
func (c *Checker) holderCall(value ast.Expr, want int) (name string, isOk, ok bool) {
	if name, ok := c.errorCall(value, want); ok {
		return name, false, true
	}
	call, isCall := value.(*ast.Call)
	if !isCall || want != 2 || !isTake(call) {
		return "", false, false
	}
	id, _ := call.Fn.(*ast.Ident)
	return id.Name, true, true
}

// okHeld reports whether v, a variable bound beside the ok of a checked
// pop or remove, is held by it: v's type has no zero value, so when ok is
// false v holds nothing it could be read as, and v cannot be read until
// ok is checked true. One of a type with a zero value holds it then, and
// is read freely.
func (c *Checker) okHeld(v ast.Decl) bool {
	switch d := v.(type) {
	case *ast.Var:
		return d.Type != types.Invalid && !c.table.HasZero(d.Type)
	default:
		return false
	}
}

// heldVars returns the variables of held that holder holds now: all of
// them for an error, and for an ok, those okHeld says it holds.
func (c *Checker) heldVars(holder ast.Decl, held []ast.Decl) []ast.Decl {
	if c.holding.oks[holder] == "" {
		return held
	}
	var out []ast.Decl
	for _, v := range held {
		if c.okHeld(v) {
			out = append(out, v)
		}
	}
	return out
}

// checkErrorLast reports every result of fn before its last that is an
// error: an error is always a function's last result.
func (c *Checker) checkErrorLast(fn *ast.Func) {
	for i, typ := range fn.ResultTypes[:max(len(fn.ResultTypes)-1, 0)] {
		if typ == types.Error {
			c.errorf(
				fn.Results[i].Spanned(),
				"error must be the last result of %s",
				c.displayName(fn),
			)
		}
	}
}

// droppedError reports e, a call statement, when its callee returns an
// error, which a call statement drops, with the binding that would keep
// it: err := f(...), with a name for each result before the error that
// cannot be discarded either, x, y, z, then x4, x5 and so on, in order, and
// a _ for each other (see bindingNames). append and reserve return an
// error alone, and add and add_all an id or a span and an error, which
// their calls check: one whose arguments were wrong has no type, and is
// not reported again.
func (c *Checker) droppedError(e ast.Expr) {
	call, ok := e.(*ast.Call)
	if !ok {
		return
	}
	if returnsListError(call) {
		if call.Type == types.Error {
			id, _ := call.Fn.(*ast.Ident)
			c.errorf(call.Spanned(),
				"the error %s returns cannot be discarded; bind it, as in err := %s(...)",
				id.Name, id.Name)
		}
		return
	}
	if addsToArena(call) {
		if call.ResultTypes != nil {
			id, _ := call.Fn.(*ast.Ident)
			c.errorf(call.Spanned(),
				"the error %s returns cannot be discarded; bind it, as in _, err := %s(...)",
				id.Name, id.Name)
		}
		return
	}
	fn, ok := returnsError(call)
	if !ok {
		return
	}
	name, _ := c.calleeName(call)
	results := ast.CallResults(call)
	if len(results) != len(fn.ResultTypes) {
		results = fn.ResultTypes
	}
	c.errorf(
		call.Spanned(),
		"the error %s returns cannot be discarded; bind it, as in %s := %s(...)",
		name, c.bindingNames(results), c.callText(call))
}

// bindingNames returns the targets of a binding that keeps every result
// of a call returning results, whose last is an error: err for the error,
// a name for each result before it that is owned, which _ cannot take,
// x, y and z, then x4, x5 and so on, in order, and _ for each other.
func (c *Checker) bindingNames(results []types.Type) string {
	names := make([]string, 0, len(results))
	owned := 0
	for _, typ := range results[:len(results)-1] {
		if !c.table.Owned(typ) {
			names = append(names, "_")
			continue
		}
		owned++
		if owned <= 3 {
			names = append(names, string(rune('x'+owned-1)))
		} else {
			names = append(names, fmt.Sprintf("x%d", owned))
		}
	}
	return strings.Join(append(names, "err"), ", ")
}

// checkErrorBinding reports a binding of value, whose results checked as
// valid, that drops an error: value is a call that returns one, and the
// last of the binding's targets, blank saying which are _ and spans where
// each is written, is _.
func (c *Checker) checkErrorBinding(value ast.Expr, blank []bool, spans []ast.Span) {
	name, isOk, ok := c.holderCall(value, len(blank))
	switch {
	case !ok:
	case isOk:
		c.checkOkBinding(value.(*ast.Call), name, blank, spans)
	case blank[len(blank)-1]:
		c.errorf(spans[len(spans)-1],
			"the error %s returns cannot be discarded; bind it to a variable instead of _", name)
	}
}

// checkOkDropped reports a binding of value, a checked form, that keeps
// its value and drops its ok, blank saying which of its two names are _
// and spans where each is written: a division or a remainder, an index, a
// slice, a conversion, a type assertion, or pop, remove or delete. The ok
// is the check the checked form exists for, and dropping it reads a value
// that may be the zero value standing in for one there is not, as an
// error cannot be discarded. Binding both to _ is its own error (see
// nothingBound), and a pop, remove or delete whose element cannot be read
// until its ok is checked says so instead (see checkOkBinding).
func (c *Checker) checkOkDropped(value ast.Expr, blank []bool, spans []ast.Span) {
	if len(blank) != 2 || blank[0] || !blank[1] || !c.isCheckedForm(value) {
		return
	}
	if call, ok := value.(*ast.Call); ok && isTake(call) && !c.table.HasZero(ast.TypeOf(call)) {
		return
	}
	c.errorf(
		spans[1],
		"the ok %s gives cannot be discarded; bind it to a variable instead of _",
		c.text(value.Spanned()),
	)
}

// isCheckedForm reports whether value, bound to two names whose results
// checked as valid, is a checked form, giving a value and an ok: a
// division or a remainder, an index, a slice, a type assertion, a
// conversion, or a call to pop, remove or delete.
func (c *Checker) isCheckedForm(value ast.Expr) bool {
	switch x := value.(type) {
	case *ast.Binary:
		return x.Op == ast.Div || x.Op == ast.Rem || isPlainArith(x)
	case *ast.Unary:
		return isPlainArith(x)
	case *ast.Index, *ast.Slice, *ast.TypeAssert:
		return true
	case *ast.Call:
		return x.Conv != types.Invalid || isTake(x)
	default:
		return false
	}
}

// checkOkBinding reports a checked pop or remove, call, calling name,
// whose binding, blank saying which names are _ and spans where each is
// written, drops what it should not: an owned element, bound to _, which
// nothing would free; or an ok that holds the element bound beside it
// (see okHeld), which, bound to _, would leave the element never to be
// read.
func (c *Checker) checkOkBinding(call *ast.Call, name string, blank []bool, spans []ast.Span) {
	elem := ast.TypeOf(call)
	switch {
	case blank[0] && c.table.Owned(elem):
		c.errorf(
			spans[0],
			"%s returns %s; bind it to a variable instead of _",
			name,
			c.droppedName(elem),
		)
	case !blank[0] && blank[1] && !c.table.HasZero(elem):
		c.errorf(
			spans[1],
			"the ok %s returns cannot be discarded: %s cannot be read until it is checked; "+
				"bind it to a variable instead of _",
			name,
			c.text(spans[0]),
		)
	}
}

// checkErrorTargets checks the targets of m, an assignment of several
// results, when its value is a call that returns an error and m binds a
// result beside it: the error and every result bound beside it must go to
// local variables, so the error's check can be tracked, and only after it
// the others read.
func (c *Checker) checkErrorTargets(m *ast.MultiAssign) {
	name, isOk, ok := c.holderCall(m.Value, len(m.Targets))
	if !ok {
		return
	}
	if isOk {
		c.checkOkTargets(m, name)
		return
	}
	last := len(m.Targets) - 1
	beside := c.besideNames(m.Targets[:last])
	if len(beside) == 0 {
		return
	}
	for _, target := range m.Targets[:last] {
		if !ast.IsBlank(target) && !c.isLocal(target) {
			c.errorf(target.Spanned(), "%s cannot take a result of %s beside its error; "+
				"bind it to a local variable", c.text(target.Spanned()), name)
		}
	}
	if errTarget := m.Targets[last]; !ast.IsBlank(errTarget) && !c.isLocal(errTarget) {
		c.errorf(errTarget.Spanned(), "%s cannot take the error of %s beside %s; "+
			"bind it to a local variable", c.text(errTarget.Spanned()), name, strings.Join(beside, ", "))
	}
}

// besideNames returns how each of targets that is not _ is written.
func (c *Checker) besideNames(targets []ast.Expr) []string {
	var names []string
	for _, target := range targets {
		if !ast.IsBlank(target) {
			names = append(names, c.text(target.Spanned()))
		}
	}
	return names
}

// checkOkTargets checks the targets of m, an assignment of a checked pop
// or remove, calling name, whose element has no zero value: the element
// and the ok must go to local variables, so the ok's check can be
// tracked, and only after it the element read, as an error's must.
func (c *Checker) checkOkTargets(m *ast.MultiAssign, name string) {
	elem, okTarget := m.Targets[0], m.Targets[1]
	if ast.IsBlank(elem) || c.table.HasZero(ast.TypeOf(m.Value)) {
		return
	}
	if !c.isLocal(elem) {
		c.errorf(elem.Spanned(), "%s cannot take the element %s gives beside its ok; "+
			"bind it to a local variable", c.text(elem.Spanned()), name)
	}
	if !ast.IsBlank(okTarget) && !c.isLocal(okTarget) {
		c.errorf(
			okTarget.Spanned(),
			"%s cannot take the ok of %s beside %s; bind it to a local variable",
			c.text(okTarget.Spanned()),
			name,
			c.text(elem.Spanned()),
		)
	}
}

// isLocal reports whether e is the name of a local variable: one declared
// in a function, not a parameter or a package-level variable.
func (c *Checker) isLocal(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := id.Def.(*ast.Var)
	return ok && !c.globals[v]
}

// localDecl returns the local variable e names, when it names one.
func (c *Checker) localDecl(e ast.Expr) (ast.Decl, bool) {
	if !c.isLocal(e) {
		return nil, false
	}
	id, _ := e.(*ast.Ident)
	return id.Def, true
}

// collectHolds records, before fn's body is checked, what every binding
// in it of a call that returns an error ties together: the local variable
// the error goes to holds each local variable a result beside it goes to.
// A binding whose error goes to _ or anywhere but a local ties nothing,
// and was or will be reported.
func (c *Checker) collectHolds(fn *ast.Func) {
	c.holding = holding{
		heldBy: make(map[ast.Decl][]ast.Decl),
		holds:  make(map[ast.Decl][]ast.Decl),
		first:  make(map[tie]ast.Span),
		oks:    make(map[ast.Decl]string),
	}
	for _, stmt := range fn.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.MultiVar:
				targets := make([]ast.Decl, len(s.Vars))
				for i, v := range s.Vars {
					if v.Name != ast.Blank {
						targets[i] = v
					}
				}
				c.hold(s.Value, targets, s.Span)
			case *ast.MultiAssign:
				targets := make([]ast.Decl, len(s.Targets))
				for i, target := range s.Targets {
					targets[i], _ = c.localDecl(target)
				}
				c.hold(s.Value, targets, s.Span)
			}
			return true
		})
	}
}

// hold records what a binding at span of value to targets, the local
// variable each result goes to or nil, ties together, when value is a
// call that returns an error: the error's variable holds every other one;
// or a checked pop or remove: its ok's variable holds the element's,
// which is enforced only once the element's type is known to have no zero
// value (see okHeld).
func (c *Checker) hold(value ast.Expr, targets []ast.Decl, span ast.Span) {
	name, isOk, ok := c.holderCall(value, len(targets))
	if !ok {
		return
	}
	errDecl := targets[len(targets)-1]
	if errDecl == nil {
		return
	}
	if isOk {
		c.holding.oks[errDecl] = name
	}
	for _, v := range targets[:len(targets)-1] {
		if v == nil {
			continue
		}
		if !containsDecl(c.holding.holds[errDecl], v) {
			c.holding.holds[errDecl] = append(c.holding.holds[errDecl], v)
		}
		if !containsDecl(c.holding.heldBy[v], errDecl) {
			c.holding.heldBy[v] = append(c.holding.heldBy[v], errDecl)
		}
		if _, seen := c.holding.first[tie{errDecl, v}]; !seen {
			c.holding.first[tie{errDecl, v}] = span
		}
	}
}

// containsDecl reports whether decls holds decl.
func containsDecl(decls []ast.Decl, decl ast.Decl) bool {
	for _, d := range decls {
		if d == decl {
			return true
		}
	}
	return false
}

// errorCodeKey returns the subject err.code is, when e is the code of an
// error variable or parameter that can hold a fact, mutable or not: the
// subject of an error's check. keyOf gives the same key for an immutable
// one; a mutable one's code carries facts only for checking the error,
// since no proof but requireChecked's asks about it.
func (c *Checker) errorCodeKey(e ast.Expr) (factKey, bool) {
	sel, ok := e.(*ast.Selector)
	if !ok || sel.Name != "code" {
		return factKey{}, false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || ast.TypeOf(id) != types.Error || !c.canHoldFact(id.Def) {
		return factKey{}, false
	}
	return factKey{decl: id.Def, path: ".code"}, true
}

// noError reports whether the facts in force prove the error variable
// errDecl holds no error: they bound its code to exactly 0.
func (c *Checker) noError(errDecl ast.Decl) bool {
	known := codeInterval()
	k := factKey{decl: errDecl, path: ".code"}
	for f := range c.heldFactsAbout(k) {
		if c.holds(f) {
			known = known.narrow(f)
		}
	}
	return isZeroOnly(known)
}

// codeInterval returns the bounds an error's code has from its type, an
// int32's.
func codeInterval() interval {
	lo, hi := typeBounds(types.Int32)
	return interval{lo: lo, hi: hi, hasLo: true, hasHi: true}
}

// isZeroOnly reports whether known allows 0 and nothing else.
func isZeroOnly(known interval) bool {
	return known.hasLo && known.hasHi && known.lo.IsZero() && known.hi.IsZero()
}

// requireChecked reports id, a read of a variable, when an error variable
// holds it and the facts in force do not prove that error is no error,
// naming the first such error, with notes on any check of it that falls
// short.
func (c *Checker) requireChecked(id *ast.Ident) {
	if id.Def == nil {
		return
	}
	for _, errDecl := range c.holding.heldBy[id.Def] {
		if c.holding.oks[errDecl] != "" {
			if !c.okHeld(id.Def) || c.okTrue(errDecl) {
				continue
			}
			name := declName(errDecl)
			c.errorNotes(
				id.Span,
				c.explain(c.okProof(errDecl)),
				"%s cannot be read before %s is checked; check that %s is true first",
				id.Name,
				name,
				name,
			)
			return
		}
		if c.noError(errDecl) || c.passedOn[id] == errDecl {
			continue
		}
		name := declName(errDecl)
		c.errorNotes(id.Span, c.explain(c.errorProof(errDecl)),
			"%s cannot be read before %s is checked; check %s.code == 0 first", id.Name, name, name)
		return
	}
}

// passOn returns the values ret, a return of fn with one value for each
// result, passes on beside the error that holds them, each for that
// error's variable: when fn's last result is an error and ret returns
// there a local error variable, each other value that is a variable it
// holds, returned whole as a result that is not a view. The caller then
// binds that value beside the same error, so reading it there waits for
// the check this return skips. An ok holds nothing a return passes on.
func (c *Checker) passOn(ret *ast.Return, fn *ast.Func) map[*ast.Ident]ast.Decl {
	last := len(ret.Values) - 1
	if last < 1 || fn.ResultTypes[last] != types.Error {
		return nil
	}
	errID, ok := ret.Values[last].(*ast.Ident)
	if !ok || errID.Def == nil || c.holding.oks[errID.Def] != "" {
		return nil
	}
	passed := make(map[*ast.Ident]ast.Decl)
	for i, v := range ret.Values[:last] {
		if id, ok := c.heldWhole(v, fn.ResultTypes[i], errID.Def); ok {
			passed[id] = errID.Def
		}
	}
	return passed
}

// heldWhole returns v as a variable errDecl holds, when it is one, returned
// whole as a result of type result that is not a view.
func (c *Checker) heldWhole(v ast.Expr, result types.Type, errDecl ast.Decl) (*ast.Ident, bool) {
	id, ok := v.(*ast.Ident)
	if !ok || id.Def == nil || c.table.IsView(result) {
		return nil, false
	}
	return id, containsDecl(c.holding.heldBy[id.Def], errDecl)
}

// readHeld reports target, the target of a compound assignment, an
// increment or a decrement, which reads the variable it is part of, as
// requireChecked reports a read.
func (c *Checker) readHeld(target ast.Expr) {
	if root := ast.Root(target); root != nil {
		c.requireChecked(root)
	}
}

// errorProof returns the proof that the error variable errDecl holds no
// error, which explain turns into notes: a check whose fact was dropped,
// with what dropped it, or a check in force that leaves a code other than
// 0 possible.
func (c *Checker) errorProof(errDecl ast.Decl) proof {
	name := declName(errDecl) + ".code"
	return proof{
		about:  factKey{decl: errDecl, path: ".code"},
		proven: func() bool { return c.noError(errDecl) },
		claim:  func(facts []fact) string { return describe(name, facts) },
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			if len(facts) == 0 {
				return nil
			}
			known := codeInterval()
			for _, f := range facts {
				known = known.narrow(f)
			}
			f := facts[len(facts)-1]
			msg := f.guard.subject() + " " + verb("allows", "allow", would) + " " +
				name + " == " + errorWitness(known).String()
			return []diag.Diagnostic{note(f.span(), msg)}
		},
	}
}

// okTrue reports whether the facts in force prove the bool variable
// okDecl true: they bound it to 1, as if ok, or !ok leaving, puts it.
func (c *Checker) okTrue(okDecl ast.Decl) bool {
	var known interval
	k := declKey(okDecl)
	for f := range c.heldFactsAbout(k) {
		if c.holds(f) {
			known = known.narrow(f)
		}
	}
	return known.hasLo && known.lo.Cmp(constant.FromInt64(1)) >= 0
}

// okProof returns the proof that the bool variable okDecl is true, which
// explain turns into notes: a check whose fact was dropped, with what
// dropped it, or a guard in force that leaves it false.
func (c *Checker) okProof(okDecl ast.Decl) proof {
	name := declName(okDecl)
	isTrue := func(facts []fact) bool {
		var known interval
		for _, f := range facts {
			known = known.narrow(f)
		}
		return known.hasLo && known.lo.Cmp(constant.FromInt64(1)) >= 0
	}
	return proof{
		about:  declKey(okDecl),
		proven: func() bool { return c.okTrue(okDecl) },
		claim: func(facts []fact) string {
			if isTrue(facts) {
				return name + " is true"
			}
			return name + " is false"
		},
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			if len(facts) == 0 {
				return nil
			}
			f := facts[len(facts)-1]
			msg := f.guard.subject() + " " + verb(
				"allows",
				"allow",
				would,
			) + " " + name + " to be false"
			return []diag.Diagnostic{note(f.span(), msg)}
		},
	}
}

// errorWitness returns a code other than 0 that known allows, preferring 1
// and then -1, or 0 when it allows no other.
func errorWitness(known interval) constant.Int {
	for _, v := range []int64{1, -1} {
		k := constant.FromInt64(v)
		if known.lo.Cmp(k) <= 0 && known.hi.Cmp(k) >= 0 {
			return k
		}
	}
	if !known.lo.IsZero() {
		return known.lo
	}
	return known.hi
}

// checkErrorAssign reports target, assigned by stmt, when it is or is part
// of an error variable that holds others and stmt is not a binding that
// assigns every one of them along with it: whatever changes the error
// must change what it holds too, or a check of the new error would let
// the old values be read.
func (c *Checker) checkErrorAssign(stmt ast.Stmt, target ast.Expr) {
	root := ast.Root(target)
	if root == nil {
		return
	}
	held := c.heldVars(root.Def, c.holding.holds[root.Def])
	if len(held) == 0 {
		return
	}
	missing := held
	if m, ok := stmt.(*ast.MultiAssign); ok && target == m.Targets[len(m.Targets)-1] &&
		target == ast.Expr(root) {
		missing = c.unassigned(m, held)
	}
	if len(missing) == 0 {
		return
	}
	c.heldAssigned(target.Spanned(), root.Def, missing, "")
}

// unassigned returns the variables of held that m, a binding whose last
// target is an error variable, does not assign, all of them when its
// value is not a call that returns an error.
func (c *Checker) unassigned(m *ast.MultiAssign, held []ast.Decl) []ast.Decl {
	if _, _, ok := c.holderCall(m.Value, len(m.Targets)); !ok {
		return held
	}
	var missing []ast.Decl
	for _, v := range held {
		assigned := false
		for _, target := range m.Targets[:len(m.Targets)-1] {
			if d, ok := c.localDecl(target); ok && d == v {
				assigned = true
			}
		}
		if !assigned {
			missing = append(missing, v)
		}
	}
	return missing
}

// checkErrorMut reports m, a mut argument, when it passes an error
// variable that holds others, or part of one: the callee could assign it
// without them.
func (c *Checker) checkErrorMut(m *ast.MutArg) {
	root := ast.Root(m.X)
	if root == nil {
		return
	}
	held := c.heldVars(root.Def, c.holding.holds[root.Def])
	if len(held) == 0 {
		return
	}
	c.heldAssigned(m.Span, root.Def, held, "cannot pass "+c.text(m.Span)+": ")
}

// heldAssigned reports an assignment at span to errDecl, an error variable
// that holds the variables missing, which the assignment does not assign,
// with a note at the binding that first tied it to the first of them,
// prefix starting the message.
func (c *Checker) heldAssigned(span ast.Span, errDecl ast.Decl, missing []ast.Decl, prefix string) {
	list := joinAnd(heldNames(missing))
	notes := []diag.Diagnostic{note(c.holding.first[tie{errDecl, missing[0]}],
		declName(errDecl)+" is bound beside "+declName(missing[0])+" here")}
	what := "holds the error that came with"
	if name := c.holding.oks[errDecl]; name != "" {
		what = "says whether " + name + " gave"
	}
	c.errorNotes(
		span,
		notes,
		"%s%s %s %s, so only a binding that also assigns %s can assign it",
		prefix,
		declName(errDecl),
		what,
		list,
		list,
	)
}

// heldNames returns the names of decls, in order.
func heldNames(decls []ast.Decl) []string {
	names := make([]string, len(decls))
	for i, d := range decls {
		names[i] = declName(d)
	}
	return names
}

// joinAnd joins names as a sentence lists them: a, b and c.
func joinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// Checker type-checks a program. It is single-use: create one with New and
// call Check once.
type Checker struct {
	prog  *ast.Program
	table *types.Table // the program's types, recorded on it by Check

	// builtins holds the signature of each builtin a call can name.
	builtins *builtin.Set

	// files holds the source text of the program's files, which notes
	// quote; see text.
	files source.Files

	// checkedDivision is the division whose value is being bound with ok,
	// which needs no proof that its divisor is not zero.
	checkedDivision *ast.Binary

	// checkedArith is the arithmetic, + - * or unary - on integers, whose
	// value is being bound with ok, which needs no proof that it does not
	// overflow.
	checkedArith ast.Expr

	// deferredOverflow, while a loop's post statement is checked, collects
	// the operations in it not proven not to overflow, for recheckPost to
	// try again with the loop's condition in force; it is nil otherwise.
	deferredOverflow *[]overflowCheck

	// arithMemo remembers, for each arithmetic operation proven or not
	// while the statement being checked, the bounds operandBounds gives it
	// and whether it was proven, so that an operation above it does not
	// work its operands out again (see memoFor). stmtGen counts the
	// statements checked, and a memo is used only in the statement, and
	// with the facts, it was made under.
	arithMemo map[ast.Expr]arithMemo
	stmtGen   int

	// typedRelations, while set, has a query's graph count the bounds of
	// each variable's type as relations (see graphBuilder.typeRange), which
	// the proofs that arithmetic does not overflow ask for.
	typedRelations bool

	// plainRelations, while set, has a query's graph leave out a stored
	// value's bounds (see storedBounds), which the notes on a missing
	// proof ask for: a link through one would name a check that follows
	// from a value the code just stored, not one it was making.
	plainRelations bool

	// relatedSubjects holds the subjects that, in the function being checked so
	// far, took part in a relation to another variable or in a proof-only
	// fact of several sides (see noteRelated), whether or not it still
	// holds. A note on what the facts allow such a subject is left out
	// (see explain).
	relatedSubjects map[factKey]bool

	// checkedIndex is the element whose value is being bound with ok, which
	// needs no proof that its index is in range.
	checkedIndex *ast.Index

	// checkedSlice is the slice whose value is being bound with ok, whose
	// bounds need no proof.
	checkedSlice *ast.Slice

	// checkedAssert is the type assertion whose value is being bound with
	// ok, the one place an assertion may be.
	checkedAssert *ast.TypeAssert

	// checkedTake is the call to pop or remove whose value is being bound
	// with ok, which needs no proof that the list holds what it takes.
	checkedTake *ast.Call

	// droppedCall is the call written as a statement being checked, whose
	// value, if any, is dropped.
	droppedCall *ast.Call

	// fn is the function whose body is being checked, which a ? returns
	// from, or nil outside every body, as in a package-level variable's
	// value.
	fn *ast.Func

	// scopeUse is the name of a scope parameter or a scope allocator about
	// to be checked where one may be: a call's callee, an argument to a
	// scope parameter, or make's allocator. A name of one anywhere else is
	// reported (see scopeElsewhere and scopeAllocElsewhere).
	scopeUse *ast.Ident

	// viewSlice is the slice about to be checked where a view may be: an
	// argument to a view parameter, len's argument, or what another slice
	// slices. A slice of an array or a view anywhere else is reported.
	viewSlice *ast.Slice

	// viewCall is the call about to be checked where a view it returns
	// may be: anywhere viewSlice may be, and also what an index indexes,
	// what a range ranges over, and the value of a return from a function
	// that returns a view. A call returning a view anywhere else is
	// reported (see viewresult.go).
	viewCall *ast.Call

	// viewSpan is the span's view of an arena, a[s], about to be checked
	// where a view may be: anywhere viewCall may be.
	viewSpan *ast.Index

	// assigning is the assignment whose target is being checked, nil
	// otherwise: an element of a list it assigns is checked against the
	// mut arguments of its value (see checkAssignBorrows).
	assigning *pendingAssign

	// chain holds the facts of the last && or || binary checks, as
	// factsFrom gives them for the result that lets its right side run,
	// so that one an operator above it joins is found once, not again at
	// every operator of a long chain (see chainFacts).
	chain chainFacts

	globals map[*ast.Var]bool // the package-level variables

	// globalState says how far each package-level variable's check has
	// got; see ensureGlobal.
	globalState map[*ast.Var]progress

	// structs maps each struct type to its declaration, and structOrder
	// lists them in source order. structState says how far each one's
	// fields have been resolved; see resolveFields.
	structs     map[types.Type]*ast.TypeDecl
	structOrder []types.Type
	structState map[types.Type]progress

	// unions maps each union type to its declaration, and unionState says
	// how far each one's cases have been resolved; see resolveCases.
	// unionBindings holds the variables union patterns bind, each with
	// the tag of its switch: they refer to a payload of it in place (see
	// move.go).
	unions        map[types.Type]*ast.TypeDecl
	unionState    map[types.Type]progress
	unionBindings map[*ast.Var]ast.Expr

	// rangeBindings holds the element variables of range loops over a
	// list, a view or a map of an owned type, and the key variables of
	// those over a map of an owned key type, each with what its loop
	// ranges over: they refer to the element or the key in place (see
	// move.go).
	rangeBindings map[*ast.Var]ast.Expr

	// rangeKeys holds the key variables among rangeBindings, of range
	// loops over a map of an owned key type, which refer to the key in
	// place.
	rangeKeys map[*ast.Var]bool

	// bindings holds the local variables no declaration can make mut,
	// each with what it is, as a message about passing it as mut names
	// it: a range loop's variables, a type switch's, and the binding of
	// a union's payload. A variable is the same kind in every pass over
	// its body, so, unlike rangeBindings, it is not among the records the
	// first pass over a body keeps apart (see bodyRecords).
	bindings map[*ast.Var]string

	// regions holds the region each region's allocator, its name, is the
	// allocator of (see checkRegion).
	regions map[*ast.Var]*ast.Region

	// closureParams holds the parameters of closures, whose signature the
	// parameter each closure is passed to fixes, so declaring one mut
	// would not type-check. It is filled before a closure's body is
	// checked and, as bindings, kept across passes.
	closureParams map[*ast.Param]bool

	// ownReported holds the names checkOwnExclusive has reported, so a
	// call around the one that reported a name does not report it again.
	ownReported map[ast.Span]bool

	// tagWrites remembers, for each switch with a tag that tagFacts has
	// been asked about, where its values first may assign a variable.
	tagWrites map[*ast.Switch]tagWrite

	// knownVars remembers, for each local variable knownVar has been asked
	// about, what it found.
	knownVars map[*ast.Var]knownResult

	// knownBinaries remembers, for each arithmetic binary
	// knownArithmetic has been asked about once checked, what it found,
	// and the binary's type then (see knownArithmetic).
	knownBinaries map[*ast.Binary]knownBinary

	// deferSpan is the defer whose code is being checked, if any, and
	// lateDefaults each switch whose default, written before some of its
	// cases, or a case that default falls into, is being checked,
	// innermost last (see lateBodies and reaches).
	deferSpan    *ast.Span
	lateDefaults []lateDefault

	// clauses holds each switch case whose body is being checked,
	// innermost last, which a fallthrough is judged against (see
	// checkFallthrough), and fellFacts the facts in force at each
	// fallthrough checked whose case the next case's body has yet to
	// take, for that body to join (see checkFallenInto).
	clauses   []clauseAt
	fellFacts map[*ast.Fallthrough][]fact

	// storing is the assignment whose targets are being checked, if any.
	// A proof a target needs is held to the whole statement, a check kept
	// on its own even though the C backend reads a single target's indexes
	// before its value, and an assignment of several stores its targets in
	// turn, so every write in the statement, in its value or to another
	// target, reaches the checks of every target, a proven index's
	// included (see reaches).
	storing *ast.Span

	// body is the body of the function being checked, appends the calls
	// to append in it checked so far whose error proves what they added
	// (see appended.go), and writesByDecl every write in it to each
	// variable asked about.
	body         []ast.Stmt
	appends      []appendRecord
	writesByDecl map[ast.Decl][]funcWrite

	// active holds the facts in force where checking has reached, innermost
	// last, and guards the guards they came from, with any guard in scope
	// that gave none; see facts.go and explain.go. In a closure's body,
	// those before outerFacts and outerGuards are the function's around
	// it, and only those keepsInClosure keeps are in force (see
	// inForce): keptAt says, for each fact, whether it keeps it, kept is
	// the positions in active of those it does, in increasing order, and
	// guardKept says, for each guard, whether it gave one it does.
	active      []fact
	guards      []*guard
	keptAt      []bool
	kept        []int
	guardKept   []bool
	outerFacts  int
	outerGuards int

	// proven holds the facts where checking has reached kept apart from
	// active, which only the proof of an unreachable place asks about
	// (see proofOnly).
	proven []proofOnly

	// factsBy indexes the facts in force by their subject, when many are:
	// the positions in active of each subject's facts, in increasing
	// order, built the first time a query needs it and kept in step with
	// active from then on, or nil before then (see factindex.go).
	factsBy *subjectIndex

	// factsEdited counts the calls of supposing and provenWithout in
	// progress, each of which has swapped in facts whose drops and endings
	// are cleared, so no fact in force may be taken as shadowed by another
	// (see factindex.go).
	factsEdited int

	// relIdx is the index of the relations the facts in force give, kept
	// in step with them once built, or nil until a query needs it; a
	// closure's body reads its own (see relindex.go).
	relIdx *relIndex

	// holding is what the bindings of errors in the function being checked
	// tie together; see errors.go.
	holding holding

	// passedOn holds, while a return's values are checked, each value that
	// return passes on unchecked beside the error that holds it, for that
	// error's variable; see passOn.
	passedOn map[*ast.Ident]ast.Decl

	// declPkg holds the package of each package-level declaration, and
	// structPkg that of each struct type. pkg is the package whose code is
	// being checked, which a type of another package is named relative to:
	// see typeName.
	declPkg   map[ast.Decl]*ast.Package
	structPkg map[types.Type]*ast.Package
	pkg       *ast.Package

	// initStack holds the package-level variables being checked, the
	// outermost first, so an initialization cycle can be named in full.
	initStack []*ast.Var

	// pendingElems holds the element types of lists, arrays and views
	// written in struct fields, whose copyability is judged once every
	// struct's fields are known; see requireCopyable.
	pendingElems []pendingElem

	// pendingKeys holds the key types of maps written in struct fields,
	// whose == is judged once every struct's fields are known; see
	// requireKey.
	pendingKeys []pendingElem

	// pendingSizes holds the array types met before every struct's and
	// union's size can be known, and sizeChecked the types whose sizes
	// have been checked, each with the span of the site it was checked
	// at (see size.go).
	pendingSizes []sizeSite
	sizeChecked  map[types.Type]ast.Span

	// contracts holds the contract each function's ensures clause gives,
	// for every function that has one that checked, and defers every
	// defer in each function whose contract is being proven, found once
	// (see ensures.go).
	contracts map[*ast.Func]*contract
	defers    map[*ast.Func][]*ast.Defer

	// typeParams maps each type parameter's type to its declaration, and
	// standIns holds the stand-in for each interface method called on a
	// type parameter's values (see typeParamMethod). inference is what
	// the call to a generic function whose arguments are being checked
	// has inferred, nil outside one (see checkGenericCall).
	typeParams map[types.Type]*ast.TypeParam
	standIns   map[standInKey]*ast.Func
	inference  *inference

	// dynamics holds the stand-in for calls through values of each
	// function type (see dynamicFunc).
	dynamics map[types.Type]*ast.Func

	// lostMethods holds, for each package, the names of the methods its
	// bad declarations were declaring, and lostAll the packages with a
	// bad declaration that got no name, which could have been any method
	// (see invalid.go).
	lostMethods map[*ast.Package]map[string]bool
	lostAll     map[*ast.Package]bool

	diags []diag.Diagnostic

	// holders holds, for each variable the resolver left to be reported
	// that holds an error or an ok, what it holds (see unread.go).
	holders map[*ast.Var]string

	// warnings are what the checker reports that does not stop
	// compilation, kept apart from diags, so that every count of diags
	// that decides what to check next, whether a body is checked for
	// moved values among them, counts errors alone.
	warnings []diag.Diagnostic

	// invariants holds, for each loop the last probe found keeps a bound,
	// the bounds it keeps, which the check after the probe puts in force;
	// invFrames are the loops a probe is checking that may keep bounds,
	// innermost last; and starts holds the known values the mutable
	// variables in scope were last given, for the probe to start bounds
	// from (see invariant.go).
	invariants map[ast.Stmt][]fact
	invFrames  []*invFrame
	starts     []start

	loops int // how many loops enclose the statement being checked

	// prior is how many errors the stages before the checker reported
	// about the program, which let it report nothing about a name bound
	// to ast.Unbound or about damaged source (see invalid.go).
	prior int

	// at is where checking has reached, for the facts in force to be
	// judged at (see reaches): the end of the innermost expression or
	// statement being checked.
	at int

	// deferred is how many defers enclose the statement being checked, and
	// deferLoops how many loops enclose the innermost of them: a break or
	// continue with no more loops around it than that would leave the
	// deferred code.
	deferred   int
	deferLoops int

	// factGen counts the changes to active: each assume, and each release
	// that takes facts out of force.
	factGen int

	// work counts the steps the provers take, for the tests that bound
	// how checking grows with the program checked (see proverWork).
	work proverWork

	// leftBy remembers, for the last statement of a block, whether the
	// block leaves, and endedBy, for a statement, whether it terminates,
	// each for an if, a switch, a fallthrough or a loop, the statements
	// whose answer asks about others. exhaustGen counts the switches
	// found to name every case of their union, after which a no
	// remembered before is worked out again (see leavememo.go).
	leftBy     map[ast.Stmt]stmtAnswer
	endedBy    map[ast.Stmt]stmtAnswer
	exhaustGen int

	// dead is how many blocks around the statement being checked have
	// left before it, by a return, a break, a continue or a call to exit,
	// so that it never runs (see ensures.go).
	dead int

	// inCoveredDefault counts the defaults around the statement being
	// checked that are reported as never running, since full coverage of a
	// union already forbids them, where an unreachable is not reported
	// again (see checkUnreachable).
	inCoveredDefault int

	// inRejectedDefer is whether the statement being checked is inside a
	// defer already reported as a defer inside a defer, where a further
	// nested defer is not reported again.
	inRejectedDefer bool

	// inFields is whether a struct's field types, or a union's payload
	// types, are being resolved, where an array length must name no
	// variable; inUnion says it is a union's.
	inFields bool
	inUnion  bool

	// sizesKnown is whether every struct's and union's size can be
	// known; see pendingSizes.
	sizesKnown bool

	// everyBodyTwice makes checkBody check every body twice, not only one
	// that needs it, for a test to compare with checking each once (see
	// findCallees).
	everyBodyTwice bool

	// probing is set while a body is checked as a probe, to find the
	// bounds its loops keep (see invariant.go and invFrames).
	probing bool

	// probeEveryBody makes checkBody probe every body and keeps the check
	// after the probe from putting in force any bound it found, and
	// neverProbe makes it probe none, for a test to show that the probe
	// leaves no trace but those bounds (see findInvariants).
	probeEveryBody, neverProbe bool
}

// New returns a Checker for prog, which must already have been resolved,
// with builtins, and whose spans point into files. builtins gives each
// builtin's signature; a nil builtins has none.
func New(prog *ast.Program, files source.Files, builtins *builtin.Set) *Checker {
	return &Checker{
		prog:     prog,
		files:    files,
		builtins: builtins,
		table:    types.NewTable(),
		globals:  make(map[*ast.Var]bool),

		globalState: make(map[*ast.Var]progress),
		structs:     make(map[types.Type]*ast.TypeDecl),
		structState: make(map[types.Type]progress),

		unions:        make(map[types.Type]*ast.TypeDecl),
		unionState:    make(map[types.Type]progress),
		unionBindings: make(map[*ast.Var]ast.Expr),
		rangeBindings: make(map[*ast.Var]ast.Expr),
		rangeKeys:     make(map[*ast.Var]bool),
		bindings:      make(map[*ast.Var]string),
		regions:       make(map[*ast.Var]*ast.Region),
		closureParams: make(map[*ast.Param]bool),
		ownReported:   make(map[ast.Span]bool),
		tagWrites:     make(map[*ast.Switch]tagWrite),

		knownVars:     make(map[*ast.Var]knownResult),
		knownBinaries: make(map[*ast.Binary]knownBinary),
		arithMemo:     make(map[ast.Expr]arithMemo),
		holders:       make(map[*ast.Var]string),

		declPkg:   declPackages(prog),
		structPkg: make(map[types.Type]*ast.Package),

		typeParams: make(map[types.Type]*ast.TypeParam),
		standIns:   make(map[standInKey]*ast.Func),
		dynamics:   make(map[types.Type]*ast.Func),

		sizeChecked: make(map[types.Type]ast.Span),
		contracts:   make(map[*ast.Func]*contract),
		defers:      make(map[*ast.Func][]*ast.Defer),
		invariants:  make(map[ast.Stmt][]fact),

		leftBy:  make(map[ast.Stmt]stmtAnswer),
		endedBy: make(map[ast.Stmt]stmtAnswer),
	}
}

// declPackages returns the package of each package-level declaration of
// prog.
func declPackages(prog *ast.Program) map[ast.Decl]*ast.Package {
	m := make(map[ast.Decl]*ast.Package)
	if prog == nil {
		return m
	}
	for _, pkg := range prog.Packages {
		for _, decl := range pkg.Decls() {
			m[decl] = pkg
		}
	}
	return m
}

// typeName returns a type's name as a diagnostic about the package being
// checked writes it: a struct or union type of another package qualified
// by that package's name, lexer.Token, and any other type as
// types.Table.Name does.
func (c *Checker) typeName(typ types.Type) string {
	return c.table.NameWith(typ, func(st types.Type, name string) string {
		if pkg := c.structPkg[st]; pkg != nil && c.pkg != nil && pkg != c.pkg {
			return pkg.Name + "." + name
		}
		return name
	})
}

// enter makes the package of decl, a package-level declaration, the one
// being checked, and returns a function that puts back the one it replaced.
func (c *Checker) enter(decl ast.Decl) (restore func()) {
	old := c.pkg
	if pkg := c.declPkg[decl]; pkg != nil {
		c.pkg = pkg
	}
	return func() { c.pkg = old }
}

// Check reports every problem it finds in the program, in source order. Checking
// continues past the first problem, so one run reports all of them.
func (c *Checker) Check() []diag.Diagnostic {
	if c.prog == nil {
		panic("program is nil")
	}
	c.prog.Types = c.table
	for _, f := range c.prog.Files() {
		f.Types = c.table
	}
	c.prior = c.prog.Errors
	c.findLostMethods()
	decls := c.prog.Decls()

	// Every struct type exists before anything is checked, so any type can
	// name any struct. Their fields are resolved next, then the globals,
	// so the types and values of both are known in every function, and in
	// every signature, whose array lengths may name a global, whatever the
	// order of declarations. A field's array length names no variable, so
	// fields never need a global; a global's initializer may be a struct
	// literal, whose fields are resolved early if it comes first. A
	// global's initializer is a constant, so it never needs a function.
	c.declareStructs()
	c.checkGlobals(decls)
	c.resolveInterfaces(decls)

	// Signatures are resolved for every function before any body is checked,
	// so a call does not depend on its callee appearing first. A generic
	// function's type parameters come first, since its parameters' types
	// may name them.
	for _, decl := range decls {
		if fn, ok := decl.(*ast.Func); ok {
			restore := c.enter(fn)
			implicitTypeParams(fn)
			c.resolveTypeParams(fn)
			c.resolveParamTypes(fn)
			c.resolveResultTypes(fn)
			c.checkTypeParamsUsed(fn)
			c.checkContract(fn)
			restore()
		}
	}

	c.checkExposure(decls)
	c.checkCNames(decls)

	for _, decl := range decls {
		if fn, ok := decl.(*ast.Func); ok {
			restore := c.enter(fn)
			c.checkFunc(fn)
			restore()
		}
	}

	c.checkBadDecls()
	c.instantiate()
	c.checkUnusedDecls()
	c.prog.Errors += len(c.diags)
	c.table.Errors = c.prog.Errors
	return append(c.diags, c.warnings...)
}

// checkFunc checks fn once every signature is known: an extern func's C
// side, or any other's body, and then that no moved value is used in it,
// and that nothing made through a region outlives it (see escape.go),
// when the body had nothing else reported. An extern method, which the
// resolver reported, is not checked, and a body with bad statements is
// checked as checkDamagedBody says.
func (c *Checker) checkFunc(fn *ast.Func) {
	switch bad := ast.BadStmts(fn.Body); {
	case fn.Extern && fn.Method && c.prior > 0:
		// The resolver reported it: a method is never extern.
	case fn.Extern:
		c.checkExtern(fn)
	case len(bad) > 0:
		c.checkDamagedBody(fn, bad[0])
	default:
		before := len(c.diags)
		c.checkBody(fn)
		if len(c.diags) == before {
			c.checkMoves(fn)
		}
		if len(c.diags) == before {
			c.checkEscapes(fn)
		}
	}
}

// checkGlobals records which of decls are package-level variables, then
// resolves every struct's fields, then checks each variable, so that a
// global's initializer may write a literal of any struct.
func (c *Checker) checkGlobals(decls []ast.Decl) {
	for _, decl := range decls {
		if v, ok := decl.(*ast.Var); ok {
			c.globals[v] = true
		}
	}
	c.checkStructs()
	c.checkPendingElems()
	c.checkPendingKeys()
	c.checkTypeSizes()
	for _, decl := range decls {
		if v, ok := decl.(*ast.Var); ok {
			c.ensureGlobal(v, v.NameSpan)
		}
	}
	c.checkGlobalsSize(decls)
}

// checkBody checks the body of fn, and that it ends in a return if fn
// declares a result.
//
// What a binding of an error ties together is collected before the body
// is checked (see collectHolds), but a binding of a method call's results
// calls a method found only once its receiver is checked, and one of a
// call through a function value returns what the value's type, known
// once the value is checked, says. So a body with either is checked
// twice: first to find what each such call calls (see findCallees), and
// then for good, with every tie known.
func (c *Checker) checkBody(fn *ast.Func) {
	c.body = fn.Body
	outerFn := c.fn
	c.fn = fn
	defer func() { c.fn = outerFn }()
	c.firstPasses(fn)
	c.writesByDecl = make(map[ast.Decl][]funcWrite)
	c.collectHolds(fn)
	m, dead := c.mark(), c.dead
	c.relatedSubjects = nil
	c.checkStmts(fn.Body, fn)
	c.reportUnread(fn.Body)
	end, outer := fn.Span, c.at
	end.Start = end.End - 1
	c.at = end.End
	c.proveContract(fn, end, nil)
	c.at = outer
	c.release(m)
	c.dead = dead
	c.appends = nil

	// The rule is syntactic, as it is in Go, but for a call to the builtin
	// exit, which never returns and so ends a body as a return does (see
	// terminates); a call to any other function does not. A bad statement
	// may have been the return (see invalid.go).
	// A function with no body, which the resolver reported, has none to
	// end in one.
	if fn.Results != nil && !blockTerminates(fn.Body) && len(ast.BadStmts(fn.Body)) == 0 &&
		(!fn.NoBody || c.prior == 0) {
		c.errorf(fn.NameSpan, "%s must end in a return", c.displayName(fn))
	}
}

// checkStmt checks a statement of fn and everything nested in it, with
// c.at at the statement's end for whatever it checks outside an
// expression (see checkStmtAt).
func (c *Checker) checkStmt(stmt ast.Stmt, fn *ast.Func) {
	c.stmtGen++
	outer := c.at
	c.at = stmt.Spanned().End
	c.checkStmtAt(stmt, fn)
	c.at = outer
}

// checkStmtAt checks a statement of fn and everything nested in it.
func (c *Checker) checkStmtAt(stmt ast.Stmt, fn *ast.Func) {
	switch s := stmt.(type) {
	case *ast.Return:
		c.checkReturn(s, fn)

	case *ast.If:
		c.checkCond(s.Cond)
		then := &guard{cond: s.Cond, span: s.Cond.Spanned(), kind: guardCond}
		c.checkGuarded(s.Then, then, c.factsFrom(s.Cond, true), fn)
		els := &guard{cond: s.Cond, span: s.Cond.Spanned(), kind: guardElse}
		c.checkGuarded(s.Else, els, c.factsFrom(s.Cond, false), fn)

	case *ast.For:
		c.checkFor(s, fn)

	case *ast.ForRange:
		c.checkForRange(s, fn)

	case *ast.Switch:
		c.checkSwitch(s, fn)

	case *ast.Defer:
		c.checkDefer(s, fn)

	case *ast.Region:
		c.checkRegion(s, fn)

	case *ast.Break:
		c.checkInLoop(s.Span, "break")

	case *ast.Continue:
		c.checkInLoop(s.Span, "continue")

	case *ast.Unreachable:
		c.checkUnreachable(s)

	case *ast.Fallthrough:
		c.checkFallthrough(s)

	default:
		c.checkSimpleStmt(stmt)
	}
}

// checkSimpleStmt checks a statement that holds no other statement: a
// declaration, an assignment, an increment or decrement, or an expression.
func (c *Checker) checkSimpleStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if q, ok := s.X.(*ast.Question); ok {
			c.checkQuestion(q)
			return
		}
		if onlyValue(s.X) {
			c.unusedValue(s.X)
			return
		}
		c.droppedCall, _ = s.X.(*ast.Call)
		c.discard(s.X)
		c.droppedCall = nil
		c.droppedError(s.X)
	case *ast.Var:
		c.checkVar(s)
	case *ast.Assign:
		c.checkAssign(s)
		c.invariantWrite(s)
	case *ast.IncDec:
		c.readHeld(s.Target)
		if typ, ok := c.checkIntTarget(s.Target, s.Op()); ok {
			c.requireStepNoOverflow(s, typ)
		}
		c.checkErrorAssign(s, s.Target)
		c.invariantWrite(s)
	case *ast.MultiVar:
		c.checkMultiVar(s)
	case *ast.MultiAssign:
		c.checkMultiAssign(s)
	}
}

// checkMultiVar checks a declaration of several variables. Its value must be
// a call to a function with exactly one result for each variable, or a
// conversion that can fail, which gives the converted value and whether it
// fit, and each variable takes the type of the value in its position. When
// the value is wrong every variable keeps types.Invalid, so its uses are not
// reported again.
func (c *Checker) checkMultiVar(m *ast.MultiVar) {
	b := binding{op: ":="}
	if len(m.Vars) == 2 {
		b.first, b.second = m.Vars[0].Name, m.Vars[1].Name
	}
	results, ok := c.multiResults(m.Value, len(m.Vars), m.Span, b)
	if !ok {
		return
	}
	for i, v := range m.Vars {
		v.Type = c.settleResult(m.Value, results[i], types.Invalid)
		c.anyLocal(v)
	}
	blank := make([]bool, len(m.Vars))
	spans := make([]ast.Span, len(m.Vars))
	for i, v := range m.Vars {
		blank[i], spans[i] = v.Name == ast.Blank, v.NameSpan
	}
	c.checkErrorBinding(m.Value, blank, spans)
	c.checkOkDropped(m.Value, blank, spans)
	c.nothingBound(m.Value, blank, m.Span)
	c.noteHolder(m.Vars[len(m.Vars)-1], m.Value, len(m.Vars))
}

// checkMultiAssign checks an assignment of several values, given as a
// MultiVar's are. Each target must be a mutable variable, named only once,
// whose type the value in its position widens to. When the value is wrong,
// the targets are still checked for being assignable.
func (c *Checker) checkMultiAssign(m *ast.MultiAssign) {
	b := binding{op: "="}
	if len(m.Targets) == 2 {
		b.first, b.second = c.text(m.Targets[0].Spanned()), c.text(m.Targets[1].Spanned())
	}
	results, ok := c.multiResults(m.Value, len(m.Targets), m.Span, b)
	seen := make(map[ast.Decl]bool, len(m.Targets))
	for i, target := range m.Targets {
		got := types.Invalid
		if ok {
			got = results[i]
		}
		c.checkMultiTarget(m.Span, m.Value, target, got, seen)
	}
	if !ok {
		return
	}
	blank := make([]bool, len(m.Targets))
	spans := make([]ast.Span, len(m.Targets))
	for i, target := range m.Targets {
		blank[i], spans[i] = ast.IsBlank(target), target.Spanned()
	}
	c.checkErrorBinding(m.Value, blank, spans)
	c.checkOkDropped(m.Value, blank, spans)
	c.nothingBound(m.Value, blank, m.Span)
	c.noteTargetHolder(m.Targets[len(m.Targets)-1], m.Value, len(m.Targets))
	c.checkErrorTargets(m)
	for _, target := range m.Targets {
		if !ast.IsBlank(target) {
			c.checkErrorAssign(m, target)
		}
	}
}

// checkMultiTarget checks one target of a MultiAssign, stmt, whose value,
// value, gives got in the target's position, Invalid when the value is
// wrong. seen holds the variables already named, each of which may be
// named only once.
func (c *Checker) checkMultiTarget(
	stmt ast.Span,
	value, target ast.Expr,
	got types.Type,
	seen map[ast.Decl]bool,
) {
	// _ discards its value, whatever its type, and may appear more than
	// once.
	if ast.IsBlank(target) {
		c.settleResult(value, got, types.Invalid)
		return
	}
	if name, isName := target.(*ast.Ident); isName {
		if seen[name.Def] {
			c.errorf(name.Span, "%s is assigned more than once", name.Name)
			return
		}
		seen[name.Def] = true
	}

	c.assigning = &pendingAssign{target: target, value: value}
	outer := c.storeTo(stmt)
	typ, assignable := c.assignable(target)
	c.storing = outer
	c.assigning = nil
	got = c.settleResult(value, got, typ)
	if !assignable || typ == types.Invalid || got == types.Invalid || types.Widens(got, typ) {
		return
	}
	c.errorf(
		target.Spanned(),
		"cannot use %s as %s in assignment to %s",
		c.typeName(got),
		c.typeName(typ),
		targetName(target),
	)
}

// multiResults checks the value of a MultiVar or MultiAssign, which binds
// want names as b says, and returns the type of each value it gives, in
// order, and whether it gives exactly want of them. It must be a call to a
// function with that many results, a ? after a call with that many before
// its error, or a division, an index, a slice, a
// conversion or a type assertion, which gives two.
// Any other value is reported as one error covering span, the whole
// statement, unless it binds two names and holds a division, a remainder,
// an index or a slice, when it is the checked form misused; see notWhole.
func (c *Checker) multiResults(
	value ast.Expr,
	want int,
	span ast.Span,
	b binding,
) ([]types.Type, bool) {
	if q, ok := value.(*ast.Question); ok {
		return c.questionResults(q, want, span)
	}
	if bin, ok := value.(*ast.Binary); ok && (bin.Op == ast.Div || bin.Op == ast.Rem) {
		return c.divisionResults(bin, want, span)
	}
	if results, ok, isChecked := c.arithResults(value, want, span); isChecked {
		return results, ok
	}
	if results, ok, isChecked := c.checkedResults(value, want, span); isChecked {
		return results, ok
	}
	call, isCall := value.(*ast.Call)
	if !isCall {
		if op := fallible(value); want == 2 && op != nil {
			c.notWhole(value, op, b)
			return nil, false
		}
		c.expr(value)
		c.errorf(span, "assignment mismatch: %d variables but 1 value", want)
		return nil, false
	}
	if call.Conv != types.Invalid {
		return c.conversionResults(call, want, span)
	}
	if isViewConversion(call) {
		c.viewConversionElsewhere(call)
		return nil, false
	}
	if isRefConversion(call) {
		c.refConversion(call, call.Fn.(*ast.RefType))
		return nil, false
	}

	return c.callResults(call, want, span)
}

// checkedResults checks value, bound to want names, when it is an index,
// a slice, a type assertion or a call to pop or remove, each of which
// gives two, and reports whether it was one.
func (c *Checker) checkedResults(
	value ast.Expr,
	want int,
	span ast.Span,
) (results []types.Type, ok, isChecked bool) {
	switch x := value.(type) {
	case *ast.Index:
		results, ok = c.indexResults(x, want, span)
	case *ast.Slice:
		results, ok = c.sliceResults(x, want, span)
	case *ast.TypeAssert:
		results, ok = c.assertResults(x, want, span)
	case *ast.Call:
		if !isTake(x) {
			return nil, false, false
		}
		results, ok = c.takeResults(x, want, span)
	default:
		return nil, false, false
	}
	return results, ok, true
}

// binding is how a MultiVar or MultiAssign binds its value: op, := or =,
// and, when it binds two names, the first and second as written.
type binding struct {
	first, second, op string
}

// fallible returns the first division, remainder, index, slice or type
// assertion x.(T) in value, in source order, outside any call, or nil when
// there is none.
func fallible(value ast.Expr) ast.Expr {
	var found ast.Expr
	ast.Walk(value, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.Call:
			return false
		case *ast.Binary:
			if x.Op == ast.Div || x.Op == ast.Rem {
				found = x
			}
		case *ast.Index:
			found = x
		case *ast.Slice:
			found = x
		case *ast.TypeAssert:
			if x.TypeExpr != nil {
				found = x
			}
		}
		return found == nil
	})
	return found
}

// notWhole reports value, bound to two names as b says, which holds op, a
// division, a remainder, an index, a slice or a type assertion, but is
// more than it: the checked form
// must be the whole value. The error names the form with op alone, and
// what to compute from its first value, the value with op replaced by the
// first name, or, when that is _, only says to use it. value is then
// checked with op bound with ok, so op needs no proof, except that a
// divisor known to be zero is still reported.
func (c *Checker) notWhole(value, op ast.Expr, b binding) {
	whole, part := value.Spanned(), op.Spanned()
	rest := "then use its value"
	if b.first != "_" {
		text := c.text(whole)
		rest = "then compute " + text[:part.Start-whole.Start] + b.first + text[part.End-whole.Start:]
	}
	c.errorf(whole, "the checked form must be the whole value: %s, %s %s %s, %s",
		b.first, b.second, b.op, c.text(part), rest)

	switch x := op.(type) {
	case *ast.Binary:
		if v, known := c.knownValue(x.Y); !known || !v.IsZero() {
			c.checkedDivision = x
		}
	case *ast.Index:
		c.checkedIndex = x
	case *ast.Slice:
		c.checkedSlice = x
	case *ast.TypeAssert:
		c.checkedAssert = x
	}
	c.expr(value)
	c.checkedDivision, c.checkedIndex, c.checkedSlice, c.checkedAssert = nil, nil, nil, nil
}

// settleResult returns the type of the value in one position of a MultiVar
// or MultiAssign, got, once it has one. Only the quotient of a checked
// division of two untyped constants, value, lacks one: it settles to typ,
// the type of the variable it is assigned to, when that is an integer type,
// and to int64 otherwise, and is Invalid if it does not fit.
func (c *Checker) settleResult(value ast.Expr, got, typ types.Type) types.Type {
	if !types.IsUntyped(got) {
		return got
	}
	if got == types.UntypedInt && types.IsInteger(typ) {
		if !c.settle(value, typ) {
			return types.Invalid
		}
		return typ
	}
	return c.valueOf(value, got)
}

// callResults checks a call bound to want names: its callee, a function
// or a method, must declare exactly that many results. The generic
// builtins len, make, append and reserve give one value each, and pop and
// remove, bound to several names, are the checked form (see
// takeResults); every other builtin gives none. A call to a generic one
// whose arguments are wrong was reported where it was checked, and is not
// reported again.
func (c *Checker) callResults(call *ast.Call, want int, span ast.Span) ([]types.Type, bool) {
	var results []types.Type
	if generic := c.genericCall(call); generic != nil {
		typ := generic(call)
		if typ == types.Invalid {
			return nil, false
		}
		results = []types.Type{typ}
		if call.ResultTypes != nil {
			// add, add_all or insert, which give two.
			results = call.ResultTypes
		}
	} else {
		c.checkCall(call)
	}
	name, ok := c.calleeName(call)
	if !ok || isValueCall(call) && call.Dynamic == nil {
		return nil, false
	}

	// Any other builtin's Def is a nil *Func, and it returns nothing.
	if fn := ast.Callee(call); fn != nil {
		results = ast.CallResults(call)
	}
	switch got := len(results); {
	case got == 0:
		c.errorf(
			span,
			"assignment mismatch: %d variables but %s returns no value",
			want,
			name,
		)
	case got != want:
		c.errorf(
			span,
			"assignment mismatch: %d variables but %s returns %d %s",
			want,
			name,
			got,
			plural(got, "value"),
		)
	default:
		return results, true
	}
	return nil, false
}

// conversionResults checks a conversion bound to want names, which must be
// two: the converted value and whether it fit.
func (c *Checker) conversionResults(call *ast.Call, want int, span ast.Span) ([]types.Type, bool) {
	if !c.checkedConversion(call) {
		return nil, false
	}
	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but a conversion gives 2 values", want)
		return nil, false
	}
	return []types.Type{call.Conv, types.Bool}, true
}

// discard checks an expression whose value is not used: a call statement,
// or a value checked only for what is inside it. A call may return
// nothing, or several values, here, and a union is built for what is
// inside it.
func (c *Checker) discard(e ast.Expr) {
	if _, _, _, built := ast.Construction(e); built {
		c.construction(e)
		return
	}
	if call, ok := e.(*ast.Call); ok && call.Conv == types.Invalid &&
		c.genericCall(call) == nil && !isViewConversion(call) && !isRefConversion(call) {
		c.checkCall(call)
		return
	}
	c.expr(e)
}

// onlyValue reports whether e, written as a statement, does nothing but
// give a value: a union built, a conversion, or a call of the builtin
// len. Dropping that value leaves the statement nothing to do, so it is
// reported (see unusedValue), even when an argument is a call that does
// something, as Go reports it. A call of a function or a method, and of
// the builtins print, exit, make, append, reserve, pop and remove, is a
// call statement instead, with rules of its own.
func onlyValue(e ast.Expr) bool {
	if _, _, _, built := ast.Construction(e); built {
		return true
	}
	call, ok := e.(*ast.Call)
	return ok && (call.Conv != types.Invalid || isLen(call))
}

// unusedValue checks e, an expression statement that only gives a value
// (see onlyValue), and reports that the value is not used. An e that had
// errors of its own, a conversion that can fail among them, or whose type
// is invalid, is not reported again.
func (c *Checker) unusedValue(e ast.Expr) {
	before := len(c.diags)
	if c.expr(e) != types.Invalid && len(c.diags) == before {
		c.unusedf(e.Spanned(), "%s is not used", c.text(e.Spanned()))
	}
}

// plural returns word, with an s added unless n is one.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// checkFor checks a loop's clauses, in source order, then its body, inside
// which break and continue are allowed. The body knows what the condition
// says, and, when the loop counts its variable, the bound it counts from;
// see counting.go.
func (c *Checker) checkFor(loop *ast.For, fn *ast.Func) {
	if loop.Init != nil {
		c.checkSimpleStmt(loop.Init)
	}
	m := c.mark()
	leave := c.enterLoop(loop)
	if loop.Cond != nil {
		c.checkCond(loop.Cond)
	}
	var pending []overflowCheck
	if loop.Post != nil {
		outer := c.deferredOverflow
		c.deferredOverflow = &pending
		c.checkSimpleStmt(loop.Post)
		c.deferredOverflow = outer
	}
	body := c.mark()
	if loop.Cond != nil {
		c.assumeLoop(loop)
	}
	c.loops++
	c.checkBlock(loop.Body, fn)
	c.loops--
	c.release(body)
	c.recheckPost(loop, pending)
	leave()
	c.release(m)
}

// assumeLoop puts in force for a loop's body what its condition says and,
// when the loop counts, the bound it counts from. A loop that would count
// but for a step that could wrap gets that bound dropped, to explain a
// missing proof.
func (c *Checker) assumeLoop(loop *ast.For) {
	facts := c.factsFrom(loop.Cond, true)
	cond := &guard{cond: loop.Cond, span: loop.Cond.Spanned(), kind: guardCond}
	c.assume(cond, facts, stmtNodes(loop.Body))
	f, how := c.countingFact(loop, facts)
	if how == notCounting {
		return
	}
	if how == wraps {
		f.drop = wrapDrop(loop, f)
	}
	// The condition runs again before every later pass, after the bound
	// was last true, so it is in the region as well as the body. The
	// start is not tried again, so a write anywhere in the region reaches
	// every later pass, before or after the use, and ends the bound.
	region := append([]ast.Node{loop.Cond}, stmtNodes(loop.Body)...)
	counted := c.dropAssigned([]fact{f}, region)
	c.assume(&guard{span: loop.Init.Spanned(), kind: guardCount}, counted, region)
}

// checkForRange checks a range loop. It ranges over an integer, of any
// type, an untyped constant count being an int64, which gives the loop
// variable the count's type; or over an array, a view, a list or a string, which
// gives the index an int64 and the element the array's or view's element
// type, or a string's byte, a uint8; or over a map, which gives the key
// its key type, referring to the key in place when it is owned, and the
// element its value type; or over an arena, which gives the index an id
// of its element type, the sentinel's first, and the element its element
// type; or over a pool, which gives the index the handle of each live
// element and the element its element type. An integer gives only one
// value, so an element variable
// over one is reported. What it ranges over may be a call returning a
// view, or a span's view of an arena.
func (c *Checker) checkForRange(loop *ast.ForRange, fn *ast.Func) {
	c.allowViewCall(loop.X)
	got := c.valueOf(loop.X, c.expr(loop.X))
	index, elem := c.rangeTypes(loop, got)
	if loop.Var != nil {
		loop.Var.Type = index
		c.bindings[loop.Var] = rangeVar
		if c.table.IsMap(got) && c.table.Owned(index) {
			c.rangeBindings[loop.Var] = loop.X
			c.rangeKeys[loop.Var] = true
		}
	}
	if loop.Elem != nil {
		loop.Elem.Type = elem
		c.bindings[loop.Elem] = rangeVar
		c.anyLocal(loop.Elem)
		if c.table.Owned(elem) {
			c.rangeBindings[loop.Elem] = loop.X
		}
	}
	header := ast.Span{File: loop.Span.File, Start: loop.Span.Start, End: loop.X.Spanned().End}
	// The facts hold of what the loop copied when it began, which a write
	// in the body changes for every later pass, whether it comes before
	// or after the use: they end at any write in the body.
	facts := c.dropAssigned(c.rangeFacts(loop, got), stmtNodes(loop.Body))
	m := c.mark()
	leave := c.enterLoop(loop)
	c.loops++
	c.checkGuarded(loop.Body, &guard{span: header, kind: guardRange}, facts, fn)
	c.loops--
	leave()
	c.release(m)
}

// rangeTypes returns the types a range over a value of type got gives its
// index and its element, reporting a type that cannot be ranged over and
// an element variable over an integer. Either type is types.Invalid where
// there is none.
func (c *Checker) rangeTypes(loop *ast.ForRange, got types.Type) (index, elem types.Type) {
	switch {
	case got == types.Invalid:
		return types.Invalid, types.Invalid
	case types.IsInteger(got):
		if loop.Elem != nil {
			c.errorf(
				ast.Span{
					File:  loop.Var.Span.File,
					Start: loop.Var.Span.Start,
					End:   loop.X.Spanned().End,
				},
				"assignment mismatch: 2 variables but range over %s gives 1 value",
				c.typeName(got),
			)
		}
		return got, types.Invalid
	case c.table.IsArray(got), c.table.IsView(got), c.table.IsList(got):
		return types.Int64, c.table.Elem(got)
	case c.table.IsMap(got):
		return c.table.Key(got), c.table.Elem(got)
	case c.table.IsArena(got):
		return c.table.ID(c.table.Elem(got)), c.table.Elem(got)
	case c.table.IsPool(got):
		return c.table.Handle(c.table.Elem(got)), c.table.Elem(got)
	case got == types.String:
		return types.Int64, types.Uint8
	default:
		c.errorf(loop.X.Spanned(), "cannot range over %s", c.typeName(got))
		return types.Invalid, types.Invalid
	}
}

// rangeFacts returns what a range loop's body knows about its index
// variable, a range over a value of type got, other than a map, whose key
// is no index and gets no fact, an arena, whose ids are no integers, and
// a pool, whose handle gets the fact has gives (see poolRangeFacts): it
// is at least 0, and below
// the count or the length when that is known. A count's is known when it is
// a known value, an array's always, and a string's when it is a known
// string, a literal or an immutable variable holding one.
// When the count is len(s) of a string variable, or the loop ranges over a
// string variable s, the index is below len(s) instead, which checkForRange keeps
// only while the body never assigns s, anywhere; s then still holds the string the
// loop copied. Over a view, whose length is never known, the index is
// below len(v), which holds for the whole call, and over a list below
// len(l), which the body can never change (see move.go). A count that is
// an integer subject n, neither known nor a length, puts the index below
// n, which checkForRange keeps only while the body never assigns n: the
// loop counts to the value n had when it began.
func (c *Checker) rangeFacts(loop *ast.ForRange, got types.Type) []fact {
	if loop.Var == nil || loop.Var.Name == ast.Blank || c.table.IsMap(got) || c.table.IsArena(got) {
		return nil
	}
	if c.table.IsPool(got) {
		return c.poolRangeFacts(loop)
	}
	f := fact{about: declKey(loop.Var), hasLo: true}
	one := constant.FromInt64(1)
	switch {
	case types.IsInteger(got):
		if n, ok := c.knownValue(loop.X); ok {
			f.hi, f.hasHi = n.Sub(one)
		} else if k, ok := c.keyOf(loop.X); ok {
			f.atMost, f.strict = k, true
		}
		f.lenOf, _ = c.stringLen(loop.X)
	case c.table.IsArray(got):
		f.hi, f.hasHi = constant.FromInt64(c.table.Len(got)).Sub(one)
	case got == types.String:
		if n, ok := c.extentOf(loop.X); ok {
			f.hi, f.hasHi = constant.FromInt64(n).Sub(one)
		}
		f.lenOf, _ = c.stringVar(loop.X)
	case c.table.IsView(got), c.table.IsList(got):
		f.lenOf, _ = c.stringVar(loop.X)
	}
	return []fact{f}
}

// checkBlock checks each statement of a block in fn. An if whose branch
// leaves the block gives the statements after it the facts of having got
// past it, and a declaration or an assignment the relations of what it
// stored (see storedFacts).
func (c *Checker) checkBlock(body []ast.Stmt, fn *ast.Func) {
	m, dead := c.mark(), c.dead
	c.checkStmts(body, fn)
	c.release(m)
	c.dead = dead
}

// checkStmts checks the statements of a block, putting in force for the
// rest of it the facts each gives the statements after it, leaving them
// in force for the caller to release, and counting the block in c.dead
// once a statement leaves it or ends in exit.
//
// The regions of what each statement gives the rest all end in the rest
// of the block, which they share, behind what an early exit falls
// through to first, and whose writes are found once for all of them (see
// region.go), so a block of n statements, each giving facts, is neither
// copied nor walked n times.
func (c *Checker) checkStmts(body []ast.Stmt, fn *ast.Func) {
	dead := c.dead
	blk := c.newBlockRegion(body)
	for i, stmt := range body {
		c.checkStmt(stmt, fn)
		rest := blk.restFrom(i + 1)
		c.recordStart(stmt, rest)
		g, facts := c.afterFacts(stmt)
		if g == nil {
			g, facts = c.storedFacts(stmt)
		}
		if g != nil {
			c.assumeIn(g, facts, c.regionOf(c.fallsThrough(stmt), rest))
		}
		if g, facts := c.contractFacts(stmt); g != nil {
			c.assumeIn(g, facts, region{rest: rest})
		}
		c.chainFacts(stmt, rest)
		if c.dead == dead && (c.leaves(body[i:i+1]) || c.terminates(stmt)) {
			c.dead++
		}
	}
}

// checkGuarded checks a block reached only when facts, which come from g,
// hold, assuming those the block cannot undo.
func (c *Checker) checkGuarded(body []ast.Stmt, g *guard, facts []fact, fn *ast.Func) {
	m := c.mark()
	c.assume(g, facts, stmtNodes(body))
	c.checkBlock(body, fn)
	c.release(m)
}

// checkCond checks the condition of an if or a loop, which must be a bool.
func (c *Checker) checkCond(cond ast.Expr) {
	got := c.valueOf(cond, c.expr(cond))
	if got != types.Invalid && got != types.Bool {
		c.errorf(cond.Spanned(), "condition must be a bool, got %s", c.typeName(got))
	}
}

// checkInLoop reports a break or continue that no loop encloses, and one
// in deferred code whose loop is outside it.
func (c *Checker) checkInLoop(span ast.Span, keyword string) {
	switch {
	case c.loops == 0:
		c.errorf(span, "%s is not in a loop", keyword)
	case c.deferred > 0 && c.loops == c.deferLoops:
		c.errorf(span, "cannot %s out of a defer", keyword)
	}
}

// checkVar records a variable's type: its declared type if it has one,
// otherwise its value's, an untyped constant being an int64, and never a
// view, which only a parameter may be. A value must be
// usable as the declared type, and only a mutable variable may leave out its
// value and start from its zero value, since an immutable one could never
// hold anything else, and only one whose type has a zero value: not a
// list, an Allocator, or an array or struct holding one.
func (c *Checker) checkVar(v *ast.Var) {
	declared := types.Invalid
	if v.TypeExpr != nil {
		declared = c.declaredType(v.TypeExpr)
	}

	if v.Value == nil {
		switch {
		case !v.Mutable:
			c.errorf(v.NameSpan, "immutable variable %s needs a value", v.Name)
		case c.globals[v] && c.table.Owned(declared):
			// checkGlobal reports a global that holds a list, which is
			// why it has no value.
		case c.noZeroValue(v, declared):
			declared = types.Invalid
		}
		v.Type = declared
		return
	}

	got := c.expr(v.Value)
	c.noteHolder(v, v.Value, 1)
	if v.TypeExpr == nil {
		v.Type = c.valueOf(v.Value, got)
		c.recordAppend(v)
		if c.anyLocal(v) {
			return
		}
		if c.table.IsView(v.Type) {
			c.errorf(
				v.NameSpan,
				"cannot declare %s as %s; %s",
				v.Name,
				c.typeName(v.Type),
				viewPlaces,
			)
			v.Type = types.Invalid
		}
		return
	}
	c.use(v.Value, got, declared, "in declaration of "+v.Name)
	v.Type = declared
}

// checkGlobal checks a package-level variable like a local, and also
// requires its initializer to be a constant expression, one the compiler
// can compute, which it evaluates into v.Const, and its type to hold no
// list, which any function could otherwise reach and change while a
// view of it, or a mut argument of it, is live, and no function value
// (see globalFuncValue). An initializer that calls
// anything but len or a conversion, or names a mutable global, is
// reported before it is checked, since the functions it could call have
// no signatures yet. A global without a value takes its type's zero
// value.
func (c *Checker) checkGlobal(v *ast.Var) {
	if v.Value != nil {
		for _, q := range questionsIn(v.Value) {
			c.errorf(q.Mark(), "cannot use ? outside a function")
		}
	}
	if c.globalFuncValue(v) {
		return
	}
	if v.Value != nil && !c.isConstant(v.Value) {
		c.notConstant(v)
		if v.TypeExpr != nil {
			v.Type = c.declaredType(v.TypeExpr)
		}
		return
	}

	before := len(c.diags)
	c.checkVar(v)
	if len(c.diags) > before {
		return
	}
	if c.badGlobalType(v) {
		return
	}

	if v.Value == nil {
		v.Const = zeroConst(v.Type)
		return
	}
	value, ok := c.fold(v.Value)
	if !ok {
		if !c.namesFailedGlobal(v.Value) {
			c.notConstant(v)
		}
		return
	}
	v.Const = value
}

// badGlobalType reports v, a package-level variable, when its type holds a
// function value, a list, a map, an arena or a pool, which no global
// may, and returns whether it did.
func (c *Checker) badGlobalType(v *ast.Var) bool {
	if c.holdsFunc(v.Type, make(map[types.Type]bool)) {
		c.noGlobalFunc(v)
		return true
	}
	if !c.table.Owned(v.Type) {
		return false
	}
	held := "a list"
	switch {
	case c.table.HoldsArena(v.Type):
		held = "an arena"
	case c.table.HoldsPool(v.Type):
		held = "a pool"
	case !c.table.HoldsList(v.Type):
		held = "a map"
	}
	c.errorf(v.NameSpan, "package-level variable %s cannot hold %s", v.Name, held)
	return true
}

// namesFailedGlobal reports whether e names an immutable global with no
// computed value, whose initializer has been reported, or a name bound to
// ast.Unbound, whose problem has been, so that e having no value either is
// not reported again.
func (c *Checker) namesFailedGlobal(e ast.Expr) bool {
	if c.namesUnbound(e) {
		return true
	}
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if g := c.constantGlobal(id); g != nil && g.Const == nil {
				found = true
			}
		}
		return !found
	})
	return found
}

// namesUnbound reports whether e names something bound to ast.Unbound,
// whose problem has been reported: whether e would be constant is
// unknown.
func (c *Checker) namesUnbound(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Def == ast.Unbound {
			found = true
		}
		return !found
	})
	return found
}

// notConstant reports that the initializer of the global v is not a
// constant expression, unless it names something bound to ast.Unbound,
// which was reported, and might have made it one.
func (c *Checker) notConstant(v *ast.Var) {
	if c.namesUnbound(v.Value) {
		return
	}
	c.errorf(v.Value.Spanned(), "%s must be initialized with a constant expression", v.Name)
}

// isConstant reports whether e is built only from literals, names of
// immutable package-level variables, of its own package or an imported
// one, operators, array and struct literals, unions built of a case and
// its payload, fields and elements, len and conversions, all of them of
// such parts, except that len may take
// anything that calls nothing, since an array's length is its type's,
// whatever holds it. A call to anything else, a function or a builtin, is
// never constant, since nothing runs before main to compute it, and a
// mutable global never is, since any function can change it. Whether the
// expression's value can be computed is found once it is checked, by fold.
func (c *Checker) isConstant(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.CharLit, *ast.StrLit, *ast.BoolLit, *ast.NoErrorLit,
		*ast.CAllocatorLit:
		return true
	case *ast.Ident:
		return c.constantGlobal(x) != nil
	}
	if isConst, built := c.isConstantConstruction(e); built {
		return isConst
	}
	if call, ok := e.(*ast.Call); ok {
		return c.isConstantCall(call)
	}
	parts, ok := subExprs(e)
	return ok && c.allConstant(parts)
}

// isConstantCall reports whether call, which builds no union, is a
// constant expression, as isConstant says: len of anything that calls
// nothing, or len or a conversion of constant expressions.
func (c *Checker) isConstantCall(call *ast.Call) bool {
	if isLen(call) && len(call.Args) == 1 && !hasCall(call.Args[0]) {
		return true
	}
	return (call.Conv != types.Invalid || isLen(call)) && c.allConstant(call.Args)
}

// subExprs returns the operands of e when it is an operator, a literal of
// an array or a struct, one of its keyed elements, a field, an element or
// a slice, what it slices and its bounds, and false for anything else.
func subExprs(e ast.Expr) ([]ast.Expr, bool) {
	switch x := e.(type) {
	case *ast.KeyValue:
		return []ast.Expr{x.Value}, true
	case *ast.CompositeLit:
		return x.Elems, true
	case *ast.Binary:
		return []ast.Expr{x.X, x.Y}, true
	case *ast.Unary:
		return []ast.Expr{x.X}, true
	case *ast.Selector:
		return []ast.Expr{x.X}, true
	case *ast.Index:
		return []ast.Expr{x.X, x.Index}, true
	default:
		return sliceParts(e)
	}
}

// sliceParts returns what e slices and the bounds written, when it is a
// slice, and false for anything else.
func sliceParts(e ast.Expr) ([]ast.Expr, bool) {
	s, ok := e.(*ast.Slice)
	if !ok {
		return nil, false
	}
	parts := []ast.Expr{s.X}
	for _, b := range []ast.Expr{s.Lo, s.Hi} {
		if b != nil {
			parts = append(parts, b)
		}
	}
	return parts, true
}

// allConstant reports whether every one of elems is constant, as
// isConstant says.
func (c *Checker) allConstant(elems []ast.Expr) bool {
	for _, elem := range elems {
		if !c.isConstant(elem) {
			return false
		}
	}
	return true
}

// constantGlobal returns the variable id names when it is an immutable
// package-level variable, whose value is a constant, and nil otherwise.
func (c *Checker) constantGlobal(id *ast.Ident) *ast.Var {
	if v, ok := id.Def.(*ast.Var); ok && !v.Mutable && c.globals[v] {
		return v
	}
	return nil
}

// zeroConst is a type's zero value as a constant. An array's or a struct's
// is nil, since every element or field of it is zero, which the backend
// writes without listing them.
func zeroConst(typ types.Type) any {
	switch {
	case types.IsInteger(typ):
		return constant.Int{}
	case typ == types.String:
		return ""
	case typ == types.Bool:
		return false
	default:
		return nil
	}
}

// fold returns the value of a checked constant expression: a constant.Int,
// computed exactly as it was checked, a string, a bool, the
// *ast.CAllocatorLit of c_allocator, an array's
// elements, or a struct's fields in declaration order, as a []any of
// those, or a union's case and payload, as an ast.UnionConst. A field or an element is computed from the struct or array it is
// part of (foldPart). A bool expression
// is evaluated in full, the right side of && and || included, as Go does,
// since any error on either side has already been reported. Arithmetic in
// a type of its own, as on character literals, gives a value of that type
// at every step, since checking the expression reported any step that does
// not, as it does in a function, and checkGlobal folds only an initializer
// that checked without errors.
func (c *Checker) fold(e ast.Expr) (any, bool) {
	if v, ok, isNumber := c.foldNumber(e); isNumber {
		return v, ok
	}
	if _, _, _, built := ast.Construction(e); built {
		return c.foldConstruction(e)
	}
	switch x := e.(type) {
	case *ast.StrLit, *ast.BoolLit, *ast.NoErrorLit, *ast.CAllocatorLit, *ast.Ident:
		return c.foldLeaf(x)
	case *ast.CompositeLit:
		return c.foldLiteral(x)
	case *ast.Binary:
		if b, ok := x.Const.(bool); ok {
			return b, true
		}
		return c.foldBoolBinary(x)
	case *ast.Unary:
		return c.foldNot(x)
	case *ast.Selector, *ast.Index:
		return c.foldPart(x)
	case *ast.Slice:
		return c.knownSlice(x)
	}
	return nil, false
}

// foldNumber computes e when it is a float literal or operator expression
// (see foldFloat) or an integer constant expression (see foldInt), and
// reports whether it has a value and whether it is one.
func (c *Checker) foldNumber(e ast.Expr) (any, bool, bool) {
	if v, ok, isFloat := foldFloat(e); isFloat {
		return v, ok, true
	}
	v, ok, isInt := c.foldInt(e)
	return v, ok, isInt
}

// foldFloat computes e when it is a literal or an operator expression of
// a float type, as foldFloatConst does, and reports whether it has a
// value and whether it is one. Any other float, a name or a part, is
// folded as any other value is.
func foldFloat(e ast.Expr) (v any, ok, isFloat bool) {
	if !types.IsFloat(ast.TypeOf(e)) {
		return nil, false, false
	}
	switch e.(type) {
	case *ast.FloatLit, *ast.IntLit, *ast.Binary, *ast.Unary:
		v, ok = foldFloatConst(e)
		return v, ok, true
	}
	return nil, false, false
}

// foldFloatConst computes e, a float constant expression, as the value
// of its float type it rounds to: a float32 or a float64. Arithmetic on
// typed floats is not computed here, so a global whose initializer does
// any has no value.
func foldFloatConst(e ast.Expr) (any, bool) {
	v, ok := floatValue(e)
	if !ok {
		return nil, false
	}
	if ast.TypeOf(e) == types.Float32 {
		f, _ := v.Float32()
		return f, true
	}
	f, _ := v.Float64()
	return f, true
}

// foldLiteral computes a constant literal of a struct or an array, as a
// []any of its fields' values in declaration order or of its elements.
func (c *Checker) foldLiteral(lit *ast.CompositeLit) (any, bool) {
	if c.table.IsStruct(lit.Type) {
		return c.foldStruct(lit)
	}
	return c.foldElems(lit)
}

// foldPart computes a field or an element whose struct, array or string is
// known: a literal of constants, the value of an immutable global, or a
// part of one. An element's index must be a known value in range, and a
// string's byte is a constant.Int. A part of anything else, a local's
// included, has no value here.
func (c *Checker) foldPart(e ast.Expr) (any, bool) {
	if sel, ok := e.(*ast.Selector); ok {
		base, ok := c.fold(sel.X)
		fields, isStruct := base.([]any)
		_, i, found := c.table.Field(ast.TypeOf(sel.X), sel.Name)
		if !ok || !isStruct || !found || i >= len(fields) {
			return nil, false
		}
		return fields[i], true
	}
	ix, ok := e.(*ast.Index)
	if !ok {
		return nil, false
	}
	i, ok := c.knownValue(ix.Index)
	if !ok || i.IsNegative() || !i.Fits(64, true) {
		return nil, false
	}
	return c.foldElement(ix.X, i.Int64())
}

// foldElement computes element i, not negative, of x, a known array or
// string: a string's byte as a constant.Int, and an array's element as
// fold gives it. An index past the end has no value.
func (c *Checker) foldElement(x ast.Expr, i int64) (any, bool) {
	if ast.TypeOf(x) == types.String {
		s, ok := c.knownString(x)
		if !ok || i >= int64(len(s)) {
			return nil, false
		}
		return constant.FromInt64(int64(s[i])), true
	}
	base, ok := c.fold(x)
	elems, isArray := base.([]any)
	if !ok || !isArray || i >= int64(len(elems)) {
		return nil, false
	}
	return elems[i], true
}

// foldLeaf computes e, a string or bool literal, noerror, an error's
// fields as a struct's are, a code of 0 and an empty message, c_allocator,
// which is its own literal, since an Allocator has no other constant form, or the name
// of an immutable global, which, checked before the initializer naming it,
// holds its computed value; a mutable one is never folded.
func (c *Checker) foldLeaf(e ast.Expr) (any, bool) {
	switch x := e.(type) {
	case *ast.StrLit:
		return x.Value, true
	case *ast.BoolLit:
		return x.Value, true
	case *ast.NoErrorLit:
		return []any{constant.Int{}, ""}, true
	case *ast.CAllocatorLit:
		return x, true
	case *ast.Ident:
		if v := c.constantGlobal(x); v != nil && v.Const != nil {
			return v.Const, true
		}
	}
	return nil, false
}

// foldInt computes e when it is an integer constant expression, untyped or
// typed, and reports whether it has a value and whether it is one. A typed
// one is a known value: a step that overflows its type was reported when
// e was checked, so e is never folded.
func (c *Checker) foldInt(e ast.Expr) (v constant.Int, ok, isInt bool) {
	if v, ok = constValue(e); ok {
		return v, true, true
	}
	typ := ast.TypeOf(e)
	if !types.IsInteger(typ) || typ == types.UntypedInt {
		return constant.Int{}, false, false
	}
	v, ok = c.knownValue(e)
	return v, ok, true
}

// foldNot computes ! on a bool constant.
func (c *Checker) foldNot(u *ast.Unary) (any, bool) {
	operand, ok := c.fold(u.X)
	if b, isBool := operand.(bool); ok && isBool && u.Op == ast.Not {
		return !b, true
	}
	return nil, false
}

// foldElems computes the elements of a constant array literal, as a []any.
func (c *Checker) foldElems(lit *ast.CompositeLit) (any, bool) {
	elems := make([]any, len(lit.Elems))
	for i, elem := range lit.Elems {
		v, ok := c.fold(elem)
		if !ok {
			return nil, false
		}
		elems[i] = v
	}
	return elems, true
}

// foldBoolBinary computes an operator that gives a bool, on two bool
// constants, or a comparison of two integer or two string ones, or ==
// and != on two bools, arrays or structs, folding both sides first;
// strings, alone or in an array or a struct, only in a package-level
// initializer. It records no Binary.Const.
func (c *Checker) foldBoolBinary(bin *ast.Binary) (any, bool) {
	left, okL := c.fold(bin.X)
	right, okR := c.fold(bin.Y)
	if !okL || !okR {
		return nil, false
	}
	switch {
	case bin.Op == ast.Eq || bin.Op == ast.Ne:
		equal, ok := c.equalConsts(left, right)
		return equal == (bin.Op == ast.Eq), ok
	case bin.Op.IsComparison():
		cmp, ok := c.compareConsts(left, right)
		return compare(bin.Op, cmp), ok
	}
	l, isBoolL := left.(bool)
	r, isBoolR := right.(bool)
	if !isBoolL || !isBoolR {
		return nil, false
	}
	return foldBools(bin.Op, l, r)
}

// compareConsts compares two integer or two string constants, giving -1, 0
// or 1, and reports whether it could. Strings compare by their bytes,
// unsigned, as strings.Compare does and as the backend does at run time.
// Only a global's initializer, checked while initStack holds it, compares
// them here: inside a function, as for a switch's case values, a
// comparison of strings is left to run time.
func (c *Checker) compareConsts(left, right any) (int, bool) {
	if a, isInt := left.(constant.Int); isInt {
		b, _ := right.(constant.Int)
		return a.Cmp(b), true
	}
	if a, isString := left.(string); isString && len(c.initStack) > 0 {
		b, _ := right.(string)
		return strings.Compare(a, b), true
	}
	return 0, false
}

// equalConsts reports whether two constants of one type are equal, and
// whether it could tell: two bools, two arrays or two structs, element by
// element or field by field, two unions, by case and then payload, or two
// integers or strings as compareConsts compares them.
func (c *Checker) equalConsts(left, right any) (equal, ok bool) {
	switch a := left.(type) {
	case unionValue:
		return c.equalUnions(a, right)
	case bool:
		b, isBool := right.(bool)
		return a == b, isBool
	case []any:
		b, isComposite := right.([]any)
		if !isComposite || len(a) != len(b) {
			return false, false
		}
		equal = true
		for i := range a {
			same, known := c.equalConsts(a[i], b[i])
			if !known {
				return false, false
			}
			equal = equal && same
		}
		return equal, true
	}
	cmp, ok := c.compareConsts(left, right)
	return cmp == 0, ok
}

// equalUnions reports whether a, a union's computed value, equals right,
// another union's of the same type, and whether it could tell: equal when
// they hold the same case and, for a case with a payload, equal payloads.
func (c *Checker) equalUnions(a unionValue, right any) (equal, ok bool) {
	b, isUnion := right.(unionValue)
	switch {
	case !isUnion:
		return false, false
	case a.Case != b.Case:
		return false, true
	case a.Payload == nil || b.Payload == nil:
		return a.Payload == nil && b.Payload == nil, true
	}
	return c.equalConsts(a.Payload, b.Payload)
}

// foldBools applies an operator to two bool constants.
func foldBools(op ast.BinaryOp, x, y bool) (any, bool) {
	switch op {
	case ast.Eq:
		return x == y, true
	case ast.Ne:
		return x != y, true
	case ast.And:
		return x && y, true
	case ast.Or:
		return x || y, true
	default:
		return nil, false
	}
}

// checkAssign requires the target to be a mutable variable and the value to
// be usable as the variable's type. Parameters are never assignable. A
// compound assignment also requires the target to be an integer, since it
// applies arithmetic.
func (c *Checker) checkAssign(a *ast.Assign) {
	got := c.expr(a.Value)
	if a.Op != ast.InvalidOp {
		outer := c.storeTo(a.Span)
		c.readHeld(a.Target)
		c.storing = outer
		c.assigning = &pendingAssign{target: a.Target, value: a.Value}
		c.checkCompound(a, got)
		c.assigning = nil
		c.checkErrorAssign(a, a.Target)
		return
	}

	outer := c.storeTo(a.Span)
	c.assigning = &pendingAssign{target: a.Target, value: a.Value}
	typ, ok := c.assignable(a.Target)
	c.assigning = nil
	c.storing = outer
	if ok {
		c.use(a.Value, got, typ, "in assignment to "+targetName(a.Target))
	}
	c.noteTargetHolder(a.Target, a.Value, 1)
	c.checkErrorAssign(a, a.Target)
}

// storeTo marks stmt, an assignment, as the one whose targets are being
// checked, so that every write in it reaches them, and returns the mark
// it replaces, which the caller puts back once they are checked.
func (c *Checker) storeTo(stmt ast.Span) *ast.Span {
	outer := c.storing
	c.storing = &stmt
	return outer
}

// assignable checks that target can be assigned: a mutable variable or a
// mut parameter, not a plain parameter, or an element of one, whose index
// must be in range, or a field of one. A view, or any part of one, is
// read-only, and a slice is not a place. It returns the type target holds and whether it can be
// assigned.
func (c *Checker) assignable(target ast.Expr) (types.Type, bool) {
	if root, isView := c.viewRoot(target); isView {
		c.readOnly(target.Spanned(), "assign to "+c.text(target.Spanned()), root)
		return types.Invalid, false
	}
	switch t := target.(type) {
	case *ast.Ident:
		switch def := t.Def.(type) {
		case *ast.Param:
			if def.Mut {
				return def.Type, def.Type != types.Invalid
			}
			c.errorf(t.Span, "cannot assign to parameter %s", t.Name)
		case *ast.Var:
			if !def.Mutable {
				c.errorf(t.Span, "cannot assign to immutable variable %s", t.Qualified())
				return types.Invalid, false
			}
			return def.Type, true
		}
		return types.Invalid, false
	case *ast.Index:
		return c.elementTarget(t)
	case *ast.Selector:
		return c.fieldTarget(t)
	default:
		c.notAssignable(target)
		return types.Invalid, false
	}
}

// notAssignable reports target, which is not a variable, an element or a
// field: a slice, which is not a place, or anything else.
func (c *Checker) notAssignable(target ast.Expr) {
	if _, ok := target.(*ast.Slice); ok {
		c.notPlace(target.Spanned(), "assign to "+c.text(target.Spanned()))
		return
	}
	c.notATarget(target)
}

// elementTarget checks an element being assigned, x[i]: x must be a
// mutable variable, or an element or field of one, since an array follows
// its variable, and the index must be in range. A string's bytes are never
// assigned, and neither is an element of a slice, which is not a place. It
// returns the element's type.
func (c *Checker) elementTarget(ix *ast.Index) (types.Type, bool) {
	switch x := ix.X.(type) {
	case *ast.Ident:
		if ast.TypeOf(x) == types.String {
			return c.stringTarget(ix)
		}
	case *ast.Index, *ast.Selector:
	case *ast.Slice:
		c.notPlace(ix.Span, "assign to "+c.text(ix.Span))
		return types.Invalid, false
	default:
		c.notATarget(ix.X)
		return types.Invalid, false
	}

	base, ok := c.containerTarget(ix.X, "element")
	if !ok {
		c.expr(ix.Index)
		return types.Invalid, false
	}
	if base == types.String {
		return c.stringTarget(ix)
	}
	ix.Type = c.element(ix, base, true)
	return ix.Type, ix.Type != types.Invalid
}

// containerTarget checks x, which holds an element or a field, what, being
// assigned: x must be a mutable variable or a mut parameter, not a plain
// parameter, or itself an element or field of one. It returns x's type, and false when anything was
// reported or x's type is unknown.
func (c *Checker) containerTarget(x ast.Expr, what string) (types.Type, bool) {
	switch x := x.(type) {
	case *ast.Ident:
		switch def := x.Def.(type) {
		case *ast.Param:
			if def.Mut {
				return def.Type, def.Type != types.Invalid
			}
			c.errorf(x.Span, "cannot assign to %s of parameter %s", what, x.Name)
		case *ast.Var:
			if def.Mutable {
				return def.Type, def.Type != types.Invalid
			}
			c.errorf(x.Span, "cannot assign to %s of immutable variable %s", what, x.Qualified())
		}
		return types.Invalid, false
	case *ast.Index:
		return c.elementTarget(x)
	case *ast.Selector:
		return c.fieldTarget(x)
	default:
		c.notATarget(x)
		return types.Invalid, false
	}
}

// notATarget reports target, which is no variable, element, field or
// slice, as no place an assignment can write, as the resolver does: after
// it has run, which an earlier error says, it has reported this one.
func (c *Checker) notATarget(target ast.Expr) {
	if c.prior == 0 {
		c.errorf(target.Spanned(), "cannot assign to this expression")
	}
}

// stringTarget reports an assignment to a byte of a string, ix, which is
// never allowed, after checking the index for errors of its own.
func (c *Checker) stringTarget(ix *ast.Index) (types.Type, bool) {
	c.expr(ix.Index)
	c.errorf(ix.Span, "cannot assign to an element of a string; strings are immutable")
	return types.Invalid, false
}

// targetName names an assignment target in a message: the variable, an
// element of it, or a field of it.
func targetName(target ast.Expr) string {
	root := ast.Root(target)
	sel, isField := target.(*ast.Selector)
	switch {
	case isField && root == nil:
		return "field " + sel.Name
	case isField:
		return "field " + sel.Name + " of " + root.Name
	case root == nil:
		return "an element"
	}
	if _, isName := target.(*ast.Ident); isName {
		return root.Name
	}
	return "an element of " + root.Name
}

// checkCompound checks a compound assignment whose value has type got: the
// target must be an integer variable it can update, or, for += -= *= and
// /=, a float one, and the value usable as the target's type, or, for <<=
// and >>=, a shift's count (see shiftCount). A float's /= needs no proof
// that its divisor is not zero; a signed integer's needs one that it is
// not the smallest value divided by -1 (see requireNoQuotientOverflow).
func (c *Checker) checkCompound(a *ast.Assign, got types.Type) {
	op := a.Op.String() + "="
	outer := c.storeTo(a.Span)
	typ, ok := c.checkNumberTarget(a.Target, op, floatOp(a.Op))
	c.storing = outer
	if ok {
		if a.Op.IsShift() {
			if c.shiftCount(a.Value, got) && got == types.UntypedInt {
				c.settle(a.Value, types.Uint64)
			}
			return
		}
		usable := c.use(a.Value, got, typ, "in "+op)
		if usable && (a.Op == ast.Div || a.Op == ast.Rem) && !types.IsFloat(typ) &&
			c.requireNonzero(a.Value, false) && a.Op == ast.Div {
			bin := &ast.Binary{X: a.Target, Op: ast.Div, Y: a.Value, Type: typ, Span: a.Span}
			c.requireNoQuotientOverflow(bin, typ, a)
		}
		if usable && (a.Op == ast.Add || a.Op == ast.Sub || a.Op == ast.Mul) {
			c.requireStepNoOverflow(a, typ)
		}
	}
}

// floatOp reports whether op is defined on floats: + - * or /.
func floatOp(op ast.BinaryOp) bool {
	return op == ast.Add || op == ast.Sub || op == ast.Mul || op == ast.Div
}

// checkIntTarget checks the target of an operator that updates an integer
// in place, such as += or ++: it must be a mutable variable of an integer
// type, or an integer element of one. It returns the target's type, and
// false when anything about it was reported, or its type is unknown, so the
// caller need not check further.
func (c *Checker) checkIntTarget(target ast.Expr, op string) (types.Type, bool) {
	return c.checkNumberTarget(target, op, false)
}

// checkNumberTarget is checkIntTarget for an operator that updates an
// integer, or, when floats says so, a float, in place.
func (c *Checker) checkNumberTarget(target ast.Expr, op string, floats bool) (types.Type, bool) {
	typ, ok := c.assignable(target)
	if !ok || typ == types.Invalid {
		return types.Invalid, false
	}
	if !types.IsInteger(typ) && (!floats || !types.IsFloat(typ)) {
		c.errorf(target.Spanned(), "operator %s is not defined on %s", op, c.typeName(typ))
		return types.Invalid, false
	}
	return typ, true
}

// declaredType returns the type a type expression denotes, reporting a type it
// does not know and returning types.Invalid for it. A name denotes a basic
// type, or the struct type the resolver bound it to, list[T] a list
// type, map[K]V a map type, arena[T], id[T] and span[T] an arena, an id
// and a span type, pool[T] and handle[T] a pool and a handle type, and
// func(A) R a function type. A view type and any
// are reported, since only a parameter may have one, or, for a view, a
// result; paramType gives a parameter's and resultType a result's.
func (c *Checker) declaredType(expr ast.Expr) types.Type {
	if typ, ok := c.compositeType(expr); ok {
		return typ
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		c.errorf(expr.Spanned(), "unsupported type expression: %T", expr)
		return types.Invalid
	}

	if typ, declared := c.namedType(id); declared {
		return typ
	}
	if c.unbound(id) {
		return types.Invalid
	}
	typ, known := types.Lookup(id.Name)
	if !known || id.Pkg != "" {
		c.errorf(id.Span, "unknown type: %s", id.Qualified())
		return types.Invalid
	}
	if typ == types.Any {
		c.errorf(id.Span, "%s", anyMessage)
		return types.Invalid
	}
	return typ
}

// compositeType returns the type expr denotes when it is an array, a
// view, a list, a map, an arena, a pool, an id, a span, a handle or a
// function type, as declaredType says, and whether it is one.
func (c *Checker) compositeType(expr ast.Expr) (types.Type, bool) {
	switch t := expr.(type) {
	case *ast.ArrayType:
		return c.arrayType(t), true
	case *ast.ViewType:
		return c.viewElsewhere(t), true
	case *ast.ListType:
		return c.listType(t), true
	case *ast.MapType:
		return c.mapType(t), true
	case *ast.ArenaType:
		return c.arenaType(t), true
	case *ast.PoolType:
		return c.poolType(t), true
	case *ast.RefType:
		return c.refType(t), true
	case *ast.FuncType:
		return c.funcType(t), true
	default:
		return types.Invalid, false
	}
}

// terminates reports whether control cannot fall out of the bottom of stmt.
// A return terminates, and so does a call to exit, which never returns,
// an unreachable, which the checker proves is never reached, and, as in
// Go, a fallthrough that ends a case other than its switch's last, which
// goes on into the next case's body (see switchTerminates). A region
// terminates when its body does, since the body runs once, in place. An
// if only terminates when it has an else and both branches do, since
// otherwise the condition being false falls through, and a switch likewise
// only when it has a default and every case does. A loop only terminates
// when it has no condition and no break leaves it, following Go. The
// checker asks through Checker.terminates, which remembers the answers
// (see leavememo.go).
func terminates(stmt ast.Stmt) bool {
	return terminatesBy(stmt, blockTerminates)
}

// terminatesBy is terminates with blockEnds deciding whether each block
// inside stmt terminates, as blockTerminates does, which the checker,
// remembering what it found, passes (see Checker.terminates).
func terminatesBy(stmt ast.Stmt, blockEnds func([]ast.Stmt) bool) bool {
	switch s := stmt.(type) {
	case *ast.Return, *ast.Unreachable:
		return true
	case *ast.Fallthrough:
		return s.Next != nil
	case *ast.ExprStmt:
		return isExitStmt(s)
	case *ast.If:
		return len(s.Else) > 0 && blockEnds(s.Then) && blockEnds(s.Else)
	case *ast.For:
		return s.Cond == nil && !breaks(s.Body)
	case *ast.Switch:
		return switchTerminatesBy(s, blockEnds)
	case *ast.Region:
		return blockEnds(s.Body)
	default:
		return false
	}
}

// breaks reports whether body holds a break that leaves the loop body
// belongs to: one directly in it or inside an if, a switch or a region,
// which a break does not leave, but not one inside a nested loop, which
// leaves only that loop.
func breaks(body []ast.Stmt) bool {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.Break:
			return true
		case *ast.Region:
			if breaks(s.Body) {
				return true
			}
		case *ast.If:
			if breaks(s.Then) || breaks(s.Else) {
				return true
			}
		case *ast.Switch:
			for _, cl := range s.Cases {
				if breaks(cl.Body) {
					return true
				}
			}
		}
	}
	return false
}

// blockTerminates reports whether control cannot fall out of the bottom of
// body: whether its last statement terminates (see terminates). An empty
// body falls out.
func blockTerminates(body []ast.Stmt) bool {
	return len(body) > 0 && terminates(body[len(body)-1])
}

// checkReturn checks a return of fn: see checkResults. A return in
// deferred code is an error of its own, since nothing may leave deferred
// code, and its values are then only checked for what is inside them.
func (c *Checker) checkReturn(ret *ast.Return, fn *ast.Func) {
	if c.deferred == 0 {
		c.checkResults(ret, fn)
		// The contract must hold once every value is computed, so a write
		// that takes effect at the return's end, return f(mut d), counts.
		keyword, outer := ret.Span, c.at
		keyword.End = keyword.Start + len("return")
		c.at = ret.Span.End + 1
		c.proveContract(fn, keyword, ret)
		c.at = outer
		return
	}
	c.errorf(ret.Span, "cannot return from a defer")
	for _, v := range ret.Values {
		c.discard(v)
	}
}

// checkResults checks that a return carries one value for each result fn
// declares, each usable as that result's type, a view result's as
// returnView says. A wrong count is one error, and then no value is
// compared with a result.
func (c *Checker) checkResults(ret *ast.Return, fn *ast.Func) {
	got, want := len(ret.Values), len(fn.Results)
	if got == want {
		c.passedOn = c.passOn(ret, fn)
		defer func() { c.passedOn = nil }()
		for i, v := range ret.Values {
			if c.table.IsView(fn.ResultTypes[i]) {
				c.returnView(v, fn.ResultTypes[i], fn)
				continue
			}
			c.use(v, c.expr(v), fn.ResultTypes[i], "in return from "+c.displayName(fn))
		}
		return
	}

	for _, v := range ret.Values {
		c.discard(v)
	}
	name := c.displayName(fn)
	switch {
	case want == 0:
		c.errorf(spanOf(ret.Values), "%s declares no result, so it cannot return a value", name)
	case got == 0 && want == 1:
		c.errorf(
			ret.Span,
			"%s must return a value of type %s",
			name,
			c.typeName(fn.ResultTypes[0]),
		)
	case got < want:
		c.errorf(ret.Span, "not enough return values: %s returns %d, got %d", name, want, got)
	default:
		c.errorf(
			spanOf(ret.Values),
			"too many return values: %s returns %d, got %d",
			name,
			want,
			got,
		)
	}
}

// spanOf returns the source range from the start of the first expression in
// a list through the end of the last. The list must not be empty.
func spanOf(exprs []ast.Expr) ast.Span {
	span := exprs[0].Spanned()
	span.End = exprs[len(exprs)-1].Spanned().End
	return span
}

// resolveResultTypes records what each declared result type denotes, a
// view included when it is the only one (see resultType). A result whose
// type is unknown keeps types.Invalid, as a parameter does.
func (c *Checker) resolveResultTypes(fn *ast.Func) {
	fn.ResultTypes = make([]types.Type, len(fn.Results))
	for i, result := range fn.Results {
		fn.ResultTypes[i] = c.resultType(result, len(fn.Results), externName(fn))
	}
	c.checkErrorLast(fn)
}

// resolveParamTypes records what each declared parameter type denotes, a
// view included, which only a parameter or a result may have (see
// paramType). A
// parameter whose type is unknown keeps types.Invalid, which every later check
// reports rather than silently accepting.
func (c *Checker) resolveParamTypes(fn *ast.Func) {
	for i, param := range fn.Params {
		if i == 0 && fn.Method && fn.RecvType == nil && c.prior > 0 {
			// The resolver reported the receiver's type, as no type the
			// method can belong to; it is not reported again.
			param.Type = types.Invalid
			continue
		}
		param.Type = c.paramType(param)
	}
	c.ownReceiver(fn)
}

// errorf reports an error at span, its message format and args as
// fmt.Sprintf formats them, with no notes (see errorNotes).
func (c *Checker) errorf(span ast.Span, format string, args ...any) {
	c.errorNotes(span, nil, format, args...)
}

// unusedf reports a warning at span, code that is never used: something
// suspicious that does not stop compilation (see warnings), marked
// Unused, which an editor may dim.
func (c *Checker) unusedf(span ast.Span, format string, args ...any) {
	c.warnings = append(c.warnings, diag.Diagnostic{
		Severity: diag.Warning,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
		Unused:   true,
	})
}

// errorNotes reports an error at span, with notes that explain it.
func (c *Checker) errorNotes(span ast.Span, notes []diag.Diagnostic, format string, args ...any) {
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
		Related:  notes,
	})
}

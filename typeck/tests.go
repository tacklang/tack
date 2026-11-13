package typeck

import (
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// Tests. With Checker.Tests set, the program is the package tack test
// loaded for its tests, with its test files (see ast.File.Test) and the
// main the driver generated for them, which calls each test. Every
// function of a test file of that package, its last, whose name starts
// with test_ is a test, and must be declared exactly
//
//	func test_x(mut t testing.T) error
//
// one mut parameter, of std/testing's T, and one result, an error: no
// type parameters, no other parameter, no other result, no own, scope or
// variadic parameter, not extern and not a method (checkTests). A test
// that is not is reported at its name, unless a type of its signature is
// one an earlier error left unknown, and the generated main, which
// passes it as a value of that type, is then left to the driver, which
// drops what is reported in it when anything else is (see package
// testmain). A function named test_ in any other file is no test.
//
// A ? written in a test's own body, not a closure's, records where it is
// before it returns the error, so the runner can report the error there:
// the checker gives the ? a literal of testing.Site holding its place,
// made as a call's is (see site.go), and the field of the test's T it is
// stored in, t.at, both typed (Question.Site and Question.SiteTo), which
// the backend lowers to one store before the ? frees what it must and
// returns. That field must exist, a Site, which checkTestingT checks of
// std/testing's T, as checkSiteShape checks Site.

// testPrefix starts the name of every test.
const testPrefix = "test_"

// testShape is how a test must be declared, for the message about one
// that is not.
const testShape = "func %s(mut t testing.T) error"

// testingT is the name of std/testing's T, and testingAt the name of its
// field a ? in a test stores its place in.
const (
	testingT  = "T"
	testingAt = "at"
)

// checkTests reports, when c.Tests is set, every test of decls, the
// program's declarations, with their signatures resolved, that is not
// declared as a test must be, and records the others in c.tests.
func (c *Checker) checkTests(decls []ast.Decl) {
	if !c.Tests {
		return
	}
	testFiles := make(map[source.FileID]bool)
	main := c.prog.Main()
	for _, f := range main.Files {
		if f.Test {
			testFiles[f.Span.File] = true
		}
	}
	for _, decl := range decls {
		fn, ok := decl.(*ast.Func)
		if !ok || !strings.HasPrefix(fn.Name, testPrefix) || c.declPkg[fn] != main ||
			!testFiles[fn.Span.File] {
			continue
		}
		if c.signatureReported(fn) {
			continue
		}
		if !c.testShaped(fn) {
			c.errorf(fn.NameSpan, "%s is a test, so it must be declared "+testShape+
				", not generic and not a method", fn.Name, fn.Name)
			continue
		}
		if c.tests == nil {
			c.tests = make(map[*ast.Func]bool)
		}
		c.tests[fn] = true
	}
}

// signatureReported reports whether a parameter or a result type of fn
// is Invalid: a type an earlier error left unknown, such as testing.T
// with std/testing not imported, which says nothing of the shape the
// test meant.
func (c *Checker) signatureReported(fn *ast.Func) bool {
	for _, p := range fn.Params {
		if p.Type == types.Invalid {
			return true
		}
	}
	for _, r := range fn.ResultTypes {
		if r == types.Invalid {
			return true
		}
	}
	return false
}

// testShaped reports whether fn, a test, is declared as checkTests
// requires.
func (c *Checker) testShaped(fn *ast.Func) bool {
	if fn.Method || fn.Generic() || fn.Extern || len(fn.Params) != 1 || len(fn.ResultTypes) != 1 {
		return false
	}
	p := fn.Params[0]
	return p.Mut && !p.Own && !p.Scope && !p.Variadic && c.isTestingT(p.Type) &&
		fn.ResultTypes[0] == types.Error
}

// isTestingT reports whether typ is std/testing's T.
func (c *Checker) isTestingT(typ types.Type) bool {
	td := c.structs[typ]
	pkg := c.structPkg[typ]
	return td != nil && pkg != nil && pkg.Path == stdTesting && td.Name == testingT
}

// checkTestingT reports std/testing's T, at its name, when c.Tests is
// set and it has no field at of type testing.Site, where a ? in a test
// stores its place. Every struct's fields are resolved by then.
func (c *Checker) checkTestingT() {
	if !c.Tests {
		return
	}
	for _, typ := range c.structOrder {
		if !c.isTestingT(typ) {
			continue
		}
		if _, ok := c.testingAt(typ); !ok {
			c.errorf(c.structs[typ].NameSpan, "testing.T must have a field %s of type testing.Site: "+
				"a ? in a test stores its place there", testingAt)
		}
	}
}

// testingAt returns the type of the field at of typ, std/testing's T,
// and whether it has one that is a testing.Site.
func (c *Checker) testingAt(typ types.Type) (types.Type, bool) {
	for _, f := range c.table.Fields(typ) {
		if f.Name == testingAt {
			return f.Type, c.isSite(f.Type)
		}
	}
	return types.Invalid, false
}

// recordQuestionSite gives q, a ? checked in the function being checked,
// the place it is written and the field it is stored in when that
// function is a test (see the top of this file), and takes any it was
// given away otherwise, as when a body is checked again.
func (c *Checker) recordQuestionSite(q *ast.Question) {
	q.Site, q.SiteTo = nil, nil
	if c.fn == nil || !c.tests[c.fn] {
		return
	}
	t := c.fn.Params[0]
	siteType, ok := c.testingAt(t.Type)
	if !ok {
		return
	}
	at := ast.Span{File: q.Span.File, Start: q.Span.End, End: q.Span.End}
	q.Site = c.siteLit(q.Span, siteType, at)
	q.SiteTo = &ast.Selector{
		X:        &ast.Ident{Name: t.Name, Def: t, Span: at},
		Name:     testingAt,
		NameSpan: at,
		Span:     at,
		Type:     siteType,
	}
}

package typeck_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

// TestFirstPassLeavesNoTrace checks every case of examples/invalid.txtar
// and examples/warnings.txtar with every function body checked once more
// first, as a body that binds a method call's or a function value's
// results is, and requires the checker to report exactly what it reports
// checking each body once. A difference is something the
// first pass left behind: a name it reported that the second pass then
// takes as already reported, and so drops, or anything else it changed
// that the second pass reads.
func TestFirstPassLeavesNoTrace(t *testing.T) {
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				_, once, checked := frontEnd(c.Source, false)
				if !checked {
					t.Skip("the lexer or the parser stops at the case before the checker")
				}
				_, twice, _ := frontEnd(c.Source, true)
				if got, want := listed(twice), listed(once); got != want {
					t.Errorf("checked twice, the checker reports\n%s\nchecked once, it reports\n%s",
						got, want)
				}
			})
		}
	}
}

// TestProbeLeavesNoTrace checks every case of examples/invalid.txtar and
// examples/warnings.txtar with every function body probed for the bounds
// its loops keep, none of which is then put in force, and requires the
// checker to report exactly what it reports with no body probed. The
// probe exists to find those bounds; a difference is anything else it
// left behind that the check after it reads.
func TestProbeLeavesNoTrace(t *testing.T) {
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				_, never, checked := checkWith(c.Source, (*typeck.Checker).NeverProbe)
				if !checked {
					t.Skip("the lexer or the parser stops at the case before the checker")
				}
				_, probed, _ := checkWith(c.Source, (*typeck.Checker).ProbeEveryBodyKeepingNothing)
				if got, want := listed(probed), listed(never); got != want {
					t.Errorf("probed, the checker reports\n%s\nnot probed, it reports\n%s",
						got, want)
				}
			})
		}
	}
}

// frontEnd lexes, parses, resolves and checks src as one file, whatever
// the parser and the resolver report, checking every body twice when
// twice is set, and returns the checker, what it reports and whether it
// checked src at all: the lexer rejecting src, or the parser stopping
// with no tree, ends the run before the checker.
func frontEnd(src []byte, twice bool) (*typeck.Checker, []diag.Diagnostic, bool) {
	if twice {
		return checkWith(src, (*typeck.Checker).CheckEveryBodyTwice)
	}
	return checkWith(src, nil)
}

// checkWith is frontEnd with set, when not nil, called on the checker
// before it checks src.
func checkWith(src []byte, set func(*typeck.Checker)) (*typeck.Checker, []diag.Diagnostic, bool) {
	f := &source.File{ID: source.FileID("test"), Filename: "main.tck", Data: src}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil, nil, false
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if file == nil {
		return nil, nil, false
	}
	prog := ast.SingleFile(file)
	prog.Errors = len(diags)
	resolver.New(prog, stdtest.MustBuiltins()).Resolve()
	c := typeck.New(prog, f, stdtest.MustBuiltins())
	if set != nil {
		set(c)
	}
	return c, c.Check(), true
}

// TestFirstPassRemembersNothing checks that what the first pass of a body
// works out, the value of a local and of an arithmetic operator, is not
// remembered into the second, which works it out again: a body checked
// twice takes exactly twice the steps a body checked once does to find
// them. A record the first pass left behind would answer the second from
// what it found, as a record of what was reported would silence it.
func TestFirstPassRemembersNothing(t *testing.T) {
	src := []byte("package main\n\nfunc main() {\n    n := 6\n    d := n - 4\n" +
		"    if 12 / d == 6 {\n        print(\"6\\n\")\n    }\n}\n")
	once, diags, _ := frontEnd(src, false)
	if len(errorsOf(diags)) > 0 {
		t.Fatalf("diagnostics:\n%s", listed(diags))
	}
	twice, _, _ := frontEnd(src, true)
	w1, w2 := once.ProverWork(), twice.ProverWork()
	for _, k := range []struct {
		what        string
		once, twice int
	}{
		{"locals worked out", w1.Locals, w2.Locals},
		{"arithmetic worked out", w1.Arithmetic, w2.Arithmetic},
	} {
		if k.once == 0 || k.twice != 2*k.once {
			t.Errorf("%s: %d checked once, %d checked twice; want some, then twice as many",
				k.what, k.once, k.twice)
		}
	}
}

// listed lists diags one to a line, each with its severity, span and
// message, and its notes after it, indented.
func listed(diags []diag.Diagnostic) string {
	var b strings.Builder
	var list func(diags []diag.Diagnostic, indent string)
	list = func(diags []diag.Diagnostic, indent string) {
		for _, d := range diags {
			fmt.Fprintf(&b, "%s%s %s:%d-%d: %s\n", indent, d.Severity, d.Span.File,
				d.Span.Start, d.Span.End, d.Message)
			list(d.Related, indent+"    ")
		}
	}
	list(diags, "")
	return b.String()
}

package compilertest

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// visibleLibrary is package calc/lexer of the programs below: a name of
// every kind at every visibility.
const visibleLibrary = `package lexer

pub type Token struct {
    pub kind int64
    lib start int64
    end int64
}

lib type Pos struct {
    pub line int64
}

type cursor struct {
    pub at int64
}

pub var KindNumber int64 = 1
pub var Unused int64 = 2
lib var Limit int64 = 3
lib var Origin = Pos{line: 1}
var hidden int64 = 4

pub func First() Token {
    c := cursor{at: hidden}
    return Token{kind: KindNumber, start: c.at, end: c.at + 1}
}

lib func Line(p Pos) int64 {
    return p.line
}
`

// TestVisibilityAllowsWhatIsVisible checks that a package uses every pub
// name of another package of its module, and every lib one, its fields
// included, that an exported global no package reads is not reported, and
// that a struct with a private field may still be declared at its zero
// value, copied and compared outside its package.
func TestVisibilityAllowsWhatIsVisible(t *testing.T) {
	files := fstest.MapFS{
		"m/tack.mod":        {Data: []byte("module calc\n")},
		"m/lexer/lexer.tck": {Data: []byte(visibleLibrary)},
		"m/main.tck": {Data: []byte(`package main

import lexer from "calc/lexer"

func main() {
    t := lexer.First()
    mut z lexer.Token
    w := t
    p := lexer.Pos{line: 2}
    if w != t || z == t || t.start != 4 || z.kind != 0 {
        exit(1)
    }
    exit(t.kind + lexer.Limit + lexer.Line(p) + lexer.Line(lexer.Origin) + p.line)
}
`)},
	}
	if got := run(t, frontEndBackend, program{files: files, target: "m"}); got.stage != "ok" {
		t.Errorf("a program using only visible names failed:\n%s", got)
	}
}

// loadModule loads the main package at target in files, failing the test
// on any problem.
func loadModule(t *testing.T, files fstest.MapFS, target string) *loader.Result {
	t.Helper()
	res, err := (&loader.Loader{FS: loader.FromFS(files), Builtins: stdtest.MustBuiltins()}).Dir(
		target,
	)
	if err != nil || len(res.Diags) > 0 {
		t.Fatalf("loading %s: %v %v", target, err, res.Diags)
	}
	return res
}

// messages returns the message of each of diags, in order.
func messages(diags []diag.Diagnostic) []string {
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.Message
	}
	return msgs
}

// inOtherModule loads a program whose main package, in module calc,
// imports calc/lexer, holding visibleLibrary, and then moves the package
// calc/lexer to module other: a module cannot import another yet, so this
// is the only way to show what lib means across modules.
func inOtherModule(t *testing.T, main string) *loader.Result {
	t.Helper()
	res := loadModule(t, fstest.MapFS{
		"m/tack.mod":        {Data: []byte("module calc\n")},
		"m/lexer/lexer.tck": {Data: []byte(visibleLibrary)},
		"m/main.tck":        {Data: []byte(main)},
	}, "m")
	lib := res.Program.Packages[0]
	if lib.Path != "calc/lexer" || lib.Module != "calc" {
		t.Fatalf("first package is %s in module %s, want calc/lexer in calc", lib.Path, lib.Module)
	}
	lib.Module = "other"
	return res
}

// TestLibIsNotVisibleFromAnotherModule checks that a lib function, type
// and global are each reported where another module names them, while its
// pub ones stay usable.
func TestLibIsNotVisibleFromAnotherModule(t *testing.T) {
	res := inOtherModule(t, `package main

import lexer from "calc/lexer"

func main() {
    var p lexer.Pos
    exit(lexer.Line(p) + lexer.Limit + lexer.First().kind)
}
`)
	got := messages(resolver.New(res.Program, stdtest.MustBuiltins()).Resolve())
	want := []string{
		"Pos is lib in module other and cannot be used from module calc",
		"Line is lib in module other and cannot be used from module calc",
		"Limit is lib in module other and cannot be used from module calc",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestLibFieldIsNotVisibleFromAnotherModule checks that a lib field of a
// pub struct is reported where another module reads or writes it, and
// that a literal of the struct there is reported for its lib and private
// fields alike.
func TestLibFieldIsNotVisibleFromAnotherModule(t *testing.T) {
	res := inOtherModule(t, `package main

import lexer from "calc/lexer"

func main() {
    mut t lexer.Token
    t.start = 1
    u := lexer.Token{kind: 1, start: 0, end: 0}
    exit(t.kind + t.start + u.kind)
}
`)
	if diags := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %v", messages(diags))
	}
	got := messages(typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check())
	want := []string{
		"start is lib in module other and cannot be used from module calc",
		"cannot write a lexer.Token literal outside package lexer: fields start, end are not pub",
		"start is lib in module other and cannot be used from module calc",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

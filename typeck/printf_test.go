package typeck_test

import (
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// checkWithStd checks src, a single file that may import the repository's
// standard library, and returns the checker's diagnostics. It fails the
// test if an earlier stage rejects src.
func checkWithStd(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	l := &loader.Loader{
		FS:       loader.FromFS(fstest.MapFS{"main.tck": {Data: []byte(src)}}),
		Builtins: stdtest.MustBuiltins(),
		Std:      stdtest.Std(),
	}
	res, err := l.File("main.tck")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) > 0 {
		t.Fatalf("load: %s", diag.RenderAllPretty(res.Files, res.Diags))
	}
	if diags := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(res.Files, diags))
	}
	return errorsOf(typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check())
}

// printfMain returns a program importing std/fmt whose main's body is
// lines, with show, which passes its format and values on to printf, and
// globals, which prints with a known and an unknown global format.
func printfMain(lines ...string) string {
	src := "package main\n\nimport fmt from \"std/fmt\"\n\nvar known = \"%d\\n\"\n\nmut unknown = \"%d\\n\"\n\n" +
		"func show(f string, args ...any) {\n    fmt.printf(f, args...)\n}\n\n" +
		"func globals() {\n    fmt.printf(known, 1)\n    fmt.printf(unknown, \"s\")\n}\n\nfunc main() {\n"
	for _, l := range lines {
		src += "    " + l + "\n"
	}
	return src + "}\n"
}

// printfAccepted are calls of printf the checker accepts.
var printfAccepted = []struct{ name, src string }{
	{
		"every verb",
		printfMain(`fmt.printf("%d %x %s %t %c %v %%\n", 1, uint8(2), "s", true, 'c', false)`),
	},
	{"known global format", printfMain(`fmt.printf(known, 1)`)},
	{"unknown format, any mistake", printfMain(`fmt.printf(unknown, "s", 1, 2)`)},
	{"parameter format", printfMain(`show("%d %d", "s")`)},
	{"mutable local format", printfMain(`mut f = "%z"`, `f = "%d"`, `fmt.printf(f)`)},
	{
		"any passed on, any verb",
		"package main\n\nimport fmt from \"std/fmt\"\n\nfunc f(a any) {\n    fmt.printf(\"%c %s\\n\", a, a)\n}\n\n" +
			"func main() {\n    f(1)\n}\n",
	},
	{
		"spread, any count",
		"package main\n\nimport fmt from \"std/fmt\"\n\nfunc f(args ...any) {\n    fmt.printf(\"%d %d %d\\n\", args...)\n}\n\n" +
			"func main() {\n    f(1)\n}\n",
	},
	{"println untouched", printfMain(`fmt.println("%d")`)},
}

// TestAcceptsPrintf checks each of printfAccepted has no diagnostics.
func TestAcceptsPrintf(t *testing.T) {
	for _, tt := range printfAccepted {
		t.Run(tt.name, func(t *testing.T) {
			if diags := checkWithStd(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestReportsEveryPrintfMistake checks one call's several mistakes are
// each reported, in the order of the format, the count last.
func TestReportsEveryPrintfMistake(t *testing.T) {
	diags := checkWithStd(t, printfMain(`fmt.printf("%s %q %d %t", 1, 2, "x")`))
	want := []string{
		"printf verb %s does not take int64",
		"unknown printf verb %q",
		"printf verb %d does not take string",
		"printf format has 4 verbs but 3 arguments",
	}
	if len(diags) != len(want) {
		t.Fatalf("got %v, want %q", diags, want)
	}
	for i, d := range diags {
		if d.Message != want[i] {
			t.Errorf("diagnostic %d is %q, want %q", i, d.Message, want[i])
		}
	}
}

// TestOtherPrintfIsNotChecked checks a function named printf outside
// std/fmt is a function like any other, whose format is not read.
func TestOtherPrintfIsNotChecked(t *testing.T) {
	src := "package main\n\nfunc printf(f string, args ...any) {\n}\n\nfunc main() {\n    printf(\"%d %q\", \"x\")\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// TestOtherEprintfIsNotChecked checks a function named eprintf outside
// std/fmt is a function like any other, whose format is not read, as
// TestOtherPrintfIsNotChecked does for printf.
func TestOtherEprintfIsNotChecked(t *testing.T) {
	src := "package main\n\nfunc eprintf(f string, args ...any) {\n}\n\nfunc main() {\n    eprintf(\"%d %q\", \"x\")\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// TestOtherAppendfIsNotChecked checks a function named appendf outside
// std/fmt is a function like any other, whose format is not read, as
// TestOtherPrintfIsNotChecked does for printf, whether its format is
// its first parameter or its second, as std/fmt's is.
func TestOtherAppendfIsNotChecked(t *testing.T) {
	for _, src := range []string{
		"package main\n\nfunc appendf(f string, args ...any) {\n}\n\nfunc main() {\n    appendf(\"%d %q\", \"x\")\n}\n",
		"package main\n\nfunc appendf(n int64, f string, args ...any) {\n}\n\nfunc main() {\n    appendf(1, \"%d %q\", \"x\")\n}\n",
	} {
		if _, diags := checkTree(t, src); len(diags) > 0 {
			t.Errorf("got %v, want no diagnostics", diags)
		}
	}
}

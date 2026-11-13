package c_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
)

// callSiteChecks is how many checks examples/call-site prints.
const callSiteChecks = 27

// TestCallSiteExampleIsSanitizerClean builds examples/call-site and std's
// C at -O2, and with the address and undefined behavior sanitizers at -O0
// and -O1, every finding fatal, and checks every check it prints passed:
// the place a testing.Site parameter is given at a direct call, through
// one and two helpers that pass their own on, on methods with plain and
// mut receivers and one called through its type, in generic functions,
// across a ?, in a defer's body and a closure's, from another package,
// at column 1 and after a tab, to a variadic function given a spread,
// directly and passed on, with the excerpts of a call of one line and of
// several, and Site values given to a generic function as plain values.
func TestCallSiteExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "call-site")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), callSiteChecks)
		})
	}
}

// siteProgram is the start of every program TestSiteReadsAsADiagnostic
// builds: show writes the place it is called from, its file, its line and
// column, and its excerpt, each line ended.
const siteProgram = "package main\n" +
	"\n" +
	"import fmt from \"std/fmt\"\n" +
	"import testing from \"std/testing\"\n" +
	"\n" +
	"func show(s testing.Site) {\n" +
	"    fmt.printf(\"%s\\n%d:%d\\n%s\", s.file, s.line, s.col, s.excerpt)\n" +
	"}\n" +
	"\n"

// TestSiteReadsAsADiagnostic builds programs whose main calls show, a
// function with a testing.Site parameter, from awkward places, and checks
// that what the program writes of the place is exactly what a diagnostic
// at the call prints: its file name and position from the header, and
// the excerpt below it, byte for byte, worked out here from the source
// alone. The call is the one that starts at the first "show(" of main.
func TestSiteReadsAsADiagnostic(t *testing.T) {
	long := strings.Repeat("x", 100_000)
	cases := []struct {
		name, main string
	}{
		{"plain", "func main() {\n    show()\n}\n"},
		{"column 1", "func main() {\nshow()\n}\n"},
		{"tabs", "func main() {\n\t\tshow()\t// \t\n\t}\n"},
		{"odd bytes", "func main() {\n    show() // \xff\x00\x01\x7f\r \xc3\xa9\n    // \xfe\n}\n"},
		{"long line", "func main() {\n    show() // " + long + "\n}\n"},
		{"long line after", "func main() {\n    show()\n    // " + long + "\n}\n"},
		{"several lines", "func main() {\n    show(\n\n    )\n}\n"},
		{"no newline at the end", "func main() { show() }"},
		{"crlf", "func main() {\r\n    show()\r\n}\r\n"},
		{"crlf several lines", "func main() {\r\n    show(\r\n\r\n    )\r\n}\r\n"},
		{"in a closure", "func call(scope f func()) {\n    f()\n}\n\nfunc main() {\n    call(func() { show() })\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pre := siteProgram
			if strings.HasPrefix(tc.name, "crlf") {
				pre = strings.ReplaceAll(pre, "\n", "\r\n")
			}
			src := pre + tc.main
			out, err := exec.Command(buildWithStd(t, src)).Output()
			if err != nil {
				t.Fatalf("running: %v", err)
			}
			if want := diagnosticPlace(t, src); string(out) != want {
				t.Errorf("wrote\n%q\nwant\n%q", out, want)
			}
		})
	}
}

// diagnosticPlace returns what show writes for the call of show in main
// in src, the file main.tck, as a diagnostic at that call renders it: its
// file and position, from the header line, and the lines below it.
func diagnosticPlace(t *testing.T, src string) string {
	t.Helper()
	start := strings.Index(src, "func main()")
	start += strings.Index(src[start:], "show(")
	end := start + strings.Index(src[start:], ")") + 1
	f := &source.File{ID: "main.tck", Filename: "main.tck", Data: []byte(src)}
	if diags := lexer.New(f).Lex(); len(diags) > 0 {
		t.Fatalf("lex: %v", diags)
	}
	d := diag.Diagnostic{Severity: diag.Error, Message: "m", Span: ast.Span{File: f.ID, Start: start, End: end}}
	pretty := diag.RenderPretty(f, d)
	header, excerpt, _ := strings.Cut(pretty, "\n")
	line, col := f.Position(start)
	if want := fmt.Sprintf("main.tck:%d:%d: error: m", line, col); header != want {
		t.Fatalf("header %q, want %q", header, want)
	}
	return fmt.Sprintf("main.tck\n%d:%d\n%s", line, col, excerpt)
}

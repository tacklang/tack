package c_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// stdInclude is how the generated source, and a tack.h that needs it,
// include std's header.
const stdInclude = "#include <tack/tack.h>\n"

// tackStringDef matches a definition of tack_string: a struct typedef
// that names it.
var tackStringDef = regexp.MustCompile(`typedef\s+struct\s*\{[^}]*\}\s*tack_string\s*;`)

// TestOnlyStdDefinesTackString checks tack_string is defined once, in
// std's include/tack/tack.h, and that the generated source and a tack.h
// whose signatures use a string include that header rather than define
// the type themselves, so every C file of a program, std's among them,
// sees one definition.
func TestOnlyStdDefinesTackString(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(stdtest.IncludeDir(), "tack", "tack.h"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(tackStringDef.FindAllString(string(data), -1)); n != 1 {
		t.Errorf("std's tack.h defines tack_string %d times, want once:\n%s", n, data)
	}

	src := "package main\n\nextern func take(s string)\n\nfunc main() {\n    print(\"hi\")\n}\n"
	prog := ast.SingleFile(compile(t, src))
	out, diags := c.New().Generate(prog, stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	for name, got := range map[string]string{
		"the generated source": string(out),
		"tack.h":               string(c.New().Header(prog)),
	} {
		if !strings.Contains(got, stdInclude) {
			t.Errorf("%s does not include std's header:\n%s", name, got)
		}
		if tackStringDef.MatchString(got) {
			t.Errorf("%s defines tack_string itself:\n%s", name, got)
		}
	}
	checkNoStdCFileDefinesTackString(t)
}

// checkNoStdCFileDefinesTackString checks no C file of the runtime built
// into the compiler defines tack_string: each takes it from the runtime's
// tack.h.
func checkNoStdCFileDefinesTackString(t *testing.T) {
	t.Helper()
	for _, f := range builtin.RuntimeCFiles() {
		if tackStringDef.Match(f.Data) {
			t.Errorf("%s defines tack_string; only the runtime's tack.h may", f.Name)
		}
	}
}

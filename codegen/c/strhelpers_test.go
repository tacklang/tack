package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
)

// stringHelperDefs is how tack.h defines its string helpers.
var stringHelperDefs = []string{
	"#define TACK_C_STR_LEN(x) (sizeof (x) - 1 + 0 * sizeof (char[1 - 2 * " +
		"__builtin_types_compatible_p(__typeof__(x), __typeof__(&(x)[0]))]))\n",
	"#define TACK_STR(lit) ((tack_string){(const unsigned char *)(\"\" lit \"\"), " +
		"sizeof (\"\" lit \"\") - 1})\n",
	"#define TACK_STR_N(p, n) ((tack_string){_Generic((p), " +
		"const unsigned char *: (p), unsigned char *: (p)), (int64_t)(n)})\n",
}

// TestHeaderDefinesStringHelpers checks tack.h defines the string helpers
// after tack_string when a signature uses a string, and not otherwise.
func TestHeaderDefinesStringHelpers(t *testing.T) {
	src := "package main\n\nextern func take(s string)\n\nfunc main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	typedef := strings.Index(got, "} tack_string;\n")
	for _, def := range stringHelperDefs {
		if i := strings.Index(got, def); i < 0 || i < typedef {
			t.Errorf("tack.h does not define, after tack_string, %q:\n%s", def, got)
		}
	}

	src = "package main\n\nextern func take(n int64)\n\nfunc main() {\n}\n"
	got = string(c.New().Header(ast.SingleFile(compile(t, src))))
	if strings.Contains(got, "TACK_STR") || strings.Contains(got, "TACK_C_STR_LEN") {
		t.Errorf("tack.h of a program without strings defines a string helper:\n%s", got)
	}
}

// TestExternStringsExampleIsWarningFree builds examples/extern-strings,
// whose C half makes each string it returns with a string helper, with
// clang's warnings as errors, and checks every check it prints passed.
func TestExternStringsExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-strings")
	src, err := os.ReadFile(filepath.Join(dir, "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	csrc, err := os.ReadFile(filepath.Join(dir, "strings.c"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(buildExtern(t, string(src), string(csrc))).Output()
	if err != nil {
		t.Fatalf("extern-strings failed: %v", err)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 7 {
		t.Errorf("extern-strings printed\n%s", out)
	}
}

// helperMisuses are uses of the string helpers that must not compile,
// each with what clang's error says.
var helperMisuses = []struct {
	name, body, want string
}{
	{
		"length of a pointer",
		"int64_t bad(const unsigned char *p) { return (int64_t)TACK_C_STR_LEN(p); }",
		"array size is negative",
	},
	{"TACK_STR of an array", "static const unsigned char a[] = \"x\";\n" +
		"tack_string bad(void) { return TACK_STR(a); }", "expected ')'"},
	{
		"TACK_STR of a pointer", "tack_string bad(const unsigned char *p) { return TACK_STR(p); }",
		"expected ')'",
	},
	{
		"TACK_STR_N of a char pointer",
		"tack_string bad(const char *p) { return TACK_STR_N(p, 1); }",
		"not compatible with any generic association type",
	},
	{
		"TACK_STR_N of an int pointer", "tack_string bad(int *p) { return TACK_STR_N(p, 1); }",
		"not compatible with any generic association type",
	},
	{
		"TACK_STR_N of a literal", "tack_string bad(void) { return TACK_STR_N(\"abc\", 3); }",
		"not compatible with any generic association type",
	},
	{
		"TACK_STR_N of NULL",
		"#include <stddef.h>\ntack_string bad(void) { return TACK_STR_N(NULL, 0); }",
		"not compatible with any generic association type",
	},
}

// TestStringHelperMisusesDoNotCompile checks each misuse of a string
// helper is a clang error even with warnings off, as the build compiles C
// files, rather than a wrong length or a pointer of the wrong type.
func TestStringHelperMisusesDoNotCompile(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	src := "package main\n\nextern func take(s string)\n\nfunc main() {\n}\n"
	dir := t.TempDir()
	header := filepath.Join(dir, c.HeaderName)
	if err := os.WriteFile(
		header,
		c.New().Header(ast.SingleFile(compile(t, src))),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	for _, m := range helperMisuses {
		t.Run(m.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "bad.c")
			if err := os.WriteFile(file, []byte(m.body+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("clang", "-w", "-fsyntax-only", "-include", header, file).
				CombinedOutput()
			if err == nil {
				t.Fatalf("compiled; want an error saying %q", m.want)
			}
			if !strings.Contains(string(out), m.want) {
				t.Errorf("clang said\n%s\nwant %q", out, m.want)
			}
		})
	}
}

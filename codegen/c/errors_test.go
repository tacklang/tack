package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// errorsSrc is a program using errors in every way the backend lowers
// them: a function returning an error alone and one returning it last, a
// literal, noerror, the fields, a global initialized with each, a struct
// and an array holding errors, and == between two.
const errorsSrc = "package main\n\n" +
	"var notFound = error{code: 2, msg: \"not found\"}\n\nvar none = noerror\n\n" +
	"type P struct {\n    err error\n}\n\n" +
	"func ready(n int64) error {\n    if n < 0 {\n        return error{code: 1, msg: \"negative\"}\n    }\n" +
	"    return noerror\n}\n\n" +
	"func parse(n int64) (int64, error) {\n    return n, ready(n)\n}\n\n" +
	"func main() {\n    v, err := parse(3)\n    if err.code != 0 {\n        exit(err.code)\n        return\n    }\n" +
	"    p := P{err: notFound}\n    a := [2]error{none, err}\n" +
	"    if p.err == notFound && a[0] == a[1] && len(p.err.msg) == 9 && v == 3 {\n        exit(42)\n    }\n}\n"

// TestGeneratesErrors checks the C an error lowers to: tack_error from
// std's tack.h, never defined in the generated source, its members code
// and msg under their own names, a literal as a compound literal of it,
// noerror and a global of it zeroed, and == through an equality helper
// that compares the code and the message's bytes.
func TestGeneratesErrors(t *testing.T) {
	got := generate(t, errorsSrc)
	for _, want := range []string{
		"static const tack_error tack_4_main8_notFound = {2, {tack_str0, TACK_C_STR_LEN(tack_str0)}};\n",
		"static const tack_error tack_4_main4_none = {};\n",
		"    tack_error f_err;\n",
		"    tack_error e[2];\n",
		"    tack_error r1;\n",
		"static tack_error tack_4_main5_ready(int64_t p_n);\n",
		"return (tack_error){.code = 1, .msg = (tack_string){",
		"return (tack_error){};\n",
		"if (v_err.code != 0) {\n",
		"static bool tack_eq_15(const tack_error *a, const tack_error *b) {\n" +
			"    return a->code == b->code && (tack_compare_strings(a->msg, b->msg) == 0);\n}\n",
		"(v_p.f_err.msg).len == 9",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C lacks\n%s\nin\n%s", want, got)
		}
	}
	if strings.Contains(got, "} tack_error;") {
		t.Errorf("generated C defines tack_error, which std's tack.h does:\n%s", got)
	}
	if status := exitStatus(t, buildStrict(t, errorsSrc)); status != 42 {
		t.Errorf("exit status = %d, want 42", status)
	}
}

// TestErrorsExampleIsSanitizerClean builds examples/errors with the
// address and undefined behavior sanitizers, every finding fatal, and
// checks every check it prints passed.
func TestErrorsExampleIsSanitizerClean(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "examples", "errors", "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildWith(
		t,
		string(src),
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("errors failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 21 {
		t.Errorf("errors printed\n%s", out)
	}
}

// TestExternErrorsExampleIsWarningFree builds examples/extern-errors with
// clang's warnings as errors, its C file against its tack.h, so
// tack_error, TACK_ERROR and TACK_NO_ERROR agree between the generated
// source, the header, std's tack.h and the C file, and checks every check
// it prints passed.
func TestExternErrorsExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-errors")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "errors.c"))
	out, err := exec.Command(buildExtern(t, src, csrc)).Output()
	if err != nil {
		t.Fatalf("extern-errors failed: %v", err)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 6 {
		t.Errorf("extern-errors printed\n%s", out)
	}
}

// TestHeaderDeclaresError checks tack.h includes std's header, which
// defines tack_error, for a signature with an error, defines no
// tack_error of its own, names a view and an array of errors after error's
// Tack name, as for any basic type, and declares each prototype with
// tack_error, a results struct holding one last.
func TestHeaderDeclaresError(t *testing.T) {
	src := "package main\n\ntype P struct {\n    errs [2]error\n}\n\n" +
		"extern func f(e error, xs ...error) (int64, error)\n\nextern func g(p P) error\n\n" +
		"func main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	for _, want := range []string{
		stdInclude,
		"typedef struct {\n    tack_error e[2];\n} tack_array_2_error;\n",
		"typedef struct {\n    tack_array_2_error errs;\n} P;\n",
		"typedef struct {\n    const tack_error *ptr;\n    int64_t len;\n} tack_view_error;\n",
		"typedef struct {\n    int64_t r0;\n    tack_error r1;\n} tack_results_f;\n",
		"tack_results_f f(tack_error /* e */, tack_view_error /* xs */); " +
			"// extern func f(e error, xs ...error) (int64, error)\n",
		"tack_error g(P /* p */); // extern func g(p P) error\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tack.h lacks\n%s\nin\n%s", want, got)
		}
	}
	if strings.Contains(got, "} tack_error;") {
		t.Errorf("tack.h defines tack_error, which std's tack.h does:\n%s", got)
	}
}

// TestStdHeaderDefinesError checks std's tack.h defines tack_error, a code
// and a message, after tack_string, and the error helpers after it.
func TestStdHeaderDefinesError(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(stdtest.IncludeDir(), "tack", "tack.h"))
	if err != nil {
		t.Fatal(err)
	}
	std := string(data)
	str := strings.Index(std, "} tack_string;\n")
	typedef := strings.Index(
		std,
		"typedef struct {\n    int32_t code;\n    tack_string msg;\n} tack_error;\n",
	)
	if typedef < 0 || typedef < str {
		t.Fatalf("std's tack.h does not define tack_error after tack_string:\n%s", std)
	}
	for _, def := range []string{
		"#define TACK_ERROR(code, lit) ((tack_error){(int32_t)(code), TACK_STR(lit)})\n",
		"#define TACK_NO_ERROR ((tack_error){0, {0, 0}})\n",
	} {
		if i := strings.Index(std, def); i < typedef {
			t.Errorf("std's tack.h does not define, after tack_error, %q", def)
		}
	}
}

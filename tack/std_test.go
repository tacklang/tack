package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/internal/stdtest"
)

// envAt is the environment with TACK_HOME set to home, in which looking
// up the home directory fails the test.
func envAt(t *testing.T, home string) environment {
	t.Helper()
	env := testEnv(t)
	env.tackHome = home
	return env
}

// program writes a single-file program whose main runs body and returns
// its path.
func program(t *testing.T, body string) string {
	t.Helper()
	return writeSource(t, []byte("package main\n\nfunc main() {\n"+body+"}\n"))
}

// missingStd is the error for a TACK_HOME of home with no std in it.
func missingStd(home string) string {
	return "tack: cannot find std at " + filepath.Join(home, "src", "std") +
		"; set TACK_HOME or run make install\n"
}

// Every command that needs the builtins fails, with exit 1, when std is not
// where TACK_HOME says, before reading the program.
func TestMissingStdFails(t *testing.T) {
	home := t.TempDir()
	src := program(t, "    print(\"hi\")\n")
	for _, args := range [][]string{
		{"--out", filepath.Join(t.TempDir(), "bin"), src},
		{"--res", src},
		{"--emit", src},
		{"--emit-header", src},
		{"--res", filepath.Join(t.TempDir(), "missing.tck")},
	} {
		t.Run(strings.Join(args[:1], " "), func(t *testing.T) {
			r := runTackEnv(t, t.Context(), envAt(t, home), args...)
			if r.code != exitFailure || r.stdout != "" || r.stderr != missingStd(home) {
				t.Errorf("exit %d, stdout %q, stderr %q; want 1 and %q",
					r.code, r.stdout, r.stderr, missingStd(home))
			}
		})
	}
}

// A std that is a file, not a directory, is missing too.
func TestStdThatIsAFileIsMissing(t *testing.T) {
	home := writeDir(t, map[string]string{"src/std": "not a directory"})
	r := runTackEnv(t, t.Context(), envAt(t, home), "--emit", program(t, ""))
	if r.code != exitFailure || r.stderr != missingStd(home) {
		t.Errorf("exit %d, stderr %q; want 1 and %q", r.code, r.stderr, missingStd(home))
	}
}

// Without TACK_HOME, std is found in ~/.tack, and a home directory that
// cannot be found is a failure saying so.
func TestStdFromHomeDirectory(t *testing.T) {
	user := t.TempDir()
	if err := os.MkdirAll(filepath.Join(user, ".tack", "src"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stdtest.Dir(), filepath.Join(user, ".tack", "src", "std")); err != nil {
		t.Fatal(err)
	}
	env := environment{homeDir: func() (string, error) { return user, nil }}
	r := runTackEnv(t, t.Context(), env, "--emit", program(t, "    print(\"hi\")\n"))
	if r.code != exitOK || !strings.Contains(r.stdout, "tack_builtin_print") {
		t.Errorf("exit %d, stderr %q; want the program emitted", r.code, r.stderr)
	}

	empty := t.TempDir()
	env = environment{homeDir: func() (string, error) { return empty, nil }}
	r = runTackEnv(t, t.Context(), env, "--emit", program(t, ""))
	if want := missingStd(
		filepath.Join(empty, ".tack"),
	); r.code != exitFailure ||
		r.stderr != want {
		t.Errorf("exit %d, stderr %q; want 1 and %q", r.code, r.stderr, want)
	}

	env = environment{
		homeDir: func() (string, error) { return "", errors.New("$HOME is not defined") },
	}
	r = runTackEnv(t, t.Context(), env, "--emit", program(t, ""))
	assertFailure(
		t,
		r,
		"cannot find std: TACK_HOME is not set and the home directory is unknown: $HOME is not defined",
	)
}

// The generated source is built with the runtime header built into the
// compiler on the include path, where it finds <tack/tack.h>: an absolute
// directory the build wrote it to, below its own temporary directory and
// not in TACK_HOME, even for a relative TACK_HOME, whose copy of tack.h is
// never read. The tests run in tack/, whose parent is the repository's
// root.
func TestBuildIncludesTheRuntimeHeader(t *testing.T) {
	out := filepath.Join(t.TempDir(), "include")
	r := runTackEnv(t, t.Context(), envAt(t, ".."),
		"--backend", includeBackend, "--out", out, program(t, ""))
	if r.code != exitOK || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	dir, header, _ := strings.Cut(string(data), "\n")
	if !filepath.IsAbs(dir) || strings.HasPrefix(dir, stdtest.Home()) ||
		filepath.Base(filepath.Dir(dir)) != "runtime" {
		t.Errorf("BuildCmd was given include directory %q, want one the build wrote", dir)
	}
	if header != string(builtin.RuntimeHeader()) {
		t.Errorf("the include directory's tack/tack.h is not the compiler's")
	}
}

// --help, --backends, --lex and --ast need no builtins, so they work with
// no std at all.
func TestCommandsThatNeedNoStd(t *testing.T) {
	env := envAt(t, t.TempDir())
	src := program(t, "    print(\"hi\")\n")
	for _, args := range [][]string{{"--help"}, {"--backends"}, {"--lex", src}, {"--ast", src}} {
		r := runTackEnv(t, t.Context(), env, args...)
		if r.code != exitOK || r.stderr != "" || r.stdout == "" {
			t.Errorf("%q: exit %d, stderr %q", args, r.code, r.stderr)
		}
	}
}

// A relative TACK_HOME works, and everything handed to clang is still
// absolute: the tests run in tack/, whose parent is the repository's root.
func TestRelativeTackHome(t *testing.T) {
	out := filepath.Join(t.TempDir(), "objs")
	r := runTackEnv(t, t.Context(), envAt(t, ".."),
		"--backend", objsBackend, "--out", out, program(t, "    print(\"hi\")\n"))
	if r.code != exitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, obj := range linkedObjects(t, out) {
		if !filepath.IsAbs(obj) {
			t.Errorf("object %s is not absolute", obj)
		}
	}
}

// linkedObjects returns the objects the objs backend wrote to out, which
// it links in the order given.
func linkedObjects(t *testing.T, out string) []string {
	t.Helper()
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

// linkedNames returns the base names of the objects the objs backend
// wrote to out.
func linkedNames(t *testing.T, out string) []string {
	t.Helper()
	objs := linkedObjects(t, out)
	names := make([]string, 0, len(objs))
	for _, obj := range objs {
		names = append(names, filepath.Base(obj))
	}
	return names
}

// A std object is linked only when it defines a builtin the program calls,
// wherever the call is, or, for a program with a list, the list functions,
// so a program that calls neither print nor exit and has no list links no
// std object at all, and so none of the C library they call. exit links
// the list object too, list or not, since it calls the allocators' release
// of every buffer they hold, which list.c defines beside them.
func TestStdObjectsLinkedOnlyWhenUsed(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"neither", "    if true {\n    }\n", nil},
		{"print", "    print(\"hi\")\n", []string{"std-print.o"}},
		{"exit", "    exit(3)\n", []string{"std-exit.o", "std-list.o"}},
		{
			"both",
			"    print(\"hi\")\n    exit(3)\n",
			[]string{"std-exit.o", "std-list.o", "std-print.o"},
		},
		{
			"print twice, in a defer",
			"    defer {\n        print(\"a\")\n    }\n    print(\"b\")\n",
			[]string{"std-print.o"},
		},
		{"len only", "    if len(\"abc\") == 3 {\n    }\n", nil},
		{
			"a list",
			"    l := make(list[int64], c_allocator)\n    if len(l) == 0 {\n    }\n",
			[]string{"std-list.o"},
		},
		{
			"a list and print",
			"    l := make(list[int64], c_allocator)\n    if len(l) == 0 {\n        print(\"hi\")\n    }\n",
			[]string{"std-list.o", "std-print.o"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := isolateTemp(t)
			out := filepath.Join(t.TempDir(), "objs")
			r := runTack(t, "--backend", objsBackend, "--out", out, program(t, tt.body))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if got := linkedNames(t, out); !slices.Equal(got, tt.want) {
				t.Errorf("linked %q, want %q", got, tt.want)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

// A C file of the program that defines a builtin's C name is a second
// definition when the program calls the builtin, which links std's; when it
// does not, std's is not linked, and nothing clashes.
func TestUserCDefiningABuiltin(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	dir := writeDir(t, map[string]string{
		"main.tck": "package main\n\nfunc main() {\n    print(\"hi\")\n}\n",
		"mine.c":   "#include <stdint.h>\nvoid tack_builtin_print(void) {}\n",
	})
	r := runTack(t, "--out", filepath.Join(t.TempDir(), "bin"), filepath.Join(dir, "main.tck"))
	assertFailure(t, r, "two C files define tack_builtin_print: ")
	if !strings.Contains(r.stderr, "<runtime>/print.c") ||
		!strings.Contains(r.stderr, "mine.c") {
		t.Errorf("stderr = %q, want both files named", r.stderr)
	}

	dir = writeDir(t, map[string]string{
		"main.tck": "package main\n\nfunc main() {\n    exit(7)\n}\n",
		"mine.c":   "void tack_builtin_print(void) {}\n",
	})
	bin := filepath.Join(t.TempDir(), "bin")
	if r := runTack(t, "--out", bin, filepath.Join(dir, "main.tck")); r.code != exitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if _, code := runBuilt(t, bin); code != 7 {
		t.Errorf("exit status %d, want 7", code)
	}
}

// A program builds and runs at every optimization level, std's objects
// included, with the same output.
func TestBuiltinsAtEveryOptLevel(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	src := program(t, "    print(\"a\")\n    print(\"\")\n    print(\"b\\n\")\n    exit(70000)\n")
	for _, opt := range []string{"-O0", "-O1", "-O2", "-O3", "-Os", "-Oz"} {
		t.Run(opt, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "bin")
			if r := runTack(t, opt, "--out", bin, src); r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			out, code := runBuilt(t, bin)
			if out != "ab\n" || code != 112 {
				t.Errorf("printed %q, exited %d; want %q and 112", out, code, "ab\n")
			}
		})
	}
}

// A binary that calls no builtin does not reference the C library's
// fwrite, which print's body calls, since print's object is not linked;
// one that calls print does, which shows the check can see it.
func TestBinaryWithoutPrintHasNoFwrite(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, tt := range []struct {
		body string
		want bool
	}{
		{"    if true {\n    }\n", false},
		{"    exit(0)\n", false},
		{"    print(\"hi\")\n", true},
	} {
		bin := filepath.Join(t.TempDir(), "bin")
		if r := runTack(t, "-O0", "--out", bin, program(t, tt.body)); r.code != exitOK {
			t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
		}
		syms, err := readSymbols(bin)
		if err != nil {
			t.Fatal(err)
		}
		// An ELF symbol table may version the name: fwrite@GLIBC_2.2.5.
		refs := slices.ContainsFunc(syms.undefined, func(s string) bool {
			return s == "fwrite" || strings.HasPrefix(s, "fwrite@")
		})
		if refs != tt.want {
			t.Errorf(
				"%q: references fwrite = %v, want %v; undefined: %q",
				tt.body,
				refs,
				tt.want,
				syms.undefined,
			)
		}
		if slices.Contains(syms.defined, "tack_builtin_print") != tt.want {
			t.Errorf("%q: defines tack_builtin_print = %v, want %v", tt.body, !tt.want, tt.want)
		}
	}
}

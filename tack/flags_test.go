package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// clangLogVar names the file the clang shim appends each command line to,
// realClangVar the clang it then runs, and clangDropVar the argument it
// leaves out first, when it is not empty.
const (
	clangLogVar  = "TACK_TEST_CLANG_LOG"
	realClangVar = "TACK_TEST_REAL_CLANG"
	clangDropVar = "TACK_TEST_CLANG_DROP"
)

// clangShim is a clang that leaves out every argument equal to
// $TACK_TEST_CLANG_DROP when that is set, appends the arguments left, one
// per line and then a line holding only a record separator, to
// $TACK_TEST_CLANG_LOG, and runs $TACK_TEST_REAL_CLANG with them.
const clangShim = "#!/bin/sh\n" +
	"if [ -n \"$" + clangDropVar + "\" ]; then\n" +
	"  for a; do shift; [ \"$a\" = \"$" + clangDropVar + "\" ] || set -- \"$@\" \"$a\"; done\n" +
	"fi\n" +
	"{ printf '%s\\n' \"$@\"; printf '\\036\\n'; } >> \"$" + clangLogVar + "\"\n" +
	"exec \"$" + realClangVar + "\" \"$@\"\n"

// isClangShim reports whether the file at path is clangShim. Only a file
// of the shim's size is read, so a real clang is never read whole.
func isClangShim(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() != int64(len(clangShim)) {
		return false
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: a clang the test is about to run
	return err == nil && string(data) == clangShim
}

// shimClang puts clangShim first on PATH, in front of the clang on PATH,
// dropping the argument drop when it is not empty, and returns a function
// reporting the argument lists of every clang run since the last time it
// was called, clang --version's left out. It records what the driver
// actually executes, after anything the driver does to a backend's
// command, not what the backend returned, and, with drop, what clang ran
// with in place of that. A second shim in one test runs the clang the
// first one does, from $TACK_TEST_REAL_CLANG, never the first shim, which
// would then run itself.
func shimClang(t *testing.T, drop string) func() [][]string {
	t.Helper()
	clang := os.Getenv(realClangVar)
	if clang == "" {
		var err error
		if clang, err = exec.LookPath("clang"); err != nil {
			t.Skip("clang not on PATH")
		}
	}
	if isClangShim(clang) {
		t.Fatalf("the real clang %s is a clang shim", clang)
	}
	t.Setenv(clangDropVar, drop)
	dir := t.TempDir()
	shim := filepath.Join(dir, "clang")
	// G306: the shim must be executable.
	if err := os.WriteFile(shim, []byte(clangShim), 0o700); err != nil { //nolint:gosec // see above
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "clang.log")
	t.Setenv(realClangVar, clang)
	t.Setenv(clangLogVar, log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	seen := 0
	return func() [][]string {
		t.Helper()
		data, err := os.ReadFile(log) //nolint:gosec // G304: the test's own log
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		var runs [][]string
		for _, rec := range strings.Split(string(data), "\x1e\n") {
			if rec == "" {
				continue
			}
			runs = append(runs, strings.Split(strings.TrimSuffix(rec, "\n"), "\n"))
		}
		fresh := runs[seen:]
		seen = len(runs)
		return slices.DeleteFunc(fresh, func(argv []string) bool {
			return slices.Equal(argv, []string{"--version"})
		})
	}
}

// Every C compile a build runs passes -fno-finite-loops, at every level,
// as clang receives it: the link of the generated source, a package's own
// C file (examples/extern's helpers.c), a std package's C files (std/fmt's,
// in examples/fmt-write) and the runtime's, with an empty cache, so every
// runtime file is compiled. Without the flag clang may assume a loop ends
// and delete one that never does.
func TestEveryCompilePassesNoFiniteLoops(t *testing.T) {
	ran := shimClang(t, "")
	for _, opt := range codegen.Opts {
		t.Run("O"+string(opt), func(t *testing.T) {
			ran()
			for _, name := range []string{"extern", "fmt-write"} {
				buildExample(t, opt, name)
			}
			seen := make(map[string]bool)
			for _, argv := range ran() {
				kind := clangRunKind(argv)
				if !slices.Contains(argv, "-fno-finite-loops") {
					t.Errorf("%s: clang %q, want -fno-finite-loops", kind, argv)
				}
				seen[kind] = true
			}
			for _, want := range []string{"link", "package C", "std package C", "runtime C"} {
				if !seen[want] {
					t.Errorf("clang ran no %s compile; the test no longer covers it", want)
				}
			}
		})
	}
}

// buildExample builds examples/<name> with the c backend at opt and
// checks the build succeeded and left nothing in the temp directory.
func buildExample(t *testing.T, opt codegen.Opt, name string) {
	t.Helper()
	tmp := isolateTemp(t)
	bin := filepath.Join(t.TempDir(), name)
	r := runTack(t, "-O"+string(opt), "--out", bin, example(name))
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("build %s: exit %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
	}
	assertEmptyDir(t, tmp)
}

// clangRunKind names what the clang run with argv does, for the coverage
// check of TestEveryCompilePassesNoFiniteLoops: "link" for one without
// -c, the build of the generated source, and for a compile, by its source,
// the last argument, "package C" for examples/extern's helpers.c, "std
// package C" for a file of std/fmt, "runtime C" for one of the runtime's,
// and "other C" otherwise.
func clangRunKind(argv []string) string {
	if !slices.Contains(argv, "-c") {
		return "link"
	}
	src := filepath.ToSlash(argv[len(argv)-1])
	switch {
	case filepath.Base(src) == "helpers.c":
		return "package C"
	case strings.Contains(src, "/std/fmt/"):
		return "std package C"
	case strings.Contains(src, "/runtime/builtin/"):
		return "runtime C"
	}
	return "other C"
}

// A second shim in one test runs the real clang, not the first shim: a
// clang run then ends, is logged once by the second shim, with its
// argument dropped, and never reaches the first.
func TestShimClangTwice(t *testing.T) {
	first := shimClang(t, "")
	second := shimClang(t, "-fno-finite-loops")
	ctx, cancel := context.WithTimeout(t.Context(), endedLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "clang", "-fno-finite-loops", "-E", "-x", "c", os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clang through two shims: %v\n%s", err, out)
	}
	if got := first(); len(got) != 0 {
		t.Errorf("the first shim ran %q, want nothing", got)
	}
	want := [][]string{{"-E", "-x", "c", os.DevNull}}
	if got := second(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("the second shim ran %q, want %q", got, want)
	}
}

package porttest

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The strict C of the run records with a strict-libs line, as the port's
// tools/corpus/strictc.tck does it, written again here: the Go test such
// a record replaces compiles the C of its build itself with clang's
// warnings as errors and full code generation, which the driver's -w
// turns off in the record's build, so the program's own C is compiled
// so at each of the record's runs (strictProgramC), and the runtime's,
// std's and the example's once per flag set (checkStrictLibs).

// strictCFlags are the flags codegen/c's tests compile every C file with,
// strictCFlags there.
var strictCFlags = []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fstrict-flex-arrays=3"}

// strictDefine is the define codegen/c's buildProgram compiles std's and
// the example's C with, and neither the runtime's nor the program's own;
// a record whose test builds through it gives it in its cflags.
const strictDefine = "-D_DEFAULT_SOURCE"

// driverFlags are the flags a record gives for the driver's sake alone,
// which no Go test compiles with: the address sanitizer's
// -fno-sanitize-address-globals-dead-stripping and the three that undo
// the driver's loop flags (see sanLoopFlags in rungen).
var driverFlags = []string{"-fno-sanitize-address-globals-dead-stripping", "-ffinite-loops", "-mllvm", "-indvars-predicate-loops=true"}

// strictCSkipped are the C files below std the strict-c check leaves out,
// each with why.
var strictCSkipped = map[string]string{
	"testing/testing.c": "std/testing's C is built by tack test alone, against a test build's header; no record's test builds it",
}

// strictSet returns the flag set of rec at the level level, empty for the
// compiler's own: -O<level> and rec's cflags but driverFlags, joined with
// spaces.
func strictSet(rec runRecord, level string) string {
	var set []string
	if level != "" {
		set = append(set, "-O"+level)
	}
	for _, f := range rec.cflags {
		if !slices.Contains(driverFlags, f) {
			set = append(set, f)
		}
	}
	return strings.Join(set, " ")
}

// strictUse is one flag set a record with a strict-libs line builds with,
// and its dir, empty when its files are its own.
type strictUse struct{ set, dir string }

// strictUses returns the flag sets of rec, a record with a strict-libs
// line, at each of its levels or at the compiler's when it names none,
// each with rec's dir.
func strictUses(rec runRecord) []strictUse {
	levels := rec.opts
	if len(levels) == 0 {
		levels = []string{""}
	}
	var out []strictUse
	for _, l := range levels {
		out = append(out, strictUse{strictSet(rec, l), rec.dir})
	}
	return out
}

// strictCompile compiles file, with header included first when not empty,
// with strictCFlags and set's flags, but strictDefine unless define says
// so, and std's include directory, to obj, reporting, empty when clang
// takes it, why not.
func strictCompile(t *testing.T, set string, define bool, file, header, obj string) string {
	t.Helper()
	args := slices.Clone(strictCFlags)
	for _, f := range strings.Fields(set) {
		if define || f != strictDefine {
			args = append(args, f)
		}
	}
	args = append(args, "-c", "-I", filepath.Join(homeStd(), "include"))
	if header != "" {
		args = append(args, "-I", filepath.Dir(header), "-include", header)
	}
	args = append(args, "-o", obj, file)
	var errOut bytes.Buffer
	cmd := exec.CommandContext(t.Context(), "clang", args...) //nolint:gosec // G204: clang on the port's C
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil || errOut.Len() > 0 {
		return fmt.Sprintf("clang rejects %s at %s: %v\n%s", file, set, err, firstK(errOut.Bytes()))
	}
	return ""
}

// strictProgramC compiles c, the program's own C that --emit wrote, for
// rec, a record with a strict-libs line, at the level level, as its Go
// test compiles program.c: with strictCFlags and rec's flag set at level,
// but strictDefine, which buildProgram gives the program's C none of,
// reporting, empty when clang takes it, why not.
func strictProgramC(t *testing.T, rec runRecord, level string, c []byte) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "strict-program.c")
	mustDo(t, os.WriteFile(file, c, 0o600))
	if why := strictCompile(t, strictSet(rec, level), false, file, "", filepath.Join(dir, "strict-program.o")); why != "" {
		return "the program's own C does not pass its Go test's strict compile: " + why
	}
	return ""
}

// cFilesBelow returns the .c files below dir, but those strictCSkipped
// names when skip says to.
func cFilesBelow(t *testing.T, dir string, skip bool) []string {
	t.Helper()
	var out []string
	mustDo(t, filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".c") {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if _, skipped := strictCSkipped[filepath.ToSlash(rel)]; skip && skipped {
			return nil
		}
		out = append(out, p)
		return nil
	}))
	return out
}

// exampleHeader returns the tack.h nearest above p, a C file below the
// corpus's examples, within its example, or none.
func exampleHeader(p string) string {
	examples := filepath.Join(env.home, portCorpusDir, "examples")
	rel, err := filepath.Rel(examples, p)
	if err != nil {
		return ""
	}
	for at := path.Dir(filepath.ToSlash(rel)); ; at = path.Dir(at) {
		h := filepath.Join(examples, filepath.FromSlash(at), "tack.h")
		if _, err := os.Stat(h); err == nil {
			return h
		}
		if !strings.Contains(at, "/") {
			return ""
		}
	}
}

// stdHeader returns the tack.h the reference compiler writes with
// --emit-header for a program that imports every package of std with a C
// file but those strictCSkipped names: one header for all of std.
func stdHeader(t *testing.T) string {
	t.Helper()
	var pkgs []string
	for _, p := range cFilesBelow(t, homeStd(), true) {
		rel, err := filepath.Rel(homeStd(), p)
		mustDo(t, err)
		if pkg := path.Dir(filepath.ToSlash(rel)); !slices.Contains(pkgs, pkg) {
			pkgs = append(pkgs, pkg)
		}
	}
	var src strings.Builder
	src.WriteString("package main\n\n")
	for i, p := range pkgs {
		fmt.Fprintf(&src, "import p%d from \"std/%s\"\n", i, p)
	}
	src.WriteString("\n// main does nothing: the program is for its header.\nfunc main() {\n}\n")
	dir := t.TempDir()
	mustDo(t, os.WriteFile(filepath.Join(dir, "main.tck"), []byte(src.String()), 0o600))
	cmd := exec.CommandContext(t.Context(), env.goBin, "--emit-header", "main.tck") //nolint:gosec // G204: the reference compiler
	cmd.Dir, cmd.Env = dir, append(compilerEnv(t.TempDir()), "TMPDIR="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--emit-header of a program importing std's packages with C: %v\n%s", err, out)
	}
	return filepath.Join(dir, "tack.h")
}

// checkStrictLibs compiles, at the flag set set, every .c of the runtime,
// without strictDefine; every .c of std but strictCSkipped, against
// header, when set holds strictDefine, as only the Go tests that build
// through buildProgram compile std's C; and the .c files below each of
// dirs, the dirs of the records of the set, each against the tack.h
// nearest above it in its example, with the set as it is; failing t with
// each file clang rejects.
func checkStrictLibs(t *testing.T, set string, dirs []string, header string) {
	t.Helper()
	obj := filepath.Join(t.TempDir(), "strict-c.o")
	report := func(why string) {
		if why != "" {
			t.Error(why)
		}
	}
	for _, f := range cFilesBelow(t, filepath.Join(env.home, "runtime"), false) {
		report(strictCompile(t, set, false, f, "", obj))
	}
	if slices.Contains(strings.Fields(set), strictDefine) {
		for _, f := range cFilesBelow(t, homeStd(), true) {
			report(strictCompile(t, set, true, f, header, obj))
		}
	}
	for _, d := range dirs {
		for _, f := range cFilesBelow(t, filepath.Join(env.home, portCorpusDir, filepath.FromSlash(d)), false) {
			report(strictCompile(t, set, true, f, exampleHeader(f), obj))
		}
	}
}

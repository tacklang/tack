package porttest

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The emit stages' names.
const (
	emitSingleStage   = "emit-single"
	emitPackagesStage = "emit-packages"
	emitLibStage      = "emit-lib"
	emitCasesStage    = "emit-cases"
	emitHeaderStage   = "emit-header"
)

// init adds the emit stages to stages, after the others, and the emit
// stages of the backend's port, emit-E0 to emit-E4 and emit-H, to the
// names required.txt may hold. Each emit stage compares standard output,
// standard error and the exit status byte for byte, and classifies its
// cases by the emit stages they need (see the package doc, Backend
// stages):
//
//   - emit-single runs --emit on each .tck file under examples/;
//   - emit-packages runs --emit on each package and module of examples/;
//   - emit-lib runs --check --emit on each package of std, and on the
//     port's driver, since --emit of a package that is not main is an
//     error, and --check checks one as a library;
//   - emit-cases runs --emit on each case of the warnings archives, the
//     programs with no error, and on each program harvested from the C
//     backend's tests hand their helpers (see harvestPrograms);
//   - emit-header runs --emit-header, --check --emit-header for std's,
//     on each input of emit-single, emit-packages and std whose package
//     holds a C file or an extern func, each compiler in a fresh copy of
//     the tree, and compares the files the copy holds afterwards too,
//     the tack.h written among them (see runHeaderCase).
func init() {
	for _, name := range emitStageNames {
		requirableChecks[name] = true
	}
	emitArgs := dumpArgs("--emit")
	libArgs := func(in input) []string { return []string{"--check", "--emit", in.file} }
	stages = append(stages,
		stage{
			name: emitSingleStage, args: emitArgs, inputs: emitSingleInputs, stdout: compareBytes,
			classify: classifyWith(emitArgs),
		},
		stage{
			name:     emitPackagesStage,
			args:     emitArgs,
			inputs:   emitPackageInputs,
			stdout:   compareBytes,
			classify: classifyWith(emitArgs),
		},
		stage{
			name: emitLibStage, args: libArgs, inputs: emitLibInputs, stdout: compareBytes,
			classify: classifyWith(libArgs),
		},
		stage{
			name: emitCasesStage, args: emitArgs, inputs: emitCaseInputs, stdout: compareBytes,
			classify: classifyWith(emitArgs),
		},
		stage{
			name: emitHeaderStage, args: headerArgs, inputs: emitHeaderInputs, stdout: compareBytes,
			classify: classifyHeader, run: runHeaderCase,
		},
	)
}

// emitSingleInputs returns every .tck file under examples/, as the
// single-file stages run them.
func emitSingleInputs() ([]input, error) {
	ins, err := singleFiles()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(ins, func(in input) bool { return in.dir != env.root }), nil
}

// emitPackageInputs returns every package and module of examples/, as
// the package stages run them.
func emitPackageInputs() ([]input, error) {
	ins, err := packageInputs()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(
		ins,
		func(in input) bool { return !strings.HasPrefix(in.name, "examples/") },
	), nil
}

// emitLibInputs returns every package of std, by its path from the
// module's root and by its import path from std's, and the port's own
// driver, as the package stages run them.
func emitLibInputs() ([]input, error) {
	ins, err := packageInputs()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(ins, func(in input) bool {
		return !strings.HasPrefix(in.name, "src/std/") && in.name != "port/driver"
	}), nil
}

// warningsArchives are the archives whose cases have no error, which
// --emit lowers.
var warningsArchives = []string{"warnings", "warnings-packages"}

// emitCaseInputs returns every case of warningsArchives, its single
// files and its targets as the other stages run them, and every program
// harvestPrograms gives, each run from the directory they are written
// to.
func emitCaseInputs() ([]input, error) {
	var out []input
	singles, err := singleFiles()
	if err != nil {
		return nil, err
	}
	pkgs, err := packageInputs()
	if err != nil {
		return nil, err
	}
	for _, a := range env.archives {
		if !slices.Contains(warningsArchives, a.name) {
			continue
		}
		for _, in := range slices.Concat(singles, pkgs) {
			if in.dir == a.dir {
				out = append(out, in)
			}
		}
	}
	harvested, err := harvestedInputs()
	if err != nil {
		return nil, err
	}
	return append(out, harvested...), nil
}

// emitHeaderInputs returns every input of emit-single and emit-packages,
// and every package of std, by both its names, whose package holds a C
// file or an extern func (see writesHeader): the inputs whose tack.h
// --emit-header writes. A directory of examples/ outside a module,
// which the loader rejects, is left out.
func emitHeaderInputs() ([]input, error) {
	singles, err := emitSingleInputs()
	if err != nil {
		return nil, err
	}
	pkgs, err := emitPackageInputs()
	if err != nil {
		return nil, err
	}
	lib, err := emitLibInputs()
	if err != nil {
		return nil, err
	}
	var out []input
	for _, in := range slices.Concat(singles, pkgs, lib) {
		if in.name == "port/driver" || outsideModule(in) {
			continue
		}
		ok, err := writesHeader(in)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, in)
		}
	}
	return out, nil
}

// outsideModule reports whether in's target is a directory with no
// tack.mod in it or above it, up to in.dir, which the loader rejects
// before it reads a file, as it does every directory of examples/ but
// a module's.
func outsideModule(in input) bool {
	target := filepath.Join(in.dir, filepath.FromSlash(in.file))
	if !dirExists(target) {
		return false
	}
	scope, _ := scopeOf(in)
	return !fileExists(filepath.Join(scope, "tack.mod"))
}

// headerArgs returns emit-header's command line for in: --emit-header of
// its target, and --check before it for a package of std, a library,
// whose --emit-header alone is an error.
func headerArgs(in input) []string {
	if strings.HasPrefix(in.name, "src/std/") {
		return []string{"--check", "--emit-header", in.file}
	}
	return []string{"--emit-header", in.file}
}

// writesHeader reports whether in's package, the directory of its file
// or the directory it names, by its path or by its import path in the
// module at in.dir, holds a C file or a Tack file declaring an extern
// func, so that --emit-header writes a tack.h for it.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func writesHeader(in input) (bool, error) {
	dir := filepath.Join(in.dir, filepath.FromSlash(in.file))
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	} else if err != nil {
		name, err := moduleName(filepath.Join(in.dir, "tack.mod"))
		if err != nil {
			return false, err
		}
		below, ok := strings.CutPrefix(in.file, name+"/")
		if !ok {
			return false, fmt.Errorf(
				"%s: %s is no path and no import path of module %s",
				in.name,
				in.file,
				name,
			)
		}
		dir = filepath.Join(in.dir, filepath.FromSlash(below))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", in.name, err)
	}
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".c":
			return true, nil
		case ".tck":
			data, err := os.ReadFile(
				filepath.Join(dir, e.Name()),
			) //nolint:gosec // G304: the corpus
			if err != nil {
				return false, fmt.Errorf("reading %s: %w", in.name, err)
			}
			if bytes.Contains(data, []byte("extern(")) {
				return true, nil
			}
		}
	}
	return false, nil
}

// headerCopy writes, in work, an empty directory, the copy of the tree
// in's run reads and writes: std, to work/src/std, and the directory
// scopeOf gives, below work by its path from the module's root, or, for
// an input outside it, the whole of in.dir, to work/case. It removes
// every tack.h of the copy's package directories, those holding a .tck
// file, so that the tack.h files there afterwards are what the run
// wrote, and returns in as it runs in the copy, with TACK_HOME set to
// work, whose std it reads, as the repository's is read from the
// module's root.
func headerCopy(in input, work string) (input, error) {
	std := filepath.Join(env.root, "src", "std")
	if err := copyTree(std, filepath.Join(work, "src", "std")); err != nil {
		return input{}, err
	}
	out := in
	out.env = append(slices.Clone(in.env), "TACK_HOME="+work)
	rel, err := filepath.Rel(env.root, in.dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		out.dir = filepath.Join(work, "case")
		if err := copyTree(in.dir, out.dir); err != nil {
			return input{}, err
		}
		return out, removeHeaders(work)
	}
	out.dir = filepath.Join(work, rel)
	scope, _ := scopeOf(in)
	if !strings.HasPrefix(scope, std+string(filepath.Separator)) && scope != std {
		below, err := filepath.Rel(env.root, scope)
		if err != nil {
			return input{}, fmt.Errorf("copying %s: %w", in.name, err)
		}
		if err := copyTree(scope, filepath.Join(work, below)); err != nil {
			return input{}, err
		}
	}
	return out, removeHeaders(work)
}

// copyTree copies every regular file and directory below src to dst,
// which may not exist yet: a directory with mode 0755, a file with 0644,
// or 0755 when its mode has an execute bit, as a fake clang's has.
func copyTree(src, dst string) error {
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return fmt.Errorf("naming %s: %w", p, err)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("copying %s: not a regular file", p)
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: the corpus
		if err != nil {
			return fmt.Errorf("copying: %w", err)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("copying %s: %w", p, err)
		}
		if info.Mode()&0o111 != 0 {
			if err := os.WriteFile(target, data, 0o755); err != nil { //nolint:gosec // G306: a fake clang
				return fmt.Errorf("copying: %w", err)
			}
			return os.Chmod(target, 0o755) //nolint:gosec // G302: a fake clang, whatever the umask
		}
		return os.WriteFile(target, data, 0o644) //nolint:gosec // G306: test files
	})
	if err != nil {
		return fmt.Errorf("copying %s: %w", src, err)
	}
	return nil
}

// removeHeaders removes every file named tack.h below work that is in a
// directory holding a .tck file, a package's; std's own, in
// src/std/include/tack, is in none.
func removeHeaders(work string) error {
	err := filepath.WalkDir(work, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "tack.h" {
			return err
		}
		tcks, err := filepath.Glob(filepath.Join(filepath.Dir(p), "*.tck"))
		if err != nil || len(tcks) == 0 {
			return err //nolint:wrapcheck // wrapped below
		}
		return os.Remove(p)
	})
	if err != nil {
		return fmt.Errorf("removing the copy's headers: %w", err)
	}
	return nil
}

// snapshot returns every regular file and directory below work, by its
// slash-separated path from work, a directory's with a / after it: a
// file with its mode on a line and then its bytes, a directory with its
// mode alone.
func snapshot(work string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(work, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == work {
			return err
		}
		rel, err := filepath.Rel(work, p)
		if err != nil {
			return fmt.Errorf("naming %s: %w", p, err)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("reading the copy: %w", err)
		}
		if d.IsDir() {
			files[filepath.ToSlash(rel)+"/"] = info.Mode().String() + "\n"
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: the copy's own files
		if err != nil {
			return fmt.Errorf("reading the copy: %w", err)
		}
		files[filepath.ToSlash(rel)] = info.Mode().String() + "\n" + string(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the copy's files: %w", err)
	}
	return files, nil
}

// filesDiff returns how got, the files the port left, differs from want,
// the Go compiler's, each path that one has and the other has not, or
// both have with another mode or other bytes, with the first line that
// differs, or ""
// when they agree.
func filesDiff(want, got map[string]string) string {
	var b strings.Builder
	for _, p := range slices.Sorted(func(yield func(string) bool) {
		for p := range want {
			if !yield(p) {
				return
			}
		}
		for p := range got {
			if _, ok := want[p]; !ok && !yield(p) {
				return
			}
		}
	}) {
		w, inWant := want[p]
		g, inGot := got[p]
		switch {
		case !inGot:
			fmt.Fprintf(&b, "\n  %s: written by Go, not by the port", p)
		case !inWant:
			fmt.Fprintf(&b, "\n  %s: written by the port, not by Go", p)
		case w != g:
			fmt.Fprintf(&b, "\n  %s differs: %s", p, firstDiff([]byte(w), []byte(g)))
		}
	}
	return b.String()
}

// runHeaderCase runs the case in of emit-header, st, with portBin as the
// port: each compiler in a fresh copy of in's tree (see headerCopy),
// made at the same path, the port's first, so that what either names by
// its path is named the same, and compares their standard output,
// standard error and exit status, as runCaseWith does, and the files
// and directories each copy holds afterwards, with their modes and the
// files' bytes (see snapshot). A not implemented answer
// skips, or fails, as runCaseWith's does.
func runHeaderCase(t *testing.T, st stage, in input, portBin string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	args := st.args(in)
	run := func(bin, cache string) (result, map[string]string) {
		if err := os.RemoveAll(work); err != nil {
			t.Fatal(err)
		}
		copied, err := headerCopy(in, work)
		if err != nil {
			t.Fatal(err)
		}
		r := runCompiler(t, bin, cache, copied, args)
		if r.notImplemented() {
			return r, nil
		}
		files, err := snapshot(work)
		if err != nil {
			t.Fatal(err)
		}
		return r, files
	}
	port, portFiles := run(portBin, env.portCache)
	if port.notImplemented() {
		declined(t, st, in, port)
	}
	want, goFiles := run(env.goBin, env.goCache)
	compareResults(t, st, in, args, want, port)
	if d := filesDiff(goFiles, portFiles); d != "" {
		t.Errorf("the files the runs leave differ:%s", d)
	}
	// Every input of the stage has C or an extern func, so a run Go
	// passes writes a tack.h, or the stage compares nothing of it.
	if want.code == 0 &&
		!slices.ContainsFunc(slices.Collect(maps.Keys(goFiles)), isPackageHeader(goFiles)) {
		t.Errorf("Go's --emit-header exits 0 and writes no tack.h")
	}
}

// isPackageHeader returns a test of whether a path of files is a tack.h
// of a package directory, one holding a .tck file.
func isPackageHeader(files map[string]string) func(p string) bool {
	return func(p string) bool {
		if path.Base(p) != "tack.h" {
			return false
		}
		dir := path.Dir(p)
		for q := range files {
			if path.Dir(q) == dir && path.Ext(q) == ".tck" {
				return true
			}
		}
		return false
	}
}

// classifyHeader is emit-header's classify: what the instrumented
// compiler covers running in, in a copy of its tree of its own, as
// runHeaderCase runs it, since --emit-header writes there.
func classifyHeader(in input, fresh bool) (covered, error) {
	if err := coverSetup(); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(cover.scratch, "header-")
	if err != nil {
		return nil, fmt.Errorf("copying %s: %w", in.name, err)
	}
	defer os.RemoveAll(work) //nolint:errcheck // scratch
	copied, err := headerCopy(in, work)
	if err != nil {
		return nil, err
	}
	return caseCovered(copied, headerArgs(in), fresh)
}

// harvestedInputs returns the programs of the harvest (see
// harvestPrograms), each written, the first time, to a directory of its
// own below the run's temporary directory, named by its name, as
// case.tck, and run from there as <name>/case.tck, named go/<name>.
func harvestedInputs() ([]input, error) {
	harvestIns.once.Do(func() { harvestIns.ins, harvestIns.err = writeHarvest() })
	return harvestIns.ins, harvestIns.err
}

// harvestIns holds the harvested programs' inputs, written once a run.
var harvestIns struct {
	err  error
	ins  []input
	once sync.Once
}

// writeHarvest writes the harvested programs below the run's temporary
// directory and returns their inputs.
func writeHarvest() ([]input, error) {
	progs, _, err := harvestPrograms()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(env.tmp, "harvested")
	out := make([]input, 0, len(progs))
	for _, p := range progs {
		file := path.Join(p.name, "case.tck")
		target := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("writing the harvested programs: %w", err)
		}
		if err := os.WriteFile(target, []byte(p.src), 0o600); err != nil {
			return nil, fmt.Errorf("writing the harvested programs: %w", err)
		}
		out = append(out, input{name: "go/" + p.name, dir: dir, file: file})
	}
	return out, nil
}

package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tacklang/tack/internal/invalid"
)

// The build stages' names, and the check of the binaries they build.
const (
	buildExamplesStage = "build-examples"
	buildInvalidCStage = "build-invalid-c"
	buildProbesStage   = "build-probes"
	// buildIdenticalCheck is the check that the two compilers build the
	// same binary, byte for byte, for a case of build-examples that both
	// build (see checkIdentical).
	buildIdenticalCheck = "build-identical"
)

// init adds the build stages to stages, and build-identical to the
// names required.txt may hold. Each builds its cases into binaries with
// both compilers, each in a fresh copy of the case's tree, and compares
// what the driver writes and leaves; for a case both build, it compares
// the binaries and what they do when run (see runBuildCase):
//
//   - build-examples builds each input of emit-single and emit-packages,
//     every .tck file, package and module of examples/;
//   - build-invalid-c builds each case of examples/invalid-c.txtar, whose
//     C files clang or the driver rejects;
//   - build-probes builds each of buildProbes, a program of
//     testdata/build-probes built with an --out path, flags or a PATH
//     of its own, which the corpus's builds never meet.
func init() {
	requirableChecks[buildIdenticalCheck] = true
	stages = append(stages,
		stage{
			name:   buildExamplesStage,
			args:   func(in input) []string { return buildArgs(buildOutShown, in) },
			inputs: buildExampleInputs,
			stdout: compareBytes,
			run:    runBuildCase,
		},
		stage{
			name:   buildInvalidCStage,
			args:   func(in input) []string { return buildArgs(buildOutShown, in) },
			inputs: buildInvalidCInputs,
			stdout: compareBytes,
			run:    runBuildCase,
		},
		stage{
			name:   buildProbesStage,
			args:   func(in input) []string { return buildArgs(buildOutShown, in) },
			inputs: buildProbeInputs,
			stdout: compareBytes,
			run:    runBuildCase,
		},
	)
}

// buildOutShown stands for the --out path of a build in the command line
// a stage's args gives, which names no case's own directory: runBuildCase
// gives each case's run a path of its own.
const buildOutShown = "<out>"

// buildArgs returns the build stages' command line for in: --out with
// out, then in's target; for a case of build-probes, its flags first,
// and its own --out path in place of out when it has one.
func buildArgs(out string, in input) []string {
	p, probe := buildProbes[in.name]
	if !probe {
		return []string{"--out", out, in.file}
	}
	switch {
	case p.outInHome && out == buildOutShown:
		out = "<home>/prog"
	case p.outInHome:
		out = filepath.Join(homeOfOut(out), "prog")
	case p.out != "":
		out = p.out
	}
	return append(slices.Clone(p.flags), "--out", out, in.file)
}

// homeOfOut returns the TACK_HOME of the case whose --out path is out,
// as newBuildDirs lays them out: out is <base>/out/prog and the home
// <base>/home, a read-only copy of std (see readOnlyHome).
func homeOfOut(out string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(out)), "home")
}

// buildProbe is one case of build-probes: a program of
// testdata/build-probes, its main.tck, built from its directory as a
// single file, with the command line and the PATH it gives the two
// compilers.
type buildProbe struct {
	// program is the directory below testdata/build-probes whose
	// main.tck the case builds.
	program string
	// out, when not empty, is the --out argument, relative to the
	// program's directory, in place of the case's own --out path: a
	// path the driver must refuse before it builds, so neither compiler
	// writes there.
	out string
	// pathDir, when not empty, is the directory below
	// testdata/build-probes that PATH names, alone, for the compilers.
	pathDir string
	// flags go before --out on the command line.
	flags []string
	// pathFront, when not empty, are elements put before the PATH the
	// compilers inherit, as they are: "" is an empty element, which
	// means the current directory, and a relative one is taken from it.
	pathFront []string
	// emptyPath sets PATH empty, set but holding no element, which Go's
	// exec.LookPath searches no directory of, in place of pathDir.
	emptyPath bool
	// outInHome makes the --out path prog in the case's TACK_HOME, a
	// directory no one may write to, so that clang's link fails.
	outInHome bool
}

// buildProbes are build-probes' cases, by name: the --out paths Go's
// checkOutPath refuses, a directory, one below a missing directory and
// one below a file, which are errors, and the input file itself, a
// usage error; a PATH that holds no clang and one that is empty, where
// Go's exec.LookPath fails; a clang that writes to both streams and
// exits 1, whose output the driver passes on before its error; an --out
// path in a directory no one may write to, where clang's link fails
// with its own messages; an empty and a relative element of PATH, which
// holds no clang, before the PATH the compilers inherit; a program that
// warns, whose warning comes before the build; and a build at each -O
// level but the default, which the examples build at. The builds the
// port declines are TestBuildDeclines', not cases here: build-probes is
// required.
var buildProbes = map[string]buildProbe{
	"build-probes/out-directory":      {program: "empty", out: "."},
	"build-probes/out-parent-missing": {program: "empty", out: "missing/prog"},
	"build-probes/out-below-file":     {program: "empty", out: "main.tck/prog"},
	"build-probes/out-is-input":       {program: "empty", out: "main.tck"},
	"build-probes/no-clang-on-path":   {program: "empty", pathDir: "empty"},
	"build-probes/empty-path":         {program: "empty", emptyPath: true},
	"build-probes/clang-fails":        {program: "empty", pathDir: "clang-fails"},
	"build-probes/out-unwritable":     {program: "empty", outInHome: true},
	"build-probes/path-empty-element": {program: "empty", pathFront: []string{""}},
	"build-probes/path-relative":      {program: "empty", pathFront: []string{"missing"}},
	"build-probes/warn-builds":        {program: "warn"},
	"build-probes/opt-0":              {program: "loops", flags: []string{"-O0"}},
	"build-probes/opt-1":              {program: "loops", flags: []string{"-O1"}},
	"build-probes/opt-2":              {program: "loops", flags: []string{"-O2"}},
	"build-probes/opt-s":              {program: "loops", flags: []string{"-Os"}},
	"build-probes/opt-z":              {program: "loops", flags: []string{"-Oz"}},
}

// buildProbeInputs returns build-probes' inputs, each of buildProbes run
// from its program's directory with main.tck as its target, and PATH as
// it says, in the order of their names.
func buildProbeInputs() ([]input, error) {
	dir := filepath.Join(env.root, "porttest", "testdata", "build-probes")
	names := make([]string, 0, len(buildProbes))
	for name := range buildProbes {
		names = append(names, name)
	}
	slices.Sort(names)
	ins := make([]input, 0, len(names))
	for _, name := range names {
		p := buildProbes[name]
		in := input{name: name, dir: filepath.Join(dir, p.program), file: "main.tck"}
		if !fileExists(filepath.Join(in.dir, in.file)) {
			return nil, fmt.Errorf("build probe %s: %s has no main.tck", name, p.program)
		}
		switch {
		case p.emptyPath:
			in.env = []string{"PATH="}
		case p.pathDir != "":
			in.env = []string{"PATH=" + filepath.Join(dir, p.pathDir)}
		case len(p.pathFront) > 0:
			in.env = []string{"PATH=" + strings.Join(append(slices.Clone(p.pathFront),
				os.Getenv("PATH")), string(filepath.ListSeparator))}
		}
		ins = append(ins, in)
	}
	return ins, nil
}

// buildExampleInputs returns build-examples' inputs: every input of
// emit-single and emit-packages, the inputs whose C the emit stages
// compare, whatever the driver makes of them.
func buildExampleInputs() ([]input, error) {
	singles, err := emitSingleInputs()
	if err != nil {
		return nil, err
	}
	pkgs, err := emitPackageInputs()
	if err != nil {
		return nil, err
	}
	ins := slices.Concat(singles, pkgs)
	for name := range buildRuns {
		if !slices.ContainsFunc(ins, func(in input) bool { return in.name == name }) {
			return nil, fmt.Errorf("buildRuns names %s, which is no input of %s", name,
				buildExamplesStage)
		}
	}
	return ins, nil
}

// buildInvalidCInputs returns build-invalid-c's inputs: each case of
// examples/invalid-c.txtar, written once a run to a directory of its
// own below the run's temporary directory, each case as <name>/<path>,
// run from there with its target, named invalid-c/<name>.
func buildInvalidCInputs() ([]input, error) {
	invalidCIns.once.Do(func() { invalidCIns.ins, invalidCIns.err = writeInvalidC() })
	return invalidCIns.ins, invalidCIns.err
}

// invalidCIns holds build-invalid-c's inputs, written once a run.
var invalidCIns struct {
	err  error
	ins  []input
	once sync.Once
}

// writeInvalidC writes the cases of examples/invalid-c.txtar below the
// run's temporary directory and returns their inputs. The archive is
// kept apart from the corpus's archives, whose files every other stage
// runs on.
func writeInvalidC() ([]input, error) {
	cases, err := invalid.LoadTree(filepath.FromSlash(invalid.CPath))
	if err != nil {
		return nil, fmt.Errorf("loading the invalid C cases: %w", err)
	}
	a := newArchive(filepath.Join(env.tmp, "corpus-c"), invalid.CPath)
	out := make([]input, 0, len(cases))
	for _, c := range cases {
		for _, f := range c.Files {
			if err := a.write(c.Name+"/"+f.Path, f.Data); err != nil {
				return nil, err
			}
		}
		out = append(out, input{name: a.name + "/" + c.Name, dir: a.dir, file: c.Target()})
	}
	return out, nil
}

// buildRun is one run of a built binary: its arguments, the variables it
// runs with, and how its output is taken. Every run's standard input is
// /dev/null, its working directory an empty directory and its TMPDIR
// another, which must be empty when it ends.
type buildRun struct {
	// name names the run in the case's messages.
	name string
	// shell, when not empty, is a script sh -c runs, with the binary as
	// $0 and args after it, in place of running the binary itself.
	shell string
	// wantFile, when not empty, is a file, by its path from the module's
	// root, that each binary's stdout must equal, as well as the other's.
	wantFile string
	// args are the binary's arguments.
	args []string
	// env holds variables, each name=value, added after the environment
	// the run inherits.
	env []string
	// drop are the prefixes of the inherited variables the run does not
	// get, as name= for one variable or a name's start for several.
	drop []string
	// combined gives standard output and standard error one pipe, so the
	// run's stdout holds both in the order they were written.
	combined bool
}

// defaultRun is every built binary's first run: no arguments, and the
// environment go test runs with, as most of the Go driver's tests of an
// example run it.
var defaultRun = buildRun{name: "default"}

// buildRuns are the runs, beyond defaultRun, of the build-examples cases
// that the Go compiler's tests run with arguments, variables or streams
// of their own, by the case's name, each as the test it is taken from
// runs it. examples/broken-pipe's runs, which break a pipe and read
// descriptor 3, are not among them: with no arguments it only says what
// it takes.
var buildRuns = map[string][]buildRun{
	// tack/main_test.go's TestBuiltOsPassesEveryCheck: osExampleArgs,
	// osExampleEnv and osLimitedArgs, writing examples/os/output.
	"examples/os/main.tck": {{
		name:     "os",
		args:     []string{"one", "", "two words"},
		env:      []string{"TACK_OS_EXAMPLE=set by the test", "TACK_OS_EXAMPLE_EMPTY="},
		drop:     []string{"TACK_OS_EXAMPLE"},
		shell:    `ulimit -n 64 && exec "$0" "$@"`,
		wantFile: "examples/os/output",
	}},
	// tack/main_test.go's endless-loops test, runBuiltArgs(t, bin,
	// "ends"): the one case whose loop ends; the others never do.
	"examples/endless-loops/main.tck": {{name: "ends", args: []string{"ends"}}},
	// tack/main_test.go's TestBuiltFmtStreamsInOnePlaceKeepCallOrder:
	// both streams one pipe, which must hold examples/fmt-interleave/combined.
	"examples/fmt-interleave/main.tck": {{
		name:     "combined",
		combined: true,
		wantFile: "examples/fmt-interleave/combined",
	}},
}

// goIrreproducible are the build-examples cases whose binary the Go
// compiler does not build the same twice, by name, each with why: the
// port's binary cannot be required to match one of them. None so far.
var goIrreproducible = map[string]string{}

// binaryTimeout is how long one run of a built binary may take before
// runBinary kills it and fails the case, a third of compilerTimeout, 20
// seconds times $PORTTEST_TIMEOUT_SCALE.
func binaryTimeout() time.Duration { return compilerTimeout / 3 }

// buildDirs are the directories of one case of a build stage, below its
// own temporary directory: each compiler runs with the same paths, the
// port's first, each made anew, so that what either writes names the
// same paths and neither can read what the other wrote.
type buildDirs struct {
	// work is the copy of the case's tree the compilers build in.
	work string
	// home is the TACK_HOME they run with, a read-only copy of std made
	// once for the case (see readOnlyHome).
	home string
	// cache is the TACK_CACHE they run with.
	cache string
	// tmp is the TMPDIR they run with, which must be empty after a build.
	tmp string
	// out is the --out path, in a directory of its own, whose every file
	// is compared after a build.
	out string
	// runCwd and runTmp are a built binary's working directory and TMPDIR.
	runCwd, runTmp string
}

// newBuildDirs returns the directories of a case below base.
func newBuildDirs(base string) buildDirs {
	return buildDirs{
		work:   filepath.Join(base, "work"),
		home:   filepath.Join(base, "home"),
		cache:  filepath.Join(base, "cache"),
		tmp:    filepath.Join(base, "tmp"),
		out:    filepath.Join(base, "out", "prog"),
		runCwd: filepath.Join(base, "run-cwd"),
		runTmp: filepath.Join(base, "run-tmp"),
	}
}

// fresh removes each of dirs and makes it again, empty.
func fresh(dirs ...string) error {
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("emptying %s: %w", d, err)
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("making %s: %w", d, err)
		}
	}
	return nil
}

// readOnlyHome writes, at home, a copy of what TACK_HOME holds for a
// build, the repository's src/std, and makes it read-only, every file
// and directory, so that a build writing to it fails, and fails the
// case, rather than change std; t's cleanup makes it writable again, for
// its removal.
func readOnlyHome(t *testing.T, home string) {
	t.Helper()
	std := filepath.Join(env.root, "src", "std")
	if err := copyTree(std, filepath.Join(home, "src", "std")); err != nil {
		t.Fatal(err)
	}
	chmodTree := func(dirMode, fileMode os.FileMode) error {
		var dirs []string
		err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case d.IsDir():
				dirs = append(dirs, p)
				return nil
			}
			return os.Chmod(p, fileMode)
		})
		// Directories last, deepest first, so that a read-only one is not
		// walked into.
		for i := len(dirs) - 1; err == nil && i >= 0; i-- {
			err = os.Chmod(dirs[i], dirMode)
		}
		if err != nil {
			return fmt.Errorf("setting the modes of %s: %w", home, err)
		}
		return nil
	}
	t.Cleanup(func() {
		if err := chmodTree(0o755, 0o644); err != nil {
			t.Error(err)
		}
	})
	if err := chmodTree(0o555, 0o444); err != nil {
		t.Fatal(err)
	}
}

// buildCopy writes, in work, which must not exist yet, the copy of the
// tree in's build reads: the directory scopeOf gives, its module or its
// file's directory, below work by its path from the module's root, or,
// for an input outside it, from in.dir. It returns in as it runs in the
// copy.
func buildCopy(in input, work string) (input, error) {
	base := env.root
	if rel, err := filepath.Rel(env.root, in.dir); err != nil || strings.HasPrefix(rel, "..") {
		base = in.dir
	}
	scope, _ := scopeOf(in)
	below, err := filepath.Rel(base, scope)
	if err != nil || strings.HasPrefix(below, "..") {
		return input{}, fmt.Errorf("copying %s: %s is not below %s", in.name, scope, base)
	}
	if err = copyTree(scope, filepath.Join(work, below)); err != nil {
		return input{}, err
	}
	cwd, err := filepath.Rel(base, in.dir)
	if err != nil {
		return input{}, fmt.Errorf("copying %s: %w", in.name, err)
	}
	out := in
	out.dir = filepath.Join(work, cwd)
	return out, nil
}

// built is how one compiler's build of a case ended: its result, the
// files its copy and the --out path's directory held afterwards, the
// binary it built, moved out of the --out path, or "" when it built
// none, with its mode, and the input as it ran in the copy.
type built struct {
	files    map[string]string
	outFiles map[string]string
	bin      string
	in       input
	res      result
	mode     os.FileMode
}

// buildOnce builds in with bin, in d's directories, each made anew but
// home, and moves the binary, when there is one, to keep. It fails t
// when TMPDIR is not empty afterwards, when the --out path exists after
// a failed build, and when it does not after one that exited 0. A not
// implemented answer is returned as it is, nothing checked.
func buildOnce(t *testing.T, bin string, d buildDirs, in input, keep string) built {
	t.Helper()
	copied, err := prepareBuild(d, in)
	if err != nil {
		t.Fatal(err)
	}
	who := "the port"
	if isGo(bin) {
		who = "Go"
	}
	r := runCompiler(t, bin, d.cache, copied, buildArgs(d.out, in))
	if r.notImplemented() {
		return built{res: r, in: copied}
	}
	if left := entries(t, d.tmp); len(left) > 0 {
		t.Errorf("%s's build left in TMPDIR: %s", who, strings.Join(left, ", "))
	}
	files, err := snapshot(d.work)
	if err != nil {
		t.Fatal(err)
	}
	outFiles, err := snapshot(filepath.Dir(d.out))
	if err != nil {
		t.Fatal(err)
	}
	// The binary's bytes are build-identical's to compare.
	if _, ok := outFiles[filepath.Base(d.out)]; ok {
		outFiles[filepath.Base(d.out)] = "(the binary)"
	}
	b := built{res: r, files: files, outFiles: outFiles, in: copied}
	b.bin, b.mode = keepBinary(t, who, r, d.out, keep)
	return b
}

// keepBinary moves the binary the build who ran, which ended r, wrote at
// out to keep and returns keep and the binary's mode, or returns "" when
// the build wrote none. It fails t when a failed build left out, and
// when one that exited 0 did not write it.
func keepBinary(t *testing.T, who string, r result, out, keep string) (string, os.FileMode) {
	t.Helper()
	info, err := os.Stat(out)
	switch {
	case errors.Is(err, os.ErrNotExist) && r.code == 0:
		t.Errorf("%s's build exited 0 and wrote nothing at its --out path", who)
		return "", 0
	case errors.Is(err, os.ErrNotExist):
		return "", 0
	case err != nil:
		t.Fatal(err)
	case r.code != 0:
		t.Errorf("%s's build failed, exit %d, and left its --out path", who, r.code)
		return "", 0
	}
	if err := os.Rename(out, keep); err != nil {
		t.Fatal(err)
	}
	return keep, info.Mode()
}

// prepareBuild makes d's build directories anew, the copy of in's tree
// among them, and returns in as it builds in the copy, with TMPDIR d.tmp
// and TACK_HOME d.home.
func prepareBuild(d buildDirs, in input) (input, error) {
	if err := os.RemoveAll(d.work); err != nil {
		return input{}, fmt.Errorf("emptying %s: %w", d.work, err)
	}
	if err := fresh(d.cache, d.tmp, filepath.Dir(d.out)); err != nil {
		return input{}, err
	}
	copied, err := buildCopy(in, d.work)
	if err != nil {
		return input{}, err
	}
	copied.env = append(slices.Clone(copied.env), "TMPDIR="+d.tmp, "TACK_HOME="+d.home)
	return copied, nil
}

// tempNamesHidden returns r with the random part of the name of the
// directory a build makes in tmp, its TMPDIR, replaced by *: the Go
// driver makes it with os.MkdirTemp("", "tk-*") (tack/build.go), as
// tk-3605748710, the port with std/os's make_temp_dir and the prefix
// tk-, as tk-aB3xY9, and each writes the program's C and tack.h there,
// which clang's notes name. That part differs run to run, and between
// the compilers; every other byte of the path must not, and only a path
// component tk-<letters and digits> right below tmp is changed.
func tempNamesHidden(r result, tmp string) result {
	re := regexp.MustCompile(regexp.QuoteMeta(tmp+string(filepath.Separator)) + `tk-[0-9A-Za-z]+\b`)
	hide := func(b []byte) []byte { return re.ReplaceAllLiteral(b, []byte(tmp+"/tk-*")) }
	r.stdout, r.stderr = hide(r.stdout), hide(r.stderr)
	return r
}

// entries returns the names of what dir holds, failing t when it cannot
// be read.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(es))
	for i, e := range es {
		names[i] = e.Name()
	}
	return names
}

// runBuildCase runs the case in of a build stage, st, with portBin as
// the port: each compiler builds it, the port first, in a fresh copy of
// its tree at the same path, with a fresh TACK_CACHE and TMPDIR at the
// same paths and the same --out path (see buildOnce). It compares the
// driver's standard output, standard error and exit status byte for
// byte, as runCaseWith does, and the files each copy holds afterwards,
// path and bytes, and those the --out path's directory holds, but for
// the binary's bytes; a binary built by one compiler and not the other
// fails the case. When both built a binary, it compares their modes,
// and the binaries byte for byte (see checkIdentical), and runs each,
// with defaultRun and the case's buildRuns, comparing what they do (see
// compareRun). A not implemented answer skips, or fails, as
// runCaseWith's does, and fails as buildDeclineVerdict says too.
func runBuildCase(t *testing.T, st stage, in input, portBin string) {
	t.Helper()
	base := t.TempDir()
	d := newBuildDirs(base)
	readOnlyHome(t, d.home)
	port := buildOnce(t, portBin, d, in, filepath.Join(base, "bin-port"))
	if port.res.notImplemented() {
		if why := buildDeclineVerdict(st.name, env.required); why != "" {
			t.Fatalf("%s, but the port says %q", why,
				strings.TrimSuffix(string(port.res.stderr), "\n"))
		}
		declined(t, st, in, port.res)
	}
	goRun, err := goStackWrapper()
	if err != nil {
		t.Fatal(err)
	}
	want := buildOnce(t, goRun, d, in, filepath.Join(base, "bin-go"))
	port.res, want.res = tempNamesHidden(port.res, d.tmp), tempNamesHidden(want.res, d.tmp)
	compareResults(t, st, want.in, buildArgs(d.out, in), want.res, port.res)
	compareBuilt(t, st, in, d, want, port)
}

// compareBuilt reports how port, the port's build of in, a case of the
// stage st, differs from want, Go's, beyond the driver's output: the
// files each left in the copy and beside the --out path, whether each
// built a binary, and, when both did, their modes, the binaries
// themselves (see checkIdentical) and what each does run with
// defaultRun and the case's buildRuns (see compareRun).
func compareBuilt(t *testing.T, st stage, in input, d buildDirs, want, port built) {
	t.Helper()
	if diff := filesDiff(want.files, port.files); diff != "" {
		t.Errorf("the files the builds leave differ:%s", diff)
	}
	if diff := filesDiff(want.outFiles, port.outFiles); diff != "" {
		t.Errorf("the files beside the --out path differ:%s", diff)
	}
	if why := oneBuilt(want.bin, port.bin); why != "" {
		t.Error(why)
		return
	}
	if want.bin == "" {
		return
	}
	if want.mode != port.mode {
		t.Errorf("binary mode: port's %v, Go's %v", port.mode, want.mode)
	}
	if st.name == buildExamplesStage || st.name == buildProbesStage {
		checkIdentical(t, in, want.bin, port.bin, identicalRequired(st.name, env.required))
	}
	for _, r := range append([]buildRun{defaultRun}, buildRuns[in.name]...) {
		compareRun(t, r, d, want.bin, port.bin)
	}
}

// oneBuilt returns why a case fails whose builds left the binaries goBin
// and portBin, "" for none: one compiler built a binary and the other
// did not; or "" when both or neither did.
func oneBuilt(goBin, portBin string) string {
	switch {
	case goBin != "" && portBin == "":
		return "Go built a binary and the port did not"
	case goBin == "" && portBin != "":
		return "the port built a binary and Go did not"
	}
	return ""
}

// goStack is the wrapper the build stages run the Go compiler through,
// written once a run (see goStackWrapper).
var goStack struct {
	err  error
	path string
	once sync.Once
}

// goStackWrapper returns the path of a shell script, written once a run
// in the run's temporary directory, that runs the Go compiler with its
// stack held to deepStack KB, times stackScale, as the port's every run
// is (see writeStackWrapper): the clang a build runs inherits the
// limit, so both compilers' clang runs under the same one.
func goStackWrapper() (string, error) {
	goStack.once.Do(func() {
		scale, err := stackScale()
		if err != nil {
			goStack.err = err
			return
		}
		goStack.path = goStackPath()
		script := fmt.Sprintf("#!/bin/sh\nulimit -s %d || exit 125\nexec %q \"$@\"\n",
			deepStack*scale, env.goBin)
		//nolint:gosec // G306: a script it runs
		if err := os.WriteFile(goStack.path, []byte(script), 0o700); err != nil {
			goStack.err = fmt.Errorf("writing the Go compiler's stack wrapper: %w", err)
		}
	})
	return goStack.path, goStack.err
}

// goStackPath returns where goStackWrapper writes its script. It reads
// only env, which TestMain sets before any test runs, so parallel cases
// may call it while goStackWrapper is writing goStack.
func goStackPath() string {
	return filepath.Join(env.tmp, "go-stack")
}

// isGo reports whether bin, a compiler runCompiler runs, is the Go
// compiler, directly or through goStackWrapper's script.
func isGo(bin string) bool {
	return bin == env.goBin || bin == goStackPath()
}

// buildDeclineVerdict returns why a case of the stage named stageName
// that the port declines must fail, beyond what declined decides, with
// required the names required.txt holds, or "": a case of build-examples
// or build-probes fails while build-identical is required, which checks
// their binaries.
func buildDeclineVerdict(stageName string, required map[string]bool) string {
	if (stageName == buildExamplesStage || stageName == buildProbesStage) &&
		required[buildIdenticalCheck] {
		return "check " + buildIdenticalCheck + " is required"
	}
	return ""
}

// identicalRequired returns required, the names required.txt holds, as
// build-identical reads them for a case of the stage named stageName:
// with build-identical in them for a case of build-probes while
// build-probes is required, whose binaries, built at every -O level,
// must then be Go's, so that a port that loses its -O flag fails; and as
// they are for any other. opt-2 cannot fail so: Go's -O2 binary of the
// loops probe is its -O3 one, the same bytes, so a port that loses -O2,
// or maps it to -O3, still matches; the probe needs a program whose -O2
// and -O3 binaries differ, as one using std, which D2 builds.
func identicalRequired(stageName string, required map[string]bool) map[string]bool {
	if stageName != buildProbesStage || !required[buildProbesStage] || required[buildIdenticalCheck] {
		return required
	}
	with := make(map[string]bool, len(required)+1)
	for name, on := range required {
		with[name] = on
	}
	with[buildIdenticalCheck] = true
	return with
}

// checkIdentical compares goBin and portBin, the two compilers' builds of
// in, by their SHA-256, and logs or fails t as identicalVerdict says.
func checkIdentical(t *testing.T, in input, goBin, portBin string, required map[string]bool) {
	t.Helper()
	goSum, portSum := "", ""
	if _, ok := goIrreproducible[in.name]; !ok {
		goSum, portSum = fileSHA256(t, goBin), fileSHA256(t, portBin)
	}
	msg, fails := identicalVerdict(in.name, goSum, portSum, required)
	if fails {
		t.Error(msg)
		return
	}
	t.Log(msg)
}

// identicalVerdict returns what build-identical says of the case named
// name whose binaries have the SHA-256 sums goSum and portSum, and
// whether the case fails: binaries that differ fail it when required
// holds build-identical, and are logged otherwise; a case of
// goIrreproducible is never compared.
func identicalVerdict(name, goSum, portSum string, required map[string]bool) (string, bool) {
	if why, ok := goIrreproducible[name]; ok {
		return "binaries not compared: Go's build is not reproducible: " + why, false
	}
	switch {
	case goSum == portSum:
		return "binaries identical: sha256 " + goSum, false
	case required[buildIdenticalCheck]:
		return fmt.Sprintf("binaries differ: Go sha256 %s, port sha256 %s", goSum, portSum), true
	default:
		return fmt.Sprintf("binaries differ: Go sha256 %s, port sha256 %s (%s is not required)",
			goSum, portSum, buildIdenticalCheck), false
	}
}

// fileSHA256 returns the SHA-256 of the file at p, in hex, failing t when
// it cannot be read.
func fileSHA256(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p) //nolint:gosec // G304: a binary the case built
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ran is how one run of a built binary ended: what it wrote and left in
// its working directory, its exit status, and the signal that ended it,
// when one did.
type ran struct {
	// files are what the run left in its working directory (see
	// snapshot).
	files map[string]string
	result
	signal syscall.Signal
}

// compareRun runs goBin and portBin the way r says, each in d's run
// directories made anew, and reports how the port's run differs from
// Go's: its exit status or signal, its standard output and its standard
// error, byte for byte, and, for a run with a wantFile, how each one's
// standard output differs from the file.
func compareRun(t *testing.T, r buildRun, d buildDirs, goBin, portBin string) {
	t.Helper()
	port := runBinary(t, r, d, portBin)
	want := runBinary(t, r, d, goBin)
	if port.code != want.code || port.signal != want.signal {
		t.Errorf("run %s: exit status: port's binary %d (signal %d), Go's %d (signal %d)",
			r.name, port.code, port.signal, want.code, want.signal)
	}
	if diff := firstDiff(want.stdout, port.stdout); diff != "" {
		t.Errorf("run %s: standard output differs: %s", r.name, diff)
	}
	if diff := firstDiff(want.stderr, port.stderr); diff != "" {
		t.Errorf("run %s: standard error differs: %s", r.name, diff)
	}
	if diff := filesDiff(want.files, port.files); diff != "" {
		t.Errorf("run %s: what the runs leave in their working directory differs:%s", r.name, diff)
	}
	if r.wantFile != "" {
		compareWantFile(t, r, want.stdout, port.stdout)
	}
}

// compareWantFile reports how goOut and portOut, the standard outputs of
// the two binaries' runs r, differ from r.wantFile.
func compareWantFile(t *testing.T, r buildRun, goOut, portOut []byte) {
	t.Helper()
	file, err := os.ReadFile(filepath.Join(env.root, filepath.FromSlash(r.wantFile)))
	if err != nil {
		t.Fatal(err)
	}
	for who, got := range map[string][]byte{"Go's": goOut, "the port's": portOut} {
		if diff := firstDiff(file, got); diff != "" {
			t.Errorf("run %s: %s binary does not write %s (Go: the file): %s",
				r.name, who, r.wantFile, diff)
		}
	}
}

// runBinary runs bin the way r says, its standard input /dev/null, in
// d.runCwd with TMPDIR d.runTmp, both made anew, and returns how it
// ended. It fails t when the binary cannot be started, runs for longer
// than binaryTimeout, when it is killed with every process it started,
// ends leaving one running (see runInGroup), each with its standard
// error, or leaves anything in its TMPDIR. What it leaves in its working
// directory is in the result, read before the next run makes it anew.
func runBinary(t *testing.T, r buildRun, d buildDirs, bin string) ran {
	t.Helper()
	if err := fresh(d.runCwd, d.runTmp); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), binaryTimeout())
	defer cancel()
	cmd := binaryCommand(ctx, r, d, bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if r.combined {
		cmd.Stderr = &stdout
	}
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, true), "run "+r.name+": "+bin, cmd, err, outlived,
		binaryTimeout(), stderr.Bytes())
	if left := entries(t, d.runTmp); len(left) > 0 {
		t.Errorf("run %s: %s left in TMPDIR: %s", r.name, bin, strings.Join(left, ", "))
	}
	files, err := snapshot(d.runCwd)
	if err != nil {
		t.Fatal(err)
	}
	out := ran{result: result{stdout: stdout.Bytes(), stderr: stderr.Bytes()}, files: files}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		out.code, out.signal = -1, ws.Signal()
	} else {
		out.code = cmd.ProcessState.ExitCode()
	}
	return out
}

// binaryCommand returns the command that runs bin the way r says, through
// sh for a run with a shell, in d.runCwd with TMPDIR d.runTmp, under
// ctx.
func binaryCommand(
	ctx context.Context, r buildRun, d buildDirs, bin string,
) *exec.Cmd {
	var cmd *exec.Cmd
	if r.shell != "" {
		cmd = exec.CommandContext(ctx, "sh", append([]string{"-c", r.shell, bin}, r.args...)...)
	} else {
		cmd = exec.CommandContext(ctx, bin, r.args...) //nolint:gosec // G204: a binary the case built
	}
	cmd.Dir = d.runCwd
	cmd.Env = append(runEnv(r.drop), append(slices.Clone(r.env), "TMPDIR="+d.runTmp)...)
	return cmd
}

// runEnv returns the environment a built binary inherits: this
// process's, but TMPDIR, which each run sets, and the variables any of
// drop starts.
func runEnv(drop []string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMPDIR=") ||
			slices.ContainsFunc(drop, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// TestIdenticalVerdict checks identicalVerdict with required checks of
// its own: equal binaries pass; differing ones fail only while
// build-identical is required; a case of goIrreproducible never fails.
func TestIdenticalVerdict(t *testing.T) {
	cases := []struct {
		required             map[string]bool
		name, goSum, portSum string
		fails                bool
	}{
		{requiredOf(buildIdenticalCheck), "same", "a", "a", false},
		{requiredOf(buildIdenticalCheck), "differ, required", "a", "b", true},
		{requiredOf(buildExamplesStage), "differ, not required", "a", "b", false},
	}
	for _, tc := range cases {
		if _, fails := identicalVerdict(tc.name, tc.goSum, tc.portSum, tc.required); fails != tc.fails {
			t.Errorf("%s: fails %v, want %v", tc.name, fails, tc.fails)
		}
	}
	goIrreproducible["irreproducible"] = "a test's"
	defer delete(goIrreproducible, "irreproducible")
	_, fails := identicalVerdict("irreproducible", "a", "b", requiredOf(buildIdenticalCheck))
	if fails {
		t.Error("a case of goIrreproducible fails")
	}
}

// TestTempNamesHidden checks that tempNamesHidden hides only the letters
// and digits ending a name made below the TMPDIR, Go's digits and the
// port's mkdtemp characters alike, on both streams, keeping its start,
// the rest of the path and every other path.
func TestTempNamesHidden(t *testing.T) {
	const tmp = "/w/tmp"
	r := tempNamesHidden(result{
		stdout: []byte("/w/tmp/tk-123/tack.h:1:2: note and /w/tmp/tk-aB3xY9/program.c\n"),
		stderr: []byte("/w/tmp/tk-9/a.c and /w/tmp/tk-9/b7.c, /w/other/tk-1/x.c, /w/tmp/plain\n"),
	}, tmp)
	if want := "/w/tmp/tk-*/tack.h:1:2: note and /w/tmp/tk-*/program.c\n"; string(r.stdout) != want {
		t.Errorf("stdout %q, want %q", r.stdout, want)
	}
	want := "/w/tmp/tk-*/a.c and /w/tmp/tk-*/b7.c, /w/other/tk-1/x.c, /w/tmp/plain\n"
	if string(r.stderr) != want {
		t.Errorf("stderr %q, want %q", r.stderr, want)
	}
	for _, kept := range []string{
		"/w/tmp/tack.h:12:3: note\n", "/w/tmp/tk-/a.c\n", "/w/tmp/tk_12/a.c\n", "/w/tmp/a/tk-12/b.c\n",
		"/w/tmp/xtk-1/a\n", "/w/tmp/tack-aB3xY9/a.c\n",
	} {
		if got := tempNamesHidden(result{stderr: []byte(kept)}, tmp); string(got.stderr) != kept {
			t.Errorf("tempNamesHidden(%q) = %q, want it unchanged", kept, got.stderr)
		}
	}
}

// TestIdenticalRequired checks identicalRequired: build-identical is
// required for a case of build-probes while build-probes is, and for no
// other stage's case unless required.txt holds it.
func TestIdenticalRequired(t *testing.T) {
	cases := []struct {
		required map[string]bool
		stage    string
		want     bool
	}{
		{requiredOf(buildProbesStage), buildProbesStage, true},
		{requiredOf(buildProbesStage), buildExamplesStage, false},
		{requiredOf(), buildProbesStage, false},
		{requiredOf(buildIdenticalCheck), buildExamplesStage, true},
	}
	for _, tc := range cases {
		if got := identicalRequired(tc.stage, tc.required)[buildIdenticalCheck]; got != tc.want {
			t.Errorf("%s with %v required: build-identical %v, want %v", tc.stage, tc.required, got,
				tc.want)
		}
	}
}

// TestBuildDeclineVerdict checks buildDeclineVerdict with required names
// of its own: a declined case of build-examples or build-probes fails
// while build-identical is required, and no other.
func TestBuildDeclineVerdict(t *testing.T) {
	cases := []struct {
		required map[string]bool
		stage    string
		fails    bool
	}{
		{requiredOf(buildIdenticalCheck), buildExamplesStage, true},
		{requiredOf(buildIdenticalCheck), buildProbesStage, true},
		{requiredOf(buildIdenticalCheck), buildInvalidCStage, false},
		{requiredOf(), buildExamplesStage, false},
		{requiredOf(buildExamplesStage), buildExamplesStage, false},
	}
	for _, tc := range cases {
		if why := buildDeclineVerdict(tc.stage, tc.required); (why != "") != tc.fails {
			t.Errorf("%s with %v required: verdict %q, want failing %v", tc.stage, tc.required, why,
				tc.fails)
		}
	}
}

package porttest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// cleanStage is the name of the stage that runs --check on the programs
// of the corpus that are not cases of an archive.
const cleanStage = "check-clean"

// init adds the --check stages to stages, after the others: one for the
// cases of the type checker's of each stage the checker is ported in,
// check-decls through check-generic, check-backend for those the backend
// rejects, and check-clean. Each runs
//
//	tack --check --max-errors 0 <file or directory>
//
// as the driver is run to check a program, every error reported, and
// compares standard output, standard error and the exit status byte for
// byte. check-backend and check-clean classify their cases by the emit
// stages they need, as the emit stages do: what --check shows of a
// program the type checker accepts is the backend's.
func init() {
	for s := stageDecls; s <= stageBackend; s++ {
		st := stage{
			name:   checkStageInfo[s].stage,
			args:   checkArgs,
			inputs: func() ([]input, error) { return checkInputs(s) },
			stdout: compareBytes,
		}
		if s == stageBackend {
			st.classify = classifyWith(checkArgs)
		}
		stages = append(stages, st)
	}
	stages = append(stages, stage{
		name:     cleanStage,
		args:     checkArgs,
		inputs:   cleanInputs,
		stdout:   compareBytes,
		classify: classifyWith(checkArgs),
	})
}

// checkArgs returns the command line of a check stage for in: --check,
// with no limit on the errors reported, of in's file.
func checkArgs(in input) []string {
	return []string{"--check", "--max-errors", "0", in.file}
}

// checkInputs returns the cases of the type checker's whose stage is s,
// in archive order, each named and run as the single-file and package
// stages name and run its archive's cases, but for a case with a std of
// its own, which runs with a TACK_HOME whose std is that (see
// checkInput).
func checkInputs(s checkStage) ([]input, error) {
	cases, err := classifiedCases()
	if err != nil {
		return nil, err
	}
	var out []input
	for _, c := range cases {
		if c.stage != s {
			continue
		}
		in, err := checkInput(c)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// checkInput returns the input that runs c, from its archive's
// directory. A case with a std of its own runs with TACK_HOME set to a
// directory of its own, beside the archives', whose src/std holds that
// std's files, as test/ loads it with that std in place of the
// repository's.
func checkInput(c checkCase) (input, error) {
	i := slices.IndexFunc(env.archives, func(a archive) bool { return a.name == c.archive })
	if i < 0 {
		return input{}, fmt.Errorf("the archive %s of case %s is not written", c.archive, c.name)
	}
	a := env.archives[i]
	in := input{name: a.name + "/" + c.target, dir: a.dir, file: c.target}
	if c.std != nil {
		home, err := caseHome(a, c)
		if err != nil {
			return input{}, err
		}
		in.env = []string{"TACK_HOME=" + home}
	}
	return in, nil
}

// homes are the TACK_HOME directories made for cases with a std of
// their own, by archive and case name, each made once a run.
var homes struct {
	made map[string]string
	mu   sync.Mutex
}

// caseHome returns the TACK_HOME for c, of archive a, writing c's std to
// its src/std the first time: homes/<archive>/<case> beside a's
// directory, outside every archive's.
func caseHome(a archive, c checkCase) (string, error) {
	homes.mu.Lock()
	defer homes.mu.Unlock()
	key := a.name + "/" + c.name
	if home, ok := homes.made[key]; ok {
		return home, nil
	}
	home := filepath.Join(filepath.Dir(a.dir), "homes", a.name, c.name)
	for _, f := range c.std {
		p := filepath.Join(home, "src", "std", filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", fmt.Errorf("writing the std of %s: %w", key, err)
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil { //nolint:gosec // G306: test files
			return "", fmt.Errorf("writing the std of %s: %w", key, err)
		}
	}
	if homes.made == nil {
		homes.made = map[string]string{}
	}
	homes.made[key] = home
	return home, nil
}

// cleanInputs returns every package input of the corpus that is no case
// of an archive: the examples, std's packages and modules, and the port's
// own driver, which Go's --check passes or reports as it does.
func cleanInputs() ([]input, error) {
	ins, err := packageInputs()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(ins, func(in input) bool {
		return slices.ContainsFunc(env.archives, func(a archive) bool { return a.dir == in.dir })
	}), nil
}

// TestCheckExpected runs the Go compiler on every case of the check
// stages as they run it and checks that it gives the case's expected
// output, as test/ renders it, on standard error, with nothing on
// standard output: so the command line the stages compare the port on
// is the one the archive's output was made with, and a case that does
// not load as test/ loads it fails here, not silently in both
// compilers. A case of a warnings archive must exit 0, and any other
// must fail, with status 1. A file of a case's own std is named, in the
// expected output, below the case's .std directory, and by Go below the
// TACK_HOME it runs with; it is compared under the archive's name. Like
// TestPort it runs only with TACK_PORT_TEST=1 and -count.
func TestCheckExpected(t *testing.T) {
	ready(t)
	cases, err := classifiedCases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		in, err := checkInput(c)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			checkExpected(t, c, in)
		})
	}
}

// checkExpected runs the Go compiler on c, as in, and checks it gives
// c's expected output, as TestCheckExpected says.
func checkExpected(t *testing.T, c checkCase, in input) {
	t.Helper()
	got := runCompiler(t, env.goBin, env.goCache, in, checkArgs(in))
	want, wantCode := expectedCheck(c)
	stderr := got.stderr
	for _, e := range in.env {
		if home, ok := strings.CutPrefix(e, "TACK_HOME="); ok {
			stderr = bytes.ReplaceAll(stderr, []byte(filepath.ToSlash(home)+"/src/std/"),
				[]byte(c.name+"/.std/"))
		}
	}
	if why, ok := checkDiffers[c.archive+"/"+c.name]; ok {
		if got.code == wantCode && bytes.Equal(stderr, want) {
			t.Fatalf("listed in checkDiffers (%s), but --check gives the expected output; "+
				"remove it from the list", why)
		}
		t.Skip("--check differs from test/ by design: " + why)
	}
	if got.code != wantCode {
		t.Errorf("exit status %d, want %d", got.code, wantCode)
	}
	if len(got.stdout) > 0 {
		t.Errorf("standard output is not empty: %q", got.stdout)
	}
	if d := firstDiff(want, stderr); d != "" {
		t.Errorf("standard error is not the archive's expected output: %s\n"+
			"command: (cd %s && tack %s)", d, in.dir, strings.Join(checkArgs(in), " "))
	}
}

// The reasons the driver's --check gives some cases other output than
// test/'s pipeline, which made their expected output.
const (
	// whyWarnings: test/ shows only the backend's errors for a program
	// the backend rejects; --check shows the type checker's warnings
	// with them, sorted among them.
	whyWarnings = "the backend's errors come with the warnings"
	// whyMainOptional: --check checks a program with no func main, or a
	// package that is not main, as a library, so it reports no missing
	// main.
	whyMainOptional = "--check needs no main"
	// whyStdPath: test/ names std by its path in the repository, src/std;
	// the driver by TACK_HOME's, absolute.
	whyStdPath = "std is named by its absolute path"
)

// checkDiffers are the cases for which Go's --check does not give the
// archive's expected output, by archive and case name, with why: their
// check stages still compare the port with Go's --check, and
// TestCheckExpected fails if one of them stops differing, so the list
// cannot go stale.
var checkDiffers = map[string]string{
	"invalid/any-reserved-helper":                whyWarnings,
	"invalid/any-reserved-id-field":              whyWarnings,
	"invalid/any-reserved-is":                    whyWarnings,
	"invalid/any-reserved-type":                  whyWarnings,
	"invalid/error-reserved-field":               whyWarnings,
	"invalid/error-reserved-helper":              whyWarnings,
	"invalid/error-reserved-type":                whyWarnings,
	"invalid/extern-header-clash":                whyWarnings,
	"invalid/extern-header-field-guard":          whyWarnings,
	"invalid/extern-header-field-std-macro":      whyWarnings,
	"invalid/extern-header-helper-clash":         whyWarnings,
	"invalid/extern-header-std-guard-clash":      whyWarnings,
	"invalid/extern-header-std-macro-clash":      whyWarnings,
	"invalid/extern-keyword-names":               whyWarnings,
	"invalid/member-extern-field-bool":           whyWarnings,
	"invalid/view-header-clash":                  whyWarnings,
	"invalid-packages/extern-same-package-names": whyWarnings,
	"invalid/no-main-func":                       whyMainOptional,
	"invalid/no-main-package":                    whyMainOptional,
	"invalid-packages/main-missing-across-files": whyMainOptional,
	"invalid-packages/library-target":            whyMainOptional,
	"invalid-packages/not-main-package":          whyMainOptional,
	"invalid-packages/import-std":                whyStdPath,
}

// expectedCheck returns what Go's --check must write to standard error
// for c, and its exit status: a warnings case's expected output, with
// status 0, and otherwise the part of an invalid case's after its stage
// line and ---, with status 1.
func expectedCheck(c checkCase) ([]byte, int) {
	if strings.HasPrefix(c.archive, "warnings") {
		return c.expected, 0
	}
	_, out, _ := bytes.Cut(c.expected, []byte("---\n"))
	return out, 1
}

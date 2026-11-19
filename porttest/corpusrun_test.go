package porttest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// corpusRunnerStage is the stage of the port's own corpus runner: its
// one case builds the port's tools/corpus with the port and runs it over
// the port's corpus, with the port as the compiler it checks (see
// runCorpusRunner).
const corpusRunnerStage = "corpus-runner"

// corpusRunnerTimeouts is how many of compilerTimeout one run of the
// corpus runner may take: it runs the compiler on each of the corpus's
// three thousand cases, one at a time, which takes the port about half a
// minute.
const corpusRunnerTimeouts = 20

// init adds corpus-runner to stages.
func init() {
	stages = append(stages, stage{
		name:   corpusRunnerStage,
		args:   func(input) []string { return []string{"--out", "corpus", "./tools/corpus"} },
		inputs: corpusRunnerInputs,
		stdout: compareBytes,
		run:    runCorpusRunner,
	})
}

// corpusRunnerInputs returns corpus-runner's one input: the port's
// source tree as the working directory.
func corpusRunnerInputs() ([]input, error) {
	return []input{{name: "corpus", dir: env.home, suite: corpusRunnerStage}}, nil
}

// runCorpusRunner builds the port's tools/corpus with portBin, the port,
// in the port's source tree, and runs it over the port's corpus,
// portCorpusDir, with the port's std and portBin as the compiler it
// checks, in a temporary directory of the case's own; the case fails
// unless the runner passes, exit status 0, its FAIL lines shown. A
// decline of the build skips, or fails the case once the stage is
// required, as any case's does. The runner needs no Go: it is the check
// the port runs of itself once Go is gone, which this keeps working. A
// port that is the Go compiler, byte for byte, as TACK_PORT_BIN names
// it to check the harness, skips: Go has none of the flags the runner
// needs.
func runCorpusRunner(t *testing.T, st stage, in input, portBin string) {
	goAsPort, err := sameBytes(env.portBin, env.goBin)
	if err != nil {
		t.Fatal(err)
	}
	if goAsPort {
		t.Skip("the port is the Go compiler, which has no --port-case or --port-test-case: " +
			"corpus-runner checks the port alone, and compares nothing with Go")
	}
	tool := filepath.Join(t.TempDir(), "corpus")
	built := runCompiler(t, portBin, env.portCache, in, []string{"--out", tool, "./tools/corpus"})
	if built.notImplemented() {
		declined(t, st, in, built)
	}
	if built.code != 0 {
		t.Fatalf("building tools/corpus with the port: exit status %d\n%s", built.code, built.stderr)
	}
	timeout := corpusRunnerTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, portBin, filepath.Join(env.home, portCorpusDir), homeStd()) //nolint:gosec // G204: the tool just built
	cmd.Dir = env.home
	cmd.Env = append(compilerEnv(env.portCache), "TMPDIR="+t.TempDir())
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running tools/corpus", cmd, err, outlived,
		timeout, failLines(out.Bytes()))
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Fatalf("tools/corpus exited %d:\n%s", code, failLines(out.Bytes()))
	}
}

// sameBytes reports whether the files a and b hold the same bytes, as
// the port's binary and the Go compiler's do when TACK_PORT_BIN names
// the Go compiler to check the harness.
func sameBytes(a, b string) (bool, error) {
	x, err := os.ReadFile(a) //nolint:gosec // G304: a compiler's binary
	if err != nil {
		return false, err
	}
	y, err := os.ReadFile(b) //nolint:gosec // G304: a compiler's binary
	if err != nil {
		return false, err
	}
	return bytes.Equal(x, y), nil
}

// failCap is how many lines of a failed run's output failLines keeps,
// besides the count that ends it.
const failCap = 200

// failLines returns the lines of out, the corpus runner's output, that
// say what failed: each FAIL line with the indented lines after it, and
// every line of the runner's own, starting corpus:, which names what
// stopped it and then the error, or counts the cases. Past failCap lines
// it keeps the first failCap, then ..., then the last, the count.
func failLines(out []byte) []byte {
	var kept [][]byte
	lines := bytes.Split(bytes.TrimSuffix(out, []byte("\n")), []byte("\n"))
	inFail := false
	for i, line := range lines {
		switch {
		case bytes.HasPrefix(line, []byte("FAIL")):
			inFail = true
		case !bytes.HasPrefix(line, []byte("  ")):
			inFail = false
		}
		if inFail || bytes.HasPrefix(line, []byte("corpus:")) || i == len(lines)-1 {
			kept = append(kept, line)
		}
	}
	if len(kept) > failCap+1 {
		last := kept[len(kept)-1]
		kept = append(kept[:failCap:failCap], []byte("..."), last)
	}
	return bytes.Join(kept, []byte("\n"))
}

// TestFailLines checks failLines, which corpus-runner shows a failed
// run's output through: it keeps each FAIL line with the indented lines
// after it, every corpus: line, an error's two among them, and the
// count, and drops what else the runner writes; and past failCap lines
// it keeps the first failCap, ... and the count. It needs no port and
// runs either way.
func TestFailLines(t *testing.T) {
	out := "updated a\ncorpus: cannot run timeout, from PATH, in /r: no such file\ncorpus: no such file\n" +
		"FAIL x/y: why\n  first difference\n    want a\nother\nFAIL z: why\ncorpus: 3 cases, 2 failed\n"
	want := "corpus: cannot run timeout, from PATH, in /r: no such file\ncorpus: no such file\n" +
		"FAIL x/y: why\n  first difference\n    want a\nFAIL z: why\ncorpus: 3 cases, 2 failed"
	if got := string(failLines([]byte(out))); got != want {
		t.Errorf("failLines gave\n%s\nwant\n%s", got, want)
	}
	var many strings.Builder
	for i := range failCap + 50 {
		fmt.Fprintf(&many, "FAIL c%d: why\n", i)
	}
	many.WriteString("corpus: 250 cases, 250 failed\n")
	lines := strings.Split(string(failLines([]byte(many.String()))), "\n")
	switch {
	case len(lines) != failCap+2:
		t.Errorf("failLines kept %d lines of %d, want %d", len(lines), failCap+51, failCap+2)
	case lines[failCap-1] != fmt.Sprintf("FAIL c%d: why", failCap-1) || lines[failCap] != "..." ||
		lines[failCap+1] != "corpus: 250 cases, 250 failed":
		t.Errorf("failLines ended %q, want the first %d, ... and the count", lines[failCap-1:], failCap)
	}
}

// TestCorpusRunnerChecks checks, with the port's tools/corpus built with
// the port, what the corpus-runner stage's passing run cannot show: that
// the runner refuses a $TMPDIR below a directory holding a tack.mod and
// leaves nothing there; that it removes its temporary directory when a
// run stops on an error, timeout missing from PATH; that an archive of a
// kind it does not know fails; that a compiler whose output changes with
// CRLF line endings is caught; that a compiler that is not there fails
// once, before any case runs; and that the port's --port-std with an
// empty name is a usage error. The compilers but the port's are shell
// stubs written before the runner runs. It runs only as TestPort does.
func TestCorpusRunnerChecks(t *testing.T) {
	ready(t)
	tool := buildCorpusRunner(t)
	corpus := filepath.Join(env.home, portCorpusDir)
	t.Run("tmpdir-below-module", func(t *testing.T) {
		mod := t.TempDir()
		mustDo(t, os.WriteFile(filepath.Join(mod, "tack.mod"), []byte("module m\n"), 0o600))
		tmp := filepath.Join(mod, "tmp")
		mustDo(t, os.Mkdir(tmp, 0o700))
		out, code := runTool(t, tool, []string{"TMPDIR=" + tmp}, env.portBin, corpus, homeStd())
		if code != 1 || !strings.Contains(out, filepath.Join(mod, "tack.mod")+" is above the run's temporary directory") {
			t.Errorf("exit %d, output:\n%s\nwant exit 1 and the refusal naming %s", code, out, mod)
		}
		checkEmpty(t, tmp)
	})
	t.Run("cleanup-on-error", func(t *testing.T) {
		tmp := t.TempDir()
		out, code := runTool(t, tool, []string{"TMPDIR=" + tmp, "PATH=" + t.TempDir()}, env.portBin, corpus, homeStd())
		if code != 1 || !strings.Contains(out, "corpus: cannot run timeout, from PATH, in ") {
			t.Errorf("exit %d, output:\n%s\nwant exit 1 and the command that could not run", code, out)
		}
		checkEmpty(t, tmp)
	})
	t.Run("unknown-archive", func(t *testing.T) {
		c := stubCorpus(t, map[string]string{"examples/new.txtar": "-- a/main.tck --\npackage main\n-- a/expected --\nx\n"})
		out, code := runTool(t, tool, []string{"TMPDIR=" + t.TempDir()}, env.portBin, c, homeStd())
		if code != 1 || !strings.Contains(out, "FAIL new.txtar/(archive): the runner does not know this archive") {
			t.Errorf("exit %d, output:\n%s\nwant exit 1 and new.txtar failed", code, out)
		}
	})
	t.Run("crlf-sensitive", func(t *testing.T) {
		stub := filepath.Join(t.TempDir(), "tack")
		mustDo(t, os.WriteFile(stub, []byte("#!/bin/sh\nfor a; do t=$a; done\n"+
			"if grep -q \"$(printf '\\r')\" \"$t\"; then printf 'stage: parse\\n---\\nCR\\n'; "+
			"else printf 'stage: parse\\n---\\nx\\n'; fi\n"), 0o700)) //nolint:gosec // G306: a stub compiler
		c := stubCorpus(t, map[string]string{
			"examples/invalid.txtar": "-- a/main.tck --\npackage main\n-- a/expected --\nstage: parse\n---\nx\n",
		})
		out, code := runTool(t, tool, []string{"TMPDIR=" + t.TempDir()}, stub, c, homeStd())
		if code != 1 || strings.Contains(out, "FAIL invalid.txtar/a: the output differs from the expected section") ||
			!strings.Contains(out, "FAIL invalid.txtar/a: the output differs from what LF line endings give") {
			t.Errorf("exit %d, output:\n%s\nwant exit 1, the LF run passing and the CRLF run failing", code, out)
		}
	})
	t.Run("missing-compiler", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "tack")
		out, code := runTool(t, tool, []string{"TMPDIR=" + t.TempDir()}, missing, corpus, homeStd())
		if code != 1 || strings.Contains(out, "FAIL") || !strings.Contains(out, "corpus: the compiler, "+missing) {
			t.Errorf("exit %d, output:\n%s\nwant exit 1, no case run and the compiler named once", code, out)
		}
	})
	t.Run("empty-port-std", func(t *testing.T) {
		r := runCompiler(t, env.portRun, env.portCache, input{name: "empty-port-std", dir: t.TempDir()},
			[]string{"--port-case", "--port-std=", "x"})
		if r.code != 2 || !strings.Contains(string(r.stderr), "--port-std needs a directory, not an empty name") {
			t.Errorf("exit %d, stderr %q; want the usage error, exit 2", r.code, r.stderr)
		}
	})
}

// buildCorpusRunner builds the port's tools/corpus with the port, in a
// temporary directory of t's, and returns its path.
func buildCorpusRunner(t *testing.T) string {
	t.Helper()
	tool := filepath.Join(t.TempDir(), "corpus")
	in := input{name: "corpus-tool", dir: env.home}
	built := runCompiler(t, env.portRun, env.portCache, in, []string{"--out", tool, "./tools/corpus"})
	if built.code != 0 {
		t.Fatalf("building tools/corpus with the port: exit status %d\n%s", built.code, built.stderr)
	}
	return tool
}

// stubCorpus writes a corpus of files, by their paths below it, with an
// empty golden/, and returns its directory.
func stubCorpus(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	mustDo(t, os.Mkdir(filepath.Join(dir, "golden"), 0o750))
	for p, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		mustDo(t, os.MkdirAll(filepath.Dir(full), 0o750))
		mustDo(t, os.WriteFile(full, []byte(data), 0o600))
	}
	return dir
}

// runTool runs the corpus runner tool with args, in this process's
// environment with vars after it, in a process group of its own, and
// returns its standard output and standard error together and its exit
// status, failing t when it does not end within the stage's limit.
func runTool(t *testing.T, tool string, vars []string, args ...string) (string, int) {
	t.Helper()
	timeout := corpusRunnerTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...) //nolint:gosec // G204: the tool just built
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), vars...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running tools/corpus", cmd, err, outlived,
		timeout, out.Bytes())
	return out.String(), cmd.ProcessState.ExitCode()
}

// checkEmpty fails t when the directory dir holds anything.
func checkEmpty(t *testing.T, dir string) {
	t.Helper()
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("%s still holds %d entries, %s first", dir, len(left), left[0].Name())
	}
}

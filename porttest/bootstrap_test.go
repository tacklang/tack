package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bootstrapStage is the stage of the port's bootstrap: its one case
// builds stage 2, the port's driver built by the port, stage 1, and
// stage 3, the driver built by stage 2, and requires the three to be the
// same bytes (see runBootstrap).
const bootstrapStage = "bootstrap"

// bootstrapTimeouts is how many of compilerTimeout one build of the
// port's driver in the bootstrap may take: a build takes the port about
// half a minute on an idle machine, and the stage's case may run beside
// other work.
const bootstrapTimeouts = 10

// bootstrapArgs are the arguments every stage of the bootstrap is built
// with, after the compiler: the driver, at -O3, the level the compiler
// is built at (the Go compiler's default, which porttest builds stage 1
// with), written to out.
func bootstrapArgs(out string) []string {
	return []string{"-O3", "--out", out, "./driver"}
}

// init adds bootstrap to stages.
func init() {
	stages = append(stages, stage{
		name:   bootstrapStage,
		args:   func(input) []string { return bootstrapArgs("stage2") },
		inputs: bootstrapInputs,
		stdout: compareBytes,
		run:    runBootstrap,
	})
}

// bootstrapInputs returns bootstrap's one input: the port's source tree
// as the working directory.
func bootstrapInputs() ([]input, error) {
	return []input{{name: "driver", dir: env.home, suite: bootstrapStage}}, nil
}

// runBootstrap builds stage 2, the port's driver, with env.portBin,
// stage 1, and stage 3 with stage 2, each as buildStage builds it, and
// fails the case unless the three are the same bytes, naming each pair
// that differs, the first byte offset where it does, and the three
// SHA-256 sums. Stage 1 is the port porttest builds with the Go compiler,
// or the one TACK_PORT_BIN names: stage 1 equals stage 2 because the C
// the port writes, and so the binary clang builds from it, is Go's, and
// stage 2 equals stage 3 because stage 2 is then stage 1. A port that is
// the Go compiler, byte for byte, as TACK_PORT_BIN names it to check the
// harness, skips: the port Go builds is no copy of Go.
func runBootstrap(t *testing.T, st stage, in input, _ string) {
	goAsPort, err := sameBytes(env.portBin, env.goBin)
	if err != nil {
		t.Fatal(err)
	}
	if goAsPort {
		t.Skip("the port is the Go compiler: the port it builds cannot be the same bytes, " +
			"and bootstrap checks the port alone")
	}
	dir := t.TempDir()
	stage2, stage3 := filepath.Join(dir, "stage2"), filepath.Join(dir, "stage3")
	buildStage(t, st, in, env.portBin, stage2)
	buildStage(t, st, in, stage2, stage3)
	sums, diffs, err := bootstrapDiffs([3]string{env.portBin, stage2, stage3})
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) > 0 {
		t.Fatalf("%s\n%s", strings.Join(diffs, "\n"), sums)
	}
	t.Log(sums)
}

// buildStage builds the port's driver at out with compiler, with
// bootstrapArgs, in the port's source tree with the port's std and a
// fresh cache of its own, so the build compiles every object anew and
// reads nothing another stage wrote. compiler runs with its stack held
// to deepStack KB, scaled as every stack wrapper's is, through sh's
// ulimit given on the command line: stage 2 is built during the case,
// and a wrapper script written then could be held open by a parallel
// case's child between its fork and its exec (see writeScripts). It
// runs in a process group of its own, which fails the case when it does
// not end within bootstrapTimeouts times a compiler run's time limit; a
// not implemented answer declines the case, which fails once the stage
// is required, and any other failed build fails it.
func buildStage(t *testing.T, st stage, in input, compiler, out string) {
	t.Helper()
	scale, err := stackScale()
	if err != nil {
		t.Fatal(err)
	}
	limit := fmt.Sprintf("ulimit -s %d || exit 125\nexec \"$0\" \"$@\"\n", deepStack*scale)
	args := append([]string{"-c", limit, compiler}, bootstrapArgs(out)...)
	timeout := bootstrapTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", args...)
	cmd.Dir = in.dir
	cmd.Env = compilerEnv(t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	outlived, err := runInGroup(cmd)
	running := fmt.Sprintf("building %s with %s %s in %s", out, compiler,
		strings.Join(bootstrapArgs(out), " "), in.dir)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), running, cmd, err, outlived,
		timeout, stderr.Bytes())
	built := result{
		stdout: stdout.Bytes(),
		stderr: stderr.Bytes(),
		code:   cmd.ProcessState.ExitCode(),
	}
	if built.notImplemented() {
		declined(t, st, in, built)
	}
	if built.code != 0 {
		t.Fatalf("%s: exit status %d\nstdout:\n%s\nstderr:\n%s", running, built.code,
			built.stdout, built.stderr)
	}
}

// bootstrapDiffs reads the three stages' binaries at paths, stage 1 to
// stage 3, and returns their SHA-256 sums, one line each, and a line for
// each pair of neighboring stages whose bytes differ, naming the first
// byte offset, from 0, where they do and both sizes; when one is a
// prefix of the other, that offset is the shorter one's size. diffs is
// empty when the three are the same bytes.
func bootstrapDiffs(paths [3]string) (sums string, diffs []string, err error) {
	var data [3][]byte
	var lines []string
	for i, p := range paths {
		if data[i], err = os.ReadFile(
			p,
		); err != nil { //nolint:gosec // G304: a stage of the bootstrap
			return "", nil, fmt.Errorf("reading stage %d: %w", i+1, err)
		}
		sum := sha256.Sum256(data[i])
		lines = append(
			lines,
			fmt.Sprintf("stage %d: sha256 %s %s", i+1, hex.EncodeToString(sum[:]), p),
		)
	}
	for i := range 2 {
		if off := firstDiffOffset(data[i], data[i+1]); off >= 0 {
			diffs = append(
				diffs,
				fmt.Sprintf("stage %d and stage %d differ first at byte offset %d "+
					"(sizes %d and %d)", i+1, i+2, off, len(data[i]), len(data[i+1])),
			)
		}
	}
	return strings.Join(lines, "\n"), diffs, nil
}

// firstDiffOffset returns the first byte offset, from 0, where a and b
// differ, the shorter one's length when it is a prefix of the other, or
// -1 when they are the same bytes.
func firstDiffOffset(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

// TestBootstrapDiffs checks bootstrapDiffs, which the bootstrap stage
// reports through, on small files, needing no port: three the same pass
// with their sums; a stage 3 with one byte altered, a stage 1 that
// differs, and a stage 3 one byte longer each fail, naming the pair, the
// offset and the sizes; and a stage that is not there is an error.
func TestBootstrapDiffs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		mustDo(t, os.WriteFile(p, []byte(data), 0o600))
		return p
	}
	same := write("same", "\x7fELF-stage")
	altered := write("altered", "\x7fELF-stAge")
	longer := write("longer", "\x7fELF-stage!")
	sum := sha256.Sum256([]byte("\x7fELF-stage"))
	for _, tc := range []struct {
		name  string
		paths [3]string
		want  []string
	}{
		{"same", [3]string{same, same, same}, nil},
		{
			"stage3-altered",
			[3]string{same, same, altered},
			[]string{"stage 2 and stage 3 differ first at byte offset 7 (sizes 10 and 10)"},
		},
		{
			"stage1-differs",
			[3]string{altered, same, same},
			[]string{"stage 1 and stage 2 differ first at byte offset 7 (sizes 10 and 10)"},
		},
		{
			"stage3-longer",
			[3]string{same, same, longer},
			[]string{"stage 2 and stage 3 differ first at byte offset 10 (sizes 10 and 11)"},
		},
		{"stage2-differs", [3]string{same, longer, same}, []string{
			"stage 1 and stage 2 differ first at byte offset 10 (sizes 10 and 11)",
			"stage 2 and stage 3 differ first at byte offset 10 (sizes 11 and 10)",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sums, diffs, err := bootstrapDiffs(tc.paths)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(diffs, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("diffs %q, want %q", diffs, tc.want)
			}
			line := "stage 1: sha256 " + hex.EncodeToString(sum[:]) + " " + same
			if tc.paths[0] == same && !strings.Contains(sums, line) {
				t.Errorf("sums\n%s\nlack %q", sums, line)
			}
		})
	}
	if _, _, err := bootstrapDiffs(
		[3]string{same, same, filepath.Join(dir, "missing")},
	); err == nil ||
		!strings.Contains(err.Error(), "reading stage 3") {
		t.Errorf("a missing stage 3: %v, want an error naming stage 3", err)
	}
}

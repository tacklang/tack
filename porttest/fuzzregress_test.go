package porttest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fuzzRegressStage is the stage of the port's own fuzz targets: its one
// case runs the port's make fuzz-regress with the port as the compiler
// that builds them (see runFuzzRegress).
const fuzzRegressStage = "fuzz-regress"

// fuzzRegressTimeouts is how many of compilerTimeout one run of the
// port's make fuzz-regress may take: it emits the C of each of its two
// targets with the port, compiles each with clang under libFuzzer and
// the sanitizers, and replays the committed inputs and the corpus's
// three thousand programs through each, which took 35 seconds in all,
// measured, most of it the parser's replay.
const fuzzRegressTimeouts = 10

// init adds fuzz-regress to stages.
func init() {
	stages = append(stages, stage{
		name:   fuzzRegressStage,
		args:   func(input) []string { return []string{"fuzz-regress"} },
		inputs: fuzzRegressInputs,
		stdout: compareBytes,
		run:    runFuzzRegress,
	})
}

// fuzzRegressInputs returns fuzz-regress's one input: the port's source
// tree as the working directory.
func fuzzRegressInputs() ([]input, error) {
	return []input{{name: "fuzz-regress", dir: env.home, suite: fuzzRegressStage}}, nil
}

// runFuzzRegress runs make fuzz-regress in the port's source tree with
// portBin, the port, as TACK, in a process group of its own, with
// FUZZ_BIN and TMPDIR directories of the case's own, so it writes
// nothing into the tree and never into a bin/fuzz a run of make fuzz is
// using, and no make variables or sanitizer options inherited from a
// make that runs porttest, make port-sanitize's among them: fuzz.sh sets
// the sanitizers' options itself, and an inherited LSAN_OPTIONS could
// otherwise turn its leak check off; the case fails unless it passes, exit status 0, its output
// shown. The port's tools/fuzz/fuzz.sh builds each fuzz target of
// tools/fuzz under clang's libFuzzer with the address, leak and
// undefined behavior sanitizers, and replays through it each input of
// tools/fuzz/regress/<target> and every program of the port's corpus,
// failing on any crash, sanitizer report, timeout or broken invariant,
// and on a leak: a buffer of c_allocator still held once an input has
// run, by the runtime's count, or memory C allocated and lost, which
// LeakSanitizer finds. It needs make and clang with libFuzzer, and no Go: it is
// the check of the port's front end under the sanitizers that runs once
// Go is gone, which this keeps working. make missing from PATH fails the
// case, as the stage is required. A port that is the Go compiler, byte
// for byte, skips: Go has neither --port-header nor --port-loaded, which
// the build reads.
func runFuzzRegress(t *testing.T, _ stage, in input, portBin string) {
	goAsPort, err := sameBytes(env.portBin, env.goBin)
	if err != nil {
		t.Fatal(err)
	}
	if goAsPort {
		t.Skip("the port is the Go compiler, which has no --port-header or --port-loaded: " +
			"fuzz-regress checks the port alone, and compares nothing with Go")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("the fuzz-regress stage runs make fuzz-regress: %v", err)
	}
	timeout := fuzzRegressTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "-s", "fuzz-regress", "TACK="+portBin)
	cmd.Dir = in.dir
	var environ []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "TMPDIR", "FUZZ_BIN",
			"ASAN_OPTIONS", "LSAN_OPTIONS", "UBSAN_OPTIONS":
		default:
			environ = append(environ, kv)
		}
	}
	environ = append(environ, "TMPDIR="+t.TempDir(), "FUZZ_BIN="+filepath.Join(t.TempDir(), "fuzz"))
	cmd.Env = environ
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running make fuzz-regress", cmd, err, outlived,
		timeout, out.Bytes())
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Fatalf("make fuzz-regress exited %d:\n%s", code, out.Bytes())
	}
}

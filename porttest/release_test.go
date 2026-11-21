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

// releaseStage is the stage of the port's release scripts: its one case
// runs the port's make test-release with the port as the compiler it
// packages and checks (see runRelease).
const releaseStage = "release"

// releaseTimeouts is how many of compilerTimeout one run of the port's
// make test-release may take: it packages several releases, which takes
// seconds, and runs make install-from-release once, three builds of the
// port's driver at -O3, which take the port about half a minute each.
const releaseTimeouts = 10

// init adds release to stages.
func init() {
	stages = append(stages, stage{
		name:   releaseStage,
		args:   func(input) []string { return []string{"test-release"} },
		inputs: releaseInputs,
		stdout: compareBytes,
		run:    runRelease,
	})
}

// releaseInputs returns release's one input: the port's source tree as
// the working directory.
func releaseInputs() ([]input, error) {
	return []input{{name: "release", dir: env.home, suite: releaseStage}}, nil
}

// runRelease runs make test-release in the port's source tree with
// portBin, the port, as TACK, as runInstall runs make test-install: in a
// process group of its own, with TMPDIR and HOME directories of the
// case's own and no make variables inherited, failing the case unless it
// passes, exit status 0, its output shown. The port's
// tools/release/test.sh packages releases and installs them from a
// file:// directory below TMPDIR, with throwaway minisign keys it makes
// there and a stand-in for gh, so the case writes neither to the tree
// nor to the real home, and reaches no network. It needs make, git,
// minisign and a readelf: the one $READELF names, which it passes on,
// or llvm-readelf or readelf, whichever is on PATH, as the port's
// glibc-check.sh picks one; and no Go: it is the check of the release
// scripts that runs once Go is gone. A port that is the Go compiler, byte for byte, as
// TACK_PORT_BIN names it to check the harness, skips: Go has no
// --version, which the script checks.
func runRelease(t *testing.T, _ stage, in input, portBin string) {
	goAsPort, err := sameBytes(env.portBin, env.goBin)
	if err != nil {
		t.Fatal(err)
	}
	if goAsPort {
		t.Skip("the port is the Go compiler, which has no --version: release checks the port alone")
	}
	readelf := os.Getenv("READELF")
	if readelf == "" {
		readelf = "llvm-readelf"
		if _, err := exec.LookPath(readelf); err != nil {
			readelf = "readelf"
		}
	}
	for _, tool := range []string{"make", "git", "minisign", readelf} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the release stage runs make test-release, which needs %s: %v", tool, err)
		}
	}
	timeout := releaseTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "-s", "test-release", "TACK="+portBin)
	cmd.Dir = in.dir
	var environ []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "TMPDIR", "HOME":
		default:
			environ = append(environ, kv)
		}
	}
	environ = append(environ, "TMPDIR="+t.TempDir(), "HOME="+filepath.Join(t.TempDir(), "home"))
	cmd.Env = environ
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running make test-release", cmd, err, outlived,
		timeout, out.Bytes())
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Fatalf("make test-release exited %d:\n%s", code, out.Bytes())
	}
}

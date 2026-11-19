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

// installStage is the stage of the port's install: its one case runs
// the port's make test-install with the port as the compiler that
// builds it (see runInstall).
const installStage = "install"

// installTimeouts is how many of compilerTimeout one run of the port's
// make test-install may take: it installs twice with the port, each
// three builds of the port's driver at -O3, which take the port about
// half a minute each, and builds and runs a program and std/hex's tests
// with what it installs; its other installs use stand-ins that take no
// time.
const installTimeouts = 20

// init adds install to stages.
func init() {
	stages = append(stages, stage{
		name:   installStage,
		args:   func(input) []string { return []string{"test-install"} },
		inputs: installInputs,
		stdout: compareBytes,
		run:    runInstall,
	})
}

// installInputs returns install's one input: the port's source tree as
// the working directory.
func installInputs() ([]input, error) {
	return []input{{name: "install", dir: env.home, suite: installStage}}, nil
}

// runInstall runs make test-install in the port's source tree with
// portBin, the port, as TACK, in a process group of its own, with
// TMPDIR and HOME directories of the case's own and no make variables
// inherited from a make that runs porttest, and fails the case unless
// it passes, exit status 0, its output shown. The port's
// tools/test-install.sh installs from a copy of the tree into
// directories below TMPDIR, and runs every make and installed compiler
// with an environment of its own, so the case writes neither to the
// tree nor to the real home. It needs make, and no Go: it is the check
// of the port's install that runs once Go is gone, which this keeps
// working. make missing from PATH fails the case, as the stage is
// required.
func runInstall(t *testing.T, _ stage, in input, portBin string) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("the install stage runs make test-install: %v", err)
	}
	timeout := installTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "-s", "test-install", "TACK="+portBin)
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
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running make test-install", cmd, err, outlived,
		timeout, out.Bytes())
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Fatalf("make test-install exited %d:\n%s", code, out.Bytes())
	}
}

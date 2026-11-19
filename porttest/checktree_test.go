package porttest

import "testing"

// checkTreeStage is the stage of the port's check of its own source
// tree: its one case runs the port's tack --check . at the root of the
// port's source tree, which must pass (see runCheckTree).
const checkTreeStage = "check-tree"

// init adds check-tree to stages.
func init() {
	stages = append(stages, stage{
		name:   checkTreeStage,
		args:   func(input) []string { return []string{"--check", "."} },
		inputs: checkTreeInputs,
		stdout: compareBytes,
		run:    runCheckTree,
	})
}

// checkTreeInputs returns check-tree's one input: the port's source
// tree as the working directory.
func checkTreeInputs() ([]input, error) {
	return []input{{name: "tree", dir: env.home, suite: checkTreeStage}}, nil
}

// runCheckTree runs portBin, the port, with --check . in the port's
// source tree, with the port's std, and fails the case unless it exits
// 0, its output shown. --check . checks the tree as a library: each
// directory below its root that holds a .tck file, but those below
// another tack.mod, as corpus/ and runtime/ are, is checked as a
// package of the module, the ignored bin/ included, so a .tck file
// that is no package's source, an expected output among them, fails
// it, as tools/unicodegen's testdata/ok/want.tck did. A decline skips,
// or fails the case once the stage is required, as any case's does. It
// compares nothing with Go.
func runCheckTree(t *testing.T, st stage, in input, portBin string) {
	got := runCompiler(t, portBin, env.portCache, in, []string{"--check", "."})
	if got.notImplemented() {
		declined(t, st, in, got)
	}
	if got.code != 0 {
		t.Fatalf("tack --check . in the port's source tree: exit status %d\nstdout:\n%s\nstderr:\n%s",
			got.code, got.stdout, got.stderr)
	}
}

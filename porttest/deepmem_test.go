package porttest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deepMemLevels is how deeply TestDeepFactsMemory nests its ifs: as
// deeply as the parser allows, less the levels the function around them
// takes.
const deepMemLevels = deepLimit - 10

// deepMemProgram returns a main of n nested ifs, each on a call that
// writes its mut argument and a bool, each holding five writes of that
// argument: every level's condition is a fact whose region holds every
// write below it, so a checker that keeps what each level's walk of its
// region finds keeps records growing with the square of the depth.
func deepMemProgram(n int) string {
	var b strings.Builder
	b.WriteString("package main\n\nfunc tick(mut c int64) int64 {\n    c +%= 1\n    return c\n}\n\n")
	b.WriteString("func tb(b bool) bool {\n    return b\n}\n\n")
	b.WriteString("func main() {\n    mut c int64 = 0\n    var b bool = tb(true)\n")
	for i := range n {
		fmt.Fprintf(&b, "if tick(mut c) > %d && b {\n", i)
		for range 5 {
			fmt.Fprintf(&b, "c +%%= %d\n", i)
		}
	}
	b.WriteString("    print(\"x\")\n")
	b.WriteString(strings.Repeat("}\n", n))
	b.WriteString("}\n")
	return b.String()
}

// TestDeepFactsMemory is the memory tripwire for facts in deep nests: it
// runs --check and --emit of deepMemProgram(deepMemLevels) through both
// compilers, perfRuns times each, after checking that both give the same
// output, and fails as TestResPerf does when the port's peak memory is
// more than perfMemoryFactor times Go's, or its time more than
// perfTimeFactor times Go's and perfTimeSlack. A port that declines
// --emit is measured on --check alone. It runs only as TestPort does,
// and is not parallel.
func TestDeepFactsMemory(t *testing.T) {
	ready(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deep.tck"), []byte(deepMemProgram(deepMemLevels)), 0o600); err != nil {
		t.Fatal(err)
	}
	in := input{name: "deep.tck", dir: dir, file: "deep.tck"}
	for _, flag := range []string{"--check", "--emit"} {
		t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) {
			args := []string{flag, "deep.tck"}
			want := runCompiler(t, env.goBin, env.goCache, in, args)
			got := runCompiler(t, env.portRun, env.portCache, in, args)
			if got.notImplemented() {
				t.Skipf("the port declines: %s", got.stderr)
			}
			if got.code != want.code || !bytes.Equal(got.stdout, want.stdout) ||
				!bytes.Equal(got.stderr, want.stderr) {
				t.Fatalf("the port exited %d, writing\n%s\nGo exited %d, writing\n%s",
					got.code, got.stderr, want.code, want.stderr)
			}
			goTime, goMem := measure(t, env.goBin, env.goCache, dir, args, want.code)
			portTime, portMem := measure(t, env.portRun, env.portCache, dir, args, want.code)
			t.Logf("%s of %d nested ifs: Go %v, %d KB; port %v, %d KB",
				flag, deepMemLevels, goTime, goMem, portTime, portMem)
			if portTime > perfTimeFactor*goTime+perfTimeSlack {
				t.Errorf("the port took %v, more than %d times Go's %v", portTime, perfTimeFactor, goTime)
			}
			if portMem > perfMemoryFactor*goMem {
				t.Errorf("the port's peak memory was %d KB, more than %d times Go's %d KB",
					portMem, perfMemoryFactor, goMem)
			}
		})
	}
}

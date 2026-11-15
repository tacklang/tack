package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// emitPerfStrings is how many distinct string literals the strings
// program of TestEmitPerf prints, each of which the backend pools and
// looks up again where it is used.
const emitPerfStrings = 20000

// emitPerfPrograms are TestEmitPerf's generated programs, each one Go's
// --emit lowers: TestCheckPerf's functions, which own, move, replace,
// range, make regions and defer, with an empty main; TestGenericPerf's
// program, its eight hundred instances and its closures; and a main
// printing emitPerfStrings strings.
var emitPerfPrograms = map[string]func() string{
	"owning.tck": func() string {
		var b strings.Builder
		b.WriteString(checkPerfDecls)
		for i := range checkPerfFuncs {
			fmt.Fprintf(&b, checkPerfFunc, i)
		}
		b.WriteString("func main() {\n}\n")
		return b.String()
	},
	"generic.tck": func() string { return genericPerfProgram(false) },
	"strings.tck": func() string {
		var b strings.Builder
		b.WriteString("package main\n\nfunc main() {\n")
		for i := range emitPerfStrings {
			fmt.Fprintf(&b, "    print(\"s%d\\n\")\n", i)
		}
		b.WriteString("}\n")
		return b.String()
	},
}

// TestEmitPerf is the performance tripwire for the backend: it runs
// --emit of the port's driver, the largest program the corpus has, and
// of each of emitPerfPrograms, first compared as emit-single compares
// its cases, a case the port declines skipping as that stage's do, then
// through both compilers, perfRuns times each, one at a time, failing as
// TestResPerf does when the port takes more than perfTimeFactor times
// Go's time and perfTimeSlack, or perfMemoryFactor times its peak
// memory, and logging both. The driver's case needs the port's source
// tree, and skips without one. It runs only as TestPort does, and is
// not parallel.
func TestEmitPerf(t *testing.T) {
	ready(t)
	st := stageNamed(emitSingleStage)
	if st == nil {
		t.Fatal("no emit-single stage")
	}
	t.Run("driver", func(t *testing.T) {
		src, _ := portSource(os.Getenv("TACK_PORT"))
		if !dirExists(filepath.Join(src, "driver")) {
			t.Skip("no port source tree: the program is the port's driver")
		}
		emitPerf(t, *st, input{name: "port/driver", dir: src, file: "./driver"})
	})
	dir := t.TempDir()
	for file, gen := range emitPerfPrograms {
		t.Run(file, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, file), []byte(gen()), 0o600); err != nil {
				t.Fatal(err)
			}
			emitPerf(t, *st, input{name: file, dir: dir, file: file})
		})
	}
}

// emitPerf compares the port with Go on in as st does, then measures
// both on it, as TestEmitPerf says.
func emitPerf(t *testing.T, st stage, in input) {
	t.Helper()
	runCaseWith(t, st, in, env.portRun)
	args := st.args(in)
	goTime, goMem := measure(t, env.goBin, env.goCache, in.dir, args, 0)
	portTime, portMem := measure(t, env.portRun, env.portCache, in.dir, args, 0)
	t.Logf("--emit %s: Go %v, %d KB; port %v, %d KB", in.file, goTime, goMem, portTime, portMem)
	if portTime > perfTimeFactor*goTime+perfTimeSlack {
		t.Errorf("the port took %v, more than %d times Go's %v", portTime, perfTimeFactor, goTime)
	}
	if portMem > perfMemoryFactor*goMem {
		t.Errorf(
			"the port's peak memory was %d KB, more than %d times Go's %d KB",
			portMem,
			perfMemoryFactor,
			goMem,
		)
	}
}

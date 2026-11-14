package porttest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// perfRuns is how many times TestResPerf runs each compiler, keeping the
// fastest run's time and the largest peak memory.
const perfRuns = 3

// perfTimeFactor and perfMemoryFactor bound the port against the Go
// compiler on TestResPerf's program: the port may take at most this many
// times Go's time, plus perfTimeSlack, and this many times its peak
// memory. Measured at M5, the port took a quarter of Go's time and a
// fifth of its memory, so either bound tripping means the port has lost
// that margin many times over.
const (
	perfTimeFactor   = 2
	perfMemoryFactor = 2
	perfTimeSlack    = 100 * time.Millisecond
)

// TestResPerf is the performance tripwire for --res: it runs both
// compilers, perfRuns times each, on the largest program the corpus has,
// the port's own driver, which imports the whole port and most of std,
// and fails when the port is slower or larger than perfTimeFactor and
// perfMemoryFactor allow, logging both compilers' fastest time and peak
// memory. It needs the port's source tree, and skips without one. It
// runs only as TestPort does, with TACK_PORT_TEST=1 and -count, and is
// not parallel, so the timings are of one compiler at a time.
func TestResPerf(t *testing.T) {
	ready(t)
	src, _ := portSource(os.Getenv("TACK_PORT"))
	if !dirExists(filepath.Join(src, "driver")) {
		t.Skip("no port source tree: the program is the port's driver")
	}
	args := []string{"--res", "./driver"}
	goTime, goMem := measure(t, env.goBin, env.goCache, src, args, 0)
	portTime, portMem := measure(t, env.portRun, env.portCache, src, args, 0)
	t.Logf("--res ./driver of the port: Go %v, %d KB; port %v, %d KB", goTime, goMem, portTime, portMem)
	if portTime > perfTimeFactor*goTime+perfTimeSlack {
		t.Errorf("the port took %v, more than %d times Go's %v", portTime, perfTimeFactor, goTime)
	}
	if portMem > perfMemoryFactor*goMem {
		t.Errorf("the port's peak memory was %d KB, more than %d times Go's %d KB", portMem, perfMemoryFactor, goMem)
	}
}

// measureVar, when set in the environment, makes the test binary a
// launcher for TestResPerf instead of running tests (see TestMain): it
// runs the command its value holds, each argument followed by a newline,
// with its standard output discarded and its standard error passed on,
// and writes to standard output the command's peak resident memory, in
// KB, from wait4. Linux keeps, as a process's peak, the peak of the
// process it was forked from, carried across exec, so a compiler started
// by the test binary itself, holding the whole corpus, would report the
// test binary's; started by the launcher, a fresh process, it reports
// its own.
const measureVar = "PORTTEST_MEASURE"

// launchMeasured is the launcher measureVar asks for, given the command
// cmdline, its arguments each followed by a newline. It returns the exit
// status, the command's, or 1 when it could not be run.
func launchMeasured(cmdline string) int {
	argv := strings.Split(strings.TrimSuffix(cmdline, "\n"), "\n")
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // G204: a compiler under test
	cmd.Stdout, cmd.Stderr = nil, os.Stderr
	err := cmd.Run()
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		fmt.Fprintln(os.Stderr, "porttest: no rusage")
		return 1
	}
	fmt.Println(ru.Maxrss)
	if err != nil {
		fmt.Fprintln(os.Stderr, "porttest:", err)
		return max(cmd.ProcessState.ExitCode(), 1)
	}
	return 0
}

// measure runs bin with args in dir perfRuns times, with cache as
// TACK_CACHE, each run through the launcher measureVar makes of this
// test binary, each to exit with the status code, and returns the
// fastest run's wall time,
// the launcher's start included, the same for both compilers, and the
// largest peak resident memory, in KB.
func measure(t *testing.T, bin, cache, dir string, args []string, code int) (time.Duration, int64) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// An environment variable cannot hold a 0 byte; no argument here
	// holds a newline.
	cmdline := bin + "\n" + strings.Join(args, "\n") + "\n"
	var best time.Duration
	var peak int64
	for i := range perfRuns {
		cmd := exec.Command(self) //nolint:gosec // G204: this test binary
		cmd.Dir = dir
		cmd.Env = append(compilerEnv(cache), measureVar+"="+cmdline)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		start := time.Now()
		if err := cmd.Run(); cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != code {
			t.Fatalf("running %s %v in %s: %v, want exit status %d\n%s", bin, args, dir, err, code, stderr.Bytes())
		}
		took := time.Since(start)
		if i == 0 || took < best {
			best = took
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(stdout.String()), 10, 64)
		if err != nil {
			t.Fatalf("the launcher wrote no peak memory: %q", stdout.String())
		}
		peak = max(peak, kb)
	}
	return best, peak
}

// checkPerfFuncs is how many functions TestCheckPerf's program holds.
const checkPerfFuncs = 3000

// checkPerfDecls and checkPerfFunc make TestCheckPerf's program: a
// struct owning a list, a function taking a list as mut and a view, and
// one returning the view it is given, then checkPerfFuncs functions, %d
// each one's number, whose bodies report nothing and hold what the
// ownership checks follow: an own parameter and owned locals moved into
// one another and into a struct literal, a replace, a mut argument
// beside a call returning a view, a range over a list moving a local in
// a branch that returns, a region making a list, and a defer.
const (
	checkPerfDecls = "package main\n\ntype S struct {\n    items list[int64]\n}\n\n" +
		"func keep(mut l list[int64], v []int64) {\n}\n\nfunc view(v []int64) []int64 {\n    return v\n}\n\n"
	checkPerfFunc = "func f%d(own p list[int64], q list[int64]) int64 {\n" +
		"    a := make(list[int64], c_allocator)\n    b := p\n    mut s := S{items: b}\n" +
		"    old := replace(mut s.items, a)\n    keep(mut s.items, view(q))\n" +
		"    for _, e := range q {\n        if e == 0 {\n            c := old\n" +
		"            if len(c) == 0 {\n                return 1\n            }\n            return 2\n        }\n    }\n" +
		"    region r {\n        t := make(list[int64], r)\n        if len(t) == 0 {\n        }\n    }\n" +
		"    defer {\n        if len(s.items) == 0 {\n        }\n    }\n    return len(view(old))\n}\n\n"
)

// TestCheckPerf is the performance tripwire for the type checker's
// ownership checks: it runs --check --max-errors 0 through both
// compilers, perfRuns times each, on a program of checkPerfFuncs
// functions each of whose bodies Go accepts and S6 decides, and a main
// with one error, so that --check stops before the backend, and fails
// as TestResPerf does when the port is slower or larger than
// perfTimeFactor and perfMemoryFactor allow, after checking that both
// give the same output. It runs only as TestPort does, and is not
// parallel.
func TestCheckPerf(t *testing.T) {
	ready(t)
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString(checkPerfDecls)
	for i := range checkPerfFuncs {
		fmt.Fprintf(&b, checkPerfFunc, i)
	}
	b.WriteString("func main() {\n    " + checkError + "}\n")
	if err := os.WriteFile(filepath.Join(dir, "perf.tck"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	runCaseWith(t, *st, input{name: "perf.tck", dir: dir, file: "perf.tck"}, env.portRun)
	args := []string{"--check", "--max-errors", "0", "perf.tck"}
	goTime, goMem := measure(t, env.goBin, env.goCache, dir, args, 1)
	portTime, portMem := measure(t, env.portRun, env.portCache, dir, args, 1)
	t.Logf("--check of %d owning functions: Go %v, %d KB; port %v, %d KB",
		checkPerfFuncs, goTime, goMem, portTime, portMem)
	if portTime > perfTimeFactor*goTime+perfTimeSlack {
		t.Errorf("the port took %v, more than %d times Go's %v", portTime, perfTimeFactor, goTime)
	}
	if portMem > perfMemoryFactor*goMem {
		t.Errorf("the port's peak memory was %d KB, more than %d times Go's %d KB", portMem, perfMemoryFactor, goMem)
	}
}

// genericPerfTypes, genericPerfFuncs and genericPerfCallers size
// TestGenericPerf's program: struct types each with a method an
// interface asks for, generic functions each called with every one of
// them, and the functions calling them, each passing closures to a
// scope parameter.
const (
	genericPerfTypes   = 40
	genericPerfFuncs   = 20
	genericPerfCallers = 2000
)

// genericPerfProgram returns TestGenericPerf's program, which Go accepts:
// genericPerfTypes structs, genericPerfFuncs generic functions of a type
// parameter constrained by an interface, each calling a method of it,
// ranging over the fields of a Data one and switching on it, and
// genericPerfCallers functions, each calling one of the generic
// functions with each struct type in turn, several of them with the
// type argument written, and passing closures that capture and write its
// locals to a scope parameter, so that the program needs every instance
// of every generic function, genericPerfTypes times genericPerfFuncs of
// them, and holds as many closures as calls. When failing, main ends in
// checkError, so that --check stops before the backend.
func genericPerfProgram(failing bool) string {
	var b strings.Builder
	b.WriteString("package main\n\ntype Sizer interface {\n    size() int64\n}\n\n" +
		"func each(n int64, scope f func(int64)) {\n    for i := range n {\n        f(i)\n    }\n}\n\n")
	for i := range genericPerfTypes {
		fmt.Fprintf(&b, "type S%d struct {\n    n int64\n    b bool\n}\n\n"+
			"func (s S%d) size() int64 {\n    return s.n\n}\n\n", i, i)
	}
	for i := range genericPerfFuncs {
		fmt.Fprintf(&b, "func g%d[T Sizer](own x T) int64 {\n    return x.size()\n}\n\n", i)
		fmt.Fprintf(&b, "func d%d[T Data](own v T) int64 {\n    mut named := false\n"+
			"    for f := range fields(T) {\n        if f.index == 0 {\n            named = true\n        }\n    }\n"+
			"    switch T {\n    case int64:\n        return v\n    case struct:\n        if named {\n            return 1\n        }\n"+
			"        return 0\n    default:\n        return 0\n    }\n}\n\n", i)
	}
	for i := range genericPerfCallers {
		g := i % genericPerfFuncs
		fmt.Fprintf(&b, "func h%d() int64 {\n    mut seen := false\n", i)
		for j := range genericPerfTypes {
			s := (i + j) % genericPerfTypes
			if j%4 == 0 {
				fmt.Fprintf(&b, "    a%d := g%d[S%d](S%d{n: 1, b: true})\n", j, g, s, s)
			} else {
				fmt.Fprintf(&b, "    a%d := g%d(S%d{n: 1, b: true})\n", j, g, s)
			}
			fmt.Fprintf(&b, "    each(1, func(k int64) {\n        if k == a%d {\n            seen = true\n        }\n    })\n", j)
		}
		fmt.Fprintf(&b, "    return d%d(S%d{n: 1, b: seen})\n}\n\n", g, i%genericPerfTypes)
	}
	b.WriteString("func main() {\n")
	for i := range genericPerfCallers {
		fmt.Fprintf(&b, "    if h%d() == 3 {\n        return\n    }\n", i)
	}
	if failing {
		b.WriteString("    " + checkError)
	}
	b.WriteString("}\n")
	return b.String()
}

// TestGenericPerf is the performance tripwire for the type checker's
// generics and closures: it checks that the port's --port-types of
// genericPerfProgram's program, which Go accepts, is Go's, as TestTypes
// checks an input of the corpus, in the phase "bodies", and then runs
// --check --max-errors 0 through both compilers, perfRuns times each, on
// the program with one error at the end of main, so that both run the
// type checker, the instances and all, and stop before the backend, and
// fails as TestCheckPerf does when the port is slower or larger than
// perfTimeFactor and perfMemoryFactor allow, after checking that both
// give the same output. It runs only as TestPort does, and is not
// parallel.
func TestGenericPerf(t *testing.T) {
	ready(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "valid.tck"), []byte(genericPerfProgram(false)), 0o600); err != nil {
		t.Fatal(err)
	}
	in := input{name: "valid.tck", dir: dir, file: "valid.tck"}
	if compared, _, phase := checkTypes(t, in, env.portRun, true); !compared || phase != "bodies" {
		t.Fatalf("not compared in the phase bodies: compared %v, phase %q", compared, phase)
	}
	checkPerfAgainstGo(t, dir, "perf.tck", genericPerfProgram(true),
		fmt.Sprintf("%d generic calls and closures", genericPerfCallers*genericPerfTypes))
}

// checkPerfAgainstGo writes src, a program whose --check reports an
// error, to file in dir, checks that the port's --check --max-errors 0
// output is Go's, as check-core compares its cases, and then runs it
// through both compilers, perfRuns times each, failing as TestResPerf
// does when the port is slower or larger than perfTimeFactor and
// perfMemoryFactor allow, and logging both, what naming the program.
func checkPerfAgainstGo(t *testing.T, dir, file, src, what string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	runCaseWith(t, *st, input{name: file, dir: dir, file: file}, env.portRun)
	args := []string{"--check", "--max-errors", "0", file}
	goTime, goMem := measure(t, env.goBin, env.goCache, dir, args, 1)
	portTime, portMem := measure(t, env.portRun, env.portCache, dir, args, 1)
	t.Logf("--check of %s: Go %v, %d KB; port %v, %d KB", what, goTime, goMem, portTime, portMem)
	if portTime > perfTimeFactor*goTime+perfTimeSlack {
		t.Errorf("the port took %v, more than %d times Go's %v", portTime, perfTimeFactor, goTime)
	}
	if portMem > perfMemoryFactor*goMem {
		t.Errorf("the port's peak memory was %d KB, more than %d times Go's %d KB", portMem, perfMemoryFactor, goMem)
	}
}

// closurePerfLocals and closurePerfNest size TestClosurePerf's programs:
// the locals of a function, each closure one of them is captured by or
// beside, and how deeply its nested closures nest.
const (
	closurePerfLocals = 8000
	closurePerfNest   = 300
)

// closurePerfShapes are the bodies of the function h TestClosurePerf
// checks, by name, each of closurePerfLocals locals x%d of the value of
// its parameter a, and closures passed to a scope parameter of sv:
// every local declared before any closure, each closure empty; a fact
// on each local, then a closure testing it; a fact on each mut local,
// an empty closure, and the local assigned, which ends the fact; and
// every local with its fact, then closurePerfNest closures, each the
// last's, testing the first local. Each closure is checked under the
// facts in force where it is written, so each shape is quadratic when
// what a closure is checked under is copied for it.
var closurePerfShapes = map[string]func(n int) string{
	"locals-then-closures": func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "    x%d := a\n", i)
		}
		for i := range n {
			fmt.Fprintf(&b, "    sv(func() {\n        if x%d > 0 {\n        }\n    })\n", i)
		}
		return b.String()
	},
	"facts-closures": func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "    x%d := a\n    if x%d < 0 {\n        return\n    }\n"+
				"    sv(func() {\n        if x%d > 0 {\n        }\n    })\n", i, i, i)
		}
		return b.String()
	},
	"mut-facts-closures": func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "    mut x%d := a\n    if x%d < 0 {\n        return\n    }\n"+
				"    sv(func() {\n    })\n    x%d = 1\n", i, i, i)
		}
		return b.String()
	},
	"facts-nested-closures": func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "    x%d := a\n    if x%d < 0 {\n        return\n    }\n", i, i)
		}
		b.WriteString(strings.Repeat("sv(func() {\n", closurePerfNest))
		b.WriteString("if x0 > 0 {\n}\n")
		b.WriteString(strings.Repeat("})\n", closurePerfNest))
		return b.String()
	},
}

// TestClosurePerf is the performance tripwire for checking closures
// under the facts in force where they are written: for each of
// closurePerfShapes, a program of h with that body and a main ending in
// checkError, which checkPerfAgainstGo checks and times against Go. It
// runs only as TestPort does, and is not parallel.
func TestClosurePerf(t *testing.T) {
	ready(t)
	for name, shape := range closurePerfShapes {
		t.Run(name, func(t *testing.T) {
			src := "package main\n\nfunc sv(scope f func()) {\n    f()\n}\n\n" +
				"func h(a int64) {\n" + shape(closurePerfLocals) + "}\n\nfunc main() {\n    " + checkError + "}\n"
			checkPerfAgainstGo(t, t.TempDir(), "perf.tck", src, fmt.Sprintf("%s, %d locals", name, closurePerfLocals))
		})
	}
}

// bindPerfFuncs and bindPerfSites size TestBindPerf's program: its
// functions, and the locals each binds to a call's value.
const (
	bindPerfFuncs = 1000
	bindPerfSites = 100
)

// TestBindPerf is the performance tripwire for a program reporting many
// warnings: bindPerfFuncs functions, each binding bindPerfSites locals
// to a call's value, v%d := f(%d, 2), every local but the one it
// returns unused, and a main ending in checkError, which
// checkPerfAgainstGo checks and times against Go. It runs only as
// TestPort does, and is not parallel.
func TestBindPerf(t *testing.T) {
	ready(t)
	var b strings.Builder
	b.WriteString("package main\n\nfunc f(a int64, b int64) int64 {\n    return b\n}\n\n")
	for i := range bindPerfFuncs {
		fmt.Fprintf(&b, "func h%d() int64 {\n", i)
		for j := range bindPerfSites {
			fmt.Fprintf(&b, "    v%d := f(%d, 2)\n", j, j)
		}
		b.WriteString("    return v0\n}\n\n")
	}
	b.WriteString("func main() {\n    " + checkError + "}\n")
	checkPerfAgainstGo(t, t.TempDir(), "perf.tck", b.String(),
		fmt.Sprintf("%d bindings to a call's value", bindPerfFuncs*bindPerfSites))
}

// arithPerfFuncs is how many functions the first program of
// TestArithPerf holds.
const arithPerfFuncs = 3000

// arithPerfFunc is a function of TestArithPerf's first program, %d its
// number: early exits relating an index to a length, values computed from
// it whose overflow they prove, a slice and an element they prove in
// range, a narrowing conversion, arithmetic on a mask, and steps.
const arithPerfFunc = `func f%d(i int64, n int64, s string, a [8]int64) int64 {
    if i < 0 || i >= n || n > len(s) || i > 1000 {
        return 0
    }
    j := i + 1
    k := n - i
    b := s[i]
    t := s[i:n]
    m := int32(i)
    q := (i & 7) * 3 + 2
    mut w int64 = 0
    if i < 8 {
        w = a[i]
    }
    mut c int64 = 0
    c += 1
    c++
    if j < n && k > 0 && len(t) > 0 {
        return j + int64(b) + int64(m) + q + w + c
    }
    return k
}

`

// arithPerfRelations is how many values TestArithPerf's second program
// relates to n, each then asked about three times.
const arithPerfRelations = 1500

// arithPerfRepeats is how many early exits TestArithPerf's third
// program repeats, each putting the same relation in force again, and
// how many times it then asks about it.
const arithPerfRepeats = 3000

// arithPerfArms is how many cases the switch of TestArithPerf's fourth
// program has, each giving the cases after it what its value not
// matching says.
const arithPerfArms = 8000

// TestArithPerf is the performance tripwire for the type checker's
// arithmetic and relations: it runs --check --max-errors 0 through both
// compilers, as TestCheckPerf does, on a program of arithPerfFuncs
// functions whose bodies Go accepts, each proving overflows, indexes, a
// slice and a conversion, and on one body holding arithPerfRelations
// values each related to n by an early exit, all in force while each is
// asked about again, through + 1, n - v and a narrowing conversion; on
// one body holding arithPerfRepeats early exits putting the same relation
// in force, each asked about as many times; and on a switch of
// arithPerfArms cases each returning. Each program's main has one error,
// so that --check stops before the backend. It fails as TestCheckPerf does, after checking that both give
// the same output. It runs only as TestPort does, and is not parallel.
func TestArithPerf(t *testing.T) {
	ready(t)
	var clean strings.Builder
	for i := range arithPerfFuncs {
		fmt.Fprintf(&clean, arithPerfFunc, i)
	}
	var rel strings.Builder
	rel.WriteString("func g(i int64, n int64, s string) int64 {\n    if i < 0 || n > len(s) {\n        return 0\n    }\n")
	for k := range arithPerfRelations {
		fmt.Fprintf(&rel, "    v%d := i + %d\n    if v%d >= n || v%d < 0 {\n        return 0\n    }\n", k, k%50, k, k)
	}
	for k := range arithPerfRelations {
		fmt.Fprintf(&rel, "    r%d := v%d + 1 + v%d\n    d%d := n - v%d - 1\n    c%d := int32(d%d)\n",
			k, k, (k*7)%arithPerfRelations, k, k, k, k)
	}
	rel.WriteString("    return r0 + d0\n}\n\n")
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	var dup strings.Builder
	dup.WriteString("func h(i int64, n int64, s string) int64 {\n    if i < 0 {\n        return 0\n    }\n")
	dup.WriteString(strings.Repeat("    if i >= n {\n        return 0\n    }\n", arithPerfRepeats))
	for k := range arithPerfRepeats {
		fmt.Fprintf(&dup, "    u%d := s[i]\n    d%d := n - i\n", k, k)
	}
	dup.WriteString("    return 0\n}\n\n")
	var sw strings.Builder
	sw.WriteString("func w(n int64, d int64) int64 {\n    switch d {\n")
	for k := range arithPerfArms {
		fmt.Fprintf(&sw, "    case %d:\n        return n\n", k+1)
	}
	sw.WriteString("    }\n    return n\n}\n\n")
	for _, prog := range []struct{ name, body string }{
		{"arith-clean.tck", clean.String()}, {"arith-relations.tck", rel.String()},
		{"arith-repeats.tck", dup.String()}, {"switch-arms.tck", sw.String()},
	} {
		dir := t.TempDir()
		src := "package main\n\n" + prog.body + "func main() {\n    " + checkError + "}\n"
		if err := os.WriteFile(filepath.Join(dir, prog.name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		runCaseWith(t, *st, input{name: prog.name, dir: dir, file: prog.name}, env.portRun)
		args := []string{"--check", "--max-errors", "0", prog.name}
		goTime, goMem := measure(t, env.goBin, env.goCache, dir, args, 1)
		portTime, portMem := measure(t, env.portRun, env.portCache, dir, args, 1)
		t.Logf("--check of %s: Go %v, %d KB; port %v, %d KB", prog.name, goTime, goMem, portTime, portMem)
		if portTime > perfTimeFactor*goTime+perfTimeSlack {
			t.Errorf("%s: the port took %v, more than %d times Go's %v", prog.name, portTime, perfTimeFactor, goTime)
		}
		if portMem > perfMemoryFactor*goMem {
			t.Errorf("%s: the port's peak memory was %d KB, more than %d times Go's %d KB",
				prog.name, portMem, perfMemoryFactor, goMem)
		}
	}
}

// loopPerfFuncs is how many functions the first program of TestLoopPerf
// holds, and loopPerfCalls how many contract calls, unreachables and
// appends its other programs repeat.
const (
	loopPerfFuncs = 1500
	loopPerfCalls = 2000
	loopPerfNest  = 120
)

// loopPerfFunc is a function of TestLoopPerf's first program, %d its
// number: loops writing variables declared before them, which a probe
// of the body looks for the bounds of, a binary search whose bounds a
// difference keeps, a counting loop, a range over a list, nested loops,
// a contract proven at each return and a call of a function with one,
// an append whose length an index reads, and unreachable places.
const loopPerfFunc = `func f%d(n int64, s []int64, mut d int64) int64 ensures d != 0 {
    arr := [8]int64{1, 2, 3, 4, 5, 6, 7, 8}
    mut i := 0
    mut total := 0
    for i < 8 {
        total = arr[i]
        i++
    }
    mut lo := 0
    hi := len(s)
    for lo < hi {
        if lo < 0 {
            unreachable
        }
        total = s[lo]
        lo++
    }
    for mut k := 0; k < 8; k++ {
        total = arr[k]
        mut j := k
        for j > 0 {
            j--
            total = arr[j]
        }
    }
    for _, v := range s {
        if v > 0 {
            total = v
        }
    }
    mut l := make(list[int64], c_allocator)
    err := append(mut l, n, total)
    if err.code != 0 {
        d = 1
        return 0
    }
    fix(mut d)
    if d == 0 {
        unreachable
    }
    return l[1] +%% 100 / d
}

`

// TestLoopPerf is the performance tripwire for the type checker's loops
// and contracts: it runs --check --max-errors 0 through both compilers,
// as TestArithPerf does, on a program of loopPerfFuncs functions whose
// bodies Go accepts, each probed for the bounds its loops keep (see
// loopPerfFunc); on one body holding loopPerfCalls calls of a function
// with a contract, each followed by a division its fact proves; on one
// holding loopPerfCalls early exits each followed by an unreachable the
// facts in force prove, and as many appends each followed by an index
// of its list; and on loopPerfNest loops nested in each other, each
// writing a variable declared before the outermost. Each program's main
// has one error, so that --check stops before the backend. It runs only
// as TestPort does, and is not parallel.
func TestLoopPerf(t *testing.T) {
	ready(t)
	fix := "func fix(mut d int64) ensures d != 0 {\n    d = 1\n}\n\n"
	var funcs strings.Builder
	funcs.WriteString(fix)
	for i := range loopPerfFuncs {
		fmt.Fprintf(&funcs, loopPerfFunc, i)
	}
	var calls strings.Builder
	calls.WriteString(fix + "func h(mut d int64, n int64) int64 {\n    mut t := 0\n")
	for k := range loopPerfCalls {
		fmt.Fprintf(&calls, "    fix(mut d)\n    t = n / d + %d\n", k%7)
	}
	calls.WriteString("    return t\n}\n\n")
	var dead strings.Builder
	dead.WriteString("func u(n int64, x int64) int64 {\n    mut l := make(list[int64], c_allocator)\n")
	for k := range loopPerfCalls {
		fmt.Fprintf(&dead, "    if n == %d {\n        return 0\n    }\n    if n == %d {\n        unreachable\n    }\n", k, k)
		fmt.Fprintf(&dead, "    e%d := append(mut l, %d)\n    if e%d.code != 0 {\n        return 0\n    }\n    x%d := l[0]\n",
			k, k, k, k)
	}
	dead.WriteString("    return x0\n}\n\n")
	var nest strings.Builder
	nest.WriteString("func w(n int64) int64 {\n")
	for k := range loopPerfNest {
		fmt.Fprintf(&nest, "    mut v%d := 0\n", k)
	}
	for k := range loopPerfNest {
		fmt.Fprintf(&nest, "for v%d < 3 {\nv%d++\n", k, k)
	}
	nest.WriteString(strings.Repeat("}\n", loopPerfNest))
	nest.WriteString("    return v0\n}\n\n")
	for _, prog := range []struct{ name, body string }{
		{"loop-funcs.tck", funcs.String()}, {"contract-calls.tck", calls.String()},
		{"dead-appends.tck", dead.String()}, {"loop-nest.tck", nest.String()},
	} {
		t.Run(prog.name, func(t *testing.T) {
			src := "package main\n\n" + prog.body + "func main() {\n    " + checkError + "}\n"
			checkPerfAgainstGo(t, t.TempDir(), prog.name, src, prog.name)
		})
	}
}

// chainPerfLength is how many operands each chain of TestChainPerf
// holds: long enough that work or memory growing with the square of it,
// in one compiler and not the other, shows.
const chainPerfLength = 4000

// TestChainPerf is the performance tripwire for long chains of && and
// ||, whose every operand puts the facts of those before it in force for
// itself: an if whose condition is chainPerfLength operands x != k joined
// by &&, values each ruled out, which only the proof of an unreachable
// place counts; one of as many x == k joined by ||; and an ensures
// clause of as many operands x != k, and of as many x > -k, which a
// call of its function then gives the caller. Each program's main has
// one error, so that --check stops before the backend, and it fails as
// TestArithPerf does, on time and on memory, after checking that both
// compilers give the same output. It runs only as TestPort does, and is
// not parallel.
func TestChainPerf(t *testing.T) {
	ready(t)
	join := func(form, op string) string {
		parts := make([]string, chainPerfLength)
		for k := range parts {
			parts[k] = fmt.Sprintf(form, k)
		}
		return strings.Join(parts, op)
	}
	head := "func g(x int64) {\n}\n\n"
	for _, prog := range []struct{ name, body string }{
		{"if-and.tck", head + "func f(x int64) {\n    if " + join("x != %d", " && ") + " {\n        g(x)\n    }\n}\n\n"},
		{"if-or.tck", head + "func f(x int64) {\n    if " + join("x == %d", " || ") + " {\n        g(x)\n    }\n}\n\n"},
		{"ensures-ne.tck", head + "func f(x int64) ensures " + join("x != %d", " && ") +
			" {\n    exit(1)\n}\n\nfunc c(y int64) {\n    f(y)\n    g(y)\n}\n\n"},
		{"ensures-gt.tck", head + "func f(x int64) ensures " + join("x > -%d", " && ") +
			" {\n    if x <= 0 {\n        exit(1)\n    }\n}\n\nfunc c(y int64) {\n    f(y)\n    g(100 / y)\n}\n\n"},
	} {
		t.Run(prog.name, func(t *testing.T) {
			src := "package main\n\n" + prog.body + "func main() {\n    " + checkError + "}\n"
			checkPerfAgainstGo(t, t.TempDir(), prog.name, src, fmt.Sprintf("%s, %d operands", prog.name, chainPerfLength))
		})
	}
}

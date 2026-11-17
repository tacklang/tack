package porttest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// baseCompilerTimeout is how long one run of a compiler may take before
// runCompiler kills it and fails the case, so a compiler that hangs
// fails its case rather than stall the run, times timeoutScaleVar's
// multiplier (see compilerTimeout).
const baseCompilerTimeout = 60 * time.Second

// timeoutScaleVar is the environment variable that multiplies
// baseCompilerTimeout: a whole number from 1 up, 1 when unset or empty.
// A port built with the sanitizers, several times slower, runs the
// largest deep inputs with it raised, as make port-sanitize runs it.
const timeoutScaleVar = "PORTTEST_TIMEOUT_SCALE"

// compilerTimeout is baseCompilerTimeout times $PORTTEST_TIMEOUT_SCALE,
// which setup sets before any test runs and nothing changes after.
var compilerTimeout = baseCompilerTimeout

// compare is how a stage compares the two compilers' standard outputs.
type compare int

const (
	// compareBytes compares them byte for byte.
	compareBytes compare = iota
	// compareJSON compares them as decoded JSON values, as the package doc
	// says.
	compareJSON
)

// stage is one way of running the compilers over a corpus.
type stage struct {
	// args returns the command line for in.
	args func(in input) []string
	// inputs returns the inputs the stage runs on.
	inputs func() ([]input, error)
	// classify, when not nil, returns what the instrumented Go compiler
	// covers running a case of the stage, whose blocks give the emit
	// stages the case needs (see stagesOf and the package doc, Backend
	// stages): a case the port declines fails, as in a required stage,
	// once every one of them is in required.txt. fresh runs the
	// compiler anew rather than reading what it covered before.
	classify func(in input, fresh bool) (covered, error)
	// run, when not nil, runs a case of the stage in place of
	// runCaseWith, with portBin as the port.
	run func(t *testing.T, st stage, in input, portBin string)
	// name is the stage's name, in required.txt and as TestPort's subtest.
	name string
	// stdout is how the standard outputs are compared.
	stdout compare
}

// stages are every stage, in the order TestPort runs them.
var stages = []stage{
	{
		name:   "backends",
		args:   func(input) []string { return []string{"--backends"} },
		inputs: rootOnly,
		stdout: compareBytes,
	},
	{name: "lex-single", args: dumpArgs("--lex"), inputs: singleFiles, stdout: compareBytes},
	{name: "ast-single", args: dumpArgs("--ast"), inputs: singleFiles, stdout: compareJSON},
	{name: "res-single", args: dumpArgs("--res"), inputs: singleFiles, stdout: compareJSON},
	{name: "lex-packages", args: dumpArgs("--lex"), inputs: packageInputs, stdout: compareBytes},
	{name: "ast-packages", args: dumpArgs("--ast"), inputs: packageInputs, stdout: compareJSON},
	{name: "res-packages", args: dumpArgs("--res"), inputs: packageInputs, stdout: compareJSON},
}

// stageNamed returns the stage called name, or nil when there is none.
func stageNamed(name string) *stage {
	for i := range stages {
		if stages[i].name == name {
			return &stages[i]
		}
	}
	return nil
}

// rootOnly returns one input, for a stage that runs once and takes no
// file: the module's root as its working directory.
func rootOnly() ([]input, error) {
	return []input{{name: "once", dir: env.root}}, nil
}

// dumpArgs returns a stage's args for the dump flag flag: the flag, then
// the input's file.
func dumpArgs(flag string) func(in input) []string {
	return func(in input) []string { return []string{flag, in.file} }
}

// result is how one run of a compiler ended.
type result struct {
	stdout []byte
	stderr []byte
	// code is the exit status.
	code int
}

// notImplemented matches the one line of standard error with which the
// port says it cannot do what it was asked yet.
var notImplemented = regexp.MustCompile(`\Atack: not implemented in the port: [^\n]*\n\z`)

// notImplementedCode is the exit status of a not implemented answer.
const notImplementedCode = 3

// notImplemented reports whether r is the port's answer that it cannot
// do what it was asked yet: exit status notImplementedCode and, on
// standard error, exactly the one line notImplemented matches. Standard
// output is not looked at.
func (r result) notImplemented() bool {
	return r.code == notImplementedCode && notImplemented.Match(r.stderr)
}

// TestPort runs every stage over its inputs, comparing the port with the
// Go compiler, as the package doc says, and fails a stage for each entry
// of the divergence list of it whose case it does not have.
func TestPort(t *testing.T) {
	ready(t)
	for _, st := range stages {
		t.Run(st.name, func(t *testing.T) {
			ins, err := st.inputs()
			if err != nil {
				t.Fatal(err)
			}
			if len(ins) == 0 {
				t.Fatal("the stage has no inputs")
			}
			checkDivergencesListed(t, st.name, ins)
			for _, in := range asSuite(ins, st.name) {
				t.Run(in.name, func(t *testing.T) {
					t.Parallel()
					if st.run != nil {
						st.run(t, st, in, env.portRun)
						return
					}
					runCase(t, st, in)
				})
			}
		})
	}
}

// runCase runs the port and then the Go compiler on in, the stage st's
// way, and reports how they differ. A not implemented answer from the
// port skips, before the Go compiler runs, unless st is required. The
// port runs with its stack held to deepStack KB, as every run of it
// does, so a port that needs more for an input of the corpus fails.
func runCase(t *testing.T, st stage, in input) {
	runCaseWith(t, st, in, env.portRun)
}

// runCaseWith is runCase with portBin run as the port, a wrapper around
// it that changes how it runs, as TestDeepNesting's does.
func runCaseWith(t *testing.T, st stage, in input, portBin string) {
	args := st.args(in)
	port := runCompiler(t, portBin, env.portCache, in, args)
	if port.notImplemented() {
		declined(t, st, in, port)
	}
	want := runCompiler(t, env.goBin, env.goCache, in, args)
	compareResults(t, st, in, args, want, port)
}

// declined skips t, the case in of the stage st, which the port answered
// port, not implemented, or fails it when declineVerdict says it must,
// or when the divergence list holds the case, which is never a skip.
func declined(t *testing.T, st stage, in input, port result) {
	t.Helper()
	msg := strings.TrimSuffix(string(port.stderr), "\n")
	if d, listed := divergenceFor(t, st, in); listed {
		t.Fatalf("%s, but the port says %q", divergedDecline(d, true), msg)
	}
	why, err := declineVerdict(st, in, env.required, func() (emitSet, error) {
		return classifyCase(st, in, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if why != "" {
		t.Fatalf("%s, but the port says %q", why, msg)
	}
	t.Skip(msg)
}

// declineVerdict returns why a case in of the stage st that the port
// declines must fail, with required the names required.txt holds, or ""
// when it skips: it fails when st is required, or when st classifies its
// cases and every emit stage in needs, which needs gives, is, but for an
// input that may decline. Every case needs E0, so while E0 is not
// required no case of a stage that classifies them is, and needs is not
// asked.
func declineVerdict(
	st stage, in input, required map[string]bool, needs func() (emitSet, error),
) (string, error) {
	switch {
	case in.declines:
		return "", nil
	case required[st.name]:
		return "stage " + st.name + " is required", nil
	case st.classify == nil || !required[emitStageNames[emitE0]]:
		return "", nil
	}
	set, err := needs()
	if err != nil {
		return "", err
	}
	if set.required(required) {
		return "every emit stage the case needs, " + set.String() + ", is required", nil
	}
	return "", nil
}

// compareResults reports how port, the port's run on in with args,
// differs from want, the Go compiler's, as the stage st compares them;
// or, for a case the divergence list holds, from the output the list
// records, failing too when Go's no longer differs from it (see
// compareDivergence).
func compareResults(t *testing.T, st stage, in input, args []string, want, port result) {
	t.Helper()
	if d, listed := divergenceFor(t, st, in); listed {
		compareDivergence(t, st, in, args, d, want, port)
		return
	}
	if port.code != want.code {
		t.Errorf("exit status: port %d, Go %d", port.code, want.code)
	}
	if d := firstDiff(want.stderr, port.stderr); d != "" {
		t.Errorf("standard error differs: %s", d)
	}
	if d := stdoutDiff(st.stdout, want.stdout, port.stdout); d != "" {
		t.Errorf("standard output differs: %s", d)
	}
	if t.Failed() {
		t.Logf("command: (cd %s && tack %s)", in.dir, strings.Join(args, " "))
	}
}

// runCompiler runs bin with args in in's directory, in the compilers'
// environment with cache as TACK_CACHE and in's own variables after it,
// and returns how it ended. It fails t when bin
// cannot be run, is stopped by a signal, which no answer of a compiler
// is, or runs for longer than compilerTimeout, when it is killed with
// every process it started, or ends leaving one running (see
// runInGroup), each with its standard error.
func runCompiler(t *testing.T, bin, cache string, in input, args []string) result {
	dir := in.dir
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(compilerEnv(cache), in.env...)
	measured := ""
	if measureLog != "" {
		cmd, measured = measuredCommand(ctx, t, bin, args)
		cmd.Dir = dir
		cmd.Env = append(append(compilerEnv(cache), in.env...),
			measureVar+"="+bin+"\n"+strings.Join(args, "\n")+"\n", measureToVar+"="+measured)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	outlived, err := runInGroup(cmd)
	if measured != "" && ctx.Err() == nil && cmd.ProcessState != nil {
		logMeasured(t, measured, bin, dir, args, in.env, cmd.ProcessState.ExitCode())
	}
	running := fmt.Sprintf("running %s %s in %s", bin, strings.Join(args, " "), dir)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), running, cmd, err, outlived,
		compilerTimeout, stderr.Bytes())
	return result{stdout: stdout.Bytes(), stderr: stderr.Bytes(), code: cmd.ProcessState.ExitCode()}
}

// measureLogVar is the environment variable naming the file every run
// of runCompiler is logged to, measured, when it is set and not empty:
// each run goes through the launcher measureToVar makes of the test
// binary, whose child, a fresh process, reports its own peak memory, and
// is appended to the file as one JSON object a line: the test, the
// binary, which compiler it is, go or port, the working directory, the arguments, the variables the input
// adds, the exit status, the wall time in nanoseconds and the peak
// resident memory in KB. A launcher killed at compilerTimeout logs
// nothing. The whole-corpus budget sweep runs TestPort with it set and
// compares each run of the port with the run of the Go compiler of the
// same test, directory and arguments (see the package doc,
// Performance). A command line can hold no newline when measured.
const measureLogVar = "PORTTEST_MEASURE_LOG"

// measureLog is $PORTTEST_MEASURE_LOG, read once.
var measureLog = os.Getenv(measureLogVar)

// measureLogMu serializes the appends to measureLog of the parallel
// cases.
var measureLogMu sync.Mutex

// measuredCommand returns the command that runs bin with args through
// the launcher, this test binary, and the file, in the test's temporary
// directory, the launcher writes its measurement to. It fails the test
// when an argument holds a newline, which the launcher's command line
// cannot carry.
func measuredCommand(
	ctx context.Context,
	t *testing.T,
	bin string,
	args []string,
) (*exec.Cmd, string) {
	t.Helper()
	for _, a := range append([]string{bin}, args...) {
		if strings.Contains(a, "\n") {
			t.Fatalf("%s set, and the argument %q holds a newline", measureLogVar, a)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "measure")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return exec.CommandContext(ctx, self), f.Name() //nolint:gosec // G204: this test binary
}

// measuredRun is one line of the measure log (see measureLogVar).
type measuredRun struct {
	Test   string   `json:"test"`
	Bin    string   `json:"bin"`
	Comp   string   `json:"compiler"`
	Dir    string   `json:"dir"`
	Args   []string `json:"args"`
	Env    []string `json:"env,omitempty"`
	Code   int      `json:"code"`
	WallNS int64    `json:"wall_ns"`
	RSSKB  int64    `json:"rss_kb"`
}

// logMeasured appends the run of bin with args in dir, with the input's
// variables extra, which exited with code, to the measure log, from the
// launcher's measurement in the file measured. It fails the test when
// the measurement or the log cannot be read or written.
func logMeasured(t *testing.T, measured, bin, dir string, args, extra []string, code int) {
	t.Helper()
	data, err := os.ReadFile(measured) //nolint:gosec // G304: the launcher's file
	if err != nil {
		t.Fatalf("reading the measurement: %v", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("the launcher wrote no measurement: %q", data)
	}
	wall, werr := strconv.ParseInt(fields[0], 10, 64)
	rss, rerr := strconv.ParseInt(fields[1], 10, 64)
	if werr != nil || rerr != nil {
		t.Fatalf("the launcher wrote a malformed measurement: %q", data)
	}
	comp := "port"
	if bin == env.goBin {
		comp = "go"
	}
	line, err := json.Marshal(measuredRun{
		Test: t.Name(), Bin: filepath.Base(bin), Comp: comp, Dir: dir, Args: args, Env: extra,
		Code: code, WallNS: wall, RSSKB: rss,
	})
	if err != nil {
		t.Fatal(err)
	}
	measureLogMu.Lock()
	defer measureLogMu.Unlock()
	//nolint:gosec // G304: the log the variable names
	f, err := os.OpenFile(fromRoot(measureLog), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening the measure log: %v", err)
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("writing the measure log: %v", err)
	}
}

// stdoutDiff returns how got, the port's standard output, differs from
// want, the Go compiler's, compared the way c says, or "" when they
// agree.
func stdoutDiff(c compare, want, got []byte) string {
	if c == compareBytes {
		return firstDiff(want, got)
	}
	if !utf8.Valid(want) || !utf8.Valid(got) {
		// Decoding would turn each invalid byte into U+FFFD, making
		// different bytes equal: compare the bytes instead.
		return firstDiff(want, got)
	}
	wantValue, err := decodeJSON(want)
	if err != nil {
		// The Go compiler wrote no JSON, as it does not when the program
		// has errors: compare what it wrote instead.
		return firstDiff(want, got)
	}
	gotValue, err := decodeJSON(got)
	if err != nil {
		return fmt.Sprintf("the Go compiler wrote JSON and the port did not: %v", err)
	}
	if reflect.DeepEqual(wantValue, gotValue) {
		return ""
	}
	return firstDiff(indent(wantValue), indent(gotValue))
}

// decodeJSON decodes data, which must hold exactly one JSON value and
// nothing after it but white space, keeping numbers as their text.
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decoding JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decoding JSON: more after the value")
	}
	return v, nil
}

// indent returns v written as indented JSON, an object's keys sorted, so
// that two values can be compared line by line.
func indent(v any) []byte {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// v came from decodeJSON, which only makes values Marshal takes.
		return []byte(fmt.Sprintf("cannot write the value: %v", err))
	}
	return out
}

// firstDiff returns "" when want and got are equal, and otherwise the
// number of the first line in which they differ, with that line of each,
// quoted so that invisible bytes show.
func firstDiff(want, got []byte) string {
	if bytes.Equal(want, got) {
		return ""
	}
	wantLines := bytes.SplitAfter(want, []byte("\n"))
	gotLines := bytes.SplitAfter(got, []byte("\n"))
	for i := 0; ; i++ {
		w, g := lineAt(wantLines, i), lineAt(gotLines, i)
		if w != g {
			return fmt.Sprintf("line %d:\n  Go:   %s\n  port: %s", i+1, w, g)
		}
	}
}

// lineAt returns line i of lines quoted, or "(none)" past their end.
func lineAt(lines [][]byte, i int) string {
	if i >= len(lines) || len(lines[i]) == 0 {
		return "(none)"
	}
	return fmt.Sprintf("%q", lines[i])
}

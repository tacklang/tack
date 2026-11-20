package porttest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// specStage is the stage of the port's checker of the language
// specification's examples: its one case builds the port's tools/spec
// with the port and runs it over the port's docs/spec, with the port as
// the compiler it checks, and over the checker's own fixtures (see
// runSpec).
const specStage = "spec"

// specTimeouts is how many of compilerTimeout one run of the port's
// tools/spec may take: it runs the compiler on each of the spec's 179
// examples that are not fragments, and builds and runs the 100 run
// examples, which took the port 10 seconds in all, measured; a program
// that never ends takes 10 seconds more, as one of the fail fixture's
// does.
const specTimeouts = 5

// specFixtures are tools/spec's fixtures in the port's source tree, each
// a directory below tools/spec/testdata holding spec/, a spec, and want,
// what the checker must write on it, with the tree's path and a slash
// left out, and then a line exit <status>, as the port's make
// test-spec-tool compares them: fail, run with the port, fails every way
// the checker reports with a real compiler beside examples that pass,
// and fake, run with the stand-in compiler fake/tack, the ways only a
// compiler that misbehaves can give.
var specFixtures = []string{"fail", "fake"}

// specGroupPoll is how often specGroups reads the launcher's group log.
const specGroupPoll = 10 * time.Millisecond

// init adds spec to stages.
func init() {
	stages = append(stages, stage{
		name:   specStage,
		args:   func(input) []string { return []string{"--out", "spec", "./tools/spec"} },
		inputs: specInputs,
		stdout: compareBytes,
		run:    runSpec,
	})
}

// specInputs returns spec's one input: the port's source tree as the
// working directory.
func specInputs() ([]input, error) {
	return []input{{name: "spec", dir: env.home, suite: specStage}}, nil
}

// runSpec builds the port's tools/spec with portBin, the port, in the
// port's source tree, and runs it over the port's docs/spec, with the
// port's source tree as TACK_HOME and portBin as the compiler it checks
// (see runSpecTool); the case fails unless the checker passes, exit
// status 0, its output shown. It then runs it over each of
// specFixtures' spec, whose output, with the port's source tree's path
// and a slash left out, and a line exit <status> after it, must be the
// fixture's want, the first line that differs shown. tools/spec checks
// the spec's index and Markdown dialect and every tack block by its
// kind, as this module's TestSpecExamples and TestSpecIndex check its
// own frozen copy, and TestPortSpec keeps the two copies the same. A run
// example is built only with clang on PATH, which the checker otherwise
// skips, as TestSpecExamples does, so clang missing from PATH fails the
// case before it runs, as the stage is required. A decline of the build
// skips, or fails the case once the stage is required, as any case's
// does. The checker needs no Go: it is the check of the spec that runs
// once Go is gone, which this keeps working. A port that is the Go
// compiler, byte for byte, runs the case too: the checker uses only
// --check and --out.
func runSpec(t *testing.T, st stage, in input, portBin string) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("the spec stage builds the spec's run examples with clang: %v", err)
	}
	tool := filepath.Join(t.TempDir(), "spec")
	built := runCompiler(t, portBin, env.portCache, in, []string{"--out", tool, "./tools/spec"})
	if built.notImplemented() {
		declined(t, st, in, built)
	}
	if built.code != 0 {
		t.Fatalf("building tools/spec with the port: exit status %d\n%s", built.code, built.stderr)
	}
	out, code := runSpecTool(t, tool, portBin, filepath.Join(env.home, "docs", "spec"))
	if code != 0 {
		t.Fatalf("tools/spec exited %d:\n%s", code, out)
	}
	for _, name := range specFixtures {
		fixture := filepath.Join(env.home, "tools", "spec", "testdata", name)
		want, err := os.ReadFile(filepath.Join(fixture, "want")) //nolint:gosec // G304: the fixture's expected output
		if err != nil {
			t.Fatal(err)
		}
		compiler := portBin
		if name == "fake" {
			compiler = filepath.Join(fixture, "tack")
		}
		out, code := runSpecTool(t, tool, compiler, filepath.Join(fixture, "spec"))
		got := strings.ReplaceAll(string(out), env.home+"/", "") + fmt.Sprintf("exit %d\n", code)
		if msg := firstDiffLabeled("want", "tools/spec", want, []byte(got)); msg != "" {
			t.Fatalf("tools/spec on testdata/%s/spec differs from its want: %s\nall it wrote:\n%s", name, msg, got)
		}
	}
}

// runSpecTool runs tool, the port's tools/spec, over the spec directory
// dir with compiler as the compiler it checks and the port's source tree
// as its TACK_HOME, from that tree, in a process group of its own, with
// a TMPDIR of its own and TACK_SPEC_GROUPS a log the launcher writes the
// process group of each example's program to (see specGroups), and
// returns what it wrote on standard output and standard error, and its
// exit status. A run that does not end within specTimeouts times
// compilerTimeout, or leaves a process of its own group behind, fails
// the test, and so does an example's group that is not empty once its
// run has ended; every example's group is killed before it returns,
// however the run ended, and while it runs a signal to the harness
// kills them too, as it kills the checker's.
func runSpecTool(t *testing.T, tool, compiler, dir string) ([]byte, int) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "groups")
	watch := watchSpecGroups(logPath)
	timeout := specTimeouts * compilerTimeout
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, compiler, env.home, dir) //nolint:gosec // G204: the tool just built
	cmd.Dir = env.home
	cmd.Env = append(compilerEnv(env.portCache), "TMPDIR="+t.TempDir(), "TACK_SPEC_GROUPS="+logPath)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	outlived, err := runInGroup(cmd)
	problems := watch.stop()
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running tools/spec", cmd, err, outlived,
		timeout, out.Bytes())
	for _, p := range problems {
		t.Errorf("running tools/spec over %s: %s", dir, p)
	}
	return out.Bytes(), cmd.ProcessState.ExitCode()
}

// specGroups follows the log the launcher of tools/spec writes when
// TACK_SPEC_GROUPS names it, a line start <pgid> once an example's
// program has its process group and end <pgid> once the launcher has
// killed the group at the run's end: each group started is registered
// with the harness's groups, so a signal to the harness kills it, and
// each group that ends must be empty, within outlivedGrace, as
// reapGroup checks a group, or it outlived its run.
type specGroups struct {
	stopped  chan struct{}  // closed to stop the poll
	done     chan struct{}  // closed once the poll has stopped
	live     map[int]bool   // the groups started and not ended
	path     string         // the log
	problems []string       // what was wrong, in the order found
	checks   sync.WaitGroup // the checks of the groups that ended
	offset   int64          // how much of the log has been read
	mu       sync.Mutex     // guards problems
}

// watchSpecGroups starts following the log at path, which need not
// exist yet, every specGroupPoll.
func watchSpecGroups(path string) *specGroups {
	g := &specGroups{path: path, stopped: make(chan struct{}), done: make(chan struct{}), live: map[int]bool{}}
	go func() {
		defer close(g.done)
		for {
			select {
			case <-g.stopped:
				return
			case <-time.After(specGroupPoll):
				g.read()
			}
		}
	}()
	return g
}

// read reads the lines of the log written since it last read it, whole
// lines only, and acts on each: a start registers the group, an end
// unregisters it and checks, in a goroutine of its own, that it empties.
func (g *specGroups) read() {
	data, err := os.ReadFile(g.path)
	if err != nil {
		return // the log is written once the first program starts
	}
	if int64(len(data)) <= g.offset {
		return
	}
	fresh := data[g.offset:]
	end := bytes.LastIndexByte(fresh, '\n')
	if end < 0 {
		return
	}
	g.offset += int64(end + 1)
	sc := bufio.NewScanner(bytes.NewReader(fresh[:end+1]))
	for sc.Scan() {
		g.line(sc.Text())
	}
}

// line acts on one line of the log, line.
func (g *specGroups) line(line string) {
	what, num, _ := strings.Cut(line, " ")
	pgid, err := strconv.Atoi(num)
	if err != nil || pgid <= 1 || (what != "start" && what != "end") {
		g.problem(fmt.Sprintf("a line of the launcher's group log is not start or end and a group: %q", line))
		return
	}
	if what == "start" {
		g.live[pgid] = true
		groups.Lock()
		groups.live[pgid] = true
		groups.Unlock()
		return
	}
	delete(g.live, pgid)
	g.checks.Add(1)
	go func() {
		defer g.checks.Done()
		if reapGroup(pgid) {
			g.problem(fmt.Sprintf("a process an example started outlived its run, in process group %d, killed", pgid))
		}
		groups.Lock()
		delete(groups.live, pgid)
		groups.Unlock()
	}()
}

// problem records p.
func (g *specGroups) problem(p string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.problems = append(g.problems, p)
}

// stop stops following the log once the run has ended, reads what is
// left of it, waits for the checks of the groups that ended, and kills
// every group that started and did not end, which a launcher killed with
// the checker leaves, and returns what was wrong, a group with no end
// among it.
func (g *specGroups) stop() []string {
	close(g.stopped)
	<-g.done
	g.read()
	g.checks.Wait()
	for pgid := range g.live {
		_ = syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck // the group may be empty already
		g.problem(fmt.Sprintf("the launcher logged no end for process group %d, killed", pgid))
		groups.Lock()
		delete(groups.live, pgid)
		groups.Unlock()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.problems
}

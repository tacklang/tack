package porttest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// runRecordTimeouts is how many of compilerTimeout one record, its build
// and its run, may take.
const runRecordTimeouts = 2

// runExecVar names the variable that makes this test binary the
// launcher of a record's program (see runExec): its value is what to
// set up, as runProgram writes it.
const runExecVar = "PORTTEST_RUN_EXEC"

// runExec is the test binary run as a record's launcher, in two stages:
// spec, the value of runExecVar, is lines: fail=<path>, the file it
// writes why it could not set the run up to, result=<path>, the file the
// first stage writes how the run ended to, deadline=<nanoseconds>, how
// long the program may run, nofile=<n> to limit the open files,
// close=<fd> for each of 0 to 2 to close, keep=<fd> for each of 3 and 4
// the run gives the program, and, in the second stage alone,
// stage=exec; then the program and its arguments follow on os.Args.
// The first stage is the reaper (see runReaper), which starts the second
// as its child. The second turns core dumps off, sets the limit, closes every
// descriptor from 3 up but those it keeps, and the ones close names, and
// replaces itself with the program, with its environment without
// runExecVar: Go's os/exec can neither set a child's limits nor give it a
// closed descriptor, and the launcher does both with no shell. When it
// cannot it writes why to the fail file, where the program never writes,
// and exits 125, so the caller tells its failure from the program's own
// exit 125.
func runExec(spec string) int {
	failFile := ""
	fail := func(err error) int {
		if failFile != "" {
			_ = os.WriteFile(failFile, []byte(err.Error()+"\n"), 0o600) //nolint:gosec // G306: the run's own file
		}
		fmt.Fprintln(os.Stderr, "porttest launcher:", err)
		return 125
	}
	var closing []int
	keep := map[int]bool{}
	nofile := 0
	resultFile, execStage := "", false
	var deadline time.Duration
	for line := range strings.Lines(spec) {
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "fail":
			failFile = v
			continue
		case "result":
			resultFile = v
			continue
		case "stage":
			execStage = v == "exec"
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fail(fmt.Errorf("bad line %q", line))
		}
		switch k {
		case "nofile":
			nofile = n
		case "close":
			closing = append(closing, n)
		case "keep":
			keep[n] = true
		case "deadline":
			deadline = time.Duration(n)
		default:
			return fail(fmt.Errorf("bad line %q", line))
		}
	}
	if !execStage {
		if resultFile == "" || deadline <= 0 {
			return fail(errors.New("no result file or deadline"))
		}
		return runReaper(spec, resultFile, deadline, fail)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{}); err != nil {
		return fail(err)
	}
	if len(os.Args) < 2 {
		return fail(errors.New("no program"))
	}
	var envv []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, runExecVar+"=") {
			envv = append(envv, kv)
		}
	}
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return fail(err)
	}
	// Every descriptor from 3 up but those kept is closed: close_range
	// from the first not kept, and one at a time below it.
	first := 3
	for keep[first] {
		first++
	}
	for fd := 3; fd < first; fd++ {
		if !keep[fd] {
			_ = syscall.Close(fd) //nolint:errcheck // a descriptor already closed is what is wanted
		}
	}
	const sysCloseRange = 436 // close_range(2) on linux/amd64
	if _, _, errno := syscall.Syscall(sysCloseRange, uintptr(first), ^uintptr(0), 0); errno != 0 {
		for fd := first; uint64(fd) < min(lim.Cur, 1<<20); fd++ {
			_ = syscall.Close(fd) //nolint:errcheck // see above
		}
	}
	for _, fd := range closing {
		_ = syscall.Close(fd) //nolint:errcheck // a descriptor already closed is what is wanted
	}
	if nofile > 0 {
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: uint64(nofile), Max: uint64(nofile)}); err != nil {
			return fail(err)
		}
	}
	err := syscall.Exec(os.Args[1], os.Args[1:], envv)
	return fail(err)
}

// runReaper is runExec's first stage: it makes itself a child
// subreaper, so that every descendant of the program, one that left its
// process group or session among them, is reparented to it when its
// parent ends, and starts the second stage, which becomes the program,
// as its child in a session of its own, with descriptors 0 to 4 as it
// was given them, and closes its own. It waits for the program with
// wait4, looking every 5 ms, so that a zombie is never taken for a live
// process, against deadline: the program alive then is killed, its group
// with it, and the result is deadline; otherwise it is exit <n> or
// signal <n>, and, when a descendant was still alive as the program
// ended, a line outlived, the reaper then waiting for the descendants
// until the deadline and killing what is left. It kills what is left by
// pid, listed in /proc/self/task/<pid>/children, the one file it reads
// under /proc: the kernel reparents an orphan to a subreaper's thread
// group leader while it lives, which a Go process's main thread does to
// the end; with no such file it kills the program's group alone and
// writes no-children-file. The result goes to resultFile, a line each;
// a failure goes through fail.
func runReaper(spec, resultFile string, deadline time.Duration, fail func(error) int) int {
	const prSetChildSubreaper = 36
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		return fail(fmt.Errorf("cannot become a child subreaper: %w", errno))
	}
	if len(os.Args) < 2 {
		return fail(errors.New("no program"))
	}
	files := make([]*os.File, 5)
	for fd := range files {
		if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0); errno == 0 {
			files[fd] = os.NewFile(uintptr(fd), "")
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, runExecVar+"=") {
			env = append(env, kv)
		}
	}
	env = append(env, runExecVar+"="+spec+"stage=exec\n")
	start := time.Now()
	runtime.LockOSThread()
	p, err := os.StartProcess(os.Args[0], os.Args, &os.ProcAttr{Env: env, Files: files,
		Sys: &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}})
	if err != nil {
		return fail(fmt.Errorf("cannot start the program: %w", err))
	}
	pid := p.Pid
	for _, f := range files {
		if f != nil {
			_ = f.Close() //nolint:errcheck // the program's descriptors are its own from here
		}
	}
	var ws syscall.WaitStatus
	ended := false
wait:
	for !ended {
		got, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
		switch {
		case err != nil && !errors.Is(err, syscall.EINTR):
			return fail(fmt.Errorf("cannot wait for the program: %w", err))
		case got == pid:
			ended = true
		case time.Since(start) >= deadline:
			break wait
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	var result strings.Builder
	noChildren := false
	if !ended {
		_ = syscall.Kill(-pid, syscall.SIGKILL) //nolint:errcheck // the group may be gone
		_ = syscall.Kill(pid, syscall.SIGKILL)  //nolint:errcheck // see above
		if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
			return fail(fmt.Errorf("cannot reap the program: %w", err))
		}
		noChildren = reaperEndAll(pid)
		result.WriteString("deadline\n")
	} else {
		left, err := reaperReap()
		if err != nil {
			return fail(fmt.Errorf("cannot wait for the program's descendants: %w", err))
		}
		outlived := left
		for left && time.Since(start) < deadline {
			time.Sleep(5 * time.Millisecond)
			if left, err = reaperReap(); err != nil {
				return fail(fmt.Errorf("cannot wait for the program's descendants: %w", err))
			}
		}
		if left {
			noChildren = reaperEndAll(pid)
		}
		if ws.Signaled() {
			fmt.Fprintf(&result, "signal %d\n", int(ws.Signal()))
		} else {
			fmt.Fprintf(&result, "exit %d\n", ws.ExitStatus())
		}
		if outlived {
			result.WriteString("outlived\n")
		}
	}
	if noChildren {
		result.WriteString("no-children-file\n")
	}
	if err := os.WriteFile(resultFile, []byte(result.String()), 0o600); err != nil {
		return fail(fmt.Errorf("cannot write the result: %w", err))
	}
	return 0
}

// reaperReap reaps every child of the reaper that has ended and reports
// whether one is still alive.
func reaperReap() (bool, error) {
	for {
		var ws syscall.WaitStatus
		got, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		switch {
		case errors.Is(err, syscall.ECHILD):
			return false, nil
		case errors.Is(err, syscall.EINTR):
		case err != nil:
			return false, err
		case got == 0:
			return true, nil
		}
	}
}

// reaperEndAll kills the process group pgid and then every child of the
// reaper /proc/self/task/<pid>/children lists, reaped and listed again
// until none is left, and reports whether that file was missing, when it
// waits up to two seconds for what is left instead and leaves the rest.
func reaperEndAll(pgid int) bool {
	_ = syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck // the group may be gone
	children := fmt.Sprintf("/proc/self/task/%d/children", os.Getpid())
	giveUp := time.Now().Add(2 * time.Second)
	for {
		left, err := reaperReap()
		if err != nil || !left {
			return false
		}
		list, err := os.ReadFile(children) //nolint:gosec // G304: this process's own children
		if err != nil {
			for left && time.Now().Before(giveUp) {
				time.Sleep(5 * time.Millisecond)
				left, _ = reaperReap() //nolint:errcheck // what is left is left
			}
			return true
		}
		for _, f := range strings.Fields(string(list)) {
			if pid, err := strconv.Atoi(f); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck // it may have just ended
			}
		}
		time.Sleep(time.Millisecond)
	}
}

// runDirs are the paths of one run of a record, all its own: base, which
// must hold root alone afterwards; root, the run's private root, which
// holds src, tmp, run when the run has a directory of its own, and bin,
// as prog, and must hold exactly those afterwards, checking stopping at
// base; src, the copy it is built from, which build is in or is; btmp, the
// builds' TMPDIR, which must be empty after each; build,
// where it is built or the compiler is given its files, a copy, run,
// where it or the compiler runs, tmp, its TMPDIR, io, where its
// descriptors' files are, cache, the record's TACK_CACHE, and bin, the
// program.
type runDirs struct{ base, root, src, btmp, build, run, tmp, io, cache, bin string }

// normalize writes each $ of b as $$, so no byte of a run reads as a
// placeholder, and then replaces d's paths, their $ doubled too, by
// $RUN, $TMP, $SRC and $PROG, the longest first; build is left when it
// is run.
func (d runDirs) normalize(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("$"), []byte("$$"))
	type pair struct{ dir, name string }
	ps := []pair{{d.run, "$RUN"}, {d.tmp, "$TMP"}}
	if d.build != d.run {
		ps = append(ps, pair{d.build, "$SRC"})
	}
	if d.bin != "" {
		ps = append(ps, pair{d.bin, "$PROG"})
	}
	sort.SliceStable(ps, func(i, j int) bool { return len(ps[i].dir) > len(ps[j].dir) })
	for _, p := range ps {
		b = bytes.ReplaceAll(b, bytes.ReplaceAll([]byte(p.dir), []byte("$"), []byte("$$")), []byte(p.name))
	}
	return b
}

// expand replaces $RUN, $TMP, $SRC and $PROG in s by d's paths, and $$
// by $, left to right, as runs.tck's expanded does.
func (d runDirs) expand(s string) string {
	return strings.NewReplacer("$$", "$", "$RUN", d.run, "$TMP", d.tmp, "$SRC", d.build, "$PROG", d.bin).Replace(s)
}

// runSeeds are the warm caches of the records' builds, one per exact
// cflags, each filled by the first build with those flags and copied,
// never written again, into every later record's own cache.
var runSeeds = struct {
	sync.Mutex
	dirs map[string]string
}{dirs: map[string]string{}}

// TestRunRecords builds every run record of the port's corpus/runs with
// the reference Go compiler and runs it as the record says, at each of
// its levels, or runs the compiler, failing the record when what happens
// differs from what it says, so no record holds what Go's own compiler
// does not do. A record of the suite run-records of
// testdata/divergences.txtar, named there as <archive>#<name>, is checked
// against that entry instead (see checkDivergedRecord). The records run
// in parallel, as many at once as go test's -parallel allows, each on
// copies of its own, its program started by os/exec through this test
// binary as a launcher (see runExec), never a shell.
func TestRunRecords(t *testing.T) {
	ready(t)
	archives, err := readRunArchives(env.home)
	if err != nil {
		t.Fatal(err)
	}
	divs, err := loadedDivergences()
	if err != nil {
		t.Fatal(err)
	}
	seedRoot := t.TempDir()
	// The seeds are this run's, in seedRoot: a run of -count above 1
	// starts with none, the last run's being gone with its TempDir.
	runSeeds.Lock()
	runSeeds.dirs = map[string]string{}
	runSeeds.Unlock()
	var keys []string
	for _, archive := range runRecordArchives {
		if archive == runInvalidArchive {
			continue
		}
		for _, c := range archives[archive] {
			key := archive + "#" + c.name
			keys = append(keys, key)
			t.Run(archive+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				rec, err := parseRecord(c, len(c.files) > 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range []struct {
					name string
					into *[]byte
				}{{rec.stdoutF, &rec.stdout}, {rec.stderrF, &rec.stderr}} {
					if f.name != "" {
						b, err := os.ReadFile(filepath.Join(env.home, portCorpusDir, filepath.FromSlash(f.name))) //nolint:gosec // G304: the corpus
						if err != nil {
							t.Fatal(err)
						}
						*f.into = bytes.ReplaceAll(b, []byte("$"), []byte("$$"))
					}
				}
				var entry *divergence
				for i, d := range divs {
					if d.suite == runRecordsSuite && d.name == key {
						entry = &divs[i]
					}
				}
				if rec.portOnly && entry == nil {
					t.Fatalf("a port-only record checks what the Go compiler does not do, so it needs an entry of suite %s in %s, naming %s",
						runRecordsSuite, divergencesFile, key)
				}
				levels := rec.opts
				if len(levels) == 0 {
					levels = []string{""}
				}
				cache := recordCache(t, rec, seedRoot)
				mustFailed := false
				for i, level := range levels {
					got := runOneRecord(t, rec, level, cache)
					if i == 0 && !rec.cold && len(rec.invoke) == 0 && rec.mustFail == "" {
						fillSeed(t, rec, seedRoot, cache)
					}
					at := ""
					if level != "" {
						at = "at -O" + level + ": "
					}
					if inMustFail := archive == runMustFailArchive; inMustFail != (rec.mustFail != "") {
						t.Fatalf("a record has a must-fail line when, and only when, it is one of %s", runMustFailArchive)
					}
					if rec.mustFail != "" {
						if checkMustFail(t, at, rec, got.differences(rec)) {
							mustFailed = true
						}
						continue
					}
					if entry != nil {
						checkDivergedRecord(t, rec, got, *entry)
						continue
					}
					for _, f := range got.differences(rec) {
						t.Error(at + f.msg)
					}
				}
				if rec.mustFail != "" && !mustFailed {
					t.Errorf("must-fail %s: no run of it failed", rec.mustFail)
				}
			})
		}
	}
	// The strict-c check, once per flag set of the records with a
	// strict-libs line (see checkStrictLibs).
	var uses []strictUse
	for _, archive := range runRecordArchives {
		if archive == runInvalidArchive {
			continue
		}
		for _, c := range archives[archive] {
			rec, err := parseRecord(c, len(c.files) > 0)
			if err == nil && rec.strictLibs {
				for _, u := range strictUses(rec) {
					if !slices.Contains(uses, u) {
						uses = append(uses, u)
					}
				}
			}
		}
	}
	if len(uses) > 0 {
		t.Run("(strict-c)", func(t *testing.T) {
			header := stdHeader(t)
			var sets []string
			dirs := map[string][]string{}
			for _, u := range uses {
				if !slices.Contains(sets, u.set) {
					sets = append(sets, u.set)
				}
				if u.dir != "" && !slices.Contains(dirs[u.set], u.dir) {
					dirs[u.set] = append(dirs[u.set], u.dir)
				}
			}
			for _, set := range sets {
				t.Run(set, func(t *testing.T) {
					t.Parallel()
					checkStrictLibs(t, set, dirs[set], header)
				})
			}
		})
	}
	t.Cleanup(func() {
		for _, why := range missingDivergences(divs, runRecordsSuite, keys) {
			t.Error(why)
		}
	})
}

// checkMustFail checks the failures of one run of the must-fail record
// rec, in the order the port's runner checks them, and reports whether
// the run failed: when it did, the first failure must be of the class
// rec's must-fail line names, as the port's runner requires of the same
// record; at says at which level. One run of the record at least must
// fail, which TestRunRecords requires.
func checkMustFail(t *testing.T, at string, rec runRecord, failures []runFailure) bool {
	t.Helper()
	if len(failures) == 0 {
		return false
	}
	if failures[0].class != rec.mustFail {
		t.Errorf("%smust-fail %s: the run failed as %s: %s", at, rec.mustFail, failures[0].class, failures[0].msg)
	}
	return true
}

// recordCache returns a TACK_CACHE of rec's own: empty when rec says
// cold-cache or no build with its cflags filled a seed yet, and a copy of
// that seed otherwise.
func recordCache(t *testing.T, rec runRecord, seedRoot string) string {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "cache")
	if rec.cold {
		return cache
	}
	runSeeds.Lock()
	seed, ok := runSeeds.dirs[strings.Join(rec.cflags, " ")]
	runSeeds.Unlock()
	if ok {
		mustDo(t, os.CopyFS(cache, os.DirFS(seed)))
	}
	return cache
}

// fillSeed makes cache, rec's after its first build, the seed of its
// cflags when none is yet, a copy under seedRoot.
func fillSeed(t *testing.T, rec runRecord, seedRoot, cache string) {
	t.Helper()
	if _, err := os.Stat(cache); err != nil {
		return
	}
	key := strings.Join(rec.cflags, " ")
	runSeeds.Lock()
	defer runSeeds.Unlock()
	if _, ok := runSeeds.dirs[key]; ok {
		return
	}
	seed := filepath.Join(seedRoot, strconv.Itoa(len(runSeeds.dirs)))
	mustDo(t, os.CopyFS(seed, os.DirFS(cache)))
	runSeeds.dirs[key] = seed
}

// checkDivergedRecord checks rec, a record of the divergence d, against
// it and against got, what the Go compiler did: for each of exit, stdout
// and stderr that d's same names, the record must hold Go's; for each
// other, the record must hold d's recorded output, and Go must give
// another, or the stream is no divergence; at least one stream must
// diverge, or the entry is stale. Every other part of the run, its
// descriptors and files, must be as the record says, as for any record.
func checkDivergedRecord(t *testing.T, rec runRecord, got runOutcome, d divergence) {
	t.Helper()
	type stream struct {
		name                 string
		record, entry, goGot []byte
	}
	streams := []stream{
		{"exit", []byte(strconv.Itoa(rec.code)), []byte(strconv.Itoa(d.want.code)), []byte(strconv.Itoa(got.code))},
		{"stdout", rec.stdout, d.want.stdout, got.stdout},
		{"stderr", rec.stderr, d.want.stderr, got.stderr},
	}
	if rec.signo != 0 || got.signo != 0 || got.running || rec.running > 0 {
		t.Errorf("divergence %s: a diverged record must end by an exit, as the Go compiler's run must", d.id)
	}
	diverged := 0
	for _, s := range streams {
		if d.same[s.name] {
			if !bytes.Equal(s.record, s.goGot) {
				t.Errorf("divergence %s says %s is the same as Go's, but the record's is not:\nrecord %q\nGo     %q",
					d.id, s.name, firstK(s.record), firstK(s.goGot))
			}
			continue
		}
		if !bytes.Equal(s.record, s.entry) {
			t.Errorf("the record's %s is not divergence %s's:\nrecord %q\nentry  %q", s.name, d.id, firstK(s.record), firstK(s.entry))
		}
		if bytes.Equal(s.record, s.goGot) {
			t.Errorf("divergence %s records %s, which Go now gives as the record holds it: the entry is stale there", d.id, s.name)
		}
		diverged++
	}
	if diverged == 0 {
		t.Errorf("divergence %s names every stream the same: the entry is stale", d.id)
	}
	rest := rec
	rest.code, rest.stdout, rest.stderr = got.code, got.stdout, got.stderr
	for _, f := range got.differences(rest) {
		t.Error(f.msg)
	}
}

// runOutcome is what one record's run did, normalized: how it ended, its
// streams, what its captured descriptors got, what was left in TMPDIR
// and the files of the directory its file lines are of that are new or
// changed, or gone, and anything else that went wrong.
type runOutcome struct {
	code     int
	signo    int
	running  bool
	stdout   []byte
	stderr   []byte
	fds      map[string][]byte
	tmpLeft  []string
	changed  map[string][]byte
	gone     []string
	cwdLeft  []string
	problems []runFailure
}

// runFailure is one way a run failed: its class, one of
// runFailureClasses, and what happened.
type runFailure struct{ class, msg string }

// differences returns how o differs from what rec says, one failure
// each, in the order the port's runner checks them, so that the first
// is what it finds first: the problems of the build and of the run
// itself, what the compiler left in its empty directory, the changes to
// $SRC and beside $RUN, TMPDIR, files gone, the end, the streams and the
// files.
func (o runOutcome) differences(rec runRecord) []runFailure {
	var msgs []runFailure
	add := func(class, format string, a ...any) {
		msgs = append(msgs, runFailure{class, fmt.Sprintf(format, a...)})
	}
	late := func(f runFailure) bool { return f.class == "src-changed" || f.class == "run-root" }
	for _, f := range o.problems {
		if !late(f) {
			msgs = append(msgs, f)
		}
	}
	if len(o.cwdLeft) > 0 {
		add("cwd", "left %v in the empty directory the compiler ran in", o.cwdLeft)
	}
	for _, f := range o.problems {
		if late(f) {
			msgs = append(msgs, f)
		}
	}
	if len(o.tmpLeft) > 0 {
		add("tmpdir", "TMPDIR holds %v after the run", o.tmpLeft)
	}
	for _, p := range o.gone {
		add("files", "left no file %s, which was there before the run", p)
	}
	switch {
	case rec.running > 0 && !o.running:
		add("still-running", "ended (exit %d, signal %d), want it still running after %v", o.code, o.signo, rec.running)
	case rec.running == 0 && o.running:
		add("did-not-end", "did not end in time")
	case rec.running > 0:
	case rec.signo != 0 && o.signo != rec.signo:
		add("exit", "ended with exit %d, signal %d, want signal %d", o.code, o.signo, rec.signo)
	case rec.signo == 0 && (o.signo != 0 || o.code != rec.code):
		add("exit", "ended with exit %d, signal %d, want exit status %d", o.code, o.signo, rec.code)
	}
	for _, s := range []struct {
		class, name string
		got, want   []byte
	}{{"stdout", "standard output", o.stdout, rec.stdout}, {"stderr", "standard error", o.stderr, rec.stderr},
		{"fd3", "fd3", o.fds["fd3"], rec.fds["fd3"]}, {"fd4", "fd4", o.fds["fd4"], rec.fds["fd4"]}} {
		if !bytes.Equal(s.got, s.want) {
			add(s.class, "%s differs from the record's:\ngot  %q\nwant %q", s.name, firstK(s.got), firstK(s.want))
		}
	}
	for _, l := range rec.lacks {
		want, got := rec.fds[l.stream], o.fds[l.stream]
		switch l.stream {
		case "stdout":
			want, got = rec.stdout, o.stdout
		case "stderr":
			want, got = rec.stderr, o.stderr
		}
		// The streams are compared with each $ doubled (see normalize).
		needle := bytes.ReplaceAll(l.bytes, []byte("$"), []byte("$$"))
		if bytes.Contains(want, needle) || bytes.Contains(got, needle) {
			add("lacks", "the record's %s or the run's holds %q, which a lacks line rules out", l.stream, l.bytes)
		}
	}
	for _, c := range rec.counts {
		got := o.fds[c.stream]
		switch c.stream {
		case "stdout":
			got = o.stdout
		case "stderr":
			got = o.stderr
		}
		// The streams are compared with each $ doubled (see normalize).
		if n := bytes.Count(got, bytes.ReplaceAll(c.bytes, []byte("$"), []byte("$$"))); n != c.n {
			add("count", "the run's %s holds %q %d times, not %d", c.stream, c.bytes, n, c.n)
		}
	}
	named := append(slices.Collect(maps.Keys(rec.want)), slices.Collect(maps.Keys(rec.links))...)
	for _, p := range slices.Sorted(maps.Keys(o.changed)) {
		if strings.HasSuffix(p, "/") && slices.ContainsFunc(named, func(w string) bool { return strings.HasPrefix(w, p) }) {
			continue
		}
		if name, ok := strings.CutSuffix(p, "\x00"); ok {
			if target, ok := rec.links[name]; !ok || target != string(o.changed[p]) {
				add("files", "symbolic link %s: points at %q, want %q (named: %v)", name, o.changed[p], target, ok)
			}
			continue
		}
		if w, ok := rec.want[p]; !ok || !bytes.Equal(w, o.changed[p]) {
			add("files", "file %s: got %q, want %q (named: %v)", p, firstK(o.changed[p]), firstK(w), ok)
		}
	}
	for p := range rec.want {
		if _, ok := o.changed[p]; !ok {
			add("files", "file %s: the run left it unchanged or left none", p)
		}
	}
	for p := range rec.links {
		if _, ok := o.changed[p+"\x00"]; !ok {
			add("files", "symbolic link %s: the run left it unchanged or left none", p)
		}
	}
	return msgs
}

// filesOf returns every file below dir, by its path below it, with its
// bytes, every directory, empty ones among them, by its path and a
// slash, with none, and every symbolic link, never followed, by its path
// and a 0 byte, which no file's path holds, with its target.
func filesOf(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	mustDo(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[filepath.ToSlash(rel)+"/"] = nil
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			out[filepath.ToSlash(rel)+"\x00"] = []byte(target)
			return err
		}
		out[filepath.ToSlash(rel)], err = os.ReadFile(p) //nolint:gosec // G304: a run's own files
		return err
	}))
	return out
}

// changedFiles returns the files of after that are new or changed since
// before, and those of before that are gone.
func changedFiles(before, after map[string][]byte) (map[string][]byte, []string) {
	changed := map[string][]byte{}
	for p, data := range after {
		if old, ok := before[p]; !ok || !bytes.Equal(old, data) {
			changed[p] = data
		}
	}
	var gone []string
	for p := range before {
		if _, ok := after[p]; !ok {
			gone = append(gone, p)
		}
	}
	return changed, gone
}

// runOneRecord builds rec with the reference compiler at level, or at
// its own when level is empty, on copies of its own, and runs it, or runs
// the compiler, as rec says, with cache as TACK_CACHE, and returns what
// happened, failing t when the build itself fails.
func runOneRecord(t *testing.T, rec runRecord, level, cache string) runOutcome {
	// base holds root alone, the run's private root; outside holds what
	// is the runner's: io, the builds' TMPDIR, the clang shim and the C
	// held to strict warnings.
	base, outside := t.TempDir(), t.TempDir()
	root := filepath.Join(base, "root")
	d := runDirs{base: base, root: root, tmp: filepath.Join(root, "tmp"), io: filepath.Join(outside, "io"), cache: cache,
		btmp: filepath.Join(outside, "buildtmp")}
	src := filepath.Join(root, "src")
	d.src = src
	if len(rec.files) > 0 {
		d.build = src
		for p, data := range rec.files {
			full := filepath.Join(src, filepath.FromSlash(p))
			mustDo(t, os.MkdirAll(filepath.Dir(full), 0o755))
			mustDo(t, os.WriteFile(full, data, 0o600))
		}
	} else {
		top, _, _ := strings.Cut(strings.TrimPrefix(rec.dir, "examples/"), "/")
		from := filepath.Join(env.home, portCorpusDir, "examples", top)
		mustDo(t, os.CopyFS(filepath.Join(src, "examples", top), os.DirFS(from)))
		d.build = filepath.Join(src, filepath.FromSlash(rec.dir))
	}
	d.run = d.build
	ownRun := len(rec.invoke) == 0 || rec.emptyCwd
	if ownRun {
		d.run = filepath.Join(root, "run")
		mustDo(t, os.MkdirAll(d.run, 0o755))
	}
	mustDo(t, os.MkdirAll(d.tmp, 0o755))
	mustDo(t, os.Mkdir(d.io, 0o755))
	mustDo(t, os.Mkdir(d.btmp, 0o755))
	judged := d.run
	if len(rec.invoke) > 0 {
		judged = d.build
	}
	before := filesOf(t, judged)
	srcBefore := filesOf(t, d.src)
	idDirs := []string{base, root, d.src, d.tmp, d.run}
	idsBefore := make([]os.FileInfo, len(idDirs))
	for i, dir := range idDirs {
		fi, err := os.Lstat(dir)
		mustDo(t, err)
		idsBefore[i] = fi
	}
	ctx, cancel := context.WithTimeout(t.Context(), runRecordTimeouts*compilerTimeout)
	defer cancel()
	var o runOutcome
	if len(rec.invoke) > 0 {
		args := make([]string, len(rec.invoke))
		for i, a := range rec.invoke {
			args[i] = d.expand(a)
		}
		cmd := exec.CommandContext(ctx, env.goBin, args...) //nolint:gosec // G204: the reference compiler
		cmd.Dir = d.run
		cmd.Env = append(compilerEnv(d.cache), "TMPDIR="+d.tmp)
		for _, kv := range rec.env {
			cmd.Env = append(cmd.Env, d.expand(kv))
		}
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		o = finish(t, ctx, cmd, false)
		o.stdout, o.stderr = d.normalize(out.Bytes()), d.normalize(errOut.Bytes())
		if d.run != d.build {
			o.cwdLeft = slices.Sorted(maps.Keys(filesOf(t, d.run)))
		}
	} else {
		d.bin = filepath.Join(root, "prog")
		if f := buildRecord(t, ctx, d, outside, rec, level); f != nil {
			return runOutcome{problems: []runFailure{*f}}
		}
		o = runProgram(t, d, rec)
	}
	inside := ""
	if len(rec.invoke) > 0 {
		rel, err := filepath.Rel(d.src, d.build)
		mustDo(t, err)
		inside = filepath.ToSlash(rel) + "/"
		if rel == "." {
			inside = ""
		}
	}
	srcChanged, srcGone := changedFiles(srcBefore, filesOf(t, d.src))
	for _, p := range append(slices.Collect(maps.Keys(srcChanged)), srcGone...) {
		if len(rec.invoke) > 0 && (inside == "" || strings.HasPrefix(p, inside)) {
			continue
		}
		o.problems = append(o.problems, runFailure{"src-changed", "the run changed " + p + " in $SRC, the copy it was built from"})
	}
	// Each directory of the run must be the one it was, by device and
	// inode, not one moved away and made again.
	for i, dir := range idDirs {
		if fi, err := os.Lstat(dir); err != nil || !os.SameFile(idsBefore[i], fi) {
			o.problems = append(o.problems, runFailure{"run-root", fmt.Sprintf("the run replaced %s with another directory, or removed it", dir)})
		}
	}
	want := []string{"src", "tmp"}
	if ownRun {
		want = append(want, "run")
	}
	if len(rec.invoke) == 0 {
		want = append(want, "prog")
	}
	for _, dir := range []struct {
		path string
		want []string
	}{{root, want}, {base, []string{"root"}}} {
		entries, err := os.ReadDir(dir.path)
		mustDo(t, err)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		slices.Sort(names)
		slices.Sort(dir.want)
		if !slices.Equal(names, dir.want) {
			o.problems = append(o.problems, runFailure{"run-root", fmt.Sprintf("%s holds %v after the run, not %v", dir.path, names, dir.want)})
		}
	}
	if left, _ := os.ReadDir(d.tmp); len(left) > 0 {
		for _, e := range left {
			o.tmpLeft = append(o.tmpLeft, e.Name())
		}
	}
	changed, gone := changedFiles(before, filesOf(t, judged))
	o.changed = map[string][]byte{}
	for p, data := range changed {
		o.changed[p] = d.normalize(data)
	}
	o.gone = gone
	return o
}

// finish runs cmd, started in a process group of its own (runInGroup),
// and returns how it ended. With stopAtDeadline the end the caller wants
// is the process still running at ctx's deadline: that is so only when
// the deadline killed it, its end the SIGKILL runInGroup's cancel sends,
// and not when it ended on its own and only something it started held its
// output open past the deadline; otherwise the deadline passing fails t.
// A process it started that outlived it is a problem of the run either
// way.
func finish(t *testing.T, ctx context.Context, cmd *exec.Cmd, stopAtDeadline bool) runOutcome {
	t.Helper()
	outlived, err := runInGroup(cmd)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("running %s: %v", cmd.Path, err)
	}
	ws, _ := cmd.ProcessState.Sys().(syscall.WaitStatus)
	o := runOutcome{code: ws.ExitStatus()}
	if ws.Signaled() {
		o.signo, o.code = int(ws.Signal()), -1
	}
	if outlived {
		o.problems = append(o.problems, runFailure{"outlived", "a process the program started outlived it"})
	}
	killedAtDeadline := ctx.Err() != nil && ws.Signaled() && ws.Signal() == syscall.SIGKILL
	if killedAtDeadline {
		if !stopAtDeadline {
			t.Fatalf("%s did not end in time", cmd.Path)
		}
		o.running, o.code, o.signo = true, 0, 0
	}
	return o
}

// buildRecord builds rec's program with the reference compiler at level
// into d.bin, with a clang that adds rec's cflags but to --version, and
// leaves a mark when it compiles or links, which must be there afterwards, when it has
// them, and, unless rec opts out, checks the C the compiler's --emit
// gives with clang -std=c11 -Wall -Wextra -Werror -fsyntax-only and, for
// a record with a strict-libs line, compiles it as its Go test does (see
// strictProgramC). It returns the first of these that fails, in the order the port's runner
// checks them, as a failure of the class build, or build-tmpdir when a
// build left something in its TMPDIR, and nil when none does.
func buildRecord(t *testing.T, ctx context.Context, d runDirs, tmp string, rec runRecord, level string) *runFailure {
	t.Helper()
	target := orDefault(rec.target, "main.tck")
	var args []string
	if level != "" {
		args = append(args, "-O"+level)
	}
	args = append(args, "--out", d.bin, target)
	buildEnv := append(compilerEnv(d.cache), "TMPDIR="+d.btmp)
	mark := ""
	if len(rec.cflags) > 0 {
		clang, err := exec.LookPath("clang")
		mustDo(t, err)
		shim := filepath.Join(tmp, "clang")
		mustDo(t, os.Mkdir(shim, 0o755))
		mark = filepath.Join(shim, "ran")
		script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = --version ]; then exec '%s' \"$@\"; fi; done\nfor a in \"$@\"; do case \"$a\" in -c|-o) : > '%s';; esac; done\nexec '%s' \"$@\" '%s'\n",
			clang, mark, clang, strings.Join(rec.cflags, "' '"))
		mustDo(t, os.WriteFile(filepath.Join(shim, "clang"), []byte(script), 0o755)) //nolint:gosec // G306: a script to run
		buildEnv = append(buildEnv, "PATH="+shim+":"+os.Getenv("PATH"))
	}
	build := func(format string, a ...any) *runFailure {
		return &runFailure{"build", fmt.Sprintf(format, a...)}
	}
	for _, step := range [][]string{args, {"--emit", target}} {
		if step[0] == "--emit" && !rec.strict {
			break
		}
		cmd := exec.CommandContext(ctx, env.goBin, step...) //nolint:gosec // G204: the reference compiler
		cmd.Dir, cmd.Env = d.build, buildEnv
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if _, err := runInGroup(cmd); err != nil || errOut.Len() > 0 || (step[0] != "--emit" && out.Len() > 0) {
			return build("tack %q with the reference compiler: %v\nstdout %q\nstderr %q", step, err, firstK(out.Bytes()), errOut.Bytes())
		}
		if step[0] != "--emit" && mark != "" {
			if _, err := os.Stat(mark); err != nil {
				return build("the build ran no compile or link of clang through the record's shim, so its cflags reached none: %v", err)
			}
		}
		if left, _ := os.ReadDir(d.btmp); len(left) > 0 {
			return &runFailure{"build-tmpdir", fmt.Sprintf("tack %q left %s in its TMPDIR", step, left[0].Name())}
		}
		if step[0] != "--emit" {
			continue
		}
		c := filepath.Join(tmp, "strict.c")
		mustDo(t, os.WriteFile(c, out.Bytes(), 0o600))
		strict := exec.CommandContext(ctx, "clang", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only",
			"-I", filepath.Join(env.home, "src", "std", "include"), c)
		if msg, err := strict.CombinedOutput(); err != nil {
			return build("the generated C is not strict C: %v\n%s", err, msg)
		}
		if rec.strictLibs {
			if why := strictProgramC(t, rec, level, out.Bytes()); why != "" {
				return build("%s", why)
			}
		}
	}
	return nil
}

// orDefault returns s, or def when s is empty.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// runProgram runs d.bin, rec's program, in d.run as rec says: its
// environment PATH, TMPDIR and rec's alone, its standard input a regular
// file, as the port's runner gives it, or /dev/null, its descriptors 0 to
// 2 and those rec makes and no other, and its limits, always through this
// test binary as the launcher (see runExec), and returns how it ended,
// its streams and descriptors normalized.
func runProgram(t *testing.T, d runDirs, rec runRecord) runOutcome {
	t.Helper()
	deadline := runRecordTimeouts * compilerTimeout
	if rec.running > 0 {
		deadline = rec.running
	}
	args := []string{d.bin}
	for _, a := range rec.args {
		args = append(args, d.expand(a))
	}
	self, err := os.Executable()
	mustDo(t, err)
	cmd := exec.Command(self, args...) //nolint:gosec // G204: this binary as the launcher
	cmd.Dir = d.run
	failFile := filepath.Join(d.io, "launcher-failed")
	resultFile := filepath.Join(d.io, "result")
	var spec strings.Builder
	fmt.Fprintf(&spec, "fail=%s\nresult=%s\ndeadline=%d\n", failFile, resultFile, int64(deadline))
	if rec.nofile > 0 {
		fmt.Fprintf(&spec, "nofile=%d\n", rec.nofile)
	}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + d.tmp}
	for _, kv := range rec.env {
		cmd.Env = append(cmd.Env, d.expand(kv))
	}
	if rec.hasStdin {
		in := filepath.Join(d.io, "stdin")
		mustDo(t, os.WriteFile(in, rec.stdin, 0o600))
		f, err := os.Open(in) //nolint:gosec // G304: the run's own file
		mustDo(t, err)
		t.Cleanup(func() { f.Close() })
		cmd.Stdin = f
	}
	open := func(name string) *os.File {
		f, err := os.Create(filepath.Join(d.io, name))
		mustDo(t, err)
		t.Cleanup(func() { f.Close() })
		return f
	}
	broken := func() *os.File {
		r, w, err := os.Pipe()
		mustDo(t, err)
		mustDo(t, r.Close())
		t.Cleanup(func() { w.Close() })
		return w
	}
	outR, outW, err := os.Pipe()
	mustDo(t, err)
	errR, errW, err := os.Pipe()
	mustDo(t, err)
	t.Cleanup(func() { outR.Close(); outW.Close(); errR.Close(); errW.Close() })
	cmd.Stdout, cmd.Stderr = outW, errW
	switch rec.streams["stdout"] {
	case "closed":
		spec.WriteString("close=1\n")
	case "broken":
		cmd.Stdout = broken()
	case "file":
		cmd.Stdout = open("stdout")
	}
	switch rec.streams["stderr"] {
	case "closed":
		spec.WriteString("close=2\n")
	case "broken":
		cmd.Stderr = broken()
	case "file":
		cmd.Stderr = open("stderr")
	case "merged":
		cmd.Stderr = cmd.Stdout
	}
	cmd.ExtraFiles = make([]*os.File, 2)
	for i, fd := range []string{"fd3", "fd4"} {
		switch rec.streams[fd] {
		case "capture":
			cmd.ExtraFiles[i] = open(fd)
		case "broken":
			cmd.ExtraFiles[i] = broken()
		default:
			continue
		}
		fmt.Fprintf(&spec, "keep=%d\n", 3+i)
	}
	cmd.Env = append(cmd.Env, runExecVar+"="+spec.String())
	out, errOut := watchProgram(t, cmd, deadline, outR, errR, outW, errW)
	if why, err := os.ReadFile(failFile); err == nil { //nolint:gosec // G304: the run's own file
		t.Fatalf("the launcher could not set the run up: %s", why)
	}
	o := readReaperResult(t, resultFile)
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(d.io, name))
		mustDo(t, err)
		return b
	}
	o.stdout, o.stderr = out, errOut
	if rec.streams["stdout"] == "file" {
		if len(out) > 0 {
			o.problems = append(o.problems, runFailure{"stdout", fmt.Sprintf("standard output is a file, yet the pipe got %q", firstK(out))})
		}
		o.stdout = read("stdout")
	}
	if rec.streams["stderr"] == "file" {
		o.stderr = read("stderr")
	}
	o.stdout, o.stderr = d.normalize(o.stdout), d.normalize(o.stderr)
	o.fds = map[string][]byte{}
	for _, fd := range []string{"fd3", "fd4"} {
		if rec.streams[fd] == "capture" {
			o.fds[fd] = d.normalize(read(fd))
		}
	}
	return o
}

// watchProgram starts cmd, the launcher of a record's program, in a
// process group of its own, its standard output and error the write ends
// outW and errW, read through outR and errR, and returns what the two
// pipes got. The launcher's first stage, the reaper, judges how the
// program ended and writes it down (see runReaper); this only bounds the
// launcher, a minute past deadline, failing t when it does not end by
// then, its group killed.
func watchProgram(t *testing.T, cmd *exec.Cmd, deadline time.Duration, outR, errR, outW, errW *os.File) ([]byte, []byte) {
	t.Helper()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", cmd.Path, err)
	}
	pgid := cmd.Process.Pid
	groups.Lock()
	groups.live[pgid] = true
	groups.Unlock()
	defer func() {
		groups.Lock()
		delete(groups.live, pgid)
		groups.Unlock()
	}()
	mustDo(t, outW.Close())
	mustDo(t, errW.Close())
	read := func(r *os.File) <-chan []byte {
		ch := make(chan []byte, 1)
		go func() {
			b, _ := io.ReadAll(r) //nolint:errcheck // a pipe's read ends at its writers' end
			ch <- b
		}()
		return ch
	}
	outCh, errCh := read(outR), read(errR)
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	bound := time.NewTimer(deadline + time.Minute)
	defer bound.Stop()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("the launcher failed: %v", err)
		}
	case <-bound.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck // the group may have just ended
		<-ended
		t.Fatalf("the launcher did not end a minute past the run's deadline")
	}
	return <-outCh, <-errCh
}

// readReaperResult reads the reaper's result file (see runReaper) into
// how the run ended: its exit status or signal, running when it was
// alive at the deadline, and the problem outlived when a descendant was
// alive when the program ended; a result that does not read fails t, as
// the launcher's failure.
func readReaperResult(t *testing.T, file string) runOutcome {
	t.Helper()
	b, err := os.ReadFile(file) //nolint:gosec // G304: the run's own file
	if err != nil {
		t.Fatalf("the launcher wrote no result: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	var o runOutcome
	head, n, _ := strings.Cut(lines[0], " ")
	num, numErr := strconv.Atoi(n)
	switch {
	case lines[0] == "deadline":
		o.running = true
	case head == "exit" && numErr == nil && num >= 0 && num <= 255:
		o.code = num
	case head == "signal" && numErr == nil && num > 0 && num <= 255:
		o.signo, o.code = num, -1
	default:
		t.Fatalf("the launcher's result does not read: %q", b)
	}
	outlived, noChildren := false, false
	for _, l := range lines[1:] {
		switch {
		case l == "outlived" && !outlived && !o.running:
			outlived = true
		case l == "no-children-file" && !noChildren:
			noChildren = true
		default:
			t.Fatalf("the launcher's result does not read: %q", b)
		}
	}
	if outlived {
		msg := "a process the program started was alive when the program ended, and outlived it"
		if noChildren {
			msg += " (the kernel has no /proc/self/task/<tid>/children to find what was left by, so only the program's process group was killed)"
		}
		o.problems = append(o.problems, runFailure{"outlived", msg})
	} else if noChildren {
		t.Fatalf("what was left at the deadline could not be found to kill: the kernel has no /proc/self/task/<tid>/children")
	}
	return o
}

// firstK returns at most the first 1024 bytes of b, for a failure's
// message.
func firstK(b []byte) []byte { return b[:min(len(b), 1024)] }

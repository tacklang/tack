package porttest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// coverPkgs are the packages the instrumented Go compiler counts the
// blocks of: the backend's and the type table's, which the
// classification stages, and the driver's, ./tack, which it does not:
// built with -coverpkg naming only packages other than the main one, the
// Go toolchain of go1.27 writes no coverage data at all when the program
// exits.
const coverPkgs = "./tack,./codegen/...,./types"

// modulePath is this module's path, which every block of the coverage
// text names its file under.
const modulePath = "github.com/tacklang/tack/"

// coverFormat is the version of what the coverage cache holds, part of
// every key, so that a change to what is cached, or how it is keyed,
// never reads an entry written before it.
const coverFormat = "porttest-cover-1"

// coverCacheVar is the environment variable naming the directory the
// coverage results are cached in; unset or empty, it is
// tack-porttest/cover in os.UserCacheDir.
const coverCacheVar = "PORTTEST_COVER_CACHE"

// coverTimeout is how long one run of the instrumented Go compiler may
// take before it is killed and the classification fails: the counters
// slow it down, so it gets more than baseCompilerTimeout. It is the Go
// compiler's, which no sanitizer slows, so $PORTTEST_TIMEOUT_SCALE
// leaves it as it is.
const coverTimeout = 3 * baseCompilerTimeout

// covered is what one run of the instrumented compiler covered: for each
// source file of coverDirs, by its path from the module's root, the
// start, line.column, of every block it ran at least once, sorted.
type covered map[string][]string

// cover is the instrumented Go compiler and what the classification
// needs once per run, which coverSetup fills the first time it is asked
// for.
var cover struct {
	// staged are the backend's sources, staged (see stagedSources).
	staged map[string]*stagedFile
	// baseline are the blocks each checkpoint's baseline programs
	// cover (see baselinePrograms), by checkpoint, file and start, and
	// idle those a run that lowers nothing covers, the package
	// variables' initializers, run as every process starts.
	baseline [emitStages]map[string]map[string]bool
	idle     map[string]map[string]bool
	// flights dedups runs of one key in progress at once.
	flights map[string]*flight
	err     error
	// bin is the instrumented compiler, covdata go tool covdata's binary.
	bin, covdata string
	// binHash is bin's SHA-256, which keys the cache.
	binHash string
	// cacheDir is where results are cached, tackCache the TACK_CACHE the
	// instrumented compiler runs with, and scratch where its runs write
	// their counters.
	cacheDir, tackCache, scratch string
	// stdHash is the fingerprint of the repository's std.
	stdHash string
	mu      sync.Mutex
	once    sync.Once
}

// flight is a run of the instrumented compiler for one key that others
// may wait for.
type flight struct {
	done chan struct{}
	cov  covered
	err  error
}

// coverSetup builds the instrumented compiler and works out what every
// classification needs, the first time it is called, and returns why it
// could not.
func coverSetup() error {
	cover.once.Do(func() { cover.err = setupCover() })
	return cover.err
}

// setupCover is coverSetup's work, done once.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func setupCover() error {
	if env.tmp == "" {
		return errors.New(
			"the classification runs only as TestPort does, with " + gateVar + "=" + gateOn,
		)
	}
	dir := filepath.Join(env.tmp, "cover")
	cover.bin = filepath.Join(dir, "tack-cover")
	cover.tackCache = filepath.Join(dir, "cache")
	cover.scratch = filepath.Join(dir, "runs")
	if err := os.MkdirAll(cover.scratch, 0o755); err != nil {
		return fmt.Errorf("making the coverage's scratch directory: %w", err)
	}
	build := exec.Command("go", "build", "-cover", "-covermode=set", "-coverpkg="+coverPkgs,
		"-o", cover.bin, "./tack")
	build.Dir = env.root
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("building the instrumented Go compiler: %w\n%s", err, out)
	}
	tool, err := exec.Command("go", "tool", "-n", "covdata").Output()
	if err != nil {
		return fmt.Errorf("finding go tool covdata: %w", err)
	}
	cover.covdata = strings.TrimSpace(string(tool))
	data, err := os.ReadFile(cover.bin)
	if err != nil {
		return fmt.Errorf("reading the instrumented Go compiler: %w", err)
	}
	sum := sha256.Sum256(data)
	cover.binHash = hex.EncodeToString(sum[:])
	if cover.cacheDir = os.Getenv(coverCacheVar); cover.cacheDir == "" {
		base, cacheErr := os.UserCacheDir()
		if cacheErr != nil {
			return fmt.Errorf("finding the coverage cache: %w (set %s)", cacheErr, coverCacheVar)
		}
		cover.cacheDir = filepath.Join(base, "tack-porttest", "cover")
	}
	cover.cacheDir = filepath.Join(fromRoot(cover.cacheDir), cover.binHash[:16])
	if err = os.MkdirAll(cover.cacheDir, 0o755); err != nil {
		return fmt.Errorf("making the coverage cache: %w", err)
	}
	if cover.staged, err = stagedSources(env.root); err != nil {
		return err
	}
	if cover.stdHash, err = treeHash(filepath.Join(env.root, "src", "std")); err != nil {
		return err
	}
	cover.flights = map[string]*flight{}
	return setupBaseline(filepath.Join(dir, "baseline"))
}

// caseCovered returns what the instrumented Go compiler covers running
// in with args. fresh runs the compiler whatever the cache holds and
// leaves the cache alone; otherwise a result is read from the cache when
// it is there and written to it when it is not, keyed on the
// instrumented binary, the command line and in's bytes (see coverKey).
func caseCovered(in input, args []string, fresh bool) (covered, error) {
	if err := coverSetup(); err != nil {
		return nil, err
	}
	cov, err := coveredBy(in, args, fresh)
	if err != nil {
		return nil, fmt.Errorf("classifying %s: %w", in.name, err)
	}
	return cov, nil
}

// classifyCase returns the emit stages in, a case of the stage st,
// needs: those of the blocks st.classify finds it covers (see stagesOf).
func classifyCase(st stage, in input, fresh bool) (emitSet, error) {
	cov, err := st.classify(in, fresh)
	if err != nil {
		return 0, err
	}
	return stagesOf(cov, emitH)
}

// coveredBy returns what the instrumented compiler covers running in
// with args, from the cache unless fresh or in.uncached, with one run at a time
// for each key.
func coveredBy(in input, args []string, fresh bool) (covered, error) {
	if fresh || in.uncached {
		cov, _, err := runCovered(in, args)
		return cov, err
	}
	key, err := coverKey(in, args)
	if err != nil {
		return nil, err
	}
	cover.mu.Lock()
	if f, ok := cover.flights[key]; ok {
		cover.mu.Unlock()
		<-f.done
		return f.cov, f.err
	}
	f := &flight{done: make(chan struct{})}
	cover.flights[key] = f
	cover.mu.Unlock()
	defer close(f.done)
	path := filepath.Join(cover.cacheDir, key[:2], key+".json")
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: the cache's own file
		if f.err = json.Unmarshal(data, &f.cov); f.err != nil {
			f.err = fmt.Errorf("reading the cached coverage %s: %w", path, f.err)
		}
		return f.cov, f.err
	}
	if f.cov, _, f.err = runCovered(in, args); f.err != nil {
		return nil, f.err
	}
	f.err = writeCached(path, f.cov)
	return f.cov, f.err
}

// writeCached writes cov to path, through a temporary file renamed into
// place, so that a run killed half way leaves no entry, and two runs
// writing one key write the same bytes.
func writeCached(path string, cov covered) error {
	data, err := json.Marshal(cov)
	if err != nil {
		return fmt.Errorf("caching the coverage: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("caching the coverage: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "tmp-*")
	if err != nil {
		return fmt.Errorf("caching the coverage: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	if err = tmp.Close(); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("caching the coverage: %w", err)
	}
	return nil
}

// runCovered runs the instrumented compiler on in with args, as the Go
// compiler runs, but with a TACK_CACHE of its own, and returns what it
// covered and its exit status. A run that is stopped by a signal, runs
// past coverTimeout or writes no coverage data is an error.
func runCovered(in input, args []string) (covered, int, error) {
	counters, err := os.MkdirTemp(cover.scratch, "run-")
	if err != nil {
		return nil, 0, fmt.Errorf("making a coverage run's directory: %w", err)
	}
	defer os.RemoveAll(counters) //nolint:errcheck // a scratch directory of the run
	ctx, cancel := context.WithTimeout(context.Background(), coverTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		cover.bin,
		args...) //nolint:gosec // G204: the compiler under test
	cmd.Dir = in.dir
	cmd.Env = append(append(compilerEnv(cover.tackCache), in.env...), "GOCOVERDIR="+counters)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = nil, &stderr
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, 0, fmt.Errorf("the instrumented compiler, %v in %s, was killed after %v",
			args, in.dir, coverTimeout)
	case err == nil:
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
	default:
		return nil, 0, fmt.Errorf("running the instrumented compiler, %v in %s: %w\n%s",
			args, in.dir, err, stderr.Bytes())
	}
	text := filepath.Join(counters, "cover.txt")
	conv := exec.Command(
		cover.covdata,
		"textfmt",
		"-i="+counters,
		"-o="+text,
	) //nolint:gosec // G204: go's tool
	if out, err := conv.CombinedOutput(); err != nil {
		return nil, 0, fmt.Errorf("go tool covdata textfmt: %w\n%s", err, out)
	}
	cov, parseErr := parseCoverText(text)
	if parseErr != nil {
		return nil, 0, parseErr
	}
	return cov, cmd.ProcessState.ExitCode(), nil
}

// parseCoverText reads the coverage text at path, go tool covdata
// textfmt's, and returns the blocks it counts as run of the files of
// coverDirs. A file that holds no block, or a block of a file outside
// this module, is an error: either means the run wrote no data, or not
// the instrumented compiler's.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func parseCoverText(path string) (covered, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the run's own file
	if err != nil {
		return nil, fmt.Errorf("reading the coverage: %w", err)
	}
	defer f.Close() //nolint:errcheck // read only
	cov := covered{}
	blocks := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		// github.com/tacklang/tack/codegen/c/gen.go:257.66,261.3 2 1
		rest, ok := strings.CutPrefix(line, modulePath)
		if !ok {
			return nil, fmt.Errorf("a block of another module in the coverage: %q", line)
		}
		file, pos, ok := strings.Cut(rest, ":")
		fields := strings.Fields(pos)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("a malformed line of the coverage: %q", line)
		}
		blocks++
		start, _, _ := strings.Cut(fields[0], ",")
		if fields[2] == "0" || !slices.Contains(coverDirs, filepath.ToSlash(filepath.Dir(file))) {
			continue
		}
		cov[file] = append(cov[file], start)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading the coverage: %w", err)
	}
	if blocks == 0 {
		return nil, errors.New("the instrumented compiler wrote no coverage")
	}
	for file := range cov {
		slices.Sort(cov[file])
		cov[file] = slices.Compact(cov[file])
	}
	return cov, nil
}

// stagesOf returns the emit stages cov needs: E0, which every case
// needs, and for every block it covers but those every process runs as
// it starts (see idleArgs), the stage stagedFile.stageAt gives it, but
// for a block of a later stage that a checkpoint's baseline programs
// run too, a dispatch's not-taken path. That one counts for E1 when E1's
// baselines run it, every lowered program needing E1, and for H's, for
// baselineH: H itself but for the census's question of what the stages
// would need were the header's part of every program the core's.
// Otherwise its port is the work of its own stage and of each later
// checkpoint whose baselines run it, any of which may come first, so it
// needs nothing more when the case needs one of them for its other
// blocks, and the first of them when it does not. A block of a file with
// no stage, which stagedSources would have refused, is an error.
func stagesOf(cov covered, baselineH emitStage) (emitSet, error) {
	set := emitSet(0).with(emitE0)
	var either []emitSet
	for _, file := range slices.Sorted(maps.Keys(cov)) {
		sf := cover.staged[file]
		if sf == nil {
			return 0, fmt.Errorf("a block of %s, which has no emit stage", file)
		}
		for _, s := range cov[file] {
			line, col, err := parseStart(s)
			if err != nil {
				return 0, err
			}
			if cover.idle[file][s] {
				// Every process runs it, whatever its input.
				continue
			}
			alts := blockStages(file, s, sf.stageAt(line, col), baselineH)
			if popCount(alts) == 1 {
				set |= alts
			} else {
				either = append(either, alts)
			}
		}
	}
	for _, alts := range either {
		if set&alts == 0 {
			set = set.with(lowest(alts))
		}
	}
	return set, nil
}

// lowest returns the first stage of s.
func lowest(s emitSet) emitStage {
	for st := range emitStages {
		if s.has(st) {
			return st
		}
	}
	return emitE0
}

// parseStart returns the line and column of s, a block's start as
// line.column.
func parseStart(s string) (int, int, error) {
	l, c, ok := strings.Cut(s, ".")
	line, err1 := strconv.Atoi(l)
	col, err2 := strconv.Atoi(c)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("a malformed block start %q", s)
	}
	return line, col, nil
}

// coverKey returns the cache key of running in with args: the SHA-256 of
// coverFormat, the instrumented binary's hash, the command line, in's
// file and environment, and the fingerprints of the files the run can
// read (see scopeOf) and of the std it runs with, so that a key is the
// same for the same bytes wherever they are written, as the archives are
// to a new temporary directory each run.
func coverKey(in input, args []string) (string, error) {
	h := sha256.New()
	// A hash's Write never fails.
	put := func(s string) { _, _ = fmt.Fprintf(h, "%d:%s;", len(s), s) }
	put(coverFormat)
	put(cover.binHash)
	put(strings.Join(args, "\x00"))
	put(in.file)
	std := cover.stdHash
	for _, e := range in.env {
		if home, ok := strings.CutPrefix(e, "TACK_HOME="); ok {
			var err error
			if std, err = treeHash(filepath.Join(home, "src", "std")); err != nil {
				return "", err
			}
			continue
		}
		put(e)
	}
	put(std)
	scope, single := scopeOf(in)
	fp, err := scopeHash(in, scope, single)
	if err != nil {
		return "", err
	}
	put(fp)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// scopeOf returns the directory holding what a run on in can read
// besides std: the nearest directory from in's target up to in.dir that
// holds a tack.mod, its module; else, for a directory, the directory,
// and for a file, the file's directory, single true, since a single file
// loads only itself and the C beside it. A target that is no path on
// disk, an import path, is read from the module at in.dir.
func scopeOf(in input) (string, bool) {
	target := filepath.Join(in.dir, filepath.FromSlash(in.file))
	info, err := os.Stat(target)
	if err != nil {
		return in.dir, false
	}
	start := target
	if !info.IsDir() {
		start = filepath.Dir(target)
	}
	for d := start; ; d = filepath.Dir(d) {
		if fileExists(filepath.Join(d, "tack.mod")) {
			return d, false
		}
		if d == in.dir || d == filepath.Dir(d) || !strings.HasPrefix(d, in.dir) {
			break
		}
	}
	return start, !info.IsDir()
}

// fileExists reports whether p is a regular file.
func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// readable reports whether a file named name can be read by a load: a
// Tack, C or header file, or a tack.mod.
func readable(name string) bool {
	switch filepath.Ext(name) {
	case ".tck", ".c", ".h":
		return true
	}
	return name == "tack.mod"
}

// scopeHash returns the fingerprint of the files below scope that a load
// of in can read (see readable), each named by its path from in.dir: of
// every one of them, or, when single, of in's target and the C and
// header files directly in scope, the other Tack files there being
// other programs. Directories whose names start with a dot are skipped.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func scopeHash(in input, scope string, single bool) (string, error) {
	h := sha256.New()
	dir := in.dir
	if single {
		if err := hashFile(h, dir, filepath.Join(dir, filepath.FromSlash(in.file))); err != nil {
			return "", fmt.Errorf("fingerprinting %s: %w", in.name, err)
		}
	}
	err := filepath.WalkDir(scope, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != scope && (strings.HasPrefix(d.Name(), ".") || single) {
				return filepath.SkipDir
			}
			return nil
		}
		if !readable(d.Name()) || single && filepath.Ext(p) == ".tck" {
			return nil
		}
		return hashFile(h, dir, p)
	})
	if err != nil {
		return "", fmt.Errorf("fingerprinting %s: %w", scope, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashFile writes p's path from dir and its bytes to h.
func hashFile(h interface{ Write([]byte) (int, error) }, dir, p string) error {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return fmt.Errorf("naming %s: %w", p, err)
	}
	data, err := os.ReadFile(p) //nolint:gosec // G304: a file of the corpus
	if err != nil {
		return fmt.Errorf("fingerprinting: %w", err)
	}
	if _, err = fmt.Fprintf(
		h,
		"%s\x00%d\x00%s",
		filepath.ToSlash(rel),
		len(data),
		data,
	); err != nil {
		return fmt.Errorf("fingerprinting %s: %w", p, err)
	}
	return nil
}

// treeHash returns the fingerprint of every file below dir, each named
// by its path from dir.
func treeHash(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return hashFile(h, dir, p)
	})
	if err != nil {
		return "", fmt.Errorf("fingerprinting %s: %w", dir, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// classifyWith returns a stage's classify for the command line args
// gives: what the instrumented compiler covers running a case, as
// caseCovered finds it.
func classifyWith(args func(input) []string) func(in input, fresh bool) (covered, error) {
	return func(in input, fresh bool) (covered, error) { return caseCovered(in, args(in), fresh) }
}

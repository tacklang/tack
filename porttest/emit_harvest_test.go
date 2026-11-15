package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The harvest of emit-cases: every program the C backend's tests hand
// the front end, captured by running those tests, go test ./codegen/c,
// with go's -overlay flag swapping in copies of the two files whose
// helpers every test's program goes through, gen_test.go's compile and
// printf_test.go's compileWithStd, each writing src to a directory before
// it compiles it, and with a clang on PATH that only writes, at its -o,
// a program that exits 0, so no test spends its time in C. No file of
// the frozen tree changes. A test that then fails, comparing what the
// fake binary did with what it wanted, has already handed over its
// program.

// harvestDirVar is the environment variable the overlaid helpers write
// the programs to.
const harvestDirVar = "PORTTEST_HARVEST_DIR"

// harvestHooks are the overlaid files, by path from the module's root,
// each with the line its helper's body starts with, after which the
// overlay calls harvestDump.
var harvestHooks = map[string]string{
	"codegen/c/gen_test.go":    "func compile(t *testing.T, src string) *ast.File {\n\tt.Helper()\n",
	"codegen/c/printf_test.go": "func compileWithStd(t testing.TB, src string) *ast.Program {\n\tt.Helper()\n",
}

// harvestDumpFile is the file the overlay adds to package c_test,
// harvestDump, which writes src to $PORTTEST_HARVEST_DIR named by its
// SHA-256, so each program is written once whatever test hands it over.
const harvestDumpFile = `package c_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func harvestDump(src string) {
	sum := sha256.Sum256([]byte(src))
	p := filepath.Join(os.Getenv("` + harvestDirVar + `"), hex.EncodeToString(sum[:])+".tck")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		panic(err)
	}
}
`

// fakeClang is the clang the harvest's tests run: it writes, at the path
// its -o names, a program that exits 0, and does nothing else.
const fakeClang = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift; fi
  shift
done
if [ -n "$out" ]; then printf '#!/bin/sh\nexit 0\n' > "$out" && chmod +x "$out"; fi
exit 0
`

// harvestTimeout bounds the run of the backend's tests.
const harvestTimeout = 20 * time.Minute

// The harvest of the frozen tests, pinned: how many programs, and the
// SHA-256 of their own SHA-256s, sorted, each followed by a newline, so
// a harvest that changes, finding other programs or fewer, fails
// TestEmitHarvest.
const (
	harvestedPrograms = 743
	harvestedDigest   = "a16aa28830a55a7f4c035243833aaae794d7b4ff18b9e3ae638c8232d492ccaf"
)

// harvested is one program of the harvest: its name, the first twelve
// digits of its SHA-256, and its source.
type harvested struct {
	name, src string
}

// harvest holds the harvest and its inputs, made once a run.
var harvest struct {
	err   error
	progs []harvested
	// fragments counts what the helpers were handed that is no program,
	// a piece with no package clause, which is left out.
	fragments int
	once      sync.Once
}

// harvestPrograms returns the harvest, in order of SHA-256, running the
// backend's tests the first time a run asks, unless the cache below the
// coverage cache holds the harvest of the same inputs (see harvestKey).
func harvestPrograms() ([]harvested, int, error) {
	harvest.once.Do(func() { harvest.progs, harvest.fragments, harvest.err = runHarvest() })
	return harvest.progs, harvest.fragments, harvest.err
}

// runHarvest is harvestPrograms' work, done once a run.
func runHarvest() ([]harvested, int, error) {
	if env.tmp == "" {
		return nil, 0, fmt.Errorf("the harvest runs only as TestPort does")
	}
	key, err := harvestKey()
	if err != nil {
		return nil, 0, err
	}
	cached := filepath.Join(harvestCacheDir(), key+"-bytes.json")
	// Each program is held as bytes, which JSON writes in base64: a
	// string would have its bytes that are not UTF-8 replaced.
	srcs, err := cachedHarvest(cached)
	if err != nil {
		return nil, 0, err
	}
	var progs []harvested
	fragments := 0
	for _, b := range srcs {
		src := string(b)
		if !isProgram(src) {
			fragments++
			continue
		}
		sum := sha256.Sum256([]byte(src))
		progs = append(progs, harvested{name: hex.EncodeToString(sum[:])[:12], src: src})
	}
	return progs, fragments, nil
}

// cachedHarvest returns the harvest cached at path, or, when there is
// none, runs the backend's tests for it and caches it there.
func cachedHarvest(path string) ([][]byte, error) {
	var srcs [][]byte
	data, err := os.ReadFile(path) //nolint:gosec // G304: the cache's own file
	if err == nil {
		if err = json.Unmarshal(data, &srcs); err != nil {
			return nil, fmt.Errorf("reading the cached harvest %s: %w", path, err)
		}
		return srcs, nil
	}
	if srcs, err = harvestByTests(); err != nil {
		return nil, err
	}
	if data, err = json.Marshal(srcs); err != nil {
		return nil, fmt.Errorf("caching the harvest: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("caching the harvest: %w", err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("caching the harvest: %w", err)
	}
	return srcs, nil
}

// harvestCacheDir is where the harvest is cached: beside the coverage
// cache, under $PORTTEST_COVER_CACHE or tack-porttest in
// os.UserCacheDir.
func harvestCacheDir() string {
	if dir := os.Getenv(coverCacheVar); dir != "" {
		return filepath.Join(fromRoot(dir), "harvest")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		// No cache directory: the harvest is redone each run.
		return filepath.Join(env.tmp, "harvest-cache")
	}
	return filepath.Join(base, "tack-porttest", "harvest")
}

// harvestFile is this file, whose overlay, fake clang and harvest code
// decide what the harvest holds.
const harvestFile = "porttest/emit_harvest_test.go"

// harvestKey returns the key the harvest is cached under: the
// fingerprint of everything that decides what the backend's tests hand
// over: the Go toolchain's version; this file, harvestFile, whole, the
// overlay, the fake clang and the code that runs them; every .go file of
// the module but porttest's; go.mod and go.sum; and the files the tests
// read or the compiler embeds, examples/, src/std, codegen/c/testdata
// and builtin/.
func harvestKey() (string, error) {
	h := sha256.New()
	version, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return "", fmt.Errorf("finding go's version: %w", err)
	}
	put := func(s string) { _, _ = fmt.Fprintf(h, "%d:%s;", len(s), s) } // A hash's Write never fails.
	put(string(version))
	goFiles, err := moduleGoHash()
	if err != nil {
		return "", err
	}
	put(goFiles)
	for _, rel := range []string{harvestFile, "go.mod", "go.sum"} {
		if err := hashFile(
			h,
			env.root,
			filepath.Join(env.root, filepath.FromSlash(rel)),
		); err != nil {
			return "", err
		}
	}
	for _, dir := range []string{
		"examples", filepath.Join("src", "std"), filepath.Join("codegen", "c", "testdata"), "builtin",
	} {
		fp, err := treeHash(filepath.Join(env.root, dir))
		if err != nil {
			return "", err
		}
		put(dir)
		put(fp)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// moduleGoHash returns the fingerprint of every .go file of the module
// but porttest's.
func moduleGoHash() (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(env.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); p != env.root &&
				(strings.HasPrefix(name, ".") || name == "porttest" ||
					name == "bin") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		return hashFile(h, env.root, p)
	})
	if err != nil {
		return "", fmt.Errorf("fingerprinting the module's Go files: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// harvestByTests runs the backend's tests with the overlay and the fake
// clang and returns what they handed over, sorted by SHA-256. The tests
// may fail, against a clang that builds nothing; a build or setup that
// fails, or a run that writes no program, is an error.
//
//nolint:cyclop,funlen // a run of steps, each returning its error or skipping; split, it reads no plainer
func harvestByTests() ([][]byte, error) {
	work := filepath.Join(env.tmp, "harvest")
	out := filepath.Join(work, "programs")
	bin := filepath.Join(work, "bin")
	for _, d := range []string{out, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("making the harvest's directories: %w", err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(bin, "clang"),
		[]byte(fakeClang),
		0o700,
	); err != nil { //nolint:gosec // G306: a script it runs
		return nil, fmt.Errorf("writing the fake clang: %w", err)
	}
	overlay, err := writeOverlay(work)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), harvestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-overlay="+overlay, "./codegen/c")
	cmd.Dir = env.root
	cmd.Env = append(os.Environ(), harvestDirVar+"="+out, "PATH="+bin+":"+os.Getenv("PATH"))
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	runErr := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("the backend's tests ran past %v", harvestTimeout)
	case bytes.Contains(log.Bytes(), []byte("[build failed]")) || bytes.Contains(log.Bytes(), []byte("[setup failed]")):
		return nil, fmt.Errorf(
			"the backend's tests did not build with the overlay: %w\n%s",
			runErr,
			log.Bytes(),
		)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return nil, fmt.Errorf("reading the harvest: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf(
			"the backend's tests handed over no program: %w\n%s",
			runErr,
			log.Bytes(),
		)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	srcs := make([][]byte, 0, len(names))
	for _, n := range names {
		data, err := os.ReadFile(
			filepath.Join(out, n),
		) //nolint:gosec // G304: the harvest's own files
		if err != nil {
			return nil, fmt.Errorf("reading the harvest: %w", err)
		}
		srcs = append(srcs, data)
	}
	return srcs, nil
}

// writeOverlay writes, below work, the overlaid files and the overlay
// file naming them, and returns its path. A hook whose helper is not
// where harvestHooks says is an error, so a change to the frozen tests
// cannot leave a helper unhooked unseen.
func writeOverlay(work string) (string, error) {
	replace := map[string]string{}
	for rel, start := range harvestHooks {
		orig := filepath.Join(env.root, filepath.FromSlash(rel))
		data, err := os.ReadFile(orig) //nolint:gosec // G304: a file of this module
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", rel, err)
		}
		if bytes.Count(data, []byte(start)) != 1 {
			return "", fmt.Errorf("%s does not hold its helper's start once: %q", rel, start)
		}
		hooked := bytes.Replace(data, []byte(start), []byte(start+"\tharvestDump(src)\n"), 1)
		copied := filepath.Join(work, filepath.Base(rel))
		if err := os.WriteFile(copied, hooked, 0o600); err != nil {
			return "", fmt.Errorf("writing the overlay: %w", err)
		}
		replace[orig] = copied
	}
	dump := filepath.Join(work, "zz_harvest_test.go")
	if err := os.WriteFile(dump, []byte(harvestDumpFile), 0o600); err != nil {
		return "", fmt.Errorf("writing the overlay: %w", err)
	}
	replace[filepath.Join(env.root, "codegen", "c", "zz_harvest_test.go")] = dump
	data, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return "", fmt.Errorf("writing the overlay: %w", err)
	}
	p := filepath.Join(work, "overlay.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", fmt.Errorf("writing the overlay: %w", err)
	}
	return p, nil
}

// harvestDigestOf returns the digest harvestedDigest pins of progs.
func harvestDigestOf(progs []harvested) string {
	h := sha256.New()
	for _, p := range progs {
		sum := sha256.Sum256([]byte(p.src))
		_, _ = fmt.Fprintf(h, "%x\n", sum) // A hash's Write never fails.
	}
	return hex.EncodeToString(h.Sum(nil))
}

// isProgram reports whether s starts with a package clause, after white
// space and // comments.
func isProgram(s string) bool {
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		rest, ok := strings.CutPrefix(s, "//")
		if !ok {
			return strings.HasPrefix(s, "package ")
		}
		_, s, _ = strings.Cut(rest, "\n")
	}
}

// TestEmitHarvest checks that the harvest of the backend's tests is the
// pinned one, harvestedPrograms programs whose digest is
// harvestedDigest, and logs how many fragments it left out. It runs the
// backend's tests, or reads their cached harvest, and so runs only as
// TestPort does.
func TestEmitHarvest(t *testing.T) {
	ready(t)
	progs, fragments, err := harvestPrograms()
	if err != nil {
		t.Fatal(err)
	}
	digest := harvestDigestOf(progs)
	t.Logf("%d programs, digest %s; %d fragments left out", len(progs), digest, fragments)
	if len(progs) != harvestedPrograms || digest != harvestedDigest {
		t.Errorf("harvested %d programs, digest %s; want %d, %s", len(progs), digest,
			harvestedPrograms, harvestedDigest)
	}
}

// TestIsProgram checks what counts as a program: a package clause
// first, after white space and comments.
func TestIsProgram(t *testing.T) {
	for _, s := range []string{"package main\n", "\n  package p", "// c\npackage main", "// a\n// b\n\tpackage m"} {
		if !isProgram(s) {
			t.Errorf("isProgram(%q) = false", s)
		}
	}
	for _, s := range []string{"", "func main() {}", "// package main", "packages", "x\npackage main"} {
		if isProgram(s) {
			t.Errorf("isProgram(%q) = true", s)
		}
	}
}

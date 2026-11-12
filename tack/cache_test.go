package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// countBackend is the c backend with its std compiles logged: each
// CompileStdCmd it returns appends the C file's path, one line, to the
// file $TACK_TEST_STD_LOG names before running clang, so a test can count
// the std files a build compiled. When $TACK_TEST_STD_DROP is set, the
// argument it names is left out of every CompileStdCmd, as a compiler
// from before that flag was added ran clang. Everything else, linking
// included, is the c backend's own.
const countBackend = "test-count"

// stdLogVar is the variable naming countBackend's log.
const stdLogVar = "TACK_TEST_STD_LOG"

// stdDropVar is the variable naming the argument countBackend leaves out
// of its std compiles.
const stdDropVar = "TACK_TEST_STD_DROP"

// countingStd is countBackend's type.
type countingStd struct{ codegen.Backend }

// Name returns countBackend.
func (countingStd) Name() string { return countBackend }

// CompileStdCmd is the c backend's command, without the argument
// $TACK_TEST_STD_DROP names when it is set, run by a shell that first
// logs src to $TACK_TEST_STD_LOG.
func (b countingStd) CompileStdCmd(src, include, obj string, opt codegen.Opt) []string {
	cmd := b.Backend.CompileStdCmd(src, include, obj, opt)
	if drop := os.Getenv(stdDropVar); drop != "" {
		cmd = slices.DeleteFunc(cmd, func(arg string) bool { return arg == drop })
	}
	return append([]string{
		"sh", "-c", `printf '%s\n' "$1" >> "$` + stdLogVar + `"; shift; exec "$@"`, "sh", src,
	}, cmd...)
}

// init registers countBackend.
func init() { codegen.Register(countingStd{Backend: c.New()}) }

// stdLog points countBackend's log at a fresh file and returns a function
// reporting the std files compiled since the last time it was called, by
// base name, sorted.
func stdLog(t *testing.T) func() []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "std.log")
	t.Setenv(stdLogVar, path)
	seen := 0
	return func() []string {
		t.Helper()
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Fields(string(data))
		fresh := lines[seen:]
		seen = len(lines)
		names := make([]string, 0, len(fresh))
		for _, l := range fresh {
			names = append(names, filepath.Base(l))
		}
		slices.Sort(names)
		return names
	}
}

// needClang skips the test when clang is not on PATH.
func needClang(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
}

// fakeVersion is the toolchain identity the cache tests give the driver
// unless they say otherwise.
const fakeVersion = "clang version test-1\nTarget: test\n"

// cacheEnv is the environment of a cache test: std at home, the cache at
// cache, and the toolchain identified as version, whatever clang is on
// PATH. Looking up the user's cache directory fails the test.
func cacheEnv(t *testing.T, home, cache, version string) environment {
	t.Helper()
	env := envAt(t, home)
	env.tackCache = cache
	env.toolVersion = func(context.Context, []string) ([]byte, error) {
		return []byte(version), nil
	}
	return env
}

// copyStd copies the repository's std into a fresh TACK_HOME and returns
// it, for a test that changes std's files.
func copyStd(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	err := filepath.WalkDir(stdtest.Dir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stdtest.Home(), path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path) //nolint:gosec // G304: copying std is the point
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return home
}

// appendTo appends text to the file at path.
func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// printExit is a program that calls both builtins, so both std objects are
// linked: it prints hi and exits 3.
const printExit = "    print(\"hi\")\n    exit(3)\n"

// buildCounted builds src with countBackend in env, extra flags first, and
// checks the binary prints hi and exits 3 and the build left nothing in
// tmp, the temp directory.
func buildCounted(t *testing.T, env environment, tmp, src string, extra ...string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	args := append(append([]string{}, extra...), "--backend", countBackend, "--out", bin, src)
	r := runTackEnv(t, t.Context(), env, args...)
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("build: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if out, code := runBuilt(t, bin); out != "hi" || code != 3 {
		t.Errorf("printed %q, exited %d; want %q and 3", out, code, "hi")
	}
	assertEmptyDir(t, tmp)
}

// allStd is every std file, by base name, sorted: what a build with an
// empty cache compiles.
var allStd = []string{"exit.c", "list.c", "print.c", "process.c", "unreachable.c"}

// A second build of a program finds std's objects in the cache and runs
// no compiler on std's C, and its binary behaves as the first's. The
// cache holds only finished entries, no temporary file.
func TestStdCacheHit(t *testing.T) {
	needClang(t)
	tmp := isolateTemp(t)
	compiled := stdLog(t)
	cache := t.TempDir()
	env := cacheEnv(t, stdtest.Home(), cache, fakeVersion)
	src := program(t, printExit)

	buildCounted(t, env, tmp, src)
	if got := compiled(); !slices.Equal(got, allStd) {
		t.Fatalf("first build compiled %q, want %q", got, allStd)
	}
	buildCounted(t, env, tmp, src)
	if got := compiled(); len(got) != 0 {
		t.Errorf("second build compiled %q, want nothing", got)
	}

	// A program calling one builtin is served from the same entries.
	bin := filepath.Join(t.TempDir(), "bin")
	r := runTackEnv(t, t.Context(), env,
		"--backend", countBackend, "--out", bin, program(t, "    exit(5)\n"))
	if r.code != exitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := compiled(); len(got) != 0 {
		t.Errorf("third build compiled %q, want nothing", got)
	}
	if _, code := runBuilt(t, bin); code != 5 {
		t.Errorf("exit status %d, want 5", code)
	}

	err := filepath.WalkDir(cache, func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".tmp") {
			t.Errorf("temporary file left in the cache: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stdChange is a change to a build's inputs, for TestStdCacheMisses.
type stdChange struct {
	// change alters the inputs, with std at home and env the environment
	// of the builds before it; it returns the environment and the flags of
	// the builds after it, env itself when its tackHome is empty.
	change func(t *testing.T, home string, env environment) (environment, []string)
	name   string
	want   []string // the runtime files the build after it compiles, nil for none
}

// stdChanges is every change to a build that could touch a runtime
// object's inputs: those that must miss the cache, the optimization level
// and the toolchain's identity, and those in TACK_HOME, where the runtime
// once came from, which no key holds and which must hit it, compiling
// nothing.
func stdChanges() []stdChange {
	return []stdChange{
		{
			name: "planted c file",
			change: func(t *testing.T, home string, env environment) (environment, []string) {
				writeOrFail(t, filepath.Join(home, "src", "std", "builtin", "print.c"),
					[]byte("#error planted\n"))
				return env, nil
			},
		},
		{
			name: "include header",
			change: func(t *testing.T, home string, env environment) (environment, []string) {
				appendTo(t, filepath.Join(home, "src", "std", "include", "tack", "tack.h"),
					"\n#error changed\n")
				return env, nil
			},
		},
		{
			name: "new include header",
			change: func(t *testing.T, home string, env environment) (environment, []string) {
				writeOrFail(t, filepath.Join(home, "src", "std", "include", "tack", "more.h"), nil)
				return env, nil
			},
		},
		{
			name: "header in std/builtin",
			change: func(t *testing.T, home string, env environment) (environment, []string) {
				writeOrFail(t, filepath.Join(home, "src", "std", "builtin", "local.h"), nil)
				return env, nil
			},
		},
		{
			name: "opt level",
			change: func(*testing.T, string, environment) (environment, []string) {
				return environment{}, []string{"-O0"}
			},
			want: allStd,
		},
		{
			name: "toolchain identity",
			change: func(t *testing.T, home string, env environment) (environment, []string) {
				return cacheEnv(t, home, env.tackCache, "clang version test-2\n"), nil
			},
			want: allStd,
		},
		{
			name: "std moved",
			change: func(t *testing.T, home string, env environment) (environment, []string) {
				moved := filepath.Join(t.TempDir(), "moved")
				if err := os.Rename(home, moved); err != nil {
					t.Fatal(err)
				}
				return cacheEnv(t, moved, env.tackCache, fakeVersion), nil
			},
		},
	}
}

// A runtime object's key holds every input that decides it, the
// optimization level and the toolchain's identity, so changing either
// misses the cache and compiles every runtime file, and a build after it
// hits again; the runtime itself is built into the compiler, so nothing
// in TACK_HOME, a planted C file, a changed or new header, or std moved,
// is an input, and a build after any of them compiles nothing.
func TestStdCacheMisses(t *testing.T) {
	needClang(t)
	for _, tt := range stdChanges() {
		t.Run(tt.name, func(t *testing.T) {
			tmp := isolateTemp(t)
			compiled := stdLog(t)
			home := copyStd(t)
			env := cacheEnv(t, home, t.TempDir(), fakeVersion)
			src := program(t, printExit)

			buildCounted(t, env, tmp, src)
			if got := compiled(); !slices.Equal(got, allStd) {
				t.Fatalf("first build compiled %q, want %q", got, allStd)
			}
			after, flags := tt.change(t, home, env)
			if after.tackHome == "" {
				after = env
			}
			buildCounted(t, after, tmp, src, flags...)
			if got := compiled(); !slices.Equal(got, tt.want) {
				t.Errorf("build after the change compiled %q, want %q", got, tt.want)
			}
			buildCounted(t, after, tmp, src, flags...)
			if got := compiled(); len(got) != 0 {
				t.Errorf("build after that compiled %q, want nothing", got)
			}
		})
	}
}

// A runtime object's key holds the whole command that compiles it, so an
// object cached by a build without -fno-finite-loops, as one from before
// the flag was added was, is never linked by a build that passes it: that
// build misses the cache and compiles every runtime file again, and a
// build after it hits.
func TestStdCacheMissesWhenFlagsChange(t *testing.T) {
	needClang(t)
	tmp := isolateTemp(t)
	compiled := stdLog(t)
	env := cacheEnv(t, stdtest.Home(), t.TempDir(), fakeVersion)
	src := program(t, printExit)

	t.Setenv(stdDropVar, "-fno-finite-loops")
	buildCounted(t, env, tmp, src)
	if got := compiled(); !slices.Equal(got, allStd) {
		t.Fatalf("build without the flag compiled %q, want %q", got, allStd)
	}
	t.Setenv(stdDropVar, "")
	buildCounted(t, env, tmp, src)
	if got := compiled(); !slices.Equal(got, allStd) {
		t.Errorf("build with the flag compiled %q, want %q", got, allStd)
	}
	buildCounted(t, env, tmp, src)
	if got := compiled(); len(got) != 0 {
		t.Errorf("build after that compiled %q, want nothing", got)
	}
}

// cacheDamage is a way to damage a cache entry, for
// TestStdCacheCorruptEntryIsRebuilt.
type cacheDamage struct {
	damage func(t *testing.T, path string) // alters the file at path
	name   string
	dir    string // the cache's subdirectory whose files damage alters
}

// writeOrFail writes data to the file at path, failing the test if it
// cannot.
func writeOrFail(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// cacheDamages is every way a cache entry is damaged that a build must
// detect and repair.
func cacheDamages() []cacheDamage {
	return []cacheDamage{
		{name: "object byte flipped", dir: "objects", damage: func(t *testing.T, path string) {
			data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own cache
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)/2] ^= 0xff
			writeOrFail(t, path, data)
		}},
		{name: "object truncated", dir: "objects", damage: func(t *testing.T, path string) {
			if err := os.Truncate(path, 10); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "object empty", dir: "objects", damage: func(t *testing.T, path string) {
			writeOrFail(t, path, nil)
		}},
		{name: "object removed", dir: "objects", damage: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "key garbage", dir: "keys", damage: func(t *testing.T, path string) {
			writeOrFail(t, path, []byte("\x00not a hash\xff"))
		}},
		{name: "key in upper case", dir: "keys", damage: func(t *testing.T, path string) {
			data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own cache
			if err != nil {
				t.Fatal(err)
			}
			writeOrFail(t, path, bytes.ToUpper(data))
		}},
		{name: "key names no object", dir: "keys", damage: func(t *testing.T, path string) {
			writeOrFail(t, path, []byte(strings.Repeat("ab", 32)+"\n"))
		}},
		{name: "key names a path", dir: "keys", damage: func(t *testing.T, path string) {
			writeOrFail(t, path, []byte("../../../../../../../../../../../../../../etc/passwd\n"))
		}},
		{
			name: "key names bytes that are no object",
			dir:  "keys",
			damage: func(t *testing.T, path string) {
				garbage := []byte("hashes right, links wrong")
				sum := hexSum(garbage)
				writeOrFail(
					t,
					filepath.Join(filepath.Dir(path), "..", "objects", sum+".o"),
					garbage,
				)
				writeOrFail(t, path, []byte(sum+"\n"))
			},
		},
		{name: "key empty", dir: "keys", damage: func(t *testing.T, path string) {
			writeOrFail(t, path, nil)
		}},
	}
}

// filledCache builds printExit with countBackend into a fresh cache, std
// the repository's, and returns the cache, the environment, the program,
// the temp directory and the log of std compiles, emptied.
func filledCache(
	t *testing.T,
) (cache string, env environment, src, tmp string, compiled func() []string) {
	t.Helper()
	tmp = isolateTemp(t)
	compiled = stdLog(t)
	cache = t.TempDir()
	env = cacheEnv(t, stdtest.Home(), cache, fakeVersion)
	src = program(t, printExit)
	buildCounted(t, env, tmp, src)
	compiled()
	return cache, env, src, tmp, compiled
}

// damageAll applies damage to every file in the subdirectory dir of the
// std part of cache, which must hold one for each std file.
func damageAll(t *testing.T, cache, dir string, damage func(*testing.T, string)) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cache, "std", dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(allStd) {
		t.Fatalf("%d entries in %s, want %d", len(entries), dir, len(allStd))
	}
	for _, e := range entries {
		damage(t, filepath.Join(cache, "std", dir, e.Name()))
	}
}

// A cache entry that is damaged, an object altered, truncated or gone, or
// a key that does not name an object, is not trusted: the object is
// compiled again, the binary still works, and the entry is repaired.
func TestStdCacheCorruptEntryIsRebuilt(t *testing.T) {
	needClang(t)
	for _, tt := range cacheDamages() {
		t.Run(tt.name, func(t *testing.T) {
			cache, env, src, tmp, compiled := filledCache(t)
			damageAll(t, cache, tt.dir, tt.damage)
			buildCounted(t, env, tmp, src)
			if got := compiled(); !slices.Equal(got, allStd) {
				t.Errorf("build after the damage compiled %q, want %q", got, allStd)
			}
			buildCounted(t, env, tmp, src)
			if got := compiled(); len(got) != 0 {
				t.Errorf("build after the repair compiled %q, want nothing", got)
			}
		})
	}
}

// A directory where an object belongs is a miss, and cannot be replaced
// by a rename, so the build fails, naming the cache, rather than linking
// what it cannot verify.
func TestStdCacheObjectIsADirectory(t *testing.T) {
	needClang(t)
	cache, env, src, tmp, _ := filledCache(t)
	damageAll(t, cache, "objects", func(t *testing.T, path string) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o750); err != nil {
			t.Fatal(err)
		}
	})
	r := runTackEnv(t, t.Context(), env, "--backend", countBackend,
		"--out", filepath.Join(t.TempDir(), "bin"), src)
	assertFailure(t, r, "cannot write to the build cache "+filepath.Join(cache, "std"))
	assertEmptyDir(t, tmp)
}

// Builds that share an empty cache and run at once all succeed, each
// writing every entry, and leave a cache a later build hits.
func TestStdCacheConcurrentBuilds(t *testing.T) {
	needClang(t)
	tmp := isolateTemp(t)
	compiled := stdLog(t)
	env := cacheEnv(t, stdtest.Home(), t.TempDir(), fakeVersion)
	src := program(t, printExit)

	const builds = 4
	bins := make([]string, builds)
	results := make([]result, builds)
	var wg sync.WaitGroup
	for i := range builds {
		bins[i] = filepath.Join(t.TempDir(), "bin")
		wg.Go(func() {
			var stdout, stderr bytes.Buffer
			code := run(t.Context(), []string{"--backend", countBackend, "--out", bins[i], src},
				env, &stdout, &stderr)
			results[i] = result{stdout: stdout.String(), stderr: stderr.String(), code: code}
		})
	}
	wg.Wait()
	for i, r := range results {
		if r.code != exitOK || r.stdout != "" || r.stderr != "" {
			t.Errorf("build %d: exit %d, stdout %q, stderr %q", i, r.code, r.stdout, r.stderr)
			continue
		}
		if out, code := runBuilt(t, bins[i]); out != "hi" || code != 3 {
			t.Errorf("build %d printed %q, exited %d; want %q and 3", i, out, code, "hi")
		}
	}
	assertEmptyDir(t, tmp)
	compiled()
	buildCounted(t, env, tmp, src)
	if got := compiled(); len(got) != 0 {
		t.Errorf("build after the concurrent ones compiled %q, want nothing", got)
	}
}

// A cache that cannot be found or created ends a build that needs std's
// objects with an error naming the problem, and the temp directory is
// still removed; a build that calls no builtin never looks for the cache.
func TestStdCacheUnusable(t *testing.T) {
	needClang(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unknown := cacheEnv(t, stdtest.Home(), "", fakeVersion)
	unknown.userCacheDir = func() (string, error) {
		return "", errors.New("neither $XDG_CACHE_HOME nor $HOME are defined")
	}
	for _, tt := range []struct {
		name string
		env  environment
		want string
	}{
		{
			"a file",
			cacheEnv(t, stdtest.Home(), file, fakeVersion),
			"cannot use the build cache " + filepath.Join(file, "std") + ": ",
		},
		{
			"beneath a file",
			cacheEnv(t, stdtest.Home(), filepath.Join(file, "cache"), fakeVersion),
			"cannot use the build cache " + filepath.Join(file, "cache", "std") + ": ",
		},
		{
			"unknown",
			unknown,
			"cannot find the build cache: TACK_CACHE is not set and the user cache " +
				"directory is unknown: neither $XDG_CACHE_HOME nor $HOME are defined",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmp := isolateTemp(t)
			r := runTackEnv(t, t.Context(), tt.env,
				"--out", filepath.Join(t.TempDir(), "bin"), program(t, printExit))
			assertFailure(t, r, tt.want)
			assertEmptyDir(t, tmp)

			bin := filepath.Join(t.TempDir(), "bin")
			r = runTackEnv(
				t,
				t.Context(),
				tt.env,
				"--out",
				bin,
				program(t, "    if true {\n    }\n"),
			)
			if r.code != exitOK || r.stderr != "" {
				t.Errorf("program calling no builtin: exit %d, stderr %q", r.code, r.stderr)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

// A cache directory that cannot be written to fails the build that would
// write an entry, naming the cache.
func TestStdCacheReadOnly(t *testing.T) {
	needClang(t)
	if os.Geteuid() == 0 {
		t.Skip("root writes to read-only directories")
	}
	cache := t.TempDir()
	for _, d := range []string{"keys", "objects"} {
		if err := os.MkdirAll(filepath.Join(cache, "std", d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	ro := filepath.Join(cache, "std", "objects")
	if err := os.Chmod(ro, 0o500); err != nil { //nolint:gosec // read-only on purpose
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o750) }) //nolint:gosec // so TempDir can remove it
	tmp := isolateTemp(t)
	r := runTackEnv(t, t.Context(), cacheEnv(t, stdtest.Home(), cache, fakeVersion),
		"--out", filepath.Join(t.TempDir(), "bin"), program(t, printExit))
	assertFailure(t, r, "cannot write to the build cache "+filepath.Join(cache, "std"))
	assertEmptyDir(t, tmp)
}

// The cache is $TACK_CACHE, made absolute from the current directory when
// relative, or the tack directory in the user's cache directory when
// TACK_CACHE is unset or empty; std's entries go in its std directory.
func TestStdCacheLocation(t *testing.T) {
	needClang(t)
	build := func(t *testing.T, env environment) {
		t.Helper()
		r := runTackEnv(t, t.Context(), env, "--backend", objsBackend,
			"--out", filepath.Join(t.TempDir(), "objs"), program(t, printExit))
		if r.code != exitOK || r.stderr != "" {
			t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
		}
	}
	hasEntries := func(t *testing.T, root string) {
		t.Helper()
		for _, d := range []string{"keys", "objects"} {
			entries, err := os.ReadDir(filepath.Join(root, "std", d))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(allStd) {
				t.Errorf("%d entries in %s, want %d", len(entries), d, len(allStd))
			}
		}
	}

	t.Run("TACK_CACHE", func(t *testing.T) {
		cache := filepath.Join(t.TempDir(), "made", "on", "demand")
		build(t, cacheEnv(t, stdtest.Home(), cache, fakeVersion))
		hasEntries(t, cache)
	})
	t.Run("relative", func(t *testing.T) {
		wd := t.TempDir()
		t.Chdir(wd)
		build(t, cacheEnv(t, stdtest.Home(), "rel", fakeVersion))
		hasEntries(t, filepath.Join(wd, "rel"))
	})
	t.Run("default", func(t *testing.T) {
		user := t.TempDir()
		env := cacheEnv(t, stdtest.Home(), "", fakeVersion)
		env.userCacheDir = func() (string, error) { return user, nil }
		build(t, env)
		hasEntries(t, filepath.Join(user, "tack"))
	})
}

// The toolchain is identified once per build, and only by a build that
// links std objects; one that cannot be identified ends the build.
func TestStdCacheToolchainIdentity(t *testing.T) {
	needClang(t)
	tmp := isolateTemp(t)
	env := cacheEnv(t, stdtest.Home(), t.TempDir(), fakeVersion)
	var calls [][]string
	env.toolVersion = func(_ context.Context, argv []string) ([]byte, error) {
		calls = append(calls, argv)
		return []byte(fakeVersion), nil
	}
	out := filepath.Join(t.TempDir(), "objs")
	if r := runTackEnv(t, t.Context(), env,
		"--backend", objsBackend, "--out", out, program(t, printExit)); r.code != exitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if want := [][]string{c.New().VersionCmd()}; !slices.EqualFunc(calls, want, slices.Equal) {
		t.Errorf("toolchain identified with %q, want %q", calls, want)
	}
	calls = nil
	if r := runTackEnv(t, t.Context(), env,
		"--backend", objsBackend, "--out", out, program(t, "")); r.code != exitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if len(calls) != 0 {
		t.Errorf("a program calling no builtin identified the toolchain: %q", calls)
	}

	env.toolVersion = func(context.Context, []string) ([]byte, error) {
		return nil, errors.New("clang --version failed: exit status 1")
	}
	r := runTackEnv(t, t.Context(), env, "--out", out, program(t, printExit))
	assertFailure(t, r, "cannot identify the toolchain: clang --version failed: exit status 1")
	assertEmptyDir(t, tmp)
}

// toolchainVersion returns the command's standard output, and a failure
// that names the command and carries what it wrote to stderr.
func TestToolchainVersion(t *testing.T) {
	out, err := toolchainVersion(t.Context(), []string{"sh", "-c", "echo v1; echo noise >&2"})
	if err != nil || string(out) != "v1\n" {
		t.Errorf("got %q, %v; want %q", out, err, "v1\n")
	}
	_, err = toolchainVersion(t.Context(), []string{"sh", "-c", "echo bad >&2; exit 4"})
	if err == nil || !strings.Contains(err.Error(), "sh -c") ||
		!strings.Contains(err.Error(), "exit status 4") || !strings.Contains(err.Error(), "bad") {
		t.Errorf("err = %v, want the command, its status and its stderr", err)
	}
	_, err = toolchainVersion(t.Context(), []string{"/nonexistent/tack-test-cc", "--version"})
	if err == nil || !strings.Contains(err.Error(), "/nonexistent/tack-test-cc --version failed") {
		t.Errorf("err = %v, want the missing command named", err)
	}
}

package porttest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// fsizeWarned is a program both compilers accept with 6 KiB of
// warnings, far past the file-size limit TestOutputWriteFailure runs
// under: thirty locals declared and never used.
var fsizeWarned = func() string {
	var b strings.Builder
	b.WriteString("package main\n\n// main declares locals it never uses.\nfunc main() {\n")
	for i := 1; i <= 30; i++ {
		b.WriteString("    unused_local_number_" + strconv.Itoa(i) + " := " + strconv.Itoa(i) + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}()

// fsizeExterns is fsizeWarned with twelve extern funcs, so the tack.h
// --emit-header writes for it, past 2 KiB, is cut short by the
// file-size limit while its warnings, on a pipe, are written in full.
var fsizeExterns = func() string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := 1; i <= 12; i++ {
		n := strconv.Itoa(i)
		b.WriteString("// ext_function_number_" + n + " is defined in C.\n")
		b.WriteString("extern func ext_function_number_" + n + "(x int64) int64\n\n")
	}
	b.WriteString("// main declares locals it never uses and calls one extern func.\nfunc main() {\n")
	for i := 1; i <= 30; i++ {
		b.WriteString("    unused_local_number_" + strconv.Itoa(i) + " := " + strconv.Itoa(i) + "\n")
	}
	b.WriteString("    if ext_function_number_1(1) > 5 {\n        exit(1)\n    }\n}\n")
	return b.String()
}()

// outputCase is one run of the compilers TestOutputWriteFailure makes:
// the program src as main.tck, the arguments args, and redirect, the
// sh redirections the run's standard output and standard error take,
// a file under the file-size limit for the one whose write is to fail;
// prefill says the file out is filled to the limit first, so that a
// write of a few bytes appended to it fails too; json says out holds a
// JSON dump, --ast's or --res's, whose white space the two compilers
// write differently (see the package doc, Stages), so only its length,
// the limit's, is compared.
type outputCase struct {
	name     string
	src      string
	args     []string
	redirect string
	prefill  bool
	json     bool
}

// outputCases are the cases of TestOutputWriteFailure, one for each
// output Go's driver reports a failed write of, with "failed to write
// output": its writeAll of a dump or of --backends to standard output
// (main.go's execute and check.go's writeCheckOutput) and its
// writeWarnings of a run's warnings to standard error (main.go); and,
// the last two, the write of tack.h by writeHeaders, which Go reports as
// cannot write and os.WriteFile's error, the warnings on a pipe. --emit
// to standard output is TestFileSizeLimit's compiler case. check.go's
// withDump, the dump of a --check --lex or --ast whose parse failed, has
// no case: the port declines --check with those dumps.
var outputCases = []outputCase{
	{"lex", fsizeWrite, []string{"--lex", "main.tck"}, "> out", false, false},
	{"ast", fsizeWrite, []string{"--ast", "main.tck"}, "> out", false, true},
	{"res", fsizeWrite, []string{"--res", "main.tck"}, "> out", false, true},
	{"check-emit", fsizeWrite, []string{"--check", "--emit", "main.tck"}, "> out", false, false},
	{"backends", fsizeWrite, []string{"--backends"}, ">> out", true, false},
	{"emit-warnings", fsizeWarned, []string{"--emit", "main.tck"}, "> out 2> err", false, false},
	{"res-warnings", fsizeWarned, []string{"--res", "main.tck"}, "> out 2> err", false, false},
	{"check-warnings", fsizeWarned, []string{"--check", "main.tck"}, "2> err", false, false},
	{"check-emit-warnings", fsizeWarned, []string{"--check", "--emit", "main.tck"}, "> /dev/null 2> err", false, false},
	{"emit-header-warnings", fsizeWarned, []string{"--emit-header", "main.tck"}, "2> err", false, false},
	{"check-emit-header-warnings", fsizeWarned, []string{"--check", "--emit-header", "main.tck"}, "2> err", false, false},
	{"emit-header-write", fsizeExterns, []string{"--emit-header", "main.tck"}, "", false, false},
	{"check-emit-header-write", fsizeExterns, []string{"--check", "--emit-header", "main.tck"}, "", false, false},
}

// outputFiles are the files a case's run may leave in its directory,
// which TestOutputWriteFailure compares: the redirected streams and the
// tack.h --emit-header writes.
var outputFiles = []string{"out", "err", "tack.h"}

// TestOutputWriteFailure runs each of outputCases with both compilers,
// each in a directory of its own, under sh's ulimit -f fsizeBlocks (see
// TestFileSizeLimit), so the write of the output the case redirects to
// a file fails with EFBIG part way, and checks the port ends as Go does:
// the same wait status, the same bytes on the standard streams left as
// pipes, and the same bytes in each of outputFiles, or both without it,
// but for a JSON dump's out, whose length alone is compared.
// The port writes its output through std/os's write_stdout and its
// warnings through write_stderr, which report the failure, and words it
// as Go does (per the owner's ruling, 2026-11-18). Go must fail, exit 1,
// so each case reaches the failure it is there for. A build's warnings
// have no case: the port writes them just before its first clang
// starts, where Go writes them before it builds anything, and under the
// limit the port declines the build (see the package doc, File-size
// limit, and TestBuildWarningsUnwritable). It runs only as TestPort
// does.
func TestOutputWriteFailure(t *testing.T) {
	ready(t)
	for _, c := range outputCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var runs [2]limitedRun
			var files [2]map[string][]byte
			for i, bin := range []string{env.goBin, env.portRun} {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(c.src), 0o600); err != nil {
					t.Fatal(err)
				}
				if c.prefill {
					// 1024 bytes: at the limit or past it, whether the
					// shell counts 512-byte blocks or kilobytes.
					fill := bytes.Repeat([]byte("x"), 1024)
					if err := os.WriteFile(filepath.Join(dir, "out"), fill, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				runs[i] = runLimited(t, dir, "sh",
					append([]string{"-c", `exec "$0" "$@" ` + c.redirect, bin}, c.args...)...)
				files[i] = readOutputFiles(t, dir)
			}
			goRun, portRun := runs[0], runs[1]
			if !goRun.status.Exited() || goRun.status.ExitStatus() != 1 {
				t.Fatalf("the Go compiler: %s, want exit 1, a failed write of its output", goRun)
			}
			if portRun.status != goRun.status || !bytes.Equal(portRun.stdout, goRun.stdout) ||
				!bytes.Equal(portRun.stderr, goRun.stderr) {
				t.Errorf("the port: %s, want the Go compiler's %s", portRun, goRun)
			}
			for _, name := range outputFiles {
				g, gok := files[0][name]
				p, pok := files[1][name]
				same := bytes.Equal(p, g)
				if c.json && name == "out" {
					same = len(p) == len(g)
				}
				if gok != pok || !same {
					t.Errorf("%s: the port left %s, the Go compiler %s",
						name, describeFile(p, pok), describeFile(g, gok))
				}
			}
		})
	}
}

// oldBinary is what TestBuildWarningsUnwritable puts at --out before an
// existing case's build.
const oldBinary = "not a binary\n"

// TestBuildWarningsUnwritable checks a build whose warnings cannot be
// written (see the package doc, File-size limit): --out of fsizeWarned,
// with a print, so the build needs the runtime's objects, with standard
// error a file opened for reading alone, so every write to
// it fails with EBADF, by each compiler in a directory of its own, with
// no file-size limit, which the port's build declines under (it cannot
// write its temporary files), and a build cache of its own. Go's
// compileWarned writes the warnings before it builds, and builds nothing
// once their write fails; the port writes them just before its first
// clang would start, and starts none once their write fails (per the
// owner's ruling, 2026-11-18). Each must exit 1, Go's report of the
// failed write lost on that same standard error, write nothing to
// standard output, and leave --out as it found it:
//
//   - fresh, nothing at --out before: nothing there after;
//   - existing, oldBinary at --out before, mode 0700: the same bytes and
//     mode after.
//
// The port's build cache must hold what Go's does, the same paths, or be
// missing as Go's is. It runs only as TestPort does.
func TestBuildWarningsUnwritable(t *testing.T) {
	ready(t)
	for _, existing := range []bool{false, true} {
		name := "fresh"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var caches [2][]string
			for i, c := range []struct{ name, bin string }{
				{"go", env.goBin}, {"port", env.portRun},
			} {
				dir := t.TempDir()
				// print makes the build need the runtime's objects, so a
				// build that went on would fill its cache.
				src := strings.Replace(fsizeWarned, "func main() {\n", "func main() {\n    print(\"built\\n\")\n", 1)
				if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "err"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				prog := filepath.Join(dir, "prog")
				if existing {
					if err := os.WriteFile(prog, []byte(oldBinary), 0o700); err != nil {
						t.Fatal(err)
					}
					// WriteFile's mode passes through the umask; this one
					// does not.
					if err := os.Chmod(prog, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				cache := filepath.Join(t.TempDir(), "cache")
				var stdout bytes.Buffer
				status := runStreamsIn(t, dir, cache, &stdout, io.Discard, "sh", "-c", `exec "$0" "$@" 2< err`,
					c.bin, "--out", "prog", "main.tck")
				if !status.Exited() || status.ExitStatus() != 1 || stdout.Len() != 0 {
					t.Errorf("the %s compiler: exit %d, standard output %q, want exit 1 and nothing written",
						c.name, status.ExitStatus(), stdout.String())
				}
				info, err := os.Stat(prog)
				switch {
				case !existing && !errors.Is(err, fs.ErrNotExist):
					t.Errorf("the %s compiler left a file at --out (%v), want none", c.name, err)
				case existing && err != nil:
					t.Errorf("the %s compiler: %v, want --out left as it was", c.name, err)
				case existing:
					got, rerr := os.ReadFile(prog)
					if rerr != nil || string(got) != oldBinary || info.Mode().Perm() != 0o700 {
						t.Errorf("the %s compiler changed --out: %s, mode %v (%v), want %q, mode 0700",
							c.name, describeFile(got, rerr == nil), info.Mode().Perm(), rerr, oldBinary)
					}
				}
				caches[i] = treeList(t, cache)
			}
			if !slices.Equal(caches[1], caches[0]) {
				t.Errorf("the port's build cache holds %q, the Go compiler's %q", caches[1], caches[0])
			}
		})
	}
}

// treeList returns the paths below root, relative to it, in order, or
// nil when there is no root.
func treeList(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		paths = append(paths, rel)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// TestTestWarningsUnwritable runs tack test of a package whose code has
// warnings, with standard error a file opened for reading alone, so the
// write of the warnings fails with EBADF, with both compilers, each in
// a module of its own. Go's runTest writes the warnings before it builds
// the tests and stops at the failure, exit 1 with nothing run; the port's
// build of the tests holds the failure's tack: line with its other ones
// (see build.tck's warnings_lost) and ends the same way, exit 1, running
// no test, so nothing reaches standard output. It runs only as TestPort
// does.
func TestTestWarningsUnwritable(t *testing.T) {
	ready(t)
	files := map[string]string{
		"tack.mod":     "module m\n",
		"p/p.tck":      "package p\n\n// z returns 26, with a local it never uses.\nfunc z() int64 {\n    unused_local := 1\n    return 26\n}\n",
		"p/p_test.tck": passingTest,
		"err":          "",
	}
	for _, c := range []struct{ name, bin string }{
		{"go", env.goBin}, {"port", env.portRun},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, data := range files {
				f := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout bytes.Buffer
			status := runStreams(t, dir, &stdout, io.Discard, "sh", "-c", `exec "$0" "$@" 2< err`,
				c.bin, "test", "p")
			if !status.Exited() || status.ExitStatus() != 1 || stdout.Len() != 0 {
				t.Errorf("exit %d, standard output %q, want exit 1 and nothing run",
					status.ExitStatus(), stdout.String())
			}
		})
	}
}

// TestEmitHeaderReadOnly runs --emit-header, with --check and without,
// of fsizeExterns, whose warnings go to a pipe, in a directory the
// compilers may not write in, mode 0555, so the open of tack.h fails
// with EACCES once the warnings are written, each compiler in a
// directory of its own. The port must end as Go does, byte for byte:
// the warnings, then cannot write tack.h: open tack.h: permission
// denied, exit 1, and no tack.h, the port wording the error from
// std/os's write_file_result. It is skipped for root, whom the mode does
// not stop. It runs only as TestPort does.
func TestEmitHeaderReadOnly(t *testing.T) {
	ready(t)
	if os.Geteuid() == 0 {
		t.Skip("root writes in a directory of mode 0555")
	}
	for name, args := range map[string][]string{
		"emit-header":       {"--emit-header", "main.tck"},
		"check-emit-header": {"--check", "--emit-header", "main.tck"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var runs [2]result
			for i, c := range []struct{ bin, cache string }{
				{env.goBin, env.goCache}, {env.portRun, env.portCache},
			} {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(fsizeExterns), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0o555); err != nil {
					t.Fatal(err)
				}
				// TempDir's cleanup removes the directory, which needs it
				// writable again.
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
				runs[i] = runCompiler(t, c.bin, c.cache, input{name: "main.tck", dir: dir, file: "main.tck"}, args)
				if _, err := os.Stat(filepath.Join(dir, "tack.h")); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("a tack.h is there (%v), want none", err)
				}
			}
			want, got := runs[0], runs[1]
			if want.code != 1 || !bytes.Contains(want.stderr, []byte("open tack.h: permission denied")) {
				t.Fatalf("the Go compiler exited %d, writing\n%s\nwant exit 1 and permission denied",
					want.code, want.stderr)
			}
			if got.code != want.code || !bytes.Equal(got.stdout, want.stdout) ||
				!bytes.Equal(got.stderr, want.stderr) {
				t.Errorf("the port exited %d, writing\n%s\nGo exited %d, writing\n%s",
					got.code, got.stderr, want.code, want.stderr)
			}
		})
	}
}

// readOutputFiles returns the bytes of each of outputFiles in dir, by
// name, leaving out each that is not there.
func readOutputFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	for _, name := range outputFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got[name] = b
	}
	return got
}

// describeFile describes a file of outputFiles for a failure: missing,
// or how many bytes it holds and the last of them.
func describeFile(b []byte, ok bool) string {
	if !ok {
		return "no file"
	}
	tail := b
	if len(tail) > 80 {
		tail = tail[len(tail)-80:]
	}
	return strconv.Itoa(len(b)) + " bytes ending " + strconv.Quote(string(tail))
}

// TestOutputBrokenPipe runs --emit with both compilers, its standard
// output a pipe whose read end is closed before the compiler starts, and
// --emit of fsizeWarned with its standard error such a pipe, the other
// stream a pipe read into the result, which must stay empty: Go dies
// writing the warnings, before any C. Go's runtime dies of SIGPIPE on a
// write to
// a broken standard output or standard error; the port's runtime
// ignores SIGPIPE and ends the process as exit(141) does on such a
// write, which std/os's write_stdout and write_stderr do too (per the
// owner's ruling, 2026-11-18): 141, the status a shell reports for a
// process SIGPIPE killed. Neither writes anything to the other stream.
// It runs only as TestPort does.
func TestOutputBrokenPipe(t *testing.T) {
	ready(t)
	for _, c := range []struct {
		name string
		src  string
		fd   int
	}{
		{"stdout", fsizeWrite, 1},
		{"stderr", fsizeWarned, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, cc := range []struct{ name, bin string }{
				{"go", env.goBin}, {"port", env.portRun},
			} {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(c.src), 0o600); err != nil {
					t.Fatal(err)
				}
				r := runBroken(t, dir, c.fd, cc.bin, "--emit", "main.tck")
				if len(r.stdout) != 0 || len(r.stderr) != 0 {
					t.Errorf("the %s compiler: %s, want nothing on the other stream", cc.name, r)
				}
				if cc.name == "go" {
					if !r.status.Signaled() || r.status.Signal() != syscall.SIGPIPE {
						t.Errorf("the Go compiler: %s, want death by SIGPIPE", r)
					}
				} else if !r.status.Exited() || r.status.ExitStatus() != 141 {
					t.Errorf("the port: %s, want exit 141", r)
				}
			}
		})
	}
}

// stdWriteLimit is the program TestStdWrite builds to write past the
// file-size limit through std/os: given out, 64 KiB through
// write_stdout, reporting its error on standard error; given err, the
// same through write_stderr, reporting on standard output.
const stdWriteLimit = `package main

import fmt from "std/fmt"
import os from "std/os"

// main writes 64 KiB through write_stdout, given out, or write_stderr,
// given err, and reports the error's code and message on the other
// stream.
func main() {
    a, ae := os.args(c_allocator)
    if ae.code != 0 {
        print("out of memory\n")
        return
    }
    mut out := false
    for i, s := range a {
        if i == 1 && s.equal([]uint8("out")) {
            out = true
        }
    }
    mut data := make(list[uint8], c_allocator)
    mut i int64 = 0
    for i < 65536 {
        e := append(mut data, 120)
        if e.code != 0 {
            print("out of memory\n")
            return
        }
        i += 1
    }
    if out {
        e := os.write_stdout(data)
        fmt.eprintf("%d %s\n", e.code, e.msg)
        return
    }
    e := os.write_stderr(data)
    fmt.printf("%d %s\n", e.code, e.msg)
}
`

// stdWriteOrder is the program TestStdWrite builds to interleave
// write_stdout and write_stderr with print, std/fmt's write, ewrite and
// eprintf, and with empty writes, a string's empty view and an empty
// list's, each of which must be no error.
const stdWriteOrder = `package main

import fmt from "std/fmt"
import os from "std/os"

// check prints what failed when e is an error.
func check(e error, what string) {
    if e.code != 0 {
        fmt.printf("[%s failed: %d %s]", what, e.code, e.msg)
    }
}

// main writes a to j, in order, through the streams' writers.
func main() {
    empty := make(list[uint8], c_allocator)
    print("a")
    check(os.write_stdout([]uint8("b")), "write_stdout")
    fmt.write([]uint8("c"))
    check(os.write_stderr([]uint8("d")), "write_stderr")
    fmt.ewrite([]uint8("e"))
    print("f")
    check(os.write_stdout([]uint8("")), "empty write_stdout")
    check(os.write_stderr(empty), "empty write_stderr")
    check(os.write_stdout(empty), "empty list write_stdout")
    check(os.write_stderr([]uint8("")), "empty string write_stderr")
    check(os.write_stdout([]uint8("g")), "write_stdout")
    fmt.eprintf("h")
    check(os.write_stderr([]uint8("i")), "write_stderr")
    print("j\n")
}
`

// stdWritePipe is the program TestStdWrite builds to write to a broken
// pipe: given out, through write_stdout; given err, through
// write_stderr; given flush, through write_stderr after print has left
// bytes in C's stdout buffer, which write_stderr flushes first. After
// each it says the write returned, on the stream that is no broken pipe.
const stdWritePipe = `package main

import fmt from "std/fmt"
import os from "std/os"

// main writes to the broken pipe its argument names, then says it
// returned.
func main() {
    a, ae := os.args(c_allocator)
    if ae.code != 0 {
        return
    }
    mut which := 0
    for i, s := range a {
        if i == 1 && s.equal([]uint8("out")) {
            which = 1
        }
        if i == 1 && s.equal([]uint8("err")) {
            which = 2
        }
    }
    if which == 1 {
        e := os.write_stdout([]uint8("x"))
        fmt.eprintf("returned %d %s\n", e.code, e.msg)
        return
    }
    if which == 2 {
        e := os.write_stderr([]uint8("x"))
        fmt.printf("returned %d %s\n", e.code, e.msg)
        return
    }
    print("buffered")
    e := os.write_stderr([]uint8("x"))
    fmt.eprintf("returned %d %s\n", e.code, e.msg)
}
`

// TestStdWrite checks std/os's write_stdout and write_stderr, port-only
// std (per the owner's ruling, 2026-11-18), with programs built by the
// port and by the reference Go compiler, which compiles the port's std:
//
//   - limit: stdWriteLimit under the file-size limit (see
//     TestFileSizeLimit), the stream it writes a file, reports
//     error{code: 3, msg: "File too large"} and exits 0, having
//     written some of the bytes but not all;
//   - order: stdWriteOrder, both streams one pipe, writes a to j in
//     order, every empty write no error;
//   - pipe: stdWritePipe, the stream it writes a pipe whose read end is
//     closed, exits 141, the runtime's rule for a broken pipe on
//     standard output or standard error, without returning: nothing is
//     written to the other stream.
//
// It runs only as TestPort does.
func TestStdWrite(t *testing.T) {
	ready(t)
	compilers := []struct{ name, bin, cache string }{
		{"port", env.portRun, env.portCache},
		{"go", env.goBin, env.goCache},
	}
	t.Run("limit", func(t *testing.T) {
		t.Parallel()
		for _, c := range compilers {
			dir := fsizeBuild(t, c.bin, c.cache, stdWriteLimit, nil)
			for _, s := range []struct{ arg, redirect string }{
				{"out", "> big"}, {"err", "2> big"},
			} {
				r := runLimited(t, dir, "sh", "-c", `exec "$0" "$@" `+s.redirect,
					filepath.Join(dir, "prog"), s.arg)
				want := "3 File too large\n"
				if !r.status.Exited() || r.status.ExitStatus() != 0 ||
					string(r.stdout)+string(r.stderr) != want {
					t.Errorf("built by the %s, %s: %s, want exit 0 and %q on the other stream",
						c.name, s.arg, r, want)
				}
				info, err := os.Stat(filepath.Join(dir, "big"))
				if err != nil {
					t.Fatalf("built by the %s: %v", c.name, err)
				}
				if info.Size() <= 0 || info.Size() >= 65536 {
					t.Errorf("built by the %s, %s: big holds %d bytes, want some of the 65536 but not all",
						c.name, s.arg, info.Size())
				}
			}
		}
	})
	t.Run("order", func(t *testing.T) {
		t.Parallel()
		for _, c := range compilers {
			dir := fsizeBuild(t, c.bin, c.cache, stdWriteOrder, nil)
			var both bytes.Buffer
			status := runStreams(t, dir, &both, &both, filepath.Join(dir, "prog"))
			if !status.Exited() || status.ExitStatus() != 0 || both.String() != "abcdefghij\n" {
				t.Errorf("built by the %s: exit %d, both streams %q, want exit 0 and %q",
					c.name, status.ExitStatus(), both.String(), "abcdefghij\n")
			}
		}
	})
	t.Run("pipe", func(t *testing.T) {
		t.Parallel()
		for _, c := range compilers {
			dir := fsizeBuild(t, c.bin, c.cache, stdWritePipe, nil)
			for _, s := range []struct {
				arg string
				fd  int
			}{
				{"out", 1}, {"err", 2}, {"flush", 1},
			} {
				r := runBroken(t, dir, s.fd, filepath.Join(dir, "prog"), s.arg)
				if !r.status.Exited() || r.status.ExitStatus() != 141 ||
					len(r.stdout) != 0 || len(r.stderr) != 0 {
					t.Errorf("built by the %s, %s: %s, want exit 141 and nothing on the other stream",
						c.name, s.arg, r)
				}
			}
		}
	})
}

// runStreams runs prog with args in dir, in the compilers' environment
// (see compilerEnv), its standard output going to stdout and its
// standard error to stderr, and returns its wait status. A run past
// fsizeTimeout, or one that cannot be started, fails t.
func runStreams(t *testing.T, dir string, stdout, stderr io.Writer, prog string, args ...string) syscall.WaitStatus {
	t.Helper()
	return runStreamsIn(t, dir, filepath.Join(t.TempDir(), "cache"), stdout, stderr, prog, args...)
}

// runStreamsIn is runStreams with cache the compilers' TACK_CACHE.
func runStreamsIn(t *testing.T, dir, cache string, stdout, stderr io.Writer, prog string, args ...string) syscall.WaitStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), fsizeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	cmd.Env = compilerEnv(cache)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	outlived, err := runInGroup(cmd)
	running := "running " + prog + " " + strings.Join(args, " ") + " in " + dir
	failEnd(t, endOf(ctx.Err(), err, outlived, true), running, cmd, err, outlived, fsizeTimeout, nil)
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("%s: no wait status", running)
	}
	return status
}

// runBroken runs prog with args in dir as runStreams does, the
// descriptor fd, 1 or 2, a pipe whose read end is closed before prog
// starts, so every write to it fails with EPIPE, or raises SIGPIPE, and
// the other stream a pipe read into the result.
func runBroken(t *testing.T, dir string, fd int, prog string, args ...string) limitedRun {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var stdout, stderr bytes.Buffer
	var out, errOut io.Writer = &stdout, &stderr
	if fd == 1 {
		out = w
	} else {
		errOut = w
	}
	status := runStreams(t, dir, out, errOut, prog, args...)
	return limitedRun{stdout: stdout.Bytes(), stderr: stderr.Bytes(), status: status}
}

// nonblockSize is how many bytes TestNonblockingStdout's program
// writes: past 64 KiB, Linux's default pipe buffer, many times over, so
// its writes fill the pipe, are cut short and then refused with EAGAIN
// while the reader waits.
const nonblockSize = 300000

// stdWriteMany is the program TestNonblockingStdout builds: it writes
// nonblockSize bytes, byte i being i % 251, a pattern no shifted or
// repeated run of it matches, through write_stdout, reporting an error
// on standard error, or, given err, through write_stderr, reporting on
// standard output.
var stdWriteMany = `package main

import fmt from "std/fmt"
import os from "std/os"

// main writes the pattern through write_stdout, or write_stderr given
// err.
func main() {
    a, ae := os.args(c_allocator)
    if ae.code != 0 {
        print("out of memory\n")
        return
    }
    mut to_err := false
    for i, s := range a {
        if i == 1 && s.equal([]uint8("err")) {
            to_err = true
        }
    }
    mut data := make(list[uint8], c_allocator)
    mut i int64 = 0
    for i < ` + strconv.Itoa(nonblockSize) + ` {
        b, fits := uint8(i % 251)
        if !fits {
            print("internal error\n")
            return
        }
        e := append(mut data, b)
        if e.code != 0 {
            print("out of memory\n")
            return
        }
        i += 1
    }
    if to_err {
        e := os.write_stderr(data)
        if e.code != 0 {
            fmt.printf("%d %s\n", e.code, e.msg)
        }
        return
    }
    e := os.write_stdout(data)
    if e.code != 0 {
        fmt.eprintf("%d %s\n", e.code, e.msg)
    }
}
`

// fsizeBig is a program both compilers accept with no warning whose C,
// past 100 KiB, fills a pipe many times over: six hundred functions,
// each called.
var fsizeBig = func() string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := 1; i <= 600; i++ {
		n := strconv.Itoa(i)
		b.WriteString("// f" + n + " returns x plus " + n + ".\nfunc f" + n +
			"(x int64) int64 {\n    return x +% " + n + "\n}\n\n")
	}
	b.WriteString("// main calls every function.\nfunc main() {\n    mut s int64 = 0\n")
	for i := 1; i <= 600; i++ {
		b.WriteString("    s = f" + strconv.Itoa(i) + "(s)\n")
	}
	b.WriteString("    if s < 0 {\n        exit(1)\n    }\n}\n")
	return b.String()
}()

// TestNonblockingStdout runs with standard output, or standard error,
// the write end of a pipe set to O_NONBLOCK, whose reader drains it only
// once it is full (see runNonblocking), so a write fills the pipe, is
// cut short, and the next is refused with EAGAIN; each run must have
// seen the pipe full. write_stdout and write_stderr write every byte:
// they wait for the descriptor to take more, poll(2) for POLLOUT, and
// write on, as Go's internal/poll waits for a non-blocking standard
// stream (per the owner's ruling, 2026-11-18). It checks:
//
//   - std: stdWriteMany built by the port and by the reference writes
//     every byte of its pattern, in order, through write_stdout, and,
//     given err, through write_stderr, and exits 0 with nothing on the
//     other stream;
//   - emit: --emit of fsizeBig by the port writes the C the Go compiler
//     writes, byte for byte, both exiting 0 with nothing on standard
//     error.
//
// EINTR has no case: no Tack program can install a signal handler, and
// without one Linux restarts a write a signal interrupts, so EINTR does
// not reach write_std. It runs only as TestPort does.
func TestNonblockingStdout(t *testing.T) {
	ready(t)
	t.Run("std", func(t *testing.T) {
		t.Parallel()
		want := make([]byte, nonblockSize)
		for i := range want {
			want[i] = byte(i % 251)
		}
		for _, c := range []struct{ name, bin, cache string }{
			{"port", env.portRun, env.portCache},
			{"go", env.goBin, env.goCache},
		} {
			dir := fsizeBuild(t, c.bin, c.cache, stdWriteMany, nil)
			for _, s := range []struct {
				arg string
				fd  int
			}{
				{"out", 1}, {"err", 2},
			} {
				r, full := runNonblocking(t, dir, s.fd, filepath.Join(dir, "prog"), s.arg)
				carried, other := r.stdout, r.stderr
				if s.fd == 2 {
					carried, other = r.stderr, r.stdout
				}
				if !full {
					t.Errorf("built by the %s, %s: the pipe was never full, so no write met EAGAIN",
						c.name, s.arg)
				}
				if !r.status.Exited() || r.status.ExitStatus() != 0 || len(other) != 0 {
					t.Errorf("built by the %s, %s: exit %d, the other stream %q, want exit 0 and nothing",
						c.name, s.arg, r.status.ExitStatus(), other)
				}
				if !bytes.Equal(carried, want) {
					t.Errorf("built by the %s, %s: %d bytes arrived, want the %d of the pattern in order%s",
						c.name, s.arg, len(carried), len(want), byteDiff(carried, want))
				}
			}
		}
	})
	t.Run("emit", func(t *testing.T) {
		t.Parallel()
		var runs [2]limitedRun
		for i, bin := range []string{env.goBin, env.portRun} {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(fsizeBig), 0o600); err != nil {
				t.Fatal(err)
			}
			r, full := runNonblocking(t, dir, 1, bin, "--emit", "main.tck")
			if !full {
				t.Errorf("%s: the pipe was never full, so no write met EAGAIN", bin)
			}
			runs[i] = r
		}
		goRun, portRun := runs[0], runs[1]
		if !goRun.status.Exited() || goRun.status.ExitStatus() != 0 || len(goRun.stdout) <= 65536 {
			t.Fatalf("the Go compiler: exit %d, %d bytes, standard error %q, want exit 0 and past 64 KiB",
				goRun.status.ExitStatus(), len(goRun.stdout), goRun.stderr)
		}
		if portRun.status != goRun.status || !bytes.Equal(portRun.stderr, goRun.stderr) ||
			!bytes.Equal(portRun.stdout, goRun.stdout) {
			t.Errorf("the port: exit %d, %d bytes, standard error %q; the Go compiler: exit %d, %d bytes%s",
				portRun.status.ExitStatus(), len(portRun.stdout), portRun.stderr,
				goRun.status.ExitStatus(), len(goRun.stdout), byteDiff(portRun.stdout, goRun.stdout))
		}
	})
}

// byteDiff describes where got first differs from want, for a failure,
// or returns an empty string when it does not.
func byteDiff(got, want []byte) string {
	for i := range min(len(got), len(want)) {
		if got[i] != want[i] {
			return ", the first difference at byte " + strconv.Itoa(i)
		}
	}
	if len(got) != len(want) {
		return ", one a prefix of the other"
	}
	return ""
}

// runNonblocking runs prog with args in dir, in the compilers'
// environment, the descriptor fd, 1 or 2, the write end of a pipe set to
// O_NONBLOCK, and the other stream a pipe read into the result, and
// returns how it ended, the bytes the non-blocking pipe carried in the
// stream's place, and whether the pipe was seen full: its reader waits,
// reading nothing, until the bytes pending in it, TIOCINQ, reach its
// capacity, F_GETPIPE_SZ, or the child ends, and only then drains it. A
// writer with bytes left that finds the pipe full is refused with
// EAGAIN, so a run that saw it full, and wrote more than it holds, met
// EAGAIN. A run past fsizeTimeout, one that cannot be started, and a
// write end that lost O_NONBLOCK when the child was started fail t.
func runNonblocking(t *testing.T, dir string, fd int, prog string, args ...string) (limitedRun, bool) {
	t.Helper()
	// The pipe is made blocking and wrapped as files Go's runtime does not
	// poll, so Start, which puts a polled file in blocking mode through
	// its Fd as it hands it to the child, leaves the write end's
	// O_NONBLOCK, set on the raw descriptor after it is wrapped, alone.
	// The flag is the open file's, so the child's copy has it.
	var fds [2]int
	if err := syscall.Pipe2(fds[:], syscall.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	r := os.NewFile(uintptr(fds[0]), "nonblock-r")
	w := os.NewFile(uintptr(fds[1]), "nonblock-w")
	defer r.Close()
	if err := syscall.SetNonblock(fds[1], true); err != nil {
		w.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), fsizeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	cmd.Env = compilerEnv(filepath.Join(t.TempDir(), "cache"))
	var other bytes.Buffer
	if fd == 1 {
		cmd.Stdout, cmd.Stderr = w, &other
	} else {
		cmd.Stdout, cmd.Stderr = &other, w
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = processWaitDelay
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := cmd.Start(); err != nil {
		w.Close()
		t.Fatalf("starting %s: %v", prog, err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fds[1]), syscall.F_GETFL, 0)
	if errno != 0 || flags&syscall.O_NONBLOCK == 0 {
		w.Close()
		t.Fatalf("the pipe's write end is not non-blocking once %s is started (flags %#x, %v): "+
			"the test would not reach EAGAIN", prog, flags, errno)
	}
	size, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fds[0]), syscall.F_GETPIPE_SZ, 0)
	if errno != 0 {
		w.Close()
		t.Fatalf("F_GETPIPE_SZ: %v", errno)
	}
	// Only the child holds the write end now, so the reader sees the end
	// of the output when the child exits.
	w.Close()
	type read struct {
		b    []byte
		full bool
	}
	exited := make(chan struct{})
	got := make(chan read, 1)
	go func() {
		full := false
	wait:
		for {
			var pending int32
			_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fds[0]), syscall.TIOCINQ,
				uintptr(unsafe.Pointer(&pending)))
			if e == 0 && uintptr(pending) >= size {
				full = true
				break
			}
			select {
			case <-exited:
				break wait
			case <-time.After(5 * time.Millisecond):
			}
		}
		b, _ := io.ReadAll(r)
		got <- read{b, full}
	}()
	werr := cmd.Wait()
	close(exited)
	var exit *exec.ExitError
	if werr != nil && !errors.As(werr, &exit) {
		t.Fatalf("running %s in %s: %v\nother stream:\n%s", prog, dir, werr, other.Bytes())
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("running %s: no wait status", prog)
	}
	rd := <-got
	if fd == 1 {
		return limitedRun{stdout: rd.b, stderr: other.Bytes(), status: status}, rd.full
	}
	return limitedRun{stdout: other.Bytes(), stderr: rd.b, status: status}, rd.full
}

// stdWriteFileResult is the program TestWriteFileResult builds: it
// writes, with write_file_result, as its argument says: ok, five bytes h
// to the file f; open, to missing/f, below a directory that is not there;
// write, 64 KiB to big, past the file-size limit it is run under. It
// prints the result's code and op, and its message for an error.
const stdWriteFileResult = `package main

import fmt from "std/fmt"
import os from "std/os"

// main writes as its argument says and prints the result.
func main() {
    a, ae := os.args(c_allocator)
    if ae.code != 0 {
        print("out of memory\n")
        return
    }
    mut which := 0
    for i, s := range a {
        if i == 1 && s.equal([]uint8("open")) {
            which = 1
        }
        if i == 1 && s.equal([]uint8("write")) {
            which = 2
        }
    }
    mut data := make(list[uint8], c_allocator)
    mut path := "f"
    mut n int64 = 5
    if which == 1 {
        path = "missing/f"
    }
    if which == 2 {
        path = "big"
        n = 65536
    }
    mut i int64 = 0
    for i < n {
        e := append(mut data, 104)
        if e.code != 0 {
            print("out of memory\n")
            return
        }
        i += 1
    }
    r := os.write_file_result([]uint8(path), data, c_allocator)
    if r.err.code == 0 {
        fmt.printf("code=0 op=%s\n", r.op.bytes())
        return
    }
    fmt.printf("code=%d op=%s msg=%s\n", r.err.code, r.op.bytes(), r.err.msg)
}
`

// TestWriteFileResult checks std/os's write_file_result, port-only std
// (per the owner's ruling, 2026-11-18), with stdWriteFileResult built by
// the port and by the reference: a write that succeeds is code 0 with no
// op, and the file holds the bytes; one whose open fails, ENOENT, is
// code 3, op open, "No such file or directory"; and one whose write
// fails, past the file-size limit (see TestFileSizeLimit), is code 3,
// op write, "File too large". It runs only as TestPort does.
func TestWriteFileResult(t *testing.T) {
	ready(t)
	for _, c := range []struct{ name, bin, cache string }{
		{"port", env.portRun, env.portCache},
		{"go", env.goBin, env.goCache},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := fsizeBuild(t, c.bin, c.cache, stdWriteFileResult, nil)
			prog := filepath.Join(dir, "prog")
			for _, s := range []struct {
				arg, want string
				limited   bool
			}{
				{"ok", "code=0 op=\n", false},
				{"open", "code=3 op=open msg=No such file or directory\n", false},
				{"write", "code=3 op=write msg=File too large\n", true},
			} {
				var r limitedRun
				if s.limited {
					r = runLimited(t, dir, prog, s.arg)
				} else {
					var stdout, stderr bytes.Buffer
					r = limitedRun{status: runStreams(t, dir, &stdout, &stderr, prog, s.arg)}
					r.stdout, r.stderr = stdout.Bytes(), stderr.Bytes()
				}
				if !r.status.Exited() || r.status.ExitStatus() != 0 || string(r.stdout) != s.want ||
					len(r.stderr) != 0 {
					t.Errorf("%s: %s, want exit 0 and the standard output %q", s.arg, r, s.want)
				}
			}
			got, err := os.ReadFile(filepath.Join(dir, "f"))
			if err != nil || string(got) != "hhhhh" {
				t.Errorf("f holds %q (%v), want %q", got, err, "hhhhh")
			}
		})
	}
}

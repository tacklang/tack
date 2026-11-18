package porttest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// portRuntimeDir is where the port keeps the originals of the files the
// compiler carries inside it, relative to its source tree's root: the
// builtins' documentation, builtin.tck, the runtime header, tack/tack.h,
// and the runtime's C files.
const portRuntimeDir = "runtime"

// portFileDivergences lists the paths, relative to the port's source
// tree, of the files of its std and runtime allowed to differ from this
// module's frozen copies, each with the reason it may. An entry whose
// file is the same on both sides, or on neither, fails TestPortFiles,
// so the list cannot outlive its reasons. The comparisons run both
// compilers with the port's std and runtime, not the frozen copies (see
// buildReference), so a file listed here is compared as the port has it.
var portFileDivergences = map[string]string{
	portRuntimeDir + "/process.c": "the runtime ignores SIGXFSZ, as it does SIGPIPE, so a write " +
		"past RLIMIT_FSIZE fails with EFBIG rather than kill the process, per the owner's " +
		"ruling, 2026-11-15",
	portRuntimeDir + "/tack/tack.h": "tack_start_'s comment says it ignores SIGXFSZ too, per " +
		"the owner's ruling, 2026-11-15",
	"src/std/include/tack/tack.h": "std's copy of the runtime header, which says tack_start_ " +
		"ignores SIGXFSZ too, per the owner's ruling, 2026-11-15",
	"src/std/os/os.c": "a child std/os starts gets SIGXFSZ's default action, as it gets " +
		"SIGPIPE's, since the runtime ignores SIGXFSZ, per the owner's ruling, 2026-11-15",
	"src/std/os/os.tck": "its docs say a write past RLIMIT_FSIZE is File too large and a " +
		"child starts with SIGXFSZ's default action, per the owner's ruling, 2026-11-15",
}

// portOnlyFiles lists the paths, relative to the port's source tree,
// of the files of its runtime directory that have no frozen copy here,
// each with the reason. Each must be in the port, as a regular file.
var portOnlyFiles = map[string]string{
	portRuntimeDir + "/tack.mod": "it makes runtime/ a module root only so that a library check " +
		"of the port skips builtin.tck, which is documentation and no code",
}

// portFile is one file of the port's std or runtime and the frozen copy
// it is compared with, each a path from its tree's root, with slashes:
// port the port's, frozen this module's.
type portFile struct {
	port, frozen string
}

// TestPortFiles checks the port's std and runtime against this module's
// frozen copies. The port owns std and the runtime (per the owner's
// ruling, 2026-11-18), and the comparisons run both compilers with the
// port's (see buildReference and compilerEnv), so the frozen copies are
// what the frozen compiler was frozen with, and a difference from them
// must be one portFileDivergences names, with its reason. It checks
// that the port's src/std holds the same files as this module's, by
// path, and that its runtime directory, portRuntimeDir, holds the same
// files as builtin/'s
// builtin.tck and builtin/runtime, builtin.tck at its root and every
// other at the same path below, each with the same bytes and the same
// executable bit; but for the paths portFileDivergences lists, which
// must differ, and those portOnlyFiles lists, which must be in the port
// alone. It also checks that the port's std copies of builtin.tck and
// tack.h, src/std/builtin/builtin.tck and src/std/include/tack/tack.h,
// are its runtime's, which the port's make std-copies writes, as this
// module's builtin test checks its own. It needs the port's source
// tree, env.home, which TestMain requires whatever TACK_PORT_BIN says.
// It runs only as TestPort does, with TACK_PORT_TEST=1 and -count.
func TestPortFiles(t *testing.T) {
	ready(t)
	src := env.home
	if !dirExists(filepath.Join(src, portRuntimeDir)) {
		t.Fatalf("the port's source tree, %s, has no %s directory", src, portRuntimeDir)
	}
	files, err := portFiles(src, env.root, portOnlyFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range comparePortFiles(src, env.root, files, portFileDivergences) {
		t.Error(msg)
	}
	for _, msg := range checkPortOnly(src, portOnlyFiles) {
		t.Error(msg)
	}
	for _, c := range []portFile{
		{port: "src/std/builtin/builtin.tck", frozen: portRuntimeDir + "/builtin.tck"},
		{port: "src/std/include/tack/tack.h", frozen: portRuntimeDir + "/tack/tack.h"},
	} {
		if msg := compareFile(src, src, c, "original", "copy"); msg != "" {
			t.Errorf("std's copy, %s, differs from the port's original, %s; run make std-copies in the port: %s",
				c.port, c.frozen, msg)
		}
	}
}

// portFiles returns every file of the port's std and runtime in
// portRoot, the port's source tree, with the frozen copy in frozenRoot,
// this module's root in TestPortFiles, it is compared with, sorted by
// the port's path: each regular file found below src/std in either
// tree, at the same path in both; and each regular file found below
// portRuntimeDir in the port, or in this module as builtin/builtin.tck
// or below builtin/runtime, portRuntimeDir/builtin.tck paired with
// builtin/builtin.tck and every other portRuntimeDir/<p> with
// builtin/runtime/<p>; but for the port's files portOnly lists. A file
// one tree has and the other has not is listed too, and comparing it
// fails. Any entry below either directory that is no regular file or
// directory, a symbolic link among them, is an error.
func portFiles(portRoot, frozenRoot string, portOnly map[string]string) ([]portFile, error) {
	paths := map[string]string{}
	for _, root := range []string{portRoot, frozenRoot} {
		std, err := treeFiles(root, "src/std")
		if err != nil {
			return nil, err
		}
		for _, p := range std {
			paths[p] = p
		}
	}
	if err := addRuntimePairs(paths, portRoot, frozenRoot, portOnly); err != nil {
		return nil, err
	}
	out := make([]portFile, 0, len(paths))
	for port, frozen := range paths {
		out = append(out, portFile{port: port, frozen: frozen})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].port < out[j].port })
	return out, nil
}

// addRuntimePairs adds to paths, keyed by the port's path, the frozen
// copy of each file of the runtime, as portFiles describes: each regular
// file below portRuntimeDir in portRoot but those portOnly lists, each
// below builtin/runtime in frozenRoot, and builtin.tck.
func addRuntimePairs(paths map[string]string, portRoot, frozenRoot string, portOnly map[string]string) error {
	const frozenDoc, frozenRuntime = "builtin/builtin.tck", "builtin/runtime/"
	portDoc := portRuntimeDir + "/builtin.tck"
	runtime, err := treeFiles(portRoot, portRuntimeDir)
	if err != nil {
		return err
	}
	for _, p := range runtime {
		switch {
		case portOnly[p] != "":
		case p == portDoc:
			paths[p] = frozenDoc
		default:
			paths[p] = frozenRuntime + strings.TrimPrefix(p, portRuntimeDir+"/")
		}
	}
	frozen, err := treeFiles(frozenRoot, strings.TrimSuffix(frozenRuntime, "/"))
	if err != nil {
		return err
	}
	for _, p := range frozen {
		paths[portRuntimeDir+"/"+strings.TrimPrefix(p, frozenRuntime)] = p
	}
	paths[portDoc] = frozenDoc
	return nil
}

// checkPortOnly returns a message for each path portOnly lists that is
// not a regular file in portRoot, the port's source tree, sorted, so the
// list cannot outlive its files.
func checkPortOnly(portRoot string, portOnly map[string]string) []string {
	paths := make([]string, 0, len(portOnly))
	for p := range portOnly {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var msgs []string
	for _, p := range paths {
		if _, _, err := readFileMode(filepath.Join(portRoot, filepath.FromSlash(p))); err != nil {
			msgs = append(msgs, fmt.Sprintf("%s is listed in portOnlyFiles, but: %v", p, err))
		}
	}
	return msgs
}

// treeFiles returns the path, from root and with slashes, of every
// regular file below dir, a directory of root given with slashes. Any
// other kind of entry but a directory, a symbolic link among them, is an
// error, since neither tree should hold one there.
func treeFiles(root, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("naming %s: %w", p, err)
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s of %s: %w", dir, root, err)
	}
	return out, nil
}

// comparePortFiles compares each of files, the port's file in the tree
// portRoot with its frozen copy in frozenRoot, and returns a message for
// each failure, in files' order: a file that differs and is not in
// allowed, the paths allowed to differ, each with its reason; and an
// entry of allowed whose file is the same in both trees, or that names
// no file of files, which says the list is stale.
func comparePortFiles(portRoot, frozenRoot string, files []portFile, allowed map[string]string) []string {
	var msgs []string
	listed := map[string]bool{}
	for _, f := range files {
		listed[f.port] = true
		msg := compareFile(portRoot, frozenRoot, f, "frozen", "port")
		_, ok := allowed[f.port]
		switch {
		case msg != "" && !ok:
			msgs = append(msgs, fmt.Sprintf("%s differs from this module's frozen %s: %s", f.port, f.frozen, msg))
		case msg == "" && ok:
			msgs = append(msgs, fmt.Sprintf("%s is allowed to differ from %s, but does not: drop it from portFileDivergences",
				f.port, f.frozen))
		}
	}
	stale := make([]string, 0, len(allowed))
	for p := range allowed {
		if !listed[p] {
			stale = append(stale, p)
		}
	}
	sort.Strings(stale)
	for _, p := range stale {
		msgs = append(msgs, fmt.Sprintf("%s is allowed to differ, but neither tree has it: drop it from portFileDivergences", p))
	}
	return msgs
}

// compareFile returns "" when f's file in portRoot and its copy in
// frozenRoot are both regular files with the same bytes and the same
// executable bit, and otherwise what differs: a file missing from one
// tree, the bit, or the first line that differs, as firstDiffLabeled
// words it, the copy's line labeled frozenLabel and the port's
// portLabel.
func compareFile(portRoot, frozenRoot string, f portFile, frozenLabel, portLabel string) string {
	port, portMode, portErr := readFileMode(filepath.Join(portRoot, filepath.FromSlash(f.port)))
	frozen, frozenMode, frozenErr := readFileMode(filepath.Join(frozenRoot, filepath.FromSlash(f.frozen)))
	switch {
	case portErr != nil && frozenErr != nil:
		return fmt.Sprintf("neither can be read: %v; %v", portErr, frozenErr)
	case portErr != nil:
		return portErr.Error()
	case frozenErr != nil:
		return frozenErr.Error()
	case portMode&0o111 != frozenMode&0o111:
		return fmt.Sprintf("its mode is %v, the %s's %v", portMode, frozenLabel, frozenMode)
	case !bytes.Equal(port, frozen):
		return firstDiffLabeled(frozenLabel, portLabel, frozen, port)
	}
	return ""
}

// readFileMode returns the bytes and the permission bits of the regular
// file at p. A missing file, or one that is not regular, is an error
// that says so.
func readFileMode(p string) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, 0, fmt.Errorf("%s is missing", p)
	case err != nil:
		return nil, 0, fmt.Errorf("looking at %s: %w", p, err)
	case !info.Mode().IsRegular():
		return nil, 0, fmt.Errorf("%s is not a regular file", p)
	}
	data, err := os.ReadFile(p) //nolint:gosec // G304: a file of std or the runtime, in either tree
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", p, err)
	}
	return data, info.Mode().Perm(), nil
}

// TestComparePortFiles checks comparePortFiles, which TestPortFiles
// runs, on two small trees: the same file passes; differing bytes, a
// differing executable bit, a file only one tree has and a path neither
// has each fail; and portFileDivergences' rules, that a listed file
// must differ and a listed path must be a file, hold. Then it checks
// portFiles and checkPortOnly on a port and a frozen tree laid out as
// the real ones are (see testPortFileList). It needs no port and runs
// either way.
func TestComparePortFiles(t *testing.T) {
	t.Run("list", testPortFileList)
	port, frozen := t.TempDir(), t.TempDir()
	write := func(root, name, data string, mode fs.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(port, "same", "a\n", 0o644)
	write(frozen, "same", "a\n", 0o644)
	write(port, "bytes", "a\nb\n", 0o644)
	write(frozen, "bytes", "a\nc\n", 0o644)
	write(port, "mode", "a\n", 0o755)
	write(frozen, "mode", "a\n", 0o644)
	write(port, "port-only", "a\n", 0o644)
	write(frozen, "frozen-only", "a\n", 0o644)
	if err := os.Symlink("same", filepath.Join(port, "link")); err != nil {
		t.Fatal(err)
	}
	write(frozen, "link", "a\n", 0o644)
	file := func(name string) []portFile { return []portFile{{port: name, frozen: name}} }
	cases := []struct {
		allowed map[string]string
		name    string
		files   []portFile
		want    int
	}{
		{nil, "same", file("same"), 0},
		{nil, "bytes", file("bytes"), 1},
		{nil, "mode", file("mode"), 1},
		{nil, "port-only", file("port-only"), 1},
		{nil, "frozen-only", file("frozen-only"), 1},
		{nil, "neither", file("absent"), 1},
		{nil, "symlink", file("link"), 1},
		{map[string]string{"bytes": "a reason"}, "allowed difference", file("bytes"), 0},
		{map[string]string{"same": "a reason"}, "allowed but same", file("same"), 1},
		{map[string]string{"gone": "a reason"}, "allowed but unlisted", file("same"), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := comparePortFiles(port, frozen, c.files, c.allowed)
			if len(msgs) != c.want {
				t.Errorf("got %d failures, want %d: %q", len(msgs), c.want, msgs)
			}
		})
	}
}

// testPortFileList checks portFiles, comparePortFiles and checkPortOnly
// together, as TestPortFiles runs them, on a port tree and a frozen tree
// that match: they pass as they are, with the port's tack.mod listed in
// portOnly; and each change below fails them: an extra file below the
// port's runtime, a frozen runtime file the port is missing, a
// symbolic link below either src/std, which portFiles rejects, and a
// port-only file the port does not have.
func testPortFileList(t *testing.T) {
	portOnly := map[string]string{portRuntimeDir + "/tack.mod": "a reason"}
	failures := func(t *testing.T, port, frozen string) ([]string, error) {
		t.Helper()
		files, err := portFiles(port, frozen, portOnly)
		if err != nil {
			return nil, err
		}
		return append(comparePortFiles(port, frozen, files, nil), checkPortOnly(port, portOnly)...), nil
	}
	cases := []struct {
		change  func(t *testing.T, port, frozen string)
		name    string
		want    int
		wantErr bool
	}{
		{func(*testing.T, string, string) {}, "matching", 0, false},
		{func(t *testing.T, port, _ string) {
			t.Helper()
			mustDo(t, os.WriteFile(filepath.Join(port, portRuntimeDir, "tack", "extra.h"), nil, 0o600))
		}, "extra runtime file", 1, false},
		{func(t *testing.T, port, _ string) {
			t.Helper()
			mustDo(t, os.Remove(filepath.Join(port, portRuntimeDir, "x.c")))
		}, "missing runtime file", 1, false},
		{func(t *testing.T, port, _ string) {
			t.Helper()
			mustDo(t, os.Symlink("a.tck", filepath.Join(port, "src", "std", "a", "b.tck")))
		}, "symlink in the port's std", 0, true},
		{func(t *testing.T, _, frozen string) {
			t.Helper()
			mustDo(t, os.Symlink("a.tck", filepath.Join(frozen, "src", "std", "a", "b.tck")))
		}, "symlink in the frozen std", 0, true},
		{func(t *testing.T, port, _ string) {
			t.Helper()
			mustDo(t, os.Remove(filepath.Join(port, portRuntimeDir, "tack.mod")))
		}, "missing port-only file", 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			port, frozen := writePortTrees(t)
			c.change(t, port, frozen)
			msgs, err := failures(t, port, frozen)
			switch {
			case c.wantErr && err == nil:
				t.Errorf("portFiles gave no error, want one; failures %q", msgs)
			case !c.wantErr && err != nil:
				t.Errorf("portFiles: %v", err)
			case len(msgs) != c.want:
				t.Errorf("got %d failures, want %d: %q", len(msgs), c.want, msgs)
			}
		})
	}
}

// writePortTrees writes, in two temporary directories, a port tree and a
// frozen tree that match as TestPortFiles requires, laid out as the real
// ones are, and returns their roots: a std package, and the runtime's
// documentation, header and one C file, with the port's tack.mod.
// Each file holds its base name, the same in both trees.
func writePortTrees(t *testing.T) (port, frozen string) {
	t.Helper()
	port, frozen = t.TempDir(), t.TempDir()
	for root, files := range map[string][]string{
		port: {
			"src/std/a/a.tck", portRuntimeDir + "/builtin.tck", portRuntimeDir + "/tack/tack.h",
			portRuntimeDir + "/x.c", portRuntimeDir + "/tack.mod",
		},
		frozen: {"src/std/a/a.tck", "builtin/builtin.tck", "builtin/runtime/tack/tack.h", "builtin/runtime/x.c"},
	} {
		for _, f := range files {
			p := filepath.Join(root, filepath.FromSlash(f))
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(filepath.Base(p)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return port, frozen
}

// mustDo fails t, and stops it, when err, the result of a step that
// sets a test up, is not nil.
func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

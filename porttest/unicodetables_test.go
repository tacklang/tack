package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unicodeTablesStage is the stage of the port's own drift check of
// std/unicode's tables: its one case builds the port's tools/unicodegen
// with the port and checks that it writes the committed tables from the
// port's copy of the Unicode Character Database (see runUnicodeTables).
const unicodeTablesStage = "unicode-tables"

// unicodeTablesFile is std/unicode's tables, and unicodeUCDDir the
// directory holding the port's copy of the UCD they are written from,
// one directory per Unicode version, both relative to the port's source
// tree's root.
const (
	unicodeTablesFile = "src/std/unicode/tables.tck"
	unicodeUCDDir     = "tools/unicodegen/ucd"
)

// init adds unicode-tables to stages.
func init() {
	stages = append(stages, stage{
		name:   unicodeTablesStage,
		args:   func(input) []string { return []string{"--out", "unicodegen", "./tools/unicodegen"} },
		inputs: unicodeTablesInputs,
		stdout: compareBytes,
		run:    runUnicodeTables,
	})
}

// unicodeTablesInputs returns unicode-tables' one input: the port's
// source tree as the working directory.
func unicodeTablesInputs() ([]input, error) {
	return []input{{name: "tables", dir: env.home, suite: unicodeTablesStage}}, nil
}

// runUnicodeTables builds the port's tools/unicodegen with portBin, the
// port, in the port's source tree, runs it on the one directory of
// unicodeUCDDir, as make unicode-tables does, writing into a temporary
// file, and fails the case unless what it wrote is unicodeTablesFile,
// byte for byte, as make check-unicode-tables checks without Go, once
// the copy is the one SHA256SUMS pins and the version it is named for
// (see checkVendoredUCD); then runs it on each of unicodegenCases, as
// make test-unicodegen does (see checkUnicodegenCases); and runs
// std/unicode's tests with the port, which must pass, as make test-std
// requires (see unicodeTestsTarget). A decline of the build skips, or
// fails the case once the stage is required, as any case's does.
func runUnicodeTables(t *testing.T, st stage, in input, portBin string) {
	ucd, err := vendoredUCD(env.home)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkVendoredUCD(ucd); err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(env.home, filepath.FromSlash(unicodeTablesFile)))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	tool := filepath.Join(tmp, "unicodegen")
	built := runCompiler(t, portBin, env.portCache, in, []string{"--out", tool, "./tools/unicodegen"})
	if built.notImplemented() {
		declined(t, st, in, built)
	}
	if built.code != 0 {
		t.Fatalf("building tools/unicodegen with the port: exit status %d\n%s", built.code, built.stderr)
	}
	out := filepath.Join(tmp, "tables.tck")
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, ucd, out) //nolint:gosec // G204: the tool just built
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running tools/unicodegen on %s: %v\n%s", ucd, err, stderr.Bytes())
	}
	written, err := os.ReadFile(out) //nolint:gosec // G304: the file the tool wrote
	if err != nil {
		t.Fatal(err)
	}
	if d := firstDiffLabeled("committed", "written", committed, written); d != "" {
		t.Errorf("%s is not what tools/unicodegen writes from %s; run make unicode-tables in the port: %s",
			unicodeTablesFile, ucd, d)
	}
	checkUnicodegenCases(t, tool, tmp)
	tested := runCompiler(t, portBin, env.portCache, in, []string{"test", unicodeTestsTarget})
	if tested.code != 0 {
		t.Errorf("std/unicode's tests fail with the port, tack test %s: exit status %d\n%s%s",
			unicodeTestsTarget, tested.code, tested.stdout, tested.stderr)
	}
}

// unicodeTestsTarget is std/unicode's directory, as tack test is given
// it from the port's source tree's root: its tests check the tables'
// classes on runes Unicode 18.0.0 assigns and on the bounds of runs, so
// tables regenerated from a copy of the UCD changed with its checksums
// fail them. test-examples runs them with both compilers, and compares
// the two, which agree when both fail; this requires them to pass.
const unicodeTestsTarget = "./src/std/unicode"

// unicodegenCases is the directory of tools/unicodegen's test cases,
// relative to the port's source tree's root: each directory below it
// holding a file want is a case, a small UCD, most of it missing or
// malformed.
const unicodegenCases = "tools/unicodegen/testdata"

// checkUnicodegenCases runs tool, the port's tools/unicodegen, on each
// case of unicodegenCases, from the port's source tree's root with the
// case's path relative to it, writing into tmp, and fails the case of
// each whose standard error, followed by a line "exit <status>", is not
// its file want, or whose tables are not its want.tck when it has one,
// as the port's make test-unicodegen checks without Go. A directory of
// no cases is an error, so the check cannot pass by finding none.
func checkUnicodegenCases(t *testing.T, tool, tmp string) {
	t.Helper()
	wants, err := filepath.Glob(filepath.Join(env.home, filepath.FromSlash(unicodegenCases), "*", "want"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wants) == 0 {
		t.Fatalf("no case of tools/unicodegen in %s", unicodegenCases)
	}
	for _, want := range wants {
		name := filepath.Base(filepath.Dir(want))
		rel := unicodegenCases + "/" + name
		out := filepath.Join(tmp, name+".tck")
		ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
		cmd := exec.CommandContext(ctx, tool, rel, out) //nolint:gosec // G204: the tool just built
		cmd.Dir = env.home
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Errorf("running tools/unicodegen on %s: %v", rel, err)
			continue
		}
		fmt.Fprintf(&stderr, "exit %d\n", cmd.ProcessState.ExitCode())
		wantErr := readString(t, want)
		if d := firstDiffLabeled("want", "tools/unicodegen", []byte(wantErr), stderr.Bytes()); d != "" {
			t.Errorf("%s: standard error and exit status: %s", rel, d)
			continue
		}
		wantTck := filepath.Join(filepath.Dir(want), "want.tck")
		if _, err := os.Stat(wantTck); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s: %v", rel, err)
			}
			continue
		}
		got, err := os.ReadFile(out) //nolint:gosec // G304: the file the tool wrote
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if d := firstDiffLabeled("want.tck", "tools/unicodegen", []byte(readString(t, wantTck)), got); d != "" {
			t.Errorf("%s: tables: %s", rel, d)
		}
	}
}

// vendoredUCD returns the path of the port's copy of the UCD, in its
// source tree src: unicodeUCDDir must hold one directory, named for its
// Unicode version, and nothing else, so the copy the tables are written
// from is never in question.
func vendoredUCD(src string) (string, error) {
	dir := filepath.Join(src, filepath.FromSlash(unicodeUCDDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return "", fmt.Errorf("%s must hold one directory, the UCD of one Unicode version, and holds %d entries",
			dir, len(entries))
	}
	return filepath.Join(dir, entries[0].Name()), nil
}

// ucdSums is the file of a UCD copy that pins the SHA-256 of each other
// file it holds, as sha256sum writes them: the hash, two spaces and the
// file's name, a line each.
const ucdSums = "SHA256SUMS"

// checkVendoredUCD returns an error unless dir, the port's copy of the
// UCD, is what its ucdSums pins: every file it lists there with that
// SHA-256, every other file of dir listed, and the version the first
// line of its PropList.txt names, "# PropList-<version>.txt", the name
// of dir, as the port's make ucd-check checks with sha256sum.
func checkVendoredUCD(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, ucdSums)) //nolint:gosec // G304: the port's UCD copy
	if err != nil {
		return err
	}
	pinned := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || len(sum) != 64 || name == "" || strings.Contains(name, "/") || pinned[name] {
			return fmt.Errorf("%s:%d: not a SHA-256, two spaces and a file name of its own", ucdSums, i+1)
		}
		file, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: a file the copy pins
		if err != nil {
			return err
		}
		got := sha256.Sum256(file)
		if hex.EncodeToString(got[:]) != sum {
			return fmt.Errorf("%s/%s has SHA-256 %x, and %s pins %s", dir, name, got, ucdSums, sum)
		}
		pinned[name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() != ucdSums && !pinned[e.Name()] {
			return fmt.Errorf("%s/%s has no SHA-256 in %s", dir, e.Name(), ucdSums)
		}
	}
	pl, err := os.ReadFile(filepath.Join(dir, "PropList.txt")) //nolint:gosec // G304: the port's UCD copy
	if err != nil {
		return err
	}
	first, _, _ := strings.Cut(string(pl), "\n")
	version := strings.TrimSuffix(strings.TrimPrefix(first, "# PropList-"), ".txt")
	if version != filepath.Base(dir) {
		return fmt.Errorf("%s/PropList.txt's first line is %q, which names no version or not %s",
			dir, first, filepath.Base(dir))
	}
	return nil
}

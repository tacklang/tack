package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// testFilesDir is examples/test-files, a module whose packages hold test
// files that would each stop the build if they were read.
var testFilesDir = filepath.Join(examplesDir, "test-files")

// TestBuiltTestFilesExits42 builds examples/test-files, whose test files,
// main_test.tck and _test.tck beside main.tck and count_test.tck in the
// package it imports, are left out, and whose UPPER_TEST.tck and
// test.tck are not test files, and checks it prints its message and
// exits 3 + 30 + 9, one part from each file that is loaded.
func TestBuiltTestFilesExits42(t *testing.T) {
	bin := buildTargetWithClang(t, "test-files", testFilesDir)
	if out, code := runBuilt(t, bin); out != "test files left out\n" || code != 42 {
		t.Errorf("printed %q and exited %d, want %q and 42", out, code, "test files left out\n")
	}
}

// TestTestFilesLeftOutOfEveryMode checks every mode that loads a
// directory leaves its test files out: each dump, with and without
// --check, of examples/test-files names none of them, and --check of
// its package count, a library, passes though count_test.tck would not.
func TestTestFilesLeftOutOfEveryMode(t *testing.T) {
	for _, flags := range [][]string{
		{"--lex"},
		{"--ast"},
		{"--res"},
		{"--emit"},
		{"--check", "--lex"},
		{"--check", "--ast"},
		{"--check", "--res"},
		{"--check", "--emit"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			r := runTack(t, append(flags, testFilesDir)...)
			assertOnlyStdout(t, r)
			for _, name := range []string{
				"test-files/main_test.tck", "test-files/_test.tck", "count/count_test.tck",
			} {
				if strings.Contains(r.stdout, name) {
					t.Errorf("output names %s", name)
				}
			}
		})
	}
	for _, target := range []string{testFilesDir, filepath.Join(testFilesDir, "count")} {
		t.Run("--check "+filepath.Base(target), func(t *testing.T) {
			r := runTack(t, "--check", target)
			if r.code != exitOK || r.stderr != "" {
				t.Errorf("exit code %d, stderr:\n%s", r.code, r.stderr)
			}
		})
	}
}

// TestDirectoryOfOnlyTestFiles checks a directory whose only Tack files
// are test files fails as one with none does, saying it holds test
// files, as a program and, under --check, as a library, whose error
// says the same of the directory and those below it.
func TestDirectoryOfOnlyTestFiles(t *testing.T) {
	dir := filepath.Join(testFilesDir, "onlytests")
	assertFailure(t, runTack(t, "--emit", dir), "no .tck files other than test files in ")
	// The library's directory is shown by its absolute path, outside the
	// current directory.
	r := runTack(t, "--check", dir)
	assertFailure(t, r, "no .tck files other than test files in ")
	assertFailure(t, r, "/onlytests or below\n")
}

// TestTestFileTargetIsRejected checks a test file named as a single
// file target is rejected by every mode, before it is read, so even one
// that does not exist is, while a file whose name ends in _TEST.tck is
// not a test file.
func TestTestFileTargetIsRejected(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prog")
	for _, name := range []string{"main_test.tck", "_test.tck", "missing_test.tck"} {
		target := filepath.Join(testFilesDir, name)
		for _, mode := range []struct {
			name  string
			flags []string
		}{
			{"build", []string{"--out", out}},
			{"lex", []string{"--lex"}},
			{"ast", []string{"--ast"}},
			{"res", []string{"--res"}},
			{"emit", []string{"--emit"}},
			{"check", []string{"--check"}},
			{"check lex", []string{"--check", "--lex"}},
		} {
			t.Run(name+" "+mode.name, func(t *testing.T) {
				assertFailure(t, runTack(t, append(mode.flags, target)...),
					target+" is a test file, which is never loaded on its own")
			})
		}
	}
	assertOnlyStdout(t, runTack(t, "--lex", filepath.Join(testFilesDir, "UPPER_TEST.tck")))
}

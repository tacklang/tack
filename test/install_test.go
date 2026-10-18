package compilertest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/internal/stdtest"
)

// installStd runs make install-std in the repository with env, the whole
// environment it gets, and fails the test if it fails.
func installStd(t *testing.T, env ...string) {
	t.Helper()
	cmd := exec.Command("make", "-s", "install-std")
	cmd.Dir = stdtest.Home()
	cmd.Env = append(append([]string{}, env...), "PATH="+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make install-std: %v\n%s", err, out)
	}
}

// make install-std copies src/std to $TACK_HOME/src/std, a path with a
// space in it included, replacing what is there, so a deleted file does
// not linger, and writes the compiler's builtins' documentation and runtime
// header there read-only. Every run here writes only to temporary directories, never
// to the real home.
func TestMakeInstallStd(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	home := filepath.Join(t.TempDir(), "tack home")
	stale := filepath.Join(home, "src", "std", "builtin", "stale.tck")
	if err := os.MkdirAll(filepath.Dir(stale), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("package builtin\n\nvar x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installStd(t, "TACK_HOME="+home, "HOME="+filepath.Join(t.TempDir(), "unused"))
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a file not in src/std survived the install: %v", err)
	}
	for _, rel := range []string{"tack.mod", "fmt/fmt.tck", "builtin/builtin.tck", "include/tack/tack.h"} {
		want, err := os.ReadFile(filepath.Join(stdtest.Dir(), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(home, "src", "std", filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("installed %s differs from the repository's: %v", rel, err)
		}
	}
	checkCompilerCopies(t, home)
	// A second install replaces the read-only copies.
	installStd(t, "TACK_HOME="+home, "HOME="+filepath.Join(t.TempDir(), "unused"))
}

// checkCompilerCopies checks the copies make install-std wrote to the std
// of home of the files built into the compiler are the compiler's own,
// read-only.
func checkCompilerCopies(t *testing.T, home string) {
	t.Helper()
	for rel, want := range map[string][]byte{
		"builtin/builtin.tck": builtin.Source(),
		"include/tack/tack.h": builtin.RuntimeHeader(),
	} {
		path := filepath.Join(home, "src", "std", filepath.FromSlash(rel))
		got, err := os.ReadFile(path) //nolint:gosec // the test's own install
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("installed %s is not the compiler's: %v", rel, err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o222 != 0 {
			t.Errorf("installed %s is writable: %v", rel, err)
		}
	}
}

// Without TACK_HOME, make install-std installs to ~/.tack, and with
// neither TACK_HOME nor HOME it fails rather than install to /.tack.
func TestMakeInstallStdWithoutTackHome(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	user := t.TempDir()
	installStd(t, "HOME="+user)
	if _, err := os.Stat(
		filepath.Join(user, ".tack", "src", "std", "builtin", "builtin.tck"),
	); err != nil {
		t.Errorf("without TACK_HOME, std was not installed to ~/.tack: %v", err)
	}

	cmd := exec.Command("make", "-s", "install-std")
	cmd.Dir = stdtest.Home()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err == nil ||
		!bytes.Contains(out, []byte("neither TACK_HOME nor HOME is set")) {
		t.Errorf("with neither TACK_HOME nor HOME, make install-std = %v:\n%s", err, out)
	}
}

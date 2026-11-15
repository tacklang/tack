package porttest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckThroughLink runs --check, compared as check-clean compares
// its cases, on targets whose path goes through a symbolic link but that
// cannot be the builtins' documentation: a directory holding no
// builtin.tck, empty, holding a program, a module and one of its
// packages, and files not named builtin.tck, each with an error so that
// no backend is needed, reached through a link to its directory, from
// a tree written in a temporary directory. The port cannot resolve a
// link, and declines only a target that may still be that copy, so these
// must match Go. It runs only as TestPort does.
func TestCheckThroughLink(t *testing.T) {
	ready(t)
	st := stageNamed(cleanStage)
	if st == nil {
		t.Fatal("no check-clean stage")
	}
	dir := t.TempDir()
	files := map[string]string{
		"plain/a.tck":   "package main\n\nfunc main() {\n    x := nothing\n}\n",
		"mod/tack.mod":  "module m\n",
		"mod/main.tck":  "package main\n\nfunc main() {\n    y := undefined\n}\n",
		"mod/sub/b.tck": "package sub\n\nvar v int64 = missing\n",
		"empty/.keep":   "",
	}
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for link, to := range map[string]string{"plainlink": "plain", "modlink": "mod", "emptylink": "empty"} {
		if err := os.Symlink(to, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{
		"plainlink", "plainlink/a.tck", "emptylink", "modlink", "modlink/sub", "modlink/main.tck",
	} {
		t.Run(target, func(t *testing.T) {
			runCaseWith(t, *st, input{name: target, dir: dir, file: target}, env.portRun)
		})
	}
}

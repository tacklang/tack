package compilertest

import (
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/loader"
)

// TestUnusedFlag checks diag.Diagnostic.Unused, which an editor dims by:
// every warning of every case of examples/warnings.txtar and
// examples/warnings-packages.txtar, each of which is about code never
// used, carries it, and no error of those or of any case of
// examples/invalid.txtar and examples/invalid-packages.txtar ever does.
func TestUnusedFlag(t *testing.T) {
	for _, tc := range flagPrograms(t) {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range frontEndDiags(t, tc.prog) {
				switch {
				case d.Severity == diag.Error && d.Unused:
					t.Errorf("the error %q is marked unused", d.Message)
				case d.Severity == diag.Warning && !d.Unused:
					t.Errorf("the warning %q is not marked unused", d.Message)
				}
			}
		})
	}
}

// namedProgram is a program of an archive's case and the case's name.
type namedProgram struct {
	name string
	prog program
}

// flagPrograms returns the program of every case of the archives
// TestUnusedFlag reads.
func flagPrograms(t *testing.T) []namedProgram {
	t.Helper()
	var out []namedProgram
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			out = append(out, namedProgram{c.Name, singleFile(c.Name+"/main.tck", c.Source)})
		}
	}
	for _, path := range []string{invalid.TreePath, invalid.WarningsTreePath} {
		trees, err := invalid.LoadTree(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range trees {
			out = append(out, namedProgram{c.Name, treeProgram(c)})
		}
	}
	return out
}

// frontEndDiags runs the front end on p, every stage, and returns what
// it reported, or nothing when the loader ends the run with an error.
func frontEndDiags(t *testing.T, p program) []diag.Diagnostic {
	t.Helper()
	l := p.loader()
	var res *loader.Result
	var err error
	if p.single {
		res, err = l.File(p.target)
	} else {
		res, err = l.Dir(p.target)
	}
	if err != nil {
		return nil
	}
	diags, _ := checkAll(res)
	return diags
}

package porttest

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/tacklang/tack/internal/invalid"
)

// input is one file of the corpus, as a run names it: the compilers run
// from dir and are given file, a path relative to dir.
type input struct {
	// name identifies the input among the corpus, as its subtest name.
	name string
	// dir is the working directory the compilers run in.
	dir string
	// file is the path, relative to dir and slash-separated, the compilers
	// are given.
	file string
}

// archive is the cases of one txtar archive, written to disk.
type archive struct {
	// name is the archive's file name without .txtar, as invalid for
	// examples/invalid.txtar.
	name string
	// dir is the directory the cases are written to, each case's files
	// under <dir>/<case>.
	dir string
	// files are the paths, relative to dir and slash-separated, of every
	// file of every case, in archive order.
	files []string
}

// singleArchives are the archives of one file a case, named by their
// paths relative to a top-level package directory.
var singleArchives = []string{invalid.Path, invalid.WarningsPath}

// treeArchives are the archives of several files a case, named as
// singleArchives are.
var treeArchives = []string{invalid.TreePath, invalid.WarningsTreePath}

// writeArchives writes the cases of every archive of singleArchives and
// treeArchives, each to a directory of its own below dir, and returns
// them.
func writeArchives(dir string) ([]archive, error) {
	var out []archive
	for _, p := range singleArchives {
		cases, err := invalid.Load(filepath.FromSlash(p))
		if err != nil {
			return nil, fmt.Errorf("loading the corpus: %w", err)
		}
		a := newArchive(dir, p)
		for _, c := range cases {
			if err := a.write(c.Name+"/main.tck", c.Source); err != nil {
				return nil, err
			}
		}
		out = append(out, a)
	}
	for _, p := range treeArchives {
		cases, err := invalid.LoadTree(filepath.FromSlash(p))
		if err != nil {
			return nil, fmt.Errorf("loading the corpus: %w", err)
		}
		a := newArchive(dir, p)
		for _, c := range cases {
			for _, f := range c.Files {
				if err := a.write(c.Name+"/"+f.Path, f.Data); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// newArchive returns the archive at p, a slash-separated path, with no
// files yet, written below dir.
func newArchive(dir, p string) archive {
	name := strings.TrimSuffix(path.Base(p), ".txtar")
	return archive{name: name, dir: filepath.Join(dir, name)}
}

// write writes data to the file at rel, relative to the archive's
// directory, making the directories it needs, and records it.
func (a *archive) write(rel string, data []byte) error {
	p := filepath.Join(a.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("writing the corpus: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil { //nolint:gosec // G306: test files
		return fmt.Errorf("writing the corpus: %w", err)
	}
	a.files = append(a.files, rel)
	return nil
}

// singleFiles returns every single file of the corpus, as the package doc
// lists them: each .tck file under examples/, then each .tck file of each
// archive's cases.
func singleFiles() ([]input, error) {
	var out []input
	err := filepath.WalkDir(filepath.Join(env.root, "examples"),
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(p) != ".tck" {
				return nil
			}
			rel, err := filepath.Rel(env.root, p)
			if err != nil {
				return fmt.Errorf("naming %s: %w", p, err)
			}
			rel = filepath.ToSlash(rel)
			out = append(out, input{name: rel, dir: env.root, file: rel})
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("listing examples: %w", err)
	}
	for _, a := range env.archives {
		for _, f := range a.files {
			if path.Ext(f) == ".tck" {
				out = append(out, input{name: a.name + "/" + f, dir: a.dir, file: f})
			}
		}
	}
	return out, nil
}

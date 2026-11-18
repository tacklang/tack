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
	// suite names the set of cases the input is one of in the divergence
	// list: a stage's name for its own cases, as TestPort runs them, or
	// emit-probes for TestEmitProbes's programs. It is empty for an input
	// that is no case of a suite, as the deep tests make, which no entry
	// applies to (see matchDivergence).
	suite string
	// env holds variables, each name=value, the compilers run with after
	// the environment they share, overriding it, as PWD for a directory
	// reached through a symbolic link; it is empty for the corpus.
	env []string
	// declines says the port may answer that it is not implemented for
	// the input even in a required stage: a failure of the file system
	// the port cannot word as Go does, as TestLoadTrees makes.
	declines bool
	// uncached says the input's classification is neither read from the
	// coverage cache nor written to it: a fuzz input's, which would fill
	// the cache with programs seen once.
	uncached bool
	// combined runs the compilers with standard output and standard error
	// one pipe, read as standard output, so that the order in which the
	// two streams were written is compared; standard error is then empty.
	combined bool
	// closedStdout runs the compilers with standard output a pipe whose
	// reader is closed, so that a write to it fails with EPIPE, or kills
	// the writer with SIGPIPE; standard output is then empty.
	closedStdout bool
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
	// targets are the directories, relative to dir and slash-separated,
	// the cases of an archive of several files a case are built from, in
	// archive order: each case's own, or the one its .target names. A
	// case built from a file has none here; it is among files.
	targets []string
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
			if target := c.Target(); path.Ext(target) != ".tck" {
				a.targets = append(a.targets, target)
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

// packageInputs returns every input of the corpus that is a package, as
// the package doc lists them: each directory under this module's
// examples/ and under the src/std of env.home, the std the comparisons
// read, that holds a .tck file directly, named and run from its tree's
// root, examples/<path> and src/std/<path>; each module under
// examples/, a directory holding a tack.mod, named . from its root, and
// each of its main packages by its import path from there; std's
// packages by their import paths from env.home's src/std, which is the
// module std;
// and each case of an archive of several files a case that is built
// from a directory, its target, from the archive's directory.
func packageInputs() ([]input, error) {
	var out []input
	for _, top := range []struct{ base, dir string }{
		{env.root, "examples"}, {env.home, filepath.Join("src", "std")},
	} {
		dirs, err := tackDirs(filepath.Join(top.base, top.dir))
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			rel, err := filepath.Rel(top.base, d)
			if err != nil {
				return nil, fmt.Errorf("naming %s: %w", d, err)
			}
			rel = filepath.ToSlash(rel)
			out = append(out, input{name: rel, dir: top.base, file: rel})
		}
	}
	mods, err := moduleInputs()
	if err != nil {
		return nil, err
	}
	out = append(out, mods...)
	for _, a := range env.archives {
		for _, target := range a.targets {
			out = append(out, input{name: a.name + "/" + target, dir: a.dir, file: target})
		}
	}
	// The port's own driver, which imports the whole port and most of
	// std, the largest program the corpus has, when its source tree is
	// here: a run given only TACK_PORT_BIN may have none.
	if src, _ := portSource(os.Getenv("TACK_PORT")); dirExists(filepath.Join(src, "driver")) {
		out = append(out, input{name: "port/driver", dir: src, file: "./driver"})
	}
	return out, nil
}

// dirExists reports whether p is a directory.
func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// tackDirs returns root and every directory below it that holds a .tck
// file directly, each once, in the order filepath.WalkDir first visits a
// .tck file in it.
func tackDirs(root string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".tck" {
			return nil
		}
		if dir := filepath.Dir(p); !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", root, err)
	}
	return dirs, nil
}

// moduleInputs returns, for each module under examples/, the module's
// root named . from itself and each directory below it holding a .tck
// file by its import path from the root, and std's packages by theirs
// from env.home's src/std, named src/std/... as from env.home. A
// directory's import path is the module's name, from
// the first module line of its tack.mod, and its path below the root.
func moduleInputs() ([]input, error) {
	var roots []string
	err := filepath.WalkDir(filepath.Join(env.root, "examples"),
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && d.Name() == "tack.mod" {
				roots = append(roots, filepath.Dir(p))
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("listing modules: %w", err)
	}
	roots = append(roots, homeStd())
	var out []input
	for _, root := range roots {
		name, err := moduleName(filepath.Join(root, "tack.mod"))
		if err != nil {
			return nil, err
		}
		base := env.root
		if root == homeStd() {
			base = env.home
		}
		rel, err := filepath.Rel(base, root)
		if err != nil {
			return nil, fmt.Errorf("naming %s: %w", root, err)
		}
		rel = filepath.ToSlash(rel)
		out = append(out, input{name: rel + "/.", dir: root, file: "."})
		dirs, err := tackDirs(root)
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			below, err := filepath.Rel(root, d)
			if err != nil {
				return nil, fmt.Errorf("naming %s: %w", d, err)
			}
			if below == "." {
				continue
			}
			ip := name + "/" + filepath.ToSlash(below)
			out = append(out, input{name: rel + "/" + ip, dir: root, file: ip})
		}
	}
	return out, nil
}

// moduleName returns the name the tack.mod at p gives its module: the
// word after module on its first module line.
func moduleName(p string) (string, error) {
	data, err := os.ReadFile(p) //nolint:gosec // G304: a file of this module
	if err != nil {
		return "", fmt.Errorf("reading a module's name: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
			return f[1], nil
		}
	}
	return "", fmt.Errorf("%s names no module", p)
}

package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/tacklang/tack/modfile"
	"github.com/tacklang/tack/source"
)

// stdName is the name of the standard library's module, the first part of
// the import path of each of its packages.
const stdName = "std"

// module is a module found on disk: the directory holding its tack.mod,
// the name the tack.mod gives it, and what its files are read through.
type module struct {
	fsys FS
	root string
	name string
}

// stdModule returns the module std, the standard library at l.Std, or nil
// when l has none. Its name is std whatever its tack.mod says; the loader
// does not read that tack.mod.
func (l *Loader) stdModule() *module {
	if l.Std == nil {
		return nil
	}
	fsys := l.Std.FS
	if fsys == nil {
		fsys = l.FS
	}
	return &module{root: path.Clean(l.Std.Dir), name: stdName, fsys: fsys}
}

// isStdRoot reports whether the directory d, read through l.FS, is the
// root of std: l.Std is read through l.FS too, and d is its directory.
func (l *Loader) isStdRoot(d string) bool {
	return l.Std != nil && l.Std.FS == nil && path.Clean(d) == path.Clean(l.Std.Dir)
}

// findModule finds the module dir belongs to: the nearest directory at or
// above dir holding a tack.mod, which it reads into res and parses. It
// returns an error when there is none, or one cannot be read, and a nil
// module with the tack.mod's diagnostics recorded in res when it is
// malformed. The root of std, l.Std's directory, is the module std,
// whose tack.mod is not read: its name, std, is one no other module may
// take.
func (l *Loader) findModule(res *Result, dir string) (*module, error) {
	for d := dir; ; {
		if l.isStdRoot(d) {
			return l.stdModule(), nil
		}
		name := path.Join(d, modfile.Name)
		data, err := l.FS.ReadFile(name)
		switch {
		case err == nil:
			return l.parseModule(res, d, name, data), nil
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("cannot read %s: %w", l.display(name), err)
		}
		parent := path.Dir(d)
		if parent == d {
			return nil, fmt.Errorf("no %s found in %s or any parent directory",
				modfile.Name, l.display(dir))
		}
		d = parent
	}
}

// parseModule parses data, the tack.mod at name in the directory root,
// recording it in res, and returns the module it declares, or nil with its
// diagnostics recorded in res.
func (l *Loader) parseModule(res *Result, root, name string, data []byte) *module {
	f := l.addFile(res, name, data)
	mf, diags := modfile.Parse(f)
	if len(diags) > 0 {
		res.report(StageLoad, diags)
		return nil
	}
	return &module{root: root, name: mf.Module, fsys: l.FS}
}

// pathOf returns the import path of dir, a directory at or below the
// module's root: the module's name, then dir's path below the root.
func (m *module) pathOf(dir string) string {
	rel := dir
	switch {
	case dir == m.root:
		return m.name
	case m.root == ".":
	case m.root == "/":
		rel = strings.TrimPrefix(dir, "/")
	default:
		rel = strings.TrimPrefix(dir, m.root+"/")
	}
	return m.name + "/" + rel
}

// dirOf returns the directory the import path importPath names in the
// module, or why it names none: its first parts must be the module's name.
func (m *module) dirOf(importPath string) (string, string) {
	if importPath == m.name {
		return m.root, ""
	}
	rest, ok := strings.CutPrefix(importPath, m.name+"/")
	if !ok {
		first, _, _ := strings.Cut(importPath, "/")
		return "", fmt.Sprintf("unknown module %s in import path %s", show(first), show(importPath))
	}
	return path.Join(m.root, rest), ""
}

// IsStdBuiltinDir reports whether the directory dir is std/builtin: the
// directory builtin at the root of the module std, the installed standard
// library or a checkout of it. Its builtin.tck is a copy of the builtins'
// documentation, which the compiler builds in and which no package of
// std or anything else is made of, so tools treat that one file apart.
func (l *Loader) IsStdBuiltinDir(dir string) bool {
	res := &Result{Files: source.NewSet()}
	mod, err := l.findModule(res, path.Clean(dir))
	return err == nil && mod != nil && mod.name == stdName &&
		mod.pathOf(path.Clean(dir)) == builtinPath
}

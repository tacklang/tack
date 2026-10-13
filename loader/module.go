package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/tacklang/tack/modfile"
)

// module is a module found on disk: the directory holding its tack.mod and
// the name the tack.mod gives it.
type module struct {
	root string
	name string
}

// findModule finds the module dir belongs to: the nearest directory at or
// above dir holding a tack.mod, which it reads into res and parses. It
// returns an error when there is none, or one cannot be read, and a nil
// module with the tack.mod's diagnostics recorded in res when it is
// malformed.
func (l *Loader) findModule(res *Result, dir string) (*module, error) {
	for d := dir; ; {
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
		res.Stage, res.Diags = StageLoad, diags
		return nil
	}
	return &module{root: root, name: mf.Module}
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

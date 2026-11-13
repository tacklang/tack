package porttest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/loader"
)

// loadedCheck is TestLoadOrder's name in required.txt.
const loadedCheck = "loaded"

// TestLoadOrder checks what the port loads for each package input of the
// corpus, the port's own driver, and each of treeCases' inputs, but
// those that set PWD, against
// this module's loader, run in process as the driver runs it for --res:
// every package of the program, in order, with its files, C files and
// what each import is bound to, the program's error count, and every
// diagnostic, in the order it was reported, before the driver sorts
// them. The port writes them with --port-loaded, a flag of its own that
// the Go compiler does not have, a temporary test hook it keeps until it
// can dump a program with --res, at M5; a port without it skips, unless
// loaded is in required.txt, as it is once the port has it. The port
// runs with its stack held to deepStack KB, as TestDeepNesting runs it. It runs only as TestPort
// does, with TACK_PORT_TEST=1 and -count.
func TestLoadOrder(t *testing.T) {
	ready(t)
	ins, err := packageInputs()
	if err != nil {
		t.Fatal(err)
	}
	// The port's own driver, which imports most of std, when its source
	// tree is here: a run given only TACK_PORT_BIN may have none.
	if src, _ := portSource(os.Getenv("TACK_PORT")); dirExists(filepath.Join(src, "driver")) {
		ins = append(ins, input{name: "port/driver", dir: src, file: "./driver"})
	}
	wrapper := stackWrapper(t, t.TempDir())
	t.Run("corpus", func(t *testing.T) {
		for _, in := range ins {
			t.Run(in.name, func(t *testing.T) {
				t.Parallel()
				checkLoaded(t, in, wrapper)
			})
		}
	})
	t.Run("trees", func(t *testing.T) {
		for name, tc := range treeCases {
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				for _, in := range tc.make(t, dir) {
					if len(in.env) > 0 {
						continue
					}
					t.Run(in.name, func(t *testing.T) { checkLoaded(t, in, wrapper) })
				}
			})
		}
	})
}

// dirExists reports whether p is a directory.
func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// checkLoaded runs portBin, the port or a wrapper around it, with
// --port-loaded on in and compares what it writes with what goLoaded
// gives, skipping an input goLoaded does not load, a single file, and
// one the port answers is not implemented, unless loaded is required
// and in does not decline.
func checkLoaded(t *testing.T, in input, portBin string) {
	t.Helper()
	want, ok := goLoaded(t, in)
	if !ok {
		t.Skip("a single file")
	}
	port := runCompiler(t, portBin, env.portCache, in, []string{"--port-loaded", in.file})
	if port.notImplemented() {
		msg := strings.TrimSuffix(string(port.stderr), "\n")
		if env.required[loadedCheck] && !in.declines {
			t.Fatalf("%s is required, but the port says %q", loadedCheck, msg)
		}
		t.Skip(msg)
	}
	if port.code != 0 || len(port.stderr) > 0 {
		t.Fatalf("the port exited %d, writing to standard error:\n%s", port.code, port.stderr)
	}
	if d := firstDiff([]byte(want), port.stdout); d != "" {
		t.Errorf("what is loaded differs: %s\ncommand: (cd %s && tack --port-loaded %s)", d, in.dir, in.file)
	}
}

// goLoaded returns what --port-loaded writes for in, from this module's
// loader, loading in's target as the driver's loadWith does for --res:
// a directory, or an import path when it names nothing on disk and looks
// like one, from in's directory as the current one, as getcwd gives it,
// its symbolic links resolved, with files shown relative to it, and the
// repository's std. It reports false for a target that is a single
// file, which it does not load.
func goLoaded(t *testing.T, in input) (string, bool) {
	t.Helper()
	cwd, err := filepath.EvalSymlinks(in.dir)
	if err != nil {
		t.Fatal(err)
	}
	l := &loader.Loader{
		FS: loader.OS{}, Builtins: builtin.Default(), MaxErrors: 10,
		Std:     &loader.Std{Dir: filepath.ToSlash(filepath.Join(env.root, "src", "std"))},
		Display: relativeTo(cwd),
	}
	full := in.file
	if !filepath.IsAbs(full) {
		full = filepath.Join(cwd, full)
	}
	info, statErr := os.Stat(full)
	var res *loader.Result
	switch {
	case statErr == nil && info.IsDir():
		res, err = l.Dir(filepath.ToSlash(full))
	case errors.Is(statErr, fs.ErrNotExist) && looksLikeImportPath(in.file):
		res, err = l.Path(filepath.ToSlash(cwd), in.file)
		if err != nil {
			err = fmt.Errorf("%s is not a file or a directory, nor an import path: %w", in.file, err)
		}
	default:
		return "", false
	}
	if err != nil {
		return fmt.Sprintf("error %v\n", err), true
	}
	return formatLoaded(res), true
}

// formatLoaded writes res as --port-loaded writes a load's result (see
// write_loaded in the port's driver).
func formatLoaded(res *loader.Result) string {
	var b strings.Builder
	if prog := res.Program; prog != nil {
		index := make(map[*ast.Package]int, len(prog.Packages))
		for i, p := range prog.Packages {
			index[p] = i
		}
		for _, p := range prog.Packages {
			fmt.Fprintf(&b, "package %s module %s name %s dir %s\n", p.Path, p.Module, p.Name, p.Dir)
			for _, f := range p.Files {
				fmt.Fprintf(&b, "file %s\n", f.Span.File)
			}
			for _, c := range p.CFiles {
				fmt.Fprintf(&b, "cfile %s\n", c)
			}
			for _, f := range p.Files {
				for _, imp := range f.Imports {
					i := -1
					if imp.Package != nil {
						i = index[imp.Package]
					}
					fmt.Fprintf(&b, "import %s %d\n", imp.Path, i)
				}
			}
		}
		fmt.Fprintf(&b, "errors %d\n", prog.Errors)
	} else {
		b.WriteString("no program\n")
	}
	for _, d := range res.Diags {
		fmt.Fprintf(&b, "diag %s:%d: %s\n", d.Span.File, d.Span.Start, d.Message)
	}
	return b.String()
}

// relativeTo is the driver's relativeTo: a loader Display function that
// shows a slash path relative to dir when it is in dir or below it, and
// as it is otherwise.
func relativeTo(dir string) func(string) string {
	return func(name string) string {
		rel, err := filepath.Rel(dir, filepath.FromSlash(name))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return name
		}
		return filepath.ToSlash(rel)
	}
}

// looksLikeImportPath is the driver's looksLikeImportPath: target, which
// names nothing on disk, is not absolute, does not start with a dot, and
// does not end in .tck.
func looksLikeImportPath(target string) bool {
	return target != "" && !filepath.IsAbs(target) && !strings.HasPrefix(target, ".") &&
		!strings.HasSuffix(target, ".tck")
}

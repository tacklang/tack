package porttest

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"
)

// loadDiffSeeds are the file trees FuzzLoadDiff starts from, in
// writeFuzzTree's form: a module with a main package, a deep target, a
// package clause that does not match, a malformed tack.mod, a hidden
// file, an import chain, a cycle, an import of the main package and one
// of std, a module named std, test files, and a lexing error in an
// imported package, which stops the load so the port can answer --res.
var loadDiffSeeds = []string{
	"tack.mod\nmodule calc\n\x00main.tck\npackage main\nfunc main() {}\n",
	"a/b/c/main.tck\npackage main\nfunc main() {}\n\x00tack.mod\nmodule m\n",
	"lexer/a.tck\npackage lexer\n\x00tack.mod\nmodule calc\n",
	"x/a.tck\npackage main\n\x00x/b.tck\npackage other\n\x00x/tack.mod\nmodule std\n",
	"my-tools/a.tck\npackage mytools\n\x00tack.mod\nmodule calc\n",
	"a\xff/b.tck\npackage main\n\x00tack.mod\nmodule\n",
	"p/.h.tck\npackage main\n\x00p/tack.mod\nmodule p\nmodule q\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport a from \"m/a\"\nfunc main() {}\n" +
		"\x00a/a.tck\npackage a\nimport b from \"m/b\"\n\x00b/b.tck\npackage b\nimport a from \"m/a\"\nimport z from \"m/z\"\n" +
		"\x00z/z.tck\npackage z\n@\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport m from \"m\"\nimport f from \"std/fmt\"\nimport z from \"m/z\"\n" +
		"\x00z/z.tck\npackage z\n\"\n",
	"std/tool/main.tck\npackage main\nimport f from \"std/fmt\"\nimport m from \"m/x\"\nfunc main() {}\n" +
		"\x00std/tack.mod\nmodule std\n\x00std/fmt/fmt.tck\npackage fmt\nimport f from \"std/fmt\"\n@\n",
	"p/main.tck\npackage main\nimport t from \"m/t\"\nfunc main() {}\n\x00p/main_test.tck\n@\n" +
		"\x00p/_test.tck\npackage main\n\x00p/X_TEST.tck\npackage main\n@\n\x00t/t_test.tck\npackage t\n" +
		"\x00tack.mod\nmodule m\n",
}

// writeFuzzTree writes data, entries separated by 0 bytes, each a path, a
// newline and the file's content, as files below dir, and returns the
// directory of the first entry written, the target, relative to dir, or
// . when none is. An entry whose path fs.FS would not take, . among
// them, or that cannot be written, as one whose directory is a file
// another entry wrote, is left out.
func writeFuzzTree(dir string, data []byte) string {
	target := ""
	for _, entry := range bytes.Split(data, []byte{0}) {
		name, content, _ := bytes.Cut(entry, []byte("\n"))
		p := string(name)
		if !fs.ValidPath(p) || p == "." {
			continue
		}
		full := filepath.Join(dir, filepath.FromSlash(p))
		if os.MkdirAll(filepath.Dir(full), 0o755) != nil {
			continue
		}
		if os.WriteFile(full, content, 0o644) != nil { //nolint:gosec // G306: test files
			continue
		}
		if target == "" {
			target = path.Dir(p)
		}
	}
	if target == "" {
		target = "."
	}
	return target
}

// FuzzLoadDiff writes each input as a file tree, in writeFuzzTree's form,
// in a temporary directory of its own, and runs both compilers on the
// directory of its first file from there: --ast, compared as the
// ast-packages stage compares it, what the port loads with
// --port-loaded, compared as TestLoadOrder compares it, and --res,
// compared as res-packages does, each skipping, and skipping the rest,
// when the port answers that it is not implemented, unless its stage is
// required. The port runs with its stack held to deepStack KB, as
// TestDeepNesting runs it. Before that it loads the tree with this module's loader, in
// process, as TestLoadOrder does, so the fuzz engine's coverage has the
// Go loader's to guide it.
//
// It is seeded with loadDiffSeeds; run as a test, without -fuzz, it
// checks them alone. It runs as FuzzLexDiff does.
func FuzzLoadDiff(f *testing.F) {
	ready(f)
	for _, s := range loadDiffSeeds {
		f.Add([]byte(s))
	}
	astStage, resStage := stageNamed("ast-packages"), stageNamed("res-packages")
	if astStage == nil || resStage == nil {
		f.Fatal("no ast-packages or res-packages stage")
	}
	wrapper := stackWrapper(f, f.TempDir())
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		// The target is written ./ first, which keeps a directory named
		// like a flag, --lex or -, from being read as one by either
		// compiler, and is the same directory.
		target := "./" + writeFuzzTree(dir, data)
		in := input{name: target, dir: dir, file: target}
		checkLoaded(t, in, wrapper)
		runCaseWith(t, *astStage, in, wrapper)
		runCaseWith(t, *resStage, in, wrapper)
	})
}

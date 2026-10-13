package loader_test

import (
	"bytes"
	"io/fs"
	"path"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/loader"
)

// treeFromBytes builds a file system from data: entries separated by NUL
// bytes, each a path, a newline, and the file's content. An entry whose
// path fs.FS cannot hold is dropped. It returns the file system and the
// directory of the first entry, the target.
func treeFromBytes(data []byte) (fstest.MapFS, string) {
	fsys := fstest.MapFS{}
	target := ""
	for _, entry := range bytes.Split(data, []byte{0}) {
		name, content, _ := bytes.Cut(entry, []byte("\n"))
		p := string(name)
		if !fs.ValidPath(p) || p == "." {
			continue
		}
		fsys[p] = &fstest.MapFile{Data: content}
		if target == "" {
			target = path.Dir(p)
		}
	}
	if target == "" {
		target = "."
	}
	return fsys, target
}

// FuzzLoad loads random file trees, with odd names, deep nesting, and
// tack.mod files anywhere, and checks the loader never panics, and either
// fails with an error or gives a program exactly when it gives no
// diagnostic, with every diagnostic's span in a file it read.
func FuzzLoad(f *testing.F) {
	for _, seed := range []string{
		"tack.mod\nmodule calc\n\x00main.tck\npackage main\nfunc main() {}\n",
		"a/b/c/main.tck\npackage main\nfunc main() {}\n\x00tack.mod\nmodule m\n",
		"lexer/a.tck\npackage lexer\n\x00tack.mod\nmodule calc\n",
		"x/a.tck\npackage main\n\x00x/b.tck\npackage other\n\x00x/tack.mod\nmodule std\n",
		"my-tools/a.tck\npackage mytools\n\x00tack.mod\nmodule calc\n",
		"a\xff/b.tck\npackage main\n\x00tack.mod\nmodule\n",
		"p/.h.tck\npackage main\n\x00p/tack.mod\nmodule p\nmodule q\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fsys, target := treeFromBytes(data)
		res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Dir(target)
		if err != nil {
			return
		}
		if (res.Program == nil) == (len(res.Diags) == 0) {
			t.Fatalf("program %v with %d diagnostics", res.Program, len(res.Diags))
		}
		for _, d := range res.Diags {
			file := res.Files.Lookup(d.Span.File)
			if file == nil || d.Span.Start < 0 || d.Span.End > len(file.Data) {
				t.Fatalf("diagnostic %q has span %v outside every file read", d.Message, d.Span)
			}
		}
	})
}

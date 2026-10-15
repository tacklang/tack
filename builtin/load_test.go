package builtin_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/types"
)

// The builtins' signatures are part of the language, so the repository's
// std/builtin is pinned here, C names included, rather than only read
// back.
func TestRepositoryBuiltins(t *testing.T) {
	set := stdtest.Builtins(t)
	want := []builtin.Func{
		{Name: "print", CName: "tack_builtin_print", Params: []types.Type{types.String}},
		{Name: "exit", CName: "tack_builtin_exit", Params: []types.Type{types.Int32}},
	}
	got := set.Funcs()
	if len(got) != len(want) {
		t.Fatalf("Funcs() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].CName != want[i].CName ||
			!equalTypes(got[i].Params, want[i].Params) {
			t.Errorf("Funcs()[%d] = %+v, want %+v", i, got[i], want[i])
		}
		found, ok := set.Lookup(want[i].Name)
		if !ok || found.CName != want[i].CName {
			t.Errorf("Lookup(%q) = %+v, %v", want[i].Name, found, ok)
		}
	}
	if set.Dir() != stdtest.BuiltinDir() {
		t.Errorf("Dir() = %q, want %q", set.Dir(), stdtest.BuiltinDir())
	}
}

// equalTypes reports whether a and b are the same types in the same order.
func equalTypes(a, b []types.Type) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// One builtin per C file, named after it, is the convention the driver's
// member selection makes cheap: each builtin's body is in <name>.c, which
// defines it, not static, and no C file is there for no builtin but
// list.c, the C of lists, which defines every one of builtin.ListSymbols,
// not static.
func TestRepositoryCFilesAreOnePerBuiltin(t *testing.T) {
	set := stdtest.Builtins(t)
	byFile := make(map[string]builtin.Func)
	for _, fn := range set.Funcs() {
		byFile[filepath.Join(set.Dir(), fn.Name+".c")] = fn
	}
	lists := filepath.Join(set.Dir(), "list.c")
	if len(set.CFiles()) != len(byFile)+1 {
		t.Errorf("CFiles() = %q, want one per builtin and list.c", set.CFiles())
	}
	for _, path := range set.CFiles() {
		if path == lists {
			checkListC(t, path)
			continue
		}
		fn, ok := byFile[path]
		if !ok {
			t.Errorf("%s is the body of no builtin", path)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		if !strings.Contains(src, "void "+fn.CName+"(") {
			t.Errorf("%s does not define void %s", path, fn.CName)
		}
		if strings.Contains(src, "static void "+fn.CName) {
			t.Errorf("%s defines %s static, which no other object can link", path, fn.CName)
		}
		if !strings.Contains(src, "#include <tack/tack.h>") {
			t.Errorf("%s does not include <tack/tack.h>", path)
		}
	}
}

// checkListC checks list.c, at path, defines every symbol lists need, none
// static, and includes <tack/tack.h>, which declares them.
func checkListC(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, sym := range builtin.ListSymbols {
		if !strings.Contains(src, sym+"(") {
			t.Errorf("%s does not define %s", path, sym)
		}
		if strings.Contains(src, "static void "+sym) || strings.Contains(src, "static void *"+sym) {
			t.Errorf("%s defines %s static, which no other object can link", path, sym)
		}
	}
	if !strings.Contains(src, "#include <tack/tack.h>") {
		t.Errorf("%s does not include <tack/tack.h>", path)
	}
}

// A generic builtin has no declaration in std/builtin, so it must at least
// be described there, as len(...) in a comment.
func TestGenericBuiltinsAreDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(stdtest.BuiltinDir(), "builtin.tck"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range builtin.Generic() {
		if !strings.Contains(string(data), "// "+name+"(") {
			t.Errorf("builtin.tck does not describe the builtin %s", name)
		}
	}
}

// Every builtin's name is in Names, the generic ones included, so the
// resolver can bind a call to it.
func TestNamesListsEveryBuiltin(t *testing.T) {
	names := " " + strings.Join(stdtest.Builtins(t).Names(), " ") + " "
	for _, want := range []string{"print", "exit", "len", "make", "append"} {
		if !strings.Contains(names, " "+want+" ") {
			t.Errorf("Names() = %q, missing %s", names, want)
		}
	}
}

// Names that are not builtins are not found: near misses, other languages'
// spellings, Go's builtins Tack does not have, and len, make and append,
// which are generic.
func TestLookupRejectsNamesThatAreNotBuiltins(t *testing.T) {
	set := stdtest.Builtins(t)
	for _, name := range []string{
		"", "Print", "PRINT", "println", "printf", " print", "exit ", "Exit", "len", "make", "append",
		"panic", "main", "tack_builtin_print",
	} {
		if got, ok := set.Lookup(name); ok {
			t.Errorf("Lookup(%q) = %+v, true, want not found", name, got)
		}
	}
}

// A nil Set is empty, not a crash: it has no builtins but the generic
// ones.
func TestNilSetIsEmpty(t *testing.T) {
	var set *builtin.Set
	if len(set.Funcs()) != 0 || len(set.CFiles()) != 0 || set.Dir() != "" {
		t.Errorf("nil Set has %v, %v, %q", set.Funcs(), set.CFiles(), set.Dir())
	}
	if _, ok := set.Lookup("print"); ok {
		t.Error("nil Set looks up print")
	}
	if got := strings.Join(set.Names(), " "); got != "len make append" {
		t.Errorf("nil Set's Names() = %q, want len make append", got)
	}
}

// writeStd writes files, by name, to a fresh directory and returns it.
func writeStd(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A std/builtin of several files, with files and directories that are not
// its own beside them, loads every .tck file's builtins in name order and
// lists only its own .c files.
func TestLoadReadsEveryFileInNameOrder(t *testing.T) {
	dir := writeStd(t, map[string]string{
		"b.tck":         "package builtin\n\nextern(\"tack_builtin_b\") func b(x bool)\n",
		"a.tck":         "package builtin\n\nextern(\"tack_builtin_a\") func a(x uint8, y unsafe_size_t)\n",
		"b.c":           "",
		"a.c":           "",
		".hidden.tck":   "not tack",
		".hidden.c":     "",
		"README.md":     "# not read",
		"sub.tck/x.tck": "not read either",
		"sub/c.c":       "",
	})
	set, err := builtin.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(set.Funcs()))
	for _, fn := range set.Funcs() {
		names = append(names, fn.Name+"="+fn.CName)
	}
	if got := strings.Join(names, " "); got != "a=tack_builtin_a b=tack_builtin_b" {
		t.Errorf("Funcs() = %s", got)
	}
	a, _ := set.Lookup("a")
	if !equalTypes(a.Params, []types.Type{types.Uint8, types.UnsafeSize}) {
		t.Errorf("a's params = %v", a.Params)
	}
	want := []string{filepath.Join(dir, "a.c"), filepath.Join(dir, "b.c")}
	if got := set.CFiles(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("CFiles() = %q, want %q", got, want)
	}
}

// A builtin may take no parameters, and a file may declare several, or
// none, as long as the directory declares something.
func TestLoadAcceptsEdgeShapes(t *testing.T) {
	dir := writeStd(t, map[string]string{
		"a.tck": "package builtin\n\n// only a comment\n",
		"b.tck": "package builtin\n\nextern(\"tack_x\") func x()\nextern(\"tack_y\") func y(a int64, b int64, c string)\n",
	})
	set, err := builtin.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Funcs()) != 2 {
		t.Errorf("Funcs() = %+v", set.Funcs())
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	_, err := builtin.Load(dir)
	if err == nil || !strings.Contains(err.Error(), "cannot read the builtins") ||
		!strings.Contains(err.Error(), dir) || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(missing) = %v", err)
	}
}

func TestLoadFileInsteadOfDirectory(t *testing.T) {
	dir := writeStd(t, map[string]string{"x": ""})
	if _, err := builtin.Load(filepath.Join(dir, "x")); err == nil {
		t.Error("Load of a file succeeded")
	}
}

func TestLoadDirectoryWithoutTackFiles(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"empty":  {},
		"only C": {"print.c": "void tack_builtin_print(void) {}\n"},
		"hidden": {".builtin.tck": "package builtin\n"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeStd(t, files)
			_, err := builtin.Load(dir)
			want := "no .tck files in " + dir + ", which declares the builtins"
			if err == nil || err.Error() != want {
				t.Errorf("Load = %v, want %q", err, want)
			}
		})
	}
}

// rejected is every way TestLoadRejects makes a std/builtin file fail to
// declare builtins, and what the error must say.
var rejected = []struct {
	name string
	src  string
	want []string
}{
	{"lex error", "package builtin\n\n\x00\n", []string{"builtin.tck:3:1: error:"}},
	{
		"syntax error",
		"package builtin\n\nextern(\"tack_p\") func p(s string\n",
		[]string{"builtin.tck:3:"},
	},
	{"truncated", "package built", []string{"builtin.tck:1:"}},
	{
		"keyword name",
		"package builtin\n\nextern(\"tack_if\") func if()\n",
		[]string{"builtin.tck:3:"},
	},
	{
		"extern with body", "package builtin\n\nextern(\"tack_p\") func p() {\n}\n",
		[]string{"extern func p cannot have a body"},
	},
	{
		"wrong package",
		"package main\n\nextern(\"tack_p\") func p()\n",
		[]string{
			"builtin.tck:1:9: error: a file of std/builtin must be package builtin, not main",
		},
	},
	{
		"import",
		"package builtin\n\nimport fmt from \"std/fmt\"\n\nextern(\"tack_p\") func p()\n",
		[]string{"a file of std/builtin cannot import a package"},
	},
	{
		"not extern",
		"package builtin\n\nfunc p() {\n}\n",
		[]string{
			"builtin.tck:3:6: error: builtin p must be an extern func; its body is C, in a .c file of std/builtin",
		},
	},
	{
		"no body, not extern", "package builtin\n\nfunc p()\n",
		[]string{"builtin p must be an extern func"},
	},
	{
		"var", "package builtin\n\nvar x = 1\n",
		[]string{"std/builtin declares only builtins, as extern funcs; var x is not one"},
	},
	{
		"type", "package builtin\n\ntype T struct {\n}\n",
		[]string{"std/builtin declares only builtins, as extern funcs; type T is not one"},
	},
	{
		"no C name",
		"package builtin\n\nextern func print(s string)\n",
		[]string{
			`builtin.tck:3:1: error: builtin print has no C name; give it one, extern("tack_builtin_print")`,
		},
	},
	{
		"C name not an identifier", "package builtin\n\nextern(\"tack builtin\") func p()\n",
		[]string{`builtin p cannot be named "tack builtin" in C: it is not a C identifier`},
	},
	{
		"empty C name", "package builtin\n\nextern(\"\") func p()\n",
		[]string{`builtin p cannot be named "" in C: it is not a C identifier`},
	},
	{
		"C keyword", "package builtin\n\nextern(\"while\") func p()\n",
		[]string{"builtin p cannot be named while in C: it is a C keyword"},
	},
	{
		"C name without tack_", "package builtin\n\nextern(\"printf\") func p()\n",
		[]string{"builtin p cannot be named printf in C: a builtin's C name starts tack_"},
	},
	{
		"type name", "package builtin\n\nextern(\"tack_int64\") func int64()\n",
		[]string{"builtin int64 cannot be named after a basic type"},
	},
	{
		"len", "package builtin\n\nextern(\"tack_len\") func len(s string)\n",
		[]string{"builtin len cannot be declared: the compiler implements len itself"},
	},
	{
		"main", "package builtin\n\nextern(\"tack_main\") func main()\n",
		[]string{"a builtin cannot be named main"},
	},
	{
		"blank", "package builtin\n\nextern(\"tack_blank\") func _()\n",
		[]string{"a builtin cannot be named _"},
	},
	{
		"pub", "package builtin\n\npub extern(\"tack_p\") func p()\n",
		[]string{"builtin p cannot be pub; every builtin is visible everywhere"},
	},
	{
		"result", "package builtin\n\nextern(\"tack_p\") func p() int64\n",
		[]string{"builtin p cannot return a value; builtins return nothing"},
	},
	{
		"mut param", "package builtin\n\nextern(\"tack_p\") func p(mut x int64)\n",
		[]string{"parameter x of builtin p cannot be mut"},
	},
	{
		"variadic param", "package builtin\n\nextern(\"tack_p\") func p(s string, xs ...int64)\n",
		[]string{"parameter xs of builtin p cannot be variadic"},
	},
	{
		"any param", "package builtin\n\nextern(\"tack_p\") func p(a any)\n",
		[]string{"parameter a of builtin p cannot be any"},
	},
	{
		"variadic any param", "package builtin\n\nextern(\"tack_p\") func p(xs ...any)\n",
		[]string{"parameter xs of builtin p cannot be variadic"},
	},
	{
		"view param", "package builtin\n\nextern(\"tack_p\") func p(v []int64)\n",
		[]string{"parameter v of builtin p must be of a basic type, such as int64 or string"},
	},
	{
		"array param", "package builtin\n\nextern(\"tack_p\") func p(v [2]int64)\n",
		[]string{"parameter v of builtin p must be of a basic type"},
	},
	{
		"unknown param type",
		"package builtin\n\nextern(\"tack_p\") func p(x Point)\n",
		[]string{
			"parameter x of builtin p must be of a basic type, such as int64 or string, not Point",
		},
	},
	{
		"int param", "package builtin\n\nextern(\"tack_p\") func p(x int)\n",
		[]string{"not int"},
	},
	{
		"duplicate name",
		"package builtin\n\nextern(\"tack_a\") func p()\nextern(\"tack_b\") func p(x int64)\n",
		[]string{
			"builtin.tck:4:23: error: builtin p is declared twice",
			"builtin p is first declared here",
		},
	},
	{
		"duplicate C name",
		"package builtin\n\nextern(\"tack_a\") func p()\nextern(\"tack_a\") func q()\n",
		[]string{"builtins q and p both have the C name tack_a", "builtin p is declared here"},
	},
	{
		"every problem",
		"package builtin\n\nvar x = 1\n\nfunc p() {\n}\n\nextern func q() int64\n",
		[]string{
			"var x is not one", "builtin p must be an extern func", "builtin q has no C name",
			"builtin q cannot return a value",
		},
	},
}

// Every way a std/builtin file can fail to declare builtins is a
// diagnostic in that file, never a panic, and every one is reported, not
// just the first.
func TestLoadRejects(t *testing.T) {
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeStd(t, map[string]string{"builtin.tck": tt.src})
			set, err := builtin.Load(dir)
			if err == nil {
				t.Fatalf("Load accepted %q: %+v", tt.src, set.Funcs())
			}
			var diags *builtin.DiagnosticsError
			if !errors.As(err, &diags) {
				t.Fatalf("Load = %T %v, want a *builtin.DiagnosticsError", err, err)
			}
			msg := err.Error()
			if !strings.Contains(msg, filepath.Join(dir, "builtin.tck")) {
				t.Errorf("the error does not name the file %s:\n%s", dir, msg)
			}
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("the error does not say %q:\n%s", w, msg)
				}
			}
		})
	}
}

// A builtin declared twice across two files is reported in the second,
// with a note in the first.
func TestLoadRejectsDuplicateAcrossFiles(t *testing.T) {
	dir := writeStd(t, map[string]string{
		"a.tck": "package builtin\n\nextern(\"tack_a\") func p()\n",
		"b.tck": "package builtin\n\nextern(\"tack_b\") func p()\n",
	})
	_, err := builtin.Load(dir)
	if err == nil {
		t.Fatal("Load accepted p declared in two files")
	}
	msg := err.Error()
	for _, w := range []string{
		filepath.Join(dir, "b.tck") + ":3:23: error: builtin p is declared twice",
		filepath.Join(dir, "a.tck") + ":3:23: note: builtin p is first declared here",
	} {
		if !strings.Contains(msg, w) {
			t.Errorf("the error does not say %q:\n%s", w, msg)
		}
	}
}

// Every file of std/builtin is lexed, and every file parsed, before Load
// stops, so a problem in each is reported.
func TestLoadReportsEveryFile(t *testing.T) {
	dir := writeStd(t, map[string]string{
		"a.tck": "package builtin\n\nextern(\"tack_a\") func a(\n",
		"b.tck": "package builtin\n\nextern(\"tack_b\") func b(\n",
	})
	_, err := builtin.Load(dir)
	if err == nil {
		t.Fatal("Load accepted two broken files")
	}
	for _, f := range []string{"a.tck", "b.tck"} {
		if !strings.Contains(err.Error(), filepath.Join(dir, f)+":") {
			t.Errorf("no diagnostic in %s:\n%v", f, err)
		}
	}
}

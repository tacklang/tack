package loader_test

import (
	"bytes"
	"io/fs"
	"path"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
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

// compileLoaded runs the rest of the pipeline on a loaded program, up to
// the first stage that reports anything, so a fuzzed program reaches every
// stage it can.
func compileLoaded(res *loader.Result) {
	if len(resolver.New(res.Program, stdtest.MustBuiltins()).Resolve()) > 0 {
		return
	}
	if len(typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check()) > 0 {
		return
	}
	_, _ = c.New().Generate(res.Program, stdtest.MustBuiltins())
}

// loadSeeds are the file trees FuzzLoad starts from, in treeFromBytes's
// form.
var loadSeeds = []string{
	"tack.mod\nmodule calc\n\x00main.tck\npackage main\nfunc main() {}\n",
	"a/b/c/main.tck\npackage main\nfunc main() {}\n\x00tack.mod\nmodule m\n",
	"lexer/a.tck\npackage lexer\n\x00tack.mod\nmodule calc\n",
	"x/a.tck\npackage main\n\x00x/b.tck\npackage other\n\x00x/tack.mod\nmodule std\n",
	"my-tools/a.tck\npackage mytools\n\x00tack.mod\nmodule calc\n",
	"a\xff/b.tck\npackage main\n\x00tack.mod\nmodule\n",
	"p/.h.tck\npackage main\n\x00p/tack.mod\nmodule p\nmodule q\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport a from \"m/a\"\nfunc main() { exit(a.N) }\n" +
		"\x00a/a.tck\npackage a\nimport b from \"m/b\"\nvar N = b.N + 1\n\x00b/b.tck\npackage b\nvar N int64 = 2\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport a from \"m/a\"\nfunc main() {}\n" +
		"\x00a/a.tck\npackage a\nimport b from \"m/b\"\n\x00b/b.tck\npackage b\nimport a from \"m/a\"\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport t from \"m/t\"\n" +
		"func main() { x := t.T{a: 1}\nexit(x.a) }\n\x00t/t.tck\npackage t\ntype T struct { a int64 }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport m from \"m\"\nfunc main() {}\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport f from \"std/fmt\"\nfunc main() { f.P() }\n" +
		"\x00std/fmt/fmt.tck\npackage fmt\nimport t from \"std/t\"\npub func P() { t.Q() }\n" +
		"\x00std/t/t.tck\npackage t\nlib func Q() {}\n\x00std/t/t.c\nint q;\n",
	"std/tool/main.tck\npackage main\nimport f from \"std/fmt\"\nimport m from \"m/x\"\nfunc main() {}\n" +
		"\x00std/tack.mod\nmodule std\n\x00std/fmt/fmt.tck\npackage fmt\nimport f from \"std/fmt\"\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport f from \"std/fmt\"\nfunc main() { f.p() }\n" +
		"\x00std/fmt/fmt.tck\npackage fmt\nimport b from \"std/builtin\"\nimport m from \"m\"\nfunc p() {}\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport t from \"m/t\"\n" +
		"func (c C) f() int64 { return c.n }\ntype C struct { n int64 }\n" +
		"func main() { mut x := t.New()\nmut x.Add(1)\nexit(int32(x.Get() - x.Get()))\n" +
		"c := C{n: 1}\nexit(int32(c.f())) }\n" +
		"\x00t/t.tck\npackage t\npub type T struct { n int64 }\npub func New() T { return T{n: 0} }\n" +
		"pub func (mut t T) Add(k int64) { t.n += k }\nlib func (t T) Get() int64 { return t.n }\n" +
		"func (t T) hidden() {}\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport s from \"m/s\"\n" +
		"type C struct { n int64 }\nfunc (mut c C) put(w string) error { c.n++\nreturn noerror }\n" +
		"func (c C) count() int64 { return c.n }\ntype K interface { count() int64 }\n" +
		"func k[A K, B K](a A, b B) int64 { return a.count() + k(b, a) }\n" +
		"func main() { mut t := s.New()\nerr := s.Feed(mut t, \"a\")\nexit(err.code)\n" +
		"c := C{n: 1}\nif k(c, t) > 0 { exit(1) } }\n" +
		"\x00s/s.tck\npackage s\npub type Sink interface { mut put(w string) error\ncount() int64 }\n" +
		"pub func Feed[S Sink](mut s S, ws ...string) error { for _, w := range ws { e := mut s.put(w)\n" +
		"if e.code != 0 { return e } }\nreturn noerror }\npub type T struct { n int64 }\n" +
		"pub func New() T { return T{n: 0} }\npub func (mut t T) put(w string) error { t.n++\nreturn noerror }\n" +
		"lib func (t T) count() int64 { return t.n }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport a from \"m/a\"\n" +
		"type V union { items list[int64]\nbag B\nnone }\ntype B struct { l list[uint8] }\n" +
		"func n(v V) int64 { switch v {\ncase items(l):\nreturn len(l)\ncase bag, none:\nreturn 0 } }\n" +
		"func main() { mut v := V.items(make(list[int64], c_allocator))\nv = V.none\n" +
		"t := a.T.num(n(v))\nswitch t {\ncase num(k):\nif k > 0 { exit(1) }\ndefault:\n}\n" +
		"if t == a.T.eof { exit(2) } }\n" +
		"\x00a/a.tck\npackage a\npub type T union { num int64\nword string\neof }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport a from \"m/a\"\n" +
		"type V union { items list[int64]\nnone }\n" +
		"func (v V) len() int64 { switch v {\ncase items(l):\nreturn len(l)\ncase none:\nreturn 0 } }\n" +
		"func (mut v V) reset() { v = V.none }\n" +
		"func main() { mut v := V.items(make(list[int64], c_allocator))\nn := a.Larger(v, a.T.num(2))\n" +
		"mut v.reset()\nt := a.T.eof\nif n + v.len() + t.len() != 2 { exit(1) } }\n" +
		"\x00a/a.tck\npackage a\npub type L interface { len() int64 }\n" +
		"pub func Larger[A L, B L](a A, b B) int64 { if a.len() > b.len() { return a.len() }\nreturn b.len() }\n" +
		"pub type T union { num int64\neof }\n" +
		"pub func (t T) len() int64 { switch t {\ncase num(n):\nreturn n\ndefault:\nreturn 0 } }\n" +
		"func (mut t T) clear() { t = T.eof }\n",
	"main.tck\npackage main\ntype V union { items list[int64]\nnone }\n" +
		"type S struct { vs list[V]\nn int64 }\n" +
		"func total(ls []list[int64]) int64 { mut t := 0\nfor _, l := range ls { t += len(l) }\nreturn t }\n" +
		"func main() { mut ls := make(list[list[int64]], c_allocator)\nmut l := make(list[int64], c_allocator)\n" +
		"e := append(mut l, 1, 2)\ne2 := append(mut ls, l, make(list[int64], c_allocator))\n" +
		"if len(ls) > 1 { e3 := append(mut ls[1], 3)\nls[0] = make(list[int64], c_allocator)\nif e3.code != 0 { exit(3) } }\n" +
		"x, ok := pop(mut ls)\nif ok && len(x) != 1 { exit(4) }\ny, ok2 := remove(mut ls, 0)\nif !ok2 { return }\n" +
		"mut s := S{vs: make(list[V], c_allocator), n: len(y)}\ne4 := append(mut s.vs, V.items(y), V.none)\n" +
		"for _, v := range s.vs { switch v {\ncase items(is):\ns.n += len(is)\ncase none:\n} }\n" +
		"if e.code != 0 || e2.code != 0 || e4.code != 0 || total(ls) != 0 { exit(1) } }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport t from \"m/t\"\n" +
		"func rest(v []uint8) []uint8 { if len(v) == 0 { return v }\nreturn v[1:] }\n" +
		"type B interface { Bytes() []uint8 }\nfunc show[T B](x T) []uint8 { return x.Bytes() }\n" +
		"func main() { mut x, err := t.Of(\"ab\")\nif err.code != 0 { return }\n" +
		"c, ok := x.Bytes()[1]\nmut n := len(rest(x.Bytes())) + len(show(x))\n" +
		"for _, b := range x.Bytes() { n += b }\ne := mut x.Push(c)\n" +
		"if !ok || n == 0 || e.code != 0 { exit(1) } }\n" +
		"\x00t/t.tck\npackage t\npub type T struct { b list[uint8] }\n" +
		"pub func Of(s string) (T, error) { mut b := make(list[uint8], c_allocator)\n" +
		"err := append(mut b, []uint8(s)...)\nreturn T{b: b}, err }\n" +
		"pub func (t T) Bytes() []uint8 { return t.b }\n" +
		"pub func (mut t T) Push(c uint8) error { return append(mut t.b, c) }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport text from \"std/text\"\n" +
		"func main() { mut t, err := text.from_bytes([]uint8(\"\\x00\\xff\"), c_allocator)\n" +
		"if err.code != 0 { return }\ne := mut t.append_byte(1)\n" +
		"if e.code != 0 || t.len() != 3 || t.hash() == 0 || !t.equal(t.bytes()) { exit(1) } }\n" +
		"\x00std/tack.mod\nmodule std\n\x00std/text/text.tck\npackage text\n" +
		"var fnv_prime uint64 = 1099511628211\npub type Text struct { b list[uint8] }\n" +
		"pub func new(alloc Allocator) Text { return Text{b: make(list[uint8], alloc)} }\n" +
		"pub func from_bytes(s []uint8, alloc Allocator) (Text, error) { mut b := make(list[uint8], alloc)\n" +
		"err := append(mut b, s...)\nreturn Text{b: b}, err }\n" +
		"pub func (t Text) len() int64 { return len(t.b) }\n" +
		"pub func (t Text) equal(s []uint8) bool { return len(t.b) == len(s) }\n" +
		"pub func (t Text) hash() uint64 { mut h uint64 = 14695981039346656037\n" +
		"for _, c := range t.b { h = (h ^ uint64(c)) * fnv_prime }\nreturn h }\n" +
		"pub func (mut t Text) append_byte(c uint8) error { return append(mut t.b, c) }\n" +
		"pub func (t Text) bytes() []uint8 { return t.b }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport os from \"std/os\"\n" +
		"func main() { d, err := os.read_file([]uint8(\"a\\x00\"), c_allocator)\n" +
		"if err.code != 0 { return }\nif len(d) > 0 { exit(1) } }\n" +
		"\x00std/tack.mod\nmodule std\n\x00std/os/os.tck\npackage os\n" +
		"extern(\"tack_os_open\") func c_open(path []uint8, mode int32) int64\n" +
		"extern(\"tack_os_close\") func c_close(fd int32) int64\n" +
		"extern(\"tack_os_read\") func c_read(fd int32, mut l list[uint8]) int64\n" +
		"extern(\"tack_os_error\") func c_error(e int64) error\n" +
		"pub func read_file(path []uint8, alloc Allocator) (list[uint8], error) {\n" +
		"empty := make(list[uint8], alloc)\nr := c_open(path, 0)\nif r < 0 { return empty, c_error(-r) }\n" +
		"fd, _ := int32(r)\ndefer { c_close(fd) }\nmut data := make(list[uint8], alloc)\n" +
		"for { e := reserve(mut data, 65536)\nif e.code != 0 { return empty, e }\n" +
		"n := c_read(fd, mut data)\nif n == 0 { return data, noerror }\n" +
		"if n < 0 { return empty, c_error(-n) } } }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\nimport os from \"std/os\"\n" +
		"func main() { mut o := make(list[uint8], c_allocator)\nmut e := make(list[uint8], c_allocator)\n" +
		"s, err := os.run(mut o, mut e, c_allocator)\nif err.code != 0 { return }\nif s != 0 { exit(1) } }\n" +
		"\x00std/tack.mod\nmodule std\n\x00std/os/os.tck\npackage os\n" +
		"extern(\"tack_os_pipe\") func c_pipe(mut fds [2]int32) int64\n" +
		"extern(\"tack_os_close\") func c_close(fd int32) int64\n" +
		"pub func run(mut out list[uint8], mut err_out list[uint8], alloc Allocator) (int64, error) {\n" +
		"mut p [2]int32\nr := c_pipe(mut p)\nif r < 0 { return -1, error{code: 3, msg: \"pipe\"} }\n" +
		"mut a := p[0]\nc_close(p[1])\nmut got := make(list[uint8], alloc)\n" +
		"for a >= 0 { c_close(a)\na = -1 }\ne := reserve(mut out, len(got))\n" +
		"if e.code != 0 { return -1, e }\ne2 := append(mut out, got...)\n" +
		"e3 := append(mut err_out, 0)\nif e2.code != 0 || e3.code != 0 { return -1, e2 }\nreturn 0, noerror }\n",
	"tack.mod\nmodule m\n\x00main.tck\npackage main\ntype S struct { l list[uint8]\nn int64 }\n" +
		"type U union { items list[int64]\nnone }\n" +
		"func main() { mut a := make(list[uint8], c_allocator)\nb := make(list[uint8], c_allocator)\n" +
		"e := append(mut a, 1, 2)\nmut ll := make(list[list[uint8]], c_allocator)\n" +
		"x := S{l: a, n: 1}\ny := S{l: b, n: 1}\nu := U.none\nv := U.none\n" +
		"if e.code != 0 || x == y || u != v || ll != ll { exit(1) } }\n",
}

// FuzzLoad loads random file trees, with odd names, deep nesting, imports
// and cycles of them, tack.mod files anywhere, and a standard library in
// the tree's directory std, which a target may be in, and checks the loader
// never panics, and either fails with an error or gives a program exactly
// when it gives no diagnostic, with every diagnostic's span in a file it
// read. A program it gives is compiled on, through code generation, which
// must not panic either.
func FuzzLoad(f *testing.F) {
	for _, seed := range loadSeeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fsys, target := treeFromBytes(data)
		l := &loader.Loader{
			FS:       loader.FromFS(fsys),
			Builtins: stdtest.MustBuiltins(),
			Std:      &loader.Std{Dir: "std"},
		}
		res, err := l.Dir(target)
		if err != nil {
			return
		}
		if (res.Program == nil) == (len(res.Diags) == 0) {
			t.Fatalf("program %v with %d diagnostics", res.Program, len(res.Diags))
		}
		if res.Program != nil {
			compileLoaded(res)
		}
		for _, d := range res.Diags {
			file := res.Files.Lookup(d.Span.File)
			if file == nil || d.Span.Start < 0 || d.Span.End > len(file.Data) {
				t.Fatalf("diagnostic %q has span %v outside every file read", d.Message, d.Span)
			}
		}
	})
}

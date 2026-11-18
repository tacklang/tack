package porttest

import (
	"bytes"
	"compress/zlib"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/builtin"
)

// symbolsC is a C file whose object holds every kind of symbol the
// build's reader tells apart: a function and a variable defined, a weak
// definition, a weak reference, a common symbol under -fcommon, an
// absolute one, a static one, which is no external symbol, and calls
// and reads of symbols defined elsewhere.
const symbolsC = `int defined_var = 1;
int common_var;
__attribute__((weak)) int weak_def(void) { return 1; }
extern int weak_ref(void) __attribute__((weak));
extern int elsewhere(void);
extern int other_var;
static int hidden(void) { return 2; }
__asm__(".globl abs_sym\n.set abs_sym, 42");
int defined_fn(void) { return elsewhere() + hidden() + other_var + (weak_ref ? weak_ref() : 0); }
`

// TestObjectSymbols compares the port's --port-symbols, what its ELF
// reader finds an object defines and leaves undefined, with Go's
// readSymbols (see goObjectSymbols), on objects clang compiles from the
// runtime's C files and from symbolsC, at every -O level, with -g too
// and with its debugging sections compressed, and symbolsC under
// -fcommon, which must read the same; and on damaged copies of them:
// truncated at the header, in it, at the section headers and past them,
// and with bytes of the header, of each section header and random ones
// changed, a fixed seed's. Of a damaged copy, the port must read what Go
// reads or say it cannot read it, which the build declines, and must
// say so wherever Go fails. It checks extended section numbering too
// (see checkExtended). It runs only as TestPort does.
func TestObjectSymbols(t *testing.T) {
	ready(t)
	dir := t.TempDir()
	objs := objectsToRead(t, dir)
	for _, obj := range objs {
		want, err := goObjectSymbols(obj)
		if err != nil {
			t.Fatalf("Go cannot read %s: %v", obj, err)
		}
		if got := portObjectSymbols(t, obj); got != want {
			t.Errorf("%s: the port reads\n%s\nwant\n%s", filepath.Base(obj), got, want)
		}
	}
	r := rand.New(rand.NewPCG(5, 17))
	// The first, and symbolsC's with compressed debugging sections, with
	// -g and with -fcommon.
	for k, obj := range []string{objs[0], objs[len(objs)-3], objs[len(objs)-2], objs[len(objs)-1]} {
		data, err := os.ReadFile(obj) //nolint:gosec // G304: the test's own object
		if err != nil {
			t.Fatal(err)
		}
		for n, bad := range damaged(data, r) {
			p := filepath.Join(dir, fmt.Sprintf("bad%d-%d.o", k, n))
			if err := os.WriteFile(p, bad, 0o600); err != nil {
				t.Fatal(err)
			}
			checkDamagedSymbols(t, p)
		}
	}
	checkExtended(t, dir)
	// symbolsC's objects are the last nine, its -O0 one first.
	checkZdebug(t, dir, objs[len(objs)-9])
}

// checkZdebug checks the port's reading of copies of obj, an object of
// symbolsC, whose symbol table, or the string table it links to, is
// named .zdebug, which debug/elf's Section.Open decompresses as zlib
// when the section starts ZLIB and a big-endian size: named so but
// holding its bytes as they are, which Go reads as they are and the
// port must too; holding them compressed, which Go decompresses and the
// port must decline; and holding ZLIB and no zlib stream, where Go fails
// and the port must decline. The string table case moves the table to
// the .comment section, renamed, since clang's symbol and section names
// share one table, which Go reads as it is before it names any section.
func checkZdebug(t *testing.T, dir, obj string) {
	t.Helper()
	data, err := os.ReadFile(obj) //nolint:gosec // G304: the test's own object
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "zdebug.o")
	for _, zc := range zdebugCopies(t, data) {
		if err := os.WriteFile(p, zc.data, 0o600); err != nil {
			t.Fatal(err)
		}
		want, goErr := goObjectSymbols(p)
		if why := zdebugVerdict(zc, want, goErr, portObjectSymbols(t, p)); why != "" {
			t.Errorf("%s: %s", zc.name, why)
		}
	}
}

// zdebugVerdict returns why the port's reading got of zc, beside Go's,
// want or goErr, is not what checkZdebug wants, or "".
func zdebugVerdict(zc zdebugCopy, want string, goErr error, got string) string {
	switch {
	case zc.raw && (goErr != nil || got != want):
		return fmt.Sprintf("Go reads %q (%v), the port %q, want the same", want, goErr, got)
	case zc.raw:
		return ""
	case zc.goReads && goErr != nil:
		return fmt.Sprintf("Go fails (%v), want it to read the decompressed table", goErr)
	case !zc.goReads && goErr == nil:
		return fmt.Sprintf("Go reads %q, want it to fail", want)
	case got != "unreadable\n":
		return fmt.Sprintf("the port reads %q, want it to decline", got)
	}
	return ""
}

// zdebugCopy is a copy of an object checkZdebug reads: its name, its
// bytes, whether its .zdebug section holds its bytes as they are, and,
// when it does not, whether Go reads it.
type zdebugCopy struct {
	name    string
	data    []byte
	raw     bool
	goReads bool
}

// zdebugObject is an ELF64 object zdebugCopies changes: its bytes, where
// its section headers are, where its section name string table is, and
// the indexes of its symbol table, of the string table that links to
// and of its .comment section.
type zdebugObject struct {
	data                 []byte
	symBytes             []byte
	strtabBytes          []byte
	shoff, shstrOff      int
	sym, strtab, comment int
}

// newZdebugObject reads data, an ELF64 object of symbolsC's, for
// zdebugCopies.
func newZdebugObject(t *testing.T, data []byte) zdebugObject {
	t.Helper()
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	o := zdebugObject{
		data:     data,
		shoff:    int(binary.LittleEndian.Uint64(data[40:])),
		shstrOff: int(f.Sections[binary.LittleEndian.Uint16(data[62:])].Offset),
	}
	for i, s := range f.Sections {
		switch s.Name {
		case ".symtab":
			o.sym = i
		case ".comment":
			o.comment = i
		}
	}
	if o.sym == 0 || o.comment == 0 {
		t.Fatal("the object has no .symtab or no .comment")
	}
	o.strtab = int(f.Sections[o.sym].Link)
	bytesOf := func(i int) []byte {
		s := f.Sections[i]
		return bytes.Clone(data[s.Offset : s.Offset+s.Size])
	}
	o.symBytes, o.strtabBytes = bytesOf(o.sym), bytesOf(o.strtab)
	return o
}

// rename gives section i of b, a copy of o's bytes, the name to, written
// over its own.
func (o zdebugObject) rename(b []byte, i int, to string) {
	at := o.shstrOff + int(binary.LittleEndian.Uint32(b[o.shoff+i*64:]))
	copy(b[at:at+len(to)], to)
}

// place appends payload to b and makes it section i's bytes.
func (o zdebugObject) place(b []byte, i int, payload []byte) []byte {
	h := o.shoff + i*64
	binary.LittleEndian.PutUint64(b[h+24:], uint64(len(b)))
	binary.LittleEndian.PutUint64(b[h+32:], uint64(len(payload)))
	return append(b, payload...)
}

// withSym returns o with its symbol table named .zdebug and, unless
// payload is nil, holding payload.
func (o zdebugObject) withSym(payload []byte) []byte {
	b := bytes.Clone(o.data)
	o.rename(b, o.sym, ".zdebug")
	if payload == nil {
		return b
	}
	return o.place(b, o.sym, payload)
}

// withStr returns o with its .comment section named .zdebugc, an
// SHT_STRTAB of no flags holding payload, and its symbol table linked to
// it.
func (o zdebugObject) withStr(payload []byte) []byte {
	b := bytes.Clone(o.data)
	o.rename(b, o.comment, ".zdebugc")
	h := o.shoff + o.comment*64
	binary.LittleEndian.PutUint32(b[h+4:], 3)
	binary.LittleEndian.PutUint64(b[h+8:], 0)
	binary.LittleEndian.PutUint32(b[o.shoff+o.sym*64+40:], uint32(o.comment))
	return o.place(b, o.comment, payload)
}

// zlibbed returns raw as debug/elf's .zdebug sections hold it: ZLIB, its
// length, big-endian, and its zlib stream.
func zlibbed(raw []byte) []byte {
	var z bytes.Buffer
	z.WriteString("ZLIB")
	_ = binary.Write(&z, binary.BigEndian, uint64(len(raw)))
	w := zlib.NewWriter(&z)
	_, _ = w.Write(raw)
	_ = w.Close()
	return z.Bytes()
}

// pad24 returns b with zeros after it up to a multiple of 24 bytes, the
// size of a symbol, so that a reader that took a .zdebug symbol table
// as it is would find symbols in it rather than fail.
func pad24(b []byte) []byte {
	return append(b, make([]byte, (24-len(b)%24)%24)...)
}

// zdebugCopies returns the copies of data, an ELF64 object, that
// checkZdebug reads.
func zdebugCopies(t *testing.T, data []byte) []zdebugCopy {
	t.Helper()
	o := newZdebugObject(t, data)
	notZlib := append([]byte("ZLIB\x00\x00\x00\x00\x00\x00\x00\x30"), bytes.Repeat([]byte{0xff}, 48)...)
	return []zdebugCopy{
		{name: "symtab .zdebug, as it is", data: o.withSym(nil), raw: true},
		{name: "symtab .zdebug, zlib", data: o.withSym(pad24(zlibbed(o.symBytes))), goReads: true},
		{name: "symtab .zdebug, ZLIB and no zlib", data: o.withSym(pad24(notZlib))},
		{name: "strtab .zdebug, as it is", data: o.withStr(o.strtabBytes), raw: true},
		{name: "strtab .zdebug, zlib", data: o.withStr(zlibbed(o.strtabBytes)), goReads: true},
		{name: "strtab .zdebug, ZLIB and no zlib", data: o.withStr(notZlib)},
	}
}

// objectsToRead compiles, into dir, the objects TestObjectSymbols reads:
// each of the runtime's C files, as builtin.RuntimeCFiles gives them,
// against its header, and symbolsC, at every -O level, with -g at -O2,
// with -g and its debugging sections compressed, and with -fcommon at
// -O0, symbolsC's last, its -fcommon one the very last. It returns
// their paths.
func objectsToRead(t *testing.T, dir string) []string {
	t.Helper()
	include := filepath.Join(dir, "include")
	header := filepath.Join(include, filepath.FromSlash(builtin.HeaderPath))
	if err := os.MkdirAll(filepath.Dir(header), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, builtin.RuntimeHeader(), 0o600); err != nil {
		t.Fatal(err)
	}
	files := builtin.RuntimeCFiles()
	srcs := make([]string, 0, len(files)+1)
	for _, f := range files {
		src := filepath.Join(dir, f.Name)
		if err := os.WriteFile(src, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, src)
	}
	sym := filepath.Join(dir, "symbols.c")
	if err := os.WriteFile(sym, []byte(symbolsC), 0o600); err != nil {
		t.Fatal(err)
	}
	srcs = append(srcs, sym)
	levels := [][]string{
		{"-O0"}, {"-O1"}, {"-O2"}, {"-O3"}, {"-Os"}, {"-Oz"}, {"-O2", "-g", "-gz=zlib"},
		{"-O2", "-g"}, {"-O0", "-fcommon"},
	}
	objs := make([]string, 0, len(srcs)*len(levels))
	for i, src := range srcs {
		for _, flags := range levels {
			obj := filepath.Join(dir, fmt.Sprintf("o%d%s.o", i, strings.Join(flags, "")))
			compileObject(t, obj, src, include, flags...)
			objs = append(objs, obj)
		}
	}
	return objs
}

// compileObject compiles src to obj with clang, warnings off, include
// on the include path, and flags, failing t when it cannot.
func compileObject(t *testing.T, obj, src, include string, flags ...string) {
	t.Helper()
	args := append([]string{"-c", "-w", "-I", include}, flags...)
	args = append(args, "-o", obj, src)
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: the test's own clang
		t.Fatalf("clang %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// checkExtended checks the port's reading of an object with extended
// section numbering, testdata/build-probes/many-sections' many.c
// compiled into dir, whose e_shnum is 0 and whose count is the first
// section header's size: as it is, and with its section name string
// table's header copied to index SHN_LORESERVE and e_shstrndx made
// SHN_XINDEX, so the index is the first section header's link, each of
// which the port must read as Go does; and changed where extended
// numbering can go wrong, each of which it must read as Go does or
// decline, and decline wherever Go fails (see extendedDamaged).
func checkExtended(t *testing.T, dir string) {
	t.Helper()
	obj := filepath.Join(dir, "many.o")
	compileObject(t, obj, filepath.Join(env.root, "porttest", "testdata", "build-probes",
		"many-sections", "many.c"), dir, "-O0")
	want, err := goObjectSymbols(obj)
	if err != nil {
		t.Fatalf("Go cannot read many.o: %v", err)
	}
	if got := portObjectSymbols(t, obj); got != want {
		t.Errorf("many.o: the port reads %d bytes, Go %d: %s", len(got), len(want),
			firstDiff([]byte(want), []byte(got)))
	}
	data, err := os.ReadFile(obj) //nolint:gosec // G304: the test's own object
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "many-bad.o")
	for k, bad := range extendedDamaged(t, data) {
		if err := os.WriteFile(p, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		if k == 0 {
			want, err := goObjectSymbols(p)
			if err != nil {
				t.Fatalf("Go cannot read many.o with e_shstrndx SHN_XINDEX: %v", err)
			}
			if got := portObjectSymbols(t, p); got != want {
				t.Errorf("many.o with e_shstrndx SHN_XINDEX: %s", firstDiff([]byte(want), []byte(got)))
			}
			continue
		}
		checkDamagedSymbols(t, p)
	}
}

// extendedDamaged returns copies of data, an ELF64 object whose e_shnum
// is 0, each changed once: first with its section name string table's
// header copied to index SHN_LORESERVE, e_shstrndx SHN_XINDEX and the
// first section header's link that index, which Go reads; then with the
// first section header's size below SHN_LORESERVE and past what the
// file holds, its type not SHT_NULL, e_shstrndx SHN_XINDEX with a link
// below SHN_LORESERVE and past the count, and with an e_shstrndx past
// SHN_LORESERVE that the count leaves out, e_phnum PN_XNUM with an info
// below it and with one of PN_XNUM, program headers of 56 bytes from the
// file's start, and the file cut short in the first section header.
func extendedDamaged(t *testing.T, data []byte) [][]byte {
	t.Helper()
	le := func(b []byte, at, n int) uint64 {
		var v uint64
		for j := n - 1; j >= 0; j-- {
			v = v<<8 | uint64(b[at+j])
		}
		return v
	}
	put := func(b []byte, at, n int, v uint64) {
		for j := range n {
			b[at+j] = byte(v >> (8 * j))
		}
	}
	shoff := int(le(data, 40, 8))
	shstrndx := int(le(data, 62, 2))
	if le(data, 60, 2) != 0 || shoff+64 > len(data) {
		t.Fatalf("many.o has e_shnum %d, want 0, extended numbering", le(data, 60, 2))
	}
	count := int(le(data, shoff+32, 8))
	var out [][]byte
	edit := func(f func(b []byte)) {
		b := bytes.Clone(data)
		f(b)
		out = append(out, b)
	}
	const loreserve, xindex = 0xff00, 0xffff
	edit(func(b []byte) {
		copy(b[shoff+loreserve*64:shoff+loreserve*64+64], b[shoff+shstrndx*64:shoff+shstrndx*64+64])
		put(b, 62, 2, xindex)
		put(b, shoff+40, 4, loreserve)
	})
	edit(func(b []byte) { put(b, shoff+32, 8, loreserve-1) })
	edit(func(b []byte) { put(b, shoff+32, 8, 1<<40) })
	edit(func(b []byte) { put(b, shoff+4, 4, 1) })
	edit(func(b []byte) { put(b, 62, 2, xindex); put(b, shoff+40, 4, 5) })
	edit(func(b []byte) { put(b, 62, 2, xindex); put(b, shoff+40, 4, uint64(count+1)) })
	edit(func(b []byte) { put(b, 62, 2, 0xfffe); put(b, shoff+32, 8, loreserve) })
	edit(func(b []byte) { put(b, 56, 2, xindex); put(b, 54, 2, 56); put(b, shoff+44, 4, 3) })
	edit(func(b []byte) { put(b, 56, 2, xindex); put(b, 54, 2, 56); put(b, shoff+44, 4, xindex) })
	edit(func(b []byte) { put(b, 56, 2, 1); put(b, 54, 2, 56); put(b, 32, 8, 0) })
	out = append(out, bytes.Clone(data[:shoff+40]))
	return out
}

// checkDamagedSymbols fails t unless the port, given the damaged object
// at p, reads what Go's readSymbols reads or says it cannot read it,
// and says so wherever Go fails.
func checkDamagedSymbols(t *testing.T, p string) {
	t.Helper()
	want, goErr := goObjectSymbols(p)
	got := portObjectSymbols(t, p)
	switch {
	case got == "unreadable\n":
	case goErr != nil:
		t.Errorf("%s: Go fails (%v), but the port reads\n%s", filepath.Base(p), goErr, got)
	case got != want:
		t.Errorf("%s: the port reads\n%s\nwant\n%s", filepath.Base(p), got, want)
	}
}

// damaged returns copies of data, an ELF64 object, each damaged once:
// truncated at lengths in and around its file header and section
// headers, with each byte of the header set to 0, 0xff and its value
// plus one, with each field of each section header set to all ones and
// to its value plus one, and with 200 random bytes set to random values,
// r's.
func damaged(data []byte, r *rand.Rand) [][]byte {
	var out [][]byte
	edit := func(f func(b []byte)) {
		b := bytes.Clone(data)
		f(b)
		out = append(out, b)
	}
	shoff := 0
	shnum := 0
	if len(data) >= 64 {
		shoff = int(uint64(data[40]) | uint64(data[41])<<8 | uint64(data[42])<<16 | uint64(data[43])<<24)
		shnum = int(data[60]) | int(data[61])<<8
	}
	for _, n := range []int{0, 1, 4, 16, 63, 64, 65, shoff, shoff + 1, shoff + 64, shoff + shnum*64 - 1, len(data) / 2, len(data) - 1} {
		if n >= 0 && n <= len(data) {
			out = append(out, bytes.Clone(data[:n]))
		}
	}
	for i := range min(64, len(data)) {
		for _, v := range []func(byte) byte{
			func(byte) byte { return 0 }, func(byte) byte { return 0xff }, func(c byte) byte { return c + 1 },
		} {
			edit(func(b []byte) { b[i] = v(b[i]) })
		}
	}
	out = append(out, sectionsDamaged(data, shoff, shnum)...)
	for range 200 {
		i := r.IntN(len(data))
		v := byte(r.Uint32())
		edit(func(b []byte) { b[i] = v })
	}
	return out
}

// sectionsDamaged returns copies of data, an ELF64 object whose shnum
// section headers start at shoff, each with one field of one section
// header set to all ones, or to its value plus one.
func sectionsDamaged(data []byte, shoff, shnum int) [][]byte {
	var out [][]byte
	edit := func(f func(b []byte)) {
		b := bytes.Clone(data)
		f(b)
		out = append(out, b)
	}
	// Each field of a section header: name, type, flags, address, offset,
	// size, link, info, alignment, entry size.
	fields := [][2]int{{0, 4}, {4, 4}, {8, 8}, {16, 8}, {24, 8}, {32, 8}, {40, 4}, {44, 4}, {48, 8}, {56, 8}}
	for s := range shnum {
		for _, f := range fields {
			at := shoff + s*64 + f[0]
			if at+f[1] > len(data) {
				continue
			}
			edit(func(b []byte) {
				for j := range f[1] {
					b[at+j] = 0xff
				}
			})
			edit(func(b []byte) { b[at]++ })
		}
	}
	return out
}

// portObjectSymbols returns what the port's --port-symbols writes for
// the object at p, failing t when it does not exit 0 with nothing on
// standard error.
func portObjectSymbols(t *testing.T, p string) string {
	t.Helper()
	r := runCompiler(t, env.portRun, env.portCache,
		input{name: filepath.Base(p), dir: filepath.Dir(p), file: filepath.Base(p)},
		[]string{"--port-symbols", filepath.Base(p)})
	if r.code != 0 || len(r.stderr) > 0 {
		t.Fatalf("--port-symbols %s exited %d, writing to standard error:\n%s", p, r.code, r.stderr)
	}
	return string(r.stdout)
}

// goObjectSymbols is the Go driver's readSymbols (tack/symbols.go),
// which is unexported, written again, rendered as --port-symbols writes
// what it reads: a line defined and the name for each symbol the object
// at p defines, then undefined and the name for each it leaves
// undefined. A Mach-O object, which the Go driver reads and the port
// does not, is rendered as nothing, which no port reading matches but
// the port's unreadable.
func goObjectSymbols(p string) (out string, err error) {
	// debug/elf indexes its sections with an e_shstrndx that extended
	// numbering can leave past the count, and panics; the Go driver
	// would too, so that is a failure the port must decline.
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("debug/elf panics: %v", r)
		}
	}()
	return goReadSymbols(p)
}

// goReadSymbols is goObjectSymbols but for a panic of debug/elf's.
func goReadSymbols(p string) (string, error) {
	if f, err := macho.Open(p); err == nil {
		_ = f.Close()
		return "", nil
	}
	f, err := elf.Open(p)
	if err != nil {
		return "", fmt.Errorf("not a Mach-O or ELF object file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	syms, err := f.Symbols()
	if errors.Is(err, elf.ErrNoSymbols) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading symbols: %w", err)
	}
	var defined, undefined strings.Builder
	for _, s := range syms {
		bind := elf.ST_BIND(s.Info)
		if bind != elf.STB_GLOBAL && bind != elf.STB_WEAK || s.Name == "" {
			continue
		}
		if s.Section == elf.SHN_UNDEF {
			undefined.WriteString("undefined " + s.Name + "\n")
		} else {
			defined.WriteString("defined " + s.Name + "\n")
		}
	}
	return defined.String() + undefined.String(), nil
}

package c_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/types"
)

// sizesProgram declares a global of each kind of type whose layout the
// checker computes (see types.Table.Size): the basic types, arrays of
// them, of no elements and of structs, structs padded between and after
// their fields, an empty struct, a struct holding an error, a string and
// a list, and unions whose tag is one, two and four bytes, with payloads
// of every alignment and with none.
func sizesProgram() string {
	var b strings.Builder
	b.WriteString(sizesTypes)
	// A union of 300 cases needs a two-byte tag, and one of 70000 a
	// four-byte one.
	for _, n := range []int{300, 70000} {
		fmt.Fprintf(&b, "type Many%d union {\n    first int32\n", n)
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "    c%d\n", i)
		}
		b.WriteString("}\n\n")
	}
	b.WriteString(sizesGlobals)
	b.WriteString("func main() {\n")
	for _, read := range sizeReads {
		fmt.Fprintf(&b, "    if %s {\n        print(\"\")\n    }\n", read)
	}
	b.WriteString("}\n")
	return b.String()
}

// sizesTypes declares sizesProgram's struct and union types but its many
// cased unions.
const sizesTypes = `package main

type Empty struct {
}

type Padded struct {
    a uint8
    b int64
    c uint8
    d int16
}

type Mixed struct {
    e error
    s string
    p Padded
    x [3]uint16
    f bool
    l list[int64]
}

type Small union {
    a uint8
    b int16
    none
}

type Wide union {
    p Padded
    w [5]uint8
}

type Bare union {
    one
    two
}

`

// sizesGlobals declares sizesProgram's globals, one of each type.
const sizesGlobals = `mut gint8 int8
mut gint16 int16
mut gint32 int32
mut gint64 int64
mut guint64 uint64
mut gbool bool
mut gstring string
mut gerror error
mut gsize unsafe_size_t
mut guintptr unsafe_uintptr_t
mut gnone [0]int64
mut gbytes [7]uint8
mut gnested [3][0]uint8
mut gempty Empty
mut gempties [5]Empty
mut gpadded Padded
mut gpaddeds [3]Padded
var gsmall = Small.none
var gwide = Wide.w([5]uint8{1, 2, 3, 4, 5})
var gbare = Bare.one
var gmany300 = Many300.c1
var gmany70000 = Many70000.c1

`

// sizeReads reads every global sizesProgram declares, one condition each,
// so none is unused.
var sizeReads = []string{
	"gint8 == 0", "gint16 == 0", "gint32 == 0", "gint64 == 0", "guint64 == 0", "gbool",
	"len(gstring) == 0", "gerror.code == 0", "gsize == 0", "guintptr == 0",
	"len(gnone) == 0", "len(gbytes) == 7", "len(gnested) == 3", "gempty == gempty",
	"len(gempties) == 5", "gpadded.a == 0", "len(gpaddeds) == 3",
	"gsmall == gsmall", "gwide == gwide", "gbare == gbare", "gmany300 == gmany300",
	"gmany70000 == gmany70000",
}

// TestSizesMatchC generates sizesProgram and asserts, with C's
// _Static_assert, that the size and alignment the checker computed for
// each global's type, and for each declared struct and union type, are
// what clang gives the C type it lowers to.
func TestSizesMatchC(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	src := sizesProgram()
	file := compile(t, src)
	out := generate(t, src)
	var asserts strings.Builder
	checked := 0
	for _, decl := range file.Decls {
		v, ok := decl.(*ast.Var)
		if !ok {
			continue
		}
		size := file.Types.Size(v.Type)
		if size.Overflow || size.Align == 0 {
			t.Fatalf("%s: size %+v", v.Name, size)
		}
		name := fmt.Sprintf("tack_4_main%d_%s", len(v.Name), v.Name)
		assertSize(&asserts, "__typeof__("+name+")", v.Name, size)
		checked++
	}
	for _, decl := range file.Decls {
		td, ok := decl.(*ast.TypeDecl)
		if !ok {
			continue
		}
		size := file.Types.Size(td.Type)
		assertSize(&asserts, fmt.Sprintf("tack_4_main%d_%s", len(td.Name), td.Name), td.Name, size)
		checked++
	}
	if checked < 25 {
		t.Fatalf("only %d globals checked", checked)
	}
	path := filepath.Join(t.TempDir(), "program.c")
	writeFile(t, path, out+"\n"+asserts.String())
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-Wno-unused-function", "-fsyntax-only",
		"-I", stdtest.IncludeDir(), path,
	}
	if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, msg)
	}
}

// assertSize writes a C _Static_assert that the C type ctype has size's
// bytes and alignment, naming label when it does not.
func assertSize(w *strings.Builder, ctype, label string, size types.Size) {
	fmt.Fprintf(w, "_Static_assert(sizeof (%s) == %d && _Alignof(%s) == %d, ",
		ctype, size.Bytes, ctype, size.Align)
	fmt.Fprintf(w, "\"%s: want %d bytes, aligned to %d\");\n", label, size.Bytes, size.Align)
}

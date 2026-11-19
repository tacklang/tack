package porttest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
)

// The fuzz targets' filter of the Unicode divergence. The reference Go
// compiler quotes and classifies runes with Go's unicode package, at
// Unicode 17.0.0, and the port with std/unicode, at 18.0.0 (owner ruling
// 2026-11-19), so the two differ, on purpose, wherever a rune whose
// class the versions disagree on reaches a dump or a message: the
// entries unicode18-lex and unicode18-printf-verb of
// testdata/divergences.txtar record it. A fuzz input is no case of a
// suite, which an entry could apply to, so each fuzz target skips an
// input that holds such a rune instead of reporting the difference.
//
// The runes are computed, not listed: they are every rune one of the
// five classes holds in this module's frozen src/std/unicode/tables.tck,
// which Go's internal/unicodegen wrote from Go's unicode package, and
// not in the port's, which its tools/unicodegen writes, or the other way
// round.

// unicodeTablesPattern matches one class of a tables.tck: its name, the
// array's length and the hex runes between its braces.
var unicodeTablesPattern = regexp.MustCompile(`(?m)^var (\w+) = \[(\d+)\]int32\{\n((?:    [^\n]*\n)*)\}\n`)

// unicodeTablesRune matches one rune of a class's array.
var unicodeTablesRune = regexp.MustCompile(`0x([0-9A-F]+),`)

// runeRun is a run of consecutive runes, first to last.
type runeRun struct {
	first, last rune
}

// parseUnicodeTables returns the runs of each class of data, a
// tables.tck as either generator writes it, by the class's array name.
// An array whose length is not the runes it holds, an odd count of
// them, or runs not ascending with a gap between each two, is an error:
// the file is read strictly, so a change to its format cannot pass for
// tables with no runes.
func parseUnicodeTables(data []byte) (map[string][]runeRun, error) {
	out := map[string][]runeRun{}
	for _, m := range unicodeTablesPattern.FindAllSubmatch(data, -1) {
		name := string(m[1])
		n, err := strconv.Atoi(string(m[2]))
		if err != nil {
			return nil, fmt.Errorf("%s: length %s: %w", name, m[2], err)
		}
		var runes []rune
		for _, r := range unicodeTablesRune.FindAllSubmatch(m[3], -1) {
			v, err := strconv.ParseInt(string(r[1]), 16, 32)
			if err != nil {
				return nil, fmt.Errorf("%s: rune %s: %w", name, r[1], err)
			}
			runes = append(runes, rune(v))
		}
		if len(runes) != n || n%2 != 0 {
			return nil, fmt.Errorf("%s holds %d runes, and its length is %d", name, len(runes), n)
		}
		var runs []runeRun
		for i := 0; i < n; i += 2 {
			r := runeRun{runes[i], runes[i+1]}
			if r.last < r.first || len(runs) > 0 && r.first <= runs[len(runs)-1].last+1 {
				return nil, fmt.Errorf("%s: run %X..%X is out of order", name, r.first, r.last)
			}
			runs = append(runs, r)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%s is declared twice", name)
		}
		out[name] = runs
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no class found")
	}
	return out, nil
}

// inRuns reports whether r is in one of runs.
func inRuns(runs []runeRun, r rune) bool {
	i := sort.Search(len(runs), func(i int) bool { return runs[i].last >= r })
	return i < len(runs) && runs[i].first <= r
}

// driftRunes returns the runes a class holds in one of a and b, two
// tables.tck, and not in the other, as runs in ascending order. The two
// must declare the same classes.
func driftRunes(a, b []byte) ([]runeRun, error) {
	ta, err := parseUnicodeTables(a)
	if err != nil {
		return nil, err
	}
	tb, err := parseUnicodeTables(b)
	if err != nil {
		return nil, err
	}
	if len(ta) != len(tb) {
		return nil, fmt.Errorf("the tables declare %d and %d classes", len(ta), len(tb))
	}
	for name := range ta {
		if _, ok := tb[name]; !ok {
			return nil, fmt.Errorf("class %s is in one table alone", name)
		}
	}
	var out []runeRun
	for r := rune(0); r <= utf8.MaxRune; r++ {
		differs := false
		for name, runs := range ta {
			if inRuns(runs, r) != inRuns(tb[name], r) {
				differs = true
				break
			}
		}
		if !differs {
			continue
		}
		if n := len(out); n > 0 && out[n-1].last == r-1 {
			out[n-1].last = r
		} else {
			out = append(out, runeRun{r, r})
		}
	}
	return out, nil
}

// loadedDrift is driftRunes of this module's frozen tables and the
// port's, in its source tree, env.home, read once.
var loadedDrift = sync.OnceValues(func() ([]runeRun, error) {
	return loadDrift(env.root, env.home)
})

// loadDrift returns driftRunes of the tables.tck of std in root, this
// module, and in home, the port's source tree.
func loadDrift(root, home string) ([]runeRun, error) {
	rel := filepath.Join("src", "std", "unicode", "tables.tck")
	frozen, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // G304: this module's std
	if err != nil {
		return nil, err
	}
	port, err := os.ReadFile(filepath.Join(home, rel)) //nolint:gosec // G304: the port's std
	if err != nil {
		return nil, err
	}
	return driftRunes(frozen, port)
}

// driftRuneIn returns the first rune of drift that src holds, and
// whether it holds one: as UTF-8 in its bytes, in what its \x escapes
// spell, decoded as the lexer decodes a string literal's, or in the
// decoded value of a string, char or comment token this module's lexer
// reads from it, so a rune spelt by escapes in a literal counts as much
// as one written as itself.
func driftRuneIn(drift []runeRun, src []byte) (rune, bool) {
	if r, ok := driftRuneInBytes(drift, src); ok {
		return r, true
	}
	if r, ok := driftRuneInBytes(drift, decodeHexEscapes(src)); ok {
		return r, true
	}
	f := &source.File{ID: fuzzFile, Filename: fuzzFile, Data: src}
	lex := lexer.New(f)
	lex.Lex()
	for _, tok := range lex.Tokens() {
		var b []byte
		switch v := tok.Value().(type) {
		case string:
			b = []byte(v)
		case []byte:
			b = v
		default:
			continue
		}
		if r, ok := driftRuneInBytes(drift, b); ok {
			return r, true
		}
	}
	return 0, false
}

// driftRuneInBytes returns the first rune of drift that b holds as
// UTF-8, and whether it holds one; a byte that starts no valid sequence
// is skipped.
func driftRuneInBytes(drift []runeRun, b []byte) (rune, bool) {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r != utf8.RuneError || size > 1 {
			if inRuns(drift, r) {
				return r, true
			}
		}
		b = b[size:]
	}
	return 0, false
}

// hexEscape matches a \x escape, two hex digits after it.
var hexEscape = regexp.MustCompile(`\\[xX]([0-9A-Fa-f]{2})`)

// decodeHexEscapes returns src with each \x escape replaced by the byte
// it spells.
func decodeHexEscapes(src []byte) []byte {
	return hexEscape.ReplaceAllFunc(src, func(m []byte) []byte {
		v, _ := strconv.ParseUint(string(m[2:]), 16, 8) // two hex digits always parse
		return []byte{byte(v)}
	})
}

// skipUnicodeDrift skips t, a fuzz input's test, when one of srcs, the
// bytes the input gives the compilers, holds a rune whose class Go's
// unicode package and the port's std/unicode disagree on (see
// driftRuneIn), and fails it when the tables cannot be read. Every fuzz
// target calls it before it runs a compiler; TestFuzzTargetsFilterDrift
// checks they do.
func skipUnicodeDrift(t *testing.T, srcs ...[]byte) {
	t.Helper()
	drift, err := loadedDrift()
	if err != nil {
		t.Fatalf("reading the Unicode tables: %v", err)
	}
	skipWith(t, drift, srcs...)
}

// TestDriftRunes checks driftRunes on two small tables: a rune in a class
// of one alone is drift, one both hold is not, and tables of another
// shape are errors.
func TestDriftRunes(t *testing.T) {
	table := func(letters string) []byte {
		return []byte("package unicode\n\nvar letter_runs = [4]int32{\n" + letters + "\n}\n\n" +
			"var digit_runs = [2]int32{\n    0x0030, 0x0039,\n}\n")
	}
	a := table("    0x0041, 0x005A, 0x0061, 0x007A,")
	b := table("    0x0041, 0x005B, 0x0060, 0x007A,")
	got, err := driftRunes(a, b)
	if err != nil {
		t.Fatal(err)
	}
	want := []runeRun{{0x5B, 0x5B}, {0x60, 0x60}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("driftRunes = %v, want %v", got, want)
	}
	same, err := driftRunes(a, a)
	if err != nil || len(same) != 0 {
		t.Errorf("driftRunes of a table and itself = %v, %v, want none", same, err)
	}
	for name, bad := range map[string][]byte{
		"wrong length": table("    0x0041, 0x005A,"),
		"out of order": table("    0x0061, 0x007A, 0x0041, 0x005A,"),
		"touching":     table("    0x0041, 0x005A, 0x005B, 0x007A,"),
		"no class":     []byte("package unicode\n"),
	} {
		if _, err := driftRunes(a, bad); err == nil {
			t.Errorf("%s: driftRunes gave no error", name)
		}
	}
	other := []byte("package unicode\n\nvar space_runs = [2]int32{\n    0x0020, 0x0020,\n}\n")
	if _, err := driftRunes(a, other); err == nil {
		t.Errorf("tables of other classes: driftRunes gave no error")
	}
}

// TestUnicodeDriftFilter checks the fuzz targets' filter on this
// module's frozen tables and the port's: that the runes it skips are the
// 13007 Unicode 18.0.0 assigns, U+0558 among them, and that an input
// holding one, as itself, through \x escapes in a literal or in a
// comment, is skipped, while one holding none, a rune every version
// classifies alike, invalid UTF-8 or a \x escape of a lone byte
// included, still compares. It needs the port's source tree, and skips
// without one only when TACK_PORT_BIN names the port.
func TestUnicodeDriftFilter(t *testing.T) {
	src, _ := portSource(os.Getenv("TACK_PORT"))
	drift, err := loadDrift(fromRoot("."), src)
	if err != nil {
		if os.Getenv("TACK_PORT_BIN") != "" && !dirExists(filepath.Join(src, "driver")) {
			t.Skip("no port source tree to read std/unicode's tables from: TACK_PORT_BIN names the port")
		}
		t.Fatal(err)
	}
	n := 0
	for _, r := range drift {
		n += int(r.last-r.first) + 1
	}
	if n != 13007 || !inRuns(drift, 0x0558) || !inRuns(drift, 0x0B53) || inRuns(drift, 'A') {
		t.Fatalf("the drift is %d runes, U+0558 %t, U+0B53 %t, A %t; want the 13007 runes "+
			"Unicode 18.0.0 assigns, U+0558 and U+0B53 among them, and not A",
			n, inRuns(drift, 0x0558), inRuns(drift, 0x0B53), inRuns(drift, 'A'))
	}
	for _, c := range []struct {
		name string
		src  string
		skip bool
	}{
		{"raw", "package main\n\nvar s = \"\u0558\"\n", true},
		{"escaped", "package main\n\nvar s = \"\\xd5\\x98\"\n", true},
		{"comment", "package main\n\n// \u0b53\n", true},
		{"bare", "\u0558", true},
		{"ascii", "package main\n\nvar s = \"A\"\n", false},
		{"old rune", "package main\n\nvar s = \"\u0559 \u3000\"\n", false},
		{"invalid", "package main\n\nvar s = \"\xd5\"\n", false},
		{"lone escape", "package main\n\nvar s = \"\\xd5\"\n", false},
		{"empty", "", false},
	} {
		if _, got := driftRuneIn(drift, []byte(c.src)); got != c.skip {
			t.Errorf("%s: driftRuneIn = %t, want %t", c.name, got, c.skip)
		}
		// The filter's own body, as a fuzz target's test runs it.
		ran := false
		t.Run(c.name, func(t *testing.T) {
			skipWith(t, drift, []byte(c.src))
			ran = true
		})
		if ran == c.skip {
			t.Errorf("%s: the input went on to the compilers: %t, want %t", c.name, ran, !c.skip)
		}
	}
}

// skipWith skips t when one of srcs holds a rune of drift (see
// driftRuneIn): skipUnicodeDrift's work, with drift given rather than
// read.
func skipWith(t *testing.T, drift []runeRun, srcs ...[]byte) {
	t.Helper()
	for _, src := range srcs {
		if r, ok := driftRuneIn(drift, src); ok {
			t.Skipf("the input holds U+%04X, whose class Go's unicode package, at Unicode 17.0.0, "+
				"and the port's std/unicode differ on: a known divergence (see unicode18-lex)", r)
		}
	}
}

// TestFuzzTargetsFilterDrift checks that every fuzz target of this
// package, each function named Fuzz..., calls skipUnicodeDrift in the
// function it fuzzes with, f.Fuzz's, or calls a function of this package
// that does, so no target can feed both compilers an input holding a
// rune they classify apart.
func TestFuzzTargetsFilterDrift(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	// calls reports whether n calls the function of this package
	// named name.
	calls := func(n ast.Node, name string) bool {
		found := false
		ast.Inspect(n, func(x ast.Node) bool {
			if c, ok := x.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == name {
					found = true
				}
			}
			return !found
		})
		return found
	}
	// fuzzBody returns the function fd passes to f.Fuzz, or nil.
	fuzzBody := func(fd *ast.FuncDecl) ast.Node {
		var body ast.Node
		ast.Inspect(fd, func(x ast.Node) bool {
			if c, ok := x.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Fuzz" && len(c.Args) == 1 {
					body = c.Args[0]
				}
			}
			return body == nil
		})
		return body
	}
	var targets []string
	for name, fd := range funcs {
		if !strings.HasPrefix(name, "Fuzz") {
			continue
		}
		targets = append(targets, name)
		ok := false
		if body := fuzzBody(fd); body != nil {
			ok = calls(body, "skipUnicodeDrift")
		}
		for callee, cd := range funcs {
			if !ok && callee != name && calls(fd, callee) {
				if body := fuzzBody(cd); body != nil && calls(body, "skipUnicodeDrift") {
					ok = true
				}
			}
		}
		if !ok {
			t.Errorf("%s does not call skipUnicodeDrift in the function it fuzzes with", name)
		}
	}
	if len(targets) == 0 {
		t.Fatal("no fuzz target found")
	}
	sort.Strings(targets)
	t.Logf("fuzz targets filtered: %s", strings.Join(targets, ", "))
}

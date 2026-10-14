package c

import (
	"strconv"
	"strings"
	"testing"
)

// decodeMangled reads a name mangle made back into its parts, the parts of
// the package path and then the declaration's name, and reports whether
// it could.
func decodeMangled(name string) ([]string, bool) {
	rest, ok := strings.CutPrefix(name, "tack_")
	if !ok || rest == "" {
		return nil, false
	}
	var parts []string
	for rest != "" {
		var part string
		if part, rest, ok = nextPart(rest); !ok {
			return nil, false
		}
		parts = append(parts, part)
	}
	return parts, true
}

// nextPart reads the part that rest starts with, its length after
// escaping, an underscore, and that many escaped bytes, and returns it
// unescaped with what follows it.
func nextPart(rest string) (string, string, bool) {
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	n, err := strconv.Atoi(rest[:digits])
	if err != nil || digits+1+n > len(rest) || rest[digits] != '_' {
		return "", "", false
	}
	part, ok := unescape(rest[digits+1 : digits+1+n])
	return part, rest[digits+1+n:], ok
}

// unescape undoes mangledPart's escaping of e.
func unescape(e string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(e); i++ {
		if e[i] != '_' {
			b.WriteByte(e[i])
			continue
		}
		if i+3 > len(e) {
			return "", false
		}
		v, err := strconv.ParseUint(e[i+1:i+3], 16, 8)
		if err != nil {
			return "", false
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), true
}

// checkRoundTrip fails the test unless the name path and name mangle to
// decodes back to exactly path's parts and name.
func checkRoundTrip(t *testing.T, path, name string) {
	t.Helper()
	mangled := "tack_" + mangledParts(path, name)
	parts, ok := decodeMangled(mangled)
	want := append(strings.Split(path, "/"), name)
	if !ok || strings.Join(parts, "\x00") != strings.Join(want, "\x00") || len(parts) != len(want) {
		t.Errorf(
			"%q, %q mangles to %s, which decodes to %q, want %q",
			path,
			name,
			mangled,
			parts,
			want,
		)
	}
}

// TestMangledNamesDecode checks every mangled name decodes back to its
// package path and name, whatever bytes they hold.
func TestMangledNamesDecode(t *testing.T) {
	for _, tc := range []struct{ path, name string }{
		{"main", "main"},
		{"calc/cmd/calc", "main"},
		{"calc/lexer", "First"},
		{"my-app/lexer", "Token"},
		{"a_b", "c"},
		{"a", "b_c"},
		{"example.com/x_5f", "_5f_"},
		{"a1", "2b"},
		{"m/\x00\xff", "\n"},
	} {
		checkRoundTrip(t, tc.path, tc.name)
	}
}

// TestOldSchemeCollisionsDoNotCollide checks pairs of names that the
// earlier scheme, tack_<package>_<name>, wrote the same, because an
// underscore could be part of either, now differ.
func TestOldSchemeCollisionsDoNotCollide(t *testing.T) {
	for _, tc := range [][4]string{
		{"a", "b_c", "a_b", "c"},
		{"main", "x_y", "main_x", "y"},
		{"m/a", "b_c", "m/a_b", "c"},
		{"main", "results_f", "results_main", "f"},
	} {
		x := "tack_" + mangledParts(tc[0], tc[1])
		y := "tack_" + mangledParts(tc[2], tc[3])
		if x == y {
			t.Errorf("%s.%s and %s.%s both mangle to %s", tc[0], tc[1], tc[2], tc[3], x)
		}
	}
}

// generatorNames are the shapes of every other name the generator makes
// starting tack_: the character after the prefix is a letter in each.
var generatorNames = []string{
	"tack_string",
	"tack_builtin_print",
	"tack_builtin_exit",
	"tack_div_int8",
	"tack_rem_uint64",
	"tack_eq_12",
	"tack_array_3",
	"tack_results_4_main1_f",
	"tack_n0",
	"tack_k1",
	"tack_r0",
	"tack_o9",
}

// FuzzMangleIsInjective checks two different package paths and names never
// mangle to one C name, that each decodes back to its parts, and that a
// mangled name, with a nonempty first path part, never takes the shape of
// a name the generator makes for itself.
func FuzzMangleIsInjective(f *testing.F) {
	f.Add("a", "b_c", "a_b", "c")
	f.Add("main", "main", "calc/cmd/calc", "main")
	f.Add("my-app/lexer", "Token", "my_2dapp/lexer", "Token")
	f.Fuzz(func(t *testing.T, path1, name1, path2, name2 string) {
		x := "tack_" + mangledParts(path1, name1)
		y := "tack_" + mangledParts(path2, name2)
		if (path1 != path2 || name1 != name2) && x == y {
			t.Fatalf("%q.%q and %q.%q both mangle to %s", path1, name1, path2, name2, x)
		}
		checkRoundTrip(t, path1, name1)
		first, _, _ := strings.Cut(path1, "/")
		if first == "" {
			return
		}
		if c := x[len("tack_")]; c < '1' || c > '9' {
			t.Fatalf("%s does not start with a length", x)
		}
		for _, own := range generatorNames {
			if x == own {
				t.Fatalf("%q.%q mangles to %s, a name of the generator", path1, name1, x)
			}
		}
	})
}

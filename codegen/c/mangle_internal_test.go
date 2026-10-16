package c

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/types"
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
	"tack_error",
	"tack_builtin_print",
	"tack_builtin_exit",
	"tack_div_int8",
	"tack_rem_uint64",
	"tack_eq_12",
	"tack_free_12",
	"tack_array_3",
	"tack_results_4_main1_f",
	"tack_n0",
	"tack_k1",
	"tack_r0",
	"tack_o9",
	"tack_s2",
	"tack_u3",
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

// TestMethodNamesDoNotCollide checks a method's C name against the names
// most like it: a function of the same name, the method's type, a
// function of a package whose path ends with the type's name, a method of
// another type, and the function named as the type and method joined by
// an underscore, which an earlier scheme, tack_<pkg>_<Type>_<method>,
// would have written alike.
func TestMethodNamesDoNotCollide(t *testing.T) {
	method := "tack_" + mangledParts("calc", methodCName("Builder", "write"))
	for _, other := range []struct{ path, name string }{
		{"calc", "write"},
		{"calc", "Builder"},
		{"calc/Builder", "write"},
		{"calc", "Builder_write"},
		{"calc", "Builderwrite"},
		{"calc", methodCName("Builde", "rwrite")},
		{"calc", methodCName("Writer", "write")},
		{"calc/x", methodCName("Builder", "write")},
	} {
		if got := "tack_" + mangledParts(other.path, other.name); got == method {
			t.Errorf(
				"%s.%s mangles to %s, as Builder.write in calc does",
				other.path,
				other.name,
				got,
			)
		}
	}
	parts, ok := decodeMangled(method)
	if !ok || strings.Join(parts, "/") != "calc/Builder.write" {
		t.Errorf("%s decodes to %q, want calc and Builder.write", method, parts)
	}
}

// FuzzMethodCNamesAreDistinct checks that a method's C name is never the
// C name of a function, a type or a global, whose names hold no period,
// nor another method's, whatever the package paths and names. Type and
// method names are identifiers, so they hold no period either.
func FuzzMethodCNamesAreDistinct(f *testing.F) {
	f.Add("calc", "Builder", "write", "calc/Builder", "write", "Builder")
	f.Add("a", "b", "c", "a", "b_c", "b")
	f.Add("m", "T", "f", "m", "T_2ef", "U")
	f.Fuzz(func(t *testing.T, path1, typ1, name1, path2, name2, typ2 string) {
		if strings.Contains(typ1+name1+name2+typ2, ".") {
			return
		}
		method := "tack_" + mangledParts(path1, methodCName(typ1, name1))
		if fn := "tack_" + mangledParts(path2, name2); fn == method {
			t.Fatalf("method %q.%q.%q and %q.%q both mangle to %s",
				path1, typ1, name1, path2, name2, fn)
		}
		other := "tack_" + mangledParts(path2, methodCName(typ2, name2))
		if (path1 != path2 || typ1 != typ2 || name1 != name2) && other == method {
			t.Fatalf("methods %q.%q.%q and %q.%q.%q both mangle to %s",
				path1, typ1, name1, path2, typ2, name2, method)
		}
	})
}

// TestInstanceNamesDoNotCollide checks an instance's C name against the
// names most like it: its generic function's own name, a method of its
// type argument's name, a function named as the instance is written
// without brackets, and instances of other type arguments, one or two,
// whose names run together differently.
func TestInstanceNamesDoNotCollide(t *testing.T) {
	inst := "tack_" + mangledParts("calc", instanceName("render", []string{"main.File"}))
	for _, other := range []struct{ path, name string }{
		{"calc", "render"},
		{"calc", "renderFile"},
		{"calc", methodCName("File", "render")},
		{"calc", methodCName("render", "File")},
		{"calc", instanceName("render", []string{"main.Fil"})},
		{"calc", instanceName("render", []string{"mai.nFile"})},
		{"calc", instanceName("render", []string{"main.File", "main.File"})},
		{"calc", instanceName("rende", []string{"rmain.File"})},
		{"calc/x", instanceName("render", []string{"main.File"})},
	} {
		if got := "tack_" + mangledParts(other.path, other.name); got == inst {
			t.Errorf("%s.%s mangles to %s, as render[main.File] in calc does",
				other.path, other.name, got)
		}
	}
	parts, ok := decodeMangled(inst)
	if !ok || strings.Join(parts, "/") != "calc/render[main.File]" {
		t.Errorf("%s decodes to %q, want calc and render[main.File]", inst, parts)
	}
}

// instanceParts reports whether names could be the names of functions
// and types, which are not empty and hold no period, comma or bracket,
// and paths import paths, which hold no comma or bracket.
func instanceParts(names, paths []string) bool {
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, ".,[]") {
			return false
		}
	}
	for _, path := range paths {
		if strings.ContainsAny(path, ",[]") {
			return false
		}
	}
	return true
}

// FuzzInstanceCNamesAreDistinct checks that two instances' C names differ
// whenever their functions' packages, their functions or their type
// arguments do, and that an instance's is never a function's, a type's, a
// global's or a method's. A function's or a type's name is an
// identifier, holding no period, comma or bracket, and an import path
// holds no comma or bracket, so inputs that do are skipped.
func FuzzInstanceCNamesAreDistinct(f *testing.F) {
	f.Add("calc", "f", "main", "A", "main", "B", "calc", "f", "main", "A", "main", "B")
	f.Add("m", "f", "a", "B", "c", "D", "m", "f", "a", "B,c", "", "D")
	f.Add("m", "f", "a.b", "C", "d", "E", "m", "f", "a", "b.C", "d", "E")
	f.Fuzz(func(t *testing.T, path1, fn1, p1, n1, q1, m1, path2, fn2, p2, n2, q2, m2 string) {
		if !instanceParts([]string{fn1, n1, m1, fn2, n2, m2}, []string{p1, q1, p2, q2}) {
			return
		}
		args1 := []string{p1 + "." + n1, q1 + "." + m1}
		args2 := []string{p2 + "." + n2, q2 + "." + m2}
		x := "tack_" + mangledParts(path1, instanceName(fn1, args1))
		y := "tack_" + mangledParts(path2, instanceName(fn2, args2))
		same := strings.Join(append([]string{path1, fn1}, args1...), "\x00") ==
			strings.Join(append([]string{path2, fn2}, args2...), "\x00")
		if !same && x == y {
			t.Fatalf(
				"%s.%s%q and %s.%s%q both mangle to %s",
				path1,
				fn1,
				args1,
				path2,
				fn2,
				args2,
				x,
			)
		}
		if fn := "tack_" + mangledParts(path2, fn2); fn == x {
			t.Fatalf("instance %s and function %s.%s both mangle to %s", x, path2, fn2, fn)
		}
		if m := "tack_" + mangledParts(path2, methodCName(fn2, n2)); m == x {
			t.Fatalf("instance %s and method %s.%s.%s both mangle to %s", x, path2, fn2, n2, m)
		}
	})
}

// TestUnionNamesDoNotCollide checks the names a union type brings to the
// generated C: its free helper is told apart from every other helper
// named by a type's ID, its tag fits every case's position in the
// narrowest type that holds them, and its payloads' members, c_ and the
// case's name, never take the tag's or the payload union's name.
func TestUnionNamesDoNotCollide(t *testing.T) {
	seen := make(map[string]bool)
	for id := range 200 {
		typ := types.Type(id)
		for _, name := range []string{
			freeName(typ), equalityName(typ), arrayName(typ),
			listName(typ), viewName(typ),
		} {
			if seen[name] {
				t.Fatalf("%s is the name of two helpers or types", name)
			}
			seen[name] = true
		}
	}
	for _, tc := range []struct {
		want string
		n    int
	}{
		{"uint8_t", 0},
		{"uint8_t", 1},
		{"uint8_t", 256},
		{"uint16_t", 257},
		{"uint16_t", 65536},
		{"uint32_t", 65537},
	} {
		if got := unionTagType(tc.n); got != tc.want {
			t.Errorf("a union of %d cases has a %s tag, want %s", tc.n, got, tc.want)
		}
	}
	for _, name := range []string{"tag", "u", "c", "int", "while", "c_x", "_"} {
		if m := caseMember(name); m == "tag" || m == "u" || !strings.HasPrefix(m, "c_") {
			t.Errorf("case %s has the member %s", name, m)
		}
	}
}

// FuzzUnionCaseMembersAreDistinct checks two different cases of a union,
// whatever their names, never share a member, and none is the tag's or the
// payload union's.
func FuzzUnionCaseMembersAreDistinct(f *testing.F) {
	f.Add("int", "name")
	f.Add("tag", "u")
	f.Add("c_a", "a")
	f.Fuzz(func(t *testing.T, a, b string) {
		ma, mb := caseMember(a), caseMember(b)
		if a != b && ma == mb {
			t.Fatalf("cases %q and %q share the member %s", a, b, ma)
		}
		if ma == "tag" || ma == "u" {
			t.Fatalf("case %q has the member %s", a, ma)
		}
	})
}

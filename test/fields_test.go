package compilertest

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/loader"
)

// fieldsFiles is a module, calc, and a std of its own, each with structs
// whose fields carry every mark, and generic functions with a fields
// loop in each package, which main calls with each struct.
var fieldsFiles = map[string]string{
	"m/tack.mod": "module calc\n",
	"m/.std/util/util.tck": `package util

pub type Pair struct {
    pub a int64
    lib b int64
    c int64
}

pub func make_pair() Pair {
    return Pair{a: 1, b: 2, c: 3}
}

pub func util_list[T Data](v T) {
    for f := range fields(T) {
        print(f.name)
        util_list(v.[f])
    }
}
`,
	"m/.std/walk/walk.tck": `package walk

pub func walk_list[T Data](v T) {
    for f := range fields(T) {
        print(f.name)
        walk_list(v.[f])
    }
}
`,
	"m/geo/geo.tck": `package geo

pub type G struct {
    pub a int64
    lib b int64
    c int64
}

lib type L struct {
    pub p int64
    lib q int64
    r int64
}

pub func make_g() G {
    return G{a: 1, b: 2, c: 3}
}

lib func make_l() L {
    return L{p: 1, q: 2, r: 3}
}

pub func geo_list[T Data](v T) {
    for f := range fields(T) {
        print(f.name)
        geo_list(v.[f])
    }
}
`,
	"m/main.tck": `package main

import geo from "calc/geo"
import util from "std/util"
import walk from "std/walk"

type Rec struct {
    pub a int64
    lib b int64
    c int64
}

func main_list[T Data](v T) {
    for f := range fields(T) {
        print(f.name)
        main_list(v.[f])
    }
}

func main() {
    r := Rec{a: 1, b: 2, c: 3}
    g := geo.make_g()
    l := geo.make_l()
    p := util.make_pair()
    walk.walk_list(r)
    geo.geo_list(r)
    main_list(r)
    walk.walk_list(l)
    geo.geo_list(l)
    main_list(l)
    walk.walk_list(g)
    main_list(g)
    main_list(p)
    walk.walk_list(p)
    util.util_list(p)
}
`,
}

// TestFieldsLoopListsByMark checks which fields a fields loop lists, by
// the field's own mark and the package declaring the loop's function,
// whatever the struct's visibility: every field in the struct's package,
// a pub field anywhere, a lib field only within its module; across a
// module and its std, a private struct's pub fields and a lib struct's
// pub fields are listed from the other module, and their lib ones only
// within their own.
func TestFieldsLoopListsByMark(t *testing.T) {
	files := make(fstest.MapFS, len(fieldsFiles))
	for name, src := range fieldsFiles {
		files[name] = &fstest.MapFile{Data: []byte(src)}
	}
	std := &loader.Std{FS: loader.FromFS(files), Dir: "m/.std"}
	p := program{files: files, target: "m", std: std}
	res, err := p.loader().Dir(p.target)
	if err != nil {
		t.Fatal(err)
	}
	if diags, _ := checkAll(res); diag.HasErrors(diags) {
		t.Fatalf("check: %s", renderSorted(res.Files, diags))
	}
	got := listed(res.Program)
	want := map[string]string{
		"walk_list Rec":  "a",
		"geo_list Rec":   "a b",
		"main_list Rec":  "a b c",
		"walk_list L":    "p",
		"geo_list L":     "p q r",
		"main_list L":    "p q",
		"walk_list G":    "a",
		"main_list G":    "a b",
		"main_list Pair": "a",
		"walk_list Pair": "a b",
		"util_list Pair": "a b c",
	}
	for key, fields := range want {
		if got[key] != fields {
			t.Errorf("%s lists %q, want %q", key, got[key], fields)
		}
	}
	if extra := len(got) - len(want); extra != 0 {
		t.Errorf("found %d listings beyond those wanted: %v", extra, slices.Sorted(maps.Keys(got)))
	}
}

// listed returns, for each generic function holding a fields loop in
// prog, and each struct type the loop was recorded for, the names of the
// fields it lists, joined by spaces, keyed by the function's name and the
// type's.
func listed(prog *ast.Program) map[string]string {
	got := make(map[string]string)
	for _, decl := range prog.Decls() {
		fn, ok := decl.(*ast.Func)
		if !ok {
			continue
		}
		ast.Walk(fn, func(n ast.Node) bool {
			_, fo, ok := ast.FieldsLoop(n)
			if !ok {
				return true
			}
			for _, u := range fo.Unrolled {
				all := prog.Types.Fields(u.Type)
				if all == nil {
					continue
				}
				names := make([]string, len(u.Fields))
				for i, f := range u.Fields {
					names[i] = all[f].Name
				}
				got[fn.Name+" "+prog.Types.Name(u.Type)] = strings.Join(names, " ")
			}
			return true
		})
	}
	return got
}

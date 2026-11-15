package porttest

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	tackast "github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// baselineProgram is a program whose blocks, at a checkpoint, count for
// that checkpoint's stage (see stagesOf): written in the features of the
// stages up to it alone, which TestEmitBaselineFeatures checks, what it
// runs of a later stage's code is that code's not-taken path, a
// dispatch's predicate asking whether the later feature is there and
// finding it is not, which every program of the checkpoint runs, so its
// port is the checkpoint's work.
type baselineProgram struct {
	// file is the program's name, a .tck file.
	file string
	// src is the program.
	src string
	// lib says it is a library, emitted with --check --emit, not --emit.
	lib bool
}

// baselinePrograms are the baseline programs of each checkpoint, E1, E2
// and E3; E4 is the last stage and has none. Each is run with --emit, or
// with --check --emit for a library, and with --check --max-errors 0,
// and must exit 0 both times.
//
// E1's: hello; scalars, globals, parameters, a mut parameter, locals of
// every scalar kind, if, else if, loops of every form, break and
// continue, wrapping and bitwise arithmetic, comparisons, && || and !,
// floats, print and exit; strings, a string global, len, ==, an index
// its length proves, len as a statement; and a library of two functions and a global. The
// plan puts switches, structs, methods, any, division and shifts,
// several values and the checked forms in E2, so none is here, and
// std/fmt's printf lowers lists, E3's.
//
// E2's: a struct with a method of each receiver, == on it, a struct of
// structs read and written through a.b.c and a call's result's field,
// f().x, several values assigned to variables declared before and to _,
// an array, a
// slice of a string, a union and a switch on it, a switch on an int, a
// function of two results, a checked division and conversion and a
// shift. fmt.printf, whose std/fmt lowers lists, is no E2 program's.
//
// E3's: a list grown by append under ?, a range over it, an own
// parameter, a map with put and has, replace, a region, an arena, a
// pool, a defer and drop.
var baselinePrograms = map[emitStage][]baselineProgram{
	emitE1: {
		{file: "e1-hello.tck", src: `package main

func main() {
    print("hi\n")
}
`},
		{file: "e1-scalars.tck", src: `package main

var limit int64 = 10
mut total int64 = 0
var flag = true
var small int8 = -7
var ratio float64 = 1.5

func plus(a int64, b int64) int64 {
    return a +% b
}

func step(mut x int64) {
    x = x +% 1
}

func pick(a bool, b bool) bool {
    return a && !b || a == b
}

func scale(f float64, g float32) float64 {
    return f * 2.0 + float64(g)
}

func main() {
    mut i int64 = 0
    mut u uint8 = 3
    for i < limit {
        if i == 3 {
            i = i +% 1
            continue
        }
        if i > 7 {
            break
        }
        total = plus(total, i *% 2) -% 1
        u = u ^ 1 | 2 & 7
        step(mut i)
    }
    for range 2 {
        total = total +% 1
    }
    for mut k := 0; k < 3; k += 1 {
        total = total +% k
    }
    if pick(flag, u != 0) && small < 0 {
        print("yes\n")
    } else if total >= 5 {
        print("no\n")
    } else {
        print("maybe\n")
    }
    if scale(ratio, 2.5) > 1.0 {
        exit(3)
    }
}
`},
		{file: "e1-strings.tck", src: `package main

var greeting = "hello"

func shout(s string) string {
    return s
}

func main() {
    s := shout(greeting)
    if len(s) > 2 && s != "bye" {
        c := s[1]
        if c == 'e' {
            print(s)
            print("\n")
        }
    }
    if s == "hello" {
        print("same\n")
    }
    len(s)
}
`},
		{file: "e1-lib.tck", src: `package util

var base int64 = 4

pub func twice(x int64) int64 {
    return x +% x +% base
}

pub func check(b bool) bool {
    return !b
}
`, lib: true},
	},
	emitE2: {{file: "e2-values.tck", src: `package main

type Point struct {
    x int64
    y int64
}

func (p Point) sum() int64 {
    return p.x +% p.y
}

func (mut p Point) shift(d int64) {
    p.x = p.x +% d
}

type Shape union {
    dot
    box Point
    size int64
}

func area(s Shape) int64 {
    switch s {
    case dot:
        return 0
    case box(p):
        return p.sum()
    case size(n):
        return n
    }
}

func pair(a int64) (int64, bool) {
    return a, a > 0
}

type Line struct {
    head Point
    tail Point
}

func origin() Point {
    return Point{x: 0, y: 0}
}

func main() {
    mut p := Point{x: 1, y: 2}
    mut p.shift(3)
    q := Point{x: 1, y: 2}
    if p == q {
        print("eq\n")
    }
    a := [3]int64{1, 2, 3}
    mut b := a
    b[0] = 9
    if a != b {
        print("ne\n")
    }
    v := "hello"[1:3]
    if len(v) == 2 {
        print(v)
    }
    mut l := Line{head: p, tail: q}
    l.tail.y = 7
    if l.head.x > origin().x && l.tail.y > 0 {
        p.y = l.tail.y
    }
    s := Shape.box(p)
    n, ok := pair(area(s))
    mut m int64 = 0
    mut big = false
    m, big = pair(n)
    _, big = pair(m)
    if big {
        print("pair\n")
    }
    if ok {
        if n > 2 {
            print("big\n")
        }
    }
    d := len("abc")
    switch d {
    case 1:
        print("one\n")
    case 3:
        print("three\n")
    default:
        print("other\n")
    }
    r, fine := n / d
    if fine {
        if r > n >> 1 {
            print("half\n")
        }
    }
    w, fits := int32(n)
    if fits && w > 0 {
        exit(w)
    }
}
`}},
	emitE3: {{file: "e3-owned.tck", src: `package main

func fill(mut l list[int64]) error {
    append(mut l, 1)?
    append(mut l, 2)?
    return noerror
}

func take(own l list[int64]) int64 {
    return len(l)
}

func main() {
    mut l := make(list[int64], c_allocator)
    defer {
        print("done\n")
    }
    err := fill(mut l)
    if err.code != 0 {
        return
    }
    for _, e := range l {
        if e == 2 {
            print("two\n")
        }
    }
    mut m := make(map[int64]int64, c_allocator)
    perr := put(mut m, 1, 2)
    if perr.code == 0 && has(m, 1) {
        print("has\n")
    }
    old := replace(mut l, make(list[int64], c_allocator))
    if take(own old) > 0 {
        print("took\n")
    }
    region r {
        t := make(list[int64], r)
        if len(t) == 0 {
            print("empty\n")
        }
    }
    mut a := make(arena[int64], 0, c_allocator)
    mut p := make(pool[int64], c_allocator)
    h, ierr := insert(mut p, 10)
    if ierr.code == 0 && has(p, h) {
        print("pool\n")
    }
    drop(own a)
    drop(own m)
}
`}},
}

// baselineArgs returns the command lines p is run with.
func baselineArgs(p baselineProgram) [][]string {
	emit := []string{"--emit", p.file}
	if p.lib {
		emit = []string{"--check", "--emit", p.file}
	}
	return [][]string{emit, {"--check", "--max-errors", "0", p.file}}
}

// idleArgs is the command line of a run of the instrumented compiler
// that lowers nothing: what it covers every process does as it starts.
var idleArgs = []string{"--backends"}

// setupBaseline writes the baseline programs below dir, runs each, and
// fills cover.baseline with what each checkpoint's cover, and
// cover.idle with what a run of idleArgs does.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func setupBaseline(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writing the baseline programs: %w", err)
	}
	idle, code, err := runCovered(input{name: "idle", dir: dir}, idleArgs)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf(
			"the instrumented compiler, run with %v, exits %d, want 0",
			idleArgs,
			code,
		)
	}
	cover.idle = blockSet(idle)
	for st, progs := range baselinePrograms {
		all := covered{}
		for _, p := range progs {
			if err := os.WriteFile(filepath.Join(dir, p.file), []byte(p.src), 0o600); err != nil {
				return fmt.Errorf("writing the baseline programs: %w", err)
			}
			for _, args := range baselineArgs(p) {
				cov, code, err := runCovered(
					input{name: "baseline/" + p.file, dir: dir, file: p.file},
					args,
				)
				if err != nil {
					return err
				}
				if code != 0 {
					return fmt.Errorf(
						"the baseline program %s, run with %v, exits %d, want 0",
						p.file,
						args,
						code,
					)
				}
				for file, starts := range cov {
					all[file] = append(all[file], starts...)
				}
			}
		}
		cover.baseline[st] = blockSet(all)
	}
	return nil
}

// blockSet returns cov's blocks as a set, by file and start.
func blockSet(cov covered) map[string]map[string]bool {
	set := map[string]map[string]bool{}
	for file, starts := range cov {
		set[file] = map[string]bool{}
		for _, s := range starts {
			set[file][s] = true
		}
	}
	return set
}

// blockStages returns the stages whose port does the block at start s
// of file, of stage st (see stagesOf): E1 alone, when E1's baselines run
// a block of a later stage; baselineH alone, when they run one of H's;
// and otherwise st and every checkpoint before it whose baselines run
// it.
func blockStages(file, s string, st, baselineH emitStage) emitSet {
	switch {
	case st == emitH && cover.baseline[emitE1][file][s]:
		return emitSet(0).with(baselineH)
	case st == emitH || st <= emitE1:
		return emitSet(0).with(st)
	case cover.baseline[emitE1][file][s]:
		return emitSet(0).with(emitE1)
	}
	alts := emitSet(0).with(st)
	for _, cp := range []emitStage{emitE2, emitE3} {
		if cp < st && cover.baseline[cp][file][s] {
			alts = alts.with(cp)
		}
	}
	return alts
}

// featureStages returns the emit stages the features of prog need, of
// every package's files, an imported std package's as much as its own,
// since the backend lowers them all, read from its typed tree, apart
// from what its run covers: E2 for a value of a struct, array, union,
// view or any type, a read of an error's field, as a struct's is read,
// a switch, a slice, several values, a type assertion, a division, a
// remainder or a shift, and a call of a function of another package,
// whose arguments may be passed as any, each value's type taken from an
// expression, a parameter, a variable, a result or a type declaration;
// E3 for a value of a list, map, arena, pool, id, span or handle type or
// one that owns, a defer, a region, a ? and an own argument; E4 for a
// value of a function type or a type parameter, a function literal, a
// fields loop, make of a type written as a name and a generic function.
// An error that is only made, passed and returned is the core's.
func featureStages(prog *tackast.Program) emitSet {
	set := emitSet(0).with(emitE0).with(emitE1)
	for _, f := range prog.Files() {
		tackast.Walk(f, func(n tackast.Node) bool {
			set = set.with(nodeStage(n))
			for _, t := range declaredTypes(n) {
				set |= typeStages(prog.Types, t)
			}
			return true
		})
	}
	return set
}

// declaredTypes returns the types of the values n holds or declares: an
// expression's, a parameter's or a variable's, and a function's
// results', so a type written only in a signature counts too.
func declaredTypes(n tackast.Node) []types.Type {
	switch n := n.(type) {
	case *tackast.Param:
		return []types.Type{n.Type}
	case *tackast.Var:
		return []types.Type{n.Type}
	case *tackast.Func:
		return n.ResultTypes
	case *tackast.TypeDecl:
		return []types.Type{n.Type}
	case tackast.Expr:
		return []types.Type{tackast.TypeOf(n)}
	}
	return nil
}

// nodeStage returns the stage featureStages gives the node n itself,
// E1 for one of the core's.
//
//nolint:cyclop // a table, one case a kind
func nodeStage(n tackast.Node) emitStage {
	switch n := n.(type) {
	case *tackast.Switch, *tackast.Slice, *tackast.MultiVar, *tackast.MultiAssign,
		*tackast.TypeAssert:
		return emitE2
	case *tackast.Binary:
		return divides(n.Op)
	case *tackast.Assign:
		return divides(n.Op)
	case *tackast.Ident:
		if _, ok := n.Def.(*tackast.Func); ok && n.Pkg != "" {
			return emitE2
		}
	case *tackast.Selector:
		// An error's field, err.code, is read as a struct's is.
		if tackast.TypeOf(n.X) == types.Error {
			return emitE2
		}
	case *tackast.Defer, *tackast.Region, *tackast.Question, *tackast.OwnArg:
		return emitE3
	case *tackast.FuncLit, *tackast.FieldsOf:
		return emitE4
	case *tackast.Call:
		// make of a type written as a name, make(T, alloc), is
		// makedata.go's.
		if name, ok := builtinName(n); ok && name == "make" && len(n.Args) > 0 {
			switch n.Args[0].(type) {
			case *tackast.Ident, *tackast.Selector:
				return emitE4
			}
		}
	case *tackast.Func:
		if len(n.TypeParams) > 0 {
			return emitE4
		}
	}
	return emitE1
}

// divides returns E2 for a division, a remainder or a shift, whose
// helpers are E2's, and E1 for any other operator.
func divides(op tackast.BinaryOp) emitStage {
	if op == tackast.Div || op == tackast.Rem || op.IsShift() {
		return emitE2
	}
	return emitE1
}

// typeStages returns the stages featureStages gives a value of type t
// of the table tb, each that applies: E4 a function type's or a type
// parameter's, E3 a list's, map's, arena's, pool's, id's, span's,
// handle's or any owning type's, E2 a struct's, array's, union's, view's
// or any's, and E1 otherwise, an error's among them (see nodeStage for
// the read of its field).
//
//nolint:cyclop // a table, one test a kind
func typeStages(tb *types.Table, t types.Type) emitSet {
	set := emitSet(0).with(emitE1)
	if t == types.Invalid {
		return set
	}
	if tb.IsFunc(t) || tb.IsTypeParam(t) {
		set = set.with(emitE4)
	}
	if tb.IsList(t) || tb.IsMap(t) || tb.IsArena(t) || tb.IsPool(t) || tb.IsRef(t) || tb.Owned(t) {
		set = set.with(emitE3)
	}
	if tb.IsStruct(t) || tb.IsArray(t) || tb.IsUnion(t) || tb.IsView(t) || t == types.Any {
		set = set.with(emitE2)
	}
	return set
}

// TestEmitBaselineFeatures checks that each baseline program, run
// through Go's loader, resolver and type checker in process, has no
// error and uses no feature of a stage after its checkpoint (see
// featureStages), judged from its typed tree and not from what it
// covers, so a baseline cannot pass a later stage's work off as its
// checkpoint's; and that its features reach its checkpoint, so it
// covers what it is there for. It needs no compiler and runs in every
// go test ./porttest.
func TestEmitBaselineFeatures(t *testing.T) {
	for cp, progs := range baselinePrograms {
		for _, p := range progs {
			t.Run(p.file, func(t *testing.T) {
				files := fstest.MapFS{p.file: {Data: []byte(p.src)}}
				prog, clean, err := typedTree(files, p.file, nil, true)
				if err != nil {
					t.Fatal(err)
				}
				if prog == nil || !clean {
					t.Fatal("Go's front end reports an error")
				}
				got := featureStages(prog)
				var allowed emitSet
				for st := emitE0; st <= cp; st++ {
					allowed = allowed.with(st)
				}
				if got&^allowed != 0 {
					t.Errorf("%s, a baseline of %s, uses features of %s", p.file, cp, got)
				}
				if cp > emitE1 && !got.has(cp) {
					t.Errorf("%s, a baseline of %s, uses none of its features: %s", p.file, cp, got)
				}
			})
		}
	}
}

// TestFeatureStagesSeesLaterFeatures checks that featureStages finds a
// later stage's feature in a program that has one, an imported
// package's among them, keyed past emitStages to share E3, so that
// TestEmitBaselineFeatures cannot pass by finding nothing.
func TestFeatureStagesSeesLaterFeatures(t *testing.T) {
	cases := map[emitStage]string{
		emitE2: "package main\n\ntype P struct {\n    x int64\n}\n\nfunc main() {\n    p := P{x: 1}\n    if p.x == 1 {\n        print(\"x\")\n    }\n}\n",
		emitE3: "package main\n\nfunc main() {\n    defer {\n        print(\"x\")\n    }\n}\n",
		// std/fmt's printf, whose own code ranges over lists: an
		// imported package's features count.
		emitE3 + emitStages: "package main\n\nimport fmt from \"std/fmt\"\n\nfunc main() {\n    fmt.printf(\"%d\\n\", 1)\n}\n",
		emitE4:              "package main\n\nfunc each(scope f func()) {\n    f()\n}\n\nfunc main() {\n    each(func() {\n        print(\"x\")\n    })\n}\n",
	}
	for st, src := range cases {
		files := fstest.MapFS{"main.tck": {Data: []byte(src)}}
		prog, clean, err := typedTree(files, "main.tck", nil, true)
		if err != nil || prog == nil || !clean {
			t.Fatalf("%s's program does not check: %v", st, err)
		}
		if got := featureStages(prog); !got.has(st % emitStages) {
			t.Errorf("featureStages of a program using %s's feature is %s", st, got)
		}
	}
}

// featureBoundStages are the stages whose cases TestEmitFeatureBound
// checks.
var featureBoundStages = []string{
	emitSingleStage, emitCasesStage, emitPackagesStage, emitLibStage, emitHeaderStage,
}

// featureBoundAllowed are the cases TestEmitFeatureBound lets need a
// stage beyond their features, by stage and input name, each with why.
var featureBoundAllowed = map[string]string{}

// TestEmitFeatureBound checks, for every case of featureBoundStages that
// featureBound can load, a single file or a directory on disk, and that
// Go's front end accepts, that the stages its run
// needs (see stagesOf) are within the stages its features need, as
// featureStages reads them from its typed tree, and H: a case that needs
// a later stage than any feature of it asks for runs a later stage's
// not-taken path no baseline program runs, which a baseline must then
// be given, or the function must be staged where it belongs. Only that
// direction is checked: featureStages counts a division E2's even when
// the checker folded it to a constant, which the backend never sees.
// featureBoundAllowed lists the exceptions, each with why. It runs only
// as TestPort does.
func TestEmitFeatureBound(t *testing.T) {
	ready(t)
	checked := 0
	culprits := map[string]int{}
	for _, name := range featureBoundStages {
		st := stageNamed(name)
		if st == nil {
			t.Fatalf("no %s stage", name)
		}
		ins, err := st.inputs()
		if err != nil {
			t.Fatal(err)
		}
		for _, in := range ins {
			msg, ok, err := checkFeatureBound(*st, in, culprits)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				checked++
			}
			if msg != "" {
				t.Error(msg)
			}
		}
	}
	var b strings.Builder
	for _, f := range slices.Sorted(maps.Keys(culprits)) {
		fmt.Fprintf(&b, "  %4d %s\n", culprits[f], f)
	}
	t.Logf("%d clean cases checked; functions running a stage beyond a case's "+
		"features, by how many cases:\n%s", checked, b.String())
}

// checkFeatureBound checks in, a case of st, as TestEmitFeatureBound
// says: it returns what is wrong, or "", and whether in is a case
// featureBound loads and Go's front end accepts, which is checked at all, counting in culprits
// each function of a block that needs a stage beyond in's features.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func checkFeatureBound(st stage, in input, culprits map[string]int) (string, bool, error) {
	bound, ok, err := featureBound(in)
	if err != nil || !ok {
		return "", false, err
	}
	cov, err := st.classify(in, false)
	if err != nil {
		return "", true, err
	}
	needs, err := stagesOf(cov, emitH)
	if err != nil {
		return "", true, err
	}
	key := st.name + " " + in.name
	_, allowed := featureBoundAllowed[key]
	over := needs &^ bound
	switch {
	case over != 0 && !allowed:
		var funcs []string
		for s := range emitStages {
			if !over.has(s) {
				continue
			}
			for _, f := range stageFuncs(cov, s) {
				funcs = append(funcs, s.String()+" "+f)
				culprits[s.String()+" "+f]++
			}
		}
		return fmt.Sprintf("%s needs %s, beyond its features' %s, by %s", key, needs, bound,
			strings.Join(funcs, ", ")), true, nil
	case over == 0 && allowed:
		return fmt.Sprintf(
			"%s is in featureBoundAllowed but needs only %s; remove it",
			key,
			needs,
		), true, nil
	}
	return "", true, nil
}

// featureBound returns the stages the features of in need, with E0 and
// H, read as typedTree reads it, with the files boundFiles gives and the
// repository's std, and whether it could be read and Go's front end
// accepts it.
func featureBound(in input) (emitSet, bool, error) {
	files, target, single, ok, err := boundFiles(in)
	if err != nil || !ok {
		return 0, false, err
	}
	prog, clean, err := typedTree(files, target, nil, single)
	if err != nil {
		return 0, false, fmt.Errorf("checking %s: %w", in.name, err)
	}
	if prog == nil || !clean {
		// A case Go's front end does not accept has no bound.
		return 0, false, nil
	}
	return featureStages(prog).with(emitH), true, nil
}

// boundFiles returns the files featureBound loads in from, by their
// paths from in.dir, the target, and whether it is a single file: a
// single .tck file alone, or a directory with every file a load can read
// below the directory scopeOf gives; and whether in is one featureBound
// can load at all: not a target given by its import path, nor the
// port's driver, which loads more of the port than typedTree's timeout
// allows.
func boundFiles(in input) (fstest.MapFS, string, bool, bool, error) {
	target := filepath.Join(in.dir, filepath.FromSlash(in.file))
	if path.Ext(in.file) == ".tck" {
		data, err := os.ReadFile(target) //nolint:gosec // G304: the corpus
		if err != nil {
			return nil, "", false, false, fmt.Errorf("reading %s: %w", in.name, err)
		}
		name := path.Base(in.file)
		return fstest.MapFS{name: {Data: data}}, name, true, true, nil
	}
	if in.name == "port/driver" || !dirExists(target) {
		return nil, "", false, false, nil
	}
	scope, _ := scopeOf(in)
	files, err := scopeFiles(in.dir, scope)
	if err != nil {
		return nil, "", false, false, fmt.Errorf("reading %s: %w", in.name, err)
	}
	return files, path.Clean(in.file), false, true, nil
}

// scopeFiles returns every file a load can read below scope, by its
// slash-separated path from dir.
func scopeFiles(dir, scope string) (fstest.MapFS, error) {
	files := fstest.MapFS{}
	err := filepath.WalkDir(scope, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !readable(d.Name()) {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return fmt.Errorf("naming %s: %w", p, err)
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: the corpus
		if err != nil {
			return fmt.Errorf("reading the corpus: %w", err)
		}
		files[filepath.ToSlash(rel)] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", scope, err)
	}
	return files, nil
}

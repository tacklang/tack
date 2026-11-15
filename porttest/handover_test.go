package porttest

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
	"github.com/tacklang/tack/types"
)

// The parts of what the port's backend will need that no stage shows
// until the backend is ported, checked here with port-only flags: the
// type table's bindings, --port-bind (see TestBind), and the C literal
// of a float constant, --port-floats (see TestHexFloats).

// bindInstances is how many instances the --port-bind dump binds, the
// port's max_bind_instances.
const bindInstances = 16

// TestBind compares the port's --port-bind dump of every input of the
// corpus, single files and packages, that Go accepts with no error and
// whose checked program has instances of generic functions, with
// bindDump's of Go's checked program: what Go's Table.Bind, BindMore,
// Unbind, Bound, HasTypeParam and NameWith give, and the IDs of the
// types they add, in order. A decline fails: the type checker checks
// every such program, and a binding the port declines is one it must
// not meet. It runs only as TestPort does.
func TestBind(t *testing.T) {
	ready(t)
	singles, err := singleFiles()
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := packageInputs()
	if err != nil {
		t.Fatal(err)
	}
	var compared atomic.Int64
	t.Run("corpus", func(t *testing.T) {
		for _, group := range []struct {
			name string
			ins  []input
		}{{"single", singles}, {"packages", pkgs}} {
			t.Run(group.name, func(t *testing.T) {
				for _, in := range group.ins {
					t.Run(in.name, func(t *testing.T) {
						t.Parallel()
						if checkBind(t, in) {
							compared.Add(1)
						}
					})
				}
			})
		}
	})
	t.Logf("%d inputs have instances and were compared", compared.Load())
}

// checkBind compares the port's --port-bind dump of in with bindDump's,
// for an input Go accepts with no error and whose program has
// instances, and reports whether in is one; any other passes at once.
func checkBind(t *testing.T, in input) bool {
	t.Helper()
	res, ok := typesLoad(t, in)
	if !ok || res.Program == nil || diag.HasErrors(res.Diags) {
		return false
	}
	prog := res.Program
	if diag.HasErrors(resolver.New(prog, builtin.Default()).Resolve()) {
		return false
	}
	if diag.HasErrors(typeck.New(prog, res.Files, builtin.Default()).Check()) || len(prog.Instances) == 0 {
		return false
	}
	want := bindDump(prog)
	port := runCompiler(t, env.portRun, env.portCache, in, []string{"--port-bind", in.file})
	if port.code != 0 || len(port.stderr) > 0 {
		t.Fatalf("the port exited %d, writing to standard error:\n%s", port.code, port.stderr)
	}
	if d := firstDiff([]byte(want), port.stdout); d != "" {
		t.Errorf("the bindings differ: %s\ncommand: (cd %s && tack --port-bind %s)", d, in.dir, in.file)
	}
	return true
}

// bindDump returns what the port's --port-bind writes for prog, checked
// in full with no error, as its typeck/binddump.tck describes it, from
// Go's Table: each type's Bound, HasTypeParam and NameWith, in that
// order, under each of the first bindInstances instances' binding, and
// each type's Bound with each type parameter bound to int64 on top of
// it by BindMore, and then every type the bindings added; before each
// type's line under an instance's binding, what every query gives about
// it (see writeQueries) and its name as header.go qualifies it, after
// instance the binding, Binding's, and after the last none.
func bindDump(prog *ast.Program) string {
	table := prog.Types
	paths := make(map[types.Type]string)
	pkgOf := make(map[types.Type]*ast.Package)
	funcPkg := make(map[*ast.Func]*ast.Package)
	for _, pkg := range prog.Packages {
		for _, d := range pkg.Decls() {
			if td, ok := d.(*ast.TypeDecl); ok && (table.IsStruct(td.Type) || table.IsUnion(td.Type)) {
				paths[td.Type] = pkg.Path
				pkgOf[td.Type] = pkg
			}
			if fn, ok := d.(*ast.Func); ok {
				funcPkg[fn] = pkg
			}
		}
	}
	qualify := func(st types.Type, n string) string {
		if p, ok := paths[st]; ok {
			return p + "." + n
		}
		return n
	}
	var b strings.Builder
	n0 := table.Count()
	fmt.Fprintf(&b, "types %d\n", n0)
	for i, inst := range prog.Instances {
		if i >= bindInstances {
			break
		}
		tps := inst.Func.TypeParams
		np := min(len(tps), len(inst.TypeArgs))
		args := make(map[types.Type]types.Type)
		for j := range np {
			args[tps[j].Type] = inst.TypeArgs[j]
		}
		table.Bind(args)
		fmt.Fprintf(&b, "instance %d\n", i)
		writeBinding(&b, table)
		instPkg := funcPkg[inst.Func]
		header := func(st types.Type, n string) string {
			if p := pkgOf[st]; p != nil && p != instPkg {
				return p.Name + "." + n
			}
			return n
		}
		for x := range n0 {
			typ := types.Added(x)
			writeQueries(&b, table, typ, x%queryCount)
			fmt.Fprintf(&b, "header %d %s\n", typ, capped(table.NameWith(typ, header)))
			bound := table.Bound(typ)
			flag := 0
			if table.HasTypeParam(typ) {
				flag = 1
			}
			fmt.Fprintf(&b, "%d %d %d %s\n", typ, bound, flag, capped(table.NameWith(typ, qualify)))
		}
		for j := range np {
			restore := table.BindMore(map[types.Type]types.Type{tps[j].Type: types.Int64})
			fmt.Fprintf(&b, "more %d\n", j)
			for x := range n0 {
				typ := types.Added(x)
				fmt.Fprintf(&b, "%d %d\n", typ, table.Bound(typ))
			}
			restore()
		}
		table.Unbind()
	}
	b.WriteString("unbound\n")
	writeBinding(&b, table)
	n := table.Count()
	fmt.Fprintf(&b, "count %d\n", n)
	for x := n0; x < n; x++ {
		typ := types.Added(x)
		fmt.Fprintf(&b, "%d %s %s\n", typ, typeKind(table, typ), capped(table.NameWith(typ, qualify)))
	}
	return b.String()
}

// maxDumpedName is the port's max_dumped_name: a longer name is written
// as its length and its hash (see capped).
const maxDumpedName = 200

// capped returns name as the port's write_capped writes it: as it is, or,
// longer than maxDumpedName, #, its length, a colon and its 64-bit
// FNV-1a hash in hexadecimal, which keeps the dump of a deeply nested
// type linear in its depth.
func capped(name string) string {
	if len(name) <= maxDumpedName {
		return name
	}
	h := fnv.New64a()
	h.Write([]byte(name))
	return fmt.Sprintf("#%d:%x", len(name), h.Sum64())
}

// writeBinding writes what the port's write_binding writes: the binding
// table's Binding gives, each type parameter and what it is bound to in
// the order of their IDs, or none for a nil one.
func writeBinding(b *strings.Builder, table *types.Table) {
	binding := table.Binding()
	if binding == nil {
		b.WriteString("binding none\n")
		return
	}
	keys := make([]types.Type, 0, len(binding))
	for k := range binding {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	b.WriteString("binding")
	for _, k := range keys {
		fmt.Fprintf(b, " %d:%d", k, binding[k])
	}
	b.WriteString("\n")
}

// queryCount is how many queries writeQueries asks, the port's
// query_count.
const queryCount = 31

// writeQueries writes what the port's write_queries writes for typ: the
// answer of each of table's queries about it, each after its number and
// with the table's count once it is answered, asked from the one
// numbered first on, in turn, wrapping, so each query is the first asked
// about some types, the one that adds what a binding makes of them, and
// one that adds a type out of Go's order shows.
func writeQueries(b *strings.Builder, table *types.Table, typ types.Type, first int) {
	bools := []func(types.Type) bool{
		table.IsFunc, table.IsUnion, table.IsTypeParam, table.IsArray, table.IsArena, table.IsID,
		table.IsSpan, table.IsRef, table.IsPool, table.IsHandle, table.Buffered, table.IsList,
		table.IsMap, table.IsView, table.InAny, table.IsStruct,
	}
	more := []func(types.Type) bool{
		table.Owned, table.HasZero, table.HoldsList, table.HoldsArena, table.HoldsPool, table.HoldsUnion,
	}
	bit := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	ask := func(which int) string {
		switch {
		case which < 16:
			return bit(bools[which](typ))
		case which == 16:
			return strconv.Itoa(int(table.Key(typ)))
		case which == 17:
			return strconv.Itoa(int(table.Elem(typ)))
		case which == 18:
			return strconv.FormatInt(table.Len(typ), 10)
		case which < 25:
			return bit(more[which-19](typ))
		case which == 25:
			size := table.Size(typ)
			over := 0
			if size.Overflow {
				over = 1
			}
			return fmt.Sprintf("%d,%d,%d", size.Bytes, size.Align, over)
		case which == 26:
			return capped(table.Name(typ))
		}
		var parts []types.Type
		var tag string
		switch which {
		case 27:
			tag = "p"
			for _, p := range table.FuncParams(typ) {
				parts = append(parts, p.Type)
			}
		case 28:
			tag = "r"
			parts = table.FuncResults(typ)
		case 29:
			tag = "c"
			for _, c := range table.Cases(typ) {
				parts = append(parts, c.Type)
			}
		default:
			tag = "f"
			for _, f := range table.Fields(typ) {
				parts = append(parts, f.Type)
			}
		}
		out := tag + strconv.Itoa(len(parts))
		for _, p := range parts {
			out += "," + strconv.Itoa(int(p))
		}
		return out
	}
	fmt.Fprintf(b, "q %d", typ)
	for k := range queryCount {
		which := (first + k) % queryCount
		answer := ask(which)
		fmt.Fprintf(b, " %d:%s/%d", which, answer, table.Count())
	}
	b.WriteString("\n")
}

// deepBindShapes are the programs TestBindDeep checks, each n levels
// deep: a generic function of two type parameters that makes, in its
// body, values of types nesting both n levels deep, in lists, maps and
// function types, which the type checker never makes for any type
// argument, so each instance's binding adds every level, in its order,
// called twice with two struct types swapped, so the second binding
// meets types the first added.
var deepBindShapes = map[string]func(n int) string{
	"lists": func(n int) string {
		return bindShape(rep("list[", n)+"T"+rep("]", n), rep("list[map[int64]", n/2)+"U"+rep("]", n/2))
	},
	"maps": func(n int) string {
		return bindShape(rep("map[int64]", n)+"T", "list["+rep("map[int64]", n)+"list[U]]")
	},
	"funcs": func(n int) string {
		return bindShape("list["+rep("func(", n)+"T"+rep(")", n)+"]", "list["+rep("func(U, ", n)+"T"+rep(")", n)+"]")
	},
}

// bindShape returns a program whose generic function f makes values of
// a and b, types written of its type parameters T and U, and whose main
// calls it with two struct types and then with them swapped.
func bindShape(a, b string) string {
	return "package main\n\ntype S struct {\n    x int64\n}\n\ntype R struct {\n    y int64\n}\n\n" +
		"func f[T Data, U Data](_ T, _ U) int64 {\n    a := make(" + a + ", c_allocator)\n" +
		"    b := make(" + b + ", c_allocator)\n    return len(a) +% len(b)\n}\n\n" +
		"func main() {\n    if f(S{x: 1}, R{y: 2}) +% f(R{y: 1}, S{x: 2}) > 0 {\n        print(\"x\")\n    }\n}\n"
}

// TestBindDeep runs each of deepBindShapes, nested to bindDepth levels,
// through both compilers as TestBind does, the port's stack held to
// deepStack KB: a port whose bindings recurse with a type's depth fails
// here. Each shape must be Go-clean, with an instance. It runs only as
// TestPort does.
func TestBindDeep(t *testing.T) {
	ready(t)
	for name, shape := range deepBindShapes {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := shape(bindDepth)
			if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			in := input{name: name, dir: dir, file: "main.tck"}
			res, ok := typesLoad(t, in)
			if !ok || res.Program == nil {
				t.Fatal("the shape does not load")
			}
			if diags := resolver.New(res.Program, builtin.Default()).Resolve(); diag.HasErrors(diags) {
				t.Fatalf("the shape does not resolve: %v", diags)
			}
			if diags := typeck.New(res.Program, res.Files, builtin.Default()).Check(); diag.HasErrors(diags) {
				t.Fatalf("the shape does not check: %s", diag.RenderAllPretty(res.Files, diags))
			}
			if len(res.Program.Instances) == 0 {
				t.Fatal("the shape has no instance")
			}
			if !checkBind(t, in) {
				t.Fatal("the shape was not compared")
			}
		})
	}
}

// bindDepth is how deeply TestBindDeep nests a type: as deeply as the
// parser lets a type nest, less a few levels for what is around it.
const bindDepth = deepLimit - 10

// TestHexFloats compares the port's --port-floats, the C literal the C
// backend writes for a float constant, with Go's backend's floatLiteral
// (see goFloatLiteral), on float64s and float32s of every kind: zeros of
// both signs, the infinities and NaNs, the smallest and largest
// subnormals and normals, powers of two across the whole range of
// exponents, values a bit either side of them, and random bit patterns,
// a fixed seed's, of normals and subnormals alike. It runs only as
// TestPort does.
func TestHexFloats(t *testing.T) {
	ready(t)
	var lines, want strings.Builder
	add := func(bits uint64, is32 bool) {
		if is32 {
			fmt.Fprintf(&lines, "32 %x\n", uint32(bits))
			want.WriteString(goFloatLiteral(float64(math.Float32frombits(uint32(bits))), true) + "\n")
			return
		}
		fmt.Fprintf(&lines, "64 %x\n", bits)
		want.WriteString(goFloatLiteral(math.Float64frombits(bits), false) + "\n")
	}
	for _, v := range []float64{
		0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.NaN(), 1, -1, 0.5, 1.5, 0.1, -0.1,
		math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64, 0x1p-1022, 0x1.fffffffffffffp-1023,
		math.MaxFloat32, math.SmallestNonzeroFloat32, 0x1p-126, math.Pi, math.E, 3, 100, 1e300, 1e-300,
	} {
		add(math.Float64bits(v), false)
		add(uint64(math.Float32bits(float32(v))), true)
	}
	for _, special := range []uint64{0x7ff0000000000001, 0xfff8000000000000, 0x7fffffffffffffff, 1, 0xfffffffffffff, 0x10000000000000} {
		add(special, false)
	}
	for _, special := range []uint32{0x7f800001, 0xffc00000, 0x7fffffff, 1, 0x7fffff, 0x800000, 0x80000001, 0x807fffff} {
		add(uint64(special), true)
	}
	for e := -1074; e <= 1023; e++ {
		p := math.Ldexp(1, e)
		for _, v := range []float64{p, -p, math.Nextafter(p, 0), math.Nextafter(p, math.Inf(1))} {
			add(math.Float64bits(v), false)
		}
	}
	for e := -149; e <= 127; e++ {
		p := float32(math.Ldexp(1, e))
		for _, v := range []float32{p, -p, math.Nextafter32(p, 0), math.Nextafter32(p, float32(math.Inf(1)))} {
			add(uint64(math.Float32bits(v)), true)
		}
	}
	r := rand.New(rand.NewPCG(7, 11))
	for range 20000 {
		add(r.Uint64(), false)
		add(r.Uint64()&0x800fffffffffffff, false)
		add(uint64(r.Uint32()), true)
		add(uint64(r.Uint32()&0x807fffff), true)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "floats.txt"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	port := runCompiler(t, env.portRun, env.portCache, input{name: "floats", dir: dir, file: "floats.txt"},
		[]string{"--port-floats", "floats.txt"})
	if port.code != 0 || len(port.stderr) > 0 {
		t.Fatalf("the port exited %d, writing to standard error:\n%s", port.code, port.stderr)
	}
	if !bytes.Equal(port.stdout, []byte(want.String())) {
		t.Errorf("the literals differ: %s", firstDiff([]byte(want.String()), port.stdout))
	}
}

// goFloatLiteral is the Go backend's floatLiteral (codegen/c gen.go:889),
// which is unexported, written again: f, a float32's value when is32
// says so, as a hexadecimal float, f after a float32's, parenthesized
// when negative.
func goFloatLiteral(f float64, is32 bool) string {
	s := strconv.FormatFloat(f, 'x', -1, 64)
	if is32 {
		s += "f"
	}
	if math.Signbit(f) {
		return "(" + s + ")"
	}
	return s
}

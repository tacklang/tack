package porttest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
	"github.com/tacklang/tack/types"
)

// typesCheck is TestTypes's name in required.txt.
const typesCheck = "types-decls"

// bodiesCheck is the name in required.txt of TestTypes's check that the
// port checks every body of an input whose Go typed tree's stage is S1,
// S2a, S2b, S3 or S4 (see treeStage): its dump must be of the phase
// "bodies".
const bodiesCheck = "types-bodies"

// TestTypes checks what the port's type checker finds, so far in its
// declaration phase, for every input of the corpus, single files and
// packages, the port's own driver among them, that Go accepts with no
// error, and for every case of check-decls, the cases whose errors Go
// reports before its first body, errors and all: the port writes it with
// --port-types, a flag of its own, and this module's loader, resolver
// and checker, run in process as the driver runs them, give what it must
// be (see goTypes). Any other input is no input of the check and passes
// at once, counted in the log. The port runs with its stack held to
// deepStack KB, as every run of it does. It runs only as TestPort does.
func TestTypes(t *testing.T) {
	ready(t)
	singles, err := singleFiles()
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := packageInputs()
	if err != nil {
		t.Fatal(err)
	}
	decls, err := checkInputs(stageDecls)
	if err != nil {
		t.Fatal(err)
	}
	core, err := checkInputs(stageCore)
	if err != nil {
		t.Fatal(err)
	}
	calls, err := checkInputs(stageCalls)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := checkInputs(stageFacts)
	if err != nil {
		t.Fatal(err)
	}
	arith, err := checkInputs(stageArith)
	if err != nil {
		t.Fatal(err)
	}
	owns, err := checkInputs(stageOwn)
	if err != nil {
		t.Fatal(err)
	}
	generics, err := checkInputs(stageGeneric)
	if err != nil {
		t.Fatal(err)
	}
	var clean, all, bodies atomic.Int64
	var assumed sync.Map
	t.Run("corpus", func(t *testing.T) {
		for _, group := range []struct {
			name  string
			ins   []input
			clean bool
		}{
			{"single", singles, true}, {"packages", pkgs, true},
			{"check-decls", decls, false}, {"check-core", core, false},
			{"check-calls", calls, false}, {"check-facts", facts, false},
			{"check-arith", arith, false}, {"check-own", owns, false},
			{"check-generic", generics, false},
		} {
			if group.name == checkStageInfo[stageOwn].stage && !env.required[group.name] {
				// A declined case's errors are its bodies', which the
				// declaration phase's dump cannot hold; until check-own
				// is required, some of its cases are declined.
				continue
			}
			t.Run(group.name, func(t *testing.T) {
				for _, in := range group.ins {
					t.Run(in.name, func(t *testing.T) {
						t.Parallel()
						all.Add(1)
						compared, kinds, phase := checkTypesOf(t, in, env.portRun, group.clean, group.name == checkStageInfo[stageGeneric].stage)
						if compared {
							clean.Add(1)
						}
						if phase == "bodies" {
							bodies.Add(1)
						}
						for _, k := range kinds {
							assumed.Store(k, true)
						}
					})
				}
			})
		}
	})
	t.Logf("%d of %d inputs are inputs of the check and were compared, %d of them with every body checked",
		clean.Load(), all.Load(), bodies.Load())
	var kinds []string
	assumed.Range(func(k, _ any) bool {
		kinds = append(kinds, k.(string)) //nolint:forcetypeassert // only strings are stored
		return true
	})
	slices.Sort(kinds)
	t.Logf("judgments the port assumed: %q", kinds)
}

// checkTypes compares the port's --port-types dump of in with goTypes',
// which clean says must be of a program Go accepts, and reports whether
// in is an input of the check, the kinds of judgment the port's dump
// says it assumed, and the dump's phase. The port must check every body
// of an input whose Go typed tree's stage is one its body check covers
// (see bodiesStage), when types-bodies is required.
func checkTypes(t *testing.T, in input, portBin string, clean bool) (bool, []string, string) {
	t.Helper()
	return checkTypesOf(t, in, portBin, clean, false)
}

// checkTypesOf is checkTypes, but for an input whose dump, declinable
// says, may be of the phase "decls" when its errors are its bodies':
// one the port's body check need not cover (see bodiesStage) skips
// when the port declines its bodies, as a case of check-generic that
// waits for a later stage does.
func checkTypesOf(t *testing.T, in input, portBin string, clean, declinable bool) (bool, []string, string) {
	t.Helper()
	want, stage, stages, ok := goTypes(t, in, clean)
	if !ok {
		return false, nil, ""
	}
	port := runCompiler(t, portBin, env.portCache, in, []string{"--port-types", in.file})
	if port.notImplemented() {
		msg := strings.TrimSuffix(string(port.stderr), "\n")
		if env.required[typesCheck] {
			t.Fatalf("%s is required, but the port says %q", typesCheck, msg)
		}
		t.Skip(msg)
	}
	if port.code != 0 || len(port.stderr) > 0 {
		t.Fatalf("the port exited %d, writing to standard error:\n%s", port.code, port.stderr)
	}
	if declinable && !bodiesStage(stage, stages) && bytes.Contains(port.stdout, []byte(`"phase":"decls"`)) {
		t.Skip("the port declines a body, which waits for a later stage")
	}
	if d := typesDiff(want, port.stdout); d != "" {
		t.Errorf("what the checker finds differs: %s\ncommand: (cd %s && tack --port-types %s)",
			d, in.dir, in.file)
	}
	var dump struct {
		Assumed []string `json:"assumed"`
		Phase   string   `json:"phase"`
	}
	if err := json.Unmarshal(port.stdout, &dump); err != nil {
		t.Errorf("reading what the port assumed: %v", err)
	}
	if bodiesStage(stage, stages) && dump.Phase != "bodies" && env.required[bodiesCheck] {
		t.Errorf("%s is required, and Go's typed tree is %s's, but the port's dump is of the phase %q",
			bodiesCheck, stage, dump.Phase)
	}
	return true, dump.Assumed, dump.Phase
}

// bodiesStage reports whether stage, a Go typed tree's (see treeStage),
// is one whose every body the port's body check covers: S1, S2a, S2b,
// S3 or S4, and S6, ownership, once check-own is in required.txt: until
// then some of its inputs need the loops, and the port declines them.
// S7, generics and closures, is covered for a tree that implies no S5
// anywhere, which stages, every stage the tree implies (see
// treeStages), says.
func bodiesStage(stage checkStage, stages map[checkStage]bool) bool {
	if stage == stageGeneric {
		return !stages[stageLoops]
	}
	return stage <= stageArith || stage == stageOwn && env.required[checkStageInfo[stageOwn].stage]
}

// typesDiff returns how got, the port's dump, differs from want, Go's,
// or "". The port's phase says what is compared: when it is "bodies",
// everything the dump holds, Go's whole table, every node and the
// errors included; when it is "decls", the port having declined a body,
// the declarations, the errors, and the table's entries up to the port's
// count, which must not be past Go's: everything Go adds before its first
// body comes first, so the port's table is a prefix of Go's. A Func's
// value is set only where a body uses the function as a value (see
// signatureValue in typeck/funcvalue.go), so in the declaration phase
// Go's is compared as it stood before bodies, invalid; and instances are
// found only after bodies. The kinds of judgment the port assumed are
// logged by the caller's run, not compared.
func typesDiff(want, got []byte) string {
	w, err := decodeJSON(want)
	if err != nil {
		return fmt.Sprintf("Go's dump: %v", err)
	}
	g, err := decodeJSON(got)
	if err != nil {
		return fmt.Sprintf("the port's dump: %v", err)
	}
	wm, _ := w.(map[string]any)
	gm, ok := g.(map[string]any)
	if ok && gm["phase"] == "bodies" {
		if reflect.DeepEqual(wm, gm) {
			return ""
		}
		return firstDiff(indent(wm), indent(gm))
	}
	if !ok || gm["phase"] != "decls" {
		return fmt.Sprintf("the port's dump has no phase this check knows: %v", gm["phase"])
	}
	wantTable, _ := wm["table"].([]any)
	gotTable, _ := gm["table"].([]any)
	if len(gotTable) > len(wantTable) {
		return fmt.Sprintf("the port's table has %d types, Go's %d", len(gotTable), len(wantTable))
	}
	decls, _ := wm["decls"].([]any)
	for _, d := range decls {
		if fn, isMap := d.(map[string]any); isMap && fn["decl"] == "Func" {
			fn["value"] = json.Number("0")
		}
	}
	delete(wm, "nodes")
	pick := func(m map[string]any, table []any) map[string]any {
		return map[string]any{"table": table, "decls": m["decls"], "errors": m["errors"]}
	}
	wv, gv := pick(wm, wantTable[:len(gotTable)]), pick(gm, gotTable)
	if reflect.DeepEqual(wv, gv) {
		return ""
	}
	return firstDiff(indent(wv), indent(gv))
}

// goTypes returns the --port-types dump of in from this module's
// compiler, in the phase "bodies", the stage its typed tree implies
// (see treeStage), and whether in is an input of the check: loaded as the
// driver loads it, with the repository's std or the one in's TACK_HOME
// names, resolved, and checked in full by typeck.New(...).Check(). When
// clean says so, only a program Go accepts with no error is one; when
// not, any program the loader builds is, checked as the port's
// --port-types checks it, whatever the stages before reported.
func goTypes(t *testing.T, in input, clean bool) ([]byte, checkStage, map[checkStage]bool, bool) {
	t.Helper()
	res, ok := typesLoad(t, in)
	if !ok || res.Program == nil || clean && diag.HasErrors(res.Diags) {
		return nil, 0, nil, false
	}
	prog := res.Program
	if diag.HasErrors(resolver.New(prog, builtin.Default()).Resolve()) && clean {
		return nil, 0, nil, false
	}
	diags := typeck.New(prog, res.Files, builtin.Default()).Check()
	if clean && diag.HasErrors(diags) {
		return nil, 0, nil, false
	}
	out, err := json.Marshal(typesDump(prog, diags))
	if err != nil {
		t.Fatal(err)
	}
	stage, _, stages := treeStages(prog, "")
	return out, stage, stages, true
}

// typesLoad loads in's target as the driver's loadWith does: a directory,
// an import path when it names nothing on disk and looks like one, or a
// single file, from in's directory as the current one, files shown
// relative to it, with the repository's std. It reports false when the
// load fails.
func typesLoad(t *testing.T, in input) (*loader.Result, bool) {
	t.Helper()
	cwd, err := filepath.EvalSymlinks(in.dir)
	if err != nil {
		t.Fatal(err)
	}
	home := env.root
	for _, kv := range in.env {
		if h, found := strings.CutPrefix(kv, "TACK_HOME="); found {
			home = h
		}
	}
	l := &loader.Loader{
		FS: loader.OS{}, Builtins: builtin.Default(), MaxErrors: 10,
		Std:     &loader.Std{Dir: filepath.ToSlash(filepath.Join(home, "src", "std"))},
		Display: relativeTo(cwd),
	}
	full := in.file
	if !filepath.IsAbs(full) {
		full = filepath.Join(cwd, full)
	}
	info, statErr := os.Stat(full)
	var res *loader.Result
	switch {
	case statErr == nil && info.IsDir():
		res, err = l.Dir(filepath.ToSlash(full))
	case errors.Is(statErr, fs.ErrNotExist) && looksLikeImportPath(in.file):
		res, err = l.Path(filepath.ToSlash(cwd), in.file)
	default:
		res, err = l.File(filepath.ToSlash(full))
	}
	return res, err == nil
}

// typesDump returns what --port-types writes for prog, checked in full,
// and diags, the checker's diagnostics, as the port's typeck/dump.tck
// describes it: the phase, every type of the table with every query it
// answers, every declaration in prog.Decls() order with what the checker
// recorded on it, the instances, the errors and nothing assumed.
func typesDump(prog *ast.Program, diags []diag.Diagnostic) map[string]any {
	declPkg := make(map[ast.Decl]*ast.Package)
	for _, pkg := range prog.Packages {
		for _, d := range pkg.Decls() {
			declPkg[d] = pkg
		}
	}
	table := prog.Types
	types := make([]any, 0, table.Count())
	for i := range table.Count() {
		types = append(types, typeEntry(table, typesAdded(i)))
	}
	decls := make([]any, 0)
	for _, d := range prog.Decls() {
		decls = append(decls, declEntry(d, declPkg))
	}
	instances := make([]any, 0)
	for _, inst := range prog.Instances {
		instances = append(instances, map[string]any{
			"func": inst.Func.Name, "type_args": typeList(inst.TypeArgs),
		})
	}
	errs := make([]any, 0)
	for _, d := range diags {
		if d.Severity != diag.Error {
			continue
		}
		notes := make([]any, 0, len(d.Related))
		for _, n := range d.Related {
			notes = append(notes, diagEntry(n))
		}
		e := diagEntry(d)
		e["notes"] = notes
		errs = append(errs, e)
	}
	return map[string]any{
		"phase": "bodies", "table": types, "decls": decls, "instances": instances,
		"errors": errs, "nodes": nodesDump(prog), "assumed": []any{},
	}
}

// nodesDump returns the dump's list of the nodes of prog's files, in
// package order, each file's in the order ast.Walk visits them, as the
// port's typeck/nodes.tck describes it: each node's kind, its type, its
// constant, what a call converts to, what a name was narrowed to,
// whether a switch is exhaustive, whether a variable was unrolled and
// whether a name moves its variable's value.
func nodesDump(prog *ast.Program) []any {
	out := make([]any, 0)
	for _, pkg := range prog.Packages {
		for _, f := range pkg.Files {
			ast.Walk(f, func(n ast.Node) bool {
				if _, isFile := n.(*ast.File); !isFile {
					out = append(out, nodeEntry(n))
				}
				return true
			})
		}
	}
	return out
}

// nodeEntry returns the dump's entry for n.
func nodeEntry(n ast.Node) map[string]any {
	e := map[string]any{
		"kind": strings.TrimPrefix(fmt.Sprintf("%T", n), "*ast."), "type": 0, "const": nil,
		"conv": 0, "narrowed": 0, "exhaustive": false, "unrolled": false, "move": false,
	}
	if x, ok := n.(ast.Expr); ok {
		e["type"] = int(ast.TypeOf(x))
	}
	switch x := n.(type) {
	case *ast.Var:
		e["type"], e["const"], e["unrolled"] = int(x.Type), constEntry(x.Const), x.Unrolled
	case *ast.Param:
		e["type"] = int(x.Type)
	case *ast.Call:
		e["conv"], e["const"] = int(x.Conv), constEntry(x.Const)
		if x.TypeArgs != nil {
			e["type_args"] = typeList(x.TypeArgs)
			e["param_types"] = typeList(x.ParamTypes)
			e["result_types"] = typeList(x.ResultTypes)
		}
	case *ast.Binary:
		e["const"] = constEntry(x.Const)
	case *ast.Unary:
		e["const"] = constEntry(x.Const)
	case *ast.Ident:
		e["narrowed"], e["move"] = int(x.Narrowed), x.Move
	case *ast.Switch:
		e["exhaustive"] = x.Exhaustive
		if x.OnType {
			clauses := make([]any, 0, len(x.Cases))
			for _, cl := range x.Cases {
				tcs := make([]any, 0, len(cl.TypeCases))
				for _, tc := range cl.TypeCases {
					tcs = append(tcs, map[string]any{
						"kind": int(tc.Kind), "type": int(tc.Type), "key": int(tc.Key),
					})
				}
				clauses = append(clauses, tcs)
			}
			e["type_cases"] = clauses
		}
	case *ast.FuncLit:
		caps := make([]any, 0, len(x.Captures))
		for _, cp := range x.Captures {
			writeAt := 0
			if cp.Write {
				writeAt = cp.WriteAt.Start
			}
			caps = append(caps, map[string]any{
				"decl": declName(cp.Decl), "at": cp.At.Start, "write": cp.Write, "write_at": writeAt,
			})
		}
		e["captures"] = caps
	case *ast.FieldsOf:
		unrolled := make([]any, 0, len(x.Unrolled))
		for _, u := range x.Unrolled {
			fields := make([]any, 0, len(u.Fields))
			for _, i := range u.Fields {
				fields = append(fields, i)
			}
			unrolled = append(unrolled, map[string]any{"type": int(u.Type), "fields": fields})
		}
		e["fo_type"], e["fo_field"], e["names"] = int(x.Type), int(x.Field), x.Names
		e["fields_for"] = unrolled
	}
	return e
}

// diagEntry returns the dump's form of d, a diagnostic or a note: its
// file, its span's start and end, and its message.
func diagEntry(d diag.Diagnostic) map[string]any {
	return map[string]any{
		"file": d.Span.File, "start": d.Span.Start, "end": d.Span.End, "message": d.Message,
	}
}

// declName returns the name decl, a variable or a parameter a closure
// captures, declares.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.Var:
		return d.Name
	case *ast.Param:
		return d.Name
	default:
		return ""
	}
}

// typesAdded is types.Added, the ID of the i'th type a Table added.
func typesAdded(i int) types.Type { return types.Added(i) }

// typeKind names typ's kind as the dump does.
func typeKind(t *types.Table, typ types.Type) string {
	switch {
	case t.IsArray(typ):
		return "array"
	case t.IsView(typ):
		return "view"
	case t.IsList(typ):
		return "list"
	case t.IsMap(typ):
		return "map"
	case t.IsArena(typ):
		return "arena"
	case t.IsID(typ):
		return "id"
	case t.IsSpan(typ):
		return "span"
	case t.IsPool(typ):
		return "pool"
	case t.IsHandle(typ):
		return "handle"
	case t.IsStruct(typ):
		return "struct"
	case t.IsUnion(typ):
		return "union"
	case t.IsFunc(typ):
		return "func"
	case t.IsTypeParam(typ):
		return "param"
	default:
		return ""
	}
}

// typeEntry returns the dump's entry for typ, a type of t.
func typeEntry(t *types.Table, typ types.Type) map[string]any {
	fields := make([]any, 0)
	for _, f := range t.Fields(typ) {
		fields = append(fields, map[string]any{"name": f.Name, "type": int(f.Type)})
	}
	cases := make([]any, 0)
	for _, c := range t.Cases(typ) {
		cases = append(cases, map[string]any{"name": c.Name, "type": int(c.Type), "payload": c.Payload})
	}
	params := make([]any, 0)
	for _, p := range t.FuncParams(typ) {
		params = append(params, map[string]any{
			"type": int(p.Type), "mut": p.Mut, "own": p.Own, "scope": p.Scope,
		})
	}
	results := make([]any, 0)
	for _, r := range t.FuncResults(typ) {
		results = append(results, int(r))
	}
	size := t.Size(typ)
	return map[string]any{
		"id": int(typ), "kind": typeKind(t, typ), "name": t.Name(typ),
		"elem": int(t.Elem(typ)), "len": t.Len(typ), "key": int(t.Key(typ)),
		"fields": fields, "cases": cases, "params": params, "results": results,
		"size":  map[string]any{"bytes": size.Bytes, "align": size.Align, "overflow": size.Overflow},
		"owned": t.Owned(typ), "has_zero": t.HasZero(typ), "holds_list": t.HoldsList(typ),
		"holds_arena": t.HoldsArena(typ), "holds_pool": t.HoldsPool(typ),
		"holds_union": t.HoldsUnion(typ), "in_any": t.InAny(typ), "buffered": t.Buffered(typ),
	}
}

// declEntry returns the dump's entry for d, a declaration of the
// program, declPkg holding each declaration's package.
func declEntry(d ast.Decl, declPkg map[ast.Decl]*ast.Package) map[string]any {
	switch x := d.(type) {
	case *ast.Func:
		tps := make([]any, 0)
		for _, tp := range x.TypeParams {
			var iface any
			if tp.Iface != nil {
				iface = declPkg[tp.Iface].Path + "." + tp.Iface.Name
			}
			tps = append(tps, map[string]any{
				"name": tp.Name, "type": int(tp.Type), "iface": iface, "data": tp.Data,
				"implicit": tp.Implicit,
			})
		}
		return map[string]any{
			"decl": "Func", "name": x.Name, "value": int(x.Value), "type_params": tps,
			"params": paramEntries(x.Params), "results": typeList(x.ResultTypes),
		}
	case *ast.Var:
		return map[string]any{"decl": "Var", "name": x.Name, "type": int(x.Type), "const": constEntry(x.Const)}
	case *ast.TypeDecl:
		methods := make([]any, 0)
		if it := x.Interface(); it != nil {
			for _, m := range it.Methods {
				methods = append(methods, map[string]any{
					"name": m.Name, "params": paramEntries(m.Params), "results": typeList(m.ResultTypes),
				})
			}
		}
		return map[string]any{"decl": "Type", "name": x.Name, "type": int(x.Type), "methods": methods}
	default:
		return map[string]any{"decl": "Bad"}
	}
}

// paramEntries returns the names and types of ps.
func paramEntries(ps []*ast.Param) []any {
	out := make([]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]any{"name": p.Name, "type": int(p.Type)})
	}
	return out
}

// typeList returns ts as the dump's list of IDs.
func typeList(ts []types.Type) []any {
	out := make([]any, 0, len(ts))
	for _, typ := range ts {
		out = append(out, int(typ))
	}
	return out
}

// constEntry returns the dump's form of a global's computed value, or
// of an operator's constant, as the port's typeck/dump.tck describes it:
// an untyped float's exact value in lowest terms (see floatRat).
func constEntry(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case constant.Int:
		return map[string]any{"int": x.String()}
	case float64:
		return map[string]any{"float64": strconv.FormatUint(math.Float64bits(x), 10)}
	case float32:
		return map[string]any{"float32": strconv.FormatUint(uint64(math.Float32bits(x)), 10)}
	case string:
		return map[string]any{"bytes": hex.EncodeToString([]byte(x))}
	case bool:
		return map[string]any{"bool": x}
	case *ast.CAllocatorLit:
		return map[string]any{"c_allocator": true}
	case []any:
		items := make([]any, 0, len(x))
		for _, item := range x {
			items = append(items, constEntry(item))
		}
		return map[string]any{"list": items}
	case ast.UnionConst:
		return map[string]any{"union": x.Case, "payload": constEntry(x.Payload)}
	case constant.Float:
		return map[string]any{"float": floatRat(x).RatString()}
	default:
		return map[string]any{"unknown": fmt.Sprintf("%T", v)}
	}
}

// deepTypeShapes are the programs TestTypesDeep checks, each n links
// long: the chains of declarations the checker's declaration phase walks,
// which have no nesting limit, each of which Go walks with a frame a link
// and the port must walk without one.
var deepTypeShapes = map[string]func(n int) string{
	// Each global's initializer names the next, declared after it, so
	// Go's check of the first nests a check for every one.
	"globals": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\n")
		for i := range n {
			fmt.Fprintf(&b, "var g%d = g%d + 1\n", i, i+1)
		}
		fmt.Fprintf(&b, "var g%d int64 = 0\n\nmut a [g0 - %d + 2]int64\n\nfunc main() {\n}\n", n, n)
		return b.String()
	},
	// Each global's type is an array whose length names the next.
	"global-lengths": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\n")
		for i := range n {
			fmt.Fprintf(&b, "mut a%d [len(a%d)]int64\n", i, i+1)
		}
		fmt.Fprintf(&b, "mut a%d [1]int64\n\nfunc main() {\n}\n", n)
		return b.String()
	},
	// One global adds up n/10 others, each the head of a chain of ten,
	// deeper than the port nests checks, so its check is suspended once
	// for each, and checked again from where it was each time.
	"fan": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nvar g0 = h0_0")
		r := n / 10
		for i := 1; i < r; i++ {
			fmt.Fprintf(&b, " + h%d_0", i)
		}
		b.WriteString("\n")
		for i := range r {
			for j := range 10 {
				fmt.Fprintf(&b, "var h%d_%d = h%d_%d + 1\n", i, j, i, j+1)
			}
			fmt.Fprintf(&b, "var h%d_10 int64 = 0\n", i)
		}
		b.WriteString("\nfunc main() {\n}\n")
		return b.String()
	},
	// Each global's type adds a type of its own, [i+1]int8, before its
	// length names the next global, and one more after, so the IDs show
	// whether a check nested in another adds its types in Go's order:
	// the port suspends and replays these checks, and a replay must not
	// move an ID. Each holds i+1 bytes, so the chain stops at a thousand,
	// far past how many checks the port nests, to keep the globals under
	// their limit.
	"global-order": func(n int) string {
		n = min(n, 1000)
		var b strings.Builder
		b.WriteString("package main\n\n")
		for i := range n {
			fmt.Fprintf(&b, "mut h%d [len(h%d)][%d]int8\n", i, i+1, i+1)
		}
		fmt.Fprintf(&b, "mut h%d [1][%d]int8\n\nfunc main() {\n}\n", n, n+1)
		return b.String()
	},
	// Each struct holds the next by value, so its size walks the whole
	// chain. Each holds a list, an arena, a pool and a union first, so
	// what it owns and holds, which the table finds afresh for every
	// type the dump writes, is found at once, and the dump stays linear.
	"structs": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\ntype U union {\n    a\n}\n\n")
		for i := range n {
			fmt.Fprintf(&b, "type S%d struct {\n    l list[int64]\n    a arena[int64]\n"+
				"    p pool[int64]\n    u U\n    s S%d\n}\n", i, i+1)
		}
		fmt.Fprintf(&b, "type S%d struct {\n    l [3]int64\n}\n\nfunc main() {\n}\n", n)
		return b.String()
	},
	// An array length, a global's value and a bool written as one long
	// chain of operators each.
	"constants": func(n int) string {
		return "package main\n\nmut a [" + rep("1 + ", n) + "1]int64\n\nvar b int64 = " +
			rep("2 * 3 - 5 + ", n) + "0\n\nvar c = " + rep("1 < 2 && ", n) + "true\n\nvar d = " +
			rep("b + ", n) + "1\n\nfunc main() {\n}\n"
	},
}

// deepTypeNests are programs TestTypesDeep checks nested n levels, as
// deeply as the parser lets a constant expression nest: an array type
// and its literal, negations and parenthesized sums, each in a global's
// value, which the port's checker recurses into a level at a time.
var deepTypeNests = map[string]func(n int) string{
	"array-literals": func(n int) string {
		typ := rep("[1]", n) + "int64"
		return "package main\n\nvar x = " + typ + rep("{", n) + "1" + rep("}", n) +
			"\n\nmut y " + typ + "\n\nfunc main() {\n}\n"
	},
	"negations": func(n int) string {
		return "package main\n\nvar x = " + rep("-(", n) + "1" + rep(")", n) +
			"\n\nvar y = " + rep("!(", n) + "true" + rep(")", n) + "\n\nfunc main() {\n}\n"
	},
	"sums": func(n int) string {
		return "package main\n\nvar x int64 = " + rep("1 + (", n) + "1" + rep(")", n) +
			"\n\nmut a [" + rep("1 + (", n) + "1" + rep(")", n) + "]int64\n\nfunc main() {\n}\n"
	},
}

// TestTypesDeep runs each of deepTypeShapes, chainLength links long,
// and each of deepTypeNests, half the nesting limit deep, since each
// level of an array literal or a sum is two brackets or parentheses in
// the parser's count, through both checkers, as TestTypes does, the port's stack held to
// deepStack KB: a port whose declaration phase recurses with a chain's
// length fails here. Each shape must be Go-clean. It runs only as
// TestPort does.
func TestTypesDeep(t *testing.T) {
	ready(t)
	shapes := make(map[string]string)
	for name, shape := range deepTypeShapes {
		shapes[name] = shape(chainLength)
	}
	for name, nest := range deepTypeNests {
		// The parser counts every bracket and parenthesis, so a few
		// levels are left for those around the nesting.
		shapes[name] = nest(deepLimit / 2)
	}
	for name, src := range shapes {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			if compared, _, _ := checkTypes(t, input{name: name, dir: dir, file: "main.tck"}, env.portRun, true); !compared {
				t.Fatal("Go reports an error for the shape, which must be clean")
			}
		})
	}
}

// floatRat returns the exact value an untyped float constant holds. The
// type keeps it in an unexported *big.Rat, nil for zero, and has no
// accessor for it, constant being frozen, so it is read through reflect;
// it is never written.
func floatRat(x constant.Float) *big.Rat {
	v := reflect.ValueOf(&x).Elem().Field(0)
	r := *(**big.Rat)(unsafe.Pointer(v.UnsafeAddr()))
	if r == nil {
		return new(big.Rat)
	}
	return r
}

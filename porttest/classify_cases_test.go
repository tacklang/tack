package porttest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestClassifyCounts classifies every case of the type checker's and
// logs how many each stage holds, why the tree raised them, and every
// message that matched no harvested template, which it fails on: a
// message the harvest cannot place is a case whose stage is guessed.
func TestClassifyCounts(t *testing.T) {
	cases, err := classifiedCases()
	if err != nil {
		t.Fatal(err)
	}
	var counts [len(checkStageInfo)]int
	whys := map[string]int{}
	several, pieced := 0, 0
	var unmatched []string
	for _, c := range cases {
		counts[c.stage]++
		if c.treeStage >= c.diagStage {
			whys[c.treeStage.String()+" "+c.why]++
		} else {
			whys[c.diagStage.String()+" diagnostics"]++
		}
		several += c.several
		pieced += c.pieced
		for _, m := range c.unmatched {
			unmatched = append(unmatched, c.archive+"/"+c.name+": "+m)
		}
	}
	var b strings.Builder
	for s := stageDecls; s <= stageBackend; s++ {
		fmt.Fprintf(&b, "%-4s %-14s %d\n", s, checkStageInfo[s].stage, counts[s])
	}
	fmt.Fprintf(&b, "total %d cases; of their messages %d matched by pieces, %d placed by "+
		"templates of several stages, %d matched by none\n", len(cases), pieced, several, len(unmatched))
	t.Log("cases per stage:\n" + b.String())
	b.Reset()
	for _, why := range slices.Sorted(maps.Keys(whys)) {
		fmt.Fprintf(&b, "%5d %s\n", whys[why], why)
	}
	t.Log("what set each stage:\n" + b.String())
	for _, u := range unmatched {
		t.Errorf("matches no template: %s", u)
	}
}

// TestClassifyHandChecked checks the stage of cases whose stage was
// worked out by reading them, one for each way a stage is reached.
func TestClassifyHandChecked(t *testing.T) {
	want := []struct {
		archive, name, why string
		stage              checkStage
	}{
		// A Data parameter outside a type parameter list, reported by
		// declaredType, and no statement in any body: S1.
		{"invalid", "data-param-type", "declaredType's error, empty bodies", stageDecls},
		// A struct cycle, found while the fields are resolved: S1.
		{"invalid", "struct-recursive", "the cycle check, empty bodies", stageDecls},
		// break outside a loop, a statement's error: S2a.
		{"invalid", "break-outside-loop", "a statement's error", stageCore},
		// any as an array's element, reported by any.go, with empty
		// bodies: S2b from the message alone.
		{"invalid", "any-array-elem", "any.go's error", stageCalls},
		// a + 1 on an any: an integer operator whose type is not known,
		// with no constant value, leans to the arithmetic: S4.
		{"invalid", "any-arithmetic", "operator on an invalid type", stageArith},
		// int32(a[3]) in the checked form, which also indexes: S4 over
		// the index's S3.
		{"invalid", "array-index-out-of-range", "checked conversion and index", stageArith},
		// An unreachable in a covered default: S5, over the switch's
		// S2a error.
		{"invalid", "unreachable-union-default-covered", "unreachable", stageLoops},
		// fallthrough in a region: S6, over the fallthrough's S2a.
		{"invalid", "region-fallthrough-inside", "a region", stageOwn},
		// A generic function: S7.
		{"invalid", "data-compare", "type parameters", stageGeneric},
		// A warning case whose message is S2a and whose program only
		// declares and calls: S2b.
		{"warnings", "unused-stmt-len", "len, a call", stageCalls},
		// The C name of an extern func, which checkFunc checks: S2a.
		{"invalid", "extern-c-name-bad", "checkExtern's error", stageCore},
		// A fields loop: S7.
		{"invalid", "fields-index-overflow", "a fields loop", stageGeneric},
		// A function's name passed as a value the checker rejects: S7.
		{"invalid", "site-func-value", "a rejected function value", stageGeneric},
		// A method expression of an interface's method, whose interface
		// S1 resolves: the method expression's call, S2b.
		{"invalid", "method-expr-interface", "an interface's method expression", stageCalls},
		// A case the backend rejects: check-backend.
		{"invalid", "extern-header-clash", "the backend's error", stageBackend},
		// A view result: S6.
		{"invalid", "view-result-mismatch", "a view result", stageOwn},
		// A call of drop, and of swap: S6.
		{"invalid", "drop-arity-none", "a call of drop", stageOwn},
		{"invalid", "swap-not-list", "a call of swap", stageOwn},
		// A resolve case, whose program loads and so is type checked:
		// what its tree implies.
		// any := 1 is a resolve error, but the program loads: int32 of
		// the int64 variable narrows, S4.
		{"invalid", "any-as-name", "a resolve case, a narrowing conversion", stageArith},
	}
	cases, err := classifiedCases()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		i := slices.IndexFunc(cases, func(c checkCase) bool {
			return c.archive == w.archive && c.name == w.name
		})
		if i < 0 {
			t.Errorf("%s/%s is not a case of the type checker's", w.archive, w.name)
			continue
		}
		if c := cases[i]; c.stage != w.stage {
			t.Errorf("%s/%s (%s) is %s (messages %s, tree %s: %s), want %s",
				w.archive, w.name, w.why, c.stage, c.diagStage, c.treeStage, c.why, w.stage)
		}
	}
}

// TestClassifyDeterministic checks that classifying the cases twice
// gives each the same stages.
func TestClassifyDeterministic(t *testing.T) {
	first, err := classifiedCases()
	if err != nil {
		t.Fatal(err)
	}
	again, err := checkCases(env.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(again) {
		t.Fatalf("%d cases, then %d", len(first), len(again))
	}
	for i := range first {
		a, b := first[i], again[i]
		if a.name != b.name || a.stage != b.stage || a.diagStage != b.diagStage ||
			a.treeStage != b.treeStage || a.why != b.why {
			t.Errorf("%s/%s: %s (%s, %s, %s), then %s/%s: %s (%s, %s, %s)",
				a.archive, a.name, a.stage, a.diagStage, a.treeStage, a.why,
				b.archive, b.name, b.stage, b.diagStage, b.treeStage, b.why)
		}
	}
}

// TestMatchLine checks how messages are matched to templates: the most
// specific whole match decides, ties of several stages give the
// highest, a message built of pieces is matched by the pieces that
// explain the most of it, and a message nothing matches is the highest
// stage.
func TestMatchLine(t *testing.T) {
	mk := func(format string, stage checkStage) template {
		tp, ok := newTemplate(formatPieces(format), stage, "test")
		if !ok {
			t.Fatalf("newTemplate(%q) refused", format)
		}
		return tp
	}
	templates := []template{
		mk("%s and %s", stageArith),
		mk("cannot use %s as %s", stageCore),
		mk("cannot use %s as %s in argument to %s", stageCalls),
		mk("cannot use %s as %s in argument to %s", stageGeneric),
		mk("divisor %s may be zero", stageCore),
		mk(", or use %s", stageCore),
		mk("this guard", stageFacts),
		mk("use q, ok := %s", stageArith),
		mk("q, ok := ... / %s", stageCore),
		mk("append", stageOwn),
		mk("100%% sure", stageLoops),
	}
	cases := []struct {
		msg, about string
		want       lineMatch
	}{
		{"cannot use x as y", "the one whole match", lineMatch{stage: stageCore}},
		{"x and y", "a vague whole match, the only one", lineMatch{stage: stageArith}},
		{"cannot use a and b as y", "the more specific of two", lineMatch{stage: stageCore}},
		{
			"cannot use x as y in argument to f", "one format in two stages",
			lineMatch{stage: stageGeneric, several: true},
		},
		{
			"divisor d may be zero, or use q, ok := ... / d",
			"pieces that fix the most, not the overlapping use q, ok :=",
			lineMatch{stage: stageCore, pieced: true},
		},
		{"this guard allows x == 0", "one piece", lineMatch{stage: stageFacts, pieced: true}},
		{
			"append takes l as mut", "a one-word piece is a name, not a piece of a message",
			lineMatch{stage: stageGeneric, none: true},
		},
		{"100% sure", "%% is a percent sign", lineMatch{stage: stageLoops}},
		{"nothing like any", "no match", lineMatch{stage: stageGeneric, none: true}},
	}
	for _, tc := range cases {
		if got := matchLine(templates, tc.msg); got != tc.want {
			t.Errorf("matchLine(%q) = %+v, want %+v (%s)", tc.msg, got, tc.want, tc.about)
		}
	}
	for _, vague := range []string{"%s", "%s: %s", "%d%s", "", " = "} {
		if _, ok := newTemplate(formatPieces(vague), stageCore, "test"); ok {
			t.Errorf("newTemplate(%q) is a template, want it refused as saying nothing", vague)
		}
	}
}

// TestTypeckFilesStaged checks that every source file of package typeck
// has a stage, and that every function the split files name is one of
// that file's, so a renamed function cannot fall back to its file's
// stage unseen.
func TestTypeckFilesStaged(t *testing.T) {
	if _, err := harvestDir(filepath.Join(env.root, "typeck"), typeckFuncStage); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for file, byStage := range typeckFuncNames {
		path := filepath.Join(env.root, "typeck", file)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		declared := map[string]bool{}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				declared[funcKey(fd)] = true
			}
		}
		for stage, names := range byStage {
			for _, n := range names {
				if !declared[n] {
					t.Errorf("%s, staged %s, declares no function %s", file, stage, n)
				}
			}
		}
	}
}

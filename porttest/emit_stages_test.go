package porttest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// emitStage is one of the stages the C backend is ported in, M7's: E0
// the handover, H the header, E1 the core, E2 values, E3 owned values and
// containers and E4 generics and functions. A case of an emit stage, or
// of check-backend or check-clean, needs the set of them its run of the
// Go compiler covers (see the package doc, Backend stages).
type emitStage int

const (
	// emitE0 is the handover: the driver's --emit and --emit-header, the
	// backend's entry and the type table's binding, codegen/backend.go,
	// types and the part of gen.go no --emit reaches. Every case needs it.
	emitE0 emitStage = iota
	// emitH is the header: header.go and reserved.go, and generate's
	// check of what tack.h cannot name, headerDiags, before it lowers
	// anything.
	emitH
	// emitE1 is the core: gen.go but for what other stages take, order.go,
	// unused.go, string.go, compare.go, unreachable.go, builtins.go and
	// names.go.
	emitE1
	// emitE2 is values: structs, arrays, unions, switches, slices, views,
	// any and swap, and gen.go's division and shift helpers, its several
	// values and its checked forms.
	emitE2
	// emitE3 is owned values and containers: lists, maps, arenas, pools,
	// defers, ?, drop, replace, the free helpers and regions.
	emitE3
	// emitE4 is generics and functions: generic instances, closures,
	// function values, fields loops and make of Data, and calls through
	// a function value.
	emitE4
	// emitStages is how many there are.
	emitStages
)

// emitStageNames are the stages' names, in required.txt, by emitStage.
var emitStageNames = [emitStages]string{
	emitE0: "emit-E0", emitH: "emit-H", emitE1: "emit-E1", emitE2: "emit-E2", emitE3: "emit-E3",
	emitE4: "emit-E4",
}

// String returns the stage's short name, E0 to E4 or H.
func (s emitStage) String() string { return strings.TrimPrefix(emitStageNames[s], "emit-") }

// emitSet is a set of emit stages, a bit a stage.
type emitSet uint8

// with returns s with st added.
func (s emitSet) with(st emitStage) emitSet { return s | 1<<st }

// has reports whether s holds st.
func (s emitSet) has(st emitStage) bool { return s&(1<<st) != 0 }

// String returns s as its stages' short names joined by +, in stage
// order, or "none".
func (s emitSet) String() string {
	var parts []string
	for st := range emitStages {
		if s.has(st) {
			parts = append(parts, st.String())
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "+")
}

// required reports whether every stage of s is named in req.
func (s emitSet) required(req map[string]bool) bool {
	for st := range emitStages {
		if s.has(st) && !req[emitStageNames[st]] {
			return false
		}
	}
	return true
}

// coverDirs are the directories, relative to the module's root, whose
// packages the instrumented Go compiler covers and whose files emitFileStages
// stages: the backend's and the type table's. The driver, ./tack, is
// instrumented too (see coverBinary) but staged by none: what it runs, the
// handover, every case needs.
var coverDirs = []string{"codegen", "codegen/c", "types"}

// emitFileStages gives each source file of the backend, by its path from
// the module's root, the stage that ports it, as the M7 plan splits them.
// A function of one of them that emitFuncStages names goes to the stage
// it gives instead; gen.go's are all named there. A non-test file of
// coverDirs that is not here fails TestEmitStagesComplete, and a covered
// block of one fails the classification, so a new file cannot go
// unstaged. Every file of types is E0's.
var emitFileStages = map[string]emitStage{
	// E0: the backend interface, Refuse and the docs.
	"codegen/backend.go": emitE0, "codegen/doc.go": emitE0, "codegen/c/doc.go": emitE0,
	// H: tack.h and the names it may not take. names.go, which the plan
	// lists with them, holds only the generator's names of the types and
	// helpers the lowering writes, which header.go never calls, so it is
	// E1's.
	"codegen/c/header.go": emitH, "codegen/c/reserved.go": emitH,
	// E1, with gen.go, whose functions emitFuncStages stages one by one.
	"codegen/c/gen.go": emitE1, "codegen/c/order.go": emitE1, "codegen/c/unused.go": emitE1,
	"codegen/c/string.go": emitE1, "codegen/c/compare.go": emitE1,
	"codegen/c/unreachable.go": emitE1, "codegen/c/builtins.go": emitE1,
	"codegen/c/names.go": emitE1,
	// E2
	"codegen/c/struct.go": emitE2, "codegen/c/array.go": emitE2, "codegen/c/union.go": emitE2,
	"codegen/c/switch.go": emitE2, "codegen/c/slice.go": emitE2, "codegen/c/view.go": emitE2,
	"codegen/c/any.go": emitE2, "codegen/c/swap.go": emitE2,
	// E3
	"codegen/c/list.go": emitE3, "codegen/c/maps.go": emitE3, "codegen/c/arena.go": emitE3,
	"codegen/c/pool.go": emitE3, "codegen/c/defer.go": emitE3, "codegen/c/question.go": emitE3,
	"codegen/c/drop.go": emitE3, "codegen/c/replace.go": emitE3,
	// E4
	"codegen/c/generic.go": emitE4, "codegen/c/closure.go": emitE4,
	"codegen/c/funcvalue.go": emitE4, "codegen/c/fields.go": emitE4,
	"codegen/c/makedata.go": emitE4,
}

// genGo is the file whose every function emitFuncStages must stage.
const genGo = "codegen/c/gen.go"

// emitFuncStages are the functions that go to a stage other than their
// file's, or, for gen.go, every function, by file and stage: a method as
// its receiver's type and its name, generator.generate, and any other
// function by its name, as funcKey names them. The plan's line ranges of
// gen.go guided the split; the names, not the lines, decide it.
var emitFuncStages = map[string]map[emitStage][]string{
	genGo: {
		// The entry and the toolchain's commands: the backend's methods,
		// which a build calls and --emit never reaches but Generate, and
		// Generate itself, Refuse and the generator's maps, before any
		// lowering; generate's check of the header is H's (see
		// emitSplits).
		emitE0: {
			"init", "New", "backend.Name", "backend.Ext", "backend.BuildCmd", "clangFlags",
			"backend.CompileCmd", "backend.CompileStdCmd", "backend.VersionCmd", "compileCmd",
			"backend.Generate",
		},
		// The core: the walk, the preamble, the string pool, the globals,
		// constants, result structs, prototypes, the entry point,
		// signatures, bodies, blocks and every statement, the loops, ifs,
		// assignments, a plain local's declaration, calls, expressions,
		// literals, operators, conversions that cannot fail, C types,
		// names and mangling. generateVar, where the plan's range of E2
		// starts, is a plain local's declaration, x := e, which every body
		// with a local reaches, so it is E1's; the rest of that range is
		// E2's. The dispatch of builtin calls, genericValue and mapValue,
		// is E1's: it runs for len as much as for put; what it dispatches
		// to is the later stages'.
		emitE1: {
			"generator.walk",
			"generator.generate",
			"generator.emitPreamble",
			"generator.usesFloats",
			"generator.usesType",
			"hasType",
			"generator.poolCheckedZero",
			"generator.poolVar",
			"generator.poolConst",
			"holdsSlice",
			"generator.collectStrings",
			"walkSwitchValues",
			"generator.emitConstantPool",
			"generator.narrowsFloats",
			"narrowing",
			"generator.emitFloatNarrowing",
			"generator.generateDecls",
			"hasBody",
			"generator.emitGlobals",
			"generator.generateGlobal",
			"intLiteral",
			"typedConstLiteral",
			"floatConst",
			"floatLiteral",
			"constLiteral",
			"generator.constValue",
			"generator.emitResultStructs",
			"generator.emitResultStruct",
			"generator.resultsName",
			"resultsOf",
			"generator.emitPrototypes",
			"generator.hasExterns",
			"generator.emitCNameMacro",
			"generator.emitFunc",
			"generator.emitEntryPoint",
			"generator.isLibrary",
			"generator.startName",
			"generator.signature",
			"generator.paramType",
			"generator.resultType",
			"generator.generateFuncBody",
			"unusedParams",
			"namesIdent",
			"isIdentByte",
			"generator.generateBlock",
			"generator.generateLoopBody",
			"generator.block",
			"generator.generateStmt",
			"generator.generateSimpleStmt",
			"generator.generateReturn",
			"generator.returnValue",
			"parenthesised",
			"condHeader",
			"generator.generateFor",
			"loopFor",
			"breakUnless",
			"generator.generateClauseFor",
			"generator.clauseFor",
			"generator.generateForRange",
			"generator.collectionRange",
			"generator.bindsInPlace",
			"bindsElem",
			"generator.rangesInPlace",
			"writesTo",
			"stmtCallsFunc",
			"rangeBlock",
			"generator.temp",
			"stripParens",
			"generator.postExpr",
			"generator.generateIf",
			"generator.ifBranches",
			"generator.flatChain",
			"elseIf",
			"generator.generateAssign",
			"generator.holdTargetIndexes",
			"generator.targetRef",
			"generator.elementRef",
			"generator.generateIncDec",
			"generator.generateExprStmt",
			"onlyValue",
			"generator.droppedValue",
			"generator.generateVar",
			"generator.zeroValue",
			"indent",
			"generator.generateCall",
			"generator.callExpr",
			"generator.expr",
			"generator.exprNode",
			"generator.literal",
			"generator.plainLiteral",
			"generator.identExpr",
			"generator.binaryExpr",
			"generator.binaryOf",
			"arith",
			"generator.unaryExpr",
			"generator.callValue",
			"generator.conversionValue",
			"generator.convert",
			"generator.genericValue",
			"generator.mapValue",
			"generator.stringValue",
			"generator.strParts",
			"generator.cType",
			"arrayNameOf",
			"basicCType",
			"paramName",
			"paramDeclName",
			"generator.paramRef",
			"generator.blankParamName",
			"generator.argument",
			"generator.mutArg",
			"generator.varRef",
			"varName",
			"declName",
			"generator.funcCName",
			"methodCName",
			"lowered",
			"generator.mangle",
			"mangledParts",
			"mangledPart",
			"generator.seenStr",
			"generator.addStr",
			"generator.strLabel",
			"generator.toBytes",
		},
		// The division and shift helpers, the plan's 618-735.
		emitE2: {
			"generator.divisions", "divisionIn", "generator.emitDivision", "generator.emitShift",
			"divisionName",
			// Several values, their declarations and assignments, and the
			// checked forms of division, arithmetic and conversion, with
			// what proves a value fits, the plan's 2279-2860 but for
			// generateVar.
			"generator.generateMultiVar", "generator.holdTemps", "generator.generateMultiAssign",
			"generator.multiAssignPost", "generator.fieldAssigns", "multiTemp.initOf",
			"multiTemp.decl", "generator.multiValues", "generator.callValues",
			"generator.divisionValues", "divisionOk", "generator.arithValues", "constValueOf",
			"generator.constantValues", "generator.conversionValues", "fitsExpr", "floatFitsExpr",
			"targetFitsExpr", "withinExpr", "bounds",
		},
		// A region's block, whose lists the region frees, the plan's
		// generateRegion.
		emitE3: {"generator.generateRegion"},
		// A call through a function value, which only a function value
		// reaches, as funcvalue.go's do.
		emitE4: {"generator.dynamicCall"},
	},
	// Functions of later stages' files that every core program runs
	// for real, not only as a predicate that finds nothing, so the core
	// owns them, by file (ruled so, and overrulable):
	//
	// array.go's index and len, which string s[i] and len(s) lower
	// through, and the dispatch to them of slice.go's elementExpr, and
	// view.go's hasLen, which both ask.
	"codegen/c/array.go": {emitE1: {"generator.indexExpr", "generator.lenValue"}},
	"codegen/c/slice.go": {emitE1: {"generator.elementExpr"}},
	// defer.go's frame and the routes of break, continue and return,
	// which every function and every jump goes through, defers or not.
	"codegen/c/defer.go": {
		emitE1: {"generator.funcFrame", "generator.generateJump", "generator.stops"},
	},
	// view.go's rendering of a call's arguments and their C types, which
	// every call goes through, and the predicates it asks of each
	// parameter on the way.
	"codegen/c/view.go": {emitE1: {
		"generator.callArgs", "generator.argTypes", "paramsOf", "generator.viewParamAt", "packs",
		"generator.hasLen",
	}},
	// struct.go's walk of the program's types, which defines every type
	// before it is used, whatever its kind, each kind's definition being
	// its own stage's; the walk of the types == compares; and a
	// global's computed value, a scalar's as much as a struct's.
	"codegen/c/struct.go": {emitE1: {
		"generator.emitTypes",
		"generator.emittedTypes",
		"generator.isComposite",
		"generator.hasEqualityHelper",
		"generator.parts",
		"generator.emitTypeDef",
		"generator.emitDef",
		"generator.comparedTypes",
		"generator.constOf",
		"generator.specialConst",
	}},
	// The filters of that walk in later stages' files, each a loop over
	// every type the program has, asking its kind: the arenas, maps and
	// pools whose helpers are written, whether a function type is there,
	// and union.go's walk to the free helpers, each kind's helper its
	// own stage's. A program of any type runs them.
	"codegen/c/arena.go":     {emitE1: {"generator.arenaTypes"}},
	"codegen/c/maps.go":      {emitE1: {"generator.mapTypes"}},
	"codegen/c/pool.go":      {emitE1: {"generator.poolTypes"}},
	"codegen/c/funcvalue.go": {emitE1: {"generator.usesFuncTypes"}},
	// switch.go's walk of a body for the variables its cases read, which
	// the frame of every function is made with, switches or not.
	"codegen/c/switch.go": {emitE1: {"caseVarsRead"}},
	// any.go's macro naming the type id of a view, which reserved.go
	// reserves among tack.h's names for every program, before anything
	// is lowered, as well as any.go's lowering calls it.
	"codegen/c/any.go": {emitH: {"viewAnyID"}},
	// union.go's free helpers, a union's own and the name a free helper
	// is looked up by, E3's; and emitFrees, the walk over every type to
	// the free helpers, the list's, the map's, the arena's, the pool's
	// and the union's, which every program with a type runs, E1's, as
	// the type walk's filters above are. struct.go has no free helper: a
	// struct's owned fields are freed by list.go's frees, E3's already.
	"codegen/c/union.go": {
		emitE1: {"generator.emitFrees"},
		emitE3: {"freeNameOf", "generator.emitUnionFree"},
	},
}

// emitSplit is a function of which a leading part goes to another stage
// than the rest: the blocks that start before the end of the first
// statement of its body that calls the function callee.
type emitSplit struct {
	callee string
	stage  emitStage
}

// emitSplits are the functions split by their statements, by file and
// funcKey. generate first rejects a program whose types tack.h could not
// name, headerDiags, and returns before it lowers anything: that check,
// and the return when it fails, is H's, so a program the header rejects
// needs no lowering stage.
var emitSplits = map[string]map[string]emitSplit{
	genGo: {"generator.generate": {callee: "headerDiags", stage: emitH}},
}

// funcSpan is where a function's declaration is, and what stage its
// blocks are.
type funcSpan struct {
	key        string
	start, end token.Position
	// splitEnd, when the function is split (see emitSplits), is the end
	// of the statement before which, and in which, its blocks are split's.
	splitEnd token.Position
	stage    emitStage
	split    emitStage
}

// stagedFile is a source file of coverDirs with each of its functions'
// spans, in order.
type stagedFile struct {
	funcs []funcSpan
	stage emitStage
}

// stagedSources returns every non-test file of coverDirs below root, by
// its path from root, with its functions' spans and stages, or an error
// for a file with no stage, a gen.go function emitFuncStages does not
// stage or stages twice, or a name emitFuncStages or emitSplits gives
// that its file does not declare.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func stagedSources(
	root string,
) (map[string]*stagedFile, error) {
	out := map[string]*stagedFile{}
	fset := token.NewFileSet()
	var errs []string
	for _, dir := range coverDirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel := dir + "/" + name
			stage, ok := emitFileStages[rel]
			if dir == "types" {
				stage, ok = emitE0, true
			}
			if !ok {
				errs = append(errs, rel+" has no emit stage")
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil,
				parser.SkipObjectResolution)
			if err != nil {
				return nil, fmt.Errorf("staging the backend's sources: %w", err)
			}
			sf, fileErrs := stageFile(fset, rel, f, stage, emitFuncStages[rel], emitSplits[rel])
			errs = append(errs, fileErrs...)
			out[rel] = sf
		}
	}
	for file := range emitFuncStages {
		if out[file] == nil {
			errs = append(errs, "emitFuncStages names "+file+", which is no source file")
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, fmt.Errorf(
			"the backend's sources are not all staged:\n%s",
			strings.Join(errs, "\n"),
		)
	}
	return out, nil
}

// stageFile returns the spans of f's functions, f the file rel of stage
// stage, each with its stage, funcs giving the functions of another
// stage, emitFuncStages's for rel, and splits those split, emitSplits's,
// and what is wrong with either for it.
//
//nolint:cyclop,funlen // a run of checks, each noting what is wrong; split, it reads no plainer
func stageFile(
	fset *token.FileSet,
	rel string,
	f *ast.File,
	stage emitStage,
	funcs map[emitStage][]string,
	splits map[string]emitSplit,
) (*stagedFile, []string) {
	var errs []string
	byName := map[string]emitStage{}
	for st, names := range funcs {
		for _, n := range names {
			if prev, dup := byName[n]; dup {
				errs = append(errs, fmt.Sprintf("%s: %s is staged %s and %s", rel, n, prev, st))
			}
			byName[n] = st
		}
	}
	sf := &stagedFile{stage: stage}
	declared := map[string]bool{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		key := funcKey(fd)
		if fd.Recv != nil {
			key = recvKey(fd)
		}
		declared[key] = true
		span := funcSpan{
			start: fset.Position(fd.Pos()),
			end:   fset.Position(fd.End()),
			key:   key,
			stage: stage,
		}
		if st, ok := byName[key]; ok {
			span.stage = st
		} else if rel == genGo {
			errs = append(errs, fmt.Sprintf("%s: %s has no stage in emitFuncStages", rel, key))
		}
		if sp, ok := splits[key]; ok {
			end, found := splitEnd(fset, fd, sp.callee)
			if !found {
				errs = append(
					errs,
					fmt.Sprintf("%s: %s's body has no statement calling %s", rel, key, sp.callee),
				)
			}
			span.splitEnd, span.split = end, sp.stage
		}
		sf.funcs = append(sf.funcs, span)
	}
	for n := range byName {
		if !declared[n] {
			errs = append(
				errs,
				fmt.Sprintf("%s declares no function %s, which emitFuncStages stages", rel, n),
			)
		}
	}
	for n := range splits {
		if !declared[n] {
			errs = append(
				errs,
				fmt.Sprintf("%s declares no function %s, which emitSplits splits", rel, n),
			)
		}
	}
	return sf, errs
}

// recvKey is funcKey for a method, its receiver's type and its name,
// with a generic receiver's type parameters left off, which funcKey
// leaves to the name alone.
func recvKey(fd *ast.FuncDecl) string {
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch x := t.(type) {
	case *ast.IndexExpr:
		t = x.X
	case *ast.IndexListExpr:
		t = x.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// splitEnd returns the end of the first statement of fd's body that
// calls callee, and whether there is one.
func splitEnd(fset *token.FileSet, fd *ast.FuncDecl, callee string) (token.Position, bool) {
	if fd.Body == nil {
		return token.Position{}, false
	}
	for _, s := range fd.Body.List {
		calls := false
		ast.Inspect(s, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == callee {
					calls = true
				}
			}
			return !calls
		})
		if calls {
			return fset.Position(s.End()), true
		}
	}
	return token.Position{}, false
}

// before reports whether the position line:col comes before p.
func before(line, col int, p token.Position) bool {
	return line < p.Line || line == p.Line && col < p.Column
}

// stageAt returns the stage of the block that starts at line:col of
// sf: the stage of the function it is in, or of its split part, or the
// file's for a block outside every function.
func (sf *stagedFile) stageAt(line, col int) emitStage {
	i := sort.Search(len(sf.funcs), func(i int) bool { return before(line, col, sf.funcs[i].end) })
	if i == len(sf.funcs) || before(line, col, sf.funcs[i].start) {
		return sf.stage
	}
	fn := sf.funcs[i]
	if fn.splitEnd.Line > 0 && before(line, col, fn.splitEnd) {
		return fn.split
	}
	return fn.stage
}

// funcAt returns the funcKey of the function the block at line:col of
// sf is in, or "" outside every function.
func (sf *stagedFile) funcAt(line, col int) string {
	i := sort.Search(len(sf.funcs), func(i int) bool { return before(line, col, sf.funcs[i].end) })
	if i == len(sf.funcs) || before(line, col, sf.funcs[i].start) {
		return ""
	}
	return sf.funcs[i].key
}

// TestEmitStagesComplete checks that every non-test source file of the
// backend and of types has a stage, every function of gen.go exactly one
// in emitFuncStages, and that every function emitFuncStages and
// emitSplits name is declared in its file, so a function renamed, added
// or removed in the frozen sources cannot fall to a stage unseen. It
// needs no compiler and runs in every go test ./porttest.
func TestEmitStagesComplete(t *testing.T) {
	staged, err := stagedSources(env.root)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[emitStage]int{}
	for _, fn := range staged[genGo].funcs {
		counts[fn.stage]++
	}
	var b strings.Builder
	for st := range emitStages {
		fmt.Fprintf(&b, " %s %d", st, counts[st])
	}
	t.Logf("gen.go's %d functions by stage:%s", len(staged[genGo].funcs), b.String())
	if !slices.ContainsFunc(
		staged[genGo].funcs,
		func(f funcSpan) bool { return f.splitEnd.Line > 0 },
	) {
		t.Error("generate is not split at its check of the header")
	}
}

// TestEmitStagesRejectUnstaged checks that stageFile reports a gen.go
// function the table does not stage, one it stages twice, one it names
// that the file does not declare, and a split with no statement calling
// its callee or of a function the file does not declare.
func TestEmitStagesRejectUnstaged(t *testing.T) {
	src := "package c\n\nfunc staged() {}\n\nfunc unstaged() {}\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[emitStage][]string{emitE1: {"staged", "missing"}, emitE2: {"staged"}}
	splits := map[string]emitSplit{"unstaged": {callee: "nothing", stage: emitH}, "gone": {}}
	_, errs := stageFile(fset, genGo, f, emitE1, funcs, splits)
	want := []string{
		"unstaged has no stage", "staged is staged", "no function missing",
		"no statement calling nothing", "no function gone",
	}
	for _, w := range want {
		if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, w) }) {
			t.Errorf("stageFile reported %q, want a report containing %q", errs, w)
		}
	}
}

// syntheticSrc is the source TestStagesOfSynthetic stages: a package
// variable's initializer, on line 3, outside every function, and the
// bodies of a, b, c and h, on lines 6, 10, 14 and 18.
const syntheticSrc = `package c

var x = 1

func a() {
	_ = 1
}

func b() {
	_ = 2
}

func c() {
	_ = 3
}

func h() {
	_ = 4
}
`

// The blocks TestStagesOfSynthetic covers, by what they are: the
// initializer, and a body each of a, of E3 as its file is; b, which the
// table gives E1; c, which it gives E4; and h, which it gives H.
const (
	synInit = "3.9"
	synA    = "6.2"
	synB    = "10.2"
	synC    = "14.2"
	synH    = "18.2"
)

// TestStagesOfSynthetic checks stagesOf and blockStages on a staged file
// and coverage made by hand, so that each rule is seen to decide: a
// block's file stage, a per-function override, an initializer every
// process runs ignored, a later stage's block E1's baselines run made
// E1's, an H block E1's baselines run made baselineH's, and a block only
// later checkpoints' baselines run, which needs nothing more when the
// case needs one of its stages anyway, and the lowest of them when it
// does not. It needs no compiler and runs in every go test ./porttest.
//
//nolint:cyclop,funlen // a table test and its few direct checks, read straight down
func TestStagesOfSynthetic(t *testing.T) {
	const file = "codegen/c/syn.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "syn.go", syntheticSrc, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	sf, errs := stageFile(fset, file, f, emitE3,
		map[emitStage][]string{emitE1: {"b"}, emitE4: {"c"}, emitH: {"h"}}, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	saved := struct {
		staged   map[string]*stagedFile
		baseline [emitStages]map[string]map[string]bool
		idle     map[string]map[string]bool
	}{cover.staged, cover.baseline, cover.idle}
	defer func() { cover.staged, cover.baseline, cover.idle = saved.staged, saved.baseline, saved.idle }()
	set := func(stages ...emitStage) emitSet {
		s := emitSet(0)
		for _, st := range stages {
			s = s.with(st)
		}
		return s
	}
	blocks := func(starts ...string) map[string]map[string]bool {
		m := map[string]bool{}
		for _, s := range starts {
			m[s] = true
		}
		return map[string]map[string]bool{file: m}
	}
	cases := []struct {
		name      string
		baseline  [emitStages]map[string]map[string]bool
		idle      map[string]map[string]bool
		cov       []string
		baselineH emitStage
		want      emitSet
	}{
		{name: "file stage", cov: []string{synA}, want: set(emitE0, emitE3)},
		{name: "override to E1", cov: []string{synB}, want: set(emitE0, emitE1)},
		{name: "override to E4", cov: []string{synC}, want: set(emitE0, emitE4)},
		{name: "initializer outside a function", cov: []string{synInit}, want: set(emitE0, emitE3)},
		{
			name: "initializer every process runs",
			idle: blocks(synInit),
			cov:  []string{synInit, synB},
			want: set(emitE0, emitE1),
		},
		{
			name:     "E1's baselines run a later stage's block",
			baseline: [emitStages]map[string]map[string]bool{emitE1: blocks(synC)},
			cov:      []string{synC}, want: set(emitE0, emitE1),
		},
		{
			name: "E1's baselines run H's block", baselineH: emitE1,
			baseline: [emitStages]map[string]map[string]bool{emitE1: blocks(synH)},
			cov:      []string{synH}, want: set(emitE0, emitE1),
		},
		{
			name: "H's block kept H's", baselineH: emitH,
			baseline: [emitStages]map[string]map[string]bool{emitE1: blocks(synH)},
			cov:      []string{synH}, want: set(emitE0, emitH),
		},
		{
			name: "either, already needed",
			baseline: [emitStages]map[string]map[string]bool{
				emitE2: blocks(synC),
				emitE3: blocks(synC),
			},
			cov:  []string{synC, synA},
			want: set(emitE0, emitE3),
		},
		{
			name: "either, the lowest",
			baseline: [emitStages]map[string]map[string]bool{
				emitE2: blocks(synC),
				emitE3: blocks(synC),
			},
			cov:  []string{synC},
			want: set(emitE0, emitE2),
		},
		{
			name:     "either, own stage already needed",
			baseline: [emitStages]map[string]map[string]bool{emitE3: blocks(synC)},
			cov:      []string{synC, synB}, want: set(emitE0, emitE1, emitE3),
		},
	}
	for _, tc := range cases {
		cover.staged = map[string]*stagedFile{file: sf}
		cover.baseline, cover.idle = tc.baseline, tc.idle
		got, err := stagesOf(covered{file: tc.cov}, tc.baselineH)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: stagesOf = %s, want %s", tc.name, got, tc.want)
		}
	}
	cover.baseline = [emitStages]map[string]map[string]bool{
		emitE2: blocks(synC),
		emitE3: blocks(synC),
	}
	if got := blockStages(file, synC, emitE4, emitH); got != set(emitE2, emitE3, emitE4) {
		t.Errorf("blockStages of an E4 block E2's and E3's baselines run = %s, want E2+E3+E4", got)
	}
	if got := blockStages(file, synA, emitE3, emitH); got != set(emitE3) {
		t.Errorf("blockStages of an E3 block no baseline runs = %s, want E3", got)
	}
	cover.staged = map[string]*stagedFile{}
	if _, err := stagesOf(covered{file: {synA}}, emitH); err == nil {
		t.Error("stagesOf of a file with no stage is no error")
	}
}

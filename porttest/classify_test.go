package porttest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing/fstest"
	"time"
	"unicode"

	tackast "github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
	"github.com/tacklang/tack/types"
)

// checkStage is one of the stages the type checker is ported in, in the
// order they are ported, so that a later stage compares greater: a case
// of a stage needs every stage up to it.
type checkStage int

const (
	// stageFront is outside the type checker: the diagnostics of the
	// loader and the resolver, which the port has already, and of the
	// backend, which --check runs on a program the checker accepts. It
	// is never a case's stage, only a line's.
	stageFront checkStage = iota
	// stageDecls, S1, is the declarations: types, globals, signatures.
	stageDecls
	// stageCore, S2a, is statements and expressions.
	stageCore
	// stageCalls, S2b, is calls and containers.
	stageCalls
	// stageFacts, S3, is facts: index and divisor proofs.
	stageFacts
	// stageArith, S4, is arithmetic and relations: overflow proofs.
	stageArith
	// stageLoops, S5, is loops and contracts.
	stageLoops
	// stageOwn, S6, is ownership.
	stageOwn
	// stageGeneric, S7, is generics and closures.
	stageGeneric
	// stageBackend is the cases the backend rejects, and those with no
	// error before the backend, warnings aside, on which --check runs
	// the backend: either way --check's output depends on the backend's
	// judgment, which the port gives once its backend is ported, at M7,
	// whatever its type checker passes.
	stageBackend
)

// checkStageInfo names each checkStage: by its short name, S1 to S7,
// and as TestPort's stage of the --check cases it holds.
var checkStageInfo = [...]struct{ short, stage string }{
	stageFront:   {"front", ""},
	stageDecls:   {"S1", "check-decls"},
	stageCore:    {"S2a", "check-core"},
	stageCalls:   {"S2b", "check-calls"},
	stageFacts:   {"S3", "check-facts"},
	stageArith:   {"S4", "check-arith"},
	stageLoops:   {"S5", "check-loops"},
	stageOwn:     {"S6", "check-own"},
	stageGeneric: {"S7", "check-generic"},
	stageBackend: {"backend", "check-backend"},
}

// String returns the stage's short name, S1 to S7, front or backend.
func (s checkStage) String() string { return checkStageInfo[s].short }

// typeckFileStages gives each source file of package typeck the stage
// that ports it. A file split between stages has its functions sorted by
// typeckFuncStage; the stage here is that of the rest of it. A file of
// typeck that is not here fails the harvest, so a new file cannot go
// unclassified.
var typeckFileStages = map[string]checkStage{
	// S1
	"doc.go": stageDecls, "size.go": stageDecls, "visibility.go": stageDecls,
	"invalid.go": stageDecls, "tests.go": stageDecls, "site.go": stageDecls,
	"paramnoun.go": stageDecls, "unusedecl.go": stageDecls,
	// S2a, and the files S1 takes a part of
	"check.go": stageCore, "extern.go": stageCore, "expr.go": stageCore, "struct.go": stageCore,
	"union.go": stageCore,
	"array.go": stageCore, "string.go": stageCore, "slice.go": stageCore, "switch.go": stageCore,
	"method.go": stageCore, "methodexpr.go": stageCore, "defer.go": stageCore,
	"firstpass.go": stageCore, "leavememo.go": stageCore, "unread.go": stageCore,
	// S2b
	"mut.go": stageCalls, "view.go": stageCalls, "own.go": stageCalls, "variadic.go": stageCalls,
	"any.go": stageCalls, "assert.go": stageCalls, "printf.go": stageCalls, "list.go": stageCalls,
	"maps.go": stageCalls, "arena.go": stageCalls, "pool.go": stageCalls, "errors.go": stageCalls,
	"question.go": stageCalls,
	// S3, factoracle and work, the provers' test hooks and step counts,
	// with the facts
	"facts.go": stageFacts, "subject.go": stageFacts, "factindex.go": stageFacts,
	"region.go": stageFacts, "reach.go": stageFacts, "explain.go": stageFacts,
	"factoracle_off.go": stageFacts, "factoracle_on.go": stageFacts, "work.go": stageFacts,
	// S4
	"arith.go": stageArith, "overflow.go": stageArith, "convfacts.go": stageArith,
	"relations.go": stageArith, "relgraph.go": stageArith, "relindex.go": stageArith,
	"relnotes.go": stageArith,
	// S5
	"invariant.go": stageLoops, "ensures.go": stageLoops, "unreachable.go": stageLoops,
	"counting.go": stageLoops, "appended.go": stageLoops,
	// S6
	"move.go": stageOwn, "escape.go": stageOwn, "exclusive.go": stageOwn,
	"viewresult.go": stageOwn, "drop.go": stageOwn, "replace.go": stageOwn,
	"swap.go": stageOwn, "scopealloc.go": stageOwn,
	// S7, and closure.go but for its fact store
	"generic.go": stageGeneric, "infer.go": stageGeneric, "instance.go": stageGeneric,
	"paramswitch.go": stageGeneric, "data.go": stageGeneric, "closure.go": stageGeneric,
	"funcvalue.go": stageGeneric,
}

// typeckFuncNames are the functions of the files split between stages
// that go to a stage other than their file's, by file: a method as its
// receiver's type and its name, Checker.fold, and any other function by
// its name, so that a new method of the same name as one here, on
// another type, keeps its file's stage.
var typeckFuncNames = map[string]map[checkStage][]string{
	// check.go's declaration phase: New, Check, the globals, fold,
	// declaredType and the resolve*Types; the rest is S2a.
	"check.go": {stageDecls: {
		"New", "declPackages", "Checker.typeName", "Checker.enter", "Checker.Check",
		"Checker.checkGlobals", "Checker.checkGlobal", "Checker.badGlobalType",
		"Checker.namesFailedGlobal", "Checker.namesUnbound", "Checker.notConstant",
		"Checker.isConstant", "Checker.isConstantCall", "subExprs", "sliceParts",
		"Checker.allConstant", "Checker.constantGlobal", "zeroConst", "Checker.fold",
		"Checker.foldNumber", "foldFloat", "foldFloatConst", "Checker.foldLiteral",
		"Checker.foldPart", "Checker.foldElement", "Checker.foldLeaf", "Checker.foldInt",
		"Checker.foldNot", "Checker.foldElems", "Checker.foldBoolBinary", "foldBools",
		"Checker.compareConsts", "Checker.equalConsts", "Checker.equalUnions",
		"Checker.declaredType", "Checker.compositeType", "Checker.resolveResultTypes",
		"Checker.resolveParamTypes", "Checker.errorf", "Checker.unusedf", "Checker.errorNotes",
	}},
	// expr.go's constant subset; the rest is S2a.
	"expr.go": {stageDecls: {
		"constValue", "Checker.foldFloat", "Checker.foldArithmetic", "foldComparison",
		"Checker.foldShift", "isConstantExpr", "Checker.constConversion",
	}},
	// struct.go's declarations, the fields and the cycles; struct
	// literals and selectors are S2a.
	"struct.go": {stageDecls: {
		"Checker.declareStructs", "Checker.checkStructs", "Checker.resolveFields",
		"Checker.reportCycles", "Checker.visitStruct", "Checker.partsOf", "Checker.structsIn",
		"Checker.reportCycle", "Checker.ensureGlobal", "Checker.initCycle",
		"Checker.foldStruct",
	}},
	// union.go's cases, as declared; constructions and switches are S2a.
	"union.go": {stageDecls: {
		"Checker.resolveCases", "Checker.typeDecl", "Checker.externUnion",
		"Checker.crossesUnion", "Checker.unionExposure", "Checker.isConstantConstruction",
		"Checker.foldConstruction",
	}},
	// list.go's and maps.go's pending checks of element and key types;
	// the rest is S2b.
	"list.go": {stageDecls: {
		"Checker.listType", "Checker.requireCopyable", "Checker.checkPendingElems",
		"Checker.notCopyable", "Checker.ownedPhrase",
	}},
	"maps.go": {stageDecls: {
		"Checker.mapType", "Checker.requireKey", "Checker.checkPendingKeys", "Checker.badKey",
	}},
	// extern.go's check of every extern func's C name against the
	// others; checkFunc checks each extern func's own side, S2a.
	"extern.go": {stageDecls: {
		"Checker.checkCNames", "Checker.declaredTwice", "Checker.externMain",
		"Checker.funcName",
	}},
	// The declaration phase's helpers in files of later stages, which the
	// port of S1 carries: a parameter's type, a result's, a generic
	// function's type parameters, and the interfaces, which Check
	// resolves before any signature.
	"view.go":       {stageDecls: {"Checker.paramType", "Checker.plainParamType"}},
	"viewresult.go": {stageDecls: {"Checker.resultType"}},
	"generic.go": {stageDecls: {
		"Checker.resolveInterfaces", "implicitTypeParams", "Checker.resolveTypeParams",
	}},
	// closure.go's fact store, and its writers, which the checks of
	// exclusive.go and viewresult.go find their mut arguments with; the
	// rest is S7.
	"closure.go": {
		stageFacts: {
			"Checker.pushKept", "Checker.popKept", "Checker.inForce", "Checker.viewFacts",
			"Checker.factsInForce", "Checker.factInForce", "Checker.guardInForce",
			"Checker.guardInView", "Checker.keepsInClosure",
		},
		stageOwn: {
			"writersIn", "writersOutside", "writer.what", "writer.name", "writer.only",
			"writer.note",
		},
	},
}

// funcKey returns how typeckFuncNames names fd: Checker.fold for a
// method, its receiver's type and its name, and its name alone for any
// other function.
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// typeckFuncStage returns the stage of function fn of the typeck source
// file file, "" for what is outside any function, and whether the file
// is classified at all.
func typeckFuncStage(file, fn string) (checkStage, bool) {
	stage, ok := typeckFileStages[file]
	if !ok {
		return 0, false
	}
	for s := stageDecls; s <= stageGeneric; s++ {
		if slices.Contains(typeckFuncNames[file][s], fn) {
			return s, true
		}
	}
	return stage, true
}

// frontDirs are the packages, relative to the module's root, whose
// diagnostics a case of the type checker's may show besides the
// checker's: those of the stages before it, which the port has, and the
// backend's, which --check runs after it.
var frontDirs = []string{
	"lexer", "parser", "loader", "modfile", "resolver", "constant", "codegen", "codegen/c",
}

// template is one message a compiler's source can report: a format
// string, or a string built with +, turned into patterns that match the
// messages it can give, whole or as a piece of a longer one.
type template struct {
	// re matches the whole of every message the template gives.
	re *regexp.Regexp
	// frag, when not nil, finds the template in a longer message, one
	// built from it and other strings: re without its ends, its gaps as
	// short as they can be. It is nil for a template too short to tell
	// apart from text of any message or from a name (see fragmentLen).
	frag *regexp.Regexp
	// literal is the longest literal piece of the template, which every
	// message it gives holds, to rule most messages out before re runs.
	literal string
	// at is where the template is, file:line, for a report.
	at string
	// size counts the bytes of the template's literal pieces: how much
	// of a message it fixes, and so how specific a match by it is.
	size int
	// stage is the stage of the function the template is in.
	stage checkStage
}

// fragmentLen is the fewest literal bytes a template needs to be looked
// for within a longer message (see pieceable).
const fragmentLen = 6

// pieceable reports whether the template of pieces, with size literal
// bytes holding words words, says enough to be looked for within a
// longer message: fragmentLen bytes or more, a space among them, and two
// words, or one in ten bytes or more, as "q, ok := ... / ". Less, as in
// " and " or " first", is text of messages of every stage, and a piece
// with no space, as "append" or "unsafe_size_t", is more likely a name
// the message quotes than a part of the message.
func pieceable(pieces []piece, size, words int) bool {
	space := slices.ContainsFunc(pieces, func(p piece) bool {
		return !p.wildcard && strings.Contains(p.text, " ")
	})
	return size >= fragmentLen && space && (words >= 2 || words == 1 && size >= 10)
}

// verb matches one verb of a fmt format string, %% included.
var verb = regexp.MustCompile(`%[-+# 0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)

// piece is one part of a template while it is built: literal text, or
// any text at all, a wildcard, for a verb or a value not known.
type piece struct {
	text     string
	wildcard bool
}

// formatPieces returns the pieces of s, a string literal's value: its
// verbs, which a format would replace, as wildcards, and %% as %. A
// string that is no format holds no verb, or if it holds one by chance
// is the harvest's loss, a pattern a little wider than its message.
func formatPieces(s string) []piece {
	var out []piece
	last := 0
	for _, m := range verb.FindAllStringIndex(s, -1) {
		if m[0] > last {
			out = append(out, piece{text: s[last:m[0]]})
		}
		if s[m[0]:m[1]] == "%%" {
			out = append(out, piece{text: "%"})
		} else {
			out = append(out, piece{wildcard: true})
		}
		last = m[1]
	}
	if last < len(s) {
		out = append(out, piece{text: s[last:]})
	}
	return out
}

// newTemplate returns the template of pieces, or false when it says too
// little to tell one message from another: fewer than two letters in a
// row in its literal text, as %s or %s: %s, which would match messages of
// every stage.
func newTemplate(pieces []piece, stage checkStage, at string) (template, bool) {
	t := template{stage: stage, at: at}
	whole, frag, words := patterns(pieces, &t)
	if words == 0 {
		return template{}, false
	}
	t.re = regexp.MustCompile(`\A` + strings.Join(whole, "") + `\z`)
	if pieceable(pieces, t.size, words) {
		t.frag = regexp.MustCompile(strings.Join(trimGaps(frag), ""))
	}
	return t, true
}

// patterns returns the parts of the patterns of pieces, whole and as a
// piece of a longer message, a run of wildcards as one gap, and how many
// words their literal text holds, setting t's literal and size.
func patterns(pieces []piece, t *template) (whole, frag []string, words int) {
	for _, p := range pieces {
		switch {
		case !p.wildcard:
			q := regexp.QuoteMeta(p.text)
			whole, frag = append(whole, q), append(frag, q)
			t.size += len(p.text)
			if len(p.text) > len(t.literal) {
				t.literal = p.text
			}
			words += countWords(p.text)
		case len(whole) == 0 || whole[len(whole)-1] != `.*`:
			whole, frag = append(whole, `.*`), append(frag, `.*?`)
		}
	}
	return whole, frag, words
}

// trimGaps returns frag without the gaps at its ends, which a piece
// found within a message does not need.
func trimGaps(frag []string) []string {
	for len(frag) > 0 && frag[0] == `.*?` {
		frag = frag[1:]
	}
	for len(frag) > 0 && frag[len(frag)-1] == `.*?` {
		frag = frag[:len(frag)-1]
	}
	return frag
}

// countWords counts the runs of two letters or more in s.
func countWords(s string) int {
	n, run := 0, 0
	for _, r := range s {
		if !unicode.IsLetter(r) {
			run = 0
			continue
		}
		if run++; run == 2 {
			n++
		}
	}
	return n
}

// harvester collects the templates of one Go package's files.
type harvester struct {
	fset *token.FileSet
	// covered are the string literals and + expressions already part of
	// a template, which are not templates of their own.
	covered map[ast.Node]bool
	out     []template
}

// harvestDir returns the templates of every non-test Go file in dir,
// each of the stage stageOf gives its file and function; stageOf's false
// for a file is an error.
func harvestDir(dir string, stageOf func(file, fn string) (checkStage, bool)) ([]template, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("harvesting messages: %w", err)
	}
	h := &harvester{fset: token.NewFileSet(), covered: map[ast.Node]bool{}}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, ok := stageOf(name, ""); !ok {
			return nil, fmt.Errorf("harvesting messages: %s has no stage", filepath.Join(dir, name))
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(h.fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("harvesting messages: %w", err)
		}
		for _, d := range f.Decls {
			fn := ""
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn = funcKey(fd)
			}
			stage, _ := stageOf(name, fn)
			ast.Inspect(d, func(n ast.Node) bool { return h.visit(n, stage) })
		}
	}
	return h.out, nil
}

// visit adds the template n is, if it is one not yet covered: a + of
// strings, or a string literal, read as a format. It always goes on into
// n's children, where a call's arguments may hold more.
func (h *harvester) visit(n ast.Node, stage checkStage) bool {
	if n == nil || h.covered[n] {
		return true
	}
	pieces, ok := h.templatePieces(n)
	if !ok {
		return true
	}
	if t, ok := newTemplate(pieces, stage, h.fset.Position(n.Pos()).String()); ok {
		h.out = append(h.out, t)
	}
	return true
}

// templatePieces returns the pieces of the template n is, when it is a
// + of strings or a string literal.
func (h *harvester) templatePieces(n ast.Node) ([]piece, bool) {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		if n.Op == token.ADD && h.holdsString(n) {
			return h.concat(n), true
		}
	case *ast.BasicLit:
		if n.Kind != token.STRING {
			return nil, false
		}
		if s, err := strconv.Unquote(n.Value); err == nil {
			return formatPieces(s), true
		}
	}
	return nil, false
}

// holdsString reports whether the + expression e has a string literal,
// or a call to fmt.Sprintf with one, among its operands.
func (h *harvester) holdsString(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.BinaryExpr:
		return e.Op == token.ADD && (h.holdsString(e.X) || h.holdsString(e.Y))
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.CallExpr:
		_, ok := sprintfFormat(e)
		return ok
	}
	return false
}

// concat returns the pieces of e, a + of strings, marking every + and
// literal in it covered: a literal, or the format of a call to
// fmt.Sprintf, as its pieces, and anything else as a wildcard.
func (h *harvester) concat(e ast.Expr) []piece {
	h.covered[e] = true
	switch e := ast.Unparen(e).(type) {
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			h.covered[e] = true
			return append(h.concat(e.X), h.concat(e.Y)...)
		}
	case *ast.BasicLit:
		if s, err := strconv.Unquote(e.Value); err == nil && e.Kind == token.STRING {
			h.covered[e] = true
			return formatPieces(s)
		}
	case *ast.CallExpr:
		if lit, ok := sprintfFormat(e); ok {
			h.covered[lit] = true
			s, err := strconv.Unquote(lit.Value)
			if err == nil { // sprintfFormat took only a literal that unquotes
				return formatPieces(s)
			}
		}
	}
	return []piece{{wildcard: true}}
}

// sprintfFormat returns the format of call when it is a call to
// fmt.Sprintf whose format is a string literal.
func sprintfFormat(call *ast.CallExpr) (*ast.BasicLit, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" || len(call.Args) == 0 {
		return nil, false
	}
	if x, isIdent := sel.X.(*ast.Ident); !isIdent || x.Name != "fmt" {
		return nil, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, false
	}
	if _, err := strconv.Unquote(lit.Value); err != nil {
		return nil, false
	}
	return lit, true
}

// harvestAll returns the templates of the type checker, each of its
// function's stage, and of the stages before it, of stageFront, read
// from the module whose root is root.
func harvestAll(root string) ([]template, error) {
	out, err := harvestDir(filepath.Join(root, "typeck"), typeckFuncStage)
	if err != nil {
		return nil, err
	}
	for _, d := range frontDirs {
		front, err := harvestDir(filepath.Join(root, d), func(string, string) (checkStage, bool) {
			return stageFront, true
		})
		if err != nil {
			return nil, err
		}
		out = append(out, front...)
	}
	return out, nil
}

// diagHeader matches the first line of a rendered diagnostic, capturing
// its message.
var diagHeader = regexp.MustCompile(
	`^(?:\S+:\d+:\d+|<unknown file>): (?:error|warning|note): (.*)$`)

// messages returns the message of every diagnostic, note or warning
// that expected, a case's expected output as the archives hold it,
// renders, in order.
func messages(expected []byte) []string {
	var out []string
	for _, line := range strings.Split(string(expected), "\n") {
		if m := diagHeader.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// lineMatch is how one message matched the templates.
type lineMatch struct {
	// stage is the message's stage, as matchLine finds it, or
	// stageGeneric when no template matched.
	stage checkStage
	// none says it matched no template.
	none bool
	// pieced says no template matched the whole message, and it was
	// matched by templates of its pieces.
	pieced bool
	// several says the templates that decided its stage are of more
	// than one stage.
	several bool
}

// matchLine matches msg against templates. Of the templates that match
// the whole of it, those that fix the most of it decide, the most
// specific: a format string written in several files, of several stages,
// matches as well in each, and gives the highest of their stages. When
// none matches the whole of it, msg was built from pieces, as an error
// with advice appended: every template found in it, but those found
// within the span of another, decides, and the highest of their stages
// is its stage. When none matches at all, the stage is the highest.
func matchLine(templates []template, msg string) lineMatch {
	best := -1
	var stages []checkStage
	for i := range templates {
		t := &templates[i]
		if t.size < best || !strings.Contains(msg, t.literal) || !t.re.MatchString(msg) {
			continue
		}
		if t.size > best {
			best, stages = t.size, nil
		}
		stages = append(stages, t.stage)
	}
	pieced := false
	if len(stages) == 0 {
		stages, pieced = pieceStages(templates, msg), true
	}
	if len(stages) == 0 {
		return lineMatch{stage: stageGeneric, none: true}
	}
	m := lineMatch{stage: slices.Max(stages), pieced: pieced}
	m.several = slices.Min(stages) != m.stage
	return m
}

// found is one place a template is found in a message: the bytes it
// spans, how many of them it fixes, and the stages of every template
// found there alike.
type found struct {
	stages     []checkStage
	start, end int
	size       int
}

// pieceStages returns the stages of the templates that best explain msg
// as pieces: of the templates found within it, those that fix the most
// of it together without overlapping, each place that more than one
// template fixes as much of giving all of their stages.
func pieceStages(templates []template, msg string) []checkStage {
	var stages []checkStage
	for _, f := range bestPieces(findPieces(templates, msg)) {
		stages = append(stages, f.stages...)
	}
	return stages
}

// findPieces returns every place a template that can be a piece is
// found in msg, templates found at the same place and fixing as much
// merged into one.
func findPieces(templates []template, msg string) []found {
	var all []found
	for i := range templates {
		t := &templates[i]
		if t.frag == nil || !strings.Contains(msg, t.literal) {
			continue
		}
		loc := t.frag.FindStringIndex(msg)
		if loc == nil {
			continue
		}
		j := slices.IndexFunc(all, func(f found) bool {
			return f.start == loc[0] && f.end == loc[1] && f.size == t.size
		})
		if j < 0 {
			all = append(all, found{start: loc[0], end: loc[1], size: t.size})
			j = len(all) - 1
		}
		all[j].stages = append(all[j].stages, t.stage)
	}
	return all
}

// bestPieces returns the places of all that fix the most of a message
// without overlapping, by weighted interval scheduling: best[i] is the
// most the first i places, by end, fix, and pick[i] the places that do.
func bestPieces(all []found) []found {
	slices.SortStableFunc(all, func(a, b found) int { return a.end - b.end })
	best := make([]int, len(all)+1)
	pick := make([][]int, len(all)+1)
	for i, f := range all {
		prev := i
		for prev > 0 && all[prev-1].end > f.start {
			prev--
		}
		best[i+1], pick[i+1] = best[i], pick[i]
		if with := best[prev] + f.size; with > best[i] {
			best[i+1] = with
			pick[i+1] = append(slices.Clone(pick[prev]), i)
		}
	}
	out := make([]found, 0, len(pick[len(all)]))
	for _, i := range pick[len(all)] {
		out = append(out, all[i])
	}
	return out
}

// checkCase is one case of the type checker's, from an archive, with
// the stage of the port that must pass it.
type checkCase struct {
	// archive is the archive's name, as invalid for invalid.txtar.
	archive string
	// name is the case's name in its archive.
	name string
	// target is what the case is built from, relative to the archive's
	// directory: <name>/main.tck for an archive of single files.
	target string
	// why names what in the tree set treeStage.
	why string
	// unmatched are its messages no template matches.
	unmatched []string
	// expected is the case's expected output, as its archive holds it.
	expected []byte
	// std are the files of the case's own std, each by its path below
	// the case's .std directory, or nil when it is built with the
	// repository's.
	std []invalid.TreeFile
	// stage is the stage, the higher of diagStage and treeStage, or
	// stageBackend for a case the backend rejects or one with no error
	// before the backend, which --check runs the backend on.
	stage checkStage
	// diagStage is the highest stage of its messages' templates.
	diagStage checkStage
	// treeStage is the stage Go's typed tree of the case implies.
	treeStage checkStage
	// several counts its messages whose stage templates of several
	// stages decided.
	several int
	// pieced counts its messages matched by pieces (see matchLine).
	pieced int
}

// checkCases loads every case of the type checker's: each case of
// invalid.txtar, invalid-packages.txtar, warnings.txtar and
// warnings-packages.txtar whose program Go's --check loads, and so type
// checks, each classified, in archive order. It runs Go's type checker
// on each, in process.
func checkCases(root string) ([]checkCase, error) {
	templates, err := harvestAll(root)
	if err != nil {
		return nil, err
	}
	var out []checkCase
	for _, p := range singleArchives {
		cases, err := singleCheckCases(root, p, templates)
		if err != nil {
			return nil, err
		}
		out = append(out, cases...)
	}
	for _, p := range treeArchives {
		cases, err := treeCheckCases(root, p, templates)
		if err != nil {
			return nil, err
		}
		out = append(out, cases...)
	}
	return out, nil
}

// archivePath returns the archive at p, a path relative to a top-level
// package directory, from the module whose root is root, and its name.
func archivePath(root, p string) (path, name string) {
	return filepath.Join(root, "porttest", filepath.FromSlash(p)),
		strings.TrimSuffix(filepath.Base(p), ".txtar")
}

// singleCheckCases returns the cases of the type checker's of the
// archive of single files at p, classified with templates.
func singleCheckCases(root, p string, templates []template) ([]checkCase, error) {
	path, archive := archivePath(root, p)
	cases, err := invalid.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading the corpus: %w", err)
	}
	var out []checkCase
	for _, c := range cases {
		target := c.Name + "/main.tck"
		cc := checkCase{archive: archive, name: c.Name, target: target, expected: c.Expected}
		typed, err := cc.classify(templates, fstest.MapFS{target: {Data: c.Source}}, nil, true)
		if err != nil {
			return nil, err
		}
		if typed {
			out = append(out, cc)
		}
	}
	return out, nil
}

// treeCheckCases returns the cases of the type checker's of the archive
// of trees at p, classified with templates, each with its own std when
// it has one.
func treeCheckCases(root, p string, templates []template) ([]checkCase, error) {
	path, archive := archivePath(root, p)
	trees, err := invalid.LoadTree(path)
	if err != nil {
		return nil, fmt.Errorf("loading the corpus: %w", err)
	}
	var out []checkCase
	for _, c := range trees {
		cc := checkCase{archive: archive, name: c.Name, target: c.Target(), expected: c.Expected}
		files := make(fstest.MapFS, len(c.Files))
		for _, f := range c.Files {
			files[c.Name+"/"+f.Path] = &fstest.MapFile{Data: f.Data}
		}
		var std *loader.Std
		if dir := c.StdDir(); dir != "" {
			std = &loader.Std{FS: loader.FromFS(files), Dir: dir}
			cc.std = stdFiles(c, dir)
		}
		typed, err := cc.classify(templates, files, std, filepath.Ext(c.Target()) == ".tck")
		if err != nil {
			return nil, err
		}
		if typed {
			out = append(out, cc)
		}
	}
	return out, nil
}

// stdFiles returns the files of c below dir, its std's directory, each
// by its path below dir.
func stdFiles(c invalid.TreeCase, dir string) []invalid.TreeFile {
	var out []invalid.TreeFile
	for _, f := range c.Files {
		if rel, ok := strings.CutPrefix(c.Name+"/"+f.Path, dir+"/"); ok {
			out = append(out, invalid.TreeFile{Path: rel, Data: f.Data})
		}
	}
	return out
}

// classify sets c's stages from c.expected matched against templates,
// and from Go's typed tree of files, built from c.target, a file when
// single, with std, or the repository's when nil; a case the backend
// rejects, or with no error before the backend, is stageBackend's. It
// reports whether
// the program loaded, and so was type checked; c's stages are set only
// when it did.
func (c *checkCase) classify(
	templates []template, files fstest.MapFS, std *loader.Std, single bool,
) (bool, error) {
	prog, clean, err := typedTree(files, c.target, std, single)
	if err != nil {
		return false, fmt.Errorf("%s/%s: %w", c.archive, c.name, err)
	}
	if prog == nil {
		return false, nil
	}
	c.diagStage = stageFront
	for _, msg := range messages(c.expected) {
		m := matchLine(templates, msg)
		if m.none {
			c.unmatched = append(c.unmatched, msg)
		}
		if m.several {
			c.several++
		}
		if m.pieced {
			c.pieced++
		}
		c.diagStage = max(c.diagStage, m.stage)
	}
	c.treeStage, c.why = treeStage(prog, c.name+"/")
	c.stage = max(c.diagStage, c.treeStage)
	if first, _, _ := strings.Cut(string(c.expected), "\n"); first == "stage: codegen" || clean {
		c.stage = stageBackend
	}
	return true, nil
}

// typedTreeTimeout bounds how long typedTree waits for Go's loader,
// resolver and type checker on one case, as test/'s caseTimeout bounds
// a pipeline run, so a hang fails the classification rather than stall
// it.
const typedTreeTimeout = 5 * time.Second

// typedTree loads target from files as Go's --check loads it, a single
// file when single and a directory otherwise, as a library may be, with
// no limit on errors, with std or the repository's, and runs Go's
// resolver and type checker on it, whatever any stage reported,
// returning the program they annotated, nil when the loader built none,
// which --check then does not type check, and whether the loader, the
// resolver and the type checker reported no error, warnings aside: then
// --check runs the backend, whose judgment its output depends on.
func typedTree(
	files fstest.MapFS, target string, std *loader.Std, single bool,
) (*tackast.Program, bool, error) {
	if std == nil {
		std = stdtest.Std()
	}
	type outcome struct {
		prog  *tackast.Program
		clean bool
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		prog, clean, err := loadAndCheck(files, target, std, single)
		done <- outcome{prog, clean, err}
	}()
	select {
	case o := <-done:
		return o.prog, o.clean, o.err
	case <-time.After(typedTreeTimeout):
		return nil, false, fmt.Errorf("go's loader and checker did not finish within %v", typedTreeTimeout)
	}
}

// loadAndCheck is typedTree's work, without its timeout; a panic of Go's
// compiler is returned as an error.
func loadAndCheck(
	files fstest.MapFS, target string, std *loader.Std, single bool,
) (prog *tackast.Program, clean bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			prog, clean, err = nil, false, fmt.Errorf("go's compiler panicked: %v", r)
		}
	}()
	l := &loader.Loader{
		FS: loader.FromFS(files), Builtins: stdtest.MustBuiltins(), Std: std, Lib: true,
	}
	var res *loader.Result
	if single {
		res, err = l.File(target)
	} else {
		res, err = l.Dir(target)
	}
	if err != nil {
		// The driver reports the error and checks nothing.
		return nil, false, nil //nolint:nilerr // a load error is a case --check does not type check
	}
	if res.Program == nil {
		return nil, false, nil
	}
	r := resolver.New(res.Program, stdtest.MustBuiltins())
	r.MainOptional = true
	diags := append(slices.Clone(res.Diags), r.Resolve()...)
	// The diagnostics are looked at only for whether any is an error, as
	// the driver's checkProgram looks at them to decide whether the
	// backend runs: the archive's expected output holds them, and
	// TestCheckExpected checks that Go's --check gives it.
	diags = append(diags, typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check()...)
	return res.Program, !diag.HasErrors(diags), nil
}

// treeStage returns the stage Go's typed tree prog implies, from every
// package's files, std's and any other imported package's included,
// since Go's checker checks every function's body, and what in it set
// the stage: what in the files named under prefix, the case's own, set
// it, or, when a package of another's set it higher, that, marked as
// an imported package's. A tree with a statement in a function's body is
// at least S2a; one with none is S1, the declarations alone, unless
// something else in it raises it.
func treeStage(prog *tackast.Program, prefix string) (checkStage, string) {
	s, why, _ := treeStages(prog, prefix)
	return s, why
}

// treeStages is treeStage, with every stage some part of the whole
// program's tree implies, imported packages' included, the ones below
// the highest among them: a tree of S7 that implies S4 or S5 too waits
// for those stages as well.
func treeStages(prog *tackast.Program, prefix string) (checkStage, string, map[checkStage]bool) {
	own := walkTree(prog, func(file string) bool { return strings.HasPrefix(file, prefix) })
	all := walkTree(prog, func(string) bool { return true })
	if all.stage > own.stage {
		return all.stage, "an imported package's " + all.why, all.stages
	}
	return own.stage, own.why, all.stages
}

// walkTree walks the files of prog whose names keep says to, and
// returns the walk, with the stage they imply and why.
func walkTree(prog *tackast.Program, keep func(file string) bool) *treeWalk {
	w := &treeWalk{stage: stageDecls, why: "declarations only", stages: map[checkStage]bool{}}
	if prog == nil {
		return w
	}
	w.table = prog.Types
	globals := map[tackast.Decl]bool{}
	for _, d := range prog.Decls() {
		globals[d] = true
	}
	for _, pkg := range prog.Packages {
		for _, f := range pkg.Files {
			if !keep(string(f.Span.File)) {
				continue
			}
			w.callees = map[*tackast.Ident]bool{}
			w.held = map[tackast.Decl]bool{}
			w.written = map[*tackast.Ident]bool{}
			tackast.Walk(f, func(n tackast.Node) bool {
				switch x := n.(type) {
				case *tackast.Call:
					if id, ok := x.Fn.(*tackast.Ident); ok {
						w.callees[id] = true
					}
				case *tackast.MultiVar:
					targets := make([]tackast.Decl, len(x.Vars))
					for i, v := range x.Vars {
						if v.Name != tackast.Blank {
							targets[i] = v
						}
					}
					w.hold(x.Value, targets)
				case *tackast.MultiAssign:
					targets := make([]tackast.Decl, len(x.Targets))
					for i, t := range x.Targets {
						if id, ok := t.(*tackast.Ident); ok {
							w.written[id] = true
							if v, ok := id.Def.(*tackast.Var); ok && !globals[v] {
								targets[i] = v
							}
						}
					}
					w.hold(x.Value, targets)
				case *tackast.Assign:
					if id, ok := x.Target.(*tackast.Ident); ok && x.Op == tackast.InvalidOp {
						w.written[id] = true
					}
				}
				return true
			})
			tackast.Walk(f, w.visit)
		}
	}
	return w
}

// treeWalk is the walk of a typed tree treeStage makes.
type treeWalk struct {
	// table is the program's types.
	table *types.Table
	// callees are the names called in the file walked.
	callees map[*tackast.Ident]bool
	// held are the variables of the file walked that a binding of an
	// error, or of a checked pop or remove's ok, holds, which reading
	// needs the facts in force (see hold); written are the names a plain
	// assignment or a binding writes whole, which read nothing.
	held    map[tackast.Decl]bool
	written map[*tackast.Ident]bool
	// why names what set stage.
	why string
	// stage is the highest stage found so far.
	stage checkStage
	// stages holds every stage some part of the tree implies, the ones
	// below stage among them.
	stages map[checkStage]bool
}

// raise raises the walk's stage to s, for the reason why, if s is
// higher; of reasons for the same stage the first is kept.
func (w *treeWalk) raise(s checkStage, why string) {
	if w.stages != nil {
		w.stages[s] = true
	}
	if s > w.stage {
		w.stage, w.why = s, why
	}
}

// visit raises the walk's stage for n and goes on into its children.
func (w *treeWalk) visit(n tackast.Node) bool {
	switch n := n.(type) {
	case *tackast.Func:
		w.function(n)
	case *tackast.Param, *tackast.Var:
		w.local(n)
	case tackast.Stmt:
		w.stmt(n)
	case tackast.Expr:
		w.expr(n)
	}
	return true
}

// local raises the walk's stage for a parameter or a variable, by its
// type and whether it owns what it holds.
func (w *treeWalk) local(n tackast.Node) {
	switch n := n.(type) {
	case *tackast.Param:
		w.typed(n.Type, "a parameter")
		if n.Own || w.owned(n.Type) {
			w.raise(stageOwn, "an owned parameter")
		}
	case *tackast.Var:
		w.typed(n.Type, "a variable")
		if w.owned(n.Type) {
			w.raise(stageOwn, "an owned local")
		}
		if appendBound(n) {
			w.raise(stageLoops, "an append's error bound")
		}
	}
}

// appendBound reports whether v is an immutable error declared with the
// value of a call of append passing values to a mut list, which Go
// records (recordAppend): the append's success then proves the list at
// least as long as the values it adds, a proof S5's appendedLen gives.
func appendBound(v *tackast.Var) bool {
	call, ok := v.Value.(*tackast.Call)
	if !ok || v.Mutable || v.Type != types.Error || len(call.Args) < 2 {
		return false
	}
	if _, ok := call.Args[0].(*tackast.MutArg); !ok {
		return false
	}
	id, ok := call.Fn.(*tackast.Ident)
	if !ok || id.Pkg != "" || id.Name != "append" {
		return false
	}
	fn, ok := id.Def.(*tackast.Func)
	return ok && fn == nil
}

// function raises the walk's stage for fn's signature and body.
func (w *treeWalk) function(fn *tackast.Func) {
	if len(fn.Body) > 0 {
		w.raise(stageCore, "a function body")
	}
	if fn.Ensures != nil {
		w.raise(stageLoops, "an ensures clause")
	}
	if len(fn.TypeParams) > 0 {
		w.raise(stageGeneric, "type parameters")
	}
	for _, t := range fn.ResultTypes {
		w.typed(t, "a result")
		if w.table != nil && w.table.IsView(t) {
			w.raise(stageOwn, "a view result")
		}
	}
	for _, r := range fn.Results {
		if _, ok := r.(*tackast.ViewType); ok {
			w.raise(stageOwn, "a view result")
		}
	}
}

// stmt raises the walk's stage for s.
func (w *treeWalk) stmt(s tackast.Stmt) {
	switch s := s.(type) {
	case *tackast.IncDec:
		w.raise(stageArith, "an increment or decrement")
	case *tackast.Assign:
		w.assign(s)
	case *tackast.MultiVar:
		w.checkedForm(s.Value)
	case *tackast.MultiAssign:
		w.checkedForm(s.Value)
	default:
		w.flow(s)
	}
}

// flow raises the walk's stage for a loop, an unreachable or a region.
func (w *treeWalk) flow(s tackast.Stmt) {
	switch s := s.(type) {
	case *tackast.For:
		if v, ok := s.Init.(*tackast.Var); ok && s.Post != nil && v.Mutable {
			w.raise(stageLoops, "a counter loop")
		}
		w.loop(s)
	case *tackast.ForRange:
		w.loop(s)
	case *tackast.Unreachable:
		w.raise(stageLoops, "unreachable")
	case *tackast.Region:
		w.raise(stageOwn, "a region")
	}
}

// assign raises the walk's stage for a compound assignment.
func (w *treeWalk) assign(a *tackast.Assign) {
	if a.Op == tackast.InvalidOp {
		return
	}
	if a.Op.IsInteger() && !types.IsFloat(tackast.TypeOf(a.Target)) {
		w.raise(stageArith, "an integer compound assignment")
	}
	if (a.Op == tackast.Div || a.Op == tackast.Rem) && !constantExpr(a.Value) {
		w.raise(stageFacts, "a divisor not constant")
	}
}

// loop raises the walk's stage for a loop whose body writes an integer
// declared before it, whose bounds the loop's invariants carry: by
// assignment, compound or not, increment or decrement, to the variable
// or a field or element of it. A target whose type an error left
// unknown counts as an integer's.
func (w *treeWalk) loop(loop tackast.Stmt) {
	inner := innerDecls(loop)
	for _, t := range loopTargets(loop) {
		root := rootIdent(t)
		if root == nil || inner[root.Def] || !isLocal(root.Def) {
			continue
		}
		if typ := tackast.TypeOf(t); typ == types.Invalid || types.IsInteger(typ) {
			w.raise(stageLoops, "a loop writing an integer declared before it")
			return
		}
	}
}

// innerDecls returns the variables and parameters declared within n.
func innerDecls(n tackast.Node) map[tackast.Decl]bool {
	inner := map[tackast.Decl]bool{}
	tackast.Walk(n, func(n tackast.Node) bool {
		switch d := n.(type) {
		case *tackast.Var:
			inner[d] = true
		case *tackast.Param:
			inner[d] = true
		}
		return true
	})
	return inner
}

// loopTargets returns every place an assignment, an increment or a
// decrement within n writes.
func loopTargets(n tackast.Node) []tackast.Expr {
	var targets []tackast.Expr
	tackast.Walk(n, func(n tackast.Node) bool {
		switch s := n.(type) {
		case *tackast.Assign:
			targets = append(targets, s.Target)
		case *tackast.IncDec:
			targets = append(targets, s.Target)
		case *tackast.MultiAssign:
			targets = append(targets, s.Targets...)
		}
		return true
	})
	return targets
}

// isLocal reports whether d is a variable or a parameter.
func isLocal(d tackast.Decl) bool {
	switch d.(type) {
	case *tackast.Var, *tackast.Param:
		return true
	}
	return false
}

// rootIdent returns the variable e is, or a field or element of, or nil
// when e is no such place.
func rootIdent(e tackast.Expr) *tackast.Ident {
	for {
		switch x := e.(type) {
		case *tackast.Ident:
			return x
		case *tackast.Selector:
			e = x.X
		case *tackast.Index:
			e = x.X
		default:
			return nil
		}
	}
}

// checkedForm raises the walk's stage for v, ok := T(x), when value is a
// conversion.
func (w *treeWalk) checkedForm(value tackast.Expr) {
	if call, ok := value.(*tackast.Call); ok && call.Conv != types.Invalid {
		w.raise(stageArith, "a checked conversion")
	}
}

// expr raises the walk's stage for e.
func (w *treeWalk) expr(e tackast.Expr) {
	switch e := e.(type) {
	case *tackast.Binary:
		w.binary(e)
	case *tackast.Unary:
		if e.Op != tackast.Not && e.Const == nil && intOrUnknown(e.Type) {
			w.raise(stageArith, "integer arithmetic not constant")
		}
	case *tackast.Call:
		w.call(e)
	case *tackast.Ident:
		w.ident(e)
	default:
		w.place(e)
	}
}

// place raises the walk's stage for an element, a slice, a literal or a
// field.
func (w *treeWalk) place(e tackast.Expr) {
	switch e := e.(type) {
	case *tackast.Index:
		w.raise(stageFacts, "an index")
		w.typed(e.Type, "an element")
	case *tackast.Slice:
		w.raise(stageArith, "a slice")
	case *tackast.CompositeLit:
		w.typed(e.Type, "a literal")
	case *tackast.Selector:
		w.typed(e.Type, "a field")
	default:
		w.kind(e)
	}
}

// intOrUnknown reports whether t is an integer type, or unknown, as an
// error leaves it.
func intOrUnknown(t types.Type) bool {
	return types.IsInteger(t) || t == types.Invalid
}

// binary raises the walk's stage for b.
func (w *treeWalk) binary(b *tackast.Binary) {
	if b.Op.IsInteger() && b.Const == nil && intOrUnknown(b.Type) {
		w.raise(stageArith, "integer arithmetic not constant")
	}
	if (b.Op == tackast.Div || b.Op == tackast.Rem) && !constantExpr(b.Y) {
		w.raise(stageFacts, "a divisor not constant")
	}
}

// kind raises the walk's stage for an expression whose kind alone sets
// it.
func (w *treeWalk) kind(e tackast.Expr) {
	switch e.(type) {
	case *tackast.FuncLit:
		w.raise(stageGeneric, "a function literal")
	case *tackast.FuncType:
		w.raise(stageGeneric, "a function type")
	case *tackast.FieldsOf:
		w.raise(stageGeneric, "a fields loop")
	case *tackast.MutArg:
		w.raise(stageCalls, "a mut argument")
	case *tackast.OwnArg:
		w.raise(stageCalls, "an own argument")
	case *tackast.TypeAssert:
		w.raise(stageCalls, "a type assertion")
	case *tackast.Question:
		w.raise(stageCalls, "a ?")
	case *tackast.ListType, *tackast.MapType, *tackast.ArenaType, *tackast.PoolType,
		*tackast.RefType, *tackast.ViewType:
		w.raise(stageCalls, "a container type")
	}
}

// call raises the walk's stage for call.
func (w *treeWalk) call(call *tackast.Call) {
	w.callee(call)
	if call.Conv == types.Invalid {
		w.raise(stageCalls, "a call")
		return
	}
	if call.Const == nil && len(call.Args) == 1 && types.IsInteger(call.Conv) {
		from := tackast.TypeOf(call.Args[0])
		if from == types.Invalid || (!types.IsUntyped(from) && !types.Widens(from, call.Conv)) {
			w.raise(stageArith, "a narrowing conversion")
		}
	}
}

// callee raises the walk's stage for what call calls and how: with type
// arguments, through a function value, with two mut arguments or more,
// or drop or swap.
func (w *treeWalk) callee(call *tackast.Call) {
	if len(call.TypeArgs) > 0 || len(call.TypeArgExprs) > 0 {
		w.raise(stageGeneric, "type arguments")
	}
	if call.Dynamic != nil {
		w.raise(stageGeneric, "a call of a function value")
	}
	if mutArgs(call) >= 2 {
		w.raise(stageOwn, "two mut arguments")
	}
	if name, ok := ownBuiltin(call); ok {
		w.raise(stageOwn, "a call of "+name)
	}
}

// mutArgs counts call's mut arguments.
func mutArgs(call *tackast.Call) int {
	n := 0
	for _, a := range call.Args {
		if _, ok := a.(*tackast.MutArg); ok {
			n++
		}
	}
	return n
}

// ownBuiltin returns the name of the builtin call calls when it is drop
// or swap, which the ownership checks own.
func ownBuiltin(call *tackast.Call) (string, bool) {
	id, ok := call.Fn.(*tackast.Ident)
	if !ok || id.Pkg != "" || id.Name != "drop" && id.Name != "swap" {
		return "", false
	}
	fn, ok := id.Def.(*tackast.Func)
	return id.Name, ok && fn == nil
}

// hold records the variables a binding of value to targets, the local
// variable each name goes to or nil, holds, as Go's collectHolds and
// hold tie them: every one but the last, when value is a call whose
// last result is an error and that gives one result for each target,
// or add, add_all or insert bound to two names; and the element of a
// checked pop, remove or delete bound with its ok, when its type has no
// zero value, as Go's okHeld says. Reading one of them is checked
// against the facts in force (Go's requireChecked), which is S3's.
func (w *treeWalk) hold(value tackast.Expr, targets []tackast.Decl) {
	call, ok := value.(*tackast.Call)
	if !ok || call.Conv != types.Invalid || len(targets) < 2 {
		return
	}
	name, builtin := builtinName(call)
	errorTie := false
	switch {
	case builtin && (name == "add" || name == "add_all" || name == "insert"):
		errorTie = len(targets) == 2
	case builtin && (name == "pop" || name == "remove" || name == "delete"):
		if len(targets) == 2 && targets[1] != nil {
			if v, ok := targets[0].(*tackast.Var); ok && v.Type != types.Invalid &&
				w.table != nil && !w.table.HasZero(v.Type) {
				w.held[v] = true
			}
		}
		return
	default:
		if fn := tackast.Callee(call); fn != nil {
			rs := tackast.CallResults(call)
			errorTie = len(rs) == len(targets) && rs[len(rs)-1] == types.Error
		}
	}
	if !errorTie || targets[len(targets)-1] == nil {
		return
	}
	for _, t := range targets[:len(targets)-1] {
		if t != nil {
			w.held[t] = true
		}
	}
}

// builtinName returns the name of the builtin call calls, and whether it
// calls one.
func builtinName(call *tackast.Call) (string, bool) {
	id, ok := call.Fn.(*tackast.Ident)
	if !ok || id.Pkg != "" {
		return "", false
	}
	fn, ok := id.Def.(*tackast.Func)
	return id.Name, ok && fn == nil
}

// ident raises the walk's stage for a name used as a function value: a
// function's name anywhere but as a callee, whether or not the checker
// let it be one, or a name whose type is a function type.
func (w *treeWalk) ident(id *tackast.Ident) {
	if id.Def != nil && w.held[id.Def] && !w.written[id] {
		w.raise(stageFacts, "a read of a variable an error or an ok holds")
	}
	switch d := id.Def.(type) {
	case *tackast.Func:
		if d != nil && !w.callees[id] {
			w.raise(stageGeneric, "a function value")
		}
	case *tackast.Param, *tackast.Var:
		if w.isFunc(tackast.TypeOf(id)) {
			w.raise(stageGeneric, "a function value")
		}
	}
}

// typed raises the walk's stage for a value of type t: a container's,
// any's or error's is S2b, a function's S7.
func (w *treeWalk) typed(t types.Type, what string) {
	if t == types.Invalid || w.table == nil {
		return
	}
	switch {
	case w.isFunc(t):
		w.raise(stageGeneric, "a function value")
	case t == types.Any || t == types.Error:
		w.raise(stageCalls, what+" of type "+w.table.Name(t))
	case w.isContainer(t):
		w.raise(stageCalls, what+" of a container type")
	}
}

// isContainer reports whether t is a list, map, arena, pool, id, span,
// handle or view type.
func (w *treeWalk) isContainer(t types.Type) bool {
	tb := w.table
	return tb.IsList(t) || tb.IsMap(t) || tb.IsArena(t) || tb.IsPool(t) || tb.IsRef(t) ||
		tb.IsView(t)
}

// isFunc reports whether t is a function type.
func (w *treeWalk) isFunc(t types.Type) bool {
	return w.table != nil && w.table.IsFunc(t)
}

// owned reports whether a value of type t is owned.
func (w *treeWalk) owned(t types.Type) bool {
	return w.table != nil && w.table.Owned(t)
}

// constantExpr reports whether e is a constant the type checker folded:
// a literal, or an operator or call it gave a value.
func constantExpr(e tackast.Expr) bool {
	switch e := e.(type) {
	case *tackast.IntLit, *tackast.FloatLit, *tackast.CharLit:
		return true
	case *tackast.Binary:
		return e.Const != nil
	case *tackast.Unary:
		return e.Const != nil
	case *tackast.Call:
		return e.Const != nil
	}
	return false
}

// classified holds checkCases' result for the module's root, worked out
// once a run.
var classified struct {
	err   error
	cases []checkCase
	once  sync.Once
}

// classifiedCases returns checkCases for env.root, classifying them on
// the first call.
func classifiedCases() ([]checkCase, error) {
	classified.once.Do(func() {
		classified.cases, classified.err = checkCases(env.root)
		if classified.err == nil && len(classified.cases) == 0 {
			classified.err = errors.New("the type checker has no cases")
		}
	})
	return classified.cases, classified.err
}

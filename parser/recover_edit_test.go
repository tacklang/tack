package parser_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// editFile is a valid program the edit fuzzers damage: its source, its
// tokens, its tree, its path from the repository's root, and the key that
// picks it (see pickFile).
type editFile struct {
	tree   *ast.File
	src    string
	path   string
	tokens []lexer.Token
	key    uint32
}

// fileKey returns the key of the file at path, from the repository's
// root: the FNV-1a hash of the path.
func fileKey(path string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(path))
	return h.Sum32()
}

// pickFile returns the file of editCorpus that key picks: the first, in
// the order of their keys, whose key is key or more, or the first of all
// past the last. A fuzz input names its file by key so that it keeps
// naming it as files are added: one added takes only the keys between
// its own and the one before it, and none of the file's own key, which
// is what a committed seed uses (see the failure's log).
func pickFile(key uint32) editFile {
	files := editCorpus()
	i, _ := slices.BinarySearchFunc(files, key, func(f editFile, key uint32) int {
		return cmp.Compare(f.key, key)
	})
	return files[i%len(files)]
}

// editCorpus holds every Tack file under examples/ and src/std/ that parses
// without an error, read once, in the order of their keys (see pickFile).
var editCorpus = sync.OnceValue(func() []editFile {
	var paths []string
	for _, pattern := range []string{
		"../examples/*/*.tck", "../examples/*/*/*.tck", "../examples/*/*/*/*.tck", "../src/std/*/*.tck",
	} {
		found, _ := filepath.Glob(pattern)
		paths = append(paths, found...)
	}
	slices.Sort(paths)
	var files []editFile
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		toks, tree, diags := parseLimited(string(data), 0)
		if tree != nil && len(diags) == 0 {
			rel := strings.TrimPrefix(filepath.ToSlash(p), "../")
			files = append(files, editFile{
				tree: tree, src: string(data), path: rel, tokens: toks, key: fileKey(rel),
			})
		}
	}
	slices.SortFunc(files, func(a, b editFile) int {
		return cmp.Or(cmp.Compare(a.key, b.key), strings.Compare(a.path, b.path))
	})
	return files
})

// parseLimited lexes src and parses it with a limit of limit errors (see
// parser.Parser.Limit), giving the tokens, the tree and the errors, or
// only the lexer's errors when src does not lex.
func parseLimited(src string, limit int) ([]lexer.Token, *ast.File, []diag.Diagnostic) {
	f := &source.File{ID: "edit", Filename: "edit.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil, nil, diags
	}
	p := parser.NewParser(f.ID, lex.Tokens())
	p.Limit(limit)
	tree, diags := p.Parse()
	return lex.Tokens(), tree, diags
}

// insertions are what an edit puts into a program: brackets, operators,
// keywords and line breaks, each a whole token or a few.
var insertions = []string{
	")", "(", "{", "}", "]", "[", ",", ";", "+", "=", ":=", "func", "x", "1",
	"if", "for", "return", "var", "type", ".", "\n", "pub", "import", "case",
	"switch", "mut", "defer", "else", "...", "*", "!", "x :=", "} else {", "unreachable", "?",
}

// editTokens returns the indexes of f's tokens that cover bytes, the ones
// an edit can delete, copy or replace.
func editTokens(f editFile) []int {
	var covering []int
	for k, tk := range f.tokens {
		if tk.Kind() != lexer.EOF && tk.OffsetEnd() > tk.OffsetStart() {
			covering = append(covering, k)
		}
	}
	return covering
}

// edit damages f at its i'th token that covers bytes, counted modulo how
// many there are: op 0 deletes it, 1 inserts insertions[ins] before it, 2
// copies it after itself, and 3 replaces it with insertions[ins]. It
// returns the damaged source, the offset of the edit, how many bytes
// longer the source got, and false when f has no such token.
func edit(f editFile, i int, op, ins uint8) (string, int, int, bool) {
	covering := editTokens(f)
	if len(covering) == 0 {
		return "", 0, 0, false
	}
	tk := f.tokens[covering[i%len(covering)]]
	a, b := tk.OffsetStart(), tk.OffsetEnd()
	text := " " + insertions[int(ins)%len(insertions)] + " "
	src := f.src
	switch op % 4 {
	case 0:
		return src[:a] + " " + src[b:], a, 1 - (b - a), true
	case 1:
		return src[:a] + text + src[a:], a, len(text), true
	case 2:
		return src[:b] + " " + src[a:b] + src[b:], b, 1 + b - a, true
	default:
		return src[:a] + text + src[b:], a, len(text) - (b - a), true
	}
}

// topDecl is a top-level declaration as the edit fuzzers compare them:
// what it declares, "func f" or "type T", or "?" for a bad one, and its
// span.
type topDecl struct {
	name string
	span ast.Span
}

// topDecls returns tree's top-level declarations.
func topDecls(tree *ast.File) []topDecl {
	out := make([]topDecl, 0, len(tree.Decls))
	for _, d := range tree.Decls {
		name := "?"
		switch x := d.(type) {
		case *ast.Func:
			name = "func " + x.Name
		case *ast.TypeDecl:
			name = "type " + x.Name
		case *ast.Var:
			name = "var " + x.Name
		}
		span := d.Spanned()
		span.File = "" // trees parsed under different IDs compare by offsets
		out = append(out, topDecl{name, span})
	}
	return out
}

// declAt returns the index of the declaration of decls an edit at the
// offset off damages: the last one that starts at or before it, since an
// edit past a declaration's end, on its line, damages it too, or the
// first one when off comes before them all.
func declAt(decls []topDecl, off int) int {
	k := 0
	for j, d := range decls {
		if d.span.Start <= off {
			k = j
		}
	}
	return k
}

// maxEditErrors is the most errors one edit of one token may give: the
// error it causes, and at most two more its recovery cannot tell apart
// from errors of their own.
const maxEditErrors = 3

// FuzzOneTokenEdit damages one token of a valid program from examples/ or
// std, the one its key picks (see pickFile), and checks recovery: the parse ends without panicking (see
// checkParse); the first error is the one a parse that stops at the first
// gives; every bad node has an error at it or at the token after it; and
// the tree renders as JSON, as --check --ast does. For an edit that keeps
// the braces balanced, also: every error is in the declaration holding the
// edit or the one after it, at most maxEditErrors of them, and every
// declaration before the damaged one, and every one after the next, is in
// the tree unchanged.
func FuzzOneTokenEdit(f *testing.F) {
	files := editCorpus()
	for i := range 40 {
		f.Add(files[i*len(files)/40].key, uint16(i*37), uint8(i%4), uint8(i))
	}
	f.Fuzz(checkOneEdit)
}

// checkOneEdit checks FuzzOneTokenEdit's properties for the edit of the
// file key picks (see pickFile) that edit describes.
func checkOneEdit(t *testing.T, key uint32, ti uint16, op, ins uint8) {
	file := pickFile(key)
	logFile(t, file)
	src, at, delta, ok := edit(file, int(ti), op, ins)
	if !ok {
		return
	}
	tree := checkParse(t, src)
	if tree == nil {
		return
	}
	if _, err := json.Marshal(tree); err != nil {
		t.Fatalf("the tree does not render as JSON: %v", err)
	}
	_, _, diags := parseLimited(src, 0)
	if len(diags) == 0 {
		return
	}
	checkFirstError(t, src, diags)
	checkBadNodes(t, tree, diags, src)
	if braceBalance(src) != braceBalance(file.src) || madeDeclLine(src, diags, at) ||
		inImports(file.tree, at) {
		// A brace left out or one too many misaligns the blocks after
		// it, and a body line edited into a declaration's, pub var x
		// int64 = 1, ends the body there: only indentation would put
		// either right. And an import whose name an edit changes, or
		// loses, makes each literal of its package, geo.Rect{...}, an
		// error where it is. The checks above hold, but how far their
		// errors reach is not bounded.
		return
	}
	orig := topDecls(file.tree)
	k := declAt(orig, at)
	late := missingBraceAt(diags, at)
	checkErrorsNear(t, src, diags, orig, k, at, delta, late)
	checkOtherDecls(t, src, tree, orig, k, at, delta, late)
}

// logFile logs which file an edit fuzzer damages, and its own key, which
// a seed committed for a failure should use, so that it names the file
// whatever files are added (see pickFile). A test's log is shown only when
// it fails.
func logFile(t *testing.T, file editFile) {
	t.Helper()
	t.Logf("damaging %s, whose key is uint32(%d)", file.path, file.key)
}

// missingBraceAt returns where diags report a block's closing brace
// missing, past the edit at offset at, or -1. A brace an edit took away is
// found missing only at the next line a declaration alone starts: what
// comes before, var and mut declarations included, reads as the block's
// own statements, which is all a parse can know without reading
// indentation. The error there is the edit's, and the declarations before
// it are in the block.
func missingBraceAt(diags []diag.Diagnostic, at int) int {
	for _, d := range diags {
		if d.Span.Start > at && strings.HasPrefix(d.Message, "expected '}'") {
			return d.Span.Start
		}
	}
	return -1
}

// inImports reports whether the offset at is in the imports of tree, the
// program before the edit.
func inImports(tree *ast.File, at int) bool {
	n := len(tree.Imports)
	return n > 0 && at <= tree.Imports[n-1].Span.End
}

// madeDeclLine reports whether diags, the errors of src, start with a
// block's closing brace found missing at a line only a declaration starts,
// on the line of the edit at offset at: the edit made a body's line read
// as a declaration, which ends the body there.
func madeDeclLine(src string, diags []diag.Diagnostic, at int) bool {
	d := diags[0]
	if lineOf(src, d.Span.Start) != lineOf(src, at) ||
		!strings.HasPrefix(d.Message, "expected '}' found '") {
		return false
	}
	for _, kw := range []string{"pub", "lib", "func", "type", "extern", "import"} {
		if strings.HasSuffix(d.Message, "'"+kw+"'") {
			return true
		}
	}
	return false
}

// checkFirstError checks that diags, a parse's errors, start with the
// error a parse that stops at the first error gives.
func checkFirstError(t *testing.T, src string, diags []diag.Diagnostic) {
	t.Helper()
	_, _, first := parseLimited(src, 1)
	if len(first) == 0 || first[0].Span != diags[0].Span || first[0].Message != diags[0].Message {
		t.Errorf(
			"the first error is %v, but a parse stopping there gives %v\n%s",
			diags[0],
			first,
			src,
		)
	}
}

// checkBadNodes checks that every bad node of tree has an error of diags
// starting from its start to the first token after its end, comments and
// semicolons aside, or the end of the file: where the parser reports what
// is wrong with it, at the token it found instead of what it expected.
func checkBadNodes(t *testing.T, tree *ast.File, diags []diag.Diagnostic, src string) {
	t.Helper()
	toks, _, _ := parseLimited(src, 0)
	for _, d := range tree.Decls {
		ast.Walk(d, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.BadDecl, *ast.BadStmt:
			default:
				return true
			}
			span := node.Spanned()
			end := nextTokenAt(toks, span.End, len(src))
			if !slices.ContainsFunc(diags, func(e diag.Diagnostic) bool {
				return e.Span.Start >= span.Start && e.Span.Start <= end
			}) {
				t.Errorf("the bad node at lines %d to %d has no error: %v\n%s",
					lineOf(src, span.Start), lineOf(src, span.End), errorLines(src, diags), src)
			}
			return true
		})
	}
}

// nextTokenAt returns where the first of toks at or after the offset off
// starts, comments and semicolons aside, or end when there is none.
func nextTokenAt(toks []lexer.Token, off, end int) int {
	for _, tk := range toks {
		if tk.OffsetStart() >= off && tk.Kind() != lexer.Semi &&
			tk.Kind() != lexer.Comment && tk.OffsetEnd() > tk.OffsetStart() {
			return tk.OffsetStart()
		}
	}
	return end
}

// checkErrorsNear checks that every one of diags, the errors of src, the
// edited program, is in orig[k], the declaration the edit at offset at
// is in, or the one after it, or at late, where a missing brace is found
// (see missingBraceAt), and that there are at most maxEditErrors.
func checkErrorsNear(
	t *testing.T,
	src string,
	diags []diag.Diagnostic,
	orig []topDecl,
	k, at, delta, late int,
) {
	t.Helper()
	lo, hi := at, len(src)
	if k < len(orig) {
		lo = min(lo, orig[k].span.Start)
	}
	if k+1 < len(orig) {
		hi = max(orig[k+1].span.End+delta, late)
	}
	for _, d := range diags {
		if d.Span.Start < lo || d.Span.Start > hi {
			t.Errorf("an error on line %d is far from the edit on line %d: %s\n%s",
				lineOf(src, d.Span.Start), lineOf(src, at), d.Message, src)
		}
	}
	if len(diags) > maxEditErrors {
		t.Errorf("%d errors for an edit on line %d: %v\n%s",
			len(diags), lineOf(src, at), errorLines(src, diags), src)
	}
}

// checkOtherDecls checks that every declaration of orig before orig[k],
// the one the edit at offset at is in, and every one after the next, is
// in tree as it was, those after it delta bytes further on, but those
// before late, where a missing brace is found, which are in the block
// (see missingBraceAt).
func checkOtherDecls(
	t *testing.T,
	src string,
	tree *ast.File,
	orig []topDecl,
	k, at, delta, late int,
) {
	t.Helper()
	got := make(map[topDecl]bool)
	for _, d := range topDecls(tree) {
		got[d] = true
	}
	for j, d := range orig {
		switch {
		case d.name == "?":
			continue
		case j < k && d.span.End <= at:
		case j > k+1 && d.span.Start+delta > late:
			d.span.Start += delta
			d.span.End += delta
		default:
			continue
		}
		if !got[d] {
			t.Errorf("%s, on line %d, is lost after an edit on line %d\n%s",
				d.name, lineOf(src, d.span.Start), lineOf(src, at), src)
		}
	}
}

// FuzzTwoDistantEdits damages two declarations of a valid program, the
// one its key picks (see pickFile), with at least two between them, and checks that when each edit alone gives
// an error in its own declaration, both together still do: recovery from
// the first does not lose the second.
func FuzzTwoDistantEdits(f *testing.F) {
	files := editCorpus()
	for i := range 20 {
		f.Add(
			files[i*len(files)/20].key,
			uint16(i*13),
			uint8(i%4),
			uint8(i),
			uint16(i*91+500),
			uint8((i+1)%4),
			uint8(i+3),
		)
	}
	f.Fuzz(func(t *testing.T, key uint32, t1 uint16, op1, in1 uint8, t2 uint16, op2, in2 uint8) {
		file := pickFile(key)
		logFile(t, file)
		n := len(editTokens(file))
		if n == 0 {
			return
		}
		e1 := tokenEdit{i: int(t1) % n, op: op1, ins: in1}
		e2 := tokenEdit{i: int(t2) % n, op: op2, ins: in2}
		if e1.i > e2.i {
			e1, e2 = e2, e1
		}
		checkTwoEdits(t, file, e1, e2)
	})
}

// tokenEdit is one edit the edit fuzzers make (see edit): of the i'th
// token covering bytes, by op, with insertions[ins].
type tokenEdit struct {
	i       int
	op, ins uint8
}

// checkTwoEdits checks, for e1 and e2, edits of file's tokens, e1 the
// earlier, that when each alone gives an error in its own declaration,
// with at least two declarations between them, both together still do.
// After a brace left unclosed or one too many by e1, what follows reads
// as the block's or as declarations, which only indentation would tell
// apart, and a declaration line damaged by e2 is read as part of the
// block, so such an e1 is left out.
func checkTwoEdits(t *testing.T, file editFile, e1, e2 tokenEdit) {
	t.Helper()
	s1, a1, _, _ := edit(file, e1.i, e1.op, e1.ins)
	s2, a2, _, _ := edit(file, e2.i, e2.op, e2.ins)
	orig := topDecls(file.tree)
	if len(orig) == 0 || braceBalance(s1) != braceBalance(file.src) {
		return
	}
	k1, k2 := declAt(orig, a1), declAt(orig, a2)
	d1, ok1 := errorsAlone(s1, orig[k1])
	d2, ok2 := errorsAlone(s2, orig[k2])
	if k2-k1 < 3 || !ok1 || !ok2 {
		return
	}
	both, delta1, ok := bothEdits(s2, e1, a1)
	if !ok || checkParse(t, both) == nil {
		return
	}
	_, _, db := parseLimited(both, 0)
	if !errorIn(db, orig[k1], 0) || !errorIn(db, orig[k2], delta1) {
		t.Errorf(
			"with edits on lines %d and %d, an error is lost: alone %v and %v, together %v\n%s",
			lineOf(both, a1),
			lineOf(both, a2+delta1),
			errorLines(s1, d1),
			errorLines(s2, d2),
			errorLines(both, db),
			both,
		)
	}
}

// bothEdits makes e1, an edit at the offset at of the program s2 was
// before its own edit, a later one, in s2, and returns the program with
// both, how many bytes longer e1 made it, and whether e1's token is still
// where it was. The later edit is made first so that it is.
func bothEdits(s2 string, e1 tokenEdit, at int) (string, int, bool) {
	toks2, _, _ := parseLimited(s2, 0)
	both, b1, delta1, ok := edit(editFile{src: s2, tokens: toks2}, e1.i, e1.op, e1.ins)
	return both, delta1, ok && b1 == at
}

// errorsAlone returns the errors of src, a program with one edit, and
// whether it parses to a tree with an error in d, the declaration the
// edit is in.
func errorsAlone(src string, d topDecl) ([]diag.Diagnostic, bool) {
	_, tree, diags := parseLimited(src, 0)
	return diags, tree != nil && errorIn(diags, d, 0)
}

// braceBalance returns how many more opening braces than closing ones
// src's tokens hold.
func braceBalance(src string) int {
	toks, _, _ := parseLimited(src, 0)
	n := 0
	for _, tk := range toks {
		switch tk.Kind() {
		case lexer.LCurl:
			n++
		case lexer.RCurl:
			n--
		}
	}
	return n
}

// errorIn reports whether one of diags starts in d, shifted by shift
// bytes, or within a short distance after it, where an error that a
// declaration's end gives is put.
func errorIn(diags []diag.Diagnostic, d topDecl, shift int) bool {
	return slices.ContainsFunc(diags, func(e diag.Diagnostic) bool {
		return e.Span.Start >= d.span.Start+shift && e.Span.Start <= d.span.End+shift+40
	})
}

// errorLines renders diags, errors in src, as "line: message".
func errorLines(src string, diags []diag.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%d: %s", lineOf(src, d.Span.Start), d.Message))
	}
	return out
}

// TestEditCorpus checks the edit fuzzers have programs to damage.
func TestEditCorpus(t *testing.T) {
	if n := len(editCorpus()); n < 100 {
		t.Fatalf("only %d programs parse cleanly", n)
	}
	if strings.Contains(editCorpus()[0].src, "\x00") {
		t.Fatal("a program holds a NUL byte")
	}
}

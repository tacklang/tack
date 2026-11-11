package parser_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// outcome is what parsing one source produced, reduced to what a comment
// must not change: the tree without its spans and comments, or the
// diagnostic messages without their spans.
type outcome struct {
	tree     any
	messages []string
	lexFails bool
}

// parseOutcome lexes and parses src and reduces the result to an outcome.
// It fails the test if either stage panics.
func parseOutcome(t *testing.T, src string) (out outcome) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on %q: %v", src, r)
		}
	}()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return outcome{lexFails: true}
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		for _, d := range diags {
			out.messages = append(out.messages, d.Message)
		}
		return out
	}
	js, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshaling the tree of %q: %v", src, err)
	}
	var tree any
	if err := json.Unmarshal(js, &tree); err != nil {
		t.Fatal(err)
	}
	out.tree = withoutSpans(tree)
	return out
}

// withoutSpans returns v, a tree decoded from JSON, with every span and the
// file's comments left out, since inserting text moves the first and adds
// to the second.
func withoutSpans(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			if strings.HasSuffix(k, "Span") || k == "Comments" {
				continue
			}
			m[k] = withoutSpans(x)
		}
		return m
	case []any:
		s := make([]any, len(v))
		for i, x := range v {
			s[i] = withoutSpans(x)
		}
		return s
	default:
		return v
	}
}

// boundaries returns every offset in src where one token ends, plus the
// start of the file: the places a comment could be written.
func boundaries(src string) []int {
	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil
	}
	offs := []int{0}
	for _, tk := range lex.Tokens() {
		if tk.OffsetEnd() > tk.OffsetStart() {
			offs = append(offs, tk.OffsetEnd())
		}
	}
	return offs
}

// checkCommentActsAsNewline checks that writing a line comment at offset at
// in src parses exactly as writing a newline there does: both succeed with
// the same tree, or both fail with the same messages. A line comment ends
// at a newline, so it can stand wherever a newline can.
func checkCommentActsAsNewline(t *testing.T, src string, at int) {
	t.Helper()

	// The space keeps the comment apart from a / that ends the token
	// before it, which would otherwise turn into a comment too.
	withNewline := parseOutcome(t, src[:at]+" \n"+src[at:])
	withComment := parseOutcome(t, src[:at]+" // c\n"+src[at:])
	if withNewline.lexFails || withComment.lexFails {
		return
	}
	if !reflect.DeepEqual(withNewline, withComment) {
		t.Errorf("a comment at offset %d parses differently from a newline there\n"+
			"newline: %v\ncomment: %v\nsource:\n%s",
			at, withNewline.messages, withComment.messages, src[:at]+" // c\n"+src[at:])
	}
}

// examples returns the source of every example file, valid and invalid,
// keyed by its path for a valid one, by invalid/<case> for an invalid one,
// and by invalid/<case>/<path> for each Tack file of an invalid case of
// several files.
func examples(t testing.TB) map[string]string {
	t.Helper()
	paths, _ := filepath.Glob("../examples/*/*.tck")
	srcs := make(map[string]string)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		srcs[path] = string(src)
	}
	cases, err := invalid.Load(invalid.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		srcs["invalid/"+c.Name] = string(c.Source)
	}
	trees, err := invalid.LoadTree(invalid.TreePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range trees {
		for _, f := range c.Files {
			if filepath.Ext(f.Path) == ".tck" {
				srcs["invalid/"+c.Name+"/"+f.Path] = string(f.Data)
			}
		}
	}
	return srcs
}

// sweepComment and sweepNewline are what TestCommentActsAsNewlineEverywhere
// writes at a boundary. The newline is padded with spaces to the comment's
// length, so both variants of a source put every later token at the same
// offset and their trees can be compared directly, spans included, without
// the JSON round trip that parseOutcome needs to drop spans.
const (
	sweepComment = " // c\n"
	sweepNewline = "     \n"
)

// parsed is what lexing and parsing one source produced, with the file's
// comments dropped: the tree and the diagnostics, or only that lexing
// failed.
type parsed struct {
	file     *ast.File
	diags    []diag.Diagnostic
	lexFails bool
}

// parseDirect lexes and parses src and drops the file's comments, the one
// thing a comment is meant to change. It fails the test if either stage
// panics.
func parseDirect(t *testing.T, src string) (out parsed) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on %q: %v", src, r)
		}
	}()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return parsed{lexFails: true}
	}
	out.file, out.diags = parser.NewParser(f.ID, lex.Tokens()).Parse()
	if out.file != nil {
		out.file.Comments = nil
	}
	return out
}

// checkCommentActsAsNewlineFast checks the property checkCommentActsAsNewline
// checks, at offset at in src, but compares the two parses deeply on the
// trees themselves (see sameTree). Because sweepNewline is as long as
// sweepComment, the trees' spans line up and are compared too, and so are
// the diagnostics, spans included: the semicolon a line end implies sits at
// the line end whether a comment ends the line or not, so a diagnostic
// about it does not move with the comment.
func checkCommentActsAsNewlineFast(t *testing.T, src string, at int) {
	t.Helper()

	withNewline := parseDirect(t, src[:at]+sweepNewline+src[at:])
	withComment := parseDirect(t, src[:at]+sweepComment+src[at:])
	if withNewline.lexFails || withComment.lexFails {
		return
	}
	if !sameTree(withNewline.file, withComment.file) ||
		!sameTree(withNewline.diags, withComment.diags) {
		t.Errorf("a comment at offset %d parses differently from a newline there\n"+
			"newline: %v\ncomment: %v\nsource:\n%s",
			at, withNewline.diags, withComment.diags,
			src[:at]+sweepComment+src[at:])
	}
}

// TestCommentActsAsNewlineEverywhere writes a comment at every token
// boundary of every example program, which between them hold every
// statement and expression form, and checks each parses as a newline there
// does. The work is one lex and parse of the whole file per boundary, so it
// grows with the square of an example's size. Each example's boundaries are
// split into parallel subtests of sweepShard boundaries each, to spread
// that over the available cores: the largest example alone would otherwise
// keep one core busy long after the rest of the package is done.
func TestCommentActsAsNewlineEverywhere(t *testing.T) {
	for path, src := range examples(t) {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			offs := boundaries(src)
			for from := 0; from < len(offs); from += sweepShard {
				shard := offs[from:min(from+sweepShard, len(offs))]
				t.Run(fmt.Sprintf("b%d", from), func(t *testing.T) {
					t.Parallel()
					for _, at := range shard {
						checkCommentActsAsNewlineFast(t, src, at)
					}
				})
			}
		})
	}
}

// sweepShard is how many boundaries of one example a subtest of
// TestCommentActsAsNewlineEverywhere checks.
const sweepShard = 500

// FuzzCommentActsAsNewline writes a comment at a token boundary of
// arbitrary source and checks it parses as a newline there does.
func FuzzCommentActsAsNewline(f *testing.F) {
	for _, src := range examples(f) {
		f.Add(src, uint(0))
		f.Add(src, uint(len(src)))
	}
	f.Fuzz(func(t *testing.T, src string, pick uint) {
		offs := boundaries(src)
		if len(offs) == 0 {
			return
		}
		checkCommentActsAsNewline(t, src, offs[pick%uint(len(offs))])
	})
}

// commentPlacements are programs with comments where only a token boundary
// allows them, each with the comment values the file must collect, in
// source order.
var commentPlacements = []struct {
	name string
	src  string
	want []string
}{
	{"before package", "// a\npackage main\nfunc main() {}\n", []string{"a"}},
	{"after package", "package main // a\nfunc main() {}\n", []string{"a"}},
	{"only comment after package", "package main\n// a", []string{"a"}},
	{"after opening brace", "package main\nfunc main() { // a\n}\n", []string{"a"}},
	{"alone in a body", "package main\nfunc main() {\n    // a\n}\n", []string{"a"}},
	{"after a statement", "package main\nfunc main() {\n    exit(0) // a\n}\n", []string{"a"}},
	{
		"in an expression",
		"package main\nfunc main() {\n    x := 1 + // a\n        2\n    exit(x)\n}\n",
		[]string{"a"},
	},
	{
		"in an array literal",
		"package main\nfunc main() {\n    a := [2]int64{ // a\n        1, // b\n        // c\n        2,\n    }\n    exit(a[0])\n}\n",
		[]string{"a", "b\nc"},
	},
	{
		"in an argument list",
		"package main\nfunc main() {\n    exit( // a\n        0, // b\n        0)\n}\n",
		[]string{"a", "b"},
	},
	{
		"in a parameter list",
		"package main\nfunc f(a int64, // a\n    b int64) {}\nfunc main() {}\n",
		[]string{"a"},
	},
	{
		"after else",
		"package main\nfunc main() {\n    if true { // a\n    } else { // b\n    } // c\n}\n",
		[]string{"a", "b", "c"},
	},
	{"at the end of the file", "package main\nfunc main() {\n} // a", []string{"a"}},
	{"merged lines in a body", "package main\nfunc main() {\n// a\n// b\n}\n", []string{"a\nb"}},
	{
		"lines split by a blank line",
		"package main\nfunc main() {\n    exit(0) // a\n\n// b\n// c\n\n// d\n}\n",
		[]string{"a", "b\nc", "d"},
	},
	{
		"indented lines in a body",
		"package main\nfunc main() {\n    // a\n\t// b\n    exit(0) // c\n      // d\n    \n    // e\n}\n",
		[]string{"a\nb", "c\nd", "e"},
	},
}

// TestCommentsAreCollectedWherever checks a comment is accepted at each
// placement and kept, in order, in the file's comments.
func TestCommentsAreCollectedWherever(t *testing.T) {
	for _, tt := range commentPlacements {
		t.Run(tt.name, func(t *testing.T) {
			file, diags := parse(t, tt.src)
			if len(diags) > 0 {
				t.Fatalf("rejected: %v", diagMessages(diags))
			}
			got := make([]string, len(file.Comments))
			for i, c := range file.Comments {
				got[i] = c.Value
				if text := tt.src[c.Span.Start:c.Span.End]; !strings.HasPrefix(text, "//") {
					t.Errorf("comment %q has span %v covering %q", c.Value, c.Span, text)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("comments = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCommentDoesNotWidenSpans checks a comment after a statement is not
// part of the statement's span or its function's, nor is the space before
// it: each span ends at its last token.
func TestCommentDoesNotWidenSpans(t *testing.T) {
	src := "package main\nfunc main() {\n    exit(0) // a\n} // b\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("rejected: %v", diagMessages(diags))
	}
	fn, ok := file.Decls[0].(*ast.Func)
	if !ok {
		t.Fatalf("declaration is %T, want *ast.Func", file.Decls[0])
	}
	if got := src[fn.Span.Start:fn.Span.End]; !strings.HasSuffix(got, "}") {
		t.Errorf("function span covers %q, want it to end at the closing brace", got)
	}
	stmt := fn.Body[0]
	if got := src[stmt.Spanned().Start:stmt.Spanned().End]; got != "exit(0)" {
		t.Errorf("statement span covers %q, want %q", got, "exit(0)")
	}
	call := fn.Body[0].(*ast.ExprStmt).X
	if got := src[call.Spanned().Start:call.Spanned().End]; got != "exit(0)" {
		t.Errorf("call span covers %q, want %q", got, "exit(0)")
	}
}

// impliedSemicolonCases are sources whose error is about the semicolon a
// line end implies, each written with a comment ending that line and with
// spaces as long as the comment in its place, so both put the line end at
// the same offset.
var impliedSemicolonCases = []struct {
	name, withComment, withSpaces string
}{
	{
		"line comment",
		"package main\nfunc main() {\n    exit(42 // c\n    )\n}\n",
		"package main\nfunc main() {\n    exit(42     \n    )\n}\n",
	},
	{
		"merged line comments",
		"package main\nfunc main() {\n    exit(42 // c\n    // d\n    )\n}\n",
		"package main\nfunc main() {\n    exit(42     \n    // d\n    )\n}\n",
	},
	{
		"crlf line end",
		"package main\r\nfunc main() {\r\n    exit(42 // c\r\n    )\r\n}\r\n",
		"package main\r\nfunc main() {\r\n    exit(42     \r\n    )\r\n}\r\n",
	},
	{
		"end of input",
		"package main\nfunc main() {\n    exit(42 // c",
		"package main\nfunc main() {\n    exit(42     ",
	},
}

// TestImpliedSemicolonAfterCommentIsAtLineEnd checks an error about the
// semicolon a line end implies sits where it does with no comment ending
// the line: at the line end, not at the comment.
func TestImpliedSemicolonAfterCommentIsAtLineEnd(t *testing.T) {
	for _, tt := range impliedSemicolonCases {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDirect(t, tt.withComment)
			want := parseDirect(t, tt.withSpaces)
			if got.lexFails || want.lexFails {
				t.Fatalf("lexing failed: with comment %v, with spaces %v", got.lexFails, want.lexFails)
			}
			if len(want.diags) == 0 {
				t.Fatalf("the source without the comment is accepted; the case tests nothing")
			}
			if !reflect.DeepEqual(got.diags, want.diags) {
				t.Errorf("with a comment: %v\nwithout: %v", got.diags, want.diags)
			}
		})
	}
}

// diagMessages returns the message of each diagnostic.
func diagMessages(diags []diag.Diagnostic) []string {
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.Message
	}
	return msgs
}

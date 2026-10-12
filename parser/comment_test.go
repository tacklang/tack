package parser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
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

// examples returns the source of every program under examples/, valid and
// invalid.
func examples(t testing.TB) map[string]string {
	t.Helper()
	paths, _ := filepath.Glob("../examples/*/main.tck")
	invalid, _ := filepath.Glob("../examples/invalid/*/main.tck")
	srcs := make(map[string]string)
	for _, path := range append(paths, invalid...) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		srcs[path] = string(src)
	}
	return srcs
}

// TestCommentActsAsNewlineEverywhere writes a comment at every token
// boundary of every example program, which between them hold every
// statement and expression form, and checks each parses as a newline there
// does.
func TestCommentActsAsNewlineEverywhere(t *testing.T) {
	for path, src := range examples(t) {
		t.Run(path, func(t *testing.T) {
			for _, at := range boundaries(src) {
				checkCommentActsAsNewline(t, src, at)
			}
		})
	}
}

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
		[]string{"a", "b", "c"},
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

// diagMessages returns the message of each diagnostic.
func diagMessages(diags []diag.Diagnostic) []string {
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.Message
	}
	return msgs
}

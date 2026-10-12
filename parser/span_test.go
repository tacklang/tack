package parser_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// trailingSpaceSources are programs with spaces and tabs after the last
// token of statements and declarations of every kind, before a newline, a
// comment, a closing brace and the end of the file.
var trailingSpaceSources = map[string]string{
	"before newlines": "package main  \nvar x int64 = 1 \t\nfunc main() {\n    exit(0)   \n}   \n",
	"before comments": "package main // p\n" +
		"var x int64 = 1 \t// v\n" +
		"func f() int64 {\n" +
		"    mut y := 1  // a\n" +
		"    y++ // b\n" +
		"    y = y + 1\t// c\n" +
		"    if y == 3 { // d\n" +
		"        return y  // e\n" +
		"    } else {\n" +
		"    }  // f\n" +
		"    for {\n" +
		"        break  // g\n" +
		"    }  // h\n" +
		"    return 0   // i\n" +
		"}  // j\n" +
		"func main() {\n    exit(f())  // k\n}  // l",
	"before a closing brace": "package main\nfunc main() { exit(0)  }\n",
	"before the end":         "package main\nfunc main() {\n    exit(0)\n}  ",
	"before an explicit semicolon": "package main\nfunc main() {\n" +
		"    x := 1 ;\n    exit(x)\n}\n",
}

// semicolonSources are programs that end every statement and declaration
// of every kind with a semicolon written in the source: a package clause,
// package variables, functions with and without a body, every simple
// statement, if with and without else and in an else-if chain, every form of
// for, break, continue, and return with and without values. The semicolons
// sit right after the last token, after spaces, before a comment and before
// a closing brace on the same line.
var semicolonSources = map[string]string{
	"explicit semicolons": "package main;\n" +
		"var g int64 = 1;\n" +
		"mut h int64;\n" +
		"func ext() int64;\n" +
		"func pair() (int64, int64) { return 1, 2; };\n" +
		"func f(xs [2]int64) int64 {\n" +
		"    var a int64 = 1;\n" +
		"    mut b := 2;\n" +
		"    c := 3;\n" +
		"    d, e := pair();\n" +
		"    d, e = pair();\n" +
		"    b = b + a;\n" +
		"    b += c;\n" +
		"    b++;\n" +
		"    xs[0] = d + e;\n" +
		"    exit(0);\n" +
		"    if b > 0 { b--; };\n" +
		"    if b > 0 { b--; } else { b++; };\n" +
		"    if b > 0 { b--; } else if b < 0 { b++; } else { b = 0; };\n" +
		"    for { break; };\n" +
		"    for b > 100 { continue; };\n" +
		"    for mut i := 0; i < 2; i++ { b++; };\n" +
		"    for _, x := range xs { b += x; };\n" +
		"    for range xs { b++; };\n" +
		"    return b;\n" +
		"};\n" +
		"func g2() { return; };\n" +
		"func main() { exit(f([2]int64{1, 2})); }\n",
	"semicolons after spaces": "package main ;\n" +
		"var g int64 = 1  ;\n" +
		"func f() int64 {\n" +
		"    mut b := 2 ;\n" +
		"    b++\t;\n" +
		"    if b > 0 { b-- ; } else { b++ ; }  ;\n" +
		"    for { break ; } ;\n" +
		"    return b ;\n" +
		"} ;\n" +
		"func main() { exit(f()) ; }\n",
	"semicolons before comments": "package main; // p\n" +
		"var g int64 = 1; // v\n" +
		"func f() int64 {\n" +
		"    mut b := 2; // a\n" +
		"    if b > 0 { // b\n" +
		"        b--; // c\n" +
		"    }; // d\n" +
		"    for { // e\n" +
		"        break; // f\n" +
		"    }; // g\n" +
		"    return b; // h\n" +
		"}; // i\n" +
		"func main() { exit(f()); } // j",
	"semicolons on one line": "package main; func main() { x := 1; exit(x); };",
}

// TestSpansEndAtTheLastToken checks every span in the tree of each program
// starts at its first token and ends right after its last one, never
// covering the spaces around them, nor the semicolon that ends a statement
// or declaration, whether it was written or inserted. An inserted semicolon
// sits after the spaces before the newline or comment, so a span that ran to
// it would cover them. Every program in trailingSpaceSources and
// semicolonSources must parse, so none is skipped unchecked.
func TestSpansEndAtTheLastToken(t *testing.T) {
	for name, src := range examples(t) {
		t.Run(name, func(t *testing.T) {
			if tree := parseTree(t, src, false); tree != nil {
				checkSpansTrimmed(t, src, "", tree)
			}
		})
	}
	for _, srcs := range []map[string]string{trailingSpaceSources, semicolonSources} {
		for name, src := range srcs {
			t.Run(name, func(t *testing.T) {
				checkSpansTrimmed(t, src, "", parseTree(t, src, true))
			})
		}
	}
}

// parseTree parses src and returns its tree decoded from JSON, without the
// file's own span, which is the whole file, spaces and all. A program that
// fails to lex or parse fails the test if mustParse is set, and otherwise
// gives a nil tree: some invalid examples have no tree to check.
func parseTree(t *testing.T, src string, mustParse bool) any {
	t.Helper()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	diags := lex.Lex()
	var file *ast.File
	if len(diags) == 0 {
		file, diags = parser.NewParser(f.ID, lex.Tokens()).Parse()
	}
	if len(diags) > 0 {
		if mustParse {
			t.Fatalf("%q does not parse: %v", src, diags)
		}
		return nil
	}

	js, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(js, &tree); err != nil {
		t.Fatal(err)
	}
	delete(tree, "Span")
	return tree
}

// checkSpansTrimmed fails the test for every span in v, a tree decoded from
// JSON, whose text in src starts or ends with white space or ends with a
// semicolon. path names where
// in the tree v is, for the failure message.
func checkSpansTrimmed(t *testing.T, src, path string, v any) {
	t.Helper()

	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if span, ok := x.(map[string]any); ok && strings.HasSuffix(k, "Span") {
				checkSpanTrimmed(t, src, path+"."+k, span)
				continue
			}
			checkSpansTrimmed(t, src, path+"."+k, x)
		}
	case []any:
		for _, x := range v {
			checkSpansTrimmed(t, src, path+"[]", x)
		}
	}
}

// checkSpanTrimmed fails the test if span, decoded from JSON, covers white
// space at either end of its text in src, or a semicolon at its end.
func checkSpanTrimmed(t *testing.T, src, path string, span map[string]any) {
	t.Helper()

	start, _ := span["Start"].(float64)
	end, _ := span["End"].(float64)
	text := src[int(start):int(end)]
	if text != strings.TrimSpace(text) {
		t.Errorf("%s covers %q, with white space at an end", path, text)
	}
	if strings.HasSuffix(text, ";") {
		t.Errorf("%s covers %q, with a semicolon at the end", path, text)
	}
}

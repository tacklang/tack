package parser_test

import (
	"encoding/json"
	"strings"
	"testing"

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

// TestSpansEndAtTheLastToken checks every span in the tree of each program
// starts at its first token and ends right after its last one, never
// covering the spaces around them. A semicolon inserted at a line end sits
// after the spaces before the newline or comment, so a span that ran to it
// would cover them.
func TestSpansEndAtTheLastToken(t *testing.T) {
	srcs := examples(t)
	for name, src := range trailingSpaceSources {
		srcs[name] = src
	}
	for name, src := range srcs {
		t.Run(name, func(t *testing.T) {
			f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
			lex := lexer.New(f)
			if diags := lex.Lex(); len(diags) > 0 {
				return // some invalid examples have no tokens to parse
			}
			file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
			if len(diags) > 0 {
				return // nor a tree to check
			}
			js, err := json.Marshal(file)
			if err != nil {
				t.Fatal(err)
			}
			var tree any
			if err := json.Unmarshal(js, &tree); err != nil {
				t.Fatal(err)
			}
			// The file's own span is the whole file, spaces and all.
			delete(tree.(map[string]any), "Span")
			checkSpansTrimmed(t, src, "", tree)
		})
	}
}

// checkSpansTrimmed fails the test for every span in v, a tree decoded from
// JSON, whose text in src starts or ends with white space. path names where
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
// space at either end of its text in src.
func checkSpanTrimmed(t *testing.T, src, path string, span map[string]any) {
	t.Helper()

	start, _ := span["Start"].(float64)
	end, _ := span["End"].(float64)
	text := src[int(start):int(end)]
	if text != strings.TrimSpace(text) {
		t.Errorf("%s covers %q, with white space at an end", path, text)
	}
}

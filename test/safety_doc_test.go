package compilertest

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// safetyDoc is the page whose examples TestSafetyDoc ties to tested programs.
const safetyDoc = "../docs/safety.md"

// safetyCasePrefix starts the name of every invalid case written for the
// safety page. Each such case must be named by an annotation on the page, so
// a case cannot outlive the example it was written for.
const safetyCasePrefix = "safety-"

// htmlComment matches any HTML comment on the page. Every one of them must
// be an annotation, so a misspelled one fails instead of being skipped.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// annotation matches one annotation and what directly follows it: either a
// fenced tack block on the next line (group 3) or, after nothing but white
// space, a code span (group 4). Group 1 is the kind and group 2 the name it
// refers to. White space, a line break included, may separate a comment from
// its code span because a comment that starts a line in Markdown is an HTML
// block, which would swallow a code span on the same line.
var annotation = regexp.MustCompile("<!-- (case|error|note|example): ([^ ]+) -->" +
	"(?:\n```tack\n((?s:.*?\n))```|\\s*`([^`]+)`)?")

// docAnnotation is one annotation on the safety page.
type docAnnotation struct {
	kind  string // case, error, note, or example
	name  string // an invalid case, or a directory under examples/
	block string // the fenced tack block that follows, if any
	span  string // the code span that follows, if any
}

// TestSafetyDoc checks that docs/safety.md agrees with the programs that test
// it. The page ties each example and quoted diagnostic to a program with an
// HTML comment written directly before it:
//
//   - <!-- case: NAME --> names a case in examples/invalid.txtar. Before a
//     fenced tack block, the block's lines must appear, consecutively and
//     ignoring indentation, in the case's program. Elsewhere it marks a
//     failing snippet written inline, and NAME must exist.
//   - <!-- error: NAME --> is followed by a code span holding a diagnostic,
//     which must be one of the diagnostics in NAME's expected output, whole.
//   - <!-- note: NAME --> is the same for a note on one of them.
//   - <!-- example: DIR --> is followed by a fenced tack block whose lines
//     must appear in examples/DIR/main.tck, which TestBuiltSafetyPassesEveryCheck in
//     tack/ runs.
//
// TestInvalid already checks that every case is rejected and pins its
// output, so together they show each failing example fails as the page says.
func TestSafetyDoc(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(safetyDoc))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	cases, err := invalid.Load(filepath.FromSlash(invalid.Path))
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]invalid.Case, len(cases))
	for _, c := range cases {
		byName[c.Name] = c
	}

	anns := parseAnnotations(t, doc)
	used := make(map[string]bool)
	for _, a := range anns {
		used[a.name] = true
		checkAnnotation(t, a, byName)
	}
	for _, c := range cases {
		if strings.HasPrefix(c.Name, safetyCasePrefix) && !used[c.Name] {
			t.Errorf("case %s is not named by any annotation in %s", c.Name, safetyDoc)
		}
	}
}

// parseAnnotations returns every annotation on the page, in order. It fails
// the test if the page has none or has an HTML comment that is not one.
func parseAnnotations(t *testing.T, doc string) []docAnnotation {
	t.Helper()
	for _, c := range htmlComment.FindAllString(doc, -1) {
		if loc := annotation.FindStringIndex(c); loc == nil || loc[0] != 0 {
			t.Errorf("%s: comment %q is not <!-- case|error|note|example: NAME -->", safetyDoc, c)
		}
	}
	matches := annotation.FindAllStringSubmatch(doc, -1)
	anns := make([]docAnnotation, 0, len(matches))
	for _, m := range matches {
		anns = append(anns, docAnnotation{kind: m[1], name: m[2], block: m[3], span: m[4]})
	}
	if len(anns) == 0 {
		t.Fatalf("%s has no annotations", safetyDoc)
	}
	return anns
}

// checkAnnotation checks one annotation against the invalid cases, or
// against the example program it names.
func checkAnnotation(t *testing.T, a docAnnotation, cases map[string]invalid.Case) {
	t.Helper()
	where := safetyDoc + ": <!-- " + a.kind + ": " + a.name + " -->"
	if a.kind == "example" {
		checkExample(t, where, a)
		return
	}
	c, ok := cases[a.name]
	if !ok {
		t.Errorf("%s: no such case in %s", where, invalid.Path)
		return
	}
	if a.kind == "error" || a.kind == "note" {
		checkDiagnostic(t, where, a, c)
		return
	}
	if a.block != "" && !containsLines(string(c.Source), a.block) {
		t.Errorf("%s: the case's program does not contain the block\n%s", where, a.block)
	}
}

// checkDiagnostic checks that the code span after an error or note
// annotation is a diagnostic of that kind in the case c's expected output.
func checkDiagnostic(t *testing.T, where string, a docAnnotation, c invalid.Case) {
	t.Helper()
	switch {
	case a.span == "":
		t.Errorf("%s must be followed directly by a code span", where)
	case !hasDiagnostic(string(c.Expected), a.kind, a.span):
		t.Errorf("%s: the case does not report %q; it reports:\n%s", where, a.span, c.Expected)
	}
}

// checkExample checks that the block after an example annotation appears in
// the example program it names.
func checkExample(t *testing.T, where string, a docAnnotation) {
	t.Helper()
	if a.block == "" {
		t.Errorf("%s must be followed directly by a fenced tack block", where)
		return
	}
	path := filepath.Join(examplesDir, a.name, "main.tck")
	src, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from the doc by design
	if err != nil {
		t.Errorf("%s: %v", where, err)
		return
	}
	if !containsLines(string(src), a.block) {
		t.Errorf("%s: %s does not contain the block\n%s", where, path, a.block)
	}
}

// hasDiagnostic reports whether expected, a case's rendered output, holds a
// diagnostic of severity kind, error or note, whose whole message is msg.
// Matching up to the end of the line keeps a message from matching a longer
// one that starts with it.
func hasDiagnostic(expected, kind, msg string) bool {
	return strings.Contains(expected, ": "+kind+": "+msg+"\n")
}

// containsLines reports whether the lines of snippet appear in src, in order
// and consecutively. Each line is compared without its leading and trailing
// white space, so a snippet may be indented differently from the program it
// was taken from.
func containsLines(src, snippet string) bool {
	want := trimmedLines(snippet)
	have := trimmedLines(src)
	if len(want) == 0 {
		return false
	}
	for i := 0; i+len(want) <= len(have); i++ {
		if slices.Equal(have[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// trimmedLines splits s into lines, dropping a final newline, and trims each.
func trimmedLines(s string) []string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return lines
}

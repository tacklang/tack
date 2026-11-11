package compilertest

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// errorMarker ends a line of an invalid closure case where an error may
// be reported.
const errorMarker = "// ERROR"

// oldLiteralError is what the compiler said of every function literal
// before closures; no closure case may still be rejected for that.
const oldLiteralError = "function literals are not supported"

// TestClosureCasesRejectedWhereMarked checks the invalid cases about
// closures and scope parameters, those of examples/invalid.txtar named
// closure-... and scope-..., for more than TestInvalid's pinned output
// does: each is rejected without a panic, with at least one error on a
// line its source marks with // ERROR and none on a line it does not
// mark, so a case is rejected for its own problem, not a stray one. It
// encodes an assumption the spec does not state, that an error is
// reported on the line of the place at fault: the closure, the capture or
// the scope parameter's use. A marked line may also hold several of
// them, and notes may point anywhere.
//
// Cases about scope must be rejected by name of the modifier, so that a
// parser that has never heard of scope, and fails on it with a generic
// syntax error, does not pass them.
func TestClosureCasesRejectedWhereMarked(t *testing.T) {
	path := filepath.FromSlash(invalid.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, cases, err := invalid.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	n := 0
	for _, c := range cases {
		if !strings.HasPrefix(c.Name, "closure-") && !strings.HasPrefix(c.Name, "scope-") {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			marked := markedLines(c.Source)
			if len(marked) == 0 {
				t.Fatalf("the case marks no line with %s", errorMarker)
			}
			got := runSource(t, frontEndBackend, c.Name+"/main.tck", c.Source)
			if got.stage == "ok" || got.stage == "panic" {
				t.Fatalf("stage %s, want the program rejected:\n%s", got.stage, got.output)
			}
			if strings.Contains(got.output, oldLiteralError) {
				t.Errorf("rejected as before closures:\n%s", got.output)
			}
			lines := errorLines(c.Name, got.output)
			hit := false
			for _, l := range lines {
				if marked[l] {
					hit = true
				} else {
					t.Errorf("an error on line %d, which is not marked:\n%s", l, got.output)
				}
			}
			if !hit {
				t.Errorf("no error on a marked line %v:\n%s", slices.Sorted(maps.Keys(marked)), got.output)
			}
			if strings.HasPrefix(c.Name, "scope-") && !mentionsScope(c.Name, got.output) {
				t.Errorf("no error names scope:\n%s", got.output)
			}
		})
	}
	// The count keeps a case renamed out of these prefixes, or dropped,
	// from going unnoticed.
	if n != closureCases {
		t.Errorf("found %d closure and scope cases, want %d", n, closureCases)
	}
}

// closureCases is how many closure-... and scope-... cases
// examples/invalid.txtar holds.
const closureCases = 65

// markedLines returns the 1-based numbers of the lines of src that end
// with errorMarker.
func markedLines(src []byte) map[int]bool {
	marked := make(map[int]bool)
	for i, line := range bytes.Split(src, []byte("\n")) {
		if bytes.HasSuffix(bytes.TrimRight(line, " \t\r"), []byte(errorMarker)) {
			marked[i+1] = true
		}
	}
	return marked
}

// errorLines returns the line of each error the rendered output reports
// in the case's file, in order.
func errorLines(name, output string) []int {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name+"/main.tck") + `:(\d+):\d+: error: `)
	var lines []int
	for _, m := range re.FindAllStringSubmatch(output, -1) {
		l, err := strconv.Atoi(m[1])
		if err != nil {
			panic(fmt.Sprintf("line %q: %v", m[1], err))
		}
		lines = append(lines, l)
	}
	return lines
}

// mentionsScope reports whether an error of the rendered output names
// the scope modifier in its message.
func mentionsScope(name, output string) bool {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name+"/main.tck") + `:\d+:\d+: error: .*\bscope\b`)
	return re.MatchString(output)
}

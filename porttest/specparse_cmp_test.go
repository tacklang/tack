package porttest

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// specParseFixtures is where the port keeps the files TestSpecParse
// reads with both readers, relative to its source tree's root: one
// Markdown file for each case of the dialect, of the index's form and of
// the block grammar the reader takes or rejects.
const specParseFixtures = "tools/spec/testdata/parse"

// specParseDump returns what the copy of tackc's reader (see
// specparse_test.go) finds in data, the file name, in the form the
// port's tools/spec --parse writes it: a line problem and the problem,
// quoted, for each problem parseSpecExamples finds, in its order; for
// each example, a line example <chapter>:<line> <kind> exit=<n>
// expect=<bool>, then src and its content, quoted, and expect and the
// content of the block after it, quoted; and a line link and the
// chapter for each chapter specIndexLinks counts, sorted. Each string
// is quoted as strconv.Quote quotes it.
func specParseDump(name, data string) string {
	var b strings.Builder
	examples, problems := parseSpecExamples(name, data)
	for _, p := range problems {
		b.WriteString("problem " + strconv.Quote(p) + "\n")
	}
	for _, ex := range examples {
		fmt.Fprintf(&b, "example %s %s exit=%d expect=%t\n", ex.pos, ex.kind, ex.exit, ex.hasExp)
		b.WriteString("src " + strconv.Quote(ex.src) + "\n")
		b.WriteString("expect " + strconv.Quote(ex.expect) + "\n")
	}
	for _, l := range slices.Sorted(maps.Keys(specIndexLinks(data))) {
		b.WriteString("link " + l + "\n")
	}
	return b.String()
}

// TestSpecParse checks that the port's tools/spec reads the language
// specification's Markdown as tackc's TestSpecExamples does: it builds
// tools/spec with the port and, for each file of the port's
// specParseFixtures, the port's docs/spec and this module's docs/spec,
// requires what tools/spec --parse writes to be specParseDump's dump of
// it, byte for byte, the first line that differs shown. A fixture
// directory with no Markdown file fails, so the comparison cannot be
// turned off unseen. It needs the port's source tree and the port, and
// runs only as TestPort does, with TACK_PORT_TEST=1 and -count.
func TestSpecParse(t *testing.T) {
	ready(t)
	in := input{name: "spec-parse", dir: env.home, suite: specStage}
	tool := filepath.Join(t.TempDir(), "spec")
	built := runCompiler(t, env.portBin, env.portCache, in, []string{"--out", tool, "./tools/spec"})
	if built.code != 0 {
		t.Fatalf("building tools/spec with the port: exit status %d\n%s", built.code, built.stderr)
	}
	files, fixtures := specParseFiles(t)
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // G304: a fixture or a chapter of the spec
		if err != nil {
			t.Fatal(err)
		}
		want := specParseDump(filepath.Base(f), string(data))
		ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
		cmd := exec.CommandContext(ctx, tool, "--parse", f) //nolint:gosec // G204: the tool just built
		got, err := cmd.Output()
		cancel()
		if err != nil {
			t.Errorf("%s: tools/spec --parse: %v", f, err)
			continue
		}
		if msg := firstDiffLabeled("Go", "port", []byte(want), got); msg != "" {
			t.Errorf("%s: tools/spec --parse differs from tackc's reader: %s", f, msg)
		}
	}
	t.Logf("compared %d files: %d fixtures and both trees' chapters", len(files), fixtures)
}

// specParseFiles returns the files TestSpecParse reads: every Markdown
// file of the port's specParseFixtures, then every chapter of the port's
// docs/spec and of this module's; and how many of them are fixtures. It
// fails t when the fixtures or either copy of the spec hold no Markdown
// file.
func specParseFiles(t *testing.T) ([]string, int) {
	t.Helper()
	fixtures, err := filepath.Glob(filepath.Join(env.home, filepath.FromSlash(specParseFixtures), "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("%s holds no Markdown file, so no reading would be compared", specParseFixtures)
	}
	files := fixtures
	for _, dir := range []string{env.home, env.root} {
		chapters, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(portSpecDir), "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		if len(chapters) == 0 {
			t.Fatalf("%s of %s holds no Markdown file", portSpecDir, dir)
		}
		files = append(files, chapters...)
	}
	return files, len(fixtures)
}

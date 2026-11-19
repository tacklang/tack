package porttest

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// divergencesFile is the list of the cases where the port differs from
// the Go compiler on purpose, below the module's root (see the package
// doc, Known divergences).
const divergencesFile = "porttest/testdata/divergences.txtar"

// emitProbesSuite is the suite of TestEmitProbes's programs, which run
// as emit-single runs its cases but are no case of it.
const emitProbesSuite = "emit-probes"

// divergence is one entry of the divergence list: a case, by its suite
// and its name, the Go bug the port does not copy there, why the port
// does what it does instead, and the port's output, reviewed, which the
// port is compared with in place of the Go compiler's.
type divergence struct {
	// same names the streams, of exit, stdout and stderr, that the list
	// does not record, where the port gives what the Go compiler gives
	// and is compared with it.
	same map[string]bool
	// id names the entry, the directory of its sections in the file.
	id string
	// suite is the stage whose case it is, by its name, or
	// emitProbesSuite.
	suite string
	// name is the case's name, as its subtest is named.
	name string
	// goBug says what the Go compiler gets wrong there.
	goBug string
	// why says what the port does instead and what ruled it, with its
	// date.
	why string
	// want is the port's recorded output, but for the streams same
	// names.
	want result
	// file, when it is not "", is the path, slash-separated, of a file a
	// case of emit-header writes in its copy, where the port writes
	// other bytes than the Go compiler, and fileWant what the port's
	// copy holds there, as snapshot gives it, its mode and its bytes
	// (see the entry's optional file section).
	file, fileWant string
}

// divergenceFields are the fields of an entry's divergence section, in
// the order the section writes them.
var divergenceFields = []string{"suite", "case", "go", "why", "same"}

// divergenceStreams are the parts of a run an entry records, each a
// section of it, and that its field same may name instead.
var divergenceStreams = []string{"exit", "stdout", "stderr"}

// divergenceSections are the sections of an entry, after its
// divergence section, in order.
var divergenceSections = []string{"divergence", "exit", "stdout", "stderr"}

// fileSection is the kind of the section an entry may have after its
// stderr, for a case of emit-header whose port writes a file other than
// Go's: its first line is path: and the file's path in the case's copy,
// slash-separated, its second the file's mode as snapshot writes it,
// and the rest the file's bytes, as reviewed.
const fileSection = "file"

// sectionMarker matches the line that starts a section of the file,
// -- <id>/<section> --.
var sectionMarker = regexp.MustCompile(`^-- ([^/ ]+)/([a-z]+) --$`)

// datedRuling matches a why that names the date of what ruled it.
var datedRuling = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// loadedDivergences is the divergence list read from divergencesFile,
// once a run.
var loadedDivergences = sync.OnceValues(func() ([]divergence, error) {
	data, err := os.ReadFile(filepath.Join(env.root, filepath.FromSlash(divergencesFile)))
	if err != nil {
		return nil, fmt.Errorf("reading the divergence list: %w", err)
	}
	return parseDivergences(data, knownSuites())
})

// knownSuites returns the suites an entry may name: every stage's name,
// emitProbesSuite, headerRenderSuite and runRecordsSuite. A file an emit-header case
// leaves is part of an entry only when its file section names it; every
// other is compared with the Go compiler's always.
func knownSuites() map[string]bool {
	out := map[string]bool{emitProbesSuite: true, headerRenderSuite: true, runRecordsSuite: true}
	for _, st := range stages {
		out[st.name] = true
	}
	return out
}

// parseDivergences reads the divergence list data, rejecting an entry
// that names a suite not in suites, lacks a field or a section, has them
// out of order or twice, gives an exit status that is no number, or a
// why with no date, whose same names what is no stream, every stream,
// or a stream its section records, and two entries for one case or of
// one id (see listSections and parseEntry).
func parseDivergences(data []byte, suites map[string]bool) ([]divergence, error) {
	secs, err := listSections(data)
	if err != nil {
		return nil, err
	}
	var out []divergence
	seen := map[string]bool{}
	for i := 0; i < len(secs); {
		n := len(divergenceSections)
		if i+n > len(secs) {
			return nil, fmt.Errorf("divergence %s: %d sections, not %d an entry", secs[i].id,
				len(secs)-i, n)
		}
		if i+n < len(secs) && secs[i+n].kind == fileSection && secs[i+n].id == secs[i].id {
			n++
		}
		d, err := parseEntry(secs[i:i+n], suites)
		if err != nil {
			return nil, err
		}
		i += n
		if seen["id "+d.id] {
			return nil, fmt.Errorf("divergence %s: listed twice", d.id)
		}
		if seen[d.suite+"\x00"+d.name] {
			return nil, fmt.Errorf(
				"divergence %s: case %s of %s is listed twice",
				d.id,
				d.name,
				d.suite,
			)
		}
		seen["id "+d.id], seen[d.suite+"\x00"+d.name] = true, true
		out = append(out, d)
	}
	return out, nil
}

// listSection is one section of the divergence list: the entry it is
// of, its kind, and what it holds.
type listSection struct {
	id, kind string
	body     []byte
}

// listSections splits the divergence list data into its sections, with
// nothing before the first but comment lines, starting #, and blank
// lines; parseDivergences groups them into entries.
func listSections(data []byte) ([]listSection, error) {
	var secs []listSection
	for i, line := range bytes.SplitAfter(data, []byte("\n")) {
		m := sectionMarker.FindSubmatch(bytes.TrimSuffix(line, []byte("\n")))
		switch {
		case m != nil:
			secs = append(secs, listSection{id: string(m[1]), kind: string(m[2])})
		case len(secs) > 0:
			secs[len(secs)-1].body = append(secs[len(secs)-1].body, line...)
		case len(bytes.TrimSpace(line)) > 0 && !bytes.HasPrefix(line, []byte("#")):
			return nil, fmt.Errorf(
				"divergence list line %d: %q before the first section",
				i+1,
				line,
			)
		}
	}
	return secs, nil
}

// parseEntry reads one entry of the divergence list from secs, its
// sections, which must be divergenceSections in order, all of one id.
func parseEntry(secs []listSection, suites map[string]bool) (divergence, error) {
	id := secs[0].id
	for j, kind := range divergenceSections {
		if s := secs[j]; s.id != id || s.kind != kind {
			return divergence{}, fmt.Errorf("divergence %s: section %s/%s where %s/%s comes",
				id, s.id, s.kind, id, kind)
		}
	}
	fields, err := divergenceHeader(secs[0].body)
	if err != nil {
		return divergence{}, fmt.Errorf("divergence %s: %w", id, err)
	}
	d := divergence{id: id, suite: fields[0], name: fields[1], goBug: fields[2], why: fields[3]}
	switch {
	case !suites[d.suite]:
		return divergence{}, fmt.Errorf("divergence %s: no suite %q", id, d.suite)
	case !datedRuling.MatchString(d.why):
		return divergence{}, fmt.Errorf("divergence %s: its why names no date", id)
	}
	if len(secs) > len(divergenceSections) {
		if d.suite != emitHeaderStage {
			return divergence{}, fmt.Errorf("divergence %s: a file section, which only emit-header's cases take", id)
		}
		if err := d.readFile(secs[len(divergenceSections)].body); err != nil {
			return divergence{}, fmt.Errorf("divergence %s: %w", id, err)
		}
	}
	if err := d.readOutput(fields[4], secs[1:]); err != nil {
		return divergence{}, fmt.Errorf("divergence %s: %w", id, err)
	}
	return d, nil
}

// readFile sets d's file and fileWant from body, its file section: a
// line path: and the path, then the file's mode and bytes.
func (d *divergence) readFile(body []byte) error {
	first, rest, ok := strings.Cut(string(body), "\n")
	p, named := strings.CutPrefix(first, "path: ")
	if !ok || !named || p == "" || !strings.Contains(rest, "\n") {
		return errors.New("its file section is no line path: and a path, then a mode and the bytes")
	}
	d.file, d.fileWant = p, rest
	return nil
}

// readOutput sets d's same from same, the value of its field, and its
// recorded output from secs, its exit, stdout and stderr sections, each
// of which must be empty when same names its stream.
func (d *divergence) readOutput(same string, secs []listSection) error {
	var err error
	if d.same, err = sameStreams(same, d.file != ""); err != nil {
		return err
	}
	for j, stream := range divergenceStreams {
		if d.same[stream] && len(secs[j].body) > 0 {
			return fmt.Errorf("same names %s, which its section records", stream)
		}
	}
	d.want = result{stdout: secs[1].body, stderr: secs[2].body}
	if !d.same["exit"] {
		d.want.code, err = strconv.Atoi(strings.TrimSuffix(string(secs[0].body), "\n"))
		if err != nil {
			return fmt.Errorf("exit status: %w", err)
		}
	}
	return nil
}

// divergenceHeader returns the fields of an entry's divergence section,
// body, in divergenceFields' order: each a line, its name, a colon and a
// space, then its value, which goes on over the lines after it that
// start with two spaces, joined by a space.
func divergenceHeader(body []byte) ([]string, error) {
	var names, values []string
	for line := range strings.Lines(string(body)) {
		line = strings.TrimSuffix(line, "\n")
		if more, ok := strings.CutPrefix(line, "  "); ok && len(values) > 0 {
			values[len(values)-1] += " " + strings.TrimSpace(more)
			continue
		}
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, fmt.Errorf("line %q is no field", line)
		}
		names, values = append(names, name), append(values, strings.TrimSpace(value))
	}
	if !slices.Equal(names, divergenceFields) {
		return nil, fmt.Errorf("fields %q, want %q", names, divergenceFields)
	}
	for i, v := range values {
		if v == "" {
			return nil, fmt.Errorf("field %s is empty", names[i])
		}
	}
	return values, nil
}

// sameStreams reads an entry's field same: none, or a list of streams,
// each one of divergenceStreams, once, separated by a comma and a space,
// and not all of them, which would record nothing, unless file says the
// entry records a file.
func sameStreams(value string, file bool) (map[string]bool, error) {
	out := map[string]bool{}
	if value == "none" {
		return out, nil
	}
	for _, stream := range strings.Split(value, ", ") {
		if !slices.Contains(divergenceStreams, stream) || out[stream] {
			return nil, fmt.Errorf(
				"same: %q is no list of streams, none, or of %q, each once",
				value,
				divergenceStreams,
			)
		}
		out[stream] = true
	}
	if len(out) == len(divergenceStreams) && !file {
		return nil, errors.New("same names every stream, recording nothing")
	}
	return out, nil
}

// expected returns what the port must give for d's case, given goRun,
// the Go compiler's run of it: the recorded streams, and goRun's for the
// streams same names.
func (d divergence) expected(goRun result) result {
	want := d.want
	if d.same["exit"] {
		want.code = goRun.code
	}
	if d.same["stdout"] {
		want.stdout = goRun.stdout
	}
	if d.same["stderr"] {
		want.stderr = goRun.stderr
	}
	return want
}

// divergenceFor returns the entry of the divergence list for in, run as
// the stage st runs its cases, and whether there is one (see
// matchDivergence). It fails t when the list cannot be read, and when an
// entry names in though in is no case of a suite.
func divergenceFor(t *testing.T, st stage, in input) (divergence, bool) {
	t.Helper()
	list, err := loadedDivergences()
	if err != nil {
		t.Fatal(err)
	}
	d, listed, why := matchDivergence(list, st.name, in)
	if why != "" {
		t.Fatal(why)
	}
	return d, listed
}

// matchDivergence returns the entry of list for in, run as the stage
// named stageName runs its cases, whether there is one, and why the run
// fails when an entry would apply to what is no case of it. Only a case
// of a suite has an entry: an input whose suite names one, a stage's own
// case, which TestPort and TestProbesPairStack mark with the stage's
// name, or one of TestEmitProbes's, marked emit-probes. Any other input,
// as the deep, perf and fuzz tests make, has no suite and never matches,
// and one an entry of its stage names by its name fails, so that an
// entry cannot apply silently to an input that merely shares a case's
// name.
func matchDivergence(list []divergence, stageName string, in input) (divergence, bool, string) {
	for _, d := range list {
		switch {
		case in.suite != "" && d.suite == in.suite && d.name == in.name:
			return d, true, ""
		case in.suite == "" && d.suite == stageName && d.name == in.name:
			return divergence{}, false, fmt.Sprintf(
				"divergence %s names %s of %s, but this run of it is no case of the stage: "+
					"an entry applies to a suite's cases alone", d.id, in.name, stageName)
		}
	}
	return divergence{}, false, ""
}

// asSuite returns ins, each marked as a case of the suite named suite,
// whose entries of the divergence list then apply to it.
func asSuite(ins []input, suite string) []input {
	out := slices.Clone(ins)
	for i := range out {
		out[i].suite = suite
	}
	return out
}

// divergenceVerdict returns why d, an entry of the divergence list for a
// case of a stage whose standard outputs compare as c says, fails, given
// goRun and portRun, how the Go compiler and the port ran it: the Go
// compiler's run no longer differing from what the port must give (see
// expected), compared as the stage compares the two compilers, which
// leaves the entry stale, and every way the port's run differs from it.
func divergenceVerdict(d divergence, c compare, goRun, portRun result) []string {
	var why []string
	want := d.expected(goRun)
	// An entry that records a file is stale when Go writes that file as
	// recorded, which the stage checks (see fileVerdict).
	if d.file == "" && goRun.code == want.code && bytes.Equal(goRun.stderr, want.stderr) &&
		stdoutDiff(c, want.stdout, goRun.stdout) == "" {
		why = append(
			why,
			"divergence "+d.id+" is stale: the Go compiler gives its recorded output; "+
				"remove the entry from "+divergencesFile,
		)
	}
	if portRun.code != want.code {
		why = append(why, fmt.Sprintf("exit status: port %d, recorded %d", portRun.code, want.code))
	}
	if diff := firstDiff(want.stderr, portRun.stderr); diff != "" {
		why = append(why, "standard error differs from the recorded: "+recordedDiff(diff))
	}
	if diff := stdoutDiff(c, want.stdout, portRun.stdout); diff != "" {
		why = append(why, "standard output differs from the recorded: "+recordedDiff(diff))
	}
	return why
}

// fileVerdict returns why d, an entry of the divergence list for a case
// of emit-header, fails, given goFiles and portFiles, what the two
// compilers' copies hold afterwards (see snapshot), and goFiles as the
// port's must be: Go's, but for d's file, which must hold what d
// records, and which Go's must not, or the entry is stale. An entry
// that records no file leaves goFiles as they are.
func fileVerdict(d divergence, goFiles, portFiles map[string]string) ([]string, map[string]string) {
	if d.file == "" {
		return nil, goFiles
	}
	var why []string
	if goFiles[d.file] == d.fileWant {
		why = append(why, "divergence "+d.id+" is stale: the Go compiler writes its recorded "+d.file+
			"; remove the entry from "+divergencesFile)
	}
	if _, ok := portFiles[d.file]; !ok {
		why = append(why, "divergence "+d.id+" records "+d.file+", which the port does not write")
	}
	want := maps.Clone(goFiles)
	want[d.file] = d.fileWant
	return why, want
}

// recordedDiff rewords diff, a firstDiff of a recorded output and the
// port's, which names the recorded side Go, to name it recorded.
func recordedDiff(diff string) string {
	return strings.Replace(diff, "\n  Go:   ", "\n  recorded: ", 1)
}

// compareDivergence reports how port, the port's run on in, differs from
// what d records, and fails when want, the Go compiler's run, no longer
// differs from it (see divergenceVerdict), in place of compareResults'
// comparison with the Go compiler.
func compareDivergence(
	t *testing.T,
	st stage,
	in input,
	args []string,
	d divergence,
	want, port result,
) {
	t.Helper()
	for _, why := range divergenceVerdict(d, st.stdout, want, port) {
		t.Error(why)
	}
	if t.Failed() {
		t.Logf("known divergence %s: %s", d.id, d.goBug)
		t.Logf("command: (cd %s && tack %s)", in.dir, strings.Join(args, " "))
	}
}

// divergedDecline returns why d's case fails when declined says the port
// answered it not implemented, or "": a listed case is never a skip.
func divergedDecline(d divergence, declined bool) string {
	if !declined {
		return ""
	}
	return "divergence " + d.id + " lists the case, which the port declines: a known divergence is never a skip"
}

// missingDivergences returns why each entry of list of the suite named
// suite fails whose case is not among names, the suite's cases.
func missingDivergences(list []divergence, suite string, names []string) []string {
	var why []string
	for _, d := range list {
		if d.suite == suite && !slices.Contains(names, d.name) {
			why = append(why, fmt.Sprintf("divergence %s names case %s of %s, which there is not",
				d.id, d.name, suite))
		}
	}
	return why
}

// checkDivergencesListed fails t for each entry of the divergence list
// of the suite named suite whose case is not one of ins.
func checkDivergencesListed(t *testing.T, suite string, ins []input) {
	t.Helper()
	list, err := loadedDivergences()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(ins))
	for i, in := range ins {
		names[i] = in.name
	}
	for _, why := range missingDivergences(list, suite, names) {
		t.Error(why)
	}
}

// TestDivergencesFile checks that the divergence list reads, so a
// mistake in it shows in a plain go test, and that each entry of a
// suite whose cases are files of this module, the probes', the lex
// probes' and the emit probes', names one there is. TestPort checks the
// rest of the entries' cases, which only a run of the corpus lists.
func TestDivergencesFile(t *testing.T) {
	list, err := loadedDivergences()
	if err != nil {
		t.Fatal(err)
	}
	probes, err := probeInputs()
	if err != nil {
		t.Fatal(err)
	}
	checkDivergencesListed(t, probesStage, probes)
	lexProbes, err := lexProbeInputs()
	if err != nil {
		t.Fatal(err)
	}
	checkDivergencesListed(t, lexProbesStage, lexProbes)
	srcs, err := emitProbeSources()
	if err != nil {
		t.Fatal(err)
	}
	ins := make([]input, 0, len(srcs))
	for name := range srcs {
		ins = append(ins, input{name: name})
	}
	checkDivergencesListed(t, emitProbesSuite, ins)
	t.Logf("%d known divergences", len(list))
}

// divergenceEntry returns one entry of a divergence list, written as
// the file writes it, with the given id, header lines and output.
// An empty exit writes an empty section, as for an entry whose same
// names exit.
func divergenceEntry(id string, header []string, exit, stdout, stderr string) string {
	if exit != "" {
		exit += "\n"
	}
	return fmt.Sprintf(
		"-- %s/divergence --\n%s\n-- %s/exit --\n%s-- %s/stdout --\n%s-- %s/stderr --\n%s",
		id,
		strings.Join(header, "\n"),
		id,
		exit,
		id,
		stdout,
		id,
		stderr,
	)
}

// goodHeader is a valid divergence section's lines.
var goodHeader = []string{
	"suite: probes",
	"case: a.tck",
	"go: Go writes the wrong C",
	"why: the port writes the right one, decided 2026-11-15,",
	"  a known divergence",
	"same: none",
}

// TestParseDivergences checks that parseDivergences reads an entry's
// fields, a field's continued value and its sections exactly, an empty
// section among them.
func TestParseDivergences(t *testing.T) {
	data := "# a comment\n\n" + divergenceEntry("a", goodHeader, "1", "", "x\n\ny\n") +
		divergenceEntry(
			"b",
			append([]string{"suite: emit-probes", "case: b.tck"}, goodHeader[2:]...),
			"0",
			"int main;\n",
			"",
		) +
		divergenceEntry("c", append([]string{"suite: probes", "case: c.tck"},
			append(slices.Clone(goodHeader[2:5]), "same: exit, stdout")...), "", "", "w\n")
	got, err := parseDivergences(
		[]byte(data),
		map[string]bool{"probes": true, emitProbesSuite: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	bug, why := "Go writes the wrong C", "the port writes the right one, decided 2026-11-15, a known divergence"
	want := []divergence{
		{
			id: "a", suite: "probes", name: "a.tck", goBug: bug, why: why, same: map[string]bool{},
			want: result{code: 1, stdout: []byte{}, stderr: []byte("x\n\ny\n")},
		},
		{
			id:    "b",
			suite: emitProbesSuite,
			name:  "b.tck",
			goBug: bug,
			why:   why,
			same:  map[string]bool{},
			want:  result{stdout: []byte("int main;\n"), stderr: []byte{}},
		},
		{
			id: "c", suite: "probes", name: "c.tck", goBug: bug, why: why,
			same: map[string]bool{"exit": true, "stdout": true},
			want: result{stdout: []byte{}, stderr: []byte("w\n")},
		},
	}
	if !reflect.DeepEqual(normalized(got), normalized(want)) {
		t.Errorf("parseDivergences =\n%+v\nwant\n%+v", got, want)
	}
}

// normalized returns list with each recorded output's nil byte slices
// made empty ones, so that DeepEqual compares their bytes alone.
func normalized(list []divergence) []divergence {
	out := slices.Clone(list)
	for i := range out {
		if out[i].want.stdout == nil {
			out[i].want.stdout = []byte{}
		}
		if out[i].want.stderr == nil {
			out[i].want.stderr = []byte{}
		}
	}
	return out
}

// TestParseDivergencesRejects checks that parseDivergences rejects every
// malformed list: text before the first section, a missing or misordered
// section or field, an unknown suite, an exit status that is no number,
// a why with no date, a same that is no list of streams, names all of
// them with no file recorded or one its section records, an entry or a
// case listed twice, and a file section of an entry not emit-header's,
// or with no path or no mode.
func TestParseDivergencesRejects(t *testing.T) {
	suites := map[string]bool{"probes": true, emitHeaderStage: true}
	for name, data := range malformedLists() {
		got, err := parseDivergences([]byte(data), suites)
		if err == nil {
			t.Errorf("%s: parseDivergences = %+v, want an error", name, got)
			continue
		}
		t.Logf("%s: %v", name, err)
	}
}

// headerEntry is an entry of emit-header recording a file, its every
// stream Go's.
var headerEntry = divergenceEntry("h", append([]string{"suite: " + emitHeaderStage, "case: src/os"},
	append(slices.Clone(goodHeader[2:5]), "same: exit, stdout, stderr")...), "", "", "") +
	"-- h/file --\npath: src/os/tack.h\n-rw-r--r--\nint os_x;\n"

// TestParseDivergencesFile checks that parseDivergences reads an entry
// of emit-header ending in a file section, its path and the file's mode
// and bytes, with same naming every stream, and the entry after it.
func TestParseDivergencesFile(t *testing.T) {
	data := headerEntry + divergenceEntry("a", goodHeader, "1", "", "x\n")
	got, err := parseDivergences([]byte(data), map[string]bool{"probes": true, emitHeaderStage: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].file != "src/os/tack.h" || got[0].fileWant != "-rw-r--r--\nint os_x;\n" ||
		!got[0].same["exit"] || !got[0].same["stdout"] || !got[0].same["stderr"] ||
		got[1].id != "a" || got[1].file != "" {
		t.Errorf("parseDivergences = %+v", got)
	}
}

// TestFileVerdict checks fileVerdict: the port's copy must hold the
// recorded file where Go's does not, Go's elsewhere; an entry whose
// file Go writes as recorded is stale, and one the port does not write
// fails; an entry recording no file leaves Go's files as they are.
func TestFileVerdict(t *testing.T) {
	d := divergence{id: "h", file: "os/tack.h", fileWant: "-rw-r--r--\nos_x\n"}
	goFiles := map[string]string{"os/": "drwxr-xr-x\n", "os/tack.h": "-rw-r--r--\nx\n"}
	port := map[string]string{"os/": "drwxr-xr-x\n", "os/tack.h": d.fileWant}
	why, want := fileVerdict(d, goFiles, port)
	if len(why) != 0 || filesDiff(want, port) != "" {
		t.Errorf("port as recorded: %q, %s", why, filesDiff(want, port))
	}
	if filesDiff(want, goFiles) == "" {
		t.Error("Go's files pass for the port's")
	}
	if why, _ := fileVerdict(d, port, port); len(why) != 1 {
		t.Errorf("stale: %q, want one failure", why)
	}
	if why, _ := fileVerdict(d, goFiles, map[string]string{"os/": "drwxr-xr-x\n"}); len(why) != 1 {
		t.Errorf("not written: %q, want one failure", why)
	}
	if why, w := fileVerdict(divergence{id: "n"}, goFiles, port); len(why) != 0 || filesDiff(w, goFiles) != "" {
		t.Errorf("no file: %q", why)
	}
}

// malformedLists returns TestParseDivergencesRejects's lists, each by
// what is wrong with it.
func malformedLists() map[string]string {
	without := func(i int) []string { return slices.Delete(slices.Clone(goodHeader), i, i+1) }
	with := func(i int, line string) []string {
		h := slices.Clone(goodHeader)
		h[i] = line
		return h
	}
	entry := func(header []string, exit, stdout, stderr string) string {
		return divergenceEntry("a", header, exit, stdout, stderr)
	}
	good := entry(goodHeader, "0", "", "")
	other := append([]string{"suite: probes", "case: other.tck"}, goodHeader[2:]...)
	return map[string]string{
		"text first":           "stray\n" + good,
		"no suite":             entry(without(0), "0", "", ""),
		"no case":              entry(without(1), "0", "", ""),
		"no go":                entry(without(2), "0", "", ""),
		"no why":               entry(without(3), "0", "", ""),
		"no same":              entry(goodHeader[:5], "0", "", ""),
		"empty field":          entry(with(2, "go: "), "0", "", ""),
		"unknown":              entry(with(0, "suite: nowhere"), "0", "", ""),
		"exit word":            entry(goodHeader, "one", "", ""),
		"undated":              entry(slices.Delete(with(3, "why: because"), 4, 5), "0", "", ""),
		"same word":            entry(with(5, "same: stdin"), "0", "", ""),
		"same twice":           entry(with(5, "same: stdout, stdout"), "0", "", ""),
		"same all":             entry(with(5, "same: exit, stdout, stderr"), "", "", ""),
		"same recorded":        entry(with(5, "same: stdout"), "0", "C\n", ""),
		"same no space":        entry(with(5, "same: exit,stdout"), "", "", "w\n"),
		"same id":              good + divergenceEntry("a", other, "0", "", ""),
		"file not emit-header": good + "-- a/file --\npath: x/tack.h\n-rw-r--r--\nint x;\n",
		"file no path":         strings.Replace(headerEntry, "path: src/os/tack.h\n", "", 1),
		"file no mode":         strings.Replace(headerEntry, "-rw-r--r--\nint os_x;\n", "", 1),
		"same all, no file":    strings.Split(headerEntry, "-- h/file --")[0],
		"same case":            good + divergenceEntry("b", goodHeader, "0", "", ""),
		"no stderr":            strings.Split(good, "-- a/stderr --")[0],
		"mixed ids":            strings.Replace(good, "-- a/exit --", "-- b/exit --", 1),
		"out of order":         strings.Replace(good, "-- a/stdout --", "-- a/stderr --", 1),
	}
}

// TestDivergenceVerdict checks divergenceVerdict: a port that gives the
// recorded output where Go does not passes; one that differs from it in
// its exit status, its standard error or its standard output fails,
// even where it gives Go's output, so an entry is never a skip; and an
// entry whose recorded output Go gives is stale and fails, compared as
// the stage compares, JSON as JSON. For an entry whose same names
// streams, the port must give Go's there and the recorded elsewhere.
func TestDivergenceVerdict(t *testing.T) {
	d := divergence{id: "x", want: result{stdout: []byte("good C\n"), stderr: []byte("w\n")}}
	goRun := result{stdout: []byte("bad C\n"), stderr: []byte("w\n")}
	checkVerdicts(t, d, []verdictCase{
		{"port as recorded", goRun, d.want, 0},
		{"port as Go", goRun, goRun, 1},
		{"exit status", goRun, result{code: 1, stdout: d.want.stdout, stderr: d.want.stderr}, 1},
		{"stderr", goRun, result{stdout: d.want.stdout, stderr: []byte("v\n")}, 1},
		{"stdout", goRun, result{stdout: []byte("good C"), stderr: d.want.stderr}, 1},
		{"stale", d.want, d.want, 1},
		{"stale and wrong", d.want, goRun, 2},
	})
	// An entry recording standard error alone: the port must give Go's
	// exit status and standard output, and the recorded standard error.
	w := divergence{
		id:   "w",
		want: result{stderr: []byte("warning\n")},
		same: map[string]bool{"exit": true, "stdout": true},
	}
	goC := result{stdout: []byte("C\n")}
	warned := result{stdout: goC.stdout, stderr: w.want.stderr}
	checkVerdicts(t, w, []verdictCase{
		{"recorded stderr, Go's stdout", goC, warned, 0},
		{"other stdout", goC, result{stdout: []byte("D\n"), stderr: w.want.stderr}, 1},
		{"other exit", goC, result{code: 1, stdout: goC.stdout, stderr: w.want.stderr}, 1},
		{"Go's stderr", goC, goC, 1},
		{"stale", warned, warned, 1},
	})
	j := divergence{id: "j", want: result{stdout: []byte(`{"a": 1}`)}}
	why := divergenceVerdict(j, compareJSON, result{stdout: []byte(`{"a":1}`)}, j.want)
	if len(why) != 1 {
		t.Errorf("a JSON entry Go gives as recorded, spaced otherwise: %q, want it stale", why)
	}
}

// verdictCase is a case of TestDivergenceVerdict: how Go and the port
// ran, and how many failures divergenceVerdict must find.
type verdictCase struct {
	name  string
	goRun result
	port  result
	fails int
}

// checkVerdicts checks divergenceVerdict of d, compared byte for byte,
// on each of cases.
func checkVerdicts(t *testing.T, d divergence, cases []verdictCase) {
	t.Helper()
	for _, tc := range cases {
		if why := divergenceVerdict(d, compareBytes, tc.goRun, tc.port); len(why) != tc.fails {
			t.Errorf(
				"%s, %s: divergenceVerdict = %q, want %d failures",
				d.id,
				tc.name,
				why,
				tc.fails,
			)
		}
	}
}

// TestMissingDivergences checks that an entry whose case its suite does
// not have fails, and that only the entries of the suite asked about are
// looked at.
func TestMissingDivergences(t *testing.T) {
	list := []divergence{
		{id: "here", suite: "probes", name: "a.tck"},
		{id: "gone", suite: "probes", name: "b.tck"},
		{id: "elsewhere", suite: "emit-single", name: "b.tck"},
	}
	why := missingDivergences(list, "probes", []string{"a.tck", "c.tck"})
	if len(why) != 1 || !strings.Contains(why[0], "divergence gone") {
		t.Errorf("missingDivergences = %q, want only gone's", why)
	}
	if why := missingDivergences(list, "lex-single", nil); len(why) != 0 {
		t.Errorf("missingDivergences of a suite with no entry = %q", why)
	}
}

// TestMatchDivergence checks that an entry applies to the case of its
// suite it names alone: not to a case of another suite of that name, and
// not to an input no suite holds, whose run fails instead when the entry
// is of the stage it runs as, as an input of a deep test named as a case
// is.
func TestMatchDivergence(t *testing.T) {
	list := []divergence{
		{id: "single", suite: "emit-single", name: "main.tck"},
		{id: "probe", suite: emitProbesSuite, name: "p.tck"},
	}
	single := input{name: "main.tck", suite: "emit-single"}
	cases := []struct {
		name   string
		stage  string
		listed string
		in     input
		fails  bool
	}{
		{name: "the stage's case", stage: "emit-single", listed: "single", in: single},
		{
			name: "an emit probe", stage: "emit-single", listed: "probe",
			in: input{name: "p.tck", suite: emitProbesSuite},
		},
		{
			name: "another suite's case", stage: "emit-single",
			in: input{name: "main.tck", suite: emitProbesSuite},
		},
		{
			name: "another stage's case", stage: "emit-cases",
			in: input{name: "main.tck", suite: "emit-cases"},
		},
		{
			name:  "a deep test's input",
			stage: "emit-single",
			in:    input{name: "main.tck"},
			fails: true,
		},
		{name: "an input of an emit probe's name", stage: "emit-single", in: input{name: "p.tck"}},
		{name: "an unnamed input", stage: "emit-single", in: input{name: "other.tck"}},
	}
	for _, tc := range cases {
		d, listed, why := matchDivergence(list, tc.stage, tc.in)
		if listed != (tc.listed != "") || d.id != tc.listed || (why != "") != tc.fails {
			t.Errorf("%s: matchDivergence = %q, %v, %q; want %q, failing %v",
				tc.name, d.id, listed, why, tc.listed, tc.fails)
		}
	}
}

// TestDivergedDecline checks that a case the divergence list holds
// fails when the port declines it, in any stage, required or not: an
// entry is never a skip.
func TestDivergedDecline(t *testing.T) {
	d := divergence{id: "x", suite: "plain", name: "a.tck"}
	if why := divergedDecline(d, true); why == "" {
		t.Error("a listed case the port declines does not fail")
	}
	if why := divergedDecline(d, false); why != "" {
		t.Errorf("a listed case the port answers fails: %q", why)
	}
}

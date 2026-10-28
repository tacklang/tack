package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// specDir is where the language specification's chapters live, relative
// to this package.
const specDir = "../docs/spec"

// specIndex is the specification's index, which links every chapter.
const specIndex = "README.md"

// specFile is the name a spec example's program is written to, and so the
// name its diagnostics show.
const specFile = "main.tck"

// The kinds of tack block a chapter may hold, named by the word after
// "tack" in the block's info string. docs/spec/README.md describes each.
const (
	specRun      = "run"      // built and run; exits with a given status
	specCheck    = "check"    // accepted by --check with no diagnostic
	specError    = "error"    // rejected by --check with given diagnostics
	specWarn     = "warn"     // accepted by --check with given warnings
	specFragment = "fragment" // not a program; not compiled
)

// The info strings of the blocks that state what the tack block before
// them must produce.
const (
	specOutput      = "output"      // what a run example writes to stdout
	specDiagnostics = "diagnostics" // what an error or warn example reports
)

// specExitAttr matches the attribute of a run block, exit=N.
var specExitAttr = regexp.MustCompile(`^exit=([0-9]+)$`)

// specIndexLine matches a whole line of the index that links a chapter,
// as 1. [Notation](01-notation.md): an ordered list marker of one to nine
// digits, a dot and one space; a title of one or more ASCII letters,
// digits, spaces and the punctuation , . : ; ( ) ' & -, and nothing else;
// and the chapter's file name as the link's destination, with nothing
// after it. Group 1 is the title and group 2 the file name.
var specIndexLine = regexp.MustCompile(
	`^[0-9]{1,9}\. \[([A-Za-z0-9 ,.:;()'&-]+)\]\(([0-9]{2}-[a-z0-9-]+\.md)\)$`)

// specChapterName matches a chapter's file name, two digits, a dash, and
// a lowercase name.
var specChapterName = regexp.MustCompile(`^[0-9]{2}-[a-z0-9-]+\.md$`)

// specHeader matches the first line of a diagnostic the driver renders
// for specFile: its position, severity and message.
var specHeader = regexp.MustCompile(`^main\.tck:[0-9]+:[0-9]+: (error|warning|note): `)

// specExcerpt matches a line of the source the driver shows under a
// diagnostic, numbered, or the marker under the line it points at.
var specExcerpt = regexp.MustCompile(`^([0-9]{2,} |   )\|\t`)

// specExample is one tack block of a chapter, with the block after it
// that states what it must produce, when it has one.
type specExample struct {
	pos    string // the chapter and the line of the opening fence, as 04-types.md:12
	kind   string // specRun, specCheck, specError, specWarn or specFragment
	src    string // the block's content
	expect string // the content of the block after it, for its kind
	exit   int    // for specRun, the status the program must exit with
	hasExp bool   // whether that block is there
}

// fence is one fenced code block of a Markdown file.
type fence struct {
	info       string // the info string, trimmed
	body       string // the lines between the fences, each ending in \n
	start, end int    // lines of the opening and closing fence, from 1
}

// specHTML matches what could be raw HTML anywhere on a line, a <
// followed by a letter, /, ! or ?, which starts a tag, a comment or a
// processing instruction.
var specHTML = regexp.MustCompile(`<[A-Za-z/!?]`)

// specInfo matches the info string of an opening fence in the spec's
// dialect: nothing, or a lowercase word followed by attributes, each a
// space and then lowercase letters, digits and =, as in tack run exit=3.
var specInfo = regexp.MustCompile(`^([a-z][a-z0-9]*( [a-z0-9=]+)*)?$`)

// specInfoWords are the first words an opening fence's info string may
// have: none, tack and the blocks that follow a tack block, and text and
// markdown for blocks shown without being checked. Any other word fails,
// so a misspelled tack, as tak or tck, is never skipped.
var specInfoWords = []string{"", "tack", specOutput, specDiagnostics, "text", "markdown"}

// badByte returns a problem naming the line and column, from 1, of the
// first byte of data that is neither printable ASCII, 0x20 to 0x7E, nor
// \n, and "" when there is none.
func badByte(data string) string {
	line, col := 1, 0
	for i := range len(data) {
		b := data[i]
		if b == '\n' {
			line, col = line+1, 0
			continue
		}
		col++
		if b < 0x20 || b > 0x7e {
			return fmt.Sprintf("%d:%d: byte 0x%02x: a spec file holds only printable "+
				"ASCII and newlines (no tab, carriage return or non-ASCII byte)", line, col, b)
		}
	}
	return ""
}

// fenceRun returns the character and the length of the run of three or
// more backticks or tildes that starts line at column 0, and false when
// line starts with no such run.
func fenceRun(line string) (byte, int, bool) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	run := len(line) - len(strings.TrimLeft(line, line[:1]))
	return line[0], run, run >= 3
}

// fenceScanner finds the fenced code blocks of one file of the spec, a
// line at a time, and collects the lines outside the spec's dialect.
type fenceScanner struct {
	open     *fence          // the block being read, nil between blocks
	name     string          // the file's name, for problems
	fences   []fence         // the blocks read so far
	problems []string        // what is outside the dialect, each with its line
	body     strings.Builder // the lines of the open block so far
	run      int             // the length of the open block's opening run
	ch       byte            // the open block's fence character
}

// bad records a problem at line n.
func (s *fenceScanner) bad(n int, format string, args ...any) {
	s.problems = append(s.problems,
		fmt.Sprintf("%s:%d: ", s.name, n)+fmt.Sprintf(format, args...))
}

// line reads line n of the file, line.
func (s *fenceScanner) line(n int, line string) {
	if s.open != nil {
		s.inside(n, line)
		return
	}
	ch, run, isFence := fenceRun(line)
	switch {
	case isFence:
		// A renderer opens a block whatever the info string, so the block
		// is read as one even when its info string is a problem.
		s.checkInfo(n, line[run:])
		s.open, s.ch, s.run = &fence{start: n, info: line[run:]}, ch, run
		s.body.Reset()
	case strings.Contains(line, "```") || strings.Contains(line, "~~~"):
		s.bad(n, "three backticks or tildes outside a fence at column 0; "+
			"use single-backtick inline code, and start every fence at column 0")
	case strings.HasPrefix(strings.TrimLeft(line, " "), "$$"):
		s.bad(n, "a line starting with $$ would start a math block on GitHub; "+
			"the spec uses none")
	case specHTML.MatchString(line):
		s.bad(n, "a < followed by a letter, /, ! or ? could be raw HTML, which the spec "+
			"does not use; write a comparison with spaces, a < b")
	}
}

// checkInfo records a problem at line n when info, the info string of an
// opening fence, is not of the dialect's grammar, or its first word is
// not one of specInfoWords.
func (s *fenceScanner) checkInfo(n int, info string) {
	switch {
	case !specInfo.MatchString(info):
		s.bad(n, "fence info string %q: write nothing, or a lowercase word and "+
			"attributes of lowercase letters, digits and =, separated by one space", info)
	case !slices.Contains(specInfoWords, infoWord(info)):
		s.bad(n, "fence info word %q: use one of %q", infoWord(info), specInfoWords)
	}
}

// inside reads line n, line, of the open block: a run of its character
// at column 0, at least as long as the opening one and followed by
// nothing but spaces, closes it, as a renderer would, and is a problem
// unless it is exactly the opening run; an indented line that is nothing
// but such a run is a problem, since a renderer may close the block there;
// any other line is the block's content.
func (s *fenceScanner) inside(n int, line string) {
	ch, run, isFence := fenceRun(line)
	if isFence && ch == s.ch && run >= s.run && strings.TrimSpace(line[run:]) == "" {
		if line != strings.Repeat(string(ch), s.run) {
			s.bad(n, "a closing fence is exactly the opening fence's run, with nothing after it")
		}
		s.open.end, s.open.body = n, s.body.String()
		s.fences = append(s.fences, *s.open)
		s.open = nil
		return
	}
	trimmed := strings.TrimSpace(line)
	if trimmed != line && len(trimmed) >= s.run && strings.Trim(trimmed, string(s.ch)) == "" {
		s.bad(n, "an indented fence: a closing fence starts at column 0")
	}
	s.body.WriteString(line)
	s.body.WriteString("\n")
}

// scanFences returns the fenced code blocks of data, a file of the spec,
// in order, backtick and tilde fences alike, and a problem for everything
// outside the spec's dialect, described in docs/spec/README.md: the
// first byte that is not printable ASCII or a newline, after which
// nothing else is read; an opening fence whose info string is not of the
// dialect's grammar; a closing fence that is not exactly the opening
// run; outside a block, a line holding three backticks or tildes that is
// not a fence, or one holding what could be raw HTML; inside a block, an
// indented line that could close it; a block that is never closed; and a
// first line of ---, which would start front matter.
func scanFences(name, data string) ([]fence, []string) {
	if why := badByte(data); why != "" {
		return nil, []string{name + ":" + why}
	}
	s := &fenceScanner{name: name}
	if first, _, _ := strings.Cut(data, "\n"); strings.TrimRight(first, " ") == "---" {
		s.bad(1, "a first line of --- would start front matter, which the spec does not use")
	}
	for i, line := range strings.Split(data, "\n") {
		s.line(i+1, line)
	}
	if s.open != nil {
		s.bad(s.open.start, "the fenced block is never closed")
	}
	return s.fences, s.problems
}

// infoWord returns the first word of a fence's info string.
func infoWord(info string) string {
	if f := strings.Fields(info); len(f) > 0 {
		return f[0]
	}
	return ""
}

// adjacent reports whether only blank lines of lines separate the closing
// fence of a from the opening fence of b. lines are the file's lines, the
// first at index 0.
func adjacent(lines []string, a, b fence) bool {
	for l := a.end + 1; l < b.start; l++ {
		if strings.TrimSpace(lines[l-1]) != "" {
			return false
		}
	}
	return true
}

// specParser reads the tack blocks of one chapter and collects the
// problems it finds in them.
type specParser struct {
	name     string   // the chapter's file name
	lines    []string // the chapter's lines, the first at index 0
	problems []string // what is wrong, each with its file and line
}

// bad records a problem at the opening fence of f.
func (p *specParser) bad(f fence, format string, args ...any) {
	p.problems = append(p.problems,
		fmt.Sprintf("%s:%d: ", p.name, f.start)+fmt.Sprintf(format, args...))
}

// parseSpecExamples returns the tack blocks of the chapter name, whose
// content is data, each with the block that follows it when its kind
// takes one, and the problems found: those scanFences finds; a tack block
// that is untagged, of an unknown kind, or with a bad attribute; an info
// string that starts with tack or tck, in any case, but is not the word
// tack; an error or warn block without a diagnostics block after it that
// names at least one error or warning, which is left out of the examples
// returned; a block directly after a tack block that is not the one its
// kind takes; and an output or diagnostics block that follows no tack
// block.
func parseSpecExamples(name, data string) ([]specExample, []string) {
	fences, problems := scanFences(name, data)
	p := &specParser{name: name, lines: strings.Split(data, "\n"), problems: problems}
	var examples []specExample
	for i := 0; i < len(fences); i++ {
		ex, ok := p.tackBlock(fences[i])
		if !ok {
			continue
		}
		var next *fence
		if i+1 < len(fences) && adjacent(p.lines, fences[i], fences[i+1]) {
			next = &fences[i+1]
		}
		consumed, valid := p.attach(&ex, fences[i], next)
		if consumed {
			i++
		}
		if valid {
			examples = append(examples, ex)
		}
	}
	return examples, p.problems
}

// tackBlock returns the example the fence f makes, and false when f is
// not a tack block or is a bad one, recording a problem for a bad one and
// for an output or diagnostics block, which belongs to no tack block when
// it gets here.
func (p *specParser) tackBlock(f fence) (specExample, bool) {
	word := infoWord(f.info)
	switch {
	case word == specOutput || word == specDiagnostics:
		p.bad(f, "%s block: it must directly follow the tack block it belongs to", word)
		return specExample{}, false
	case word != "tack":
		return specExample{}, false
	}
	return p.exampleOf(f)
}

// attach gives ex, the example of the tack block f, the block next that
// directly follows it, nil when none does. It reports whether next was
// taken, as ex's expectation or as a block that is wrong there, and
// whether ex is complete enough to run.
func (p *specParser) attach(ex *specExample, f fence, next *fence) (consumed, valid bool) {
	want := expectWord(ex.kind)
	needed := ex.kind == specError || ex.kind == specWarn
	switch {
	case next != nil && want != "" && infoWord(next.info) == want:
		if next.info != want {
			p.bad(*next, "the %s block of a tack block takes no attributes", want)
		}
		ex.expect, ex.hasExp = next.body, true
		consumed = true
	case next != nil:
		p.bad(*next, "a %q block directly after a tack %s block: %s; put text between them",
			next.info, ex.kind, describeExpect(want))
		consumed = infoWord(next.info) != "tack"
	}
	return consumed, !needed || p.hasDiagnostics(*ex, f)
}

// hasDiagnostics reports whether ex, the example of the tack error or
// warn block f, has its diagnostics block and the block holds at least
// one diagnostic of the severity its kind names, recording a problem when
// it does not.
func (p *specParser) hasDiagnostics(ex specExample, f fence) bool {
	switch {
	case !ex.hasExp:
		p.bad(f, "a tack %s block must be followed directly by a %s block",
			ex.kind, specDiagnostics)
		return false
	case !hasSeverity(ex.expect, severityOf(ex.kind)):
		p.bad(f, "the %s block of a tack %s block must hold at least one %s",
			specDiagnostics, ex.kind, severityOf(ex.kind))
		return false
	}
	return true
}

// expectWord returns the info string of the block that may follow a tack
// block of kind, or "" for a kind that takes none.
func expectWord(kind string) string {
	switch kind {
	case specRun:
		return specOutput
	case specError, specWarn:
		return specDiagnostics
	}
	return ""
}

// describeExpect says, for a problem, what may directly follow a tack
// block whose expectation block is want, "" for none.
func describeExpect(want string) string {
	switch want {
	case specOutput:
		return "only an output block may follow it there"
	case specDiagnostics:
		return "only a diagnostics block may follow it there"
	}
	return "no block may follow it there"
}

// hasSeverity reports whether expect, the content of a diagnostics block,
// holds the header line of a diagnostic of severity, read from the line's
// own severity field, never from its message.
func hasSeverity(expect, severity string) bool {
	for line := range strings.Lines(expect) {
		m := specHeader.FindStringSubmatch(strings.TrimSuffix(line, "\n"))
		if m != nil && m[1] == severity {
			return true
		}
	}
	return false
}

// severityOf returns the severity a diagnostics block of a tack block of
// kind, error or warn, must hold at least one of.
func severityOf(kind string) string {
	if kind == specWarn {
		return "warning"
	}
	return "error"
}

// exampleOf reads the tack block f, recording a problem and returning
// false when its info string names no kind, an unknown one, or
// attributes the kind does not take.
func (p *specParser) exampleOf(f fence) (specExample, bool) {
	words := strings.Fields(f.info)
	ex := specExample{pos: fmt.Sprintf("%s:%d", p.name, f.start), src: f.body}
	if len(words) < 2 {
		p.bad(f, "untagged tack block: write tack run exit=N, tack check, tack error, "+
			"tack warn or tack fragment")
		return ex, false
	}
	ex.kind = words[1]
	attrs := words[2:]
	switch ex.kind {
	case specRun:
		code, ok := p.exitOf(f, attrs)
		ex.exit = code
		return ex, ok
	case specCheck, specError, specWarn, specFragment:
		if len(attrs) != 0 {
			p.bad(f, "a tack %s block takes no attributes", ex.kind)
			return ex, false
		}
		return ex, true
	}
	p.bad(f, "unknown kind %q: want run, check, error, warn or fragment", ex.kind)
	return ex, false
}

// exitOf returns the status the attributes of the tack run block f name,
// exactly one, exit=N with N from 0 to 255, recording a problem and
// returning false for anything else.
func (p *specParser) exitOf(f fence, attrs []string) (int, bool) {
	if len(attrs) != 1 {
		p.bad(f, "a tack run block takes exactly one attribute, exit=N")
		return 0, false
	}
	m := specExitAttr.FindStringSubmatch(attrs[0])
	if m == nil {
		p.bad(f, "attribute %q: want exit=N", attrs[0])
		return 0, false
	}
	code, err := strconv.Atoi(m[1])
	if err != nil || code > 255 {
		p.bad(f, "exit status %s: want 0 to 255", m[1])
		return 0, false
	}
	return code, true
}

// specRunTimeout bounds how long a run example's binary may run before
// the example fails, so a program that never ends fails rather than
// hanging the suite. It is a variable only so a test of checkSpecDir can
// lower it for a looping example; TestSpecRunLimits pins its value.
var specRunTimeout = 10 * time.Second

// specRunner says how checkSpecChapter runs the programs of run examples.
type specRunner struct {
	timeout time.Duration // how long a binary may run
	clang   bool          // whether clang is on PATH, so a binary can be built
}

// specRunnerFor returns the specRunner whose binaries may run for
// timeout, and are built only when clang is on PATH.
func specRunnerFor(timeout time.Duration) specRunner {
	_, err := exec.LookPath("clang")
	return specRunner{timeout: timeout, clang: err == nil}
}

// specChapters returns the names of the Markdown files of dir, sorted.
func specChapters(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("listing the chapters of %s: %w", dir, err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	slices.Sort(names)
	return names, nil
}

// diagnosticLines splits stderr, the driver's diagnostics for specFile,
// into the first line of each diagnostic, error, warning or note, each
// ending in \n, and the lines that are neither such a line, nor a line of
// the source shown under one, nor the empty line the driver writes before
// each diagnostic and after the last, which no example expects.
func diagnosticLines(stderr string) (headers string, unexpected []string) {
	var b strings.Builder
	for line := range strings.Lines(stderr) {
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "":
		case specHeader.MatchString(line):
			b.WriteString(line)
			b.WriteString("\n")
		case !specExcerpt.MatchString(line):
			unexpected = append(unexpected, line)
		}
	}
	return b.String(), unexpected
}

// specReport is what checking one chapter of the spec found.
type specReport struct {
	problems  []string // what is wrong, each with the chapter and line
	fragments []string // where the fragments are, which are not compiled
	skipped   []string // the run examples not built, and why
	examples  int      // how many tack blocks the chapter holds
}

// checkSpecChapter checks the chapter name, whose content is data: it
// parses it, reporting what parseSpecExamples finds, and checks each
// example that is not a fragment with checkSpecExample, running the
// binaries of run examples as run says. Each problem of an example
// starts with its position and kind. t is used only to run the driver;
// every problem is returned, none reported, so a test can check the
// checker.
func checkSpecChapter(t *testing.T, name, data string, run specRunner) specReport {
	t.Helper()
	examples, problems := parseSpecExamples(name, data)
	rep := specReport{problems: problems, examples: len(examples)}
	for _, ex := range examples {
		if ex.kind == specFragment {
			rep.fragments = append(rep.fragments, ex.pos)
			continue
		}
		found, skip := checkSpecExample(t, ex, run)
		for _, p := range found {
			rep.problems = append(rep.problems, ex.pos+" ("+ex.kind+"): "+p)
		}
		if skip != "" {
			rep.skipped = append(rep.skipped, ex.pos+": "+skip)
		}
	}
	return rep
}

// TestSpecExamples checks every fenced tack block in docs/spec, the
// language specification, through checkSpecDir and reportSpec, by the
// kind its info string names:
//
//   - tack run exit=N is a program --check accepts with no diagnostic,
//     which the compiler builds into a binary that writes nothing to
//     stderr and exits with status N within specRunTimeout. An output
//     block directly after it pins what the binary writes to stdout,
//     byte for byte. The build needs clang, and is skipped without it
//     after the check.
//   - tack check is a program --check accepts with no diagnostic.
//   - tack error is a program --check rejects. The diagnostics block
//     directly after it holds the first line of each diagnostic, error,
//     warning and note, as the driver renders them for main.tck, in
//     order; they must be exactly the diagnostics reported, and at least
//     one an error.
//   - tack warn is a program --check accepts. Its diagnostics block holds
//     the diagnostics reported as for tack error, at least one a warning.
//   - tack fragment is not a complete program and is not compiled; the
//     test logs how many there are.
//
// --check never writes to stdout for any of them. The chapters are
// written in a strict dialect of Markdown, so that the blocks found are
// the ones a renderer shows: scanFences and parseSpecExamples list what
// makes a chapter fail before any program runs. Each program is written
// alone to main.tck, so its diagnostics name that file, and run through
// the driver in process, as runTack does for every driver test. The tests of
// spec_checker_test.go check checkSpecDir, checkSpecChapter and
// reportSpec in turn.
func TestSpecExamples(t *testing.T) {
	reportSpec(t, checkSpecDir(t, specDir))
}

// specReporter is what reportSpec reports through: a *testing.T, or a
// recorder in the tests of reportSpec.
type specReporter interface {
	Error(args ...any)
	Log(args ...any)
}

// specOutcome is what a check of the spec found: its failures, and the
// lines it logs. It is one value, so a test hands both to reportSpec in
// one argument and cannot swap them.
type specOutcome struct {
	failures []string // what fails the test
	logs     []string // what is only logged
}

// reportSpec reports each failure of o as a test failure, and each of its
// log lines as a log line, through r.
func reportSpec(r specReporter, o specOutcome) {
	for _, f := range o.failures {
		r.Error(f)
	}
	for _, l := range o.logs {
		r.Log(l)
	}
}

// checkSpecDir checks every Markdown file of dir, a directory of the
// spec, the index among them, with checkSpecChapter, building run
// examples when clang is on PATH and allowing their binaries
// specRunTimeout. It returns the failures: every problem found, a file
// that cannot be read, and a directory with no Markdown file; and the
// lines to log: each run example not built, and how many tack blocks
// there are and where the fragments are. t is used only to run the
// driver.
func checkSpecDir(t *testing.T, dir string) specOutcome {
	t.Helper()
	run := specRunnerFor(specRunTimeout)
	// checkSpecChapter changes the current directory, so a relative dir
	// would name another one after the first example.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return specOutcome{failures: []string{err.Error()}}
	}
	names, err := specChapters(dir)
	if err != nil {
		return specOutcome{failures: []string{err.Error()}}
	}
	if len(names) == 0 {
		return specOutcome{failures: []string{"no Markdown files in " + dir}}
	}
	var failures, logs, fragments []string
	total := 0
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: a spec file
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		rep := checkSpecChapter(t, name, string(data), run)
		failures = append(failures, rep.problems...)
		for _, s := range rep.skipped {
			logs = append(logs, "not built: "+s)
		}
		fragments = append(fragments, rep.fragments...)
		total += rep.examples
	}
	logs = append(logs, fmt.Sprintf("%d tack blocks in %s; fragments, not compiled: %d (%s)",
		total, dir, len(fragments), strings.Join(fragments, ", ")))
	return specOutcome{failures: failures, logs: logs}
}

// checkSpecExample checks one example of the spec, ex, against its kind,
// and returns what is wrong with it, and, when a run example cannot be
// built because run says clang is not on PATH, why. It runs --check on
// every example, and builds and runs a run example that passes it, its
// binary allowed run's timeout. t is used only to run the driver.
func checkSpecExample(t *testing.T, ex specExample, run specRunner) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, specFile), []byte(ex.src), 0o600); err != nil {
		return []string{err.Error()}, ""
	}
	// The driver shows a file below the current directory by its relative
	// path, so the diagnostics name main.tck wherever the test runs.
	t.Chdir(dir)
	problems := judgeCheck(ex, runTack(t, "--check", "--max-errors", "0", specFile))
	if ex.kind != specRun || len(problems) != 0 {
		return problems, ""
	}
	if !run.clang {
		return nil, "clang not on PATH"
	}
	bin := filepath.Join(dir, "prog")
	if problems := judgeBuild(runTack(t, "--out", bin, specFile)); len(problems) != 0 {
		return problems, ""
	}
	return judgeRun(ex, runSpecBinary(t.Context(), bin, run.timeout)), ""
}

// judgeCheck returns what is wrong with r, the --check run of the example
// ex: anything on stdout; for a check or run example, an exit status
// other than 0 or any diagnostic; for an error or warn example, an exit
// status other than 1 or 0, a line that is not part of a diagnostic, or
// diagnostics other than those the example's diagnostics block holds.
func judgeCheck(ex specExample, r result) []string {
	var problems []string
	if r.stdout != "" {
		problems = append(problems, fmt.Sprintf("--check wrote to stdout:\n%s", r.stdout))
	}
	switch ex.kind {
	case specCheck, specRun:
		if r.code != exitOK || r.stderr != "" {
			problems = append(problems, fmt.Sprintf(
				"--check exited %d, want %d and no diagnostic; it reported:\n%s",
				r.code, exitOK, r.stderr))
		}
	case specError, specWarn:
		problems = append(problems, judgeDiagnostics(ex, r)...)
	}
	return problems
}

// judgeDiagnostics returns what is wrong with r, the --check run of the
// error or warn example ex, beyond its stdout.
func judgeDiagnostics(ex specExample, r result) []string {
	var problems []string
	want := exitFailure
	if ex.kind == specWarn {
		want = exitOK
	}
	if r.code != want {
		problems = append(problems, fmt.Sprintf(
			"--check exited %d, want %d; it reported:\n%s", r.code, want, r.stderr))
	}
	got, unexpected := diagnosticLines(r.stderr)
	for _, line := range unexpected {
		problems = append(problems,
			fmt.Sprintf("--check wrote a line that is not part of a diagnostic: %q", line))
	}
	if got != ex.expect {
		problems = append(problems, fmt.Sprintf(
			"diagnostics differ from the block after the example\n\n"+
				"--- want ---\n%s--- got ---\n%s--- in full ---\n%s", ex.expect, got, r.stderr))
	}
	return problems
}

// judgeBuild returns what is wrong with r, the build of a run example:
// an exit status other than 0, or anything on stderr.
func judgeBuild(r result) []string {
	if r.code == exitOK && r.stderr == "" {
		return nil
	}
	return []string{fmt.Sprintf("build exited %d, want %d and no diagnostic; it reported:\n%s",
		r.code, exitOK, r.stderr)}
}

// specRunResult is what a run example's binary did.
type specRunResult struct {
	err            error         // why it could not be run, if it could not
	stdout, stderr string        // what it wrote
	timeout        time.Duration // how long it was allowed
	code           int           // its exit status
	timedOut       bool          // whether it was stopped for running past timeout
}

// judgeRun returns what is wrong with r, what the binary of the run
// example ex did: that it could not be run, ran past its timeout, wrote
// to stderr, exited with a status other than the example's, or, when the
// example pins it, wrote other than its output block to stdout.
func judgeRun(ex specExample, r specRunResult) []string {
	switch {
	case r.err != nil:
		return []string{fmt.Sprintf("the program could not be run: %v", r.err)}
	case r.timedOut:
		return []string{fmt.Sprintf("the program did not finish within %s", r.timeout)}
	}
	var problems []string
	if r.stderr != "" {
		problems = append(problems, fmt.Sprintf("the program wrote to stderr:\n%s", r.stderr))
	}
	if r.code != ex.exit {
		problems = append(problems, fmt.Sprintf("the program exited %d, want %d", r.code, ex.exit))
	}
	if ex.hasExp && r.stdout != ex.expect {
		problems = append(problems, fmt.Sprintf(
			"the program wrote\n%q\nwant, from the output block,\n%q", r.stdout, ex.expect))
	}
	return problems
}

// runSpecBinary runs the binary bin and returns what it did. A run
// finishes when the binary has exited and its stdout and stderr have both
// closed, which a process it started may hold open after it exits; all
// the output written until then is captured, whatever the exit status. A
// run that has not finished timeout after it started, or when ctx is
// done, is stopped and reported as timed out, with the output written
// before then: its pipes are closed.
//
// No process the binary starts outlives the run, or the test process:
//
//   - The binary runs in a process group of its own, which is killed with
//     SIGKILL once the run has ended either way, so a process it started
//     dies with it unless it left the group.
//   - Its own group puts it out of reach of a terminal's Ctrl-C, so from
//     the first run on until it ends the test process catches SIGINT and
//     SIGTERM, unless Go reports one ignored, kills every running binary's
//     group, and then ends as the signal would have ended it (see
//     watchSpecSignals). A
//     binary is recorded for this under the same lock as it starts, so a
//     signal at its start still finds it (see startSpecTracked).
//   - On Linux the binary is also killed when the test process dies any
//     other way, by SIGKILL or a test timeout's panic: it gets SIGKILL
//     when the thread that started it ends, and runSpecBinary keeps its
//     goroutine on that thread for the whole run (see specParentDeath).
//     Processes the binary started are not covered there: they outlive a
//     test process killed so. On macOS, which has no such signal, the
//     binary outlives it too.
//   - On Windows none of this applies: there are no process groups or
//     signals to use, so only the binary itself is killed at its deadline,
//     and what it started, and the binary when the test process dies,
//     outlive it.
//
// A group is killed after its leader is reaped on a run that finished,
// since the leader's exit status must be read first; were every process
// of the group gone and its ID given to a new group in between, that
// group would be killed. Linux gives no new process an ID still in use as
// a group's, so this needs the whole group gone and the ID reused within
// that moment.
func runSpecBinary(ctx context.Context, bin string, timeout time.Duration) specRunResult {
	// The binary's parent-death signal is tied to the thread that starts it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := specRunResult{timeout: timeout}
	b := &specBinary{
		cmd:    exec.Command(bin),
		copies: make(chan struct{}, 2),
		waited: make(chan error, 1),
	} //nolint:gosec // G204: the binary the test built
	if err := b.start(); err != nil {
		r.err = err
		return r
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	finished, err := b.await(ctx, deadline.C)
	if !finished {
		b.stop()
	}
	killSpecGroup(b.cmd)
	untrackSpecGroup(b.cmd)
	r.stdout, r.stderr, r.timedOut = b.out.String(), b.errOut.String(), !finished
	var exitErr *exec.ExitError
	switch {
	case !finished, err == nil:
	case errors.As(err, &exitErr):
		r.code = exitErr.ExitCode()
	default:
		r.err = err
	}
	return r
}

// specBinary is a run example's binary being run by runSpecBinary.
type specBinary struct {
	cmd            *exec.Cmd
	stdout, stderr io.ReadCloser // the binary's pipes, read by two copies
	copies         chan struct{} // each copy sends once its pipe has closed
	waited         chan error    // cmd.Wait's result, once a waiter is started
	out, errOut    bytes.Buffer  // what the binary wrote to stdout and stderr
	pending        int           // how many copies have not ended
	waiting        bool          // whether a waiter on cmd has started
}

// start starts the binary, and a copy from each of its pipes.
func (b *specBinary) start() error {
	var err error
	if b.stdout, err = b.cmd.StdoutPipe(); err != nil {
		return fmt.Errorf("stdout of %s: %w", b.cmd.Path, err)
	}
	if b.stderr, err = b.cmd.StderrPipe(); err != nil {
		return fmt.Errorf("stderr of %s: %w", b.cmd.Path, err)
	}
	specOwnGroup(b.cmd)
	if err := startSpecTracked(b.cmd); err != nil {
		return err
	}
	b.pending = 2
	go copyUntilClosed(&b.out, b.stdout, b.copies)
	go copyUntilClosed(&b.errOut, b.stderr, b.copies)
	return nil
}

// await waits for the run to finish: both pipes closed, and then the
// binary exited. It returns true and the binary's exit error when that
// happens before deadline fires or ctx is done, and false otherwise. It
// waits on the binary only once both pipes are closed, as StdoutPipe
// asks.
func (b *specBinary) await(ctx context.Context, deadline <-chan time.Time) (bool, error) {
	for {
		if b.pending == 0 && !b.waiting {
			b.waiting = true
			go func() { b.waited <- b.cmd.Wait() }()
		}
		select {
		case <-b.copies:
			b.pending--
		case err := <-b.waited:
			return true, err
		case <-deadline:
			return false, nil
		case <-ctx.Done():
			return false, nil
		}
	}
}

// stop stops a binary that has not finished: it kills its process group
// and the binary, before reaping it, so the group's ID cannot have been
// given to another; closes both pipes, so the copies end even when a
// process it started still holds them; and waits for the copies and for
// the binary.
func (b *specBinary) stop() {
	killSpecGroup(b.cmd)
	_ = b.stdout.Close() // closing a pipe after its copy ended is no matter
	_ = b.stderr.Close()
	for ; b.pending > 0; b.pending-- {
		<-b.copies
	}
	if !b.waiting {
		b.waiting = true
		go func() { b.waited <- b.cmd.Wait() }()
	}
	<-b.waited //nolint:errcheck // the run timed out, so how the binary ended is not reported
}

// copyUntilClosed copies from, one of a binary's pipes, into to until the
// pipe is closed, by the binary or by stop, and then sends on copies.
func copyUntilClosed(to *bytes.Buffer, from io.Reader, copies chan<- struct{}) {
	_, _ = io.Copy(to, from) // its error is only the pipe closed under it, an end like EOF
	copies <- struct{}{}
}

// specIndexProblems returns what is wrong with dir, a directory of the
// spec: an entry that is a directory or not a .md file; a chapter its
// index, specIndex, does not link; a link from the index to a chapter
// that does not exist; and a file other than the index not named
// NN-name.md. It returns an error when dir or its index cannot be read.
func specIndexProblems(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var problems []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			problems = append(problems, fmt.Sprintf(
				"%s: docs/spec holds only the index and the chapters, .md files", e.Name()))
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, specIndex)) //nolint:gosec // G304: the spec's index
	if err != nil {
		return nil, fmt.Errorf("reading the index: %w", err)
	}
	chapters, err := specChapters(dir)
	if err != nil {
		return nil, err
	}
	return append(problems, specLinkProblems(string(data), chapters)...), nil
}

// specIndexLinks returns the chapters index, the content of the spec's
// index, links: the file name of each line specIndexLine matches that is
// outside every fenced block scanFences finds, and that starts the file,
// follows a blank line, or follows another line counted so.
//
// Each such line renders as a link to its chapter, under the spec's
// dialect. Starting at column 0 after a blank line, or right after a
// sibling item, it starts a new block, an ordered list item: no code
// span, link, image, title or reference definition begun on an earlier
// line can reach into it, since none crosses a blank line or a list item
// that starts a block, and no block quote, list item or code block
// indented on an earlier line contains a line at column 0 after a blank
// line. One space after the marker keeps its content a paragraph, not an
// indented code block. That content is one inline link. Its text holds
// only letters, digits, spaces and , . : ; ( ) ' & -: no bracket, so no
// other link, image or footnote; no backtick, backslash, <, >, *, _, ~ or
// $, so no code span, escape, autolink, HTML, emphasis, strikethrough or
// math; no |, without which no line around it makes a GitHub table of it;
// no / or @, which GitHub's URL and email autolinks need; and no www.,
// which its www autolinks start with. A & may start an entity reference,
// which renders as its character, inside the link. Its destination, two
// digits, a dash, lowercase letters, digits and dashes and .md, needs no
// escaping. Nothing after the closing parenthesis can change what came
// before it. Any other mention of a chapter in the index, a link or not,
// counts for nothing.
func specIndexLinks(index string) map[string]bool {
	lines := strings.Split(index, "\n")
	inFence := make([]bool, len(lines))
	fences, _ := scanFences(specIndex, index)
	for _, f := range fences {
		// The fence lines themselves never match specIndexLine and are
		// never blank, so marking them too changes nothing.
		for l := f.start; l <= f.end; l++ {
			inFence[l-1] = true
		}
	}
	linked := make(map[string]bool)
	startsBlock := true // whether a line here starts a block
	for i, line := range lines {
		m := specIndexLine.FindStringSubmatch(line)
		if inFence[i] || m == nil || !startsBlock ||
			strings.Contains(strings.ToLower(m[1]), "www.") {
			startsBlock = strings.TrimSpace(line) == ""
			continue
		}
		linked[m[2]] = true
	}
	return linked
}

// specLinkProblems returns what is wrong between index, the content of
// the spec's index, and chapters, the names of the Markdown files beside
// it, the index's among them: a file other than the index not named
// NN-name.md, a chapter the index does not link, and a link to a chapter
// that is not among them. Only a line specIndexLinks counts links a
// chapter.
func specLinkProblems(index string, chapters []string) []string {
	linked := specIndexLinks(index)
	var problems []string
	for _, name := range chapters {
		switch {
		case name == specIndex:
		case !specChapterName.MatchString(name):
			problems = append(problems, name+": a chapter's file is named NN-name.md")
		case !linked[name]:
			problems = append(problems, fmt.Sprintf("%s does not link the chapter %s",
				specIndex, name))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(linked)) {
		if !slices.Contains(chapters, name) {
			problems = append(problems, fmt.Sprintf("%s links %s, which does not exist",
				specIndex, name))
		}
	}
	return problems
}

// TestSpecIndex checks docs/spec with specIndexOutcome, reported
// through reportSpec: that its index,
// README.md, links every chapter, and only chapters that exist; that
// every other file there is a chapter, named NN-name.md; and that it
// holds nothing else, no other file and no directory, so no example sits
// where TestSpecExamples does not look.
func TestSpecIndex(t *testing.T) {
	reportSpec(t, specIndexOutcome(specDir))
}

// specIndexOutcome returns, as failures, what specIndexProblems finds
// wrong with dir, or the error it returns.
func specIndexOutcome(dir string) specOutcome {
	problems, err := specIndexProblems(dir)
	if err != nil {
		return specOutcome{failures: []string{err.Error()}}
	}
	return specOutcome{failures: problems}
}

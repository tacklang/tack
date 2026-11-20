package porttest

// This file is a copy of the reader of the language specification's
// examples from tackc's tack/spec_test.go, at the frozen tag go-frozen:
// its declarations from specIndex to exitOf, but specFile,
// specChapterName and specExcerpt, and specIndexLinks, byte for byte. The original is
// frozen, so the copy cannot drift from it; it is here because porttest
// cannot import a test file of package main. TestSpecParse compares what
// it reads with what the port's tools/spec --parse reads (see
// specparse_cmp_test.go).

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// specIndex is the specification's index, which links every chapter.
const specIndex = "README.md"

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

// specHeader matches the first line of a diagnostic the driver renders
// for specFile: its position, severity and message.
var specHeader = regexp.MustCompile(`^main\.tck:[0-9]+:[0-9]+: (error|warning|note): `)

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

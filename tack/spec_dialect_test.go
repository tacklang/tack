package main

import (
	"slices"
	"strings"
	"testing"
)

// mdQuotes turns runs of four and three single quotes into backticks.
var mdQuotes = strings.NewReplacer("''''", "````", "'''", "```")

// md returns s with every run of three single quotes turned into three
// backticks, and every run of four into four, so the Markdown of a
// dialect test can be written in a Go string.
func md(s string) string {
	return mdQuotes.Replace(s)
}

// specDialectCases are chapters outside the spec's dialect or its tagging
// rules, each with text that a problem parseSpecExamples reports for it
// must hold. Cases a, b and c were each shown, on GitHub, to render a
// block where an earlier, looser scanner saw none, or none where it saw
// one; the others each fail when the rule they name is removed.
var specDialectCases = []struct {
	name, doc, want string
}{
	{
		"case a: a no-break space after a closing fence",
		md("'''tack check\npackage main\n\nfunc main() {}\n''' \nfunc bad() {}\n'''\n"),
		"5:4: byte 0xc2",
	},
	{
		"case b: an entity in an info string",
		md("'''&#116;ack run exit=1\npackage main\n\nfunc main() { exit(0) }\n'''\n"),
		"fence info string",
	},
	{
		"case c: carriage returns as line endings",
		"~~~text\r~~~\r~~~tack run exit=1\rpackage main\r~~~\n",
		"1:8: byte 0x0d",
	},
	{"a tab", "Text\twith a tab.\n", "1:5: byte 0x09"},
	{"a control byte", "Text\x01.\n", "1:5: byte 0x01"},
	{"a DEL byte", "Text\x7f.\n", "1:5: byte 0x7f"},
	{"a byte order mark", "\xef\xbb\xbf# Title\n", "1:1: byte 0xef"},
	{"a fence in a list item", md("- '''text\n  x\n  '''\n"), "three backticks"},
	{
		"a fence in a block quote",
		md("> '''tack check\n> package main\n> '''\n"),
		"three backticks",
	},
	{
		"a tilde fence in a block quote",
		"> ~~~tack check\n> bad\n> ~~~\n",
		"three backticks or tildes",
	},
	{
		"an indented fence",
		md("    '''tack check\n    package main\n    '''\n"),
		"three backticks",
	},
	{"inline triple backticks", md("See '''x''' here.\n"), "three backticks"},
	{"an HTML block", "<div>\n\nText.\n\n</div>\n", "could be raw HTML"},
	{
		"an HTML comment hiding a fence",
		md("<!--\n'''text\n-->\n'''tack check\nbad\n'''\n"),
		"1: a < followed by a letter, /, ! or ? could be raw HTML",
	},
	{"an HTML comment in a list item", "- <!--\n- -->\n", "1: a < followed by"},
	{"a tag in a list item", "- <pre lang=\"tack\">x</pre>\n", "1: a < followed by"},
	{"an inline tag", "Text with <pre>x</pre> in it.\n", "1: a < followed by"},
	{"a processing instruction", "Text <?x?>.\n", "1: a < followed by"},
	{"an end tag", "Text </x>.\n", "1: a < followed by"},
	{"an uppercase tag", "Text <DIV>.\n", "1: a < followed by"},
	{"front matter", "---\ntitle: x\n---\n\n# Title\n", "1: a first line of ---"},
	{"a math block", "$$\n\n1. [A](01-a.md)\n\n$$\n", "1: a line starting with $$"},
	{"an indented math block", "Text.\n\n  $$x$$\n", "3: a line starting with $$"},
	{"a math line with HTML", "$$ <b>\n", "1: a line starting with $$"},
	{"a math block in a list item", "- a\n\n    $$\n    x\n    $$\n", "3: a line starting with $$"},
	{"front matter with a trailing space", "---  \ntitle: x\n---\n", "1: a first line of ---"},
	{"an unclosed block", md("'''text\nx\n"), "never closed"},
	{
		"a closing fence with spaces after it",
		md("'''text\nx\n'''  \n"),
		"closing fence is exactly",
	},
	{"a longer closing fence", md("'''text\nx\n''''\n"), "closing fence is exactly"},
	{"an indented closing fence", md("'''text\nx\n  '''\n'''\n"), "an indented fence"},
	{
		"an indented longer closing fence",
		md("'''text\nx\n  ''''\n'''\n"),
		"an indented fence",
	},
	{"a Tack near miss", md("'''Tack check\npackage main\n'''\n"), "fence info string"},
	{"a tackrun near miss", md("'''tackrun\npackage main\n'''\n"), "fence info word \"tackrun\""},
	{"a tck near miss", md("'''tck check\npackage main\n'''\n"), "fence info word \"tck\""},
	{"a tak near miss", md("'''tak check\npackage main\n'''\n"), "fence info word \"tak\""},
	{"another language", md("'''go\npackage main\n'''\n"), "fence info word \"go\""},
	{
		"a bad info string still opens a block",
		md("'''Tack check\n<div>\n'''\n"),
		"1: fence info string",
	},
	{"punctuation in an info string", md("'''tack,run exit=0\n'''\n"), "fence info string"},
	{"a leading space in an info string", md("''' tack check\n'''\n"), "fence info string"},
	{"a trailing space in an info string", md("'''tack check \n'''\n"), "fence info string"},
	{"two spaces in an info string", md("'''tack  check\n'''\n"), "fence info string"},
	{"an uppercase attribute", md("'''tack run EXIT=0\n'''\n"), "fence info string"},
	{"an untagged tack block", md("'''tack\npackage main\n'''\n"), "untagged tack block"},
	{"an unknown kind", md("'''tack build\npackage main\n'''\n"), "unknown kind"},
	{
		"an attribute on a check block",
		md("'''tack check x\npackage main\n'''\n"),
		"a tack check block takes no attributes",
	},
	{
		"a run block without exit",
		md("'''tack run\npackage main\n'''\n"),
		"exactly one attribute",
	},
	{
		"a run block with two attributes",
		md("'''tack run exit=0 exit=1\npackage main\n'''\n"),
		"exactly one attribute",
	},
	{
		"an exit status with a suffix",
		md("'''tack run exit=3x\npackage main\n'''\n"),
		"want exit=N",
	},
	{
		"an exit status with a prefix",
		md("'''tack run xexit=3\npackage main\n'''\n"),
		"want exit=N",
	},
	{
		"an exit status past 255",
		md("'''tack run exit=256\npackage main\n'''\n"),
		"want 0 to 255",
	},
	{
		"an output block after nothing",
		md("Text.\n\n'''output\nhi\n'''\n"),
		"output block: it must directly follow",
	},
	{
		"a diagnostics block after nothing",
		md("Text.\n\n'''diagnostics\nmain.tck:1:1: error: x\n'''\n"),
		"diagnostics block: it must directly follow",
	},
	{
		"an attribute on an output block",
		md("'''tack run exit=0\nx\n'''\n\n'''output x\nhi\n'''\n"),
		"the output block of a tack block takes no attributes",
	},
	{
		"an attribute on a diagnostics block",
		md("'''tack error\nx\n'''\n\n'''diagnostics x\nmain.tck:1:1: error: y\n'''\n"),
		"the diagnostics block of a tack block takes no attributes",
	},
	{
		"a text block directly after a run block",
		md("'''tack run exit=0\npackage main\n'''\n\n'''text\nhi\n'''\n"),
		"directly after a tack run block",
	},
	{
		"a tack block directly after a fragment",
		md("'''tack fragment\nx\n'''\n\n'''tack fragment\ny\n'''\n"),
		"directly after a tack fragment block",
	},
	{
		"an error block without diagnostics",
		md("'''tack error\npackage main\n'''\n"),
		"followed directly by a diagnostics block",
	},
	{
		"an error block with text before its diagnostics",
		md("'''tack error\nx\n'''\n\nText.\n\n'''diagnostics\nmain.tck:1:1: error: y\n'''\n"),
		"followed directly by a diagnostics block",
	},
	{
		"an error block with an empty diagnostics block",
		md("'''tack error\npackage main\n'''\n\n'''diagnostics\n'''\n"),
		"must hold at least one error",
	},
	{
		"an error block whose only error is in a note's text",
		md("'''tack error\npackage main\n'''\n\n'''diagnostics\n" +
			"main.tck:1:1: note: x: error: y\n'''\n"),
		"must hold at least one error",
	},
	{
		"a warn block whose diagnostics are only errors",
		md("'''tack warn\npackage main\n'''\n\n'''diagnostics\n" +
			"main.tck:1:1: error: x\n'''\n"),
		"must hold at least one warning",
	},
}

// TestSpecDialectRejects checks that parseSpecExamples reports each
// chapter of specDialectCases, with a problem holding the case's text.
func TestSpecDialectRejects(t *testing.T) {
	for _, tc := range specDialectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := parseSpecExamples("t.md", tc.doc)
			for _, p := range problems {
				if strings.Contains(p, tc.want) {
					return
				}
			}
			t.Errorf("no problem holds %q; the problems are %q", tc.want, problems)
		})
	}
}

// specAcceptedCases are chapters inside the dialect, each with exactly
// the examples parseSpecExamples must find in it, every field given.
var specAcceptedCases = []struct {
	name string
	doc  string
	want []specExample
}{
	{
		"every kind, and a longer fence quoting a tack block",
		md("# Title\n\nUse `x` inline, ``a`b`` too, and a < b.\n\n" +
			"'''tack run exit=3\nrun src\n'''\n\n'''output\nhi\n'''\n\nText.\n\n" +
			"'''tack check\ncheck src\n'''\n\nText.\n\n" +
			"'''tack error\nerror src\n'''\n\n'''diagnostics\n" +
			"main.tck:1:1: error: x\nmain.tck:2:1: note: y\n'''\n\n" +
			"Text.\n\n~~~tack warn\nwarn src\n~~~\n\n~~~diagnostics\n" +
			"main.tck:1:1: warning: x\n~~~\n\nText.\n\n'''tack fragment\nx := 1\n'''\n\n" +
			"Text.\n\n'''tack run exit=0\nquiet\n'''\n\n" +
			"Text.\n\n''''markdown\n'''tack error\nnot checked\n'''\n''''\n"),
		[]specExample{
			{pos: "t.md:5", kind: specRun, src: "run src\n", exit: 3, expect: "hi\n", hasExp: true},
			{pos: "t.md:15", kind: specCheck, src: "check src\n"},
			{
				pos: "t.md:21", kind: specError, src: "error src\n", hasExp: true,
				expect: "main.tck:1:1: error: x\nmain.tck:2:1: note: y\n",
			},
			{
				pos: "t.md:32", kind: specWarn, src: "warn src\n", hasExp: true,
				expect: "main.tck:1:1: warning: x\n",
			},
			{pos: "t.md:42", kind: specFragment, src: "x := 1\n"},
			{pos: "t.md:48", kind: specRun, src: "quiet\n"},
		},
	},
	{
		"a tilde line inside a backtick block does not close it",
		md("'''text\n~~~\n'''\n\nText.\n\n'''tack check\nbad\n'''\n"),
		[]specExample{{pos: "t.md:7", kind: specCheck, src: "bad\n"}},
	},
	{
		"a shorter run inside a block does not close it",
		md("''''text\n'''\n''''\n\nText.\n\n'''tack check\nbad\n'''\n"),
		[]specExample{{pos: "t.md:7", kind: specCheck, src: "bad\n"}},
	},
	{
		"a run followed by text inside a block does not close it",
		md("'''text\n'''x\n'''\n\nText.\n\n'''tack check\nbad\n'''\n"),
		[]specExample{{pos: "t.md:7", kind: specCheck, src: "bad\n"}},
	},
	{
		"an indented line of other text inside a block is content",
		md("'''tack fragment\n  x\n'''\n"),
		[]specExample{{pos: "t.md:1", kind: specFragment, src: "  x\n"}},
	},
	{
		"an indented run shorter than the fence inside a block is content",
		md("'''tack fragment\n  ``\n'''\n"),
		[]specExample{{pos: "t.md:1", kind: specFragment, src: "  ``\n"}},
	},
	{
		"a run of two backticks at column 0 is inline code",
		md("``a`` is inline.\n\n'''tack check\nbad\n'''\n"),
		[]specExample{{pos: "t.md:3", kind: specCheck, src: "bad\n"}},
	},
	{
		"an exit status of 255",
		md("'''tack run exit=255\nx\n'''\n"),
		[]specExample{{pos: "t.md:1", kind: specRun, src: "x\n", exit: 255}},
	},
	{
		"a dash line that is not the first",
		"# Title\n\n---\n",
		nil,
	},
	{"a line starting with one dollar", "$5 is a price.\n", nil},
	{
		"a dash line with a trailing space that is not the first",
		"# Title\n\n--- \n",
		nil,
	},
	{
		"a block with an empty info string",
		md("'''\nplain\n'''\n\nText.\n\n'''tack check\nbad\n'''\n"),
		[]specExample{{pos: "t.md:7", kind: specCheck, src: "bad\n"}},
	},
}

// TestSpecDialectAccepts checks that each chapter of specAcceptedCases
// gives no problem and exactly its examples.
func TestSpecDialectAccepts(t *testing.T) {
	for _, tc := range specAcceptedCases {
		t.Run(tc.name, func(t *testing.T) {
			examples, problems := parseSpecExamples("t.md", tc.doc)
			if len(problems) != 0 {
				t.Errorf("problems in a chapter inside the dialect: %q", problems)
			}
			if !slices.Equal(examples, tc.want) {
				t.Errorf("examples\n%+v\nwant\n%+v", examples, tc.want)
			}
		})
	}
}

// TestSpecBadInfoOpensBlock checks that an opening fence whose info string
// is a problem still opens a block, as a renderer does, so its content is
// not read as prose: the HTML line inside is not reported.
func TestSpecBadInfoOpensBlock(t *testing.T) {
	_, problems := parseSpecExamples("t.md", md("'''Tack check\n<div>\n'''\n"))
	if len(problems) != 1 || !strings.Contains(problems[0], "t.md:1: fence info string") {
		t.Errorf("problems %q, want only the info string's", problems)
	}
}

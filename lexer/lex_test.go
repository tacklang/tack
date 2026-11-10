package lexer_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
)

// lexed is everything one Lex run produced.
type lexed struct {
	file   *source.File
	tokens []lexer.Token
	diags  []diag.Diagnostic
}

// lex runs the lexer over src. A panic is turned into a test failure naming
// the input, since bad input must never crash the lexer.
func lex(t *testing.T, src string) (out lexed) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("lexer panicked on %q: %v", src, r)
		}
	}()

	f := &source.File{ID: "test", Filename: "case.tck", Data: []byte(src)}
	l := lexer.New(f)
	diags := l.Lex()
	return lexed{file: f, tokens: l.Tokens(), diags: diags}
}

// checkInvariants fails the test if a lex run broke a guarantee that must hold
// for every input, valid or not: tokens in order, inside the file, ending in
// EOF; diagnostic spans inside the file; line starts that match the newlines;
// and diagnostics that render without crashing.
func checkInvariants(t *testing.T, src string, out lexed) {
	t.Helper()

	checkTokenOrder(t, src, out.tokens)
	checkCommentSpans(t, src, out.tokens)
	checkRawStrings(t, src, out.tokens)
	checkDiagSpans(t, src, out.diags)
	checkLineStarts(t, src, out.file)
	checkRendering(t, src, out)
}

// checkCommentSpans fails the test unless every comment token covers its
// source exactly. A line comment starts with "//" and stops right before a
// newline, \n or \r\n, or at the end of src, so it neither drops its last
// byte nor swallows the newline, and every line it covers must start with
// "//" past its indentation, so it never runs across a blank line. A block
// comment is checked by checkBlockComment.
func checkCommentSpans(t *testing.T, src string, toks []lexer.Token) {
	t.Helper()

	for i := range toks {
		tok := &toks[i]
		if tok.Kind() != lexer.Comment {
			continue
		}
		start, end := tok.OffsetStart(), tok.OffsetEnd()
		if start < 0 || end > len(src) || start > end {
			t.Errorf("%q: comment span [%d:%d] is outside the source", src, start, end)
			continue
		}
		text := src[start:end]
		if strings.HasPrefix(text, "/*") {
			checkBlockComment(t, src, start, end)
			continue
		}
		if !strings.HasPrefix(text, "//") || strings.HasSuffix(text, "\n") {
			t.Errorf("%q: comment span [%d:%d] covers %q", src, start, end, text)
		}
		if !atLineEnd(src, end) {
			t.Errorf("%q: comment span [%d:%d] stops before the end of its line", src, start, end)
		}
		checkCommentLines(t, src, text)
	}
}

// checkBlockComment fails the test unless the block comment src[start:end]
// ends at the "*/" that closes its first "/*", counting the ones nested in
// it, or, never closed, runs to the end of src.
func checkBlockComment(t *testing.T, src string, start, end int) {
	t.Helper()
	depth, i := 1, start+2
	for i < len(src) && depth > 0 {
		switch {
		case strings.HasPrefix(src[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(src[i:], "*/"):
			depth--
			i += 2
		default:
			i++
		}
	}
	if i != end {
		t.Errorf("%q: block comment span [%d:%d] should end at %d", src, start, end, i)
	}
}

// atLineEnd reports whether offset i of src is where a line ends: at a
// newline, \n or \r\n, or at the end of src.
func atLineEnd(src string, i int) bool {
	return i >= len(src) || src[i] == '\n' || strings.HasPrefix(src[i:], "\r\n")
}

// checkRawStrings fails the test unless every backquoted string token's value
// is exactly the bytes it covers between its backquotes, carriage returns
// removed: a raw string decodes nothing. An unterminated one, which runs to
// the end of src, has no closing backquote to drop.
func checkRawStrings(t *testing.T, src string, toks []lexer.Token) {
	t.Helper()

	for i := range toks {
		tok := &toks[i]
		start, end := tok.OffsetStart(), tok.OffsetEnd()
		if !isRawString(src, tok) {
			continue
		}
		body := src[start+1 : end]
		if end-start >= 2 && src[end-1] == '`' {
			body = src[start+1 : end-1]
		} else if end != len(src) {
			t.Errorf("%q: raw string [%d:%d] ends before EOF without a backquote", src, start, end)
		}
		if want := strings.ReplaceAll(body, "\r", ""); tok.Value() != want {
			t.Errorf("%q: raw string [%d:%d] = %#v, want %#v", src, start, end, tok.Value(), want)
		}
	}
}

// isRawString reports whether tok is a string token whose span lies inside
// src and starts with a backquote.
func isRawString(src string, tok *lexer.Token) bool {
	start, end := tok.OffsetStart(), tok.OffsetEnd()
	return tok.Kind() == lexer.Str && start >= 0 && start < end && end <= len(src) &&
		src[start] == '`'
}

// checkCommentLines fails the test unless every line of the comment text
// starts with "//" once its leading spaces and tabs are skipped, so the
// comment does not run across a blank line, a line holding only spaces and
// tabs, or a line of code.
func checkCommentLines(t *testing.T, src, text string) {
	t.Helper()

	for line := range strings.SplitSeq(text, "\n") {
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "//") {
			t.Errorf("%q: comment %q covers the line %q", src, text, line)
		}
	}
}

// checkEndsInEOF fails the test unless toks ends in an EOF token at the end
// of src.
func checkEndsInEOF(t *testing.T, src string, toks []lexer.Token) {
	t.Helper()

	n := len(src)
	if len(toks) == 0 {
		t.Fatalf("%q: no tokens, want at least EOF", src)
	}
	last := toks[len(toks)-1]
	if last.Kind() != lexer.EOF || last.OffsetStart() != n || last.OffsetEnd() != n {
		t.Errorf("%q: last token is %s, want EOF at [%d:%d]", src, last.String(), n, n)
	}
}

// checkTokenOrder fails the test unless toks is a non-empty run of tokens
// inside src, in order, not overlapping, with exactly one EOF at the end of
// the input.
func checkTokenOrder(t *testing.T, src string, toks []lexer.Token) {
	t.Helper()

	checkEndsInEOF(t, src, toks)

	n := len(src)
	for i := range toks {
		tok := &toks[i]
		if tok.OffsetStart() < 0 || tok.OffsetStart() > tok.OffsetEnd() || tok.OffsetEnd() > n {
			t.Errorf("%q: token %s is outside [0:%d] or inverted", src, tok.String(), n)
		}
		if tok.Kind() == lexer.EOF && i != len(toks)-1 {
			t.Errorf("%q: EOF at index %d of %d", src, i, len(toks))
		}
		if i > 0 && tok.OffsetStart() < toks[i-1].OffsetEnd() {
			t.Errorf("%q: token %s overlaps %s", src, tok.String(), toks[i-1].String())
		}
	}
}

// checkDiagSpans fails the test if a diagnostic's span is inverted or reaches
// outside src.
func checkDiagSpans(t *testing.T, src string, diags []diag.Diagnostic) {
	t.Helper()

	for _, d := range diags {
		if d.Span.Start < 0 || d.Span.Start > d.Span.End || d.Span.End > len(src) {
			t.Errorf("%q: diagnostic %q has span [%d:%d], outside [0:%d]",
				src, d.Message, d.Span.Start, d.Span.End, len(src))
		}
	}
}

// checkLineStarts fails the test unless the lexer recorded a line start at 0
// and one past every newline in src, and nowhere else.
func checkLineStarts(t *testing.T, src string, f *source.File) {
	t.Helper()

	want := []int{0}
	for i := range len(src) {
		if src[i] == '\n' {
			want = append(want, i+1)
		}
	}
	if got := f.LineStarts(); !equalInts(got, want) {
		t.Errorf("%q: line starts = %v, want %v", src, got, want)
	}
}

// checkRendering fails the test if rendering the run's diagnostics panics.
func checkRendering(t *testing.T, src string, out lexed) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%q: rendering diagnostics panicked: %v", src, r)
		}
	}()
	_ = diag.RenderAllPretty(out.file, out.diags)
}

// equalInts reports whether a and b hold the same ints in the same order.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// kinds renders the token kinds of a run as one space-separated string, with
// inserted semicolons shown as "<semi>" so they are told apart from written
// ones.
func kinds(toks []lexer.Token) string {
	parts := make([]string, len(toks))
	for i := range toks {
		tok := &toks[i]
		if tok.Kind() == lexer.Semi && tok.OffsetStart() == tok.OffsetEnd() {
			parts[i] = "<semi>"
			continue
		}
		parts[i] = tok.Kind().String()
	}
	return strings.Join(parts, " ")
}

// messages returns the diagnostic messages of a run, in order.
func messages(diags []diag.Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = d.Message
	}
	return out
}

// adversarialInputs are inputs aimed at the lexer's edges: empty and
// truncated input, every literal cut off at every point, stray and non-ASCII
// bytes, and recovery paths that move the position by hand.
var adversarialInputs = []string{
	"",
	"?",
	"??\n?",
	"f()?\n",
	"f() ?.x?[0]?",
	"\n",
	"\n\n\n",
	" \t ",
	"x",
	"x\n",
	"0",
	"9223372036854775808",
	"1.",
	"1..2",
	"1.2.3",
	".5",
	`"`,
	`""`,
	`"abc`,
	"\"abc\n",
	`"\`,
	`"\"`,
	`"\q"`,
	"`",
	"`raw\nstring`",
	"`unterminated\n",
	"`\\`",
	"`\\``",
	"`a\r\nb`",
	"`\r",
	"`\r\n",
	"`a\\",
	"`\n\n\n",
	"``\n``",
	"`\"` \"`\"",
	"'",
	"''",
	"'a",
	"'a'",
	"'ab",
	"'ab'",
	"'ab\nx",
	`'\`,
	`'\'`,
	`'\''`,
	`'\n'`,
	`'\q'`,
	"'é'",
	"'é",
	"'éé'",
	"'\xff'",
	"'\xe2\x82'",
	"'\xe2\x82\xac",
	"/",
	"//",
	"// c",
	"// c\n",
	"// a\n// b\n",
	"/*",
	"/* c */",
	"x /* c */ y",
	"/* a\nb */\nx",
	"/*/",
	"*/",
	"asm",
	"asm {",
	"asm { mov }",
	"asm {\n\tmov\n}\nx",
	"@",
	"\x00",
	"\xff",
	"é",
	"x\r\ny",
	"x\r",
	"\"\\x",
	"\"\\x4",
	"'\\x",
	"'\\x4",
	"'\\xff",
	"\r\n",
	"\r\r\n\n\r",
	"\"\r\n",
	"'\r\n",
	"'\\\r\n",
	"// a\r\n// b\r\n",
	"// a\r",
	"a.b",
	"...",
	"&^=",
	"<<=",
	">>=",
	"return\n",
	"package main\n\nfunc main() {\n    print(\"hi\")\n}\n",
}

// TestInvariantsHoldForAdversarialInput lexes every adversarial input and
// checks the guarantees that must hold however malformed the input is.
func TestInvariantsHoldForAdversarialInput(t *testing.T) {
	for _, src := range adversarialInputs {
		t.Run(src, func(t *testing.T) {
			checkInvariants(t, src, lex(t, src))
		})
	}
}

// TestInvariantsHoldForEveryExample lexes every program under examples/,
// valid or not.
func TestInvariantsHoldForEveryExample(t *testing.T) {
	paths, err := filepath.Glob("../examples/*/main.tck")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("found no example programs")
	}
	cases, err := invalid.Load(invalid.Path)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			checkInvariants(t, string(src), lex(t, string(src)))
		})
	}
	for _, c := range cases {
		t.Run("invalid/"+c.Name, func(t *testing.T) {
			checkInvariants(t, string(c.Source), lex(t, string(c.Source)))
		})
	}
}

// tokenKindCases are inputs whose correct lexing is known, with the token
// kinds each gives, including where semicolons are and are not inserted.
var tokenKindCases = []struct {
	name string
	src  string
	want string
}{
	{"empty", "", "EOF"},
	{"ident at eof gets a semicolon", "x", "ident <semi> EOF"},
	{"ident before newline gets one semicolon", "x\n\n\n", "ident <semi> EOF"},
	{"operator before newline gets none", "+\n", "+ EOF"},
	{"open paren gets none", "(\n", "( EOF"},
	{"close paren gets one", ")\n", ") <semi> EOF"},
	{"close brack gets one", "]\n", "] <semi> EOF"},
	{"close curl gets one", "}\n", "} <semi> EOF"},
	{"return gets one", "return\n", "keyword <semi> EOF"},
	{"break gets one", "break\n", "keyword <semi> EOF"},
	{"continue gets one", "continue\n", "keyword <semi> EOF"},
	{"func gets none", "func\n", "keyword EOF"},
	{"true gets one", "true\n", "keyword <semi> EOF"},
	{"false gets one", "false\n", "keyword <semi> EOF"},
	{"inc gets one", "x++\n", "ident ++ <semi> EOF"},
	{"dec gets one", "x--\n", "ident -- <semi> EOF"},
	{"int gets one", "1\n", "int <semi> EOF"},
	{"float gets one", "1.5\n", "float <semi> EOF"},
	{"string gets one", "\"s\"\n", "string <semi> EOF"},
	{"char gets one", "'c'\n", "char <semi> EOF"},
	{"raw string gets one", "`s`\n", "string <semi> EOF"},
	{
		"newlines inside a raw string get none",
		"x := `a\n\nb`\ny",
		"ident := string <semi> ident <semi> EOF",
	},
	{"raw string then tokens on its last line", "`a\nb` + c", "string + ident <semi> EOF"},
	{"comment marker inside a raw string", "`// c` x", "string ident <semi> EOF"},
	{"written semicolon", "x;", "ident ; EOF"},
	{"semicolon before a trailing comment", "x // c\n", "ident <semi> comment EOF"},
	{"block comment on one line is a space", "x /* c */ + y", "ident comment + ident <semi> EOF"},
	{
		"block comment on one line before a newline",
		"x /* c */\ny",
		"ident comment <semi> ident <semi> EOF",
	},
	{
		"block comment spanning lines is a newline",
		"x /* a\nb */ y",
		"ident <semi> comment ident <semi> EOF",
	},
	{
		"block comment spanning lines after an operator",
		"x + /* a\nb */ y",
		"ident + comment ident <semi> EOF",
	},
	{"block comment at the end", "x /* c */", "ident comment <semi> EOF"},
	{"nested block comment is one token", "/* a /* b */ c */ x", "comment ident <semi> EOF"},
	{
		"block then line comment",
		"x /* a */ // b\ny",
		"ident comment <semi> comment ident <semi> EOF",
	},
	{
		"block comment spanning crlf lines",
		"x /* a\r\nb */ y",
		"ident <semi> comment ident <semi> EOF",
	},
	{"longest operator wins", "<<= <<", "<<= << EOF"},
	{"and-not assign", "&^=", "&^= EOF"},
	{"wrapping operators", "a +% b -% c *% d", "ident +% ident -% ident *% ident <semi> EOF"},
	{"wrapping compound assignments", "+%= -%= *%=", "+%= -%= *%= EOF"},
	{"wrapping negation", "-%x", "-% ident <semi> EOF"},
	{"wrapping operators unspaced", "a+%b-%-%c", "ident +% ident -% -% ident <semi> EOF"},
	{"increment before a percent is not a wrap", "x++%y", "ident ++ % ident <semi> EOF"},
	{"remainder alone", "a % b %= c", "ident % ident %= ident <semi> EOF"},
	{"a wrapping operator before a newline gets none", "a +%\nb", "ident +% ident <semi> EOF"},
	{"wrapping compound beats increment", "x +%= 1", "ident +%= int <semi> EOF"},
	{"minus then wrap", "a - -%b", "ident - -% ident <semi> EOF"},
	{"ellipsis then period", "....", "... . EOF"},
	{"question after a call", "f(x)?", "ident ( ident ) ? <semi> EOF"},
	{"question spaced and doubled", "f() ? ?", "ident ( ) ? ? <semi> EOF"},
	{"question before a selector", "f()?.b[2]", "ident ( ) ? . ident [ int ] <semi> EOF"},
	{"question at a line's end gets a semicolon", "f()?\ng()", "ident ( ) ? <semi> ident ( ) <semi> EOF"},
	{"question before a comment gets a semicolon", "f()? // c\n", "ident ( ) ? <semi> comment EOF"},
	{"question alone", "?", "? <semi> EOF"},
	{"question before a call", "?f()", "? ident ( ) <semi> EOF"},
	{"define", "x := 1", "ident := int <semi> EOF"},
	{"arrow", "<-", "<- EOF"},
	{"digits in ident", "a1b2", "ident <semi> EOF"},
	{"underscore ident", "_", "ident <semi> EOF"},
	{"keyword prefix is an ident", "funcs", "ident <semi> EOF"},
	{"second dot starts another float", "1.2.3", "float float <semi> EOF"},
	{"crlf ends a line", "x\r\ny\r\n", "ident <semi> ident <semi> EOF"},
	{"a run of crlfs gets one semicolon", "x\r\n\r\n\r\ny", "ident <semi> ident <semi> EOF"},
	{"crlf and lf mixed", "x\n\r\n\ny\r\n", "ident <semi> ident <semi> EOF"},
	{"operator before crlf gets none", "+\r\nx", "+ ident <semi> EOF"},
	{"comment before crlf", "x // c\r\ny", "ident <semi> comment ident <semi> EOF"},
	{"string before crlf", "\"s\"\r\n'c'\r\n", "string <semi> char <semi> EOF"},
	{
		"crlf inside a raw string gets none",
		"x := `a\r\nb`\r\ny",
		"ident := string <semi> ident <semi> EOF",
	},
}

// TestTokenKinds pins the token stream for each of tokenKindCases.
func TestTokenKinds(t *testing.T) {
	for _, tt := range tokenKindCases {
		t.Run(tt.name, func(t *testing.T) {
			out := lex(t, tt.src)
			checkInvariants(t, tt.src, out)
			if got := kinds(out.tokens); got != tt.want {
				t.Errorf("kinds(%q) = %q, want %q", tt.src, got, tt.want)
			}
			if len(out.diags) > 0 {
				t.Errorf("%q: unexpected diagnostics %v", tt.src, messages(out.diags))
			}
		})
	}
}

// literalValueCases are literals of each kind, with the value each
// decodes to.
var literalValueCases = []struct {
	src  string
	want string
}{
	{`"hi"`, "hi"},
	{`""`, ""},
	{`"a\nb"`, "a\nb"},
	{`"\a\b\f\v\t\r\\"`, "\a\b\f\v\t\r\\"},
	{`"say \"hi\""`, `say "hi"`},
	{"`raw\nline`", "raw\nline"},
	{"``", ""},
	{"`\\n`", `\n`},
	{"`\\\"`", `\"`},
	{"`\\\\`", `\\`},
	{"`\\`", `\`},
	{"`a\\\nb`", "a\\\nb"},
	{"`\\t\\x41\\u00e9\\0`", `\t\x41\u00e9\0`},
	{"`say \"hi\" // not a comment`", `say "hi" // not a comment`},
	{"`'`", "'"},
	{"`\n`", "\n"},
	{"`\n\n`", "\n\n"},
	{"`one\ntwo\nthree`", "one\ntwo\nthree"},
	{"`a\r\nb\r\n`", "a\nb\n"},
	{"`\r`", ""},
	{"`a\rb\r\r`", "ab"},
	{"`\xff\x00\x80é`", "\xff\x00\x80é"},
	{"`\t`", "\t"},
	{"`/* not a comment */`", "/* not a comment */"},
	{"\"\xff\xfe\"", "\xff\xfe"},
	{"\"a\rb\r\"", "a\rb\r"},
	{`"\x00\xff\x41\xAb\x7F"`, "\x00\xffA\xab\x7f"},
	{`"\x410"`, "A0"},
	{`"\x5c\x22"`, `\"`},
	{`'\x00'`, "\x00"},
	{`'\xFF'`, "\xff"},
	{`'\x27'`, "'"},
	{"'\r'", "\r"},
	{"// a\r\n", "a"},
	{"\"é\"", "é"},
	{`'a'`, "a"},
	{`'\n'`, "\n"},
	{`'\''`, "'"},
	{`'"'`, `"`},
	{"'\xff'", "\xff"},
	{"'\x80'", "\x80"},
	{"'\xc3'", "\xc3"},
	{"'\x00'", "\x00"},
	{"0", "0"},
	{"70", "70"},
	{"0.5", "0.5"},
	{"00.5", "00.5"},
	{"9223372036854775808", "9223372036854775808"},
	{"1.25", "1.25"},
	{"x_1", "x_1"},
	{"// note", "note"},
}

// TestLiteralValues pins the decoded value of each of literalValueCases.
func TestLiteralValues(t *testing.T) {
	for _, tt := range literalValueCases {
		t.Run(tt.src, func(t *testing.T) {
			out := lex(t, tt.src)
			checkInvariants(t, tt.src, out)
			if len(out.diags) > 0 {
				t.Fatalf("%q: unexpected diagnostics %v", tt.src, messages(out.diags))
			}
			if got := out.tokens[0].Value(); got != tt.want {
				t.Errorf("value of %q = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}
}

// tokenSpan is a token's kind and the byte range it covers.
type tokenSpan struct {
	kind       lexer.TokenKind
	start, end int
}

// checkOffsets fails the test unless toks are exactly want, in order.
func checkOffsets(t *testing.T, toks []lexer.Token, want []tokenSpan) {
	t.Helper()
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens (%s), want %d", len(toks), kinds(toks), len(want))
	}
	for i, w := range want {
		tok := &toks[i]
		if tok.Kind() != w.kind || tok.OffsetStart() != w.start || tok.OffsetEnd() != w.end {
			t.Errorf("token %d = %s, want %s [%d:%d]", i, tok.String(), w.kind, w.start, w.end)
		}
	}
}

// TestTokenOffsets pins the byte range each token covers.
func TestTokenOffsets(t *testing.T) {
	out := lex(t, "func f() {\n\t\"hi\"\n}")
	checkInvariants(t, "func f() {\n\t\"hi\"\n}", out)

	checkOffsets(t, out.tokens, []tokenSpan{
		{lexer.Keyword, 0, 4},
		{lexer.Ident, 5, 6},
		{lexer.LParen, 6, 7},
		{lexer.RParen, 7, 8},
		{lexer.LCurl, 9, 10},
		{lexer.Str, 12, 16},
		{lexer.Semi, 16, 16},
		{lexer.RCurl, 17, 18},
		{lexer.Semi, 18, 18},
		{lexer.EOF, 18, 18},
	})
}

// TestCRLFOffsetsAndPositions checks that a \r\n ends a line as \n does:
// the semicolon it implies sits at its carriage return, the next line
// starts after its line feed, and a position on that line counts columns
// from there.
func TestCRLFOffsetsAndPositions(t *testing.T) {
	src := "x := 1\r\n\r\n  @ y\r\n"
	out := lex(t, src)
	checkInvariants(t, src, out)

	if got, want := out.file.LineStarts(), []int{0, 8, 10, 17}; !equalInts(got, want) {
		t.Errorf("line starts = %v, want %v", got, want)
	}
	checkOffsets(t, out.tokens, []tokenSpan{
		{lexer.Ident, 0, 1},
		{lexer.Define, 2, 4},
		{lexer.Int, 5, 6},
		{lexer.Semi, 6, 6},
		{lexer.Ident, 14, 15},
		{lexer.Semi, 15, 15},
		{lexer.EOF, 17, 17},
	})

	if len(out.diags) != 1 || out.diags[0].Message != "invalid character @" {
		t.Fatalf("diagnostics = %v, want one invalid character @", messages(out.diags))
	}
	line, col := out.file.Position(out.diags[0].Span.Start)
	if line != 3 || col != 3 {
		t.Errorf("@ is at %d:%d, want 3:3", line, col)
	}
	if line, col := out.file.Position(6); line != 1 || col != 7 {
		t.Errorf("the carriage return is at %d:%d, want 1:7", line, col)
	}
}

// commentSpanCases pin the byte range of the one comment token in each
// source, with and without a newline after it.
var commentSpanCases = []struct {
	src        string
	start, end int
}{
	{"// hi", 0, 5},
	{"// hi\n", 0, 5},
	{"x // hi", 2, 7},
	{"x // hi\n", 2, 7},
	{"//", 0, 2},
	{"//\n", 0, 2},
	{"// a\n// b", 0, 9},
	{"// a\n// b\n", 0, 9},
	{"x\n// a\n//", 2, 9},
	{"// a\n\nx", 0, 4},
	{"// a\n\n", 0, 4},
	{"// hi\r\n", 0, 5},
	{"x // hi\r\ny", 2, 7},
	{"// a\r\n// b\r\n", 0, 10},
	{"// a\r", 0, 5},
	{"// a\rb\n", 0, 6},
}

// commentValueCases pin the value of the one comment token in each source:
// every line of a line comment has its surrounding space trimmed alike,
// the first as much as the rest, and a block comment is the text inside
// it, nested comments included, with its surrounding space trimmed.
var commentValueCases = []struct {
	src  string
	want string
}{
	{"// a\n// b", "a\nb"},
	{"// a\n// b\n", "a\nb"},
	{"//a\n//b", "a\nb"},
	{"//   a  \n//\tb\t", "a\nb"},
	{"// a\n//\n// b", "a\n\nb"},
	{"// a\r\n// b\r\n", "a\nb"},
	{"//\n//", "\n"},
	{"// x // y\n// z", "x // y\nz"},
	{"    // a\n\t//  b \n  //c", "a\nb\nc"},
	{"/* a */", "a"},
	{"/**/", ""},
	{"/*  a\n   b  */", "a\n   b"},
	{"/* a\r\nb */", "a\nb"},
	{"/* a /* b */ c */", "a /* b */ c"},
	{"/* open", "open"},
	{"/* a // b */", "a // b"},
}

// commentGroupCases pin every comment token in each source, and the token
// kinds around them: a blank line, empty or holding only spaces and tabs,
// ends a comment, as it ends a comment group in Go, while lines with no
// blank line between them stay one comment however each is indented.
// Splitting a comment adds no semicolon.
var commentGroupCases = []struct {
	src   string
	kinds string
	want  []commentTok
}{
	{"// a\n\n// b", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 6, 10}}},
	{"// a\n\n// b\n", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 6, 10}}},
	{"// a\n\n\n// b", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 7, 11}}},
	{"// a\n \n// b", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 7, 11}}},
	{"// a\n\t\n// b", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 7, 11}}},
	{"// a\r\n\r\n// b\r\n", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 8, 12}}},
	{
		"// a\r\n// b\r\n\r\n// c",
		"comment comment EOF",
		[]commentTok{{"a\nb", 0, 10}, {"c", 14, 18}},
	},
	{"// a\r\n  \r\n// b", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b", 10, 14}}},
	{"//\n\n//", "comment comment EOF", []commentTok{{"", 0, 2}, {"", 4, 6}}},
	{"// a\n// b\n\n// c", "comment comment EOF", []commentTok{{"a\nb", 0, 9}, {"c", 11, 15}}},
	{"// a\n\n// b\n// c", "comment comment EOF", []commentTok{{"a", 0, 4}, {"b\nc", 6, 15}}},
	{
		"x // a\n\n// b\ny",
		"ident <semi> comment comment ident <semi> EOF",
		[]commentTok{{"a", 2, 6}, {"b", 8, 12}},
	},
	{"x // a\n\ny", "ident <semi> comment ident <semi> EOF", []commentTok{{"a", 2, 6}}},
	{"    // a\n    // b", "comment EOF", []commentTok{{"a\nb", 4, 17}}},
	{"// a\n    // b", "comment EOF", []commentTok{{"a\nb", 0, 13}}},
	{"    // a\n// b", "comment EOF", []commentTok{{"a\nb", 4, 13}}},
	{"\t// a\n\t// b\n", "comment EOF", []commentTok{{"a\nb", 1, 11}}},
	{"  // a\n\t // b\n \t// c", "comment EOF", []commentTok{{"a\nb\nc", 2, 20}}},
	{"    // a\n    //\n    // b", "comment EOF", []commentTok{{"a\n\nb", 4, 24}}},
	{"    // a\n    \n    // b", "comment comment EOF", []commentTok{{"a", 4, 8}, {"b", 18, 22}}},
	{"    // a\n\t\n\t// b", "comment comment EOF", []commentTok{{"a", 4, 8}, {"b", 12, 16}}},
	{"    // a\n    ", "comment EOF", []commentTok{{"a", 4, 8}}},
	{
		"    // a\n  x\n  // b",
		"comment ident <semi> comment EOF",
		[]commentTok{{"a", 4, 8}, {"b", 15, 19}},
	},
	{"// a\n  / b", "comment / ident <semi> EOF", []commentTok{{"a", 0, 4}}},
	{
		"x // a\n    // b\ny",
		"ident <semi> comment ident <semi> EOF",
		[]commentTok{{"a\nb", 2, 15}},
	},
}

// commentTok is the value and byte range of one comment token.
type commentTok struct {
	value      string
	start, end int
}

// TestCommentGroups checks where a run of comment lines splits into separate
// comment tokens, and that the split leaves the other tokens alone.
func TestCommentGroups(t *testing.T) {
	for _, tt := range commentGroupCases {
		t.Run(tt.src, func(t *testing.T) {
			out := lex(t, tt.src)
			checkInvariants(t, tt.src, out)
			var got []commentTok
			for i := range out.tokens {
				tok := &out.tokens[i]
				if tok.Kind() == lexer.Comment {
					got = append(
						got,
						commentTok{tok.Value().(string), tok.OffsetStart(), tok.OffsetEnd()},
					)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("comments = %+v, want %+v", got, tt.want)
			}
			if k := kinds(out.tokens); k != tt.kinds {
				t.Errorf("tokens = %s, want %s", k, tt.kinds)
			}
		})
	}
}

// TestCommentValues checks the value of merged comment lines.
func TestCommentValues(t *testing.T) {
	for _, tt := range commentValueCases {
		t.Run(tt.src, func(t *testing.T) {
			out := lex(t, tt.src)
			checkInvariants(t, tt.src, out)
			if got := out.tokens[0].Value(); got != tt.want {
				t.Errorf("value of %q = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}
}

// TestCommentOffsets checks a comment token ends right after its last byte,
// whether a newline or the end of the input follows it.
func TestCommentOffsets(t *testing.T) {
	for _, tt := range commentSpanCases {
		t.Run(tt.src, func(t *testing.T) {
			out := lex(t, tt.src)
			checkInvariants(t, tt.src, out)
			var found bool
			for i := range out.tokens {
				tok := &out.tokens[i]
				if tok.Kind() != lexer.Comment {
					continue
				}
				found = true
				if tok.OffsetStart() != tt.start || tok.OffsetEnd() != tt.end {
					t.Errorf("comment = [%d:%d], want [%d:%d]",
						tok.OffsetStart(), tok.OffsetEnd(), tt.start, tt.end)
				}
			}
			if !found {
				t.Errorf("no comment token in %s", kinds(out.tokens))
			}
		})
	}
}

// diagnosticCases are malformed inputs, with the messages the lexer must
// report for each, in order.
var diagnosticCases = []struct {
	name string
	src  string
	want []string
}{
	{"invalid character", "@", []string{"invalid character @"}},
	{"every invalid character", "@ $ #", []string{
		"invalid character @", "invalid character $", "invalid character #",
	}},
	{"unterminated string at EOF", `"abc`, []string{"unterminated string"}},
	{"unterminated string at newline", "\"abc\nx", []string{"unterminated string"}},
	{"unterminated raw string", "`abc", []string{"unterminated string"}},
	{"invalid escape", `"\q"`, []string{`Invalid escape sequence "\\q"`}},
	{"empty char", "''", []string{"empty char literal"}},
	{"lone quote", "'", []string{"unterminated char literal"}},
	{"char cut off", "'a", []string{"unterminated char literal"}},
	{"char too long", "'ab'", []string{"too many characters in char literal"}},
	{"char too long at EOF", "'ab", []string{"too many characters in char literal"}},
	{"char too long before newline", "'ab\nx", []string{"too many characters in char literal"}},
	{"char cut off by a newline", "'\nx", []string{"unterminated char literal"}},
	{"char two bytes in UTF-8", "'é'", []string{
		"char literal 'é' is 2 bytes in UTF-8; a char literal must be one byte",
	}},
	{"char three bytes in UTF-8", "'€'", []string{
		"char literal '€' is 3 bytes in UTF-8; a char literal must be one byte",
	}},
	{"char four bytes in UTF-8", "'😀'", []string{
		"char literal '😀' is 4 bytes in UTF-8; a char literal must be one byte",
	}},
	{"char the replacement character", "'\uFFFD'", []string{
		"char literal '\uFFFD' is 3 bytes in UTF-8; a char literal must be one byte",
	}},
	{"char multi-byte then more", "'éa'", []string{"too many characters in char literal"}},
	{"char multi-byte cut off", "'é", []string{"too many characters in char literal"}},
	{
		"char multi-byte before newline",
		"'é\nx",
		[]string{"too many characters in char literal"},
	},
	{
		"char truncated UTF-8 then more",
		"'\xc3a'",
		[]string{"too many characters in char literal"},
	},
	{"lexing goes on after a multi-byte char", "'é' @", []string{
		"char literal 'é' is 2 bytes in UTF-8; a char literal must be one byte",
		"invalid character @",
	}},
	{"char escape cut off by a newline", "'\\\nx", []string{
		`Invalid escape sequence "\\"`, "unterminated char literal",
	}},
	{"string escape cut off by a newline", "\"a\\\nb\"", []string{
		`Invalid escape sequence "\\"`, "unterminated string", "unterminated string",
	}},
	{"unterminated raw string at a newline", "`abc\nx", []string{"unterminated string"}},
	{"unterminated raw string ending in a backslash", "`a\\", []string{"unterminated string"}},
	{"lone backquote", "`", []string{"unterminated string"}},
	{"backslash does not escape a backquote", "`\\``", []string{"unterminated string"}},
	{"raw string holds no escapes to report", "`\\q \\\n \\`", nil},
	{"block comment", "/* c */", nil},
	{"unterminated block comment", "/* c", []string{"block comment not terminated"}},
	{"unterminated nested block comment", "/* a /* b */", []string{"block comment not terminated"}},
	{"star-slash-star opens nothing new", "/*/", []string{"block comment not terminated"}},
	{"lexing goes on after a block comment", "/* c */ @", []string{"invalid character @"}},
	{"/* between operands runs to the end", "a/*b", []string{"block comment not terminated"}},
	{"/* after a division", "4 / /* 2 */ 2", nil},
	{"/* inside a string is text", `"/* c */"`, nil},
	{"*/ alone is two operators", "a */ b", nil},
	{"leading zeros", "007", []string{"integer literal 007 has a leading zero; write 7"}},
	{"zeros only", "00", []string{"integer literal 00 has a leading zero; write 0"}},
	{"leading zero before a large value", "0018446744073709551616", []string{
		"integer literal 0018446744073709551616 has a leading zero; write 18446744073709551616",
	}},
	{
		"leading zero in an expression",
		"x := 1 + 010",
		[]string{"integer literal 010 has a leading zero; write 10"},
	},
	{"zero alone", "0", nil},
	{"hexadecimal", "0x10 0XfF 0x_1F", nil},
	{"binary and octal", "0b1010 0B1 0o17 0O7", nil},
	{"separators", "1_000_000 0xFF_FF 0b1_0", nil},
	{"a prefix with no digits", "0x", []string{"hexadecimal literal 0x has no digits"}},
	{"a prefix with only a separator", "0b_", []string{"binary literal 0b_ has no digits"}},
	{"a digit binary lacks", "0b102", []string{"invalid digit 2 in binary literal 0b102"}},
	{"a digit octal lacks", "0o8", []string{"invalid digit 8 in octal literal 0o8"}},
	{"a letter hex lacks", "0x1g", []string{"invalid digit g in hexadecimal literal 0x1g"}},
	{"a doubled separator", "1__0", []string{"_ must separate successive digits in 1__0"}},
	{"a trailing separator", "1_", []string{"_ must separate successive digits in 1_"}},
	{"a trailing separator after a prefix", "0x1_", []string{
		"_ must separate successive digits in 0x1_",
	}},
	{"a leading zero with a separator", "0_7", []string{
		"integer literal 0_7 has a leading zero; write 7",
	}},
	{"a name right after a decimal", "12ab", nil},
	{"floats", "1.5 .5 1. 1e9 1.5e-3 1E+2 0.0 01.5 0e5", nil},
	{"float separators", "1_000.000_5 1_0e1_0", nil},
	{"an exponent with no digits", "1e", []string{"exponent has no digits in 1e"}},
	{"an exponent with only a sign", "2.5e+", []string{"exponent has no digits in 2.5e+"}},
	{"an exponent with only a separator", "1e_", []string{"exponent has no digits in 1e_"}},
	{"a separator before the point", "1_.5", []string{
		"_ must separate successive digits in 1_.5",
	}},
	{"a separator after the point", "1._5", []string{
		"_ must separate successive digits in 1._5",
	}},
	{"a separator after the exponent's letter", "1e_5", []string{
		"_ must separate successive digits in 1e_5",
	}},
	{"a separator before the exponent's letter", "1_e5", []string{
		"_ must separate successive digits in 1_e5",
	}},
	{"a point then a point", "1..2", nil},
	{"asm is a name, with no brace after it", "func f() {\n    asm {\n        mov\n", nil},
	{"hex escape with one digit", `"\x4"`, []string{`Invalid escape sequence "\\x4"`}},
	{"hex escape with no digits", `"\x"`, []string{`Invalid escape sequence "\\x"`}},
	{"hex escape with a non-hex digit", `"\xg0"`, []string{`Invalid escape sequence "\\x"`}},
	{
		"hex escape with a non-hex second digit",
		`"\x4g"`,
		[]string{`Invalid escape sequence "\\x4"`},
	},
	{"hex escape in a char with one digit", `'\x4'`, []string{`Invalid escape sequence "\\x4"`}},
	{"hex escape cut off by the end", `"\x4`, []string{
		`Invalid escape sequence "\\x4"`, "unterminated string",
	}},
	{"hex escape cut off by a newline", "\"\\x\nx", []string{
		`Invalid escape sequence "\\x"`, "unterminated string",
	}},
	{"upper-case X is not a hex escape", `"\X41"`, []string{`Invalid escape sequence "\\X"`}},
	{"lone carriage return", "x\ry", []string{"invalid character \r"}},
	{"carriage return at the end", "x\r", []string{"invalid character \r"}},
	{"two carriage returns before a newline", "x\r\r\ny", []string{"invalid character \r"}},
	{"string cut off by crlf", "\"ab\r\nx", []string{"unterminated string"}},
	{"char cut off by crlf", "'\r\nx", []string{"unterminated char literal"}},
	{"char cut off by crlf after its byte", "'a\r\nx", []string{"unterminated char literal"}},
	{"char too long before crlf", "'ab\r\nx", []string{"too many characters in char literal"}},
	{"char escape cut off by crlf", "'\\\r\nx", []string{
		`Invalid escape sequence "\\"`, "unterminated char literal",
	}},
	{"string escape cut off by crlf", "\"a\\\r\nb\"", []string{
		`Invalid escape sequence "\\"`, "unterminated string", "unterminated string",
	}},
	{"crlf inside a block comment", "/* a\r\nb */\r\nx", nil},
	{"lone carriage return inside a block comment is text", "/* a\rb */", nil},
}

// TestDiagnostics pins which inputs are errors, what each error says, and
// that lexing goes on to report every problem in one run.
func TestDiagnostics(t *testing.T) {
	for _, tt := range diagnosticCases {
		t.Run(tt.name, func(t *testing.T) {
			out := lex(t, tt.src)
			checkInvariants(t, tt.src, out)
			got := messages(out.diags)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("diagnostics for %q = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestRecoveryKeepsTheNextLine checks that recovering from a malformed char
// literal does not swallow anything on the following line.
func TestRecoveryKeepsTheNextLine(t *testing.T) {
	out := lex(t, "'ab\nxy")
	checkInvariants(t, "'ab\nxy", out)

	var idents []string
	for i := range out.tokens {
		if out.tokens[i].Kind() == lexer.Ident {
			idents = append(idents, out.tokens[i].Value().(string))
		}
	}
	if strings.Join(idents, ",") != "xy" {
		t.Errorf("identifiers after recovery = %q, want [xy]", idents)
	}
}

// FuzzLex feeds the lexer arbitrary bytes and checks it never panics and
// never breaks its invariants.
func FuzzLex(f *testing.F) {
	for _, src := range adversarialInputs {
		f.Add(src)
	}
	paths, _ := filepath.Glob("../examples/*/main.tck")
	for _, path := range paths {
		if src, err := os.ReadFile(path); err == nil {
			f.Add(string(src))
		}
	}
	cases, err := invalid.Load(invalid.Path)
	if err != nil {
		f.Fatal(err)
	}
	for _, c := range cases {
		f.Add(string(c.Source))
	}

	f.Fuzz(func(t *testing.T, src string) {
		checkInvariants(t, src, lex(t, src))
	})
}

// TestDeclarationKeywords pins the keywords that start a variable
// declaration, and that a longer word starting with one is an identifier.
func TestDeclarationKeywords(t *testing.T) {
	tests := []struct {
		src  string
		want lexer.KeywordType
	}{
		{"var", lexer.Var},
		{"mut", lexer.Mut},
	}
	for _, tt := range tests {
		out := lex(t, tt.src)
		checkInvariants(t, tt.src, out)
		if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != tt.want {
			t.Errorf("%q lexed as %s, want keyword %s", tt.src, out.tokens[0].String(), tt.want)
		}
	}

	out := lex(t, "mutable")
	if out.tokens[0].Kind() != lexer.Ident {
		t.Errorf("%q lexed as %s, want an identifier", "mutable", out.tokens[0].String())
	}
}

// TestOwnKeyword pins own as a keyword, and that a longer word starting
// with it, or a name holding it, is an identifier.
func TestOwnKeyword(t *testing.T) {
	out := lex(t, "own")
	checkInvariants(t, "own", out)
	if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != lexer.Own {
		t.Errorf("%q lexed as %s, want keyword own", "own", out.tokens[0].String())
	}
	for _, src := range []string{"owner", "owns", "_own", "own_", "Own"} {
		out := lex(t, src)
		if out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestEnsuresKeyword pins ensures as a keyword, and that a longer word
// starting with it, or a name holding it, is an identifier.
func TestEnsuresKeyword(t *testing.T) {
	out := lex(t, "ensures")
	checkInvariants(t, "ensures", out)
	if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != lexer.Ensures {
		t.Errorf("%q lexed as %s, want keyword ensures", "ensures", out.tokens[0].String())
	}
	for _, src := range []string{"ensure", "ensured", "_ensures", "ensures_", "Ensures"} {
		out := lex(t, src)
		if out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestBoolKeywords pins true and false as keywords, and that a longer word
// starting with one is an identifier.
func TestBoolKeywords(t *testing.T) {
	tests := []struct {
		src  string
		want lexer.KeywordType
	}{
		{"true", lexer.True},
		{"false", lexer.False},
	}
	for _, tt := range tests {
		out := lex(t, tt.src)
		checkInvariants(t, tt.src, out)
		if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != tt.want {
			t.Errorf("%q lexed as %s, want keyword %s", tt.src, out.tokens[0].String(), tt.want)
		}
	}

	for _, src := range []string{"trueish", "falsey", "True"} {
		if out := lex(t, src); out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestFromKeyword pins from as a keyword, reserved everywhere and not only
// in an import, and that a longer word starting with it is an identifier.
func TestFromKeyword(t *testing.T) {
	out := lex(t, "from")
	checkInvariants(t, "from", out)
	if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != lexer.From {
		t.Errorf("%q lexed as %s, want keyword from", "from", out.tokens[0].String())
	}
	for _, src := range []string{"fromage", "From", "from_"} {
		if out := lex(t, src); out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestExternKeyword pins extern as a keyword, reserved everywhere and not
// only before func, and that a longer word starting with it is an
// identifier.
func TestExternKeyword(t *testing.T) {
	out := lex(t, "extern")
	checkInvariants(t, "extern", out)
	if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != lexer.Extern {
		t.Errorf("%q lexed as %s, want keyword extern", "extern", out.tokens[0].String())
	}
	for _, src := range []string{"externs", "Extern", "extern_"} {
		if out := lex(t, src); out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestNoErrorKeyword pins noerror as a keyword, the literal of the error
// that is no error, which ends a statement at a newline as true does, and
// that a longer word starting with it is an identifier.
func TestNoErrorKeyword(t *testing.T) {
	out := lex(t, "noerror\n")
	checkInvariants(t, "noerror\n", out)
	if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != lexer.NoError {
		t.Errorf("noerror lexed as %s, want keyword %s", out.tokens[0].String(), lexer.NoError)
	}
	if out.tokens[1].Kind() != lexer.Semi {
		t.Errorf(
			"after noerror and a newline came %s, want an inserted semicolon",
			out.tokens[1].String(),
		)
	}
	for _, src := range []string{"noerrors", "noError", "no_error"} {
		if out := lex(t, src); out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestListKeywords pins list and c_allocator as keywords: list, which
// starts a list type as map would a map type, does not end a statement at
// a newline, and c_allocator, the allocator literal, does, as noerror
// does. A longer word starting with either is an identifier.
func TestListKeywords(t *testing.T) {
	out := lex(t, "list\nc_allocator\n")
	checkInvariants(t, "list\nc_allocator\n", out)
	if out.tokens[0].Kind() != lexer.Keyword || out.tokens[0].Value() != lexer.List {
		t.Errorf("list lexed as %s, want keyword %s", out.tokens[0].String(), lexer.List)
	}
	if out.tokens[1].Kind() != lexer.Keyword || out.tokens[1].Value() != lexer.CAllocator {
		t.Errorf("c_allocator lexed as %s, want keyword %s after list and no semicolon",
			out.tokens[1].String(), lexer.CAllocator)
	}
	if out.tokens[2].Kind() != lexer.Semi {
		t.Errorf("after c_allocator and a newline came %s, want an inserted semicolon",
			out.tokens[2].String())
	}
	for _, src := range []string{"lists", "List", "c_allocators", "c_Allocator", "allocator"} {
		if out := lex(t, src); out.tokens[0].Kind() != lexer.Ident {
			t.Errorf("%q lexed as %s, want an identifier", src, out.tokens[0].String())
		}
	}
}

// TestAsmIsAnIdentifier checks asm is an ordinary name, not a keyword:
// alone it is an identifier, and the braces and words after it are lexed
// as any others, with nothing skipped and nothing reported.
func TestAsmIsAnIdentifier(t *testing.T) {
	out := lex(t, "asm")
	checkInvariants(t, "asm", out)
	if out.tokens[0].Kind() != lexer.Ident || out.tokens[0].Value() != "asm" {
		t.Errorf("%q lexed as %s, want the identifier asm", "asm", out.tokens[0].String())
	}
	if _, ok := lexer.LookupKeyword("asm"); ok {
		t.Error("asm is a keyword")
	}
	src := "asm { mov }"
	out = lex(t, src)
	checkInvariants(t, src, out)
	if len(out.diags) != 0 {
		t.Errorf("%q reported %v, want nothing", src, out.diags)
	}
	kinds := make([]string, 0, len(out.tokens))
	for _, tok := range out.tokens {
		kinds = append(kinds, tok.String())
	}
	if len(out.tokens) < 4 || out.tokens[2].Kind() != lexer.Ident ||
		out.tokens[2].Value() != "mov" {
		t.Errorf("%q lexed as %v, want asm, {, mov and } as tokens of their own", src, kinds)
	}
}

// TestStartsLine checks which tokens start a line: the first, each after
// a newline, a comment alone on its line included, and none on a line
// after another
// token, the semicolon a line end inserts among them, which sits on the
// line it ends.
func TestStartsLine(t *testing.T) {
	src := "package main\n\nfunc f() {\n    x := 1 // c\n    y\n    // d\n}"
	out := lex(t, src)
	var starts []string
	for _, tk := range out.tokens {
		if tk.StartsLine() {
			starts = append(starts, src[tk.OffsetStart():tk.OffsetEnd()])
		}
	}
	want := []string{"package", "func", "x", "y", "// d", "}"}
	if strings.Join(starts, "|") != strings.Join(want, "|") {
		t.Errorf("tokens starting a line: %q, want %q", starts, want)
	}
}

package porttest

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file check the harness itself, so that a port that
// differs from the Go compiler cannot pass for one that does not. They
// need neither compiler and run in every go test ./porttest.

// TestFirstDiff checks that firstDiff is empty only for equal bytes, and
// otherwise names the first line that differs, for differences a line
// split could hide: a missing final newline, a carriage return, a prefix,
// an empty side and a difference past the first line.
func TestFirstDiff(t *testing.T) {
	cases := []struct {
		name      string
		want, got string
		line      string
	}{
		{"missing final newline", "a\nb\n", "a\nb", "line 2:"},
		{"extra final newline", "a", "a\n", "line 1:"},
		{"carriage return", "a\n", "a\r\n", "line 1:"},
		{"prefix", "a\nb\n", "a\n", "line 2:"},
		{"empty port", "a\n", "", "line 1:"},
		{"empty Go", "", "a\n", "line 1:"},
		{"third line", "a\nb\nc\n", "a\nb\nd\n", "line 3:"},
		{"invisible byte", "a\n", "a\x00\n", "line 1:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := firstDiff([]byte(tc.want), []byte(tc.got))
			if !strings.HasPrefix(d, tc.line) {
				t.Errorf("firstDiff(%q, %q) = %q, want it to start %q", tc.want, tc.got, d, tc.line)
			}
		})
	}
	for _, s := range []string{"", "a", "a\n", "a\nb\n\n"} {
		if d := firstDiff([]byte(s), []byte(s)); d != "" {
			t.Errorf("firstDiff(%q, itself) = %q, want empty", s, d)
		}
	}
}

// TestDecodeJSON checks that decodeJSON takes exactly one value, with
// only white space after it, and keeps numbers as their text.
func TestDecodeJSON(t *testing.T) {
	for _, s := range []string{`{"a": 1}`, "[1, 2]\n", " null \n\t"} {
		if _, err := decodeJSON([]byte(s)); err != nil {
			t.Errorf("decodeJSON(%q) = %v, want no error", s, err)
		}
	}
	for _, s := range []string{"", "\n", `{"a": 1}{}`, "[1] x", "1 2", `{"a": 1`, "tack: error\n"} {
		if _, err := decodeJSON([]byte(s)); err == nil {
			t.Errorf("decodeJSON(%q) has no error, want one", s)
		}
	}
}

// TestStdoutDiffJSON checks what the JSON comparison counts as the same,
// white space, key order and escape forms, and that it counts every
// other difference, numbers that float64 would make equal among them.
func TestStdoutDiffJSON(t *testing.T) {
	same := []struct{ want, got string }{
		{`{"a":1,"b":[true,null]}`, "{\n  \"b\": [true, null],\n  \"a\": 1\n}\n"},
		{`{"s":"\u0041"}`, `{"s":"A"}`},
		{`{"s":"\u00e9"}`, `{"s":"é"}`},
		{`{"s":"a/b"}`, `{"s":"a\/b"}`},
	}
	for _, tc := range same {
		if d := stdoutDiff(compareJSON, []byte(tc.want), []byte(tc.got)); d != "" {
			t.Errorf("stdoutDiff(%q, %q) = %q, want no difference", tc.want, tc.got, d)
		}
	}
	differ := []struct{ name, want, got string }{
		{"value", `{"a":1}`, `{"a":2}`},
		{"key", `{"a":1}`, `{"b":1}`},
		{"extra key", `{"a":1}`, `{"a":1,"b":1}`},
		{"array order", `[1,2]`, `[2,1]`},
		{"type", `{"a":1}`, `{"a":"1"}`},
		{"null and empty", `{"a":null}`, `{"a":[]}`},
		{"big integers float64 makes equal", `18446744073709551615`, `18446744073709551616`},
		{"number form", `1`, `1.0`},
		{"exponent form", `100`, `1e2`},
		{"trailing data", `{"a":1}`, `{"a":1}{}`},
		{"port wrote no JSON", `{"a":1}`, ``},
		{"invalid UTF-8 bytes", "\"\xff\"", "\"\xfe\""},
		{"invalid UTF-8 and U+FFFD", "\"\xff\"", "\"�\""},
		{"Go wrote no JSON", "tack: error\n", "tack: errors\n"},
		{"Go wrote no JSON, port wrote JSON", "", "{}"},
	}
	for _, tc := range differ {
		t.Run(tc.name, func(t *testing.T) {
			if d := stdoutDiff(compareJSON, []byte(tc.want), []byte(tc.got)); d == "" {
				t.Errorf("stdoutDiff(%q, %q) found no difference", tc.want, tc.got)
			}
		})
	}
}

// TestStdoutDiffBytes checks that the byte comparison counts what the
// JSON one does not.
func TestStdoutDiffBytes(t *testing.T) {
	if d := stdoutDiff(compareBytes, []byte(`{"a":1,"b":2}`), []byte(`{"b":2,"a":1}`)); d == "" {
		t.Error("compareBytes found no difference in key order")
	}
	if d := stdoutDiff(compareBytes, []byte("x\n"), []byte("x\n")); d != "" {
		t.Errorf("compareBytes found a difference in equal bytes: %q", d)
	}
}

// TestNotImplemented checks that only exit status 3 with exactly one
// line of the protocol on standard error counts as not implemented, so
// that no other answer of the port can skip a case.
func TestNotImplemented(t *testing.T) {
	const line = "tack: not implemented in the port: --lex\n"
	yes := []result{
		{code: 3, stderr: []byte(line)},
		{code: 3, stderr: []byte("tack: not implemented in the port: \n")},
		{code: 3, stderr: []byte(line), stdout: []byte("anything")},
	}
	for _, r := range yes {
		if !r.notImplemented() {
			t.Errorf("%q, exit %d, is not counted as not implemented", r.stderr, r.code)
		}
	}
	no := []result{
		{code: 1, stderr: []byte(line)},
		{code: 2, stderr: []byte(line)},
		{code: 0, stderr: []byte(line)},
		{code: 3, stderr: nil},
		{code: 3, stderr: []byte(strings.TrimSuffix(line, "\n"))},
		{code: 3, stderr: []byte(line + line)},
		{code: 3, stderr: []byte("error: x\n" + line)},
		{code: 3, stderr: []byte(line + "more\n")},
		{code: 3, stderr: []byte(" " + line)},
		{code: 3, stderr: []byte("tack: not implemented in the port:--lex\n")},
		{code: 3, stderr: []byte("tack: not implemented: --lex\n")},
		{code: 3, stderr: []byte("tack: not implemented in the port: --lex\r\n\n")},
	}
	for _, r := range no {
		if r.notImplemented() {
			t.Errorf("%q, exit %d, is counted as not implemented", r.stderr, r.code)
		}
	}
}

// writeFile writes content to a file named name in dir and returns its
// path, failing t if it cannot.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReadRequired checks that readRequired skips comments and blank
// lines and takes every stage named.
func TestReadRequired(t *testing.T) {
	dir := t.TempDir()
	got, err := readRequired(writeFile(t, dir, "ok.txt",
		"# a comment\n\n  \nlex-single\n  ast-single  \n\t# indented comment\nbackends"))
	if err != nil {
		t.Fatalf("readRequired: %v", err)
	}
	want := map[string]bool{"lex-single": true, "ast-single": true, "backends": true}
	if len(got) != len(want) {
		t.Errorf("readRequired = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("readRequired = %v, missing %s", got, name)
		}
	}
	empty, err := readRequired(writeFile(t, dir, "empty.txt", "# nothing required\n"))
	if err != nil || len(empty) != 0 {
		t.Errorf("readRequired of comments only = %v, %v, want none", empty, err)
	}
}

// TestReadRequiredRejects checks that readRequired rejects a name that is
// no stage, however close to one, and a file that is not there.
func TestReadRequiredRejects(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"lex\n", "Lex-single\n", "lex-single # why\n", "backends,lex-single\n"} {
		if r, err := readRequired(writeFile(t, dir, "bad.txt", bad)); err == nil {
			t.Errorf("readRequired(%q) = %v, want an error", bad, r)
		}
	}
	if _, err := readRequired(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("readRequired of a missing file has no error")
	}
}

// TestRequiredFile checks that this package's required.txt reads, so a
// mistake in it shows in a plain go test, not only when the port is
// checked.
func TestRequiredFile(t *testing.T) {
	if _, err := readRequired("required.txt"); err != nil {
		t.Fatal(err)
	}
}

// TestFromRoot checks that fromRoot keeps an absolute path, cleaned, and
// takes a relative one from the module's root, not the working directory.
func TestFromRoot(t *testing.T) {
	if env.root == "" || !filepath.IsAbs(env.root) {
		t.Fatalf("TestMain left the module root %q, want an absolute path", env.root)
	}
	cases := []struct{ in, want string }{
		{"/a/b", "/a/b"},
		{"/a/./b/../c", "/a/c"},
		{"tack", filepath.Join(env.root, "tack")},
		{"../tack", filepath.Join(filepath.Dir(env.root), "tack")},
		{"./bin/tack", filepath.Join(env.root, "bin", "tack")},
	}
	for _, tc := range cases {
		if got := fromRoot(tc.in); got != tc.want {
			t.Errorf("fromRoot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPortSource checks that an empty TACK_PORT is the default, ../tack
// from the module's root, and any other value is explicit, and that a
// missing port is an error either way, naming which.
func TestPortSource(t *testing.T) {
	src, explicit := portSource("")
	if want := filepath.Join(filepath.Dir(env.root), "tack"); src != want || explicit {
		t.Errorf("portSource(\"\") = %q, %v, want %q, false", src, explicit, want)
	}
	for _, in := range []string{"../tack", "/abs/port", "."} {
		src, explicit := portSource(in)
		if want := fromRoot(in); src != want || !explicit {
			t.Errorf("portSource(%q) = %q, %v, want %q, true", in, src, explicit, want)
		}
	}
	explicitErr := missingPort("/p", true)
	if explicitErr == nil || !strings.Contains(explicitErr.Error(), "TACK_PORT is /p") {
		t.Errorf("missingPort(explicit) = %v, want it to name TACK_PORT", explicitErr)
	}
	defaultErr := missingPort("/p", false)
	if defaultErr == nil || !strings.Contains(defaultErr.Error(), "the default") {
		t.Errorf("missingPort(default) = %v, want it to name the default", defaultErr)
	}
}

// TestCountGiven checks that countGiven sees -test.count only when the
// command line set it, whatever its value, and not its default.
func TestCountGiven(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"-test.v"}, false},
		{[]string{"-test.count=1"}, true},
		{[]string{"-test.count", "2"}, true},
		{[]string{"-test.v", "-test.count=1"}, true},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.Uint("test.count", 1, "")
		fs.Bool("test.v", false, "")
		if err := fs.Parse(tc.args); err != nil {
			t.Fatalf("parsing %q: %v", tc.args, err)
		}
		if got := countGiven(fs); got != tc.want {
			t.Errorf("countGiven after %q = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// ratchetStages are the stages TestDeclinedRatchet decides for: one
// that classifies its cases and one that does not.
var ratchetStages = struct{ classifying, plain stage }{
	classifying: stage{
		name:     "emit-x",
		classify: func(input, bool) (covered, error) { return nil, nil },
	},
	plain: stage{name: "plain"},
}

// requiredOf returns names as required.txt's set.
func requiredOf(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// ratchetCases are TestDeclinedRatchet's: what is required, the case,
// and whether it must fail and its needs be asked for.
var ratchetCases = []struct {
	required map[string]bool
	name     string
	st       stage
	in       input
	fails    bool
	asked    bool
}{
	{requiredOf("plain"), "required stage", ratchetStages.plain, input{}, true, false},
	{
		requiredOf("emit-E0", "emit-H", "emit-E1"),
		"stage not required",
		ratchetStages.plain,
		input{},
		false,
		false,
	},
	{
		requiredOf("emit-E0", "emit-H", "emit-E1"),
		"every needed stage required",
		ratchetStages.classifying,
		input{},
		true,
		true,
	},
	{
		requiredOf("emit-E0", "emit-H"),
		"one needed stage not required",
		ratchetStages.classifying,
		input{},
		false,
		true,
	},
	{
		requiredOf("emit-H", "emit-E1"),
		"E0 not required",
		ratchetStages.classifying,
		input{},
		false,
		false,
	},
	{
		requiredOf("emit-x", "emit-E0", "emit-H", "emit-E1"), "input that may decline",
		ratchetStages.classifying,
		input{declines: true},
		false, false,
	},
	{
		requiredOf("emit-x"),
		"classifying stage required",
		ratchetStages.classifying,
		input{},
		true,
		false,
	},
}

// TestDeclinedRatchet checks declineVerdict with required stages of its
// own: a declined case of a required stage fails; one of a stage that
// classifies its cases fails once every emit stage it needs is
// required, and skips while one is not, or while E0 is not, when what
// it needs is not even asked; an input that may decline always skips;
// and a classification that fails is an error.
func TestDeclinedRatchet(t *testing.T) {
	needs := emitSet(0).with(emitE0).with(emitH).with(emitE1)
	for _, tc := range ratchetCases {
		asked := false
		why, err := declineVerdict(tc.st, tc.in, tc.required, func() (emitSet, error) {
			asked = true
			return needs, nil
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if (why != "") != tc.fails || asked != tc.asked {
			t.Errorf(
				"%s: verdict %q, needs asked %v; want failing %v, asked %v",
				tc.name,
				why,
				asked,
				tc.fails,
				tc.asked,
			)
		}
	}
	_, err := declineVerdict(
		ratchetStages.classifying,
		input{},
		requiredOf("emit-E0"),
		func() (emitSet, error) {
			return 0, errors.New("no coverage")
		},
	)
	if err == nil {
		t.Error("a classification that fails is no error")
	}
}

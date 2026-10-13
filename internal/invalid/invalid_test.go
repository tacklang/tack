package invalid_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// TestRoundTrip checks that Format and Parse invert each other on cases
// that stress the format: an empty program, a program without a final
// newline, odd bytes, and lines that look almost like headers.
func TestRoundTrip(t *testing.T) {
	comment := []byte("a comment\n-- not a header\n")
	cases := []invalid.Case{
		{Name: "a-empty", Source: nil, Expected: []byte("stage: parse\n")},
		{Name: "b-no-newline", Source: []byte("package main\nx"), Expected: []byte("e\n")},
		{Name: "c-one-newline", Source: []byte("\n"), Expected: []byte("e\n")},
		{
			Name:     "d-odd-bytes",
			Source:   []byte("\x00\xff\r\n-- x --\r\n--x --\n"),
			Expected: []byte("é\n"),
		},
		{Name: "e-blank-lines", Source: []byte("x\n\n\n"), Expected: []byte("\n\n")},
	}
	data, err := invalid.Format(comment, cases)
	if err != nil {
		t.Fatal(err)
	}
	gotComment, got, err := invalid.Parse(data)
	if err != nil {
		t.Fatalf("Parse(Format(...)): %v\n%s", err, data)
	}
	if string(gotComment) != string(comment) {
		t.Errorf("comment = %q, want %q", gotComment, comment)
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d cases, want %d", len(got), len(cases))
	}
	for i := range cases {
		if got[i].Name != cases[i].Name || string(got[i].Source) != string(cases[i].Source) ||
			string(got[i].Expected) != string(cases[i].Expected) {
			t.Errorf("case %d = %+q, want %+q", i, got[i], cases[i])
		}
	}
}

// TestParseRejectsMalformed checks that every structural mistake Parse
// guards against is an error, not a quietly different case.
func TestParseRejectsMalformed(t *testing.T) {
	ok := "-- a/main.tck --\nsrc\n-- a/expected --\nout\n"
	tests := []struct {
		name, data, want string
	}{
		{"no final newline", strings.TrimSuffix(ok, "\n"), "end with a newline"},
		{"empty header", "-- --\n", "malformed header"},
		{"header with only spaces", "--    --\n", "malformed header"},
		{"padded header name", "--  a/main.tck --\n", "malformed header"},
		{"expected first", "-- a/expected --\nout\n", "want a header"},
		{"missing expected", "-- a/main.tck --\nsrc\n", "right after its program"},
		{
			"expected of another case",
			"-- a/main.tck --\n-- b/expected --\nx\n",
			"right after its program",
		},
		{"empty expected", "-- a/main.tck --\n-- a/expected --\n", "empty expected"},
		{"stray file", ok + "-- a/notes --\nx\n", "want a header"},
		{"nested name", "-- a/b/main.tck --\n-- a/b/expected --\nx\n", "want a header"},
		{"duplicate", ok + ok, "strictly increasing"},
		{"out of order", strings.ReplaceAll(ok, "a/", "b/") + ok, "strictly increasing"},
		{"non-empty marker", ok + "-- a/no-final-newline --\nx\n", "must be empty"},
		{
			"marker on empty program",
			"-- a/main.tck --\n-- a/expected --\nx\n-- a/no-final-newline --\n",
			"no newline to drop",
		},
		{
			"marker after a blank last line",
			"-- a/main.tck --\nx\n\n-- a/expected --\ny\n-- a/no-final-newline --\n",
			"last line is empty",
		},
		{
			"marker after a lone newline",
			"-- a/main.tck --\n\n-- a/expected --\ny\n-- a/no-final-newline --\n",
			"last line is empty",
		},
		{
			"marker twice",
			ok + "-- a/no-final-newline --\n-- a/no-final-newline --\n",
			"want a header",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := invalid.Parse([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse(%q) error = %v, want one mentioning %q", tt.data, err, tt.want)
			}
		})
	}
}

// TestParseEmpty checks that an empty archive and a comment-only one are
// valid and hold no cases.
func TestParseEmpty(t *testing.T) {
	for _, data := range []string{"", "just a comment\n"} {
		comment, cases, err := invalid.Parse([]byte(data))
		if err != nil || len(cases) != 0 || string(comment) != data {
			t.Errorf(
				"Parse(%q) = %q, %v, %v; want the comment, no cases, no error",
				data,
				comment,
				cases,
				err,
			)
		}
	}
}

// good is a case Format accepts, for tests to break one field of.
var good = invalid.Case{Name: "a", Source: []byte("x\n"), Expected: []byte("e\n")}

// with returns good, changed by f, as the only case in a list.
func with(f func(*invalid.Case)) []invalid.Case {
	c := good
	f(&c)
	return []invalid.Case{c}
}

// TestFormatRejectsUnrepresentable checks that Format refuses anything
// txtar could not give back exactly.
func TestFormatRejectsUnrepresentable(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    string
		cases   []invalid.Case
	}{
		{
			name:  "header in source",
			cases: with(func(c *invalid.Case) { c.Source = []byte("-- x --\n") }),
			want:  "reads as a txtar header",
		},
		{
			name:  "header at end of source",
			cases: with(func(c *invalid.Case) { c.Source = []byte("x\n-- --") }),
			want:  "reads as a txtar header",
		},
		{
			name:  "header in expected",
			cases: with(func(c *invalid.Case) { c.Expected = []byte("-- x --\n") }),
			want:  "reads as a txtar header",
		},
		{name: "header in comment", comment: "-- x --\n", want: "reads as a txtar header"},
		{name: "comment without newline", comment: "c", want: "comment does not end"},
		{
			name:  "empty expected",
			cases: with(func(c *invalid.Case) { c.Expected = nil }),
			want:  "non-empty and end with a newline",
		},
		{
			name:  "expected without newline",
			cases: with(func(c *invalid.Case) { c.Expected = []byte("e") }),
			want:  "non-empty and end with a newline",
		},
		{
			name:  "empty name",
			cases: with(func(c *invalid.Case) { c.Name = "" }),
			want:  "invalid case name",
		},
		{
			name:  "slash in name",
			cases: with(func(c *invalid.Case) { c.Name = "a/b" }),
			want:  "invalid case name",
		},
		{
			name:  "space in name",
			cases: with(func(c *invalid.Case) { c.Name = "a b" }),
			want:  "invalid case name",
		},
		{name: "duplicate", cases: []invalid.Case{good, good}, want: "strictly increasing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := invalid.Format([]byte(tt.comment), tt.cases)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

// TestLoad checks the real archive and Load's own failures.
func TestLoad(t *testing.T) {
	cases, err := invalid.Load("../../examples/invalid.txtar")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	if _, err := invalid.Load("does-not-exist.txtar"); err == nil {
		t.Error("Load of a missing file succeeded")
	}
}

// FuzzParse checks that Parse never panics, and that whatever it accepts
// Format writes back to the same bytes and parses to the same cases.
func FuzzParse(f *testing.F) {
	f.Add([]byte("-- a/main.tck --\nsrc\n-- a/expected --\nout\n"))
	f.Add([]byte("c\n-- a/main.tck --\nx\n-- a/expected --\no\n-- a/no-final-newline --\n"))
	f.Add([]byte("-- --\n--  --\n-- a --\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		comment, cases, err := invalid.Parse(data)
		if err != nil {
			return
		}
		out, err := invalid.Format(comment, cases)
		if err != nil {
			t.Fatalf("Format rejected what Parse accepted: %v\n%q", err, data)
		}
		if string(out) != string(data) {
			t.Fatalf("round trip changed the archive\nin:  %q\nout: %q", data, out)
		}
		comment2, cases2, err := invalid.Parse(out)
		if err != nil || string(comment2) != string(comment) || !reflect.DeepEqual(cases2, cases) {
			t.Fatalf("reparse differs: %v", err)
		}
	})
}

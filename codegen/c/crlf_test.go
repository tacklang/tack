package c_test

import (
	"strings"
	"testing"
)

// crlfProgram exercises everything a line ending touches: semicolons implied
// at the end of a line, comments merged across lines, a raw string spanning
// lines, a string and a character literal right before a line ending, and
// statements split over several lines.
const crlfProgram = "package main\n" +
	"\n" +
	"// count adds up to n.\n" +
	"// It spans two comment lines.\n" +
	"func count(n int64) int64 {\n" +
	"    mut sum := 0\n" +
	"    for i := range n {\n" +
	"        sum += i\n" +
	"    }\n" +
	"    return sum\n" +
	"}\n" +
	"\n" +
	"func main() {\n" +
	"    s := `a\n" +
	"b`\n" +
	"    c := 'x'\n" +
	"    if len(s) != 3 || s[1] != 10 || c != 120 {\n" +
	"        exit(1)\n" +
	"    }\n" +
	"    print(\"one\\n\")\n" +
	"    switch count(4) {\n" +
	"    case 6:\n" +
	"        print(\"six\\n\")\n" +
	"    default:\n" +
	"        exit(2)\n" +
	"    }\n" +
	"    exit(count(5) +\n" +
	"        0)\n" +
	"}\n"

// TestCRLFProgramRuns builds a program saved with \r\n line endings and
// checks it compiles to the same C as with \n, and that the C runs as the
// program says: the raw string holds a lone \n, since its carriage return
// is dropped.
func TestCRLFProgramRuns(t *testing.T) {
	crlf := strings.ReplaceAll(crlfProgram, "\n", "\r\n")
	if got, want := generate(t, crlf), generate(t, crlfProgram); got != want {
		t.Errorf("CRLF source generated different C:\n%s\nwant:\n%s", got, want)
	}

	out, status := runSanitized(t, crlf)
	if out != "one\nsix\n" || status != 10 {
		t.Errorf("got stdout %q, status %d; want %q, 10", out, status, "one\nsix\n")
	}
}

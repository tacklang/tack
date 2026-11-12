package c_test

import (
	"bytes"
	"math"
	"math/rand/v2"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// quoteProgram reads the file its first argument names, records as
// utf8Program's decode mode reads them, a two-byte big-endian length and
// that many bytes, and writes each record quoted by std/conv's
// append_quote, followed by a newline, which a quoted string never holds.
const quoteProgram = `package main

import conv from "std/conv"
import fmt from "std/fmt"
import os from "std/os"

// at returns byte i of s, or 0 when s has no byte i.
func at(s []uint8, i int64) uint8 {
    c, ok := s[i]
    if !ok {
        return 0
    }
    return c
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 2 {
        print("usage: quote file\n")
        exit(2)
    }
    data, e2 := os.read_file(args[1].bytes(), c_allocator)
    if e2.code != 0 {
        print("cannot read the file\n")
        exit(2)
    }
    mut out := make(list[uint8], c_allocator)
    mut i int64 = 0
    for i >= 0 && i < len(data) {
        n := int64(at(data, i)) << 8 | int64(at(data, i +% 1))
        start := i +% 2
        end := start +% n
        if start < 0 || end > len(data) || start > end {
            print("bad record\n")
            exit(1)
        }
        e3 := conv.append_quote(mut out, data[start:end])
        e4 := append(mut out, '\n')
        if e3.code != 0 || e4.code != 0 {
            print("out of memory\n")
            exit(1)
        }
        i = end
    }
    fmt.write(out)
}
`

// checkQuote runs bin, a build of quoteProgram, on inputs, and checks
// that it quoted each exactly as Go's strconv.Quote does.
func checkQuote(t *testing.T, bin string, inputs [][]byte) {
	t.Helper()
	got := runQuote(t, bin, inputs)
	for _, s := range inputs {
		want := strconv.Quote(string(s)) + "\n"
		if !bytes.HasPrefix(got, []byte(want)) {
			line, _, _ := bytes.Cut(got, []byte("\n"))
			t.Fatalf("quoting % x: wrote %s, Go's strconv.Quote is %s", s, line, want)
		}
		got = got[len(want):]
	}
	if len(got) != 0 {
		t.Fatalf("wrote %d bytes more than Go", len(got))
	}
}

// runQuote runs bin, a build of quoteProgram, on inputs, and returns what
// it wrote.
func runQuote(t *testing.T, bin string, inputs [][]byte) []byte {
	t.Helper()
	return runOnFile(t, bin, utf8Records(t, inputs))
}

// quoteInputs returns the byte strings TestQuoteMatchesGo quotes: every
// rune from 0 to 0x10FFFF encoded on its own, so each printable and
// non-printable rune is met; every byte string utf8Inputs returns, every
// pair of bytes among them; strings of the bytes and runes quoting
// treats apart, a quote, a backslash, each escaped control character,
// 0x7f, U+FFFD written out, bad bytes and runes of each length, mixed at
// random from r; and long strings.
func quoteInputs(r *rand.Rand) [][]byte {
	var in [][]byte
	for c := rune(0); c <= unicode.MaxRune; c++ {
		in = append(in, utf8.AppendRune(nil, c))
	}
	in = append(in, utf8Inputs(r)...)
	parts := []string{
		"\"",
		"\\",
		"\a",
		"\b",
		"\f",
		"\n",
		"\r",
		"\t",
		"\v",
		"\x00",
		"\x1f",
		"\x7f",
		" ",
		"a",
		"'",
		"`",
		"\xef\xbf\xbd",
		"\xff",
		"\x80",
		"\xc0\x80",
		"\xed\xa0\x80",
		"\xf4\x90\x80\x80",
		"\xe4\xb8",
		"\u00a0",
		"\u00ad",
		"\u0085",
		"é",
		"世",
		"\u2028",
		"\ufeff",
		"\uffff",
		"😀",
		"\U000e0001",
		"\U0010ffff",
	}
	for range 20000 {
		b := make([]byte, 0, 48)
		for range r.IntN(12) {
			b = append(b, parts[r.IntN(len(parts))]...)
		}
		in = append(in, b)
	}
	in = append(in, bytes.Repeat([]byte{0xff}, 3000), bytes.Repeat([]byte("\"\\\n"), 3000),
		bytes.Repeat([]byte("\U0010ffff"), 3000), bytes.Repeat([]byte("世"), 3000))
	return in
}

// TestQuoteMatchesGo checks that std/conv's append_quote quotes every
// string of quoteInputs exactly as Go's strconv.Quote does, built with
// utf8Sanitizers at -O0 and -O1.
func TestQuoteMatchesGo(t *testing.T) {
	inputs := quoteInputs(rand.New(rand.NewPCG(13, 14)))
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		checkQuote(t, buildWithStd(t, quoteProgram, flags...), inputs)
	})
	t.Logf("%d inputs", len(inputs))
}

// FuzzQuoteMatchesGo checks, for each byte string the fuzzer makes, that
// std/conv's append_quote quotes it exactly as Go's strconv.Quote does.
func FuzzQuoteMatchesGo(f *testing.F) {
	for _, s := range []string{"", "a", "\"\\", "\a\x00\x7f", "\xc0\x80", "\xed\xa0\x80", "\u00a0", "\U000e0001", "世界\xff"} {
		f.Add([]byte(s))
	}
	bin := buildWithStd(f, quoteProgram, utf8Sanitizers...)
	f.Fuzz(func(t *testing.T, s []byte) {
		if len(s) > math.MaxUint16 {
			t.Skip("too long for a record")
		}
		checkQuote(t, bin, [][]byte{s})
	})
}

// TestQuoteExampleIsSanitizerClean builds examples/quote and its C file,
// live.c, with clang's warnings as errors and utf8Sanitizers, and checks
// it writes exactly its output, every check ok, a failed append_quote
// leaving its list as it was and every buffer freed among them.
func TestQuoteExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "quote", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "quote", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/quote/output expects a check to fail")
	}
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		if out := runExample(t, buildWithStdAndC(t, src, csrc, flags...)); out != want {
			t.Errorf("quote example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

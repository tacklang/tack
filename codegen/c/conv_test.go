package c_test

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// convBases and convBitSizes are the bases and bit sizes convProgram
// parses each input in, every pair of them: the bases and widths
// programs use, the edges of each range, and values Go refuses.
var (
	convBases    = []int{0, 2, 8, 10, 16, 36, 1, 37, -1}
	convBitSizes = []int{0, 1, 8, 16, 32, 63, 64, 65, -1}
)

// convProgram is a program that parses each of its arguments with
// std/conv's parse_int and parse_uint in every base of convBases and
// bit size of convBitSizes, and writes one line for each pair, what
// convWant gives for it.
var convProgram = fmt.Sprintf(`package main

import conv from "std/conv"
import fmt from "std/fmt"
import os from "std/os"

var bases = [%d]int64{%s}
var bit_sizes = [%d]int64{%s}

func one(s []uint8) {
    for _, b := range bases {
        for _, n := range bit_sizes {
            v, e := conv.parse_int(s, b, n)
            if e.code != 0 {
                fmt.printf("e%%d %%s|", e.code, e.msg)
            } else {
                fmt.printf("%%d|", v)
            }
            u, e2 := conv.parse_uint(s, b, n)
            if e2.code != 0 {
                fmt.printf("e%%d %%s\n", e2.code, e2.msg)
            } else {
                fmt.printf("%%d\n", u)
            }
        }
    }
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 {
        print("args failed\n")
        return
    }
    for i, a := range args {
        if i > 0 {
            one(a.bytes())
        }
    }
}
`, len(convBases), joinInts(convBases), len(convBitSizes), joinInts(convBitSizes))

// joinInts returns xs in decimal, separated by commas.
func joinInts(xs []int) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = strconv.Itoa(x)
	}
	return strings.Join(s, ", ")
}

// convErrorCode returns the code std/conv gives for err, an error of Go's
// strconv, with std/conv's message: 6 for a syntax error, 7 for a range
// error, 8 for an invalid base and 9 for an invalid bit size.
func convErrorCode(err error) string {
	switch {
	case errors.Is(err, strconv.ErrSyntax):
		return "e6 invalid syntax"
	case errors.Is(err, strconv.ErrRange):
		return "e7 value out of range"
	case strings.Contains(err.Error(), "invalid base"):
		return "e8 invalid base"
	case strings.Contains(err.Error(), "invalid bit size"):
		return "e9 invalid bit size"
	}
	return "unknown error " + err.Error()
}

// convWant returns what convProgram writes for inputs, from Go's
// strconv: for each input, base and bit size, ParseInt's value or error,
// then ParseUint's.
func convWant(inputs []string) string {
	var w strings.Builder
	for _, s := range inputs {
		for _, b := range convBases {
			for _, n := range convBitSizes {
				if v, err := strconv.ParseInt(s, b, n); err != nil {
					w.WriteString(convErrorCode(err) + "|")
				} else {
					fmt.Fprintf(&w, "%d|", v)
				}
				if u, err := strconv.ParseUint(s, b, n); err != nil {
					w.WriteString(convErrorCode(err) + "\n")
				} else {
					fmt.Fprintf(&w, "%d\n", u)
				}
			}
		}
	}
	return w.String()
}

// runConv runs bin, a build of convProgram, on inputs and checks it
// writes what Go's strconv gives, line by line.
func runConv(t *testing.T, bin string, inputs []string) {
	t.Helper()
	out, err := exec.Command(bin, inputs...).Output()
	if err != nil {
		t.Fatalf("conv program failed: %v\n%s", err, out)
	}
	got, want := strings.Split(string(out), "\n"), strings.Split(convWant(inputs), "\n")
	if len(got) != len(want) {
		t.Fatalf("wrote %d lines, want %d", len(got), len(want))
	}
	per := len(convBases) * len(convBitSizes)
	for i := range want {
		if got[i] != want[i] {
			k := i % per
			t.Errorf("parse of %q in base %d, bit size %d: got %q, want %q",
				inputs[i/per], convBases[k/len(convBitSizes)], convBitSizes[k%len(convBitSizes)],
				got[i], want[i])
		}
	}
}

// randomConvInputs returns n inputs of up to 24 bytes from a fixed seed,
// so a failure repeats, drawn from the bytes Go's rules are about: every
// digit, the letters of the prefixes and of base 36's ends in both cases,
// signs, underscores, a space and a 0xff byte. A run of digits makes each
// fourth input, so values near and past each bit size's limits come up.
func randomConvInputs(n int) []string {
	r := rand.New(rand.NewPCG(5, 6))
	const alphabet = "0123456789abfxzABFXZoO_+- \xff"
	inputs := make([]string, n)
	for i := range inputs {
		b := make([]byte, r.IntN(25))
		for j := range b {
			if i%4 == 0 {
				b[j] = '0' + byte(r.IntN(10))
			} else {
				b[j] = alphabet[r.IntN(len(alphabet))]
			}
		}
		inputs[i] = string(b)
	}
	return inputs
}

// TestConvMatchesGo builds convProgram and checks that std/conv parses
// 1500 random inputs, in every base and bit size of convBases and
// convBitSizes, exactly as Go's strconv does: the same value, or the
// same error.
func TestConvMatchesGo(t *testing.T) {
	bin := buildWithStd(t, convProgram)
	inputs := randomConvInputs(1500)
	inputs = append(inputs, "", "-", "+", "0", "-0", "0x", "0b", "0o", "_", "-9223372036854775808",
		"18446744073709551615", "18446744073709551616", "0x_1", "1_0", strconv.Itoa(math.MinInt32))
	runConv(t, bin, inputs)
}

// FuzzConvMatchesGo builds convProgram once and checks, for each input
// the fuzzer makes, that std/conv parses it in every base and bit size of
// convBases and convBitSizes exactly as Go's strconv does. An input
// holding a 0 byte cannot be a program's argument, so it is skipped;
// examples/conv parses those.
func FuzzConvMatchesGo(f *testing.F) {
	for _, s := range []string{
		"", "0", "-1", "+0x_f", "0b101", "0o17", "017", "1_000",
		"18446744073709551616", "-9223372036854775809", "zz", "\xff",
	} {
		f.Add(s)
	}
	bin := buildWithStd(f, convProgram)
	f.Fuzz(func(t *testing.T, s string) {
		if strings.IndexByte(s, 0) >= 0 {
			t.Skip("a program's argument cannot hold a 0 byte")
		}
		runConv(t, bin, []string{s})
	})
}

// convCase matches a case of examples/conv's tables, a call to ints or
// uints with a string literal, a base, a bit size, the value expected and
// the error code expected.
var convCase = regexp.MustCompile(
	`^    (ints|uints)\("((?:[^"\\]|\\x[0-9a-f]{2})*)", (-?\d+), (-?\d+), (-?\d+), (\d+)\)$`)

// unquoteConvCase returns the bytes of lit, a string literal of
// examples/conv's tables without its quotes, in which only \x escapes
// appear.
func unquoteConvCase(t *testing.T, lit string) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < len(lit); i++ {
		if lit[i] != '\\' {
			b.WriteByte(lit[i])
			continue
		}
		v, err := strconv.ParseUint(lit[i+2:i+4], 16, 8)
		if err != nil {
			t.Fatalf("bad escape in %q: %v", lit, err)
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String()
}

// TestConvExampleTablesMatchGo checks every case of examples/conv's
// tables against Go's strconv: the value it expects, when it expects no
// error, and the error code it expects, so the expectations the example
// asserts are Go's, not copied from std/conv's output.
func TestConvExampleTablesMatchGo(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "conv", "main.tck"))
	n := 0
	for line := range strings.SplitSeq(src, "\n") {
		if !strings.HasPrefix(line, "    ints(") && !strings.HasPrefix(line, "    uints(") {
			continue
		}
		m := convCase.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("case not understood: %.80s", line)
			continue
		}
		n++
		s := unquoteConvCase(t, m[2])
		base, _ := strconv.Atoi(m[3])
		bits, _ := strconv.Atoi(m[4])
		var got string
		var err error
		if m[1] == "ints" {
			var v int64
			v, err = strconv.ParseInt(s, base, bits)
			got = strconv.FormatInt(v, 10)
		} else {
			var v uint64
			v, err = strconv.ParseUint(s, base, bits)
			got = strconv.FormatUint(v, 10)
		}
		code := "0"
		if err != nil {
			code = strings.Fields(convErrorCode(err))[0][1:]
			got = "0"
		}
		if got != m[5] || code != m[6] {
			t.Errorf("%s(%.40q, %d, %d) expects %s, code %s; Go gives %s, code %s",
				m[1], s, base, bits, m[5], m[6], got, code)
		}
	}
	if n < 1500 {
		t.Errorf("found %d cases, want at least 1500", n)
	}
}

// TestConvExampleIsSanitizerClean builds examples/conv with clang's
// warnings as errors and the address and undefined behavior sanitizers
// on, every finding fatal, and checks it writes exactly its output, every
// check ok: std/conv, written in Tack, reads no view past its end and
// overflows nothing C counts as undefined, on inputs thousands of bytes
// long too.
func TestConvExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "conv")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/conv/output expects a check to fail")
	}
	bin := buildWithStd(t, src, "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("conv failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	if string(out) != want {
		t.Errorf("conv wrote\n%q\nwant\n%q", out, want)
	}
}

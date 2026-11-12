package c_test

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestFmtAppendfExampleIsSanitizerClean builds examples/fmt-appendf and
// its C file, live.c, with clang's warnings as errors and the address
// and undefined behavior sanitizers on, every finding fatal, and checks
// it writes exactly its output, every check ok: a failed appendf leaves
// its list as it was, and every buffer is freed.
func TestFmtAppendfExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "fmt-appendf", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "fmt-appendf", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/fmt-appendf/output expects a check to fail")
	}
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		if out := runExample(t, buildWithStdAndC(t, src, csrc, flags...)); out != want {
			t.Errorf("fmt-appendf example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

// appendfSep is what both programs of TestAppendfMatchesPrintf write
// after each case, so their outputs split into the same cases. No case's
// format or value holds it.
const appendfSep = "\x00SEP\x00\n"

// appendfCase is one call both programs make: the Tack expression of
// each argument after the format, and the format itself. known says the
// format may be written as a literal, which the compiler checks: every
// verb in it takes its argument, and the counts match. Every case is
// also made through show, which passes the format on as a variable the
// compiler does not know.
type appendfCase struct {
	format string
	args   []string
	known  bool
}

// allVerbs is a format with every verb printf knows, three it does not,
// one of them a byte that is no UTF-8, and %%, each taking the same
// value but %%: twelve arguments.
const allVerbs = "[%d|%x|%s|%t|%c|%v|%g|%e|%f|%z|%é|%%|%\xff]"

// tackString returns s as a Tack string literal, every byte that is not
// printable ASCII written as \x and two hexadecimal digits.
func tackString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "\\x%02x", c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// valueCases returns, for each value, a case of allVerbs given it twelve
// times, through show only, and a case of the verbs that take its type
// with a known format: format holds those verbs, each taking expr.
func valueCases(expr, format string) []appendfCase {
	all := make([]string, 12)
	for i := range all {
		all[i] = expr
	}
	known := []string{}
	for range strings.Count(format, "%") - 2*strings.Count(format, "%%") {
		known = append(known, expr)
	}
	return []appendfCase{{allVerbs, all, false}, {format, known, true}}
}

// appendfCases returns the cases TestAppendfMatchesPrintf makes: every
// integer of intCases, bools, strings empty, holding 0 bytes, verbs,
// bytes that are no UTF-8, and long; floats of both sizes at their edges,
// NaN, the infinities and negative zero, edgeFloats among them; views of
// every element type, empty and not, which only a []uint8 is written
// for; and formats with missing, extra and lone verbs, verbs cut short at
// the end, and no verb at all.
func appendfCases(r *rand.Rand) []appendfCase {
	var cases []appendfCase
	for _, ic := range intCases() {
		format := "<%d|%x|%v>"
		if ic.typ == "uint8" {
			format = "<%d|%x|%v|%c>"
		}
		cases = append(cases, valueCases(ic.expr, format)...)
	}
	for _, b := range []string{"true", "false"} {
		cases = append(cases, valueCases(b, "<%t|%v>")...)
	}
	for _, s := range []string{
		"", "a", "hi there", "a\x00b", "\x00", "%d%%%", "%!d(MISSING)", "\xff\xfe", "世界", "é",
		"\xe4\xb8", "line\nbreak\t", strings.Repeat("long", 2000),
	} {
		cases = append(cases, valueCases(tackString(s), "<%s|%v>")...)
	}
	floats := append([]float64{
		math.NaN(), math.Inf(1), math.Inf(-1), math.Copysign(0, -1), 1e6, 999999, 1e-4, 1e-5,
		123456.789, 1e100, -2.5, 0.000123, 1e21, 5e-324,
	}, edgeFloats(r)...)
	for _, x := range floats {
		s := strconv.FormatFloat(x, 'g', -1, 64)
		cases = append(cases, valueCases("fl("+tackString(s)+")", "<%v|%g|%e|%f>")...)
		s32 := strconv.FormatFloat(float64(float32(x)), 'g', -1, 32)
		cases = append(cases, valueCases("fl32("+tackString(s32)+")", "<%v|%g|%e|%f>")...)
	}
	for _, v := range []string{"vb", "vi8", "vi16", "vi32", "vi64", "vu16", "vu32", "vu64", "vsz", "vptr", "vs", "vf32", "vf64"} {
		cases = append(cases, valueCases(v+"[:]", "")...)
		cases = append(cases, valueCases(v+"[0:0]", "")...)
	}
	cases = append(cases, valueCases("vu8[:]", "<%s|%v>")...)
	cases = append(cases, valueCases("vu8[0:0]", "<%s|%v>")...)
	cases = append(cases, valueCases("vu8[1:3]", "<%s|%v>")...)
	cases = append(cases, valueCases("big[:]", "<%s|%v>")...)
	for _, c := range []appendfCase{
		{"", nil, true},
		{"plain text", nil, true},
		{"%%", nil, true},
		{"100%% sure", nil, true},
		{"%d and %d", []string{"1"}, false},
		{"%s %v %d", nil, false},
		{"%d", []string{"1", "2"}, false},
		{"", []string{"1", `"a"`, "true", "int16(-2)", "vu8[:]", "vi64[:]", "fl(\"0.5\")", "fl32(\"-0\")"}, false},
		{"|%v|", []string{"1", "vu8[0:0]", "vs[:]"}, false},
		{"%", nil, false},
		{"100%", []string{"1"}, false},
		{"%d%", []string{"1"}, false},
		{"%\xe4", []string{"1"}, false},
		{"%\xe4\xb8", []string{"1"}, false},
		{"%\xe4\xb8\x96|", []string{"1"}, false},
		{"%\xf0\x9f\x98\x80|", []string{"1"}, false},
		{"%\xf0\x9f|", []string{"1"}, false},
		{"%\x80|%\xc3", []string{"1", "2"}, false},
		{"%\n|", nil, false},
		{"%z", nil, false},
		{strings.Repeat("%d", 300), func() []string {
			a := make([]string, 300)
			for i := range a {
				a[i] = strconv.Itoa(i)
			}
			return a
		}(), true},
	} {
		cases = append(cases, c)
	}
	return cases
}

// appendfProgram returns a program making every case, through printf
// when printf is true, and otherwise through fmt.appendf into a new list
// and fmt.write of it, writing appendfSep after each: each case known is
// made with its format a literal, and every case through show, with its
// format a variable.
func appendfProgram(cases []appendfCase, printf bool) string {
	var b strings.Builder
	b.WriteString(`package main

import conv from "std/conv"
import fmt from "std/fmt"

// fl returns the float64 s holds.
func fl(s string) float64 {
    v, e := conv.parse_float([]uint8(s), 64)
    if e.code != 0 {
        print("bad float\n")
        exit(3)
    }
    return v
}

// fl32 returns the float32 s holds.
func fl32(s string) float32 {
    v, e := conv.parse_float([]uint8(s), 32)
    if e.code != 0 {
        print("bad float\n")
        exit(3)
    }
    return float32(v)
}

// sep marks the end of a case.
func sep() {
    print(` + tackString(appendfSep) + `)
}

`)
	if printf {
		b.WriteString(`// show prints format with args, a format the compiler does not know.
func show(format string, args ...any) {
    fmt.printf(format, args...)
    sep()
}
`)
	} else {
		b.WriteString(`// show appends format with args to a new list, a format the compiler
// does not know, and writes it.
func show(format string, args ...any) {
    mut out := make(list[uint8], c_allocator)
    e := fmt.appendf(mut out, format, args...)
    if e.code != 0 {
        print("out of memory\n")
        exit(4)
    }
    fmt.write(out)
    sep()
}

// done writes out, which a known format was appended to with the error e.
func done(out []uint8, e error) {
    if e.code != 0 {
        print("out of memory\n")
        exit(4)
    }
    fmt.write(out)
    sep()
}
`)
	}
	// The views' arrays are immutable globals, and the cases are split
	// into functions of appendfChunk cases each, since one function
	// holding every case takes clang minutes to compile under the
	// sanitizers. big, a list, is passed to each as a view.
	b.WriteString(`
var vb = [3]bool{true, false, true}
var vi8 = [3]int8{-128, 0, 127}
var vi16 = [2]int16{-32768, 32767}
var vi32 = [2]int32{-2147483648, 2147483647}
var vi64 = [2]int64{-9223372036854775807 - 1, 9223372036854775807}
var vu8 = [4]uint8{'a', 0, 255, 'z'}
var vu16 = [1]uint16{65535}
var vu32 = [1]uint32{4294967295}
var vu64 = [1]uint64{18446744073709551615}
var vsz = [1]unsafe_size_t{4294967295}
var vptr = [1]unsafe_uintptr_t{0}
var vs = [2]string{"a", ""}
var vf32 = [2]float32{0.5, 1.5}
var vf64 = [2]float64{0.25, -1.0}
`)
	chunks := 0
	for start := 0; start < len(cases); start += appendfChunk {
		fmt.Fprintf(&b, "\nfunc cases%d(big []uint8) {\n", chunks)
		chunks++
		for k, c := range cases[start:min(start+appendfChunk, len(cases))] {
			args := ""
			if len(c.args) > 0 {
				args = ", " + strings.Join(c.args, ", ")
			}
			if c.known {
				if printf {
					fmt.Fprintf(&b, "    fmt.printf(%s%s)\n    sep()\n", tackString(c.format), args)
				} else {
					fmt.Fprintf(&b, "    mut out%d := make(list[uint8], c_allocator)\n"+
						"    e%d := fmt.appendf(mut out%d, %s%s)\n    done(out%d, e%d)\n",
						k, k, k, tackString(c.format), args, k, k)
				}
			}
			fmt.Fprintf(&b, "    show(%s%s)\n", tackString(c.format), args)
		}
		b.WriteString("}\n")
	}
	b.WriteString(`
func main() {
    mut big := make(list[uint8], c_allocator)
    for mut i := 0; i < 70000; i++ {
        e := append(mut big, 'q')
        if e.code != 0 {
            print("out of memory\n")
            exit(4)
        }
    }
`)
	for i := range chunks {
		fmt.Fprintf(&b, "    cases%d(big)\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

// appendfChunk is how many cases each function of appendfProgram makes.
const appendfChunk = 40

// runAppendfProgram runs bin, a build of appendfProgram, and returns what
// it wrote, split into its cases.
func runAppendfProgram(t *testing.T, bin string) [][]byte {
	t.Helper()
	out := runExample(t, bin)
	parts := bytes.Split([]byte(out), []byte(appendfSep))
	return parts[:len(parts)-1]
}

// TestAppendfMatchesPrintf checks that std/fmt's appendf, written in
// Tack, appends exactly the bytes printf, written in C, writes, case by
// case over appendfCases: every verb on every type an any holds, its
// edges, and every marker, through a known format and an unknown one.
// Both programs are built with the address and undefined behavior
// sanitizers on, every finding fatal, at -O0 and -O1.
func TestAppendfMatchesPrintf(t *testing.T) {
	cases := appendfCases(rand.New(rand.NewPCG(21, 22)))
	printfSrc, appendfSrc := appendfProgram(cases, true), appendfProgram(cases, false)
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		want := runAppendfProgram(t, buildWithStd(t, printfSrc, flags...))
		got := runAppendfProgram(t, buildWithStd(t, appendfSrc, flags...))
		if len(got) != len(want) {
			t.Fatalf("appendf wrote %d cases, printf %d", len(got), len(want))
		}
		calls := []appendfCase{}
		for _, c := range cases {
			if c.known {
				calls = append(calls, c)
			}
			calls = append(calls, c)
		}
		if len(calls) != len(want) {
			t.Fatalf("printf wrote %d cases, want %d", len(want), len(calls))
		}
		bad := 0
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				bad++
				if bad <= 10 {
					t.Errorf("format %q args %v: appendf wrote %.200q, printf %.200q",
						calls[i].format, calls[i].args, got[i], want[i])
				}
			}
		}
		if bad > 0 {
			t.Errorf("%d of %d cases differ", bad, len(want))
		}
		t.Logf("%d cases", len(want))
	})
}

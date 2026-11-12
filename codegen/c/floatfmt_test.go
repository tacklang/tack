package c_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// floatFmtProgram parses each of its arguments with std/conv's
// parse_float, in bit size 64 and then 32, and writes one line for each:
// for each bit size, the error's code, or the value as printf's %v, %e
// and %f write it, the C formatter, then as std/conv's append_float
// writes it in each of floatFmtForms, the Tack one, each field followed
// by |, the two bit sizes separated by #.
const floatFmtProgram = `package main

import conv from "std/conv"
import fmt from "std/fmt"
import os from "std/os"

// field appends v to b as conv.append_float writes it in format with
// precision prec and bit size bits, and a |, and reports whether both
// appends held.
func field(mut b list[uint8], v float64, format uint8, prec int64, bits int64) bool {
    e := conv.append_float(mut b, v, format, prec, bits)
    if e.code != 0 {
        return false
    }
    e2 := append(mut b, '|')
    return e2.code == 0
}

// one writes the line for the input s.
func one(s []uint8) {
    v, e := conv.parse_float(s, 64)
    if e.code != 0 {
        fmt.printf("e%d", e.code)
    } else {
        fmt.printf("%v|%e|%f|", v, v, v)
        mut b := make(list[uint8], c_allocator)
        ok := field(mut b, v, 'g', -1, 64) && field(mut b, v, 'e', 6, 64) &&
            field(mut b, v, 'f', 6, 64) && field(mut b, v, 'e', -1, 64) &&
            field(mut b, v, 'f', -1, 64) && field(mut b, v, 'g', 3, 64) &&
            field(mut b, v, 'e', 15, 64) && field(mut b, v, 'f', 2, 64) && field(mut b, v, 'G', -1, 64)
        if !ok {
            print("out of memory")
        }
        fmt.write(b[:])
    }
    print("#")
    w, e2 := conv.parse_float(s, 32)
    if e2.code != 0 {
        fmt.printf("e%d", e2.code)
    } else {
        f := float32(w)
        fmt.printf("%v|%e|%f|", f, f, f)
        mut b := make(list[uint8], c_allocator)
        ok := field(mut b, w, 'g', -1, 32) && field(mut b, w, 'e', 6, 32) && field(mut b, w, 'f', 6, 32)
        if !ok {
            print("out of memory")
        }
        fmt.write(b[:])
    }
    print("\n")
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
`

// floatForm is a format and precision append_float is asked for.
type floatForm struct {
	format byte
	prec   int
}

// floatFmtForms are the forms floatFmtProgram writes a float64 in with
// append_float, in order, and floatFmtForms32 a float32.
var (
	floatFmtForms = []floatForm{
		{
			'g',
			-1,
		},
		{'e', 6},
		{'f', 6},
		{'e', -1},
		{'f', -1},
		{'g', 3},
		{'e', 15},
		{'f', 2},
		{'G', -1},
	}
	floatFmtForms32 = []floatForm{{'g', -1}, {'e', 6}, {'f', 6}}
)

// parseCode returns the field floatFmtProgram writes for err, an error of
// Go's strconv.ParseFloat: e6 for bad syntax, e7 for a value out of
// range.
func parseCode(err error) string {
	switch {
	case errors.Is(err, strconv.ErrSyntax):
		return "e6"
	case errors.Is(err, strconv.ErrRange):
		return "e7"
	}
	return "unknown error " + err.Error()
}

// floatFmtWant returns the line floatFmtProgram writes for s, from Go's
// strconv and fmt.
func floatFmtWant(s string) string {
	var w strings.Builder
	if v, err := strconv.ParseFloat(s, 64); err != nil {
		w.WriteString(parseCode(err))
	} else {
		fmt.Fprintf(&w, "%v|%e|%f|", v, v, v)
		for _, f := range floatFmtForms {
			w.WriteString(strconv.FormatFloat(v, f.format, f.prec, 64) + "|")
		}
	}
	w.WriteString("#")
	if v, err := strconv.ParseFloat(s, 32); err != nil {
		w.WriteString(parseCode(err))
	} else {
		f := float32(v)
		fmt.Fprintf(&w, "%v|%e|%f|", f, f, f)
		for _, ff := range floatFmtForms32 {
			w.WriteString(strconv.FormatFloat(v, ff.format, ff.prec, 32) + "|")
		}
	}
	return w.String()
}

// checkCAgreesWithTack checks that, in got, a line floatFmtProgram wrote,
// printf's %v, %e and %f, the C formatter, wrote each bit size's value as
// append_float, the Tack one, did in the same form: the two can never
// drift apart, whatever Go does.
func checkCAgreesWithTack(t *testing.T, input, got string) {
	t.Helper()
	for half, part := range strings.Split(got, "#") {
		f := strings.Split(part, "|")
		if len(f) < 6 {
			continue // an error code, or a line already reported
		}
		for i := range 3 {
			if f[i] != f[3+i] {
				t.Errorf("%q in bit size %d: printf wrote %q, append_float %q",
					input, 64-32*half, f[i], f[3+i])
			}
		}
	}
}

// floatStrings returns, for the float64 x, ways of writing it a parse
// must read back exactly: its shortest form, its exact value, the exact
// halfway point to the next float up, and values a hair either side of
// that halfway point, which must round down and up.
func floatStrings(x float64) []string {
	out := []string{strconv.FormatFloat(x, 'g', -1, 64), strconv.FormatFloat(x, 'e', 25, 64)}
	if math.IsInf(x, 0) || math.IsNaN(x) || x == math.MaxFloat64 {
		return out
	}
	next := math.Nextafter(x, math.Inf(1))
	mid := new(big.Float).SetPrec(2000).SetFloat64(x)
	mid.Add(mid, new(big.Float).SetPrec(2000).SetFloat64(next))
	mid.Quo(mid, big.NewFloat(2))
	exact := mid.Text('e', 800)
	mant, exp, _ := strings.Cut(exact, "e")
	mant = strings.TrimRight(mant, "0")
	out = append(out, mant+"e"+exp)
	if strings.Contains(mant, ".") {
		out = append(out, mant+"1e"+exp)
	}
	return out
}

// floatFmtInputs returns the inputs TestFloatFormattingMatchesGo parses:
// every float of edgeFloats written by floatStrings, specialInputs, and
// random strings of the bytes a float's syntax is made of.
func floatFmtInputs(r *rand.Rand) []string {
	xs := edgeFloats(r)
	in := make([]string, 0, 5*len(xs))
	for _, x := range xs {
		in = append(in, floatStrings(x)...)
	}
	in = append(in, specialInputs()...)
	const alphabet = "0123456789.eE+-_xXpPabfin "
	for range 300 {
		b := make([]byte, r.IntN(12))
		for j := range b {
			b[j] = alphabet[r.IntN(len(alphabet))]
		}
		in = append(in, string(b))
	}
	return in
}

// edgeFloats returns the edges of float64 and float32, values whose
// shortest form is long or whose rounding is a tie, powers of ten and of
// two across the whole range, and random float64 and float32 bit
// patterns, from r.
func edgeFloats(r *rand.Rand) []float64 {
	xs := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.1, 0.5, 1.5, 2.5, 1e23, 5e-324,
		math.SmallestNonzeroFloat64 * 3, 2.2250738585072014e-308,
		math.Nextafter(2.2250738585072014e-308, 0), math.MaxFloat64, -math.MaxFloat64,
		math.MaxFloat32, math.SmallestNonzeroFloat32, 1.1754943508222875e-38, 16777216, 16777217,
		9007199254740992, 9007199254740993, 123456789, 1e21, 1e20, 999999.5, 0.0001, 0.00001,
		1.0 / 3.0, 2.0 / 3.0,
	}
	for e := -324; e <= 308; e += 7 {
		xs = append(xs, math.Pow(10, float64(e)))
	}
	for e := -1074; e <= 1023; e += 37 {
		xs = append(xs, math.Ldexp(1, e))
	}
	for range 150 {
		xs = append(xs, math.Float64frombits(r.Uint64()), float64(math.Float32frombits(r.Uint32())))
	}
	return xs
}

// specialInputs returns inputs no float's own text gives: NaN and the
// infinities spelled every way and misspelled, control bytes where a sign
// goes, bad syntax, underscores in
// and out of place, hexadecimal floats at their edges, values past the
// range either way, and digit strings longer than a decimal holds.
func specialInputs() []string {
	return []string{
		"NaN",
		"nan",
		"NAN",
		"+Inf",
		"-Inf",
		"inf",
		"-inf",
		"Infinity",
		"-infinity",
		"+INFINITY",
		"infin",
		"infx",
		"+nan",
		"-nan",
		// '\r' and '\v' become '-' and '+' when lowered with | 0x20,
		// which is only right for letters.
		"\rInf",
		"\rinfinity",
		"\vInf",
		"\r1",
		"\v1",
		"",
		".",
		"-",
		"e5",
		"1e",
		"1e+",
		"1x",
		".5",
		"5.",
		"1_000.5",
		"1__0",
		"_1",
		"1_",
		"0x1p-2",
		"0X1.8P1",
		"0x1",
		"0x.8p1",
		"0x1p1024",
		"0x1p-1075",
		"0x1.fffffffffffff8p1023",
		"0x1p-1074",
		"0x1.000000000000000001p0",
		"1e400",
		"-1e400",
		"1e-400",
		"1e10000000000",
		"0.000000000000000000000000000000000000000000001e-300",
		strings.Repeat("9", 900),
		"0." + strings.Repeat("0", 400) + "1",
		"1" + strings.Repeat("0", 799) + "1e-800",
		"2.4703282292062327208828439643411068618252990130716238221279284125033775363510437593264991818" +
			"0811533040808125e-324",
		strings.Repeat("1", 820) + "e-820",
	}
}

// TestFloatFormattingMatchesGo builds floatFmtProgram with
// floatSanitizers and runs it on floatFmtInputs, a batch at a time, and
// checks every line is what Go's strconv and fmt give, and that printf's
// C and append_float's Tack wrote each value the same way.
func TestFloatFormattingMatchesGo(t *testing.T) {
	inputs := floatFmtInputs(rand.New(rand.NewPCG(7, 8)))
	atO0Too(t, append([]string{"-O1"}, floatSanitizers...), func(t *testing.T, flags []string) {
		bin := buildWithStd(t, floatFmtProgram, flags...)
		const batch = 100
		for start := 0; start < len(inputs); start += batch {
			chunk := inputs[start:min(start+batch, len(inputs))]
			out, err := exec.Command(bin, chunk...).Output()
			if err != nil {
				t.Fatalf("float program failed: %v\n%s", err, out)
			}
			got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
			if len(got) != len(chunk) {
				t.Fatalf("wrote %d lines for %d inputs", len(got), len(chunk))
			}
			for i, s := range chunk {
				checkCAgreesWithTack(t, s, got[i])
				if want := floatFmtWant(s); got[i] != want {
					t.Errorf("%q: got\n%s\nwant\n%s", s, got[i], want)
				}
			}
		}
	})
	t.Logf("%d inputs", len(inputs))
}

// FuzzFloatFormattingMatchesGo parses the fuzz engine's strings with
// floatFmtProgram, built with floatSanitizers at -O0 and at -O1, and
// checks each line is what Go's strconv and fmt give, and that printf's
// C and append_float's Tack agree.
func FuzzFloatFormattingMatchesGo(f *testing.F) {
	for _, s := range []string{
		"0", "-0", "0.1", "1e23", "5e-324", "1.7976931348623157e308", "1e400", "NaN", "-Inf",
		"0x1p-2", "1_000.5", "2.4703282292062328e-324", "infx", "", ".5e-3",
	} {
		f.Add(s)
	}
	var bins []string
	for _, level := range []string{"-O0", "-O1"} {
		bins = append(bins, buildWithStd(f, floatFmtProgram, append([]string{level}, floatSanitizers...)...))
	}
	f.Fuzz(func(t *testing.T, s string) {
		if strings.IndexByte(s, 0) >= 0 {
			t.Skip("a program's argument cannot hold a 0 byte")
		}
		for _, bin := range bins {
			out, err := exec.Command(bin, s).Output()
			if err != nil {
				t.Fatalf("float program failed on %q: %v\n%s", s, err, out)
			}
			got := strings.TrimSuffix(string(out), "\n")
			checkCAgreesWithTack(t, s, got)
			if want := floatFmtWant(s); got != want {
				t.Errorf("%q: got\n%s\nwant\n%s", s, got, want)
			}
		}
	})
}

// floatHugeProgram writes its first argument, parsed as a float64, with
// std/conv's append_float, in the format its second argument's first
// byte names and with the precision its third, parsed as an int64, gives,
// into a list holding x already. It writes the error's code, the list's
// length, a |, and then the list when it holds at most 4096 bytes.
const floatHugeProgram = `package main

import conv from "std/conv"
import fmt from "std/fmt"
import os from "std/os"

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 4 {
        print("bad arguments\n")
        return
    }
    mut v := 0.0
    mut format := 'e'
    mut prec := 0
    for i, a := range args {
        if i == 1 {
            x, e1 := conv.parse_float(a.bytes(), 64)
            if e1.code != 0 {
                print("bad value\n")
                return
            }
            v = x
        } else if i == 2 {
            for _, c := range a.bytes() {
                format = c
                break
            }
        } else if i == 3 {
            p, e2 := conv.parse_int(a.bytes(), 10, 64)
            if e2.code != 0 {
                print("bad precision\n")
                return
            }
            prec = p
        }
    }
    mut b := make(list[uint8], c_allocator)
    e3 := append(mut b, 'x')
    if e3.code != 0 {
        print("out of memory\n")
        return
    }
    e4 := conv.append_float(mut b, v, format, prec, 64)
    fmt.printf("%d %d|", e4.code, len(b))
    if len(b) <= 4096 {
        fmt.write(b[:])
    }
    print("\n")
}
`

// TestFloatPrecisionFailsPromptlyPastMemory builds floatHugeProgram, with
// floatSanitizers at -O0 and -O1 and without them at -O2, and runs it on
// precisions no list can hold the digits of, 10^12, the largest int64
// and one below it, in every format. %e and %f write a byte for each
// place, so append_float must refuse them with error{code: 1}, leaving
// the list as it was, and do so at once, without growing toward the
// size first; %g writes no more digits than the value has, so it must
// write what Go's strconv writes for it. Precisions that fit, from 0 to
// 1100, must write what Go's strconv writes, byte for byte, the room
// made first changing nothing. Each run gets 60 seconds and must
// stay under 1 GiB; the address sanitizer's malloc is told to return
// null, as the real one does, rather than abort on a size it refuses.
func TestFloatPrecisionFailsPromptlyPastMemory(t *testing.T) {
	values := []string{"0", "0.5", "-1.5", "123456.789", "1e300", "-5e-324"}
	precs := []int64{1_000_000_000_000, math.MaxInt64, math.MaxInt64 - 1}
	builds := map[string][]string{
		"sanitized-O0": append([]string{"-O0"}, floatSanitizers...),
		"sanitized-O1": append([]string{"-O1"}, floatSanitizers...),
		"plain-O2":     {"-O2"},
	}
	for name, flags := range builds {
		t.Run(name, func(t *testing.T) {
			bin := buildWithStd(t, floatHugeProgram, flags...)
			for _, s := range values {
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					t.Fatal(err)
				}
				for _, format := range []byte("eEfgG") {
					for _, prec := range precs {
						want := "1 1|x"
						if format == 'g' || format == 'G' {
							// Go's strconv makes room for prec bytes up
							// front, so it is asked for 1100 digits: a
							// float64 has under 800 significant digits and
							// an exponent under 309, so %g writes the same
							// for every precision past 1100.
							out := "x" + strconv.FormatFloat(v, format, 1100, 64)
							want = fmt.Sprintf("0 %d|%s", len(out), out)
						}
						runHuge(t, bin, s, format, prec, want)
					}
					for _, prec := range []int64{0, 1, 2, 5, 17, 100, 767, 1100} {
						out := "x" + strconv.FormatFloat(v, format, int(prec), 64)
						runHuge(t, bin, s, format, prec, fmt.Sprintf("0 %d|%s", len(out), out))
					}
				}
			}
		})
	}
}

// runHuge runs bin, floatHugeProgram built, on s, format and prec, and
// checks it writes want and a newline within 60 seconds, its peak
// resident memory under 1 GiB.
func runHuge(t *testing.T, bin, s string, format byte, prec int64, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, s, string(format), strconv.FormatInt(prec, 10))
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=allocator_may_return_null=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	start := time.Now()
	out, err := cmd.Output()
	took := time.Since(start)
	if err != nil {
		t.Errorf("%s %c %d: %v after %v\n%s\nstderr:\n%s", s, format, prec, err, took, out, stderr.String())
		return
	}
	if got := strings.TrimSuffix(string(out), "\n"); got != want {
		t.Errorf("%s %c %d: wrote %q, want %q", s, format, prec, got, want)
	}
	if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok && ru.Maxrss >= 1<<20 {
		t.Errorf("%s %c %d: peak resident memory %d KiB, want under 1 GiB", s, format, prec, ru.Maxrss)
	}
}

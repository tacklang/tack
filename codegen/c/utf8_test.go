package c_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// utf8Program reads the file its second argument names and answers for
// it in the mode its first argument names, writing bytes Go's
// unicode/utf8 and unicode are compared with byte for byte:
//
//   - classes: for every rune from 0 to 0x10FFFF, and then every int32
//     the file holds, four bytes each, big-endian, one byte: 0x40 with a
//     bit set for each class std/unicode puts the value in, 1 is_print,
//     2 is_graphic, 4 is_letter, 8 is_digit and 16 is_space.
//   - encode: for the same values, the number of bytes std/utf8's
//     append_rune appends for it, as one byte, and those bytes.
//   - decode: for each record of the file, a two-byte big-endian length
//     and that many bytes, a byte 1 or 0 for whether std/utf8's valid
//     holds for them, and then, for each step of decode across them,
//     from the first byte to past the last, the rune, four bytes
//     big-endian, and the width, one byte; the step past the last is the
//     empty view's, width 0.
const utf8Program = `package main

import fmt from "std/fmt"
import os from "std/os"
import unicode from "std/unicode"
import utf8 from "std/utf8"

// at returns byte i of s, or 0 when s has no byte i.
func at(s []uint8, i int64) uint8 {
    c, ok := s[i]
    if !ok {
        return 0
    }
    return c
}

// out_byte appends c to out, and counts a failure in failed.
func out_byte(mut out list[uint8], c uint8, mut failed bool) {
    e := append(mut out, c)
    if e.code != 0 {
        failed = true
    }
}

// out_int32 appends v to out, big-endian.
func out_int32(mut out list[uint8], v int32, mut failed bool) {
    for mut k uint32 = 0; k < 4; k++ {
        b, ok := uint8(v >> (24 - 8 * k) & 0xff)
        if !ok {
            failed = true
        }
        out_byte(mut out, b, mut failed)
    }
}

// classes appends r's classes to out.
func classes(mut out list[uint8], r int32, mut failed bool) {
    mut c uint8 = 0x40
    if unicode.is_print(r) {
        c |= 1
    }
    if unicode.is_graphic(r) {
        c |= 2
    }
    if unicode.is_letter(r) {
        c |= 4
    }
    if unicode.is_digit(r) {
        c |= 8
    }
    if unicode.is_space(r) {
        c |= 16
    }
    out_byte(mut out, c, mut failed)
}

// encode appends the length of r's encoding, and the encoding, to out.
func encode(mut out list[uint8], r int32, mut failed bool) {
    mut b := make(list[uint8], c_allocator)
    e := utf8.append_rune(mut b, r)
    if e.code != 0 || len(b) > 4 {
        failed = true
        return
    }
    n, ok := uint8(len(b))
    if !ok {
        failed = true
    }
    out_byte(mut out, n, mut failed)
    e2 := append(mut out, b...)
    if e2.code != 0 {
        failed = true
    }
}

// one answers for r in mode, encode or classes.
func one(mut out list[uint8], r int32, encoding bool, mut failed bool) {
    if encoding {
        encode(mut out, r, mut failed)
    } else {
        classes(mut out, r, mut failed)
    }
}

// runes answers for every rune and then every int32 in data.
func runes(mut out list[uint8], data []uint8, encoding bool, mut failed bool) {
    for mut r int32 = 0; r <= 0x10ffff; r++ {
        one(mut out, r, encoding, mut failed)
    }
    for mut i int64 = 0; i +% 3 < len(data); i +%= 4 {
        v := int32(at(data, i)) << 24 | int32(at(data, i +% 1)) << 16 |
            int32(at(data, i +% 2)) << 8 | int32(at(data, i +% 3))
        one(mut out, v, encoding, mut failed)
    }
}

// decode answers for s.
func decode(mut out list[uint8], s []uint8, mut failed bool) {
    if utf8.valid(s) {
        out_byte(mut out, 1, mut failed)
    } else {
        out_byte(mut out, 0, mut failed)
    }
    mut i int64 = 0
    for i >= 0 && i <= len(s) {
        r, w := utf8.decode(s[i:])
        out_int32(mut out, r, mut failed)
        n, ok := uint8(w)
        if !ok {
            failed = true
            return
        }
        out_byte(mut out, n, mut failed)
        if w == 0 {
            return
        }
        i +%= w
    }
}

// records answers for every record of data.
func records(mut out list[uint8], data []uint8, mut failed bool) {
    mut i int64 = 0
    for i >= 0 && i < len(data) && !failed {
        n := int64(at(data, i)) << 8 | int64(at(data, i +% 1))
        start := i +% 2
        end := start +% n
        if start < 0 || end > len(data) || start > end {
            failed = true
            return
        }
        decode(mut out, data[start:end], mut failed)
        i = end
    }
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 3 {
        print("usage: utf8 classes|encode|decode file\n")
        exit(2)
    }
    data, e2 := os.read_file(args[2].bytes(), c_allocator)
    if e2.code != 0 {
        print("cannot read the file\n")
        exit(2)
    }
    mode := at(args[1].bytes(), 0)
    mut out := make(list[uint8], c_allocator)
    mut failed := false
    if mode == 'd' {
        records(mut out, data, mut failed)
    } else {
        runes(mut out, data, mode == 'e', mut failed)
    }
    if failed {
        print("failed\n")
        exit(1)
    }
    fmt.write(out)
}
`

// utf8OddRunes are the int32 values past the runes utf8Program answers
// for after every rune: negative ones, the edges of int32, and values
// just past the last rune, U+10FFFF.
var utf8OddRunes = []int32{
	-1, -2, -0xfffd, -0x10ffff, math.MinInt32, math.MinInt32 + 1, math.MaxInt32, math.MaxInt32 - 1,
	0x110000, 0x110001, 0x1fffff, 0x200000, 0x7fffff00, -0x80, -0xd800,
}

// utf8OddFile returns utf8OddRunes as utf8Program reads them, and the
// runes in the order it answers for them.
func utf8OddFile() ([]byte, []rune) {
	var b []byte
	all := make([]rune, 0, unicode.MaxRune+1+len(utf8OddRunes))
	for r := rune(0); r <= unicode.MaxRune; r++ {
		all = append(all, r)
	}
	for _, r := range utf8OddRunes {
		b = binary.BigEndian.AppendUint32(b, uint32(r))
		all = append(all, r)
	}
	return b, all
}

// utf8Classes returns the byte utf8Program writes in classes mode for
// r, from Go's unicode.
func utf8Classes(r rune) byte {
	c := byte(0x40)
	for i, is := range []func(rune) bool{
		unicode.IsPrint, unicode.IsGraphic, unicode.IsLetter, unicode.IsDigit, unicode.IsSpace,
	} {
		if is(r) {
			c |= 1 << i
		}
	}
	return c
}

// utf8Inputs returns the byte strings TestUTF8DecodeMatchesGo decodes:
// every pair of bytes, each followed by nothing, by one continuation byte
// and by two; every rune's edges encoded, cut short at every length and
// followed by a byte that continues nothing; overlong encodings,
// surrogates and values past U+10FFFF; and random strings from r, of
// any bytes and of valid encodings with bad bytes among them.
func utf8Inputs(r *rand.Rand) [][]byte {
	in := make([][]byte, 0, 3*256*256)
	for b0 := range 256 {
		for b1 := range 256 {
			in = append(in, []byte{byte(b0), byte(b1)}, []byte{byte(b0), byte(b1), 0x80},
				[]byte{byte(b0), byte(b1), 0xbf, 0x80})
		}
	}
	in = append(in, utf8Edges(r)...)
	for _, s := range []string{
		"", "\xc0\x80", "\xc1\xbf", "\xe0\x80\x80", "\xe0\x9f\xbf", "\xf0\x80\x80\x80",
		"\xf0\x8f\xbf\xbf", "\xed\xa0\x80", "\xed\xbf\xbf", "\xf4\x90\x80\x80", "\xf7\xbf\xbf\xbf",
		"\xf8\x88\x80\x80\x80", "\xef\xbf\xbd", "\xef\xbf\xbe",
	} {
		in = append(in, []byte(s))
	}
	in = append(in, bytes.Repeat([]byte("\xe4\xb8\x96"), 1000), bytes.Repeat([]byte{0xff}, 3000))
	for range 20000 {
		in = append(in, randomUTF8(r))
	}
	return in
}

// utf8Edges returns the encodings of the runes at the edges of each
// length and of 2000 random runes from r, each cut short at every
// length, alone, followed by an ASCII byte and followed by 0xc0, which
// continues nothing.
func utf8Edges(r *rand.Rand) [][]byte {
	edges := make([]rune, 0, 2011)
	edges = append(
		edges,
		0,
		0x7f,
		0x80,
		0x7ff,
		0x800,
		0xd7ff,
		0xe000,
		0xfffd,
		0xffff,
		0x10000,
		0x10ffff,
	)
	for range 2000 {
		edges = append(edges, rune(r.IntN(unicode.MaxRune+1)))
	}
	var in [][]byte
	for _, e := range edges {
		enc := utf8.AppendRune(nil, e)
		for n := 0; n <= len(enc); n++ {
			in = append(in, enc[:n], append(enc[:n:n], 'a'), append(enc[:n:n], 0xc0))
		}
	}
	return in
}

// randomUTF8 returns a random byte string from r: up to 15 bytes of any
// value, or, as often, valid encodings of random runes with a random
// byte among them one time in five, 16 bytes or a few more.
func randomUTF8(r *rand.Rand) []byte {
	if r.IntN(2) == 0 {
		b := make([]byte, r.IntN(16))
		for j := range b {
			b[j] = byte(r.IntN(256))
		}
		return b
	}
	b := make([]byte, 0, 20)
	for len(b) < 16 {
		if r.IntN(5) == 0 {
			b = append(b, byte(r.IntN(256)))
		} else {
			b = utf8.AppendRune(b, rune(r.IntN(unicode.MaxRune+1)))
		}
	}
	return b
}

// utf8DecodeWant returns what utf8Program writes in decode mode for s,
// from Go's unicode/utf8.
func utf8DecodeWant(s []byte) []byte {
	var out []byte
	if utf8.Valid(s) {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	for i := 0; ; {
		r, w := utf8.DecodeRune(s[i:])
		out = binary.BigEndian.AppendUint32(out, uint32(r))
		out = append(out, byte(w))
		if w == 0 {
			return out
		}
		i += w
	}
}

// runUTF8 runs bin, a build of utf8Program, in mode on the file holding
// data, and returns what it wrote.
func runUTF8(t *testing.T, bin, mode string, data []byte) []byte {
	t.Helper()
	return runOnFile(t, bin, data, mode)
}

// runOnFile writes data to a file and runs bin with args and then the
// file's path, and returns what it wrote, failing t if it did not exit
// 0.
func runOnFile(t *testing.T, bin string, data []byte, args ...string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd := exec.Command(bin, append(args, path)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf(
			"%s %s failed: %v\n%.200s\nstderr:\n%s",
			filepath.Base(bin),
			strings.Join(args, " "),
			err,
			out,
			stderr.String(),
		)
	}
	return out
}

// runExample runs bin, a build of an example, and returns what it wrote,
// failing t if it did not exit 0.
func runExample(t *testing.T, bin string) string {
	t.Helper()
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("example failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	return string(out)
}

// utf8Sanitizers are the flags every build of utf8Program and
// quoteProgram takes: the address and undefined behavior sanitizers,
// every finding fatal, so std/utf8, std/unicode and std/conv's quoting
// read no view or table past its end and overflow nothing C counts as
// undefined, on every input the tests give them.
var utf8Sanitizers = []string{
	"-O1",
	"-fsanitize=address,undefined",
	useAfterReturn,
	"-fno-sanitize-recover=all",
}

// TestUnicodeClassesMatchGo checks that std/unicode's is_print,
// is_graphic, is_letter, is_digit and is_space answer for every rune,
// 0 to 0x10FFFF, and for negative and out-of-range int32 values, exactly
// as Go's unicode.IsPrint, IsGraphic, IsLetter, IsDigit and IsSpace do,
// and is_print as strconv.IsPrint, which strconv.Quote asks, does.
func TestUnicodeClassesMatchGo(t *testing.T) {
	data, all := utf8OddFile()
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		bin := buildWithStd(t, utf8Program, flags...)
		got := runUTF8(t, bin, "classes", data)
		if len(got) != len(all) {
			t.Fatalf("wrote %d bytes for %d runes", len(got), len(all))
		}
		bad := 0
		for i, r := range all {
			want := utf8Classes(r)
			if got[i] != want && bad < 20 {
				bad++
				t.Errorf(
					"rune %#x: classes %05b, Go's %05b (print, graphic, letter, digit, space from the right)",
					r,
					got[i]&0x1f,
					want&0x1f,
				)
			}
		}
	})
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsPrint(r) != strconv.IsPrint(r) {
			t.Fatalf(
				"rune %#x: unicode.IsPrint and strconv.IsPrint differ, so is_print cannot match both",
				r,
			)
		}
	}
}

// TestUTF8EncodeMatchesGo checks that std/utf8's append_rune encodes
// every rune, 0 to 0x10FFFF, the surrogates among them, and negative and
// out-of-range int32 values, exactly as Go's utf8.AppendRune does.
func TestUTF8EncodeMatchesGo(t *testing.T) {
	data, all := utf8OddFile()
	var want []byte
	for _, r := range all {
		enc := utf8.AppendRune(nil, r)
		want = append(want, byte(len(enc)))
		want = append(want, enc...)
	}
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		bin := buildWithStd(t, utf8Program, flags...)
		got := runUTF8(t, bin, "encode", data)
		if !bytes.Equal(got, want) {
			for _, r := range all {
				enc := utf8.AppendRune(nil, r)
				if len(got) < 1+len(enc) ||
					!bytes.Equal(got[:1+len(enc)], append([]byte{byte(len(enc))}, enc...)) {
					t.Fatalf(
						"rune %#x: wrote % x, Go's %x is % x",
						r,
						got[:min(len(got), 5)],
						r,
						enc,
					)
				}
				got = got[1+len(enc):]
			}
			t.Fatal("wrote more than Go")
		}
	})
}

// utf8Records returns inputs as utf8Program reads them in decode mode.
func utf8Records(t testing.TB, inputs [][]byte) []byte {
	var data []byte
	for _, s := range inputs {
		if len(s) > math.MaxUint16 {
			t.Fatalf("an input of %d bytes is too long for a record", len(s))
		}
		data = binary.BigEndian.AppendUint16(data, uint16(len(s)))
		data = append(data, s...)
	}
	return data
}

// checkUTF8Decode runs bin, a build of utf8Program, in decode mode on
// inputs, and checks that what it wrote for each is what Go's
// utf8.Valid and DecodeRune give.
func checkUTF8Decode(t *testing.T, bin string, inputs [][]byte) {
	t.Helper()
	got := runUTF8(t, bin, "decode", utf8Records(t, inputs))
	for _, s := range inputs {
		want := utf8DecodeWant(s)
		if len(got) < len(want) || !bytes.Equal(got[:len(want)], want) {
			t.Fatalf(
				"decoding % x: wrote % x, Go's is % x",
				s,
				got[:min(len(got), len(want))],
				want,
			)
		}
		got = got[len(want):]
	}
	if len(got) != 0 {
		t.Fatalf("wrote %d bytes more than Go", len(got))
	}
}

// TestUTF8DecodeMatchesGo checks that std/utf8's decode and valid give,
// for every byte string of utf8Inputs, exactly what Go's utf8.DecodeRune
// and Valid give, at every step of decoding each from its start to past
// its end.
func TestUTF8DecodeMatchesGo(t *testing.T) {
	inputs := utf8Inputs(rand.New(rand.NewPCG(11, 12)))
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		checkUTF8Decode(t, buildWithStd(t, utf8Program, flags...), inputs)
	})
	t.Logf("%d inputs", len(inputs))
}

// FuzzUTF8DecodeMatchesGo checks, for each byte string the fuzzer makes,
// that std/utf8's decode and valid give what Go's do, as
// TestUTF8DecodeMatchesGo does.
func FuzzUTF8DecodeMatchesGo(f *testing.F) {
	for _, s := range []string{"", "a", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe4\xb8", "世界"} {
		f.Add([]byte(s))
	}
	bin := buildWithStd(f, utf8Program, utf8Sanitizers...)
	f.Fuzz(func(t *testing.T, s []byte) {
		if len(s) > math.MaxUint16 {
			t.Skip("too long for a record")
		}
		checkUTF8Decode(t, bin, [][]byte{s})
	})
}

// TestUTF8ExampleIsSanitizerClean builds examples/utf8 and its C file,
// live.c, with clang's warnings as errors and utf8Sanitizers, and checks
// it writes exactly its output, every check ok, a failed append_rune
// leaving its list as it was and every buffer freed among them.
func TestUTF8ExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "utf8", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "utf8", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/utf8/output expects a check to fail")
	}
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		if out := runExample(t, buildWithStdAndC(t, src, csrc, flags...)); out != want {
			t.Errorf("utf8 example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

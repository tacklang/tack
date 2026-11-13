package c_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// jsonScalarProgram reads the file its first argument names, records as
// utf8Program's decode mode reads them, a two-byte big-endian length and
// that many bytes, and writes a line for each: what std/json writes for
// the record, or "error" and the code of the error it returned. A
// record's first byte says what the rest of it, its payload, is:
//
//   - s: a Text holding the payload's bytes, through encode;
//   - w: the payload's bytes through a Writer's string;
//   - k: an object of one member, the payload's bytes as its key and
//     null as its value, through a Writer;
//   - f, g: a float64, or a float32, std/conv's parse_float of the
//     payload, through encode;
//   - x: a float64 as for f, through a Writer's float;
//   - a, b, c, d: an int8, an int16, an int32 or an int64, std/conv's
//     parse_int of the payload in decimal, through encode;
//   - e, h, m, n, z: a uint8, a uint16, a uint32, a uint64 or an
//     unsafe_size_t, std/conv's parse_uint of it, through encode;
//   - v: a uint64 as for n, through a Writer's uint, and i an int64 as
//     for d, through its int.
//
// A record it cannot read prints "bad record" and exits 1.
const jsonScalarProgram = `package main

import bytes from "std/bytes"
import conv from "std/conv"
import fmt from "std/fmt"
import json from "std/json"
import os from "std/os"
import text from "std/text"

// at returns byte i of s, or 0 when s has no byte i.
func at(s []uint8, i int64) uint8 {
    c, ok := s[i]
    if !ok {
        return 0
    }
    return c
}

// bad is the error for a record whose payload does not fit its kind.
var bad = error{code: 99, msg: "bad record"}

// written appends what w finished holding to out.
func written(mut w json.Writer, mut out list[uint8]) error {
    b := mut w.finish(c_allocator)?
    return append(mut out, b...)
}

// one appends what std/json writes for the record of kind k and payload
// p to out.
func one(mut out list[uint8], k uint8, p []uint8) error {
    if k == 's' {
        t := text.from_bytes(p, c_allocator)?
        return json.append_encode(mut out, t, c_allocator)
    }
    if k == 'w' || k == 'k' || k == 'x' || k == 'v' || k == 'i' {
        mut w := json.new_writer(c_allocator)
        if k == 'w' {
            mut w.string(p)?
        } else if k == 'x' {
            mut w.float(conv.parse_float(p, 64)?)?
        } else if k == 'v' {
            mut w.uint(conv.parse_uint(p, 10, 64)?)?
        } else if k == 'i' {
            mut w.int(conv.parse_int(p, 10, 64)?)?
        } else {
            mut w.begin_object()?
            mut w.key(p)?
            mut w.null()?
            mut w.end_object()?
        }
        return written(mut w, mut out)
    }
    if k == 'f' {
        return json.append_encode(mut out, conv.parse_float(p, 64)?, c_allocator)
    }
    if k == 'g' {
        f := conv.parse_float(p, 32)?
        return json.append_encode(mut out, float32(f), c_allocator)
    }
    if k == 'a' || k == 'b' || k == 'c' || k == 'd' {
        n := conv.parse_int(p, 10, 64)?
        if k == 'a' {
            v, ok := int8(n)
            if !ok {
                return bad
            }
            return json.append_encode(mut out, v, c_allocator)
        }
        if k == 'b' {
            v, ok := int16(n)
            if !ok {
                return bad
            }
            return json.append_encode(mut out, v, c_allocator)
        }
        if k == 'c' {
            v, ok := int32(n)
            if !ok {
                return bad
            }
            return json.append_encode(mut out, v, c_allocator)
        }
        return json.append_encode(mut out, n, c_allocator)
    }
    u := conv.parse_uint(p, 10, 64)?
    if k == 'e' {
        v, ok := uint8(u)
        if !ok {
            return bad
        }
        return json.append_encode(mut out, v, c_allocator)
    }
    if k == 'h' {
        v, ok := uint16(u)
        if !ok {
            return bad
        }
        return json.append_encode(mut out, v, c_allocator)
    }
    if k == 'm' {
        v, ok := uint32(u)
        if !ok {
            return bad
        }
        return json.append_encode(mut out, v, c_allocator)
    }
    if k == 'z' {
        v, ok := unsafe_size_t(u)
        if !ok {
            return bad
        }
        return json.append_encode(mut out, v, c_allocator)
    }
    if k == 'n' {
        return json.append_encode(mut out, u, c_allocator)
    }
    return bad
}

// fail exits, saying why.
func fail(s string) {
    print(s)
    print("\n")
    exit(1)
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 2 {
        print("usage: json file\n")
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
        body := start +% 1
        if start < 0 || end > len(data) || body < 0 || body > end {
            print("bad record\n")
            exit(1)
        }
        before := len(out)
        e3 := one(mut out, at(data, start), data[body:end])
        if e3.code == 99 || e3.code == 6 || e3.code == 7 {
            fail("bad record")
        }
        if e3.code != 0 {
            // A refused value leaves out as it was.
            if len(out) != before {
                fail("a refused value changed out")
            }
            if append(mut out, 'e', 'r', 'r', 'o', 'r', ' ').code != 0 ||
                bytes.append_int(mut out, int64(e3.code)).code != 0 {
                fail("out of memory")
            }
        }
        if append(mut out, '\n').code != 0 {
            fail("out of memory")
        }
        i = end
    }
    fmt.write(out)
}
`

// jsonRecord is one record of jsonScalarProgram's input: its kind, its
// payload, and what Go's json.Marshal gives for the value it stands
// for, or the error line when Go refuses it.
type jsonRecord struct {
	kind    byte
	payload []byte
	want    string
}

// marshalLine returns Go's json.Marshal of v, or "error 11", std/json's
// line for the value it refuses, when Go refuses v, which it does only
// for NaN and the infinities among what the tests give it.
func marshalLine(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		var unsupported *json.UnsupportedValueError
		if !errors.As(err, &unsupported) {
			t.Fatalf("json.Marshal(%#v): %v", v, err)
		}
		return "error 11"
	}
	return string(b)
}

// stringRecords returns the records for s, as a Text through encode, as
// a Writer's string, and as a Writer's key.
func stringRecords(t testing.TB, s []byte) []jsonRecord {
	return []jsonRecord{
		{'s', s, marshalLine(t, string(s))},
		{'w', s, marshalLine(t, string(s))},
		{'k', s, marshalLine(t, map[string]any{string(s): nil})},
	}
}

// float64Records returns the records for f, through encode and a
// Writer.
func float64Records(t testing.TB, f float64) []jsonRecord {
	s := []byte(strconv.FormatFloat(f, 'g', -1, 64))
	want := marshalLine(t, f)
	return []jsonRecord{{'f', s, want}, {'x', s, want}}
}

// float32Record returns the record for f, through encode.
func float32Record(t testing.TB, f float32) jsonRecord {
	return jsonRecord{'g', []byte(strconv.FormatFloat(float64(f), 'g', -1, 32)), marshalLine(t, f)}
}

// intRecords returns the records for n as each signed type it fits,
// through encode, and as an int64 through a Writer.
func intRecords(t testing.TB, n int64) []jsonRecord {
	s := []byte(strconv.FormatInt(n, 10))
	recs := []jsonRecord{{'d', s, marshalLine(t, n)}, {'i', s, marshalLine(t, n)}}
	if n == int64(int8(n)) {
		recs = append(recs, jsonRecord{'a', s, marshalLine(t, int8(n))})
	}
	if n == int64(int16(n)) {
		recs = append(recs, jsonRecord{'b', s, marshalLine(t, int16(n))})
	}
	if n == int64(int32(n)) {
		recs = append(recs, jsonRecord{'c', s, marshalLine(t, int32(n))})
	}
	return recs
}

// uintRecords returns the records for u as each unsigned type it fits,
// through encode, and as a uint64 through a Writer.
func uintRecords(t testing.TB, u uint64) []jsonRecord {
	s := []byte(strconv.FormatUint(u, 10))
	recs := []jsonRecord{
		{'n', s, marshalLine(t, u)},
		{'v', s, marshalLine(t, u)},
		{'z', s, marshalLine(t, uintptr(u))},
	}
	if u == uint64(uint8(u)) {
		recs = append(recs, jsonRecord{'e', s, marshalLine(t, uint8(u))})
	}
	if u == uint64(uint16(u)) {
		recs = append(recs, jsonRecord{'h', s, marshalLine(t, uint16(u))})
	}
	if u == uint64(uint32(u)) {
		recs = append(recs, jsonRecord{'m', s, marshalLine(t, uint32(u))})
	}
	return recs
}

// jsonStringParts are the pieces random strings are made of: each byte
// and rune escaping treats apart, and bad bytes and runes of each
// length.
var jsonStringParts = []string{
	"\"", "\\", "/", "<", ">", "&", "\b", "\f", "\n", "\r", "\t", "\v", "\x00", "\x1f",
	"\x7f", " ", "a", "'", "\u2028", "\u2029", "\u202a", "\u2027", "\xef\xbf\xbd", "\xff",
	"\xfe", "\x80", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe4\xb8", "\xe2\x80",
	"\u00a0", "é", "世", "😀", "\U0010ffff", "\ufeff",
}

// randomString returns a string of up to n pieces of jsonStringParts.
func randomString(r *rand.Rand, n int) string {
	var b strings.Builder
	for range r.IntN(n + 1) {
		b.WriteString(jsonStringParts[r.IntN(len(jsonStringParts))])
	}
	return b.String()
}

// interestingFloats returns float64s on each side of Go's switch to an
// exponent, at 1e21 and 1e-6, a few steps either way; the powers of ten
// from 1e-30 to 1e30 and their neighbours; the extremes; zeros; NaN and
// the infinities; and random bit patterns from r.
func interestingFloats(r *rand.Rand) []float64 {
	var fs []float64
	near := func(f float64, steps int) {
		lo, hi := f, f
		fs = append(fs, f)
		for range steps {
			lo = math.Nextafter(lo, math.Inf(-1))
			hi = math.Nextafter(hi, math.Inf(1))
			fs = append(fs, lo, hi)
		}
	}
	near(1e21, 8)
	near(1e-6, 8)
	near(1e-7, 2)
	near(1e20, 2)
	for e := -30; e <= 30; e++ {
		near(math.Pow(10, float64(e)), 1)
	}
	near(math.MaxFloat64, 1)
	near(math.SmallestNonzeroFloat64, 1)
	near(0x1p-1022, 1)
	near(1, 2)
	near(0.1, 1)
	near(123456789, 1)
	fs = append(fs, 0, math.Copysign(0, -1), math.NaN(), math.Inf(1), math.Inf(-1))
	for _, f := range slices.Clone(fs) {
		fs = append(fs, -f)
	}
	for range 30000 {
		fs = append(fs, math.Float64frombits(r.Uint64()))
	}
	return fs
}

// interestingFloat32s returns float32s as interestingFloats does
// float64s, with float32's own extremes.
func interestingFloat32s(r *rand.Rand) []float32 {
	var fs []float32
	near := func(f float32, steps int) {
		lo, hi := f, f
		fs = append(fs, f)
		for range steps {
			lo = math.Nextafter32(lo, float32(math.Inf(-1)))
			hi = math.Nextafter32(hi, float32(math.Inf(1)))
			fs = append(fs, lo, hi)
		}
	}
	near(1e21, 8)
	near(1e-6, 8)
	near(1e-7, 2)
	for e := -30; e <= 30; e++ {
		near(float32(math.Pow(10, float64(e))), 1)
	}
	near(math.MaxFloat32, 1)
	near(math.SmallestNonzeroFloat32, 1)
	near(16777216, 2)
	fs = append(fs, 0, float32(math.Copysign(0, -1)), float32(math.NaN()),
		float32(math.Inf(1)), float32(math.Inf(-1)))
	for _, f := range slices.Clone(fs) {
		fs = append(fs, -f)
	}
	for range 30000 {
		fs = append(fs, math.Float32frombits(r.Uint32()))
	}
	return fs
}

// interestingInts returns int64s at the edges of every signed width,
// and random ones of each width from r.
func interestingInts(r *rand.Rand) []int64 {
	ns := []int64{0, 1, -1, 9, 10, -10, 99, 100}
	for _, bits := range []int{8, 16, 32, 64} {
		lo := int64(-1) << (bits - 1)
		hi := -(lo + 1)
		ns = append(ns, lo, lo+1, hi, hi-1)
		for range 200 {
			ns = append(ns, int64(r.Uint64())>>(64-bits))
		}
	}
	return ns
}

// interestingUints returns uint64s at the edges of every unsigned
// width, and random ones of each width from r.
func interestingUints(r *rand.Rand) []uint64 {
	us := []uint64{0, 1, 9, 10, 99, 100}
	for _, bits := range []int{8, 16, 32, 64} {
		hi := uint64(math.MaxUint64) >> (64 - bits)
		us = append(us, hi, hi-1, hi/2, hi/2+1)
		for range 200 {
			us = append(us, r.Uint64()>>(64-bits))
		}
	}
	return us
}

// jsonScalarRecords returns the records TestJSONScalarsMatchGo checks:
// every single byte and every rune encoded on its own as a string,
// random strings of jsonStringParts, long strings, and the floats and
// integers above.
func jsonScalarRecords(t testing.TB, r *rand.Rand) []jsonRecord {
	var recs []jsonRecord
	for b := range 256 {
		recs = append(recs, stringRecords(t, []byte{byte(b)})...)
	}
	for c := rune(0x80); c <= utf8.MaxRune; c += 1 + rune(r.IntN(64)) {
		recs = append(recs, stringRecords(t, utf8.AppendRune(nil, c))...)
	}
	for _, c := range []rune{0x2028, 0x2029, 0xfffd, 0xd800, 0xdfff, utf8.MaxRune} {
		recs = append(recs, stringRecords(t, utf8.AppendRune(nil, c))...)
	}
	recs = append(recs, stringRecords(t, []byte{})...)
	for range 5000 {
		recs = append(recs, stringRecords(t, []byte(randomString(r, 12)))...)
	}
	for _, s := range []string{
		strings.Repeat("\xff", 3000), strings.Repeat("<\"\\\n", 3000),
		strings.Repeat("\u2028", 3000), strings.Repeat("世", 3000),
	} {
		recs = append(recs, stringRecords(t, []byte(s))...)
	}
	for _, f := range interestingFloats(r) {
		recs = append(recs, float64Records(t, f)...)
	}
	for _, f := range interestingFloat32s(r) {
		recs = append(recs, float32Record(t, f))
	}
	for _, n := range interestingInts(r) {
		recs = append(recs, intRecords(t, n)...)
	}
	for _, u := range interestingUints(r) {
		recs = append(recs, uintRecords(t, u)...)
	}
	return recs
}

// checkJSONScalars runs bin, a build of jsonScalarProgram, on recs, and
// checks it wrote each record's line exactly as Go's json.Marshal gives
// it.
func checkJSONScalars(t *testing.T, bin string, recs []jsonRecord) {
	t.Helper()
	inputs := make([][]byte, len(recs))
	for i, rec := range recs {
		inputs[i] = append([]byte{rec.kind}, rec.payload...)
	}
	got := runOnFile(t, bin, utf8Records(t, inputs))
	for _, rec := range recs {
		want := rec.want + "\n"
		if !bytes.HasPrefix(got, []byte(want)) {
			line, _, _ := bytes.Cut(got, []byte("\n"))
			t.Fatalf("record %c %q: wrote %q, Go's json.Marshal gives %q", rec.kind, rec.payload, line,
				rec.want)
		}
		got = got[len(want):]
	}
	if len(got) != 0 {
		t.Fatalf("wrote %d bytes more than Go", len(got))
	}
}

// jsonSanitizers are the flags every build of the json programs takes:
// the address, leak and undefined behavior sanitizers, every finding
// fatal, so std/json reads no view past its end, frees what it
// allocates exactly once and overflows nothing C counts as undefined.
var jsonSanitizers = utf8Sanitizers

// TestJSONScalarsMatchGo checks that std/json writes every string, float
// and integer of jsonScalarRecords, through encode and through a Writer,
// exactly as Go's json.Marshal does, NaN and the infinities refused as
// Go refuses them, built with jsonSanitizers at -O0 and -O1.
func TestJSONScalarsMatchGo(t *testing.T) {
	recs := jsonScalarRecords(t, rand.New(rand.NewPCG(21, 22)))
	atO0Too(t, jsonSanitizers, func(t *testing.T, flags []string) {
		checkJSONScalars(t, buildWithStd(t, jsonScalarProgram, flags...), recs)
	})
	t.Logf("%d records", len(recs))
}

// FuzzJSONMatchesGo checks, for each byte string the fuzzer makes, that
// std/json writes it as a string, through encode, a Writer's string and
// a Writer's key, exactly as Go's json.Marshal does, and, read as the
// bits of a float64 and a float32, those floats too.
func FuzzJSONMatchesGo(f *testing.F) {
	for _, s := range []string{"", "a", "\"\\", "<>&", "\x00\x7f", "\xc0\x80", "\xed\xa0\x80",
		"\u2028\u2029", "\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8", "世界\xff"} {
		f.Add([]byte(s))
	}
	bin := buildWithStd(f, jsonScalarProgram, jsonSanitizers...)
	f.Fuzz(func(t *testing.T, s []byte) {
		if len(s) > math.MaxUint16-1 {
			t.Skip("too long for a record")
		}
		recs := stringRecords(t, s)
		if len(s) >= 8 {
			recs = append(recs, float64Records(t, math.Float64frombits(leUint64(s)))...)
			recs = append(recs, float32Record(t, math.Float32frombits(uint32(leUint64(s)))))
		}
		checkJSONScalars(t, bin, recs)
	})
}

// leUint64 returns the first 8 bytes of s as a little-endian uint64.
func leUint64(s []byte) uint64 {
	var u uint64
	for i := 7; i >= 0; i-- {
		u = u<<8 | uint64(s[i])
	}
	return u
}

// The Go types the structured test marshals, each the mirror of a Tack
// type of jsonValueTypes: the same fields in the same order, keyed by
// their json tags as std/json keys a field by its name, an unexported
// field where the Tack type has a private one, a []int where it has a
// list[uint8], which Go would write as base64, and a []uint64 where it
// has a list[unsafe_size_t].
type (
	jsonLeaf struct {
		S      string  `json:"s"`
		I8     int8    `json:"i8"`
		U16    uint16  `json:"u16"`
		F32    float32 `json:"f32"`
		hidden int64
	}
	jsonEmpty struct{}
	jsonNode  struct {
		Name   string                      `json:"name"`
		Ok     bool                        `json:"ok"`
		N      int64                       `json:"n"`
		U      uint64                      `json:"u"`
		F      float64                     `json:"f"`
		Leaf   jsonLeaf                    `json:"leaf"`
		Leaves []jsonLeaf                  `json:"leaves"`
		Grid   [][]int32                   `json:"grid"`
		Tags   map[string]string           `json:"tags"`
		Groups map[string][]jsonLeaf       `json:"groups"`
		Bytes  []int                       `json:"bytes"`
		Sizes  []uint64                    `json:"sizes"`
		Empty  jsonEmpty                   `json:"empty"`
		Nested map[string]map[string]int16 `json:"nested"`
	}
)

// jsonValueTypes declares the Tack types jsonNode and the rest mirror,
// and the helpers the generated program builds values with.
const jsonValueTypes = `package main

import conv from "std/conv"
import fmt from "std/fmt"
import json from "std/json"
import text from "std/text"

type Leaf struct {
    pub s   text.Text
    pub i8  int8
    pub u16 uint16
    pub f32 float32
    hidden  int64
}

type Empty struct {
}

type Node struct {
    pub name   text.Text
    pub ok     bool
    pub n      int64
    pub u      uint64
    pub f      float64
    pub leaf   Leaf
    pub leaves list[Leaf]
    pub grid   list[list[int32]]
    pub tags   map[text.Text]text.Text
    pub groups map[text.Text]list[Leaf]
    pub bytes  list[uint8]
    pub sizes  list[unsafe_size_t]
    pub empty  Empty
    pub nested map[text.Text]map[text.Text]int16
}

// bad is the error for a value the generated code cannot build.
var bad = error{code: 99, msg: "bad value"}

// tx returns a Text holding s.
func tx(s string) (text.Text, error) {
    mut t := text.new(c_allocator)
    mut t.append([]uint8(s))?
    return t, noerror
}

// f64 returns the float64 s holds.
func f64(s string) (float64, error) {
    f := conv.parse_float([]uint8(s), 64)?
    return f, noerror
}

// f32 returns the float32 s holds.
func f32(s string) (float32, error) {
    f := conv.parse_float([]uint8(s), 32)?
    return float32(f), noerror
}

// sz returns u as an unsafe_size_t.
func sz(u uint64) (unsafe_size_t, error) {
    v, ok := unsafe_size_t(u)
    if !ok {
        return 0, bad
    }
    return v, noerror
}

// leaf returns a Leaf of these values, its hidden field set.
func leaf(s string, i8 int8, u16 uint16, f string) (Leaf, error) {
    mut l := make(Leaf, c_allocator)
    l.s = tx(s)?
    l.i8 = i8
    l.u16 = u16
    l.f32 = f32(f)?
    l.hidden = 42
    return l, noerror
}

// line writes v as encode writes it, and a newline.
func line[T Data](v T) error {
    mut out := make(list[uint8], c_allocator)
    json.append_encode(mut out, v, c_allocator)?
    append(mut out, '\n')?
    fmt.write(out)
    return noerror
}
`

// tackInt returns n as a Tack constant expression of a signed type
// bits wide: the smallest value is written as one more than it, minus
// one, since its magnitude does not fit.
func tackInt(n int64, bits int) string {
	if n == int64(-1)<<(bits-1) {
		return fmt.Sprintf("(%d - 1)", n+1)
	}
	return strconv.FormatInt(n, 10)
}

// jsonGen makes random values of jsonNode and the Tack code that builds
// each.
type jsonGen struct {
	r     *rand.Rand
	nodes []jsonNode
	code  strings.Builder
	tmp   int
}

// name returns a new local variable name for the generated code.
func (g *jsonGen) name() string {
	g.tmp++
	return fmt.Sprintf("t%d", g.tmp)
}

// str returns a random string, empty about one time in six.
func (g *jsonGen) str() string {
	if g.r.IntN(6) == 0 {
		return ""
	}
	return randomString(g.r, 6)
}

// int returns a random int64 that fits bits, an edge one about one time
// in three.
func (g *jsonGen) int(bits int) int64 {
	lo := int64(-1) << (bits - 1)
	switch g.r.IntN(6) {
	case 0:
		return lo
	case 1:
		return -(lo + 1)
	case 2:
		return 0
	}
	return int64(g.r.Uint64()) >> (64 - bits)
}

// uint returns a random uint64 that fits bits, an edge one about one
// time in three.
func (g *jsonGen) uint(bits int) uint64 {
	hi := uint64(math.MaxUint64) >> (64 - bits)
	switch g.r.IntN(6) {
	case 0:
		return hi
	case 1:
		return 0
	}
	return g.r.Uint64() >> (64 - bits)
}

// float returns a random finite float64: one of interestingFloats' first
// part, or random bits.
func (g *jsonGen) float() float64 {
	for {
		var f float64
		switch g.r.IntN(4) {
		case 0:
			f = math.Nextafter(1e21, float64(g.r.IntN(3)-1)*math.Inf(1))
		case 1:
			f = math.Nextafter(1e-6, float64(g.r.IntN(3)-1)*math.Inf(1))
		case 2:
			f = math.Pow(10, float64(g.r.IntN(61)-30))
		default:
			f = math.Float64frombits(g.r.Uint64())
		}
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
}

// float32 returns a random finite float32, as float does a float64.
func (g *jsonGen) float32() float32 {
	for {
		var f float32
		switch g.r.IntN(3) {
		case 0:
			f = math.Nextafter32(1e21, float32(g.r.IntN(3)-1)*float32(math.Inf(1)))
		case 1:
			f = math.Nextafter32(1e-6, float32(g.r.IntN(3)-1)*float32(math.Inf(1)))
		default:
			f = math.Float32frombits(g.r.Uint32())
		}
		if !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) {
			return f
		}
	}
}

// count returns how many elements a list or a map gets: none about one
// time in four, and up to max.
func (g *jsonGen) count(max int) int {
	if g.r.IntN(4) == 0 {
		return 0
	}
	return 1 + g.r.IntN(max)
}

// keys returns n distinct random keys, any byte strings, the ones that
// differ only in bytes that are not UTF-8 among them.
func (g *jsonGen) keys(n int) []string {
	seen := map[string]bool{}
	var ks []string
	for len(ks) < n {
		k := g.str()
		if !seen[k] {
			seen[k] = true
			ks = append(ks, k)
		}
	}
	return ks
}

// leaf returns a random jsonLeaf and the Tack call that builds it.
func (g *jsonGen) leaf() (jsonLeaf, string) {
	l := jsonLeaf{S: g.str(), I8: int8(g.int(8)), U16: uint16(g.uint(16)), F32: g.float32(),
		hidden: 42}
	return l, fmt.Sprintf("leaf(%s, %s, %d, %q)?", tackString(l.S), tackInt(int64(l.I8), 8), l.U16,
		strconv.FormatFloat(float64(l.F32), 'g', -1, 32))
}

// node adds a random jsonNode to g.nodes and the Tack function nodeN,
// N its index, that builds the same value.
func (g *jsonGen) node() {
	idx := len(g.nodes)
	var n jsonNode
	c := &g.code
	fmt.Fprintf(c, "\nfunc node%d() (Node, error) {\n    mut x := make(Node, c_allocator)\n", idx)
	n.Name = g.str()
	fmt.Fprintf(c, "    x.name = tx(%s)?\n", tackString(n.Name))
	n.Ok = g.r.IntN(2) == 0
	fmt.Fprintf(c, "    x.ok = %t\n", n.Ok)
	n.N = g.int(64)
	fmt.Fprintf(c, "    x.n = %s\n", tackInt(n.N, 64))
	n.U = g.uint(64)
	fmt.Fprintf(c, "    x.u = %d\n", n.U)
	n.F = g.float()
	fmt.Fprintf(c, "    x.f = f64(%q)?\n", strconv.FormatFloat(n.F, 'g', -1, 64))
	var call string
	n.Leaf, call = g.leaf()
	fmt.Fprintf(c, "    x.leaf = %s\n", call)
	n.Leaves = []jsonLeaf{}
	for range g.count(3) {
		l, call := g.leaf()
		n.Leaves = append(n.Leaves, l)
		fmt.Fprintf(c, "    append(mut x.leaves, %s)?\n", call)
	}
	n.Grid = [][]int32{}
	for range g.count(3) {
		row := []int32{}
		v := g.name()
		fmt.Fprintf(c, "    mut %s := make(list[int32], c_allocator)\n", v)
		for range g.count(4) {
			e := int32(g.int(32))
			row = append(row, e)
			fmt.Fprintf(c, "    append(mut %s, %s)?\n", v, tackInt(int64(e), 32))
		}
		n.Grid = append(n.Grid, row)
		fmt.Fprintf(c, "    append(mut x.grid, %s)?\n", v)
	}
	n.Tags = map[string]string{}
	for _, k := range g.keys(g.count(5)) {
		v := g.str()
		n.Tags[k] = v
		fmt.Fprintf(c, "    put(mut x.tags, tx(%s)?, tx(%s)?)?\n", tackString(k), tackString(v))
	}
	n.Groups = map[string][]jsonLeaf{}
	for _, k := range g.keys(g.count(3)) {
		ls := []jsonLeaf{}
		v := g.name()
		fmt.Fprintf(c, "    mut %s := make(list[Leaf], c_allocator)\n", v)
		for range g.count(2) {
			l, call := g.leaf()
			ls = append(ls, l)
			fmt.Fprintf(c, "    append(mut %s, %s)?\n", v, call)
		}
		n.Groups[k] = ls
		fmt.Fprintf(c, "    put(mut x.groups, tx(%s)?, %s)?\n", tackString(k), v)
	}
	n.Bytes = []int{}
	for range g.count(6) {
		b := int(g.uint(8))
		n.Bytes = append(n.Bytes, b)
		fmt.Fprintf(c, "    append(mut x.bytes, %d)?\n", b)
	}
	n.Sizes = []uint64{}
	for range g.count(3) {
		u := g.uint(64)
		n.Sizes = append(n.Sizes, u)
		fmt.Fprintf(c, "    append(mut x.sizes, sz(%d)?)?\n", u)
	}
	n.Nested = map[string]map[string]int16{}
	for _, k := range g.keys(g.count(3)) {
		m := map[string]int16{}
		v := g.name()
		fmt.Fprintf(c, "    mut %s := make(map[text.Text]int16, c_allocator)\n", v)
		for _, k2 := range g.keys(g.count(3)) {
			e := int16(g.int(16))
			m[k2] = e
			fmt.Fprintf(c, "    put(mut %s, tx(%s)?, %s)?\n", v, tackString(k2), tackInt(int64(e), 16))
		}
		n.Nested[k] = m
		fmt.Fprintf(c, "    put(mut x.nested, tx(%s)?, %s)?\n", tackString(k), v)
	}
	c.WriteString("    return x, noerror\n}\n")
	g.nodes = append(g.nodes, n)
}

// jsonValueProgram returns a Tack program that writes count random
// Nodes, a line each, then a list of the first three and a map of the
// first three under keys that differ only in bytes that are not UTF-8,
// and an empty Node, all as encode writes them, and the lines Go's
// json.Marshal gives for the same values.
func jsonValueProgram(t testing.TB, r *rand.Rand, count int) (string, string) {
	g := &jsonGen{r: r}
	for range count {
		g.node()
	}
	var want strings.Builder
	var run strings.Builder
	run.WriteString("\nfunc run() error {\n")
	for i, n := range g.nodes {
		fmt.Fprintf(&run, "    v%d := node%d()?\n    line(v%d)?\n", i, i, i)
		want.WriteString(marshalLine(t, n) + "\n")
	}
	run.WriteString("    mut l := make(list[Node], c_allocator)\n")
	run.WriteString("    mut m := make(map[text.Text]Node, c_allocator)\n")
	keys := []string{"\xff", "\xfe", "\xef\xbf\xbd"}
	for i := range 3 {
		fmt.Fprintf(&run, "    append(mut l, node%d()?)?\n", i)
		fmt.Fprintf(&run, "    put(mut m, tx(%s)?, node%d()?)?\n", tackString(keys[i]), i)
	}
	run.WriteString("    line(l)?\n    line(m)?\n    e := make(Node, c_allocator)\n    line(e)?\n")
	run.WriteString("    return noerror\n}\n")
	want.WriteString(marshalLine(t, g.nodes[:3]) + "\n")
	want.WriteString(marshalLine(t, map[string]jsonNode{keys[0]: g.nodes[0], keys[1]: g.nodes[1],
		keys[2]: g.nodes[2]}) + "\n")
	empty := jsonNode{Leaves: []jsonLeaf{}, Grid: [][]int32{}, Tags: map[string]string{},
		Groups: map[string][]jsonLeaf{}, Bytes: []int{}, Sizes: []uint64{},
		Nested: map[string]map[string]int16{}}
	want.WriteString(marshalLine(t, empty) + "\n")
	main := "\nfunc main() {\n    if run().code != 0 {\n        print(\"error\\n\")\n        exit(1)\n    }\n}\n"
	return jsonValueTypes + g.code.String() + run.String() + main, want.String()
}

// TestJSONValuesMatchGo checks that std/json's encode writes random
// structs holding every kind of Data, nested in lists and maps, empty
// ones among them, a private field it must leave out and map keys that
// differ only in bytes that are not UTF-8, exactly as Go's json.Marshal
// writes their Go mirrors, built with jsonSanitizers at -O0 and -O1.
func TestJSONValuesMatchGo(t *testing.T) {
	src, want := jsonValueProgram(t, rand.New(rand.NewPCG(23, 24)), 40)
	atO0Too(t, jsonSanitizers, func(t *testing.T, flags []string) {
		bin := buildWithStd(t, src, flags...)
		got, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("run: %v\n%s", err, got)
		}
		gotLines := strings.SplitAfter(string(got), "\n")
		wantLines := strings.SplitAfter(want, "\n")
		for i := range min(len(gotLines), len(wantLines)) {
			if gotLines[i] != wantLines[i] {
				t.Fatalf("line %d: wrote\n%q\nGo's json.Marshal gives\n%q", i+1, gotLines[i],
					wantLines[i])
			}
		}
		if len(gotLines) != len(wantLines) {
			t.Fatalf("wrote %d lines, Go %d", len(gotLines), len(wantLines))
		}
	})
}

// cFunc matches the first line of a function's definition in the
// generated C, capturing its name.
var cFunc = regexp.MustCompile(`(?m)^static [^;=]*?\b(tack_\w+)\((?:[^;{]*)\) \{$`)

// cCalls matches a call in the generated C, capturing the name called.
var cCalls = regexp.MustCompile(`\b(tack_\w+)\(`)

// callGraph returns, for each function the generated C out defines, the
// functions it defines that the body calls.
func callGraph(t *testing.T, out string) map[string][]string {
	t.Helper()
	graph := map[string][]string{}
	for _, m := range cFunc.FindAllStringSubmatchIndex(out, -1) {
		name := out[m[2]:m[3]]
		end := strings.Index(out[m[1]:], "\n}\n")
		if end < 0 {
			t.Fatalf("no end to %s", name)
		}
		graph[name] = nil
		body := out[m[1] : m[1]+end]
		for _, c := range cCalls.FindAllStringSubmatch(body, -1) {
			graph[name] = append(graph[name], c[1])
		}
	}
	for name, calls := range graph {
		graph[name] = slices.DeleteFunc(calls, func(c string) bool {
			_, ok := graph[c]
			return !ok
		})
	}
	return graph
}

// cycleThrough returns a cycle of calls in graph through a function
// whose name holds part, or nil when there is none.
func cycleThrough(graph map[string][]string, part string) []string {
	const (
		unseen = iota
		open
		closed
	)
	state := map[string]int{}
	var path []string
	var visit func(string) []string
	visit = func(f string) []string {
		state[f] = open
		path = append(path, f)
		for _, c := range graph[f] {
			switch state[c] {
			case open:
				i := slices.Index(path, c)
				cycle := slices.Clone(path[i:])
				if slices.ContainsFunc(cycle, func(s string) bool { return strings.Contains(s, part) }) {
					return append(cycle, c)
				}
			case unseen:
				if cycle := visit(c); cycle != nil {
					return cycle
				}
			}
		}
		path = path[:len(path)-1]
		state[f] = closed
		return nil
	}
	names := make([]string, 0, len(graph))
	for name := range graph {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if state[name] == unseen {
			if cycle := visit(name); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// TestJSONDoesNotRecurse checks that no function of std/json in the C
// generated for jsonValueProgram's program, which encodes structs
// nested in structs, lists and maps three deep, and for examples/json,
// whose Writer nests 100000 arrays, takes part in a cycle of calls: each
// instance of encode's walk calls the instances for its type's fields,
// elements and values, other functions, so the calls follow the type
// down to its basic types, and a Writer's nesting is a list, not C's
// stack. It also checks the walk for a Node calls the walk for a Leaf,
// not itself.
//
// The graph holds direct calls only, by name: a call through a function
// value or a pointer, a closure's or an allocator's, is out of its
// scope. That leaves no cycle unseen here. std/json takes no function
// value and stores none, so nothing a caller passes can call back into
// it; the one function value it makes is its own less, which std/sort's
// by calls indirectly and which calls only before and Text.bytes, direct
// calls the graph sees, so no cycle passes through it; and the allocator
// calls go to std's runtime C, which never calls Tack.
func TestJSONDoesNotRecurse(t *testing.T) {
	src, _ := jsonValueProgram(t, rand.New(rand.NewPCG(25, 26)), 3)
	gen, diags := c.New().Generate(compileWithStd(t, src), stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	ex, diags := c.New().Generate(compileExampleDir(t, "json"), stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	for name, out := range map[string]string{"values": string(gen), "examples/json": string(ex)} {
		graph := callGraph(t, out)
		found := 0
		for f := range graph {
			if strings.Contains(f, "_std4_json") {
				found++
			}
		}
		if found < 20 {
			t.Fatalf("%s: found %d functions of std/json in the C, want many more", name, found)
		}
		if cycle := cycleThrough(graph, "_std4_json"); cycle != nil {
			t.Errorf("%s: std/json recurses: %s", name, strings.Join(cycle, " -> "))
		}
	}
	node := instanceBody(t, string(gen), "value_5bmain_2eNode_5d")
	// The body starts after the definition's first line, which names the
	// function itself.
	node = node[strings.Index(node, "{\n"):]
	if strings.Contains(node, "value_5bmain_2eNode_5d(") {
		t.Errorf("the walk for a Node calls itself:\n%s", node)
	}
	if !strings.Contains(node, "value_5bmain_2eLeaf_5d(") {
		t.Errorf("the walk for a Node does not call the walk for a Leaf:\n%s", node)
	}
}

// jsonExampleChecks is how many checks examples/json prints.
const jsonExampleChecks = 46

// TestJSONExampleIsSanitizerClean builds examples/json, its C file and
// std's C at -O2, and with the address, undefined behavior and leak
// sanitizers at -O0 and -O1, every finding fatal, and checks every check
// it prints passed: encode of every kind of Data at its edges, escaping,
// sorted map keys, empty values, NaN and the infinities refused, and
// every allocation refused in turn leaving out as it was; a Writer's
// tokens, each misuse refused leaving it as it was, 100000 arrays deep,
// and every allocation refused in turn; and a region, leaving
// c_allocator holding nothing.
func TestJSONExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "json")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), jsonExampleChecks)
		})
	}
}

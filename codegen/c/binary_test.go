package c_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
)

// binaryProgram reads the file its second argument names and answers
// for it in the mode its first argument names, writing bytes Go's
// encoding/binary is compared with byte for byte. It reads and writes
// its own input and output with shifts, not with std/binary, so a bug
// there cannot hide itself.
//
//   - write: the file is uint64 values, eight bytes each, big-endian.
//     For each value v, it appends to one list, in order,
//     append_uint16_be and append_uint16_le of v's low 16 bits,
//     append_uint32_be and append_uint32_le of its low 32 bits, and
//     append_uint64_be and append_uint64_le of v: 28 bytes.
//   - read: the file is records, each a one-byte length and that many
//     bytes. For each record, and for each of uint16_be, uint16_le,
//     uint32_be, uint32_le, uint64_be and uint64_le in that order, it
//     writes a byte 1 or 0 for the reader's bool and the value it gave,
//     eight bytes big-endian: 54 bytes.
//
// An append that fails prints "append failed", and a record running
// past the file "bad record", each exiting 1.
const binaryProgram = `package main

import binary from "std/binary"
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

// fail prints what failed, and exits.
func fail(what string) {
    print(what)
    print("\n")
    exit(1)
}

// check exits when e is an error.
func check(e error) {
    if e.code != 0 {
        fail("append failed")
    }
}

// u64 returns the eight bytes of s from i, big-endian.
func u64(s []uint8, i int64) uint64 {
    mut v uint64 = 0
    for mut k int64 = 0; k < 8; k++ {
        v = v << 8 | uint64(at(s, i +% k))
    }
    return v
}

// out_u64 appends v to out, eight bytes, big-endian.
func out_u64(mut out list[uint8], v uint64) {
    for mut k uint64 = 0; k < 8; k++ {
        b, ok := uint8(v >> (56 - 8 * k) & 0xff)
        if !ok {
            fail("conversion failed")
        }
        check(append(mut out, b))
    }
}

// out_read appends ok, as a byte 1 or 0, and v to out.
func out_read(mut out list[uint8], v uint64, ok bool) {
    if ok {
        check(append(mut out, 1))
    } else {
        check(append(mut out, 0))
    }
    out_u64(mut out, v)
}

// writes answers for every value of data.
func writes(mut out list[uint8], data []uint8) {
    for mut i int64 = 0; i +% 7 < len(data); i +%= 8 {
        v := u64(data, i)
        v16, ok16 := uint16(v & 0xffff)
        v32, ok32 := uint32(v & 0xffffffff)
        if !ok16 || !ok32 {
            fail("conversion failed")
        }
        check(binary.append_uint16_be(mut out, v16))
        check(binary.append_uint16_le(mut out, v16))
        check(binary.append_uint32_be(mut out, v32))
        check(binary.append_uint32_le(mut out, v32))
        check(binary.append_uint64_be(mut out, v))
        check(binary.append_uint64_le(mut out, v))
    }
}

// reads answers for s.
func reads(mut out list[uint8], s []uint8) {
    a, a_ok := binary.uint16_be(s)
    out_read(mut out, a, a_ok)
    b, b_ok := binary.uint16_le(s)
    out_read(mut out, b, b_ok)
    c, c_ok := binary.uint32_be(s)
    out_read(mut out, c, c_ok)
    d, d_ok := binary.uint32_le(s)
    out_read(mut out, d, d_ok)
    e, e_ok := binary.uint64_be(s)
    out_read(mut out, e, e_ok)
    f, f_ok := binary.uint64_le(s)
    out_read(mut out, f, f_ok)
}

// records answers for every record of data.
func records(mut out list[uint8], data []uint8) {
    mut i int64 = 0
    for i >= 0 && i < len(data) {
        start := i +% 1
        end := start +% int64(at(data, i))
        if start < 0 || end > len(data) || start > end {
            fail("bad record")
            return
        }
        reads(mut out, data[start:end])
        i = end
    }
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 3 {
        print("usage: binary write|read file\n")
        exit(2)
    }
    data, e2 := os.read_file(args[2].bytes(), c_allocator)
    if e2.code != 0 {
        print("cannot read the file\n")
        exit(2)
    }
    mut out := make(list[uint8], c_allocator)
    if at(args[1].bytes(), 0) == 'w' {
        writes(mut out, data)
    } else {
        records(mut out, data)
    }
    fmt.write(out)
}
`

// binaryWriteWant returns what binaryProgram writes in write mode for v,
// from Go's encoding/binary.
func binaryWriteWant(v uint64) []byte {
	var out []byte
	out = binary.BigEndian.AppendUint16(out, uint16(v))
	out = binary.LittleEndian.AppendUint16(out, uint16(v))
	out = binary.BigEndian.AppendUint32(out, uint32(v))
	out = binary.LittleEndian.AppendUint32(out, uint32(v))
	out = binary.BigEndian.AppendUint64(out, v)
	return binary.LittleEndian.AppendUint64(out, v)
}

// binaryReadWant returns what binaryProgram writes in read mode for s,
// from Go's encoding/binary: for a view too short for a reader, false
// and 0, where Go's reader would panic.
func binaryReadWant(s []byte) []byte {
	var out []byte
	one := func(n int, read func([]byte) uint64) {
		if len(s) < n {
			out = append(out, 0)
			out = binary.BigEndian.AppendUint64(out, 0)
			return
		}
		out = append(out, 1)
		out = binary.BigEndian.AppendUint64(out, read(s))
	}
	one(2, func(b []byte) uint64 { return uint64(binary.BigEndian.Uint16(b)) })
	one(2, func(b []byte) uint64 { return uint64(binary.LittleEndian.Uint16(b)) })
	one(4, func(b []byte) uint64 { return uint64(binary.BigEndian.Uint32(b)) })
	one(4, func(b []byte) uint64 { return uint64(binary.LittleEndian.Uint32(b)) })
	one(8, binary.BigEndian.Uint64)
	one(8, binary.LittleEndian.Uint64)
	return out
}

// binaryValues returns the values TestBinaryWriteMatchesGo writes, with
// random ones from r: 0 and the largest value of each width, and one
// past it; each power of 2 and each one less; every byte value at every
// one of the eight places, alone and filling the value; and random
// values, of all 64 bits and of 16 and 32.
func binaryValues(r *rand.Rand) []uint64 {
	vs := make([]uint64, 0, 8+2*64+256*9+3*5000)
	vs = append(
		vs,
		0,
		math.MaxUint16,
		math.MaxUint16+1,
		math.MaxUint32,
		math.MaxUint32+1,
		math.MaxUint64,
		0x0102030405060708,
		0xfedcba9876543210,
	)
	for k := range 64 {
		vs = append(vs, 1<<k, 1<<k-1)
	}
	for c := range uint64(256) {
		vs = append(vs, c*0x0101010101010101)
		for p := range 8 {
			vs = append(vs, c<<(8*p))
		}
	}
	for range 5000 {
		vs = append(vs, r.Uint64(), r.Uint64()&math.MaxUint16, r.Uint64()&math.MaxUint32)
	}
	return vs
}

// binaryViews returns the views TestBinaryReadMatchesGo reads, with
// random bytes from r: every length from 0 to 17 of 0x00, of 0xff and
// of random bytes, so every reader meets every view one byte short of
// it and shorter; every byte value at every one of the first eight
// places of an eight-byte view of zeros; and random views up to 32
// bytes.
func binaryViews(r *rand.Rand) [][]byte {
	in := make([][]byte, 0, 3*18+256*8+5000)
	for n := range 18 {
		in = append(
			in,
			bytes.Repeat([]byte{0x00}, n),
			bytes.Repeat([]byte{0xff}, n),
			randomBytes(r, n),
		)
	}
	for c := range 256 {
		for p := range 8 {
			b := make([]byte, 8)
			b[p] = byte(c)
			in = append(in, b)
		}
	}
	for range 5000 {
		in = append(in, randomBytes(r, r.IntN(33)))
	}
	return in
}

// checkBinaryWrite runs bin, a build of binaryProgram, in write mode on
// vs, and checks that what it wrote for each is Go's.
func checkBinaryWrite(t *testing.T, bin string, vs []uint64) {
	t.Helper()
	var data []byte
	for _, v := range vs {
		data = binary.BigEndian.AppendUint64(data, v)
	}
	got := runOnFile(t, bin, data, "write")
	for _, v := range vs {
		want := binaryWriteWant(v)
		if !bytes.HasPrefix(got, want) {
			t.Fatalf(
				"writing %#x: wrote\n% x\nGo's is\n% x",
				v,
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

// checkBinaryRead runs bin, a build of binaryProgram, in read mode on
// views, and checks that what it wrote for each is Go's.
func checkBinaryRead(t *testing.T, bin string, views [][]byte) {
	t.Helper()
	var data []byte
	for _, s := range views {
		if len(s) > math.MaxUint8 {
			t.Fatalf("a view of %d bytes is too long for a record", len(s))
		}
		data = append(data, byte(len(s)))
		data = append(data, s...)
	}
	got := runOnFile(t, bin, data, "read")
	for _, s := range views {
		want := binaryReadWant(s)
		if !bytes.HasPrefix(got, want) {
			t.Fatalf(
				"reading % x: wrote\n% x\nGo's is\n% x",
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

// binarySanitizers are the flags every build of binaryProgram takes:
// the address and undefined behavior sanitizers, every finding fatal, so
// std/binary reads no view past its end and shifts nothing C counts as
// undefined, on every input the tests give it.
var binarySanitizers = utf8Sanitizers

// TestBinaryWriteMatchesGo checks that std/binary's append_uintN_be and
// append_uintN_le, for N of 16, 32 and 64, append for every value of
// binaryValues exactly what Go's BigEndian and LittleEndian AppendUintN
// do, onto a list already holding everything written before.
func TestBinaryWriteMatchesGo(t *testing.T) {
	vs := binaryValues(rand.New(rand.NewPCG(19, 20)))
	atO0Too(t, binarySanitizers, func(t *testing.T, flags []string) {
		checkBinaryWrite(t, buildWithStd(t, binaryProgram, flags...), vs)
	})
	t.Logf("%d values", len(vs))
}

// TestBinaryReadMatchesGo checks that std/binary's uintN_be and
// uintN_le, for N of 16, 32 and 64, give for every view of binaryViews
// what Go's BigEndian and LittleEndian UintN give, and true, or 0 and
// false for a view shorter than N/8 bytes, every length from 0 to N/8 -
// 1 among them, where Go's panic.
func TestBinaryReadMatchesGo(t *testing.T) {
	views := binaryViews(rand.New(rand.NewPCG(21, 22)))
	atO0Too(t, binarySanitizers, func(t *testing.T, flags []string) {
		checkBinaryRead(t, buildWithStd(t, binaryProgram, flags...), views)
	})
	t.Logf("%d views", len(views))
}

// FuzzBinaryMatchesGo checks, for each byte string the fuzzer makes,
// that std/binary's readers give Go's values for it and for every
// shorter view of its start, and that its appends write Go's bytes for
// every value its eight-byte pieces hold.
func FuzzBinaryMatchesGo(f *testing.F) {
	for _, s := range []string{
		"", "\x01", "\x01\x02\x03", "\xff\xff\xff\xff\xff\xff\xff",
		"\x01\x02\x03\x04\x05\x06\x07\x08", "\x80\x00\x00\x00\x00\x00\x00\x00\xff",
	} {
		f.Add([]byte(s))
	}
	bin := buildWithStd(f, binaryProgram, binarySanitizers...)
	f.Fuzz(func(t *testing.T, s []byte) {
		if len(s) > math.MaxUint8 {
			t.Skip("too long for a record")
		}
		views := make([][]byte, 0, min(len(s), 9)+1)
		for n := 0; n <= min(len(s), 8); n++ {
			views = append(views, s[:n])
		}
		checkBinaryRead(t, bin, append(views, s))
		var vs []uint64
		for i := 0; i+8 <= len(s); i += 8 {
			vs = append(vs, binary.BigEndian.Uint64(s[i:]))
		}
		checkBinaryWrite(t, bin, vs)
	})
}

// TestBinaryExampleIsSanitizerClean builds examples/binary and its C
// file, live.c, with clang's warnings as errors and binarySanitizers,
// and checks it writes exactly its output, every check ok, a failed
// append leaving its list as it was and every buffer freed among them.
func TestBinaryExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "binary", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "binary", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/binary/output expects a check to fail")
	}
	atO0Too(t, binarySanitizers, func(t *testing.T, flags []string) {
		if out := runExample(t, buildWithStdAndC(t, src, csrc, flags...)); out != want {
			t.Errorf("binary example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

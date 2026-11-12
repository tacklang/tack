package c_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sha256Program reads the file its first argument names, a sequence of
// records, each a message and the pieces to write of it: a four-byte
// big-endian length and that many bytes, then a four-byte big-endian
// count of pieces and that many four-byte big-endian piece lengths,
// which add up to at most the message's length. For each record it
// writes the pieces, in order from the message's start, to a Digest
// from new, writing the digest's sum, 32 bytes, after every piece; then
// the sum of a zero Digest, declared without new, and that of a copy of
// it after every piece written to the copy; and then the message's
// sha256.sum, 32 bytes more. A record it cannot read,
// its pieces running past the message, prints "bad record" and exits 1.
const sha256Program = `package main

import fmt from "std/fmt"
import os from "std/os"
import sha256 from "std/sha256"

// at returns byte i of s, or 0 when s has no byte i.
func at(s []uint8, i int64) uint8 {
    c, ok := s[i]
    if !ok {
        return 0
    }
    return c
}

// u32 returns the four bytes of s from i, big-endian. i +% 1 to i +% 3
// wrap only for an i within 3 of the largest int64, far past any file's
// length, where at gives 0 for the wrapped index as for any index past
// s.
func u32(s []uint8, i int64) int64 {
    return int64(at(s, i)) << 24 | int64(at(s, i +% 1)) << 16 |
        int64(at(s, i +% 2)) << 8 | int64(at(s, i +% 3))
}

// bad reports a record that cannot be read, and exits.
func bad() {
    print("bad record\n")
    exit(1)
}

// emit appends d to out, and exits when out cannot grow.
func emit(mut out list[uint8], d [32]uint8) {
    e := append(mut out, d...)
    if e.code != 0 {
        print("out of memory\n")
        exit(1)
    }
}

// piece writes msg[pos:pos + n] to d and returns pos + n, or prints
// "bad record" and exits when the piece is not within msg.
func piece(mut d sha256.Digest, msg []uint8, pos int64, n int64) int64 {
    // pos is at most len(msg) and n below 2^32, so pos +% n does not
    // wrap; the check after it would reject a wrapped stop anyway, being
    // below pos.
    stop := pos +% n
    if pos < 0 || stop > len(msg) || pos > stop {
        bad()
        return 0
    }
    mut d.write(msg[pos:stop])
    return stop
}

// pieces writes, to out, d's sum after each piece of msg written to it,
// whose count and lengths are in data from i, and returns where the
// lengths end.
func pieces(mut out list[uint8], mut d sha256.Digest, msg []uint8, data []uint8, i int64) int64 {
    count := u32(data, i)
    // i is at most len(data), so i +% 4 and each j +% 4 below wrap only
    // past a length far beyond any file's; a j past len(data) is caught
    // by the check after the loop.
    mut j := i +% 4
    mut pos int64 = 0
    for range count {
        pos = piece(mut d, msg, pos, u32(data, j))
        j +%= 4
        emit(mut out, d.sum())
    }
    if j > len(data) {
        bad()
    }
    return j
}

// record writes, to out, the sum after each piece of msg, whose count
// and lengths are in data from i, to a Digest from new; then the sum of
// a zero Digest, declared without new and copied before its first
// write, and its sum after each piece; then msg's sum. It returns where
// the next record starts.
func record(mut out list[uint8], msg []uint8, data []uint8, i int64) int64 {
    mut d := sha256.new()
    j := pieces(mut out, mut d, msg, data, i)
    mut zero sha256.Digest
    emit(mut out, zero.sum())
    mut z := zero
    if pieces(mut out, mut z, msg, data, i) != j {
        bad()
    }
    emit(mut out, sha256.sum(msg))
    return j
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 2 {
        print("usage: sha256 file\n")
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
        // i is below len(data), and u32 below 2^32, so neither +% wraps;
        // the check after them rejects a record running past the file.
        start := i +% 4
        end := start +% u32(data, i)
        if start < 0 || end > len(data) || start > end {
            bad()
            return
        }
        i = record(mut out, data[start:end], data, end)
    }
    fmt.write(out)
}
`

// sha256Case is one record of sha256Program's input: a message and the
// lengths of the pieces a Digest is written, from the message's start,
// which add up to at most its length; no pieces checks sum alone.
type sha256Case struct {
	msg    []byte
	pieces []int
}

// sha256Records returns cases as sha256Program reads them.
func sha256Records(cases []sha256Case) []byte {
	var data []byte
	for _, c := range cases {
		data = binary.BigEndian.AppendUint32(data, uint32(len(c.msg)))
		data = append(data, c.msg...)
		data = binary.BigEndian.AppendUint32(data, uint32(len(c.pieces)))
		for _, n := range c.pieces {
			data = binary.BigEndian.AppendUint32(data, uint32(n))
		}
	}
	return data
}

// sha256Want returns what sha256Program writes for c, from Go's
// crypto/sha256: the sum after every piece, the empty message's sum,
// the sum after every piece again, for the zero Digest, and then the
// message's.
func sha256Want(c sha256Case) []byte {
	streamed := func(out []byte) []byte {
		h := sha256.New()
		pos := 0
		for _, n := range c.pieces {
			h.Write(c.msg[pos : pos+n])
			pos += n
			out = h.Sum(out)
		}
		return out
	}
	out := streamed(nil)
	empty := sha256.Sum256(nil)
	out = streamed(append(out, empty[:]...))
	sum := sha256.Sum256(c.msg)
	return append(out, sum[:]...)
}

// checkSHA256 runs bin, a build of sha256Program, on cases, and checks
// that every digest it wrote is Go's.
func checkSHA256(t *testing.T, bin string, cases []sha256Case) {
	t.Helper()
	got := runOnFile(t, bin, sha256Records(cases))
	for _, c := range cases {
		want := sha256Want(c)
		if !bytes.HasPrefix(got, want) {
			t.Fatalf(
				"message of %d bytes (%.16x...) in pieces %v: wrote\n%x\nGo's is\n%x",
				len(c.msg),
				c.msg,
				c.pieces[:min(len(c.pieces), 8)],
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

// randomBytes returns n bytes from r.
func randomBytes(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// randomPieces returns lengths from r adding up to n, mostly short, some
// empty, some longer than a block, so writes start and end at every
// offset of a block.
func randomPieces(r *rand.Rand, n int) []int {
	var pieces []int
	for n > 0 {
		var k int
		switch r.IntN(4) {
		case 0:
			k = 0
		case 1:
			k = 1 + r.IntN(64)
		case 2:
			k = 1 + r.IntN(300)
		default:
			k = 1 + r.IntN(70000)
		}
		k = min(k, n)
		pieces = append(pieces, k)
		n -= k
	}
	return pieces
}

// sha256Cases returns the cases TestSHA256MatchesGo checks, with random
// bytes from r: the empty message, in no pieces and in one empty piece;
// every length from 1 to 200, each 0x00, 0xff, 'a' and random bytes, so
// every padding boundary, 55, 56, 63, 64, 119 and 120, is met with every
// byte value's bits, in one piece and, for random bytes, cut in two at
// every point and in three at points around the block's edges; random
// messages up to 5000 bytes in random pieces; and messages of 1, 3 and 8
// MiB, one of zeros, in one piece and in random ones.
func sha256Cases(r *rand.Rand) []sha256Case {
	cases := []sha256Case{{msg: []byte{}}, {msg: []byte{}, pieces: []int{0}}}
	for n := 1; n <= 200; n++ {
		for _, fill := range []byte{0x00, 0xff, 'a'} {
			cases = append(cases, sha256Case{msg: bytes.Repeat([]byte{fill}, n), pieces: []int{n}})
		}
		msg := randomBytes(r, n)
		cases = append(cases, sha256Case{msg: msg})
		for k := 0; k <= n; k++ {
			cases = append(cases, sha256Case{msg: msg, pieces: []int{k, n - k}})
		}
		for _, a := range []int{1, 55, 56, 63, 64, 65} {
			for _, b := range []int{0, 1, 8, 63, 64} {
				if a+b <= n {
					cases = append(cases, sha256Case{msg: msg, pieces: []int{a, b, n - a - b}})
				}
			}
		}
		ones := make([]int, n)
		for i := range ones {
			ones[i] = 1
		}
		cases = append(cases, sha256Case{msg: msg, pieces: ones})
	}
	for range 2000 {
		msg := randomBytes(r, r.IntN(5000))
		cases = append(cases, sha256Case{msg: msg, pieces: randomPieces(r, len(msg))})
	}
	for _, n := range []int{1 << 20, 3<<20 + 17, 8 << 20} {
		msg := randomBytes(r, n)
		cases = append(cases, sha256Case{msg: msg, pieces: []int{n}},
			sha256Case{msg: msg, pieces: randomPieces(r, n)})
	}
	zeros := make([]byte, 8<<20)
	cases = append(cases, sha256Case{msg: zeros, pieces: []int{len(zeros)}})
	return cases
}

// sha256Sanitizers are the flags every build of sha256Program takes: the
// address and undefined behavior sanitizers, every finding fatal, so
// std/sha256 reads and writes no array or view past its end, and
// overflows nothing C counts as undefined, on every input the tests
// give it.
var sha256Sanitizers = utf8Sanitizers

// TestSHA256MatchesGo checks that std/sha256's sum, and a Digest's sum
// after every write of every case of sha256Cases, are byte for byte Go's
// crypto/sha256, built with sha256Sanitizers at -O0 and -O1.
func TestSHA256MatchesGo(t *testing.T) {
	cases := sha256Cases(rand.New(rand.NewPCG(15, 16)))
	atO0Too(t, sha256Sanitizers, func(t *testing.T, flags []string) {
		checkSHA256(t, buildWithStd(t, sha256Program, flags...), cases)
	})
	t.Logf("%d cases", len(cases))
}

// FuzzSHA256MatchesGo checks, for each message the fuzzer makes, cut
// into pieces where cuts says, a byte of it each, that std/sha256 gives
// Go's digest after every piece and for the whole message.
func FuzzSHA256MatchesGo(f *testing.F) {
	for _, s := range []string{"", "a", "abc", strings.Repeat("a", 55), strings.Repeat("b", 56),
		strings.Repeat("c", 64), strings.Repeat("d", 119), strings.Repeat("e", 200)} {
		f.Add([]byte(s), []byte{1, 0, 63, 64, 255})
	}
	bin := buildWithStd(f, sha256Program, sha256Sanitizers...)
	f.Fuzz(func(t *testing.T, msg, cuts []byte) {
		var pieces []int
		left := len(msg)
		for _, c := range cuts {
			n := min(int(c), left)
			pieces = append(pieces, n)
			left -= n
		}
		if left > 0 {
			pieces = append(pieces, left)
		}
		checkSHA256(t, bin, []sha256Case{{msg: msg, pieces: pieces}})
	})
}

// sha256Digest matches a digest examples/sha256 expects: a global
// holding 64 hex digits.
var sha256Digest = regexp.MustCompile(`(?m)^var (\w+) = "([0-9a-f]{64})"$`)

// sha256Message matches a message examples/sha256 hashes.
var sha256Message = regexp.MustCompile(`(?m)^var (msg\d+) = "([a-z]+)"$`)

// TestSHA256ExampleDigestsMatchGo checks that every digest examples/sha256
// expects is Go's crypto/sha256 of its message, so the example's
// expectations are Go's and FIPS 180-4's, not copied from std/sha256's
// output.
func TestSHA256ExampleDigestsMatchGo(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "sha256", "main.tck"))
	msgs := map[string]string{}
	for _, m := range sha256Message.FindAllStringSubmatch(src, -1) {
		msgs[m[1]] = m[2]
	}
	want := map[string]string{
		"empty":     "",
		"abc":       "abc",
		"sum448":    msgs["msg448"],
		"sum896":    msgs["msg896"],
		"million_a": strings.Repeat("a", 1000000),
	}
	if len(msgs["msg448"]) != 56 || len(msgs["msg896"]) != 112 {
		t.Fatalf("the 448- and 896-bit messages are %d and %d bytes", len(msgs["msg448"]), len(msgs["msg896"]))
	}
	n := 0
	for _, m := range sha256Digest.FindAllStringSubmatch(src, -1) {
		msg, ok := want[m[1]]
		if !ok {
			t.Errorf("digest %s has no known message", m[1])
			continue
		}
		n++
		sum := sha256.Sum256([]byte(msg))
		if hex.EncodeToString(sum[:]) != m[2] {
			t.Errorf("%s is %s, Go's is %x", m[1], m[2], sum)
		}
	}
	if n != len(want) {
		t.Errorf("found %d digests, want %d", n, len(want))
	}
}

// TestSHA256ExampleIsSanitizerClean builds examples/sha256 with clang's
// warnings as errors and sha256Sanitizers, and checks it writes exactly
// its output, every check ok.
func TestSHA256ExampleIsSanitizerClean(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "sha256", "main.tck"))
	want := readFile(t, filepath.Join("..", "..", "examples", "sha256", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/sha256/output expects a check to fail")
	}
	atO0Too(t, sha256Sanitizers, func(t *testing.T, flags []string) {
		if out := runExample(t, buildWithStd(t, src, flags...)); out != want {
			t.Errorf("sha256 example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

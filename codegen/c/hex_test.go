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

// hexProgram reads the file its first argument names, a sequence of
// records, each a four-byte big-endian length and that many bytes, and
// appends each record's bytes in hex, with std/hex's append_encode, to
// one list, which it writes out at the end, so every record is encoded
// after the ones before it, onto a list already holding their digits. A
// record running past the file prints "bad record", and an
// append_encode that fails prints "append_encode failed", each exiting
// 1.
const hexProgram = `package main

import fmt from "std/fmt"
import hex from "std/hex"
import os from "std/os"

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

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 2 {
        print("usage: hex file\n")
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
            print("bad record\n")
            exit(1)
        }
        e3 := hex.append_encode(mut out, data[start:end])
        if e3.code != 0 {
            print("append_encode failed\n")
            exit(1)
        }
        i = end
    }
    fmt.write(out)
}
`

// hexRecords returns inputs as hexProgram reads them.
func hexRecords(inputs [][]byte) []byte {
	var data []byte
	for _, in := range inputs {
		data = binary.BigEndian.AppendUint32(data, uint32(len(in)))
		data = append(data, in...)
	}
	return data
}

// checkHex runs bin, a build of hexProgram, on inputs, and checks that
// what it wrote for each is Go's hex.AppendEncode of it, onto the
// digits of the inputs before it.
func checkHex(t *testing.T, bin string, inputs [][]byte) {
	t.Helper()
	got := runOnFile(t, bin, hexRecords(inputs))
	for _, in := range inputs {
		want := hex.AppendEncode(nil, in)
		if !bytes.HasPrefix(got, want) {
			t.Fatalf(
				"encoding %d bytes (% .16x...): wrote\n%.80s\nGo's is\n%.80s",
				len(in),
				in,
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

// hexInputs returns the byte strings TestHexMatchesGo encodes, with
// random bytes from r: the empty view, between others and twice in a
// row; every byte value alone, and all 256 in order and in reverse;
// every length from 1 to 300 of 0x00, of 0xff and of random bytes;
// random views up to 5000 bytes; a SHA-256 digest; and views of 1 and
// 3 MiB and a few bytes more.
func hexInputs(r *rand.Rand) [][]byte {
	in := [][]byte{{}, {0}, {}, {}}
	all := make([]byte, 256)
	for i := range all {
		in = append(in, []byte{byte(i)})
		all[i] = byte(i)
	}
	rev := make([]byte, 256)
	for i := range rev {
		rev[i] = byte(255 - i)
	}
	in = append(in, all, rev)
	for n := 1; n <= 300; n++ {
		in = append(
			in,
			bytes.Repeat([]byte{0x00}, n),
			bytes.Repeat([]byte{0xff}, n),
			randomBytes(r, n),
		)
	}
	for range 2000 {
		in = append(in, randomBytes(r, r.IntN(5000)))
	}
	sum := sha256.Sum256([]byte("abc"))
	in = append(in, sum[:], randomBytes(r, 1<<20), randomBytes(r, 3<<20+5))
	return in
}

// TestHexMatchesGo checks that std/hex's append_encode appends, for
// every byte string of hexInputs, exactly what Go's hex.AppendEncode
// does, onto a list already holding the digits of every input before
// it, built with utf8Sanitizers at -O0 and -O1.
func TestHexMatchesGo(t *testing.T) {
	inputs := hexInputs(rand.New(rand.NewPCG(17, 18)))
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		checkHex(t, buildWithStd(t, hexProgram, flags...), inputs)
	})
	t.Logf("%d inputs", len(inputs))
}

// FuzzHexMatchesGo checks, for each byte string the fuzzer makes, that
// std/hex's append_encode appends Go's hex.AppendEncode of it, after an
// empty view and before it again.
func FuzzHexMatchesGo(f *testing.F) {
	for _, s := range []string{"", "\x00", "\xff", "abc", "\x0f\xf0", "\xde\xad\xbe\xef"} {
		f.Add([]byte(s))
	}
	bin := buildWithStd(f, hexProgram, utf8Sanitizers...)
	f.Fuzz(func(t *testing.T, s []byte) {
		checkHex(t, bin, [][]byte{{}, s, s})
	})
}

// hexExampleString matches a global examples/hex expects append_encode
// to write: a name and a string of hex digits.
var hexExampleString = regexp.MustCompile(`(?m)^var (\w+) = "([0-9a-f]*)"$`)

// TestHexExampleStringsMatchGo checks that the strings examples/hex
// expects for every byte value and for the SHA-256 of "abc" are what Go's
// hex.EncodeToString writes for them, so the example's expectations are
// Go's, not copied from std/hex's output.
func TestHexExampleStringsMatchGo(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "hex", "main.tck"))
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	sum := sha256.Sum256([]byte("abc"))
	want := map[string]string{
		"every_byte": hex.EncodeToString(all),
		"abc_sum":    hex.EncodeToString(sum[:]),
	}
	n := 0
	for _, m := range hexExampleString.FindAllStringSubmatch(src, -1) {
		w, ok := want[m[1]]
		if !ok {
			continue
		}
		n++
		if m[2] != w {
			t.Errorf("%s is %s, Go's is %s", m[1], m[2], w)
		}
	}
	if n != len(want) {
		t.Errorf("found %d of the %d strings", n, len(want))
	}
}

// TestHexExampleIsSanitizerClean builds examples/hex and its C file,
// live.c, with clang's warnings as errors and utf8Sanitizers, and
// checks it writes exactly its output, every check ok, a failed
// append_encode leaving its list as it was and every buffer freed among
// them.
func TestHexExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "hex", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "hex", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/hex/output expects a check to fail")
	}
	atO0Too(t, utf8Sanitizers, func(t *testing.T, flags []string) {
		if out := runExample(t, buildWithStdAndC(t, src, csrc, flags...)); out != want {
			t.Errorf("hex example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

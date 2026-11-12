package c_test

import (
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bigProgram reads the file its first argument names, a line per
// operation, its words separated by single spaces, and writes a line for
// each: the operation's result, or "error" and the code of the error it
// returned. Integers are written in base 16, signed, as std/big's parse
// and format take them. The operations:
//
//   - plus, sub, mul x y: the sum, the difference or the product.
//   - cmp x y: cmp's -1, 0 or 1.
//   - divmod x y: the quotient and the remainder, separated by a space.
//   - neg, abs x: -x or |x|.
//   - sign, zero x: sign's -1, 0 or 1, or is_zero's true or false.
//   - shl, shr x n: x shifted by n, a decimal int64.
//   - i64, u64 x: to_int64 or to_uint64 of x in decimal, or "no".
//   - fi64, fu64 n: from_int64 or from_uint64 of n, in decimal.
//   - parse base s: parse of the bytes s, written in hex, two digits a
//     byte, in base, a decimal int64.
//   - format base x: format of x in base, appended to "=", so a failed
//     format shows the list was left as it was.
//   - rt base x: whether parse gives x back from format of x in base,
//     true or false.
//
// A line it cannot read prints "bad line" and exits 1; out of memory
// that is not an operation's result prints "out of memory" and exits 1.
const bigProgram = `package main

import big from "std/big"
import bytes from "std/bytes"
import conv from "std/conv"
import fmt from "std/fmt"
import os from "std/os"

// tok returns the k-th of the space-separated words of l, from 0, or an
// empty view when l has fewer. n counts words, at most len(l), so its
// +% never wraps.
func tok(l []uint8, k int64) []uint8 {
    mut start int64 = 0
    mut n int64 = 0
    for i, c := range l {
        if c == ' ' {
            if n == k && start >= 0 && start <= i {
                return l[start:i]
            }
            n +%= 1
            start = i + 1
        }
    }
    if n == k && start >= 0 && start <= len(l) {
        return l[start:]
    }
    return l[0:0]
}

// is reports whether t is s.
func is(t []uint8, s string) bool {
    return bytes.equal(t, []uint8(s))
}

// oom exits when e is an error, which only out of memory is here.
func oom(e error) {
    if e.code != 0 {
        print("out of memory\n")
        exit(1)
    }
}

// num returns the Int t holds in base 16, and exits when it holds none.
func num(t []uint8) big.Int {
    x, err := big.parse(t, 16, c_allocator)
    if err.code != 0 {
        print("bad line\n")
        exit(1)
    }
    return x
}

// count returns the int64 t holds in decimal, and exits when it holds
// none.
func count(t []uint8) int64 {
    n, err := conv.parse_int(t, 10, 64)
    if err.code != 0 {
        print("bad line\n")
        exit(1)
    }
    return n
}

// unhex appends to b the bytes t holds in hex, two digits each.
func unhex(mut b list[uint8], t []uint8) {
    mut hi uint8 = 0
    for i, c := range t {
        mut v uint8 = 0
        if c >= '0' && c <= '9' {
            v = c - '0'
        } else if c >= 'a' && c <= 'f' {
            v = c - 'a' + 10
        } else {
            print("bad line\n")
            exit(1)
        }
        if i % 2 == 0 {
            hi = v
        } else {
            oom(append(mut b, hi << 4 | v))
        }
    }
}

// text appends the bytes of s.
func text(mut out list[uint8], s string) {
    oom(append(mut out, []uint8(s)...))
}

// emit writes x in base 16.
func emit(mut out list[uint8], x big.Int) {
    oom(big.format(mut out, x, 16))
}

// emit_error writes err's code after "error".
func emit_error(mut out list[uint8], err error) {
    text(mut out, "error ")
    oom(bytes.append_int(mut out, err.code))
}

// emit_bool writes b as true or false.
func emit_bool(mut out list[uint8], b bool) {
    if b {
        text(mut out, "true")
    } else {
        text(mut out, "false")
    }
}

// binary writes the result of op on the Ints of words 1 and 2 of l.
func binary(mut out list[uint8], l []uint8) {
    x := num(tok(l, 1))
    y := num(tok(l, 2))
    if is(tok(l, 0), "cmp") {
        oom(bytes.append_int(mut out, big.cmp(x, y)))
    } else if is(tok(l, 0), "divmod") {
        q, r, err := big.div_mod(x, y, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
            return
        }
        emit(mut out, q)
        text(mut out, " ")
        emit(mut out, r)
    } else if is(tok(l, 0), "plus") {
        z, err := big.plus(x, y, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "sub") {
        z, err := big.sub(x, y, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "mul") {
        z, err := big.mul(x, y, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else {
        print("bad line\n")
        exit(1)
    }
}

// unary writes the result of op on the Int of word 1 of l.
func unary(mut out list[uint8], l []uint8) {
    x := num(tok(l, 1))
    if is(tok(l, 0), "neg") {
        z, err := big.neg(x, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "abs") {
        z, err := big.abs(x, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "sign") {
        oom(bytes.append_int(mut out, big.sign(x)))
    } else if is(tok(l, 0), "zero") {
        emit_bool(mut out, big.is_zero(x))
    } else if is(tok(l, 0), "i64") {
        v, ok := big.to_int64(x)
        if ok {
            oom(bytes.append_int(mut out, v))
        } else {
            text(mut out, "no")
        }
    } else if is(tok(l, 0), "u64") {
        v, ok := big.to_uint64(x)
        if ok {
            oom(bytes.append_uint(mut out, v))
        } else {
            text(mut out, "no")
        }
    } else {
        print("bad line\n")
        exit(1)
    }
}

// run writes the result of the operation l names.
func run(mut out list[uint8], l []uint8) {
    if is(tok(l, 0), "shl") || is(tok(l, 0), "shr") {
        x := num(tok(l, 1))
        n := count(tok(l, 2))
        if is(tok(l, 0), "shl") {
            z, err := big.shl(x, n, c_allocator)
            if err.code != 0 {
                emit_error(mut out, err)
            } else {
                emit(mut out, z)
            }
        } else {
            z, err := big.shr(x, n, c_allocator)
            if err.code != 0 {
                emit_error(mut out, err)
            } else {
                emit(mut out, z)
            }
        }
    } else if is(tok(l, 0), "fi64") {
        z, err := big.from_int64(count(tok(l, 1)), c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "fu64") {
        n, e := conv.parse_uint(tok(l, 1), 10, 64)
        if e.code != 0 {
            print("bad line\n")
            exit(1)
        }
        z, err := big.from_uint64(n, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "parse") {
        mut s := make(list[uint8], c_allocator)
        unhex(mut s, tok(l, 2))
        z, err := big.parse(s, count(tok(l, 1)), c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
        } else {
            emit(mut out, z)
        }
    } else if is(tok(l, 0), "format") {
        x := num(tok(l, 2))
        text(mut out, "=")
        err := big.format(mut out, x, count(tok(l, 1)))
        if err.code != 0 {
            emit_error(mut out, err)
        }
    } else if is(tok(l, 0), "rt") {
        base := count(tok(l, 1))
        x := num(tok(l, 2))
        mut t := make(list[uint8], c_allocator)
        oom(big.format(mut t, x, base))
        y, err := big.parse(t, base, c_allocator)
        if err.code != 0 {
            emit_error(mut out, err)
            return
        }
        emit_bool(mut out, big.cmp(x, y) == 0)
    } else if is(tok(l, 0), "neg") || is(tok(l, 0), "abs") || is(tok(l, 0), "sign") ||
        is(tok(l, 0), "zero") || is(tok(l, 0), "i64") || is(tok(l, 0), "u64") {
        unary(mut out, l)
    } else {
        binary(mut out, l)
    }
    text(mut out, "\n")
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 || len(args) != 2 {
        print("usage: big file\n")
        exit(2)
    }
    data, e2 := os.read_file(args[1].bytes(), c_allocator)
    if e2.code != 0 {
        print("cannot read the file\n")
        exit(2)
    }
    mut out := make(list[uint8], c_allocator)
    mut start int64 = 0
    for i, c := range data {
        if c == '\n' {
            if start >= 0 && start <= i {
                run(mut out, data[start:i])
            }
            start = i + 1
        }
    }
    fmt.write(out)
}
`

// bigHex returns x in base 16, signed, as bigProgram reads and writes
// integers.
func bigHex(x *big.Int) string {
	return x.Text(16)
}

// bigCase is one line of bigProgram's input and the line Go's math/big
// says it writes.
type bigCase struct {
	in, want string
}

// bigCases gathers cases.
type bigCases []bigCase

// add appends the line in and its expected output.
func (c *bigCases) add(in, want string) {
	*c = append(*c, bigCase{in: in, want: want})
}

// binary adds plus, sub, mul, cmp and divmod of x and y.
func (c *bigCases) binary(x, y *big.Int) {
	xs, ys := bigHex(x), bigHex(y)
	c.add("plus "+xs+" "+ys, bigHex(new(big.Int).Add(x, y)))
	c.add("sub "+xs+" "+ys, bigHex(new(big.Int).Sub(x, y)))
	c.add("mul "+xs+" "+ys, bigHex(new(big.Int).Mul(x, y)))
	c.add("cmp "+xs+" "+ys, strconv.Itoa(x.Cmp(y)))
	if y.Sign() == 0 {
		c.add("divmod "+xs+" "+ys, "error 10")
		return
	}
	q, r := new(big.Int).QuoRem(x, y, new(big.Int))
	c.add("divmod "+xs+" "+ys, bigHex(q)+" "+bigHex(r))
}

// unary adds neg, abs, sign, zero, i64 and u64 of x, and its shifts by
// each of shifts.
func (c *bigCases) unary(x *big.Int, shifts []int64) {
	xs := bigHex(x)
	c.add("neg "+xs, bigHex(new(big.Int).Neg(x)))
	c.add("abs "+xs, bigHex(new(big.Int).Abs(x)))
	c.add("sign "+xs, strconv.Itoa(x.Sign()))
	c.add("zero "+xs, strconv.FormatBool(x.Sign() == 0))
	if x.IsInt64() {
		c.add("i64 "+xs, strconv.FormatInt(x.Int64(), 10))
	} else {
		c.add("i64 "+xs, "no")
	}
	if x.IsUint64() {
		c.add("u64 "+xs, strconv.FormatUint(x.Uint64(), 10))
	} else {
		c.add("u64 "+xs, "no")
	}
	for _, n := range shifts {
		ns := strconv.FormatInt(n, 10)
		c.add("shl "+xs+" "+ns, bigHex(new(big.Int).Lsh(x, uint(n))))
		c.add("shr "+xs+" "+ns, bigHex(new(big.Int).Rsh(x, uint(n))))
	}
}

// texts adds format and rt of x in every base, and parse of x's text in
// every base, as written, in uppercase, with a + or with leading zeros.
func (c *bigCases) texts(x *big.Int) {
	xs := bigHex(x)
	for base := 2; base <= 36; base++ {
		bs := strconv.Itoa(base)
		s := x.Text(base)
		c.add("format "+bs+" "+xs, "="+s)
		c.add("rt "+bs+" "+xs, "true")
		c.parse(s, base)
		c.parse(strings.ToUpper(s), base)
		if x.Sign() >= 0 {
			c.parse("+"+s, base)
			c.parse("000"+s, base)
		} else {
			c.parse("-000"+s[1:], base)
		}
	}
}

// parse adds parse of s in base, expecting what Go's SetString gives:
// the value, or code 6 where it fails, code 8 for a base outside 2 to
// 36 with s not empty, and code 6 for an empty s, whatever the base.
func (c *bigCases) parse(s string, base int) {
	in := "parse " + strconv.Itoa(base) + " " + hex.EncodeToString([]byte(s))
	switch {
	case s == "":
		c.add(in, "error 6")
	case base < 2 || base > 36:
		c.add(in, "error 8")
	default:
		x, ok := new(big.Int).SetString(s, base)
		if !ok {
			c.add(in, "error 6")
		} else {
			c.add(in, bigHex(x))
		}
	}
}

// bigEdges returns the edge values bigCases starts from: 0, 1, the
// int64 and uint64 edges, 2^64 and 2^128 and their neighbors, words of
// all ones, and powers of every base around the word boundaries, each
// with both signs.
func bigEdges() []*big.Int {
	var vals []*big.Int
	add := func(x *big.Int) {
		vals = append(vals, x, new(big.Int).Neg(x))
	}
	one := big.NewInt(1)
	pow2 := func(n uint) *big.Int { return new(big.Int).Lsh(one, n) }
	add(big.NewInt(0))
	add(one)
	add(big.NewInt(2))
	add(big.NewInt(1<<63 - 1))
	add(pow2(63))
	for _, n := range []uint{32, 64, 128, 192, 256} {
		p := pow2(n)
		add(p)
		add(new(big.Int).Sub(p, one))
		add(new(big.Int).Add(p, one))
	}
	add(new(big.Int).SetUint64(1<<32 + 1))
	for base := int64(3); base <= 36; base += 11 {
		for _, k := range []int64{1, 40, 41} {
			add(new(big.Int).Exp(big.NewInt(base), big.NewInt(k), nil))
		}
	}
	return vals
}

// bigRandom returns a random Int of up to bits bits, a random sign, its
// bits either random, or long runs of ones and zeros, which make carries
// and borrows run far.
func bigRandom(r *rand.Rand, bits int) *big.Int {
	n := r.IntN(bits + 1)
	x := new(big.Int)
	if r.IntN(2) == 0 {
		b := make([]byte, (n+7)/8)
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		x.SetBytes(b)
	} else {
		for i := 0; i < n; {
			run := 1 + r.IntN(200)
			if r.IntN(2) == 0 {
				for j := i; j < min(i+run, n); j++ {
					x.SetBit(x, j, 1)
				}
			}
			i += run
		}
	}
	if r.IntN(2) == 0 {
		x.Neg(x)
	}
	return x
}

// bigFull makes TestBigMatchesGo check 3000 pairs of random values, as
// go test ./codegen/c -run TestBigMatchesGo -bigfull asks, rather than
// the 300 it checks by default, which keep the test to a few seconds.
var bigFull = flag.Bool("bigfull", false, "check std/big against math/big on 3000 random pairs, not 300")

// bigShifts are the shift counts every value is shifted by: around
// every word boundary to 3 words, and 0.
var bigShifts = []int64{0, 1, 2, 31, 32, 33, 63, 64, 65, 127, 128, 129, 191, 192, 193, 1000}

// bigAllCases returns every case TestBigMatchesGo checks, with random
// values from r: every operation on every pair of bigEdges, each with
// every shift and in every base; random values of up to 4000 bits in
// pairs, 300 of them or 3000 under -bigfull, mixed in length, so a divisor may be a word or nearly the
// dividend; and the errors: division by zero, negative and huge
// counts, bad bases and bad syntax.
func bigAllCases(r *rand.Rand) bigCases {
	var c bigCases
	edges := bigEdges()
	for _, x := range edges {
		for _, y := range edges {
			c.binary(x, y)
		}
		c.unary(x, bigShifts)
		c.texts(x)
	}
	pairs := 300
	if *bigFull {
		pairs = 3000
	}
	for range pairs {
		x := bigRandom(r, 4000)
		y := bigRandom(r, 1+r.IntN(4000))
		c.binary(x, y)
		// Dividend and divisor close in length, and a divisor of one
		// word.
		c.binary(x, new(big.Int).Add(x, bigRandom(r, 64)))
		c.binary(x, bigRandom(r, 64))
		c.unary(x, []int64{int64(r.IntN(70)), int64(r.IntN(5000))})
	}
	for range 200 {
		c.texts(bigRandom(r, 3000))
	}
	for _, n := range []int64{0, 1, -1, 1<<63 - 1, -1 << 63, 1 << 32, -1 << 32, 4294967297} {
		c.add("fi64 "+strconv.FormatInt(n, 10), bigHex(big.NewInt(n)))
	}
	for _, n := range []uint64{0, 1, 1<<63 - 1, 1 << 63, 1<<64 - 1} {
		c.add("fu64 "+strconv.FormatUint(n, 10), bigHex(new(big.Int).SetUint64(n)))
	}
	for range 200 {
		n := int64(r.Uint64())
		c.add("fi64 "+strconv.FormatInt(n, 10), bigHex(big.NewInt(n)))
		u := r.Uint64()
		c.add("fu64 "+strconv.FormatUint(u, 10), bigHex(new(big.Int).SetUint64(u)))
	}
	for _, x := range []string{"0", "1", "-1", "ffffffffffffffffffffffff", "-123456789abcdef0123456789abcdef"} {
		for _, n := range []int64{-1, -64, -1 << 63} {
			c.add("shl "+x+" "+strconv.FormatInt(n, 10), "error 2")
			c.add("shr "+x+" "+strconv.FormatInt(n, 10), "error 2")
		}
		v, _ := new(big.Int).SetString(x, 16)
		// Shifted past every bit, toward negative infinity.
		c.add("shr "+x+" 9223372036854775807", bigHex(new(big.Int).Rsh(v, 1<<20)))
		if v.Sign() == 0 {
			c.add("shl "+x+" 9223372036854775807", "0")
		} else {
			c.add("shl "+x+" 9223372036854775807", "error 1")
		}
		for _, base := range []int{-1, 0, 1, 37, 100} {
			c.add("format "+strconv.Itoa(base)+" "+x, "=error 8")
			c.parse(x, base)
		}
	}
	for _, s := range []string{"", "-", "+", "--1", "+-1", "-+1", " 1", "1 ", "1_0", "_1", "0x10", "0b1",
		"0o7", "1.0", "1e3", "\x00", "\xff", "١", "z", "Z", "10", "-0", "+0", "00", "-00", "9", "a"} {
		for _, base := range []int{-1, 0, 1, 2, 8, 10, 16, 35, 36, 37} {
			c.parse(s, base)
		}
	}
	for range 2000 {
		b := make([]byte, r.IntN(12))
		for i := range b {
			const chars = "0123456789abcxyzABCXYZ+-_ \x00\xff"
			b[i] = chars[r.IntN(len(chars))]
		}
		c.parse(string(b), 2+r.IntN(35))
	}
	return c
}

// bigInput returns cases as bigProgram reads them.
func bigInput(cases bigCases) []byte {
	var b strings.Builder
	for _, c := range cases {
		b.WriteString(c.in)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// runBig runs bin, a build of bigProgram or of examples/big, on the
// file holding data, or on no file for a nil data, and returns what it
// wrote, failing t if it did not exit 0. Shifting left by the largest
// int64 asks c_allocator for close to 2^63 bytes, which malloc refuses
// with a null pointer; the address sanitizer's malloc aborts instead
// unless told to return null as the real one does, so the run sets
// allocator_may_return_null, as examples/bytes' does.
func runBig(t *testing.T, bin string, data []byte) []byte {
	t.Helper()
	var args []string
	if data != nil {
		path := filepath.Join(t.TempDir(), "input")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, path)
	}
	var stderr strings.Builder
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=allocator_may_return_null=1")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s failed: %v\n%.200s\nstderr:\n%s", filepath.Base(bin), err, out, stderr.String())
	}
	return out
}

// checkBig runs bin, a build of bigProgram, on cases, and checks that
// it writes the line math/big gives for every one.
func checkBig(t *testing.T, bin string, cases bigCases) {
	t.Helper()
	got := strings.Split(string(runBig(t, bin, bigInput(cases))), "\n")
	if len(got) != len(cases)+1 || got[len(cases)] != "" {
		t.Fatalf("wrote %d lines for %d cases", len(got)-1, len(cases))
	}
	bad := 0
	for i, c := range cases {
		if got[i] != c.want {
			bad++
			if bad <= 20 {
				t.Errorf("%.200s\nwrote %.200s\nGo's  %.200s", c.in, got[i], c.want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d cases differ from math/big", bad, len(cases))
	}
}

// bigSanitizers are the flags every build of bigProgram and of
// examples/big takes: the address and undefined behavior sanitizers,
// every finding fatal, so std/big reads and writes no list past its
// end, leaks nothing, and overflows nothing C counts as undefined, on
// every input the tests give it.
var bigSanitizers = utf8Sanitizers

// TestBigMatchesGo checks that every operation of std/big gives exactly
// what Go's math/big gives, on every case of bigAllCases, built with
// bigSanitizers at -O0 and -O1.
func TestBigMatchesGo(t *testing.T) {
	cases := bigAllCases(rand.New(rand.NewPCG(17, 18)))
	atO0Too(t, bigSanitizers, func(t *testing.T, flags []string) {
		checkBig(t, buildWithStd(t, bigProgram, flags...), cases)
	})
	t.Logf("%d cases", len(cases))
}

// bigFromBytes returns the Int whose magnitude is b, big-endian, and
// which is negative when neg is.
func bigFromBytes(b []byte, neg bool) *big.Int {
	x := new(big.Int).SetBytes(b)
	if neg {
		x.Neg(x)
	}
	return x
}

// FuzzBigMatchesGo checks, for each pair of magnitudes the fuzzer
// makes, with signs and a shift count, every operation on them against
// math/big, and parse of the first's bytes as text in every base.
func FuzzBigMatchesGo(f *testing.F) {
	f.Add([]byte{1}, []byte{2}, false, true, uint16(3))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}, true, false, uint16(64))
	f.Add([]byte("-12ab"), []byte{}, false, false, uint16(0))
	bin := buildWithStd(f, bigProgram, bigSanitizers...)
	f.Fuzz(func(t *testing.T, a, b []byte, aneg, bneg bool, n uint16) {
		x, y := bigFromBytes(a, aneg), bigFromBytes(b, bneg)
		var c bigCases
		c.binary(x, y)
		c.binary(y, x)
		shifts := []int64{int64(n), int64(n % 130)}
		c.unary(x, shifts)
		c.unary(y, shifts)
		c.texts(x)
		for base := 2; base <= 36; base++ {
			c.parse(string(a), base)
		}
		checkBig(t, bin, c)
	})
}

// TestBigExampleIsSanitizerClean builds examples/big and its C file,
// live.c, with clang's warnings as errors and bigSanitizers, and checks
// it writes exactly its output, every check ok: every refused
// allocation returns the builtin append's error, and every buffer is
// freed. It runs as runBig runs it, for its shift by the largest int64.
func TestBigExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "big", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "big", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/big/output expects a check to fail")
	}
	atO0Too(t, bigSanitizers, func(t *testing.T, flags []string) {
		if out := string(runBig(t, buildWithStdAndC(t, src, csrc, flags...), nil)); out != want {
			t.Errorf("big example wrote\n%q\nwant\n%q", out, want)
		}
	})
}

// TestBigExampleValuesMatchGo checks the values examples/big writes in
// its expectations, 30! and the base-36 forms among them, against
// math/big, so they are Go's and not copied from std/big's output.
func TestBigExampleValuesMatchGo(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "big", "main.tck"))
	f := big.NewInt(1)
	for i := int64(1); i <= 30; i++ {
		f.Mul(f, big.NewInt(i))
	}
	two64 := new(big.Int).Lsh(big.NewInt(1), 64)
	x, _ := new(big.Int).SetString("-340282366920938463463374607431768211457", 10)
	y, _ := new(big.Int).SetString("18446744073709551617", 10)
	q, r := new(big.Int).QuoRem(x, y, new(big.Int))
	umax := new(big.Int).Sub(two64, big.NewInt(1))
	for _, want := range []string{
		f.String(),
		two64.Text(2), two64.Text(8), two64.Text(36),
		x.Text(36), q.String(), r.String(),
		new(big.Int).Mul(umax, umax).String(),
		new(big.Int).Lsh(umax, 1).String(),
	} {
		if !strings.Contains(src, fmt.Sprintf("%q", want)) {
			t.Errorf("examples/big/main.tck does not expect %s", want)
		}
	}
}

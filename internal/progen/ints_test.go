package progen

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

// integer is every fixed-width integer type, for nativeOp.
type integer interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// nativeOp applies op to a and b as Go does on T, whose rules Tack's
// wrapping operators follow, as do + - and * wherever they fit: wrapping,
// the smallest signed value divided by -1 being itself, and shifts past
// the width shifting every bit out.
func nativeOp[T integer](op string, a, b T, n uint64) T {
	switch op {
	case "+", "+%":
		return a + b
	case "-", "-%":
		return a - b
	case "*", "*%":
		return a * b
	case "/":
		return a / b
	case "%":
		return a % b
	case "&":
		return a & b
	case "|":
		return a | b
	case "^":
		return a ^ b
	case "&^":
		return a &^ b
	}
	return nativeShift(op, a, n)
}

// nativeShift shifts a by n bits as Go does, left for <<, right for >>.
func nativeShift[T integer](op string, a T, n uint64) T {
	if op == "<<" {
		return a << n
	}
	return a >> n
}

// native applies op as nativeOp does on the Go type of t, to values held
// as norm holds them, and returns the result held so.
func native(t *IntType, op string, a, b, n uint64) uint64 {
	switch t {
	case Int8:
		return uint64(int64(nativeOp(op, int8(a), int8(b), n))) //nolint:gosec // bits
	case Int16:
		return uint64(int64(nativeOp(op, int16(a), int16(b), n))) //nolint:gosec // bits
	case Int32:
		return uint64(int64(nativeOp(op, int32(a), int32(b), n))) //nolint:gosec // bits
	case Int64:
		return uint64(nativeOp(op, int64(a), int64(b), n)) //nolint:gosec // bits
	case Uint8:
		return uint64(nativeOp(op, uint8(a), uint8(b), n)) //nolint:gosec // bits
	case Uint16:
		return uint64(nativeOp(op, uint16(a), uint16(b), n)) //nolint:gosec // bits
	case Uint32:
		return uint64(nativeOp(op, uint32(a), uint32(b), n)) //nolint:gosec // bits
	}
	return nativeOp(op, a, b, n)
}

// interesting returns a value of t: an edge of its range, or random bits.
func interesting(r *rand.Rand, t *IntType) uint64 {
	switch r.IntN(6) {
	case 0:
		return maxOf(t)
	case 1:
		return minOf(t)
	case 2:
		return norm(t, ^uint64(0))
	case 3:
		return uint64(r.IntN(3)) //nolint:gosec // small
	}
	return norm(t, r.Uint64())
}

// TestArithMatchesGo checks the interpreter's arithmetic, bitwise and shift
// operators against Go's on every integer type, at the edges of each
// range and at random.
func TestArithMatchesGo(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // not crypto
	ops := []string{"+", "-", "*", "+%", "-%", "*%", "/", "%", "&", "|", "^", "&^"}
	for range 20000 {
		it := intTypes[r.IntN(len(intTypes))]
		a, b := interesting(r, it), interesting(r, it)
		for _, op := range ops {
			if (op == "/" || op == "%") && b == 0 {
				continue
			}
			if got, want := arith(it, op, a, b), native(it, op, a, b, 0); got != want {
				t.Fatalf("%s: %s %s %s = %s, want %s", it.Name, literal(it, a), op,
					literal(it, b), literal(it, got), literal(it, want))
			}
		}
		n := uint64(r.IntN(it.Bits + 8)) //nolint:gosec // small
		for _, op := range []string{"<<", ">>"} {
			if got, want := shift(it, op, a, n), native(it, op, a, 0, n); got != want {
				t.Fatalf("%s: %s %s %d = %s, want %s", it.Name, literal(it, a), op, n,
					literal(it, got), literal(it, want))
			}
		}
	}
}

// bigOf returns the value of t held in bits as a big.Int.
func bigOf(t *IntType, bits uint64) *big.Int {
	if t.Signed {
		return big.NewInt(int64(bits)) //nolint:gosec // bits
	}
	return new(big.Int).SetUint64(bits)
}

// TestCompareAndFitMatchMath checks that comparisons across integer types
// compare mathematical values, and that a conversion fits exactly when the
// value is in the target's range.
func TestCompareAndFitMatchMath(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // not crypto
	for range 20000 {
		ta, tb := intTypes[r.IntN(len(intTypes))], intTypes[r.IntN(len(intTypes))]
		a, b := interesting(r, ta), interesting(r, tb)
		if got, want := compare(ta, a, tb, b), bigOf(ta, a).Cmp(bigOf(tb, b)); got != want {
			t.Fatalf("compare(%s %s, %s %s) = %d, want %d", ta.Name, literal(ta, a), tb.Name,
				literal(tb, b), got, want)
		}
		v := bigOf(ta, a)
		want := v.Cmp(bigOf(tb, minOf(tb))) >= 0 && v.Cmp(bigOf(tb, maxOf(tb))) <= 0
		if got := fits(ta, a, tb); got != want {
			t.Fatalf("fits(%s %s, %s) = %v, want %v", ta.Name, literal(ta, a), tb.Name, got, want)
		}
	}
}

// TestBytesSourceEndsInZeros checks that a Bytes source gives its bytes as
// choices, and only 0 once they run out, so every input is a finite program.
func TestBytesSourceEndsInZeros(t *testing.T) {
	b := NewBytes([]byte{7, 200})
	if got := b.Intn(5); got != 2 {
		t.Errorf("first choice %d, want 7 %% 5 = 2", got)
	}
	if got := b.Intn(300); got != 200<<8%300 {
		t.Errorf("a choice over 256 takes two bytes: got %d", got)
	}
	for range 10 {
		if got := b.Intn(9); got != 0 {
			t.Fatalf("choice past the end = %d, want 0", got)
		}
	}
}

// TestGenerateIsDeterministic checks one seed gives one program, and that
// an empty input gives a small valid one.
func TestGenerateIsDeterministic(t *testing.T) {
	a, b := Generate(NewSeeded(42)), Generate(NewSeeded(42))
	if a.Source != b.Source {
		t.Fatal("two programs of one seed differ")
	}
	empty := Generate(NewBytes(nil))
	if out, ok := empty.Expect(1000); !ok || out != "0\n" {
		t.Errorf("the empty input's program prints %q, ok %v, want \"0\\n\"", out, ok)
	}
}

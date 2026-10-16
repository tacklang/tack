package c

import (
	"strconv"
	"strings"
	"testing"
)

// TestCommaListKeepsOrderAndStaysShallow checks commaList joins short runs
// as they are and groups long ones, keeping every expression once, in
// order, and nesting them no deeper than the logarithm of their number.
func TestCommaListKeepsOrderAndStaysShallow(t *testing.T) {
	for _, n := range []int{1, 2, flatCommas, flatCommas + 1, 100, 20000} {
		exprs := make([]string, n)
		for i := range exprs {
			exprs[i] = "t" + strconv.Itoa(i) + " = " + strconv.Itoa(i)
		}
		got := commaList(exprs)
		if n <= flatCommas && got != strings.Join(exprs, ", ") {
			t.Errorf("%d expressions: got %q, want them joined as they are", n, got)
		}
		flat := strings.NewReplacer("(", "", ")", "").Replace(got)
		if flat != strings.Join(exprs, ", ") {
			t.Errorf("%d expressions: not each once, in order", n)
		}
		if depth, bound := cDepth(got), 2+bitsOf(n); depth > bound {
			t.Errorf("%d expressions nest %d deep, want at most %d", n, depth, bound)
		}
	}
}

// bitsOf returns how many bits n takes.
func bitsOf(n int) int {
	return len(strconv.FormatInt(int64(n), 2))
}

// TestCDepth checks cDepth counts parentheses, brackets and braces, and
// skips those inside character and string literals, escapes included.
func TestCDepth(t *testing.T) {
	tests := []struct {
		src  string
		want int
	}{
		{"x", 0},
		{"(x)", 1},
		{"((a) + (b))", 2},
		{"f(a[(int64_t){1}.e[0]])", 3},
		{"g('(', \"((\\\"(\")", 1},
		{"(')' + '\\'')", 1},
		{"(a), (b), ((c))", 2},
	}
	for _, tt := range tests {
		if got := cDepth(tt.src); got != tt.want {
			t.Errorf("cDepth(%q) = %d, want %d", tt.src, got, tt.want)
		}
	}
}

// TestWithSteps checks an expression with no steps is left as it is, and
// one with steps runs them first, parenthesized twice, so a condition's
// pair taken off still keeps them with it.
func TestWithSteps(t *testing.T) {
	if got := withSteps(nil, "x"); got != "x" {
		t.Errorf("no steps: got %q, want x", got)
	}
	if got := withSteps([]string{"t0 = a", "t1 = b"}, "t1"); got != "((t0 = a, t1 = b, t1))" {
		t.Errorf("two steps: got %q", got)
	}
}

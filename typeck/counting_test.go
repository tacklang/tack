package typeck_test

import (
	"strings"
	"testing"
)

// countingLoopCases are three-clause loops that count their variable from
// a known start towards a bound the condition gives, by a known step,
// without ever wrapping and without the body assigning it, so the body
// knows the variable lies between the start and the bound.
var countingLoopCases = []struct{ name, src string }{
	{"up to a constant", indexing("for mut j := 0; j < 3; j++ {", "    m[j] = a[j]", "}")},
	{"up to len", indexing("for mut j := 0; j < len(a); j++ {", "    m[j] = a[j]", "}")},
	{"at most a constant", indexing("for mut j := 0; j <= 2; j++ {", "    m[j] = a[j]", "}")},
	{"bound on the left", indexing("for mut j := 0; 3 > j; j++ {", "    m[j] = a[j]", "}")},
	{
		"bound at least on the left",
		indexing("for mut j := 0; 2 >= j; j++ {", "    m[j] = a[j]", "}"),
	},
	{"start above zero", indexing("for mut j := 1; j < 3; j++ {", "    m[j] = a[j]", "}")},
	{"step of one by +=", indexing("for mut j := 0; j < 3; j += 1 {", "    m[j] = a[j]", "}")},
	{"step of two", indexing("for mut j := 0; j < 3; j += 2 {", "    m[j] = a[j]", "}")},
	{
		"step of a known variable",
		indexing("step := 2", "for mut j := 0; j < 3; j += step {", "    m[j] = a[j]", "}"),
	},
	{
		"start of a known variable",
		indexing("first := 1", "for mut j := first; j < 3; j++ {", "    m[j] = a[j]", "}"),
	},
	{"declared type", indexing("for mut j int8 = 0; j < 3; j++ {", "    m[j] = a[j]", "}")},
	{"unsigned", indexing("for mut j uint8 = 0; j < 3; j++ {", "    m[j] = a[j]", "}")},
	{
		"continue",
		indexing(
			"for mut j := 0; j < 3; j++ {",
			"    if j == 1 {",
			"        continue",
			"    }",
			"    m[j] = a[j]",
			"}",
		),
	},
	{
		"nested loops",
		indexing(
			"for mut j := 0; j < 3; j++ {",
			"    for mut l := 0; l < 3; l++ {",
			"        m[j] = a[l]",
			"    }",
			"}",
		),
	},
	{
		"and in the condition",
		indexing("for mut j := 0; j < 3 && n > 0; j++ {", "    m[j] = a[j]", "}"),
	},
	{
		"divisor counted from one",
		indexing("mut x := 0", "for mut d := 1; d < 10; d++ {", "    x +%= 12 / d", "}", "return x"),
	},
	{
		"int8 up to one below its maximum",
		indexing(
			"mut x := 0",
			"for mut d int8 = 1; d <= 126; d++ {",
			"    x +%= 100 / d",
			"}",
			"return x",
		),
	},
	{
		"int8 step ending at its maximum",
		indexing(
			"mut x := 0",
			"for mut d int8 = 1; d < 28; d += 100 {",
			"    x +%= 100 / d",
			"}",
			"return x",
		),
	},
	{
		"up to a string's length",
		stringing("for mut j := 0; j < len(s); j++ {", "    return s[j]", "}"),
	},
	{
		"up to a mutable string's length",
		stringing("for mut j := 0; j < len(t); j++ {", "    return t[j]", "}"),
	},
	{"down to zero", indexing("for mut j := 2; j >= 0; j-- {", "    m[j] = a[j]", "}")},
	{"down past minus one", indexing("for mut j := 2; j > -1; j -= 1 {", "    m[j] = a[j]", "}")},
	{
		"down from len minus one",
		indexing("for mut j := len(a) - 1; 0 <= j; j-- {", "    m[j] = a[j]", "}"),
	},
	{
		"unsigned down to one",
		indexing("for mut j uint8 = 2; j >= 1; j-- {", "    m[j] = a[j]", "}"),
	},
}

// TestAcceptsCountingLoops checks each of countingLoopCases has no
// diagnostics.
func TestAcceptsCountingLoops(t *testing.T) {
	for _, tt := range countingLoopCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// indexJ is the diagnostic for an index j into a [3]int64 not proven.
var indexJ = spanned{
	"index j may be out of range; check 0 <= j && j < 3 first, or use v, ok := ...[j]",
	"j",
}

// notCountingLoopCases are three-clause loops that do not prove their
// variable, with the one diagnostic each must produce: the body assigns
// it, the post does not count it towards the bound by a known positive
// step, the start is not known, the bound is missing or not one the
// condition gives, the step could wrap the variable, or the variable is
// used outside the body.
var notCountingLoopCases = []struct {
	name string
	src  string
	want spanned
	// post holds the steps reported after want, as not proven not to
	// overflow: a post the loop's condition does not keep within its
	// type, once the body may have changed the variable, or the condition
	// gives no bound on the side the step moves towards.
	post []spanned
}{
	{
		"body assigns the variable",
		indexing("for mut j := 0; j < 3; j++ {", "    m[0] = a[j]", "    j = 0", "}"),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{
		"body increments the variable",
		indexing("for mut j := 0; j < 3; j++ {", "    m[0] = a[j]", "    j++", "}"),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{
		"deferred code assigns the variable",
		indexing(
			"for mut j := 0; j < 3; j++ {",
			"    defer {",
			"        j = 0",
			"    }",
			"    m[0] = a[j]",
			"}",
		),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{
		"inner loop's post steps the variable",
		indexing(
			"for mut j := 0; j < 3; j++ {",
			"    m[0] = a[j]",
			"    for mut l := 0; l < 1; j++ {",
			"        l = 1",
			"    }",
			"}",
		),
		indexJ,
		[]spanned{stepOverflow("j++", "int64"), stepOverflow("j++", "int64")},
	},
	{
		"inner loop's init assigns the variable",
		indexing(
			"for mut j := 0; j < 3; j++ {",
			"    m[0] = a[j]",
			"    for j = 0; n < 0; {",
			"    }",
			"}",
		),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{"post counts down", indexing("for mut j := 0; j < 3; j-- {", "    m[0] = a[j]", "}"), indexJ, []spanned{stepOverflow("j--", "int64")}},
	{
		"post steps by an unknown value",
		indexing("for mut j := 0; j < 3; j += n {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j += n", "int64")},
	},
	{
		"post steps by a mutable variable",
		indexing("mut step := 1", "for mut j := 0; j < 3; j += step {", "    m[0] = a[j]", "}"),
		indexJ,
		nil,
	},
	{
		"post steps by zero",
		indexing("for mut j := 0; j < 3; j += 0 {", "    m[0] = a[j]", "}"),
		indexJ,
		nil,
	},
	{
		"post steps by a negative value",
		indexing("for mut j := 0; j < 3; j += -1 {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j += -1", "int64")},
	},
	{
		"post multiplies",
		indexing("for mut j := 0; j < 3; j *= 2 {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j *= 2", "int64")},
	},
	{
		"post counts another variable",
		indexing("mut l := 0", "for mut j := 0; j < 3; l++ {", "    m[0] = a[j]", "}", "return l"),
		indexJ,
		[]spanned{stepOverflow("l++", "int64")},
	},
	{"start unknown", indexing("for mut j := n; j < 3; j++ {", "    m[0] = a[j]", "}"), indexJ, nil},
	{
		"start from a mutable variable",
		indexing("mut first := 0", "for mut j := first; j < 3; j++ {", "    m[0] = a[j]", "}"),
		indexJ,
		nil,
	},
	{"start negative", indexing("for mut j := -1; j < 3; j++ {", "    m[0] = a[j]", "}"), indexJ, nil},
	{
		"counter declared before the loop",
		indexing("mut j := 0", "for j = 0; j < 3; j++ {", "    m[0] = a[j]", "}"),
		indexJ,
		nil,
	},
	{
		"no condition",
		indexing("for mut j := 0; ; j++ {", "    m[0] = a[j]", "    break", "}"),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{
		"condition with no upper bound",
		indexing("for mut j := 0; j != 3; j++ {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{
		"or in the condition",
		indexing("for mut j := 0; j < 3 || n > 0; j++ {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j++", "int64")},
	},
	{
		"bound by an unknown value",
		indexing("for mut j := 0; j < n; j++ {", "    m[0] = a[j]", "}"),
		indexJ,
		nil,
	},
	{
		"step that wraps past the maximum",
		indexing("for mut j int8 = 2; j < 3; j += 126 {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j += 126", "int8")},
	},
	{
		"int8 up to its maximum",
		indexing(
			"mut x := 0",
			"for mut d int8 = 1; d <= 127; d++ {",
			"    x +%= 100 / d",
			"}",
			"return x",
		),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
		[]spanned{stepOverflow("d++", "int8")},
	},
	{
		"int8 step past its maximum",
		indexing(
			"mut x := 0",
			"for mut d int8 = 1; d < 29; d += 100 {",
			"    x +%= 100 / d",
			"}",
			"return x",
		),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
		[]spanned{stepOverflow("d += 100", "int8")},
	},
	{
		"int8 up to a string's length",
		stringing("for mut j int8 = 0; j < len(s); j++ {", "    return s[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(s) first, or use v, ok := ...[j]",
			"j",
		},
		[]spanned{stepOverflow("j++", "int8")},
	},
	{
		"unsigned down past zero",
		indexing("for mut j uint8 = 2; j >= 0; j-- {", "    m[0] = a[j]", "}"),
		spanned{"index j may be out of range; check j < 3 first, or use v, ok := ...[j]", "j"},
		[]spanned{{"j-- may go below 0 in uint8; check j > 0 first, use j -%= 1 to wrap, or v, ok := j - 1 and j = v when ok", "j--"}},
	},
	{
		"down with no lower bound",
		indexing("for mut j := 2; j < 3; j-- {", "    m[0] = a[j]", "}"),
		indexJ,
		[]spanned{stepOverflow("j--", "int64")},
	},
	{
		"used in the condition",
		indexing("for mut j := 0; j < 3 && a[j] > 0; j++ {", "}"),
		indexJ,
		nil,
	},
}

// stepOverflow is the error for stmt, an increment, a decrement or a
// compound assignment by + - or * of a target of the type typ, not proven
// not to overflow it, covering stmt.
func stepOverflow(stmt, typ string) spanned {
	var target, op, amount string
	switch {
	case strings.HasSuffix(stmt, "++"):
		target, op, amount = strings.TrimSuffix(stmt, "++"), "+", "1"
	case strings.HasSuffix(stmt, "--"):
		target, op, amount = strings.TrimSuffix(stmt, "--"), "-", "1"
	default:
		parts := strings.SplitN(stmt, " ", 3)
		target, op, amount = parts[0], strings.TrimSuffix(parts[1], "="), parts[2]
	}
	return spanned{
		stmt + " may overflow " + typ + "; use " + target + " " + op + "%= " + amount +
			" to wrap, or v, ok := " + target + " " + op + " " + amount + " and " + target + " = v when ok",
		stmt,
	}
}

// TestRejectsLoopsThatDoNotCount checks each of notCountingLoopCases is one
// error, covering the right source, followed by the steps it says are not
// proven.
func TestRejectsLoopsThatDoNotCount(t *testing.T) {
	for _, tt := range notCountingLoopCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			want := append([]spanned{tt.want}, tt.post...)
			if len(diags) != len(want) {
				t.Fatalf("got %v, want %v", diags, want)
			}
			for i, w := range want {
				if diags[i].Message != w.msg {
					t.Errorf("message %d = %q, want %q", i, diags[i].Message, w.msg)
				}
				if got := tt.src[diags[i].Span.Start:diags[i].Span.End]; got != w.at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, w.at)
				}
			}
		})
	}
}

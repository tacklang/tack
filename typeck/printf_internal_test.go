package typeck

import (
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/types"
)

// nameOf names a type in checkFormat's messages, as the checker would.
func nameOf(t types.Type) string { return (*types.Table)(nil).Name(t) }

// formatCases are formats and the types of the values given them, and
// the messages checkFormat returns, in order, each with the value it is
// about, -1 for the format; spread says the values are a spread, whose
// number and types are not known.
var formatCases = []struct {
	name   string
	format string
	values []types.Type
	want   []formatProblem
	spread bool
}{
	{name: "no verbs", format: "plain"},
	{name: "empty", format: ""},
	{name: "percent", format: "100%%"},
	{
		name:   "every verb",
		format: "%d %x %s %t %c %v %v %v %%",
		values: []types.Type{
			types.Int8, types.UnsafeSize, types.String, types.Bool, types.Uint8,
			types.Uint64, types.String, types.Bool,
		},
	},
	{
		name:   "any passed on matches any verb",
		format: "%d %s %c",
		values: []types.Type{types.Any, types.Any, types.Any},
	},
	{name: "invalid matches any verb", format: "%t", values: []types.Type{types.Invalid}},
	{
		name:   "mismatches",
		format: "%d %s %t %c %x",
		values: []types.Type{types.String, types.Int64, types.Uint8, types.Int64, types.Bool},
		want: []formatProblem{
			{"printf verb %d does not take string", 0, 1},
			{"printf verb %s does not take int64", 1, 1},
			{"printf verb %t does not take uint8", 2, 1},
			{"printf verb %c does not take int64", 3, 1},
			{"printf verb %x does not take bool", 4, 1},
		},
	},
	{
		name: "missing", format: "%d %d", values: []types.Type{types.Int64},
		want: []formatProblem{{"printf format has 2 verbs but 1 argument", -1, 0}},
	},
	{
		name: "extra", format: "%d", values: []types.Type{types.Int64, types.Bool, types.String},
		want: []formatProblem{{"printf format has 1 verb but 3 arguments", 1, 2}},
	},
	{
		name: "extra without verbs", format: "", values: []types.Type{types.Int64},
		want: []formatProblem{{"printf format has 0 verbs but 1 argument", 0, 1}},
	},
	{
		name: "lone percent", format: "50%",
		want: []formatProblem{{"printf format ends in a lone %", -1, 0}},
	},
	{
		name: "lone percent after a verb, with its value", format: "%d%",
		values: []types.Type{types.Int64},
		want:   []formatProblem{{"printf format ends in a lone %", -1, 0}},
	},
	{
		name: "unknown verbs take a value", format: "%q %d %Z",
		values: []types.Type{types.String, types.Bool, types.Int64},
		want: []formatProblem{
			{"unknown printf verb %q", -1, 0},
			{"printf verb %d does not take bool", 1, 1},
			{"unknown printf verb %Z", -1, 0},
		},
	},
	{
		name: "unknown verb, not graphic", format: "%\n%\x00%\t",
		values: []types.Type{types.Int64, types.Int64, types.Int64},
		want: []formatProblem{
			{`unknown printf verb "%\n"`, -1, 0},
			{`unknown printf verb "%\x00"`, -1, 0},
			{`unknown printf verb "%\t"`, -1, 0},
		},
	},
	{
		name: "unknown verb, UTF-8", format: "%é%世", values: []types.Type{types.Int64, types.Int64},
		want: []formatProblem{{"unknown printf verb %é", -1, 0}, {"unknown printf verb %世", -1, 0}},
	},
	{
		name: "unknown verb, a stray byte", format: "%\xff", values: []types.Type{types.Int64},
		want: []formatProblem{{`unknown printf verb "%\xff"`, -1, 0}},
	},
	{
		name:   "unknown verb, truncated UTF-8",
		format: "%\xe4\xb8",
		values: []types.Type{types.Int64},
		want:   []formatProblem{{`unknown printf verb "%\xe4\xb8"`, -1, 0}},
	},
	{
		name: "spread checks only verbs", format: "%d %s %y", spread: true,
		want: []formatProblem{{"unknown printf verb %y", -1, 0}},
	},
	{name: "spread, no count", format: "%d %d %d", spread: true},
}

// TestCheckFormat checks the problems checkFormat finds in each of
// formatCases.
func TestCheckFormat(t *testing.T) {
	for _, tt := range formatCases {
		t.Run(tt.name, func(t *testing.T) {
			got := checkFormat(tt.format, tt.values, !tt.spread, nameOf)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestVerbLen checks a verb's length is the UTF-8 sequence its first byte
// starts, cut at the end of the format.
func TestVerbLen(t *testing.T) {
	for _, tt := range []struct {
		b       byte
		n, want int
	}{
		{'d', 5, 1},
		{0x80, 5, 1},
		{0xBF, 5, 1},
		{0xC3, 5, 2},
		{0xDF, 5, 2},
		{0xE4, 5, 3},
		{0xEF, 5, 3},
		{0xF0, 5, 4},
		{0xF7, 5, 4},
		{0xF8, 5, 1},
		{0xFF, 5, 1},
		{0xF0, 2, 2},
		{0xE4, 1, 1},
	} {
		if got := verbLen(tt.b, tt.n); got != tt.want {
			t.Errorf("verbLen(%#x, %d) = %d, want %d", tt.b, tt.n, got, tt.want)
		}
	}
}

// FuzzCheckFormat checks checkFormat on any format and any values never
// panics, that each problem about values names values there are, that a
// spread gets no problem about a value or the count, and that values all
// of types.Any, one per verb, only ever draw problems about the format
// itself.
func FuzzCheckFormat(f *testing.F) {
	for _, tt := range formatCases {
		kinds := make([]byte, len(tt.values))
		for i, v := range tt.values {
			kinds[i] = byte(v)
		}
		f.Add(tt.format, kinds)
	}
	f.Fuzz(func(t *testing.T, format string, kinds []byte) {
		values := make([]types.Type, len(kinds))
		for i, k := range kinds {
			values[i] = types.Type(int(k) % 16)
		}
		checkValueProblems(t, checkFormat(format, values, true, nameOf), len(values))
		checkFormatOnly(t, "a spread", checkFormat(format, nil, false, nameOf))
		verbs, _ := formatVerbs(format)
		anys := slices.Repeat([]types.Type{types.Any}, len(verbs))
		checkFormatOnly(t, "one any per verb", checkFormat(format, anys, true, nameOf))
	})
}

// checkValueProblems fails t if a problem among problems, found for n
// values, has no message or names values that are not there.
func checkValueProblems(t *testing.T, problems []formatProblem, n int) {
	t.Helper()
	for _, p := range problems {
		if p.message == "" || p.value >= 0 && (p.values < 1 || p.value+p.values > n) {
			t.Fatalf("problem %+v for %d values", p, n)
		}
	}
}

// checkFormatOnly fails t if a problem among problems, found for what, is
// about a value or the number of them.
func checkFormatOnly(t *testing.T, what string, problems []formatProblem) {
	t.Helper()
	for _, p := range problems {
		if p.value >= 0 || strings.Contains(p.message, "argument") {
			t.Fatalf("%s got %+v", what, p)
		}
	}
}

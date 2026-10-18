package typeck_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/diag"
)

// relParams are the parameters every relation case's f takes: a view a, a
// string s, int64s i, j and n, a uint64 u, an int8 b, a uint8 c, an
// unsafe_size_t z and a list l passed as mut.
const relParams = "a []int64, s string, i int64, j int64, n int64, u uint64, b int8, " +
	"c uint8, z unsafe_size_t, mut l list[int64]"

// relating wraps lines in f, which takes relParams and returns an int64.
func relating(lines ...string) string {
	return inF2(relParams, lines...)
}

// acceptedRelationCases are uses the relations in force prove, and
// nothing else does.
var acceptedRelationCases = []struct{ name, src string }{
	{"chain", relating("if 0 <= i && i < n && n <= len(a) {", "    return a[i]", "}")},
	{"chain reversed", relating("if i >= 0 && n > i && len(a) >= n {", "    return a[i]", "}")},
	{
		"chain of three",
		relating("if 0 <= i && i < j && j < n && n <= len(a) {", "    return a[i] + a[j]", "}"),
	},
	{"equality", relating("if 0 <= j && j < len(a) && i == j {", "    return a[i]", "}")},
	{"plus one, bounded first", relating(
		"if 0 <= i && i < len(a) && i + 1 < len(a) {", "    k := i + 1", "    return a[k]", "}",
	)},
	{"plus one, bounded after", relating(
		"if i + 1 < len(a) && 0 <= i && i < len(a) {", "    k := i + 1", "    return a[k]", "}",
	)},
	{"plus zero needs no bound", relating("if 0 <= i && i + 0 < len(a) {", "    return a[i]", "}")},
	{
		"constant first",
		relating(
			"if 0 <= i && i < len(a) && 1 + i < len(a) {",
			"    k := -1 + i + 2",
			"    return a[k]",
			"}",
		),
	},
	{"minus a negative", relating(
		"if 0 <= i && i < len(a) && i - -1 < len(a) {", "    k := i + 1", "    return a[k]", "}",
	)},
	{
		"len minus one",
		relating("if 0 <= i && i < len(a) - 1 {", "    k := i + 1", "    return a[k]", "}"),
	},
	{
		"len minus one, at most",
		relating("if 0 <= i && i <= len(s) - 1 {", "    return int64(s[i])", "}"),
	},
	{
		"minus one after a lower bound",
		relating("if 0 < j && j <= len(a) {", "    k := j - 1", "    return a[k]", "}"),
	},
	{
		"minus two in two steps",
		relating(
			"if 1 < j && j <= len(a) {",
			"    k := j - 1",
			"    m := k - 1",
			"    return a[m] + a[k]",
			"}",
		),
	},
	{
		"unsigned minus one",
		relating("if 0 < u && u < len(a) {", "    k := u - 1", "    return a[k]", "}"),
	},
	{"int8 plus one under a small bound", relating(
		"if 0 <= b && b < 100 && b + 1 < len(a) {", "    k := b + 1", "    return a[k]", "}",
	)},
	{"uint8 plus one under a small bound", relating(
		"if c < 200 && c + 1 < len(a) {", "    k := c + 1", "    return a[k]", "}",
	)},
	{"target width plus one just under the portable bound", relating(
		"if z < 4294967295 && z + 1 < len(a) {", "    k := z + 1", "    return a[k]", "}",
	)},
	{"a large step under a tight bound", relating(
		"if 0 <= i && i <= 0 && i + 9223372036854775807 < len(a) {", "    return a[i]", "}",
	)},
	{"target width plus one under a portable bound", relating(
		"if z < 4000000000 && z + 1 < len(a) {", "    k := z + 1", "    return a[k]", "}",
	)},
	{"stored length", relating("m := len(a)", "if 0 <= i && i < m {", "    return a[i]", "}")},
	{"stored copy", relating("k := i", "if 0 <= k && k < len(a) {", "    return a[i]", "}")},
	{"last", relating("last := len(a) - 1", "if 0 <= last {", "    return a[last]", "}")},
	{
		"reassigned",
		relating(
			"mut k int64 = 0",
			"if 0 < j && j < len(a) {",
			"    k = j - 1",
			"    return a[k]",
			"}",
		),
	},
	{
		"mutable, used before its write",
		relating(
			"if 0 < j && j < len(a) {",
			"    mut k := j - 1",
			"    t := a[k]",
			"    k = 0",
			"    return t + k",
			"}",
		),
	},
	{
		"constant index under a chain",
		relating("if 3 < n && n <= len(s) {", "    return int64(s[3])", "}"),
	},
	{"array index", relating(
		"arr := [4]int64{1, 2, 3, 4}", "if 0 <= i && i < j && j <= 4 {", "    return arr[i]", "}",
	)},
	{
		"array index through a copy",
		relating(
			"arr := [4]int64{1, 2, 3, 4}",
			"if 0 < j && j <= 4 {",
			"    k := j - 1",
			"    return arr[k]",
			"}",
		),
	},
	{
		"slice chain",
		relating("if 0 <= i && i <= j && j <= n && n <= len(a) {", "    return len(a[i:n])", "}"),
	},
	{
		"slice low bound through a copy",
		relating("if 0 < j && j <= len(a) {", "    k := j - 1", "    return len(a[k:])", "}"),
	},
	{
		"slice of a known length",
		relating(
			"arr := [4]int64{1, 2, 3, 4}",
			"if 0 <= i && i < j && j <= 4 {",
			"    return len(arr[i:j])",
			"}",
		),
	},
	{"list index", relating("if 0 <= i && i < n && n <= len(l) {", "    return l[i]", "}")},
	{
		"swap through a copy",
		relating(
			"if 0 <= i && i < len(l) && i + 1 < len(l) {",
			"    k := i + 1",
			"    swap(mut l, i, k)",
			"}",
		),
	},
	{"remove through a copy", relating(
		"if 0 < j && j <= len(l) {", "    k := j - 1", "    return remove(mut l, k)", "}",
	)},
	{"path subjects", "package main\n\ntype P struct {\n    lo int64\n    hi int64\n}\n\n" +
		"func f(a []int64, p P) int64 {\n    if 0 <= p.lo && p.lo < p.hi && p.hi <= len(a) {\n" +
		"        return a[p.lo]\n    }\n    return 0\n}\n\nfunc main() {\n}\n"},
	{
		"loop condition restated",
		relating(
			"mut lo int64 = 0",
			"mut hi := len(a)",
			"mut t int64 = 0",
			"for 0 <= lo && lo < hi && hi <= len(a) {",
			"    mid := lo + (hi - lo) / 2",
			"    if lo <= mid && mid < hi {",
			"        t += a[mid]",
			"        lo = mid + 1",
			"    } else {",
			"        return t",
			"    }",
			"}",
		),
	},
	{
		"declared again each pass",
		relating(
			"mut k int64 = 1",
			"mut t int64 = 0",
			"for 0 < k && k < len(a) {",
			"    m := k - 1",
			"    t += a[m]",
			"    k++",
			"}",
			"if t < 0 {",
			"    return t",
			"}",
		),
	},
	{
		"early exit",
		relating("if i < 0 || i >= n || n > len(a) {", "    return 0", "}", "return a[i]"),
	},
}

// TestAcceptsRelations checks that each of acceptedRelationCases checks
// with no diagnostic.
func TestAcceptsRelations(t *testing.T) {
	for _, tt := range acceptedRelationCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// rejectedRelationCases are uses no relation in force proves, with the
// message of the one error each must give and the text it covers.
var rejectedRelationCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"plus one alone wraps",
		relating("if 0 <= i && i + 1 < len(a) {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"plus one alone, copied",
		relating("if 0 <= i && i + 1 < len(a) {", "    k := i + 1", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check 0 <= k && k < len(a) first, or use v, ok := ...[k]",
			"k",
		},
	},
	{"int8 plus one under a length", relating(
		"if 0 <= b && b < len(a) && b + 1 < len(a) {", "    k := b + 1", "    return a[k]", "}",
	), spanned{"index k may be out of range; check 0 <= k && k < len(a) first, or use v, ok := ...[k]", "k"}},
	{
		"uint8 plus one at its top",
		relating("if c + 1 < len(a) {", "    k := c + 1", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check k < len(a) first, or use v, ok := ...[k]", "k",
		},
	},
	{"target width past the portable bound", relating(
		"if z <= 4294967295 && z + 1 < len(a) {", "    k := z + 1", "    return a[k]", "}",
	), spanned{"index k may be out of range; check k < len(a) first, or use v, ok := ...[k]", "k"}},
	{
		"unsigned minus one at zero",
		relating("if u < len(a) {", "    k := u - 1", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check k < len(a) first, or use v, ok := ...[k]", "k",
		},
	},
	{
		"minus one without a lower bound",
		relating("if j < len(a) {", "    k := j - 1", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check 0 <= k && k < len(a) first, or use v, ok := ...[k]",
			"k",
		},
	},
	{
		"minus one is not below",
		relating("if 0 <= i && i - 1 < len(a) {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{"a large step needs a tight bound", relating(
		"if 0 <= i && i < 5 && i + 9223372036854775807 < len(a) {", "    return a[i]", "}",
	), spanned{"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]", "i"}},
	{
		"the chain is missing a link",
		relating("if 0 <= i && i < n {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{"a link ends at a write", relating(
		"mut m := n", "if 0 <= i && i < m && m <= len(a) {", "    m++", "    return a[i]", "}",
	), spanned{"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]", "i"}},
	{
		"a link ends at a mut call",
		relating(
			"if 0 <= i && i < len(l) {",
			"    err := append(mut l, 1)",
			"    if err.code != 0 {",
			"        return 0",
			"    }",
			"    return l[i]",
			"}",
		),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(l) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{"a stored length ends at an append", relating(
		"m := len(l)", "err := append(mut l, 1)", "if err.code != 0 {", "    return 0", "}",
		"if 0 <= i && i < m {", "    return l[i]", "}",
	), spanned{"index i may be out of range; check 0 <= i && i < len(l) first, or use v, ok := ...[i]", "i"}},
	{
		"a copy ends at a write to its source",
		relating(
			"mut m := j",
			"if 0 < m && m < len(a) {",
			"    k := m - 1",
			"    m = 0",
			"    return a[k]",
			"}",
		),
		spanned{
			"index k may be out of range; check 0 <= k && k < len(a) first, or use v, ok := ...[k]",
			"k",
		},
	},
	{"a self-reference relates nothing", relating(
		"mut k := j", "if 0 < k && k < len(a) {", "    k = k - 1", "    return a[k]", "}",
	), spanned{"index k may be out of range; check 0 <= k && k < len(a) first, or use v, ok := ...[k]", "k"}},
	{
		"a back edge",
		relating(
			"mut m := n",
			"mut t int64 = 0",
			"if 0 <= i && i < m && m <= len(a) {",
			"    for k := range 3 {",
			"        t += a[i] + k",
			"        m++",
			"    }",
			"}",
			"if t < 0 {",
			"    return t",
			"}",
		),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"an or drops relations",
		relating("if 0 <= i && (i < n || i < 2) && n <= len(a) {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(a) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"a loop that moves its bounds",
		relating(
			"mut lo int64 = 0",
			"mut hi := len(a)",
			"for lo < hi {",
			"    mid := lo + (hi - lo) / 2",
			"    if a[mid] < n {",
			"        lo = mid + 1",
			"    } else {",
			"        hi = mid",
			"    }",
			"}",
		),
		spanned{
			"index mid may be out of range; check 0 <= mid && mid < len(a) first, or use v, ok := ...[mid]",
			"mid",
		},
	},
	{
		"a multiply",
		relating(
			"if 0 <= i && i < n && n <= len(a) {",
			"    k := 2 * i + 1",
			"    if k < n {",
			"        return a[k]",
			"    }",
			"}",
		),
		spanned{
			"index k may be out of range; check 0 <= k && k < len(a) first, or use v, ok := ...[k]",
			"k",
		},
	},
	{
		"an index that is no name",
		relating("if 0 <= i && i < len(a) - 1 {", "    return a[i + 1]", "}"),
		spanned{
			"index may be out of range; use v, ok := ...[...]", "i + 1",
		},
	},
	{
		"a slice chain missing a link",
		relating("if 0 <= i && i <= j && n <= len(a) {", "    return len(a[i:n])", "}"),
		spanned{
			"slice bounds i:n may be out of range; check i <= n first", "i:n",
		},
	},
}

// TestRejectsRelations checks that each of rejectedRelationCases gives its
// one error, whatever notes come with it.
func TestRejectsRelations(t *testing.T) {
	for _, tt := range rejectedRelationCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			errs := errorsOf(diags)
			if len(errs) != 1 {
				t.Fatalf("got %v, want only %q", diags, tt.want.msg)
			}
			if errs[0].Message != tt.want.msg {
				t.Errorf("message = %q, want %q", errs[0].Message, tt.want.msg)
			}
			if got := tt.src[errs[0].Span.Start:errs[0].Span.End]; got != tt.want.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.want.at)
			}
		})
	}
}

// errorsOf returns the diagnostics of diags that are errors, leaving out
// their notes.
func errorsOf(diags []diag.Diagnostic) []diag.Diagnostic {
	var errs []diag.Diagnostic
	for _, d := range diags {
		if d.Severity == diag.Error {
			errs = append(errs, d)
		}
	}
	return errs
}

// relationNoteCases are missing proofs whose relations explain them, with
// the one note about relations each error must carry.
var relationNoteCases = []struct {
	name, src, want string
}{
	{
		"a missing link after",
		relating("if 0 <= i && i < n {", "    return a[i]", "}"),
		"i < n holds here; check n <= len(a) too",
	},
	{
		"a missing link before, once compared",
		relating(
			"mut m := i",
			"if 0 <= m && m < n && n <= len(a) {",
			"    m++",
			"    if m < n {",
			"        return a[m]",
			"    }",
			"}",
		),
		"",
	},
	{
		"a link ended by a write",
		relating(
			"mut m := n",
			"if 0 <= i && i < m && m <= len(a) {",
			"    m++",
			"    if i < m {",
			"        return a[i]",
			"    }",
			"}",
		),
		"i < m holds here; check m <= len(a) too",
	},
	{
		"every link ended by a write",
		relating(
			"mut m := n",
			"if 0 <= i && i < m && m <= len(a) {",
			"    m++",
			"    return a[i]",
			"}",
		),
		"",
	},
	{
		"a step that may wrap",
		relating("if 0 <= i && i + 1 < len(a) {", "    return a[i]", "}"),
		"a relation through + or - here holds only where it cannot wrap; check i < 9223372036854775807 too",
	},
	{
		"an unsigned step that may wrap",
		relating("if u < len(a) {", "    k := u - 1", "    return a[k]", "}"),
		"a relation through + or - here holds only where it cannot wrap; check 1 <= u too",
	},
	{
		"an int8 step that may wrap",
		relating("if 0 <= b && b + 1 < len(a) {", "    k := b + 1", "    return a[k]", "}"),
		"a relation through + or - here holds only where it cannot wrap; check b < 127 too",
	},
	{
		"a slice order link",
		relating("if 0 <= i && i <= j && n <= len(a) {", "    return len(a[i:n])", "}"),
		"i <= j holds here; check j <= n too",
	},
	{
		"no relation, no note",
		relating("if 0 <= i && i < 3 {", "    return a[i]", "}"),
		"",
	},
}

// TestRelationNotes checks the note about relations each of
// relationNoteCases carries, or that it carries none when want is empty.
func TestRelationNotes(t *testing.T) {
	for _, tt := range relationNoteCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			var got []string
			for _, d := range diags {
				for _, n := range d.Related {
					if relationNote(n.Message) {
						got = append(got, n.Message)
					}
				}
			}
			if tt.want == "" {
				if len(got) > 0 {
					t.Errorf("got notes %q, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("got notes %q, want only %q; all: %v", got, tt.want, diags)
			}
		})
	}
}

// relationNote reports whether msg is a note relationNotes writes.
func relationNote(msg string) bool {
	return strings.Contains(msg, " holds here; check ") ||
		strings.HasPrefix(
			msg,
			"a relation through",
		) || strings.HasPrefix(msg, "the checker follows")
}

// chainSource returns a function taking a view a and n int64 parameters,
// v0 to v<n-1>, that bounds them as a chain, 0 <= v0 < v1 < ... and
// v<n-1> < len(a), and reads a at every one of them.
func chainSource(n int) string {
	params := make([]string, 0, n)
	reads := make([]string, 0, n)
	terms := []string{"0 <= v0"}
	for k := range n {
		params = append(params, fmt.Sprintf("v%d int64", k))
		if k > 0 {
			terms = append(terms, fmt.Sprintf("v%d < v%d", k-1, k))
		}
		reads = append(reads, fmt.Sprintf("        t += a[v%d]", k))
	}
	terms = append(terms, fmt.Sprintf("v%d < len(a)", n-1))
	return "package main\n\nfunc f(a []int64, " + strings.Join(params, ", ") + ") int64 {\n" +
		"    mut t int64 = 0\n    if " + strings.Join(terms, " && ") + " {\n" +
		strings.Join(reads, "\n") + "\n    }\n    return t\n}\n\nfunc main() {\n}\n"
}

// TestShortRelationChainsProve checks that a chain within the cap proves
// every index along it.
func TestShortRelationChainsProve(t *testing.T) {
	if _, diags := checkTree(t, chainSource(30)); len(diags) > 0 {
		t.Errorf("got %d diagnostics, first %v", len(diags), diags[0])
	}
}

// TestLongRelationChainsCheckQuickly checks a chain of thousands of
// distinct variables, each a node of the graph a query builds, every one
// of them an index. Every index needs the whole chain, from 0 through v0
// to it and from it to len(a), which is past the cap, so each is
// reported, with the note on the cap, and each query stays within the
// cap, so the steps the relation queries take, the facts read into
// their index, and the facts the queries about one subject read, grow
// linearly with the chain (see checkGrowth).
func TestLongRelationChainsCheckQuickly(t *testing.T) {
	const n = 500
	_, small := checkWork(t, chainSource(n))
	diags, grown := checkWork(t, chainSource(4*n))
	checkGrowth(t, "relation steps", small.Relations, grown.Relations)
	checkGrowth(t, "facts indexed", small.Indexed, grown.Indexed)
	checkGrowth(t, "facts scanned", small.Scanned, grown.Scanned)
	if errs := errorsOf(diags); len(errs) != 4*n {
		t.Fatalf("got %d errors, want %d", len(errs), 4*n)
	}
	for _, d := range diags {
		if !slices.ContainsFunc(d.Related, func(n diag.Diagnostic) bool {
			return strings.HasPrefix(n.Message, "the checker follows at most 64 related values")
		}) {
			t.Fatalf("no note on the cap: %v", d)
		}
	}
}

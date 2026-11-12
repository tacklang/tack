package typeck_test

import (
	"strings"
	"testing"
)

// arenaFactProgram returns a program whose main holds lines, after a
// function giving the zero id, which no value the checker knows names.
func arenaFactProgram(lines ...string) string {
	return "package main\n\nfunc zero() id[int64] {\n    mut z id[int64]\n    return z\n}\n\n" +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// TestArenaElementFacts checks what the facts in force say about an
// arena's element, a[e], the one place an id meets a proof: an element
// of an immutable arena, through an immutable id, keeps what a check
// says of it, as an array's element through an immutable index does,
// since nothing can write it; one of a mutable arena never does, since a
// write through any id, the sentinel's among the ids that alias one
// another, or an add, may change it; and an id that is assigned, or is
// another id, names another element, which the check said nothing of.
// The checker never reads an id as an integer, so no other fact is about
// one.
func TestArenaElementFacts(t *testing.T) {
	const divisor = "may be zero"
	for _, tt := range []struct {
		name  string
		want  string // "" for none
		lines []string
	}{
		{name: "an immutable arena's element keeps its check", lines: []string{
			"a := make(arena[int64], 5, c_allocator)", "e := zero()",
			"if a[e] != 0 {", "    x := 100 / a[e]", "    if x == 1 {", "        exit(1)", "    }", "}",
		}, want: ""},
		{name: "a check of one id says nothing of another", lines: []string{
			"a := make(arena[int64], 5, c_allocator)", "e := zero()", "f := zero()",
			"if a[e] != 0 {", "    x := 100 / a[f]", "    if x == 1 {", "        exit(1)", "    }", "}",
		}, want: divisor},
		{name: "assigning the id ends the check", lines: []string{
			"a := make(arena[int64], 5, c_allocator)", "mut e := zero()", "f := zero()",
			"if a[e] != 0 {", "    e = f", "    x := 100 / a[e]", "    if x == 1 {", "        exit(1)", "    }", "}",
		}, want: divisor},
		{name: "a mutable arena's element keeps no check", lines: []string{
			"mut a := make(arena[int64], 5, c_allocator)", "e := zero()",
			"if a[e] != 0 {", "    x := 100 / a[e]", "    if x == 1 {", "        exit(1)", "    }", "}",
		}, want: divisor},
		{name: "a write through an aliasing id ends nothing it kept", lines: []string{
			"mut a := make(arena[int64], 5, c_allocator)", "e := zero()", "f := zero()",
			"if a[e] != 0 {", "    a[f] = 0", "    x := 100 / a[e]", "    if x == 1 {", "        exit(1)", "    }", "}",
		}, want: divisor},
		{name: "an add keeps no check", lines: []string{
			"mut a := make(arena[int64], 5, c_allocator)", "e := zero()",
			"if a[e] != 0 {", "    _, err := add(mut a, 0)", "    x := 100 / a[e]",
			"    if x == 1 || err.code != 0 {", "        exit(1)", "    }", "}",
		}, want: divisor},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, arenaFactProgram(tt.lines...))
			var errs []string
			for _, d := range diags {
				if d.Severity.String() == "error" {
					errs = append(errs, d.Message)
				}
			}
			switch {
			case tt.want == "" && len(errs) > 0:
				t.Errorf("got %q, want no errors", errs)
			case tt.want != "" && (len(errs) != 1 || !strings.Contains(errs[0], tt.want)):
				t.Errorf("got %q, want one error containing %q", errs, tt.want)
			}
		})
	}
}

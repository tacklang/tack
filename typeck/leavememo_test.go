package typeck_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/typeck"
)

// oneUseLoopIfs returns j proven, a loop whose body holds n ifs on n,
// each assigning j and returning, then one use of a[j], in the loop.
func oneUseLoopIfs(n int) []string {
	lines := append(provenJ(), "for range 2 {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "if n == "+s+" {", "j = "+s, "return "+s, "}")
	}
	return append(lines, "t +%= a[j]", "}", "return t")
}

// endingElseIfs returns an if and n-1 else ifs on n, each holding a loop
// with no condition that returns, which terminates but does not leave,
// and an else that returns, so the chain terminates and whether it does
// is asked of every link afresh, each link being the last statement of
// the else of the one before.
func endingElseIfs(n int) []string {
	var lines []string
	for k := range n {
		s := strconv.Itoa(k)
		head := "} else if n == " + s + " {"
		if k == 0 {
			head = "if n == 0 {"
		}
		lines = append(lines, head, "for {", "return "+s, "}")
	}
	return append(lines, "} else {", "return -1", "}")
}

// unionChain returns a union U of two cases, and f, holding an if and
// n-1 else ifs on n, each link holding a switch on u that names both
// cases, with no default, and returns from each, then an unreachable
// after the chain. The chain leaves only once checking each switch has
// found that it names every case (see Switch.Exhaustive), and the
// unreachable is proven only if it does.
func unionChain(n int) string {
	src := "package main\n\ntype U union {\n    a\n    b\n}\n\nfunc f(n int64, u U) int64 {\n"
	for k := range n {
		head := "    } else if n == " + strconv.Itoa(k) + " {\n"
		if k == 0 {
			head = "    if n == 0 {\n"
		}
		src += head + "        switch u {\n        case a:\n            return 1\n" +
			"        case b:\n            return 2\n        }\n"
	}
	return src + "    } else {\n        return 0\n    }\n    unreachable\n}\n\n" +
		"func main() {\n    code, fits := int32(f(1, U.a))\n    if fits { exit(code) }\n}\n"
}

// TestLeavingIsWorkedOutOnce checks that a long else if chain, a loop of
// ifs that each leave, and a chain that terminates without leaving are
// checked in steps linear in their links (see checkGrowth): the ifs,
// switches, fallthroughs and loops whether they leave or terminate is
// worked out for, and the statements copied into the regions the facts
// of an early exit hold in. Each link of a chain is the last statement
// of the else before it, so working out afresh at every link whether it
// leaves or terminates walks the rest of the chain, and copying the rest
// of a loop's body for each early exit in it copies it once an exit;
// either makes them quadratic, as copying the uses after an else if
// chain into the region of each link's facts, which starts with the next
// link (see chainFacts), would.
func TestLeavingIsWorkedOutOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines func(n int) []string
	}{
		{"else if chain", leavingElseIfs},
		{"else if chain with as many uses", elseIfWrites},
		{"loop of leaving ifs", oneUseLoopIfs},
		{"loop of leaving ifs with as many uses", loopIfWrites},
		{"terminating else if chain", endingElseIfs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags, small := checkWork(t, inF(tc.lines(500)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, inF(tc.lines(2000)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "answers of leaving or terminating worked out", small.Decided, grown.Decided)
			checkGrowth(t, "statements copied into regions", small.Copied, grown.Copied)
		})
	}
}

// fallenUnion returns a union U of two cases, and f, holding a switch on
// n whose first case falls through into its default, which ends in a
// switch on u that names both cases, with no default, and returns from
// each, then an unreachable after the switch on n. Whether the first
// case leaves is asked once it is checked, and answered no, since the
// switch on u, not yet checked, is not yet known to name every case;
// once it is, the switch on n leaves, and the unreachable is proven
// only if that answer is not trusted any more.
func fallenUnion() string {
	return "package main\n\ntype U union {\n    a\n    b\n}\n\n" +
		"func f(n int64, u U) int64 {\n    switch {\n    case n < 0:\n        fallthrough\n" +
		"    default:\n        switch u {\n        case a:\n            return 1\n" +
		"        case b:\n            return 2\n        }\n    }\n    unreachable\n}\n\n" +
		"func main() {\n    code, fits := int32(f(1, U.a))\n    if fits { exit(code) }\n}\n"
}

// TestUnionSwitchesInAChainLeave checks that a chain whose every link
// ends in a switch on a union naming every case, with no default, and a
// case falling through into such a switch, are taken to leave, with every answer remembered checked against one
// worked out afresh (see typeck.CheckLeaving): whether a switch names
// every case is known only once it is checked, so an answer of no
// remembered before then must not be trusted after (see leavememo.go).
func TestUnionSwitchesInAChainLeave(t *testing.T) {
	defer typeck.CheckLeaving()()
	for _, n := range []int{1, 2, 3, 40} {
		_, c := checkerFor(t, unionChain(n))
		if diags := c.Check(); len(diags) != 0 {
			t.Errorf("%d links: got %v, want no diagnostics", n, diags)
		}
	}
	_, c := checkerFor(t, fallenUnion())
	if diags := c.Check(); len(diags) != 0 {
		t.Errorf("fallthrough into a switch on a union: got %v, want no diagnostics", diags)
	}
}

// TestRememberedLeavingMatchesAfresh checks every case of
// examples/invalid.txtar and examples/warnings.txtar, and the shapes the
// tests above and the reach tests measure, each at a few sizes, with
// every answer the checker remembers for whether a block leaves or a
// statement terminates checked against one worked out afresh (see
// typeck.CheckLeaving), which panics on a difference. Whether a program
// is accepted does not matter here, only that both answer the same.
func TestRememberedLeavingMatchesAfresh(t *testing.T) {
	defer typeck.CheckLeaving()()
	check := func(t *testing.T, name string, src []byte) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s: %v", name, r)
			}
		}()
		checkWith(src, nil)
	}
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			check(t, c.Name, c.Source)
		}
	}
	shapes := append(reachShapes[:len(reachShapes):len(reachShapes)],
		reachShape{"one use loop of ifs", oneUseLoopIfs, 0},
		reachShape{"terminating chain", endingElseIfs, 0},
		reachShape{"leaving chain", leavingElseIfs, 0},
		reachShape{"continuing chain", continuingElseIfs, 0},
		reachShape{"continuing levels", continuingLevels, 0},
	)
	for _, shape := range shapes {
		for _, n := range []int{1, 3, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(inF(shape.lines(n)...)))
		}
	}
	for _, n := range []int{1, 3, 17} {
		check(t, fmt.Sprintf("union chain/%d", n), []byte(unionChain(n)))
	}
	check(t, "fallthrough into a switch on a union", []byte(fallenUnion()))
}

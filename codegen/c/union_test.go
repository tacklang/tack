package c_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// unionsLiveC is the C file of the union programs below: their window on
// c_allocator's count of live buffers.
const unionsLiveC = `#include <tack/tack.h>

int64_t unions_live(void) {
    return tack_c_allocator_live_();
}
`

// unionsSanitized holds unions of lists in every way a program can, and
// checks after each part that c_allocator's count of live buffers is back
// where it was, so a list a union held is freed exactly once: a list read
// after it is freed, freed twice or leaked fails the sanitizers or a
// check.
const unionsSanitized = `package main

extern("unions_live") func live() int64

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

type Bag struct {
    items list[int64]
    n     int64
}

type V union {
    num   int64
    items list[int64]
    bag   Bag
    none
}

type Outer union {
    v    V
    pair [2]int64
    leaf
}

type Holder struct {
    v V
    o Outer
}

type P union {
    pair [2]int64
    leaf
}

func listOf(n int64) list[int64] {
    mut l := make(list[int64], c_allocator)
    for i := range n {
        err := append(mut l, i + 1)
        if err.code != 0 {
            exit(1)
        }
    }
    return l
}

func sum(v V) int64 {
    switch v {
    case num(n):
        return n
    case items(l):
        mut s := 0
        for _, x := range l {
            s += x
        }
        return s
    case bag(b):
        mut s := b.n
        for i := range len(b.items) {
            s += b.items[i]
        }
        return s
    case none:
        return 0
    }
}

func outerSum(o Outer) int64 {
    switch o {
    case v(inner):
        return sum(inner)
    case pair(p):
        return p[0] + p[1]
    case leaf:
        return -1
    }
}

func reassign() {
    mut v := V.items(listOf(3))
    for i := range 20 {
        if i % 3 == 0 {
            v = V.bag(Bag{items: listOf(i), n: i})
        } else if i % 3 == 1 {
            v = V.items(listOf(i))
        } else {
            v = V.num(i)
        }
        if sum(v) < 0 {
            exit(2)
        }
    }
    v = V.none
    check(live() == 0, "reassigned in a loop")
}

func conditional(move bool) int64 {
    v := V.items(listOf(5))
    if move {
        h := Holder{v: v, o: Outer.leaf}
        return sum(h.v)
    }
    return 0
}

func nested() {
    mut o := Outer.v(V.bag(Bag{items: listOf(4), n: 1}))
    check(outerSum(o) == 11, "a union in a union")
    o = Outer.v(V.items(listOf(2)))
    check(outerSum(o) == 3 && live() == 1, "a union in a union reassigned")
    h := Holder{v: V.items(listOf(1)), o: o}
    check(sum(h.v) + outerSum(h.o) == 4, "unions in a struct")
}

func deferred(mut out int64) {
    v := V.items(listOf(4))
    defer {
        out = sum(v)
    }
    w := V.bag(Bag{items: listOf(2), n: 0})
    defer {
        out += sum(w)
    }
}

func inList() {
    mut vs := make(list[P], c_allocator)
    for i := range 10 {
        err := append(mut vs, P.pair([2]int64{i, i}))
        if err.code != 0 {
            exit(3)
        }
    }
    mut s := 0
    for i := range len(vs) {
        switch vs[i] {
        case pair(p):
            s += p[0] + p[1]
        default:
        }
    }
    check(s == 90, "a list of unions holding no list")
}

func ranging() int64 {
    v := V.items(listOf(100))
    mut s := 0
    switch v {
    case items(l):
        for i, x := range l {
            if l[i] != x {
                exit(4)
            }
            s += x
        }
    default:
    }
    return s
}

func main() {
    reassign()
    check(conditional(true) == 15 && live() == 0, "moved on one path")
    check(conditional(false) == 0 && live() == 0, "not moved")
    nested()
    check(live() == 0, "nested freed")
    mut out := 0
    deferred(mut out)
    check(out == 10 && live() == 0, "read from defers, last first")
    inList()
    check(ranging() == 5050 && live() == 0, "ranging over a payload")
}
`

// TestUnionsAreSanitizerClean builds unionsSanitized with the address and
// undefined behavior sanitizers, every finding fatal, and checks every
// check it prints passed.
func TestUnionsAreSanitizerClean(t *testing.T) {
	bin := buildExternWith(t, unionsSanitized, unionsLiveC,
		"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	runChecks(t, bin, 10)
}

// TestUnionsCompileWarningFree builds unionsSanitized with clang's
// warnings as errors, at no optimization, so the union structs, their
// helpers and the switches on them are C clang finds nothing suspicious
// in, a function ending in a switch covering every case included.
func TestUnionsCompileWarningFree(t *testing.T) {
	runChecks(t, buildExternWith(t, unionsSanitized, unionsLiveC), 10)
}

// manyCases returns a program declaring a union of n cases, every other
// one with an int64 payload, that builds one of each, matches it, and
// checks what it matched and that == tells every two apart.
func manyCases(n int) string {
	var b strings.Builder
	b.WriteString("package main\n\ntype Many union {\n")
	for i := range n {
		if i%2 == 0 {
			fmt.Fprintf(&b, "    c%d int64\n", i)
		} else {
			fmt.Fprintf(&b, "    c%d\n", i)
		}
	}
	b.WriteString("}\n\nfunc which(m Many) int64 {\n    switch m {\n")
	for i := range n {
		if i%2 == 0 {
			fmt.Fprintf(&b, "    case c%d(p):\n        return p\n", i)
		} else {
			fmt.Fprintf(&b, "    case c%d:\n        return %d\n", i, i)
		}
	}
	b.WriteString("    }\n}\n\nfunc main() {\n    mut all := make(list[Many], c_allocator)\n")
	for i := range n {
		if i%2 == 0 {
			fmt.Fprintf(&b, "    e%d := append(mut all, Many.c%d(%d))\n", i, i, i)
		} else {
			fmt.Fprintf(&b, "    e%d := append(mut all, Many.c%d)\n", i, i)
		}
		fmt.Fprintf(&b, "    if e%d.code != 0 {\n        exit(1)\n    }\n", i)
	}
	b.WriteString(`    mut bad := 0
    for i, m := range all {
        if which(m) != i {
            bad++
        }
        for j := range len(all) {
            if (all[j] == m) != (i == j) {
                bad++
            }
        }
    }
    if bad == 0 {
        print("ok\n")
    }
}
`)
	return b.String()
}

// TestManyCases builds a union of 300 cases, whose tag needs 16 bits,
// with clang's warnings as errors, and checks every case is built,
// matched and compared as itself.
func TestManyCases(t *testing.T) {
	src := manyCases(300)
	if !strings.Contains(generate(t, src), "uint16_t tag;") {
		t.Errorf("a union of 300 cases does not hold its tag in a uint16_t")
	}
	bin := buildStrict(t, src)
	out, err := exec.Command(bin).Output()
	if err != nil || string(out) != "ok\n" {
		t.Errorf("printed %q, err %v, want ok", out, err)
	}
}

// deepUnions returns a program declaring depth unions, each holding the
// one before it or a leaf, the first a list of numbers or a leaf, that
// builds a value nested through every one of them, matches it to the
// bottom, and frees it.
func deepUnions(depth int) string {
	var b strings.Builder
	b.WriteString("package main\n\nextern(\"unions_live\") func live() int64\n\n")
	b.WriteString("type U0 union {\n    items list[int64]\n    leaf\n}\n\n")
	for i := 1; i < depth; i++ {
		fmt.Fprintf(&b, "type U%d union {\n    inner U%d\n    leaf\n}\n\n", i, i-1)
	}
	b.WriteString("func bottom0(u U0) int64 {\n    switch u {\n    case items(l):\n" +
		"        return len(l)\n    case leaf:\n        return -1\n    }\n}\n\n")
	for i := 1; i < depth; i++ {
		fmt.Fprintf(&b, "func bottom%d(u U%d) int64 {\n    switch u {\n    case inner(v):\n"+
			"        return bottom%d(v)\n    case leaf:\n        return -1\n    }\n}\n\n", i, i, i-1)
	}
	b.WriteString("func build() int64 {\n    mut l := make(list[int64], c_allocator)\n" +
		"    err := append(mut l, 1, 2, 3)\n    if err.code != 0 {\n        exit(1)\n    }\n" +
		"    v0 := U0.items(l)\n")
	for i := 1; i < depth; i++ {
		fmt.Fprintf(&b, "    v%d := U%d.inner(v%d)\n", i, i, i-1)
	}
	fmt.Fprintf(&b, "    return bottom%d(v%d)\n}\n\n", depth-1, depth-1)
	b.WriteString(`func main() {
    if build() == 3 && live() == 0 {
        print("ok\n")
    }
}
`)
	return b.String()
}

// TestDeeplyNestedUnions builds 60 unions nested in one another, each
// owned since the innermost holds a list, under the sanitizers, and
// checks the value built through all of them is matched to the bottom
// and its list freed once, by the outermost's free helper calling each
// inner one's.
func TestDeeplyNestedUnions(t *testing.T) {
	bin := buildExternWith(t, deepUnions(60), unionsLiveC,
		"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil || string(out) != "ok\n" {
		t.Errorf("printed %q, err %v, want ok", out, err)
	}
}

// unionPlaces switches on unions held in every kind of place, and on a
// mutable global, which a call in a clause changes: the binding keeps the
// payload it had when the switch began, since a switch on a mutable global
// holds a copy.
const unionPlaces = `package main

type U union {
    n int64
    s string
    none
}

type S struct {
    u  U
    us [2]U
}

mut g = U.n(1)

func setG() {
    g = U.s("changed")
}

func get(u U) int64 {
    switch u {
    case n(x):
        return x
    default:
        return -1
    }
}

func main() {
    local := U.n(2)
    s := S{u: U.n(3), us: [2]U{U.none, U.n(4)}}
    mut bad := 0
    if get(local) != 2 || get(s.u) != 3 || get(s.us[1]) != 4 || get(s.us[0]) != -1 {
        bad++
    }
    switch g {
    case n(x):
        setG()
        if x != 1 || g != U.s("changed") {
            bad++
        }
    default:
        bad++
    }
    switch s.us[1] {
    case n(x):
        if x != 4 {
            bad++
        }
    default:
        bad++
    }
    if bad == 0 {
        print("ok\n")
    }
}
`

// TestUnionSwitchPlaces checks a switch on a union reads the payload of a
// local, a parameter, a field and an element in place, through a pointer
// to it, and of a mutable global from a copy, and that each binding
// reads the payload its switch matched.
func TestUnionSwitchPlaces(t *testing.T) {
	out := generate(t, unionPlaces)
	for _, want := range []string{
		"const tack_4_main1_U *const tack_s",
		"= &p_u;",
		"= &v_s.f_us.e[1];",
		"const tack_4_main1_U tack_u",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated C lacks %q:\n%s", want, out)
		}
	}
	bin := buildStrict(t, unionPlaces)
	got, err := exec.Command(bin).Output()
	if err != nil || string(got) != "ok\n" {
		t.Errorf("printed %q, err %v, want ok", got, err)
	}
}

// emptyUnion declares a union with no cases, which has no value: a
// function taking one can never be called, and a switch on one covers
// every case with no clause.
const emptyUnion = `package main

type Never union {}

type Holds struct {
    n Never
}

func absurd(n Never) int64 {
    switch n {
    }
}

func same(a Never, b Never) bool {
    return a == b
}

func wrapped(h Holds) int64 {
    return absurd(h.n)
}

func main() {
    print("ok\n")
}
`

// TestEmptyUnion builds a program declaring a union with no cases, and
// functions taking one, with clang's warnings as errors but for the
// functions nothing can call, and checks it runs.
func TestEmptyUnion(t *testing.T) {
	bin := buildWith(t, emptyUnion, "-Wno-unused-function")
	out, err := exec.Command(bin).Output()
	if err != nil || string(out) != "ok\n" {
		t.Errorf("printed %q, err %v, want ok", out, err)
	}
}

// unionMisc holds unions of every other kind of payload, an error, an
// Allocator, an array of strings and a struct of them, and uses them
// through results beside an error, mut parameters, struct fields, defers
// and loops, printing ok when every one behaves.
const unionMisc = `package main

type Pair struct {
    names [2]string
    n     int8
}

type M union {
    err   error
    alloc Allocator
    names [3]string
    pair  Pair
    small uint8
    flag
}

func describe(m M) int64 {
    switch m {
    case err(e):
        return int64(e.code)
    case alloc:
        return -1
    case names(ns):
        return len(ns[0]) + len(ns[1]) + len(ns[2])
    case pair(p):
        return p.n + len(p.names[1])
    case small(s):
        return s
    case flag:
        return 0
    }
}

func make_m(k int64) (M, error) {
    if k < 0 {
        return M.flag, error{code: 7, msg: "negative"}
    }
    if k == 0 {
        return M.alloc(c_allocator), noerror
    }
    s, ok := uint8(k % 200)
    if !ok {
        exit(1)
    }
    return M.small(s), noerror
}

func flip(mut m M) {
    switch m {
    case flag:
        m = M.err(error{code: 3, msg: "flipped"})
    default:
        m = M.flag
    }
}

type Box struct {
    m  M
    id int64
}

func main() {
    mut bad := 0
    if describe(M.err(error{code: 5, msg: "x"})) != 5 || describe(M.alloc(c_allocator)) != -1 {
        bad++
    }
    if describe(M.names([3]string{"a", "bc", "def"})) != 6 {
        bad++
    }
    if describe(M.pair(Pair{names: [2]string{"", "four"}, n: -2})) != 2 {
        bad++
    }
    m, err := make_m(-1)
    if err.code != 7 {
        bad++
    }
    if err.code == 0 && describe(m) != 0 {
        bad++
    }
    m2, err2 := make_m(250)
    if err2.code != 0 || describe(m2) != 50 {
        bad++
    }
    mut f := M.flag
    flip(mut f)
    if describe(f) != 3 {
        bad++
    }
    flip(mut f)
    if describe(f) != 0 {
        bad++
    }
    mut b := Box{m: M.small(1), id: 1}
    b.m = M.small(9)
    if describe(b.m) != 9 || b.id != 1 {
        bad++
    }
    mut total := 0
    for i := range 6 {
        mm, e := make_m(i)
        if e.code != 0 {
            continue
        }
        switch mm {
        case small(s):
            if s == 4 {
                continue
            }
            if s == 5 {
                break
            }
            total += s
        default:
            total += 100
        }
    }
    if total != 106 {
        bad++
    }
    mut seen := 0
    if total > 0 {
        d := M.names([3]string{"x", "y", "z"})
        defer {
            switch d {
            case names(ns):
                seen = len(ns[2])
            default:
            }
        }
    }
    if seen != 1 {
        bad++
    }
    if bad == 0 {
        print("ok\n")
    }
}
`

// TestUnionPayloadsOfEveryKind builds unionMisc with clang's warnings as
// errors, and under the sanitizers, and checks it prints ok.
func TestUnionPayloadsOfEveryKind(t *testing.T) {
	for _, bin := range []string{
		buildStrict(t, unionMisc),
		buildWith(t, unionMisc, "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"),
	} {
		out, err := exec.Command(bin).Output()
		if err != nil || string(out) != "ok\n" {
			t.Errorf("printed %q, err %v, want ok", out, err)
		}
	}
}

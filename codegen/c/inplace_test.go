package c_test

import (
	"regexp"
	"testing"
)

// arrayTemp matches the declaration of a temporary whose type is an array's
// struct, not a pointer to one: a copy of a whole array.
var arrayTemp = regexp.MustCompile(`\btack_array_\d+ +tack_[a-z]+\d+\b`)

// inPlaceCases are programs that read one element of an array, or range
// over one, where nothing could change the array before the read, so the
// generated C must read it where it is rather than copy it first.
var inPlaceCases = []struct {
	name string
	src  string
}{
	{
		"checked index of a mut parameter's field",
		"package main\n\ntype T struct {\n    n int64\n    a [1000]int64\n}\n\n" +
			"func get(mut t T, i int64) int64 {\n    v, ok := t.a[i]\n    if !ok {\n        return -1\n    }\n    return v\n}\n\n" +
			"func main() {\n    mut t T\n    code, fits := int32(get(mut t, 3))\n    if fits { exit(code) }\n}\n",
	},
	{
		"checked index assigned",
		"package main\n\ntype T struct {\n    a [1000]int64\n}\n\n" +
			"func get(mut t T, i int64) int64 {\n    mut v := 0\n    mut ok := false\n" +
			"    v, ok = t.a[i]\n    if !ok {\n        return -1\n    }\n    return v\n}\n\n" +
			"func main() {\n    mut t T\n    code, fits := int32(get(mut t, 3))\n    if fits { exit(code) }\n}\n",
	},
	{
		"checked index as a loop post",
		"package main\n\ntype T struct {\n    a [1000]int64\n}\n\n" +
			"func sum(mut t T) int64 {\n    mut i := 0\n    mut v := 0\n    mut ok := true\n    mut s := 0\n" +
			"    for ; ok; v, ok = t.a[i] {\n        s +%= v\n        i +%= 1\n    }\n    return s\n}\n\n" +
			"func main() {\n    mut t T\n    code, fits := int32(sum(mut t))\n    if fits { exit(code) }\n}\n",
	},
	{
		"checked index of a variable passed as mut, by a call",
		"package main\n\nfunc fill(mut a [1000]int64) {\n    a[3] = 7\n}\n\n" +
			"func three() int64 {\n    return 3\n}\n\n" +
			"func main() {\n    mut a [1000]int64\n    fill(mut a)\n    v, ok := a[three()]\n" +
			"    if ok {\n        code, fits := int32(v)\n        if fits { exit(code) }\n    }\n}\n",
	},
	{
		"range over a mut parameter's field the body never writes",
		"package main\n\ntype T struct {\n    a [1000]int64\n}\n\n" +
			"func sum(mut t T) int64 {\n    mut s := 0\n    for _, x := range t.a {\n        s +%= x\n    }\n    return s\n}\n\n" +
			"func main() {\n    mut t T\n    t.a[5] = 2\n    code, fits := int32(sum(mut t))\n    if fits { exit(code) }\n}\n",
	},
	{
		"range over a plain parameter",
		"package main\n\nfunc sum(a [1000]int64) int64 {\n    mut s := 0\n    for _, x := range a {\n        s +%= x\n    }\n    return s\n}\n\n" +
			"func main() {\n    mut a [1000]int64\n    a[9] = 3\n    code, fits := int32(sum(a))\n    if fits { exit(code) }\n}\n",
	},
	{
		"range over a variable the body only reads",
		"package main\n\nfunc main() {\n    mut a [1000]int64\n    a[1] = 4\n    mut s := 0\n" +
			"    for i, x := range a {\n        s +%= x +% a[i]\n    }\n    code, fits := int32(s)\n    if fits { exit(code) }\n}\n",
	},
}

// TestElementReadsDoNotCopyTheArray checks that reading one element of an
// array, or ranging over one the loop's body cannot change, declares no
// temporary holding a whole array.
func TestElementReadsDoNotCopyTheArray(t *testing.T) {
	for _, tt := range inPlaceCases {
		t.Run(tt.name, func(t *testing.T) {
			got := generate(t, tt.src)
			if m := arrayTemp.FindString(got); m != "" {
				t.Errorf("the array is copied into %q:\n%s", m, got)
			}
			buildStrict(t, tt.src)
		})
	}
}

// arrayCopyCases are programs where the array an element is read from could
// change between the moment Tack evaluates it and the read, so the read
// must see the array as it was: the copy the generated C keeps, or takes
// no longer, must not show.
var arrayCopyCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"checked index of a mutable global its index changes",
		"package main\n\nmut g = [3]int64{1, 2, 3}\n\n" +
			"func setG() int64 {\n    g[0] = 100\n    return 0\n}\n\n" +
			"func main() {\n    v, ok := g[setG()]\n    if ok {\n        code, fits := int32(v +% g[0])\n        if fits { exit(code) }\n    }\n}\n",
		"",
		101,
	},
	{
		"checked index of a mutable global string its index changes",
		"package main\n\nmut s = \"ab\"\n\n" +
			"func setS() int64 {\n    s = \"xy\"\n    return 1\n}\n\n" +
			"func main() {\n    c, ok := s[setS()]\n    if ok {\n        print(s)\n        exit(c)\n    }\n}\n",
		"xy",
		'b',
	},
	{
		"checked index of an array its index passes as mut",
		"package main\n\nfunc bump(mut a [3]int64) int64 {\n    a[0] = 50\n    return 0\n}\n\n" +
			"func main() {\n    mut a = [3]int64{1, 2, 3}\n    v, ok := a[bump(mut a)]\n" +
			"    if ok {\n        code, fits := int32(v +% a[0])\n        if fits { exit(code) }\n    }\n}\n",
		"",
		51,
	},
	{
		"checked index of a mut parameter its index passes on",
		"package main\n\nfunc bump(mut a [256]int64) int64 {\n    a[0] = 50\n    return 0\n}\n\n" +
			"func read(mut a [256]int64) int64 {\n    v, ok := a[bump(mut a)]\n    if !ok {\n        return -1\n    }\n    return v\n}\n\n" +
			"func main() {\n    mut a [256]int64\n    a[0] = 1\n    code, fits := int32(read(mut a) +% a[0])\n    if fits { exit(code) }\n}\n",
		"",
		51,
	},
	{
		"an index inside the array is evaluated before the outer index",
		"package main\n\ntype B struct {\n    a [3]int64\n}\n\n" +
			"func next(mut k uint8) int64 {\n    k +%= 1\n    return 1\n}\n\n" +
			"func main() {\n    mut bs [256]B\n    bs[0].a = [3]int64{1, 2, 3}\n    bs[1].a = [3]int64{4, 5, 6}\n" +
			"    mut k uint8 = 0\n    v, ok := bs[k].a[next(mut k)]\n    w, okW := bs[k].a[next(mut k)]\n" +
			"    if ok && okW {\n        code, fits := int32(v *% 10 +% w)\n        if fits { exit(code) }\n    }\n}\n",
		"",
		25,
	},
	{
		"a variable passed as mut elsewhere is read in place",
		"package main\n\nfunc fill(mut a [256]int64) {\n    a[3] = 7\n}\n\n" +
			"func three() int64 {\n    return 3\n}\n\n" +
			"func main() {\n    mut a [256]int64\n    fill(mut a)\n    v, ok := a[three()]\n" +
			"    w, okW := a[three()]\n    if ok && okW {\n        code, fits := int32(v +% w)\n        if fits { exit(code) }\n    }\n}\n",
		"",
		14,
	},
	{
		"range over a mut parameter the body passes as mut",
		"package main\n\nfunc clear(mut a [3]int64) {\n    a = [3]int64{0, 0, 0}\n}\n\n" +
			"func sum(mut a [3]int64) int64 {\n    mut s := 0\n    for _, x := range a {\n        clear(mut a)\n        s +%= x\n    }\n    return s\n}\n\n" +
			"func main() {\n    mut a = [3]int64{1, 2, 3}\n    code, fits := int32(sum(mut a) +% a[0])\n    if fits { exit(code) }\n}\n",
		"",
		6,
	},
	{
		"range over a field of a variable whose element the body writes",
		"package main\n\ntype T struct {\n    a [3]int64\n}\n\n" +
			"func main() {\n    mut t T\n    t.a = [3]int64{1, 2, 3}\n    mut s := 0\n" +
			"    for i, x := range t.a {\n        t.a[2] = 0\n        t.a[i] = 9\n        s = s *% 10 +% x\n    }\n    code, fits := int32(s -% 100)\n    if fits { exit(code) }\n}\n",
		"",
		23,
	},
	{
		"range over a mutable global a call in the body changes",
		"package main\n\nmut g = [3]int64{1, 2, 3}\n\nfunc zero() {\n    g = [3]int64{0, 0, 0}\n}\n\n" +
			"func main() {\n    mut s := 0\n    for _, x := range g {\n        zero()\n        s +%= x\n    }\n    code, fits := int32(s +% g[0])\n    if fits { exit(code) }\n}\n",
		"",
		6,
	},
	{
		"range over a mutable global the body only reads",
		"package main\n\nmut g = [3]int64{1, 2, 3}\n\n" +
			"func main() {\n    mut s := 0\n    for i, x := range g {\n        s +%= x *% g[i]\n    }\n    code, fits := int32(s)\n    if fits { exit(code) }\n}\n",
		"",
		14,
	},
}

// TestArrayReadsKeepEvaluationOrder runs each program, built with clang's
// undefined behavior sanitizer, and checks it sees the array as Tack
// evaluates it: before the index, and once before a range loop's first
// iteration.
func TestArrayReadsKeepEvaluationOrder(t *testing.T) {
	for _, tt := range arrayCopyCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

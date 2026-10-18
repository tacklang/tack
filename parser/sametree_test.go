package parser_test

import (
	"math"
	"reflect"
	"testing"
)

// sameTree reports whether a and b are deeply equal in the sense of
// reflect.DeepEqual, for values without cycles, such as the trees and
// diagnostics a parse returns. It skips what makes DeepEqual slow on a
// large tree: DeepEqual records every pointer pair it compares, to stop
// on cycles, and a parse's tree has none. TestSameTreeMatchesDeepEqual
// and TestSameTreeMatchesDeepEqualOnExamples check the two agree.
func sameTree(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.Type() == vb.Type() && sameValue(va, vb)
}

// sameValue is sameTree for two values of one type.
func sameValue(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		return sameRef(a, b)
	case reflect.Struct:
		for i := range a.NumField() {
			if !sameValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array, reflect.Map:
		return sameCollection(a, b)
	default:
		return sameScalar(a, b)
	}
}

// sameRef is sameValue for two pointers or two interfaces: both nil, or
// the same pointer, or neither nil with equal values behind them, of one
// type.
func sameRef(a, b reflect.Value) bool {
	if a.Kind() == reflect.Pointer && a.UnsafePointer() == b.UnsafePointer() {
		return true
	}
	if a.IsNil() || b.IsNil() {
		return a.IsNil() && b.IsNil()
	}
	ea, eb := a.Elem(), b.Elem()
	return ea.Type() == eb.Type() && sameValue(ea, eb)
}

// sameCollection is sameValue for two slices, two arrays or two maps. Two
// slices or maps are both nil or neither, as for DeepEqual, and of one
// length.
func sameCollection(a, b reflect.Value) bool {
	if a.Kind() != reflect.Array && (a.IsNil() != b.IsNil() || a.Len() != b.Len()) {
		return false
	}
	if a.Kind() != reflect.Array && a.UnsafePointer() == b.UnsafePointer() {
		return true
	}
	if a.Kind() == reflect.Map {
		return sameEntries(a, b)
	}
	for i := range a.Len() {
		if !sameValue(a.Index(i), b.Index(i)) {
			return false
		}
	}
	return true
}

// sameEntries reports whether the maps a and b, of one length, have the
// same keys, each holding equal values.
func sameEntries(a, b reflect.Value) bool {
	iter := a.MapRange()
	for iter.Next() {
		vb := b.MapIndex(iter.Key())
		if !vb.IsValid() || !sameValue(iter.Value(), vb) {
			return false
		}
	}
	return true
}

// sameScalar is sameValue for two values of a kind that holds no other
// values: a number, a bool, a string, a func, a channel or an unsafe
// pointer.
func sameScalar(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Func:
		// DeepEqual holds two funcs equal only when both are nil.
		return a.IsNil() && b.IsNil()
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.String:
		return a.String() == b.String()
	case reflect.Chan, reflect.UnsafePointer:
		return a.UnsafePointer() == b.UnsafePointer()
	default:
		return sameNumber(a, b)
	}
}

// sameNumber is sameValue for two numbers, compared with ==, as DeepEqual
// compares them: NaN is unequal to itself.
func sameNumber(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	case reflect.Complex64, reflect.Complex128:
		return a.Complex() == b.Complex()
	default:
		panic("sameTree: unhandled kind " + a.Kind().String())
	}
}

// sameTreeNode is a value with a field of each kind sameTree descends
// into, for TestSameTreeMatchesDeepEqual.
type sameTreeNode struct {
	val  any
	m    map[string]int
	name string
	kids []*sameTreeNode
}

// sameTreePairs returns pairs of values that differ, or not, in each way
// reflect.DeepEqual tells apart.
func sameTreePairs() []struct{ a, b any } {
	type node = sameTreeNode
	nan := math.NaN()
	shared := &node{name: "x"}
	return []struct{ a, b any }{
		{nil, nil},
		{nil, 0},
		{0, int64(0)},
		{[]int(nil), []int{}},
		{[]int{1, 2}, []int{1, 2}},
		{[]int{1, 2}, []int{1, 3}},
		{[]int{1}, []int{1, 1}},
		{[2]string{"a", "b"}, [2]string{"a", "b"}},
		{[2]string{"a", "b"}, [2]string{"a", "c"}},
		{map[string]int(nil), map[string]int{}},
		{map[string]int{"a": 1}, map[string]int{"a": 1}},
		{map[string]int{"a": 1}, map[string]int{"b": 1}},
		{map[string]int{"a": 0}, map[string]int{"b": 0}},
		{nan, nan},
		{[]float64{nan}, []float64{nan}},
		{&node{name: "a"}, &node{name: "a"}},
		{&node{name: "a"}, &node{name: "b"}},
		{&node{val: 1}, &node{val: int8(1)}},
		{&node{val: nil}, &node{val: 0}},
		{&node{kids: []*node{nil}}, &node{kids: []*node{{}}}},
		{&node{kids: []*node{shared, shared}}, &node{kids: []*node{shared, {name: "x"}}}},
		{&node{m: map[string]int{}}, &node{}},
		{(*node)(nil), (*node)(nil)},
		{(*node)(nil), &node{}},
		{func() {}, func() {}},
		{(func())(nil), (func())(nil)},
	}
}

// TestSameTreeMatchesDeepEqual checks sameTree answers as reflect.DeepEqual
// does on values that differ, or not, in each way DeepEqual tells apart.
func TestSameTreeMatchesDeepEqual(t *testing.T) {
	for i, p := range sameTreePairs() {
		if got, want := sameTree(p.a, p.b), reflect.DeepEqual(p.a, p.b); got != want {
			t.Errorf("pair %d: sameTree(%#v, %#v) = %v, DeepEqual says %v", i, p.a, p.b, got, want)
		}
	}
}

// TestSameTreeMatchesDeepEqualOnExamples checks sameTree answers as
// reflect.DeepEqual does on the trees of every example, parsed twice, which
// are equal, and parsed with a newline written at one boundary, which are
// not, their spans moved, and may differ in shape where the newline ends a
// statement.
func TestSameTreeMatchesDeepEqualOnExamples(t *testing.T) {
	for path, src := range examples(t) {
		a, b := parseDirect(t, src), parseDirect(t, src)
		if a.lexFails {
			continue
		}
		if !sameTree(a.file, b.file) || !sameTree(a.diags, b.diags) {
			t.Errorf("%s: sameTree says two parses of one source differ", path)
		}
		offs := boundaries(src)
		at := offs[len(offs)/2]
		c := parseDirect(t, src[:at]+sweepNewline+src[at:])
		if got, want := sameTree(a.file, c.file), reflect.DeepEqual(a.file, c.file); got != want {
			t.Errorf("%s: with a newline at %d, sameTree says %v, DeepEqual says %v",
				path, at, got, want)
		}
		got, want := sameTree(a.diags, c.diags), reflect.DeepEqual(a.diags, c.diags)
		if got != want {
			t.Errorf("%s: diagnostics with a newline at %d, sameTree says %v, DeepEqual says %v",
				path, at, got, want)
		}
	}
}

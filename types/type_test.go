package types_test

import (
	"slices"
	"testing"

	"github.com/tacklang/tack/types"
)

// Diagnostics name types through a Table, so each basic type must come out
// as a program writes it.
func TestTableNamesBasicTypesAsTheSourceDoes(t *testing.T) {
	tests := []struct {
		want string
		typ  types.Type
	}{
		{"int64", types.Int64},
		{"string", types.String},
		{"bool", types.Bool},
		{"invalid", types.Invalid},
	}

	table := types.NewTable()
	for _, tt := range tests {
		if got := table.Name(tt.typ); got != tt.want {
			t.Errorf("Name(%d) = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

// A zero Type is what an unchecked or failed expression carries, so it must
// be Invalid rather than some real type.
func TestInvalidIsTheZeroValue(t *testing.T) {
	var zero types.Type

	if zero != types.Invalid {
		t.Errorf("zero Type = %d, want Invalid (%d)", zero, types.Invalid)
	}
}

// The basic types are distinct IDs, so == tells them apart.
func TestBasicTypesAreDistinct(t *testing.T) {
	all := []types.Type{types.Invalid, types.Int64, types.String, types.Bool}
	seen := make(map[types.Type]bool)
	for _, typ := range all {
		if seen[typ] {
			t.Errorf("ID %d is used by more than one basic type", typ)
		}
		seen[typ] = true
	}
}

// Every type a program can write round-trips through Lookup and Name.
func TestLookupRoundTripsEveryNamedType(t *testing.T) {
	table := types.NewTable()
	for _, typ := range []types.Type{types.Int64, types.String, types.Bool} {
		got, ok := types.Lookup(table.Name(typ))
		if !ok || got != typ {
			t.Errorf("Lookup(%q) = %v, %v, want %v, true", table.Name(typ), got, ok, typ)
		}
	}
}

func TestLookupRejectsNamesThatAreNotTypes(t *testing.T) {
	for _, name := range []string{"invalid", "", "Int", "INT", "INT64", " int64", "int64 ", "int", "uint", "float", "byte", "rune", "str", "boolean", "untyped int", "int128", "uint128"} {
		if got, ok := types.Lookup(name); ok || got != types.Invalid {
			t.Errorf("Lookup(%q) = %v, %v, want Invalid, false", name, got, ok)
		}
	}
}

// An ID the Table never handed out is named invalid rather than guessed at
// or indexed out of range.
func TestTableNamesUnknownIDsInvalid(t *testing.T) {
	table := types.NewTable()
	for _, typ := range []types.Type{-1, types.Added(0), 1 << 30, -1 << 62} {
		if got := table.Name(typ); got != "invalid" {
			t.Errorf("Name(%d) = %q, want %q", typ, got, "invalid")
		}
	}
}

// A tree built without the type checker has no Table, and naming a basic
// type through the nil Table must still work rather than panic.
func TestNilTableNamesBasicTypes(t *testing.T) {
	var table *types.Table
	if got := table.Name(types.Int64); got != "int64" {
		t.Errorf("nil Table: Name(Int64) = %q, want %q", got, "int64")
	}
	if got := table.Name(99); got != "invalid" {
		t.Errorf("nil Table: Name(99) = %q, want %q", got, "invalid")
	}
}

// TestWidens checks every pair of integer types: a type widens to another
// exactly when every value of the first is a value of the second.
func TestWidens(t *testing.T) {
	ints := []types.Type{
		types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint8, types.Uint16, types.Uint32, types.Uint64,
	}
	// lowest and highest are each type's range, as a pair of float64s, which
	// hold every bound exactly.
	bounds := func(t types.Type) (float64, float64) {
		n := float64(types.Bits(t))
		if types.IsUnsigned(t) {
			return 0, pow2(n) - 1
		}
		return -pow2(n - 1), pow2(n-1) - 1
	}
	for _, from := range ints {
		for _, to := range ints {
			flo, fhi := bounds(from)
			tlo, thi := bounds(to)
			want := flo >= tlo && fhi <= thi
			if got := types.Widens(from, to); got != want {
				t.Errorf("Widens(%d, %d) = %v, want %v", from, to, got, want)
			}
		}
	}
	for _, other := range []types.Type{types.Bool, types.String, types.UntypedInt, types.Invalid} {
		if types.Widens(other, types.Int64) || types.Widens(types.Int64, other) {
			t.Errorf("type %d widens to or from int64", other)
		}
	}
}

// TestWidensTargetWidth checks every pair of an integer type and a
// target-width one: a type widens to another exactly when every value of
// the first is a value of the second at every width a target-width type
// can have, from 32 to 64 bits. So a target-width type takes only what
// fits it at 32 bits and widens only to what holds it at 64, and the two
// target-width types, which may differ in width, widen to neither each
// other nor anything else.
func TestWidensTargetWidth(t *testing.T) {
	fixed := []types.Type{
		types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint8, types.Uint16, types.Uint32, types.Uint64,
	}
	target := []types.Type{types.UnsafeSize, types.UnsafeUintptr}
	for _, tw := range target {
		for _, f := range fixed {
			lo, hi := fixedRange(f)
			wantWidens(t, f, tw, lo >= 0 && hi <= pow2(32)-1)
			wantWidens(t, tw, f, lo <= 0 && hi >= pow2(64)-1)
		}
		for _, other := range target {
			wantWidens(t, tw, other, tw == other)
		}
		for _, other := range []types.Type{types.Bool, types.String, types.UntypedInt, types.Invalid} {
			wantWidens(t, other, tw, false)
			wantWidens(t, tw, other, false)
		}
	}
}

// fixedRange returns the range of the fixed-width integer type typ, as a
// pair of float64s, which hold every bound exactly.
func fixedRange(typ types.Type) (float64, float64) {
	n := float64(types.Bits(typ))
	if types.IsUnsigned(typ) {
		return 0, pow2(n) - 1
	}
	return -pow2(n - 1), pow2(n-1) - 1
}

// wantWidens reports Widens(from, to) when it is not want.
func wantWidens(t *testing.T, from, to types.Type, want bool) {
	t.Helper()
	if got := types.Widens(from, to); got != want {
		t.Errorf("Widens(%d, %d) = %v, want %v", from, to, got, want)
	}
}

// TestTargetWidthBits checks the widths the target-width types are taken
// to have: no fixed width, 32 bits for what every target holds, and 64
// for what any target can.
func TestTargetWidthBits(t *testing.T) {
	for _, typ := range []types.Type{types.UnsafeSize, types.UnsafeUintptr} {
		if !types.TargetWidth(typ) || !types.IsInteger(typ) || !types.IsUnsigned(typ) {
			t.Errorf("type %d is not an unsigned target-width integer", typ)
		}
		if types.Bits(typ) != 0 || types.PortableBits(typ) != 32 || types.MaxBits(typ) != 64 {
			t.Errorf("type %d: Bits %d, PortableBits %d, MaxBits %d, want 0, 32, 64",
				typ, types.Bits(typ), types.PortableBits(typ), types.MaxBits(typ))
		}
	}
	if types.PortableMax != 1<<32-1 {
		t.Errorf("PortableMax = %d, want %d", uint64(types.PortableMax), uint64(1<<32-1))
	}
}

// TestFixedWidthBits checks a fixed-width integer type is its own width in
// Bits, PortableBits and MaxBits alike, and not a target-width type.
func TestFixedWidthBits(t *testing.T) {
	for _, typ := range []types.Type{types.Int8, types.Uint32, types.Int64, types.Uint64} {
		n := types.Bits(typ)
		if types.TargetWidth(typ) || types.PortableBits(typ) != n || types.MaxBits(typ) != n {
			t.Errorf("type %d: TargetWidth or its widths differ from Bits", typ)
		}
	}
}

// pow2 returns 2 to the power n.
func pow2(n float64) float64 {
	v := 1.0
	for range int(n) {
		v *= 2
	}
	return v
}

// Every sized integer type can be named, the target-width ones included,
// and UntypedInt cannot.
func TestLookupIntegerTypes(t *testing.T) {
	names := []string{
		"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64",
		"unsafe_size_t", "unsafe_uintptr_t",
	}
	for _, name := range names {
		typ, ok := types.Lookup(name)
		if !ok || !types.IsInteger(typ) || types.MaxBits(typ) == 0 {
			t.Errorf("Lookup(%q) = %v, %v", name, typ, ok)
		}
	}
	for _, name := range []string{"size_t", "uintptr_t", "unsafe_size", "unsafe_uintptr", "usize"} {
		if _, ok := types.Lookup(name); ok {
			t.Errorf("%s can be named", name)
		}
	}
	if _, ok := types.Lookup("untyped int"); ok {
		t.Error("untyped int can be named")
	}
}

// An array type written twice is one ID, and differs from any other.
func TestArrayTypesAreInterned(t *testing.T) {
	table := types.NewTable()
	a := table.Array(types.Int64, 3)
	b := table.Array(types.Int64, 3)
	if a != b {
		t.Fatalf("[3]int64 got two IDs, %d and %d", a, b)
	}
	if c := table.Array(types.Int64, 4); c == a {
		t.Error("[4]int64 got the same ID as [3]int64")
	}
	if c := table.Array(types.Uint8, 3); c == a {
		t.Error("[3]uint8 got the same ID as [3]int64")
	}
}

// An array type's shape and name come back from the Table, nested to any
// depth.
func TestArrayTypeShapes(t *testing.T) {
	table := types.NewTable()
	a := table.Array(types.Int64, 3)
	nested := table.Array(a, 2)
	if !table.IsArray(nested) || table.Elem(nested) != a || table.Len(nested) != 2 {
		t.Errorf("[2][3]int64 has element %v and length %d", table.Elem(nested), table.Len(nested))
	}
	if got := table.Name(nested); got != "[2][3]int64" {
		t.Errorf("Name = %q, want [2][3]int64", got)
	}
	if got := table.Name(table.Array(types.Bool, 0)); got != "[0]bool" {
		t.Errorf("Name = %q, want [0]bool", got)
	}
	if table.IsArray(types.Int64) || table.Len(types.Int64) != 0 ||
		table.Elem(types.Int64) != types.Invalid {
		t.Error("int64 is described as an array")
	}
}

// An array type's ID means nothing to another Table, or to none.
func TestArrayIDsBelongToTheirTable(t *testing.T) {
	a := types.NewTable().Array(types.Int64, 3)
	other := types.NewTable()
	if other.IsArray(a) || other.Name(a) != "invalid" {
		t.Error("another Table knows an array type it never added")
	}
	var none *types.Table
	if none.IsArray(a) || none.Name(a) != "invalid" || none.Count() != 0 {
		t.Error("a nil Table knows an array type")
	}
	if types.Widens(a, a) != true || types.Widens(a, types.Int64) {
		t.Error("an array type widens to something other than itself")
	}
}

// View types are interned by element type.
func TestViewTypesAreInterned(t *testing.T) {
	table := types.NewTable()
	v := table.View(types.Int64)
	if w := table.View(types.Int64); w != v {
		t.Fatalf("[]int64 got two IDs, %d and %d", v, w)
	}
	if table.View(types.Int32) == v {
		t.Error("[]int32 got the same ID as []int64")
	}
}

// View types are neither arrays nor structs, and have an element type but
// no length.
func TestViewTypeShapes(t *testing.T) {
	table := types.NewTable()
	v := table.View(types.Int64)
	a := table.Array(types.Int64, 3)
	if a == v || table.IsArray(v) || table.IsStruct(v) || !table.IsView(v) || table.IsView(a) {
		t.Error("a view is described as an array or a struct, or an array as a view")
	}
	if table.Elem(v) != types.Int64 || table.Len(v) != 0 {
		t.Errorf("[]int64 has element %v and length %d", table.Elem(v), table.Len(v))
	}
}

// View types are named []elem, holding an array, widen from nothing else,
// and mean nothing to another Table.
func TestViewTypes(t *testing.T) {
	table := types.NewTable()
	v := table.View(types.Int64)
	a := table.Array(types.Int64, 3)
	if got := table.Name(table.View(a)); got != "[][3]int64" {
		t.Errorf("Name = %q, want [][3]int64", got)
	}
	if types.Widens(table.View(types.Int8), v) || types.Widens(a, v) {
		t.Error("a view widens from another type")
	}
	if other := types.NewTable(); other.IsView(v) {
		t.Error("another Table knows a view type it never added")
	}
}

// Struct types are told apart by declaration: two of the same name, or the
// same fields, are two types.
func TestStructTypesAreNotInterned(t *testing.T) {
	table := types.NewTable()
	a := table.Struct("P")
	b := table.Struct("P")
	if a == b {
		t.Fatalf("two struct declarations got one ID, %d", a)
	}
	fields := []types.Field{{Name: "x", Type: types.Int64}}
	table.SetFields(a, fields)
	table.SetFields(b, fields)
	if a == b || types.Widens(a, b) {
		t.Error("structs with the same fields are the same type")
	}
}

// A struct is not described as an array, nor an array of structs as a
// struct, and a struct has no fields until they are set.
func TestStructTypeShapes(t *testing.T) {
	table := types.NewTable()
	p := table.Struct("Point")
	if got := table.Fields(p); got != nil {
		t.Errorf("Fields before SetFields = %v, want nil", got)
	}
	arr := table.Array(p, 2)
	if !table.IsStruct(p) || table.IsArray(p) || table.Elem(p) != types.Invalid ||
		table.Len(p) != 0 {
		t.Error("Point is not described as a struct")
	}
	if table.IsStruct(arr) || !table.IsArray(arr) || table.Elem(arr) != p {
		t.Error("[2]Point is not described as an array of Point")
	}
	if got := table.Name(arr); got != "[2]Point" {
		t.Errorf("Name = %q, want [2]Point", got)
	}
}

// A struct's fields are found by name, with their position, and only a
// struct has any.
func TestStructFields(t *testing.T) {
	table := types.NewTable()
	p := table.Struct("Point")
	arr := table.Array(p, 2)
	table.SetFields(
		p,
		[]types.Field{{Name: "x", Type: types.Int64}, {Name: "ok", Type: types.Bool}},
	)
	f, i, ok := table.Field(p, "ok")
	if !ok || i != 1 || f.Type != types.Bool {
		t.Errorf("Field(ok) = %v, %d, %v", f, i, ok)
	}
	if _, _, ok := table.Field(p, "y"); ok {
		t.Error("Point has a field y")
	}
	if _, _, ok := table.Field(arr, "x"); ok || table.Fields(types.Int64) != nil {
		t.Error("a type that is not a struct has fields")
	}
}

// SetFields on anything but a struct of the Table changes nothing, and a nil
// Table knows no structs.
func TestSetFieldsIgnoresOtherTypes(t *testing.T) {
	table := types.NewTable()
	arr := table.Array(types.Int64, 1)
	table.SetFields(arr, []types.Field{{Name: "x", Type: types.Int64}})
	table.SetFields(types.Int64, []types.Field{{Name: "x", Type: types.Int64}})
	table.SetFields(99, nil)
	if table.Fields(arr) != nil || table.Elem(arr) != types.Int64 {
		t.Error("SetFields changed an array type")
	}
	var none *types.Table
	if none.IsStruct(types.Added(0)) || none.Fields(types.Added(0)) != nil {
		t.Error("a nil Table knows a struct")
	}
}

// TestNameWithQualifiesStructs checks NameWith writes a struct type as the
// function given says, an array's element included, and every other type
// as Name does.
func TestNameWithQualifiesStructs(t *testing.T) {
	table := types.NewTable()
	tok := table.Struct("Token")
	arr := table.Array(table.Array(tok, 2), 3)
	q := func(_ types.Type, name string) string { return "lexer." + name }
	for _, tt := range []struct {
		want string
		typ  types.Type
	}{
		{"lexer.Token", tok},
		{"[3][2]lexer.Token", arr},
		{"int64", types.Int64},
		{"[4]uint8", table.Array(types.Uint8, 4)},
	} {
		if got := table.NameWith(tt.typ, q); got != tt.want {
			t.Errorf("NameWith = %q, want %q", got, tt.want)
		}
	}
	if got := table.NameWith(arr, nil); got != table.Name(arr) || got != "[3][2]Token" {
		t.Errorf("NameWith(nil) = %q, want %q", got, table.Name(arr))
	}
}

// TestErrorIsABasicType checks error is a basic type, looked up by its
// name, that is neither an integer nor passed as any.
func TestErrorIsABasicType(t *testing.T) {
	if got, ok := types.Lookup("error"); !ok || got != types.Error {
		t.Errorf("Lookup(error) = %v, %v, want Error, true", got, ok)
	}
	if types.InAny(types.Error) || types.IsInteger(types.Error) {
		t.Errorf("error can be passed as any, or is an integer")
	}
}

// TestErrorIsAFixedStruct checks every Table, a nil one included, names
// error and reports it as a struct with the fields code, an int32, and
// msg, a string, in that order, and no other basic type as one.
func TestErrorIsAFixedStruct(t *testing.T) {
	for _, table := range []*types.Table{types.NewTable(), nil} {
		checkErrorStruct(t, table)
		for _, typ := range []types.Type{types.Int64, types.String, types.Any} {
			if table.IsStruct(typ) {
				t.Errorf("%s is a struct", table.Name(typ))
			}
		}
	}
}

// checkErrorStruct checks table names error and gives it its two fields.
func checkErrorStruct(t *testing.T, table *types.Table) {
	t.Helper()
	if got := table.Name(types.Error); got != "error" {
		t.Errorf("Name(Error) = %q, want error", got)
	}
	if !table.IsStruct(types.Error) || table.IsArray(types.Error) || table.IsView(types.Error) {
		t.Errorf("Error is not a struct alone")
	}
	want := []types.Field{{Name: "code", Type: types.Int32}, {Name: "msg", Type: types.String}}
	if fields := table.Fields(types.Error); !slices.Equal(fields, want) {
		t.Errorf("Fields(Error) = %v, want %v", fields, want)
	}
	if f, i, ok := table.Field(types.Error, "msg"); !ok || i != 1 || f.Type != types.String {
		t.Errorf("Field(Error, msg) = %v, %d, %v", f, i, ok)
	}
}

// TestSetFieldsLeavesErrorAlone checks SetFields cannot change error's
// fields, and that knowing error adds no type to a Table.
func TestSetFieldsLeavesErrorAlone(t *testing.T) {
	table := types.NewTable()
	table.SetFields(types.Error, []types.Field{{Name: "x", Type: types.Int64}})
	if fields := table.Fields(types.Error); len(fields) != 2 || fields[0].Name != "code" {
		t.Errorf("SetFields changed error's fields to %v", fields)
	}
	if table.Count() != 0 {
		t.Errorf("a Table added %d types for error", table.Count())
	}
}

// A list type is interned by its element type, and is no array, view or
// struct.
func TestListTypes(t *testing.T) {
	table := types.NewTable()
	l := table.List(types.Int64)
	if m := table.List(types.Int64); m != l {
		t.Fatalf("list[int64] got two IDs, %d and %d", l, m)
	}
	if table.List(types.Int32) == l || table.View(types.Int64) == l {
		t.Error("list[int32] or []int64 got the same ID as list[int64]")
	}
	if !table.IsList(l) || table.IsView(l) || table.IsArray(l) || table.IsStruct(l) {
		t.Error("list[int64] is not a list, or is taken for a view, an array or a struct")
	}
	if table.IsList(table.View(types.Int64)) || table.IsList(types.Int64) {
		t.Error("IsList holds for a view or an int64")
	}
}

// A list type knows its element, names itself list[T], and nothing widens
// to it.
func TestListTypeShapes(t *testing.T) {
	table := types.NewTable()
	l := table.List(types.Int64)
	if table.Elem(l) != types.Int64 {
		t.Errorf("Elem = %s, want int64", table.Name(table.Elem(l)))
	}
	if got := table.Name(table.List(table.Array(types.String, 2))); got != "list[[2]string]" {
		t.Errorf("Name = %q, want list[[2]string]", got)
	}
	if types.Widens(table.List(types.Int8), l) {
		t.Error("list[int8] widens to list[int64]")
	}
}

// Owned holds for a list, and an array or struct holding one at any
// depth, even through a struct that holds itself through an array of none,
// and for nothing else; HasZero fails for those and for Allocator, and
// anything holding one, and holds for everything else.
func TestOwnedAndZeroValues(t *testing.T) {
	table := types.NewTable()
	l := table.List(types.Int64)
	inner := table.Struct("Inner")
	outer := table.Struct("Outer")
	plain := table.Struct("Plain")
	handle := table.Struct("Handle")
	self := table.Struct("Self")
	table.SetFields(inner, []types.Field{{Name: "items", Type: l}})
	table.SetFields(outer, []types.Field{{Name: "n", Type: types.Int64}, {Name: "in", Type: inner}})
	table.SetFields(plain, []types.Field{{Name: "s", Type: types.String}})
	table.SetFields(handle, []types.Field{{Name: "a", Type: types.Allocator}})
	table.SetFields(self, []types.Field{{Name: "me", Type: table.Array(self, 0)}})
	for _, tt := range []struct {
		typ         types.Type
		owned, zero bool
	}{
		{l, true, false},
		{inner, true, false},
		{outer, true, false},
		{table.Array(outer, 2), true, false},
		{plain, false, true},
		{self, false, true},
		{types.Allocator, false, false},
		{handle, false, false},
		{table.Array(types.Allocator, 3), false, false},
		{table.View(types.Int64), false, true},
		{types.Error, false, true},
		{types.Int64, false, true},
		{types.Invalid, false, true},
	} {
		if got := table.Owned(tt.typ); got != tt.owned {
			t.Errorf("Owned(%s) = %v, want %v", table.Name(tt.typ), got, tt.owned)
		}
		if got := table.HasZero(tt.typ); got != tt.zero {
			t.Errorf("HasZero(%s) = %v, want %v", table.Name(tt.typ), got, tt.zero)
		}
	}
}

// Allocator is a basic type, named by Lookup, that no value of can be
// passed as any, and that is no integer.
func TestAllocatorIsABasicType(t *testing.T) {
	if typ, ok := types.Lookup("Allocator"); !ok || typ != types.Allocator {
		t.Errorf("Lookup(Allocator) = %v, %v", typ, ok)
	}
	if types.InAny(types.Allocator) || types.IsInteger(types.Allocator) {
		t.Error("Allocator passes as any, or is an integer")
	}
	if got := (*types.Table)(nil).Name(types.Allocator); got != "Allocator" {
		t.Errorf("Name = %q", got)
	}
}

// TestFuncTypes checks that a function type is found again by its
// signature, mut parameters and results included, is named as a program
// writes it, and keeps its own copy of its signature.
func TestFuncTypes(t *testing.T) {
	table := types.NewTable()
	point := table.Struct("Point")
	params := []types.FuncParam{{Type: types.Int64}, {Type: point, Mut: true}}
	f := table.Func(params, []types.Type{types.Int64, types.Error})
	if got := table.Func(params, []types.Type{types.Int64, types.Error}); got != f {
		t.Errorf("one signature gave two types, %d and %d", f, got)
	}
	others := []types.Type{
		table.Func(params, []types.Type{types.Int64}),
		table.Func(
			[]types.FuncParam{{Type: types.Int64}, {Type: point}},
			[]types.Type{types.Int64, types.Error},
		),
		table.Func(nil, nil),
	}
	for _, other := range others {
		if other == f {
			t.Errorf("a different signature gave the same type %d", f)
		}
	}
	for typ, want := range map[types.Type]string{
		f:         "func(int64, mut Point) (int64, error)",
		others[0]: "func(int64, mut Point) int64",
		others[2]: "func()",
	} {
		if got := table.Name(typ); got != want {
			t.Errorf("named %q, want %q", got, want)
		}
	}
	params[0].Type = types.Bool
	if got := table.FuncParams(f)[0].Type; got != types.Int64 {
		t.Errorf("changing the caller's parameters changed the type's to %s", table.Name(got))
	}
}

// TestFuncTypeShapes checks that a function type is no other kind of
// type, owns nothing, has no zero value, and is a pointer in size, and
// that a struct holding one has no zero value either.
func TestFuncTypeShapes(t *testing.T) {
	table := types.NewTable()
	f := table.Func([]types.FuncParam{{Type: types.Int64}}, []types.Type{types.Bool})
	switch {
	case !table.IsFunc(f), table.IsArray(f), table.IsStruct(f), table.IsList(f), table.IsView(f):
		t.Errorf("%s is the wrong kind of type", table.Name(f))
	case table.Owned(f) || table.HasZero(f):
		t.Errorf(
			"%s: owned %v, has a zero value %v, want neither",
			table.Name(f),
			table.Owned(f),
			table.HasZero(f),
		)
	case table.Size(f) != types.Size{Bytes: 8, Align: 8}:
		t.Errorf("%s has size %+v, want 8 bytes aligned to 8", table.Name(f), table.Size(f))
	}
	holder := table.Struct("Holder")
	table.SetFields(holder, []types.Field{{Name: "f", Type: f}})
	if table.HasZero(holder) || table.Owned(holder) {
		t.Errorf("a struct holding a function value: has a zero value %v, owned %v; want neither",
			table.HasZero(holder), table.Owned(holder))
	}
}

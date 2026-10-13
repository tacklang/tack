package types_test

import (
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
	for _, typ := range []types.Type{-1, 12, 1 << 30, -1 << 62} {
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

// pow2 returns 2 to the power n.
func pow2(n float64) float64 {
	v := 1.0
	for range int(n) {
		v *= 2
	}
	return v
}

// Every sized integer type can be named, and UntypedInt cannot.
func TestLookupIntegerTypes(t *testing.T) {
	for _, name := range []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64"} {
		typ, ok := types.Lookup(name)
		if !ok || !types.IsInteger(typ) || types.Bits(typ) == 0 {
			t.Errorf("Lookup(%q) = %v, %v", name, typ, ok)
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

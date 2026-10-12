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

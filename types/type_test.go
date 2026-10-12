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
		{"int", types.Int},
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
	all := []types.Type{types.Invalid, types.Int, types.String, types.Bool}
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
	for _, typ := range []types.Type{types.Int, types.String, types.Bool} {
		got, ok := types.Lookup(table.Name(typ))
		if !ok || got != typ {
			t.Errorf("Lookup(%q) = %v, %v, want %v, true", table.Name(typ), got, ok, typ)
		}
	}
}

func TestLookupRejectsNamesThatAreNotTypes(t *testing.T) {
	for _, name := range []string{"invalid", "", "Int", "INT", " int", "int ", "int64", "uint", "float", "byte", "rune", "str", "boolean"} {
		if got, ok := types.Lookup(name); ok || got != types.Invalid {
			t.Errorf("Lookup(%q) = %v, %v, want Invalid, false", name, got, ok)
		}
	}
}

// An ID the Table never handed out is named invalid rather than guessed at
// or indexed out of range.
func TestTableNamesUnknownIDsInvalid(t *testing.T) {
	table := types.NewTable()
	for _, typ := range []types.Type{-1, 4, 1 << 30, -1 << 62} {
		if got := table.Name(typ); got != "invalid" {
			t.Errorf("Name(%d) = %q, want %q", typ, got, "invalid")
		}
	}
}

// A tree built without the type checker has no Table, and naming a basic
// type through the nil Table must still work rather than panic.
func TestNilTableNamesBasicTypes(t *testing.T) {
	var table *types.Table
	if got := table.Name(types.Int); got != "int" {
		t.Errorf("nil Table: Name(Int) = %q, want %q", got, "int")
	}
	if got := table.Name(99); got != "invalid" {
		t.Errorf("nil Table: Name(99) = %q, want %q", got, "invalid")
	}
}

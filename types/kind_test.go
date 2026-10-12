package types_test

import (
	"testing"

	"github.com/tacklang/tack/types"
)

func TestKindNamesMatchTheSourceLanguage(t *testing.T) {
	tests := []struct {
		want string
		kind types.Kind
	}{
		{"int", types.Int},
		{"string", types.String},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("Kind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// A zero Kind is what an unchecked or failed expression carries, so it must
// render as something a diagnostic can print without looking like a real type.
func TestInvalidKindIsTheZeroValueAndNamesItself(t *testing.T) {
	var zero types.Kind

	if zero != types.Invalid {
		t.Errorf("zero Kind = %d, want Invalid (%d)", zero, types.Invalid)
	}
	if got := zero.String(); got != "invalid" {
		t.Errorf("Invalid.String() = %q, want %q", got, "invalid")
	}
}

// Every kind a program can write round-trips through Lookup and String.
func TestLookupRoundTripsEveryNamedKind(t *testing.T) {
	for _, kind := range []types.Kind{types.Int, types.String, types.Bool} {
		got, ok := types.Lookup(kind.String())
		if !ok || got != kind {
			t.Errorf("Lookup(%q) = %v, %v, want %v, true", kind.String(), got, ok, kind)
		}
	}
}

// Names that are not types, including the name Invalid prints as, near
// misses, and names from other languages, are not found.
func TestLookupRejectsNamesThatAreNotTypes(t *testing.T) {
	for _, name := range []string{"invalid", "", "Int", "INT", " int", "int ", "int64", "uint", "float", "byte", "rune", "str", "boolean"} {
		if got, ok := types.Lookup(name); ok || got != types.Invalid {
			t.Errorf("Lookup(%q) = %v, %v, want Invalid, false", name, got, ok)
		}
	}
}

// A Kind outside the declared set prints as invalid rather than as a type
// name or a panic.
func TestUnknownKindPrintsAsInvalid(t *testing.T) {
	for _, kind := range []types.Kind{-1, 4, 1 << 30} {
		if got := kind.String(); got != "invalid" {
			t.Errorf("Kind(%d).String() = %q, want %q", int(kind), got, "invalid")
		}
	}
}

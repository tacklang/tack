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

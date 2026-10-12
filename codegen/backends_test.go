package codegen_test

import (
	"testing"

	"github.com/tacklang/tack/codegen"
	// The backends register themselves from init. Importing them is the whole
	// of what plugging a backend in requires.
	_ "github.com/tacklang/tack/codegen/c"
)

func TestShippedBackendsAreRegistered(t *testing.T) {
	for _, name := range []string{"c"} {
		e, ok := codegen.Get(name)
		if !ok {
			t.Errorf(
				"Get(%q) = _, false, want the backend to be registered by importing its package",
				name,
			)
			continue
		}
		if e.Name() != name {
			t.Errorf("Get(%q).Name() = %q, want %q", name, e.Name(), name)
		}
	}
}

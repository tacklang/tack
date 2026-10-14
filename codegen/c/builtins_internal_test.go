package c

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/tacklang/tack/builtin"
)

// TestEveryBuiltinHasACBody checks the backend ships a C body for every
// builtin extern func, defining it under its C name, so no call can lower
// to a function the generated C never defines.
func TestEveryBuiltinHasACBody(t *testing.T) {
	for _, fn := range builtin.Funcs() {
		body, err := builtinBody(fn)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		if !strings.HasPrefix(body.def, "//") ||
			!strings.Contains(body.def, "static void "+fn.CName+"(") {
			t.Errorf("builtin/%s.c does not document and define static void %s:\n%s",
				fn.Name, fn.CName, body.def)
		}
		if strings.Contains(body.def, "#include") {
			t.Errorf("builtin/%s.c has an #include after its first line of code", fn.Name)
		}
	}
}

// TestNoCBodyWithoutABuiltin checks every file in builtin/ is the body of a
// builtin, so none is dead and none is misnamed.
func TestNoCBodyWithoutABuiltin(t *testing.T) {
	files, err := fs.Glob(builtinFS, "builtin/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(f, "builtin/"), ".c")
		if _, ok := builtin.Lookup(name); !ok {
			t.Errorf("%s is not the body of any builtin", f)
		}
	}
}

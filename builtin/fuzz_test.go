package builtin

import (
	"errors"
	"testing"

	"github.com/tacklang/tack/source"
)

// FuzzDeclare feeds declare arbitrary std/builtin files: whatever they
// hold, it returns builtins or a *DiagnosticsError, and never panics.
func FuzzDeclare(f *testing.F) {
	for _, seed := range []string{
		"package builtin\n\nextern(\"tack_builtin_print\") func print(s string)\n",
		"package builtin\n\nextern func print(s string) int64\n",
		"package builtin\n\nfunc p() {\n}\nvar x = 1\ntype T struct {\n}\n",
		"package builtin\n\nimport a from \"b\"\n",
		"package builtin\n\nextern(\"tack_p\") func p(mut v []int64, x [2]Point)\n",
		"package main",
		"",
		"\x00\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file := &source.File{ID: "builtin.tck", Filename: "builtin.tck", Data: []byte(src)}
		funcs, err := declare([]*source.File{file})
		if err == nil {
			for _, fn := range funcs {
				if fn.Name == "" || len(fn.CName) <= len(cNamePrefix) {
					t.Fatalf("declared a builtin with no name: %+v", fn)
				}
			}
			return
		}
		var diags *DiagnosticsError
		if !errors.As(err, &diags) || len(diags.Diags) == 0 {
			t.Fatalf("declare = %v, want diagnostics", err)
		}
		_ = err.Error()
	})
}

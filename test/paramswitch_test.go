package compilertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// FuzzParamSwitchFrontEnd is FuzzFrontEndAfterErrors started from
// switches on type parameters alone: every invalid example of one, and
// examples/reflect-switch's main package, whose switches nest, narrow
// and recurse through generic functions, so the fuzzer spends its time
// near them, where damaged cases, patterns and tags reach the resolver,
// the type checker and the search for instances.
func FuzzParamSwitchFrontEnd(f *testing.F) {
	cases, err := invalid.Load(filepath.FromSlash(invalid.Path))
	if err != nil {
		f.Fatal(err)
	}
	for _, c := range cases {
		if strings.HasPrefix(c.Name, "param-switch-") {
			f.Add(c.Source)
		}
	}
	src, err := os.ReadFile(filepath.Join(examplesDir, "reflect-switch", "main.tck"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(src)
	f.Fuzz(checkFrontEnd)
}

package compilertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// FuzzBlankParamsFrontEnd is FuzzFrontEndAfterErrors started from
// parameters named _ alone: every invalid example of one, the warnings
// example of one, and examples/blank-params's main package, whose _
// parameters are plain, own, mut, scope, variadic, receivers, an
// interface's, a closure's and an extern func's, so the fuzzer spends its
// time near them, where damaged signatures and bodies reading _ reach
// the resolver, the type checker and the unused-parameter warning.
func FuzzBlankParamsFrontEnd(f *testing.F) {
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(filepath.FromSlash(path))
		if err != nil {
			f.Fatal(err)
		}
		for _, c := range cases {
			if strings.HasPrefix(c.Name, "blank-param-") || c.Name == "unused-param-blank" {
				f.Add(c.Source)
			}
		}
	}
	src, err := os.ReadFile(filepath.Join(examplesDir, "blank-params", "main.tck"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(src)
	f.Fuzz(checkFrontEnd)
}

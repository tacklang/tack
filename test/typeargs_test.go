package compilertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// FuzzTypeArgsFrontEnd is FuzzFrontEndAfterErrors started from calls that
// write type arguments alone: every invalid example of one, and
// examples/type-args's main package, whose calls write them plain,
// qualified and partial, of basic, struct, list, map and nested types, in
// generic bodies, fields loops, closures and defers, so the fuzzer spends
// its time near them, where damaged brackets, type arguments of the wrong
// kind or count, and callees that are not generic reach the parser, the
// resolver and the type checker.
func FuzzTypeArgsFrontEnd(f *testing.F) {
	cases, err := invalid.Load(filepath.FromSlash(invalid.Path))
	if err != nil {
		f.Fatal(err)
	}
	for _, c := range cases {
		if strings.HasPrefix(c.Name, "type-args-") ||
			strings.HasPrefix(c.Name, "generic-explicit-") ||
			strings.HasPrefix(c.Name, "generic-type-param-") ||
			c.Name == "func-value-generic-brackets" {
			f.Add(c.Source)
		}
	}
	src, err := os.ReadFile(filepath.Join(examplesDir, "type-args", "main.tck"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(src)
	f.Fuzz(checkFrontEnd)
}

package compilertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// FuzzMakeDataFrontEnd is FuzzFrontEndAfterErrors started from make of
// Data types alone: every invalid example of one, and
// examples/reflect-make's main package, whose makes name basic types,
// structs, Texts and type parameters, in cases of switches on them and
// beside fields loops, so the fuzzer spends its time near them, where
// damaged type names, allocators and argument counts reach the
// resolver and the type checker.
func FuzzMakeDataFrontEnd(f *testing.F) {
	cases, err := invalid.Load(filepath.FromSlash(invalid.Path))
	if err != nil {
		f.Fatal(err)
	}
	for _, c := range cases {
		if strings.HasPrefix(c.Name, "make-data-") || strings.HasPrefix(c.Name, "list-make-") {
			f.Add(c.Source)
		}
	}
	src, err := os.ReadFile(filepath.Join(examplesDir, "reflect-make", "main.tck"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(src)
	f.Fuzz(checkFrontEnd)
}

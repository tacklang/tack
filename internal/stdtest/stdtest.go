package stdtest

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/tacklang/tack/builtin"
)

// Home returns the root of the repository, which holds the standard
// library at src/std as an installation's TACK_HOME does.
func Home() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("stdtest: cannot find this package's source file")
	}
	// This file is internal/stdtest/stdtest.go below the root.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Dir returns the repository's standard library, Home()/src/std.
func Dir() string { return filepath.Join(Home(), "src", "std") }

// BuiltinDir returns the repository's std/builtin directory.
func BuiltinDir() string { return filepath.Join(Dir(), "builtin") }

// IncludeDir returns the directory std's C files are compiled with on the
// include path, Dir()/include.
func IncludeDir() string { return filepath.Join(Dir(), "include") }

// loaded is the result of loading the repository's builtins, once.
var loaded = sync.OnceValues(func() (*builtin.Set, error) {
	return builtin.Load(BuiltinDir())
})

// Builtins returns the builtins the repository's std/builtin declares. It
// fails tb if they do not load.
func Builtins(tb testing.TB) *builtin.Set {
	tb.Helper()
	set, err := loaded()
	if err != nil {
		tb.Fatalf("loading the repository's builtins: %v", err)
	}
	return set
}

// MustBuiltins is Builtins for code with no testing.TB at hand, such as a
// pipeline helper that turns panics into results: it panics if the
// repository's builtins do not load.
func MustBuiltins() *builtin.Set {
	set, err := loaded()
	if err != nil {
		panic("stdtest: loading the repository's builtins: " + err.Error())
	}
	return set
}

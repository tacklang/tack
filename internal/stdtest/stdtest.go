package stdtest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/loader"
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

// IncludeDir returns the repository's std include directory, Dir()/include,
// whose tack/tack.h is a copy of the runtime header built into the
// compiler, kept byte for byte by a test of package builtin: a test
// compiling C outside the driver puts it on the include path.
func IncludeDir() string { return filepath.Join(Dir(), "include") }

// StdPath is where Std puts the standard library, relative to Home: the
// path the names of std's files start with in a loader's diagnostics.
const StdPath = "src/std"

// Std returns the repository's standard library as a loader takes it, for
// a loader whose own files are elsewhere, such as in memory: Home()/src/std,
// read from the host's file system rooted at Home(), so std's files are
// named src/std/... wherever the repository is, and goldens that show
// them stay portable.
func Std() *loader.Std {
	return &loader.Std{FS: loader.FromFS(os.DirFS(Home())), Dir: StdPath}
}

// Builtins returns the builtins the compiler implements, for a test's
// pipeline.
func Builtins(tb testing.TB) *builtin.Set {
	tb.Helper()
	return builtin.Default()
}

// MustBuiltins is Builtins for code with no testing.TB at hand, such as a
// pipeline helper that turns panics into results.
func MustBuiltins() *builtin.Set { return builtin.Default() }

// runtimeFiles is where RuntimeCFiles wrote the runtime's C files, once
// per test binary.
var runtimeFiles = sync.OnceValues(writeRuntimeCFiles)

// RuntimeCFiles returns the paths of the runtime's C files built into the
// compiler, in name order, written once per test binary to a directory
// named after their hash below the system's temporary directory, for a
// test that compiles them outside the driver. It panics if they cannot be
// written.
func RuntimeCFiles() []string {
	paths, err := runtimeFiles()
	if err != nil {
		panic("stdtest: writing the runtime's C files: " + err.Error())
	}
	return paths
}

// RuntimeCFile returns the path RuntimeCFiles wrote the runtime's C file
// named name to.
func RuntimeCFile(name string) string {
	for _, p := range RuntimeCFiles() {
		if filepath.Base(p) == name {
			return p
		}
	}
	panic("stdtest: the runtime has no C file " + name)
}

// writeRuntimeCFiles writes the runtime's C files to a directory named
// after their hash, each whole to a temporary file renamed into place, so
// test binaries running at once share it safely, and returns their paths.
func writeRuntimeCFiles() ([]string, error) {
	files := builtin.RuntimeCFiles()
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name))
		h.Write(f.Data)
	}
	dir := filepath.Join(os.TempDir(), "tack-runtime-"+hex.EncodeToString(h.Sum(nil))[:16])
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("making %s: %w", dir, err)
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(dir, f.Name)
		tmp, err := os.CreateTemp(dir, f.Name+".*")
		if err != nil {
			return nil, fmt.Errorf("writing %s: %w", f.Name, err)
		}
		_, werr := tmp.Write(f.Data)
		cerr := tmp.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(tmp.Name())
			return nil, errors.Join(werr, cerr)
		}
		if err := os.Rename(tmp.Name(), paths[i]); err != nil {
			return nil, fmt.Errorf("writing %s: %w", f.Name, err)
		}
	}
	return paths, nil
}

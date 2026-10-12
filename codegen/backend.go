// Package codegen defines the contract between the compiler front end and a
// code generation backend, and the registry that makes backends pluggable.
//
// A backend is a Backend: it turns a resolved *ast.File into the bytes of
// some target language, names the file extension those bytes belong in, and
// says how to turn that file into an executable. Nothing above this package
// knows what any particular backend emits.
//
// Backends live in sub-packages and register themselves from init, so adding
// one is a new package plus an import. See codegen/c.
package codegen

import (
	"fmt"
	"sort"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// Backend is a code generation backend.
type Backend interface {
	// Name is the identifier the driver's --backend flag selects on.
	Name() string

	// Ext is the file extension of the generated output, including the dot.
	Ext() string

	// Generate turns a resolved file into target source. A non-empty
	// diagnostic slice means generation failed and the bytes are unusable.
	Generate(file *ast.File) ([]byte, []diag.Diagnostic)

	// BuildCmd is the command that turns generated output at src into an
	// executable at out. The first element is the program, the rest its
	// arguments.
	BuildCmd(src, out string) []string
}

var backends = make(map[string]Backend)

// Register adds a backend to the registry. It panics on a duplicate name,
// because two backends answering to one name is a build-time mistake, not a
// condition a caller could recover from. Backends call this from init.
func Register(e Backend) {
	name := e.Name()
	if _, dup := backends[name]; dup {
		panic(fmt.Sprintf("codegen: backend %q registered twice", name))
	}
	backends[name] = e
}

// Get returns the backend registered under name.
func Get(name string) (Backend, bool) {
	e, ok := backends[name]
	return e, ok
}

// Names returns the registered backend names in sorted order.
func Names() []string {
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

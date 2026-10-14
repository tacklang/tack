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

	// Generate turns a type-checked program into target source. A
	// non-empty diagnostic slice means generation failed and the bytes are
	// unusable.
	Generate(prog *ast.Program) ([]byte, []diag.Diagnostic)

	// Header returns the C header, tack.h, that declares every extern func
	// of prog, which Generate must have accepted, and every type their
	// signatures use, for the C files that define them.
	Header(prog *ast.Program) []byte

	// CompileCmd and BuildCmd take every path absolute, so that a file
	// named like an option, -fplugin=x.c, is still read as a file.
	//
	// CompileCmd is the command that compiles the C file at src, one that
	// defines extern funcs, into an object file at obj, with the header
	// Header returned at header included first. The first element is the
	// program, the rest its arguments.
	CompileCmd(src, header, obj string) []string

	// BuildCmd is the command that turns generated output at src into an
	// executable at out, linked with the object files objs, which
	// CompileCmd made. The first element is the program, the rest its
	// arguments.
	BuildCmd(src string, objs []string, out string) []string
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

package codegen

import (
	"fmt"
	"sort"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
)

// Backend is a code generation backend.
type Backend interface {
	// Name is the identifier the driver's --backend flag selects on.
	Name() string

	// Ext is the file extension of the generated output, including the dot.
	Ext() string

	// Generate turns a type-checked program into target source. builtins
	// are the builtins prog was resolved and checked with, whose C names
	// its calls to them lower to. A non-empty diagnostic slice means
	// generation failed and the bytes are unusable. A program Refuse
	// refuses, one not checked or with errors, is never lowered.
	Generate(prog *ast.Program, builtins *builtin.Set) ([]byte, []diag.Diagnostic)

	// Header returns the C header, tack.h, that declares every extern func
	// of prog, which Generate must have accepted, and every type their
	// signatures use, for the C files that define them.
	Header(prog *ast.Program) []byte

	// CompileCmd, CompileStdCmd and BuildCmd take every path absolute,
	// so that a file named like an option, -fplugin=x.c, is still read as
	// a file, and all build at the optimization level opt.
	//
	// CompileCmd is the command that compiles the C file at src, one that
	// defines extern funcs, into an object file at obj, with the header
	// Header returned at header included first, and the directory include,
	// the standard library's own headers, on the include path, where that
	// header finds what it includes from there. The first element is the
	// program, the rest its arguments.
	CompileCmd(src, header, include, obj string, opt Opt) []string

	// CompileStdCmd is the command that compiles the C file at src, one of
	// the standard library's, such as the body of a builtin, into an
	// object file at obj, as CompileCmd does a program's, but with the
	// directory include, the standard library's own headers, on the
	// include path, and no program's header. The first element is the
	// program, the rest its arguments.
	CompileStdCmd(src, include, obj string, opt Opt) []string

	// BuildCmd is the command that turns generated output at src into an
	// executable at out, linked with the object files objs, which
	// CompileCmd and CompileStdCmd made, with the directory include, the
	// standard library's own headers, on the include path, where the
	// generated output finds what it includes from there. The first
	// element is the program, the rest its arguments.
	BuildCmd(src, include string, objs []string, out string, opt Opt) []string

	// VersionCmd is the command whose standard output identifies the
	// toolchain CompileStdCmd runs: its version, and its default target
	// when it has one. The driver keys the objects it caches across builds
	// on that output, so a different toolchain never reuses them. The
	// first element is the program, the rest its arguments.
	VersionCmd() []string
}

// Opt is an optimization level a build runs at, as clang's -O flags name
// them: 0 for none, 1 to 3 for more, each faster code built more slowly,
// and s and z for smaller code, z the smallest.
type Opt string

// Opts is every optimization level, in the order -O flags are listed.
var Opts = []Opt{"0", "1", "2", "3", "s", "z"}

// DefaultOpt is the level a build runs at unless asked for another: the
// highest that keeps the C standard's semantics. clang's -Ofast is higher
// but not a level here, since it gives those up.
const DefaultOpt Opt = "3"

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

// Refuse returns the error a backend reports, before lowering anything,
// for a program that must never have code generated from it: one the type
// checker has not checked, or one any stage reported an error about,
// counted in prog.Errors and in its type table's Errors. It returns nil
// for a program the front end accepted. Every backend's Generate calls it
// first: the front end goes on past errors to report as many as it can, so
// a tree that reached the backend may hold names bound to nothing and
// types that failed to check, and only this check keeps them from being
// lowered. Reaching it is a bug in the caller, never the program's fault.
func Refuse(prog *ast.Program) []diag.Diagnostic {
	var msg string
	switch {
	case prog == nil:
		msg = "internal error: no program to generate code from"
	case prog.Types == nil:
		msg = "internal error: code generation was given a program the type checker has not checked"
	case prog.Errors > 0 || prog.Types.Errors > 0:
		msg = fmt.Sprintf(
			"internal error: code generation was given a program with %d errors",
			max(prog.Errors, prog.Types.Errors),
		)
	default:
		return nil
	}
	return []diag.Diagnostic{{Severity: diag.Error, Message: msg}}
}

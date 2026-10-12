// Package codegen defines the contract between the compiler front end and a
// code generation backend, and the registry that makes backends pluggable.
//
// A backend implements Backend. It turns a resolved, type-checked *ast.File
// into the bytes of some target language, or into diagnostics if the file uses
// something it cannot lower. It also names the file extension those bytes
// belong in, and the command that turns such a file into an executable. That
// is all the driver needs: it writes the bytes to a temporary file with the
// backend's extension and runs the backend's build command. Nothing above this
// package knows what any particular backend emits.
//
// Backends live in sub-packages and register themselves from init by calling
// Register, so plugging one in is a new package plus a blank import in the
// driver. Registering two backends under one name panics, since that is a
// mistake in how the compiler was built, not something a caller could recover
// from. Get finds a backend by name, and Names lists them in sorted order.
//
// One backend ships today: codegen/c.
package codegen

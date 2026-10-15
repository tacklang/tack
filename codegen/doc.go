// Package codegen defines the contract between the compiler front end and a
// code generation backend, and the registry that makes backends pluggable.
//
// A backend implements Backend. It turns a resolved, type-checked
// *ast.Program into the bytes of some target language, or into diagnostics
// if the program uses something it cannot lower. It also names the file
// extension those bytes belong in, the header, tack.h, that the program's C
// files are compiled against, the command that compiles each of those files
// to an object, the command that turns the generated file and the
// objects into an executable, and the command whose output identifies the
// toolchain, which the driver keys the std objects it caches on. That is
// all the driver needs: it writes the
// bytes to a temporary file with the backend's extension and the header
// beside it, runs the compile command on each C file and then the build
// command. Nothing above this package knows what any particular backend
// emits.
//
// Backends live in sub-packages and register themselves from init by calling
// Register, so plugging one in is a new package plus a blank import in the
// driver. Registering two backends under one name panics, since that is a
// mistake in how the compiler was built, not something a caller could recover
// from. Get finds a backend by name, and Names lists them in sorted order.
//
// One backend ships today: codegen/c.
package codegen

// Package stdtest gives tests the repository's own standard library,
// tackc/src/std, so that no test depends on where, or whether, Tack is
// installed.
//
// The compiler loads std's packages from $TACK_HOME. A test that read
// $TACK_HOME, or ~/.tack when it is unset, would test whatever happens to
// be installed rather than the std beside the compiler under test, and
// would fail on a machine where nothing is. So tests never read either:
// they take the std of the source tree they are in from here.
//
// Home is the repository's root, laid out as an installation is, with the
// standard library at Home()/src/std; tests of the driver pass it as
// TACK_HOME, and tests of the loader and of the pipeline give Std to a
// loader, for its imports of std packages. Builtins is the compiler's own
// builtins, builtin.Default, which are built in and the same for every
// test. IncludeDir is std's include directory, whose tack/tack.h is a copy
// of the runtime header built into the compiler, for a test compiling C
// outside the driver. The root is found from this package's own source file, so it
// does not depend on the working directory of the test.
package stdtest

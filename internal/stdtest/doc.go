// Package stdtest gives tests the repository's own standard library,
// tackc/src/std, so that no test depends on where, or whether, Tack is
// installed.
//
// The compiler takes its builtins from std/builtin under $TACK_HOME, and
// the driver links their C bodies from there. A test that read
// $TACK_HOME, or ~/.tack when it is unset, would test whatever happens to
// be installed rather than the std beside the compiler under test, and
// would fail on a machine where nothing is. So tests never read either:
// they take the std of the source tree they are in from here.
//
// Home is the repository's root, laid out as an installation is, with the
// standard library at Home()/src/std; tests of the driver pass it as
// TACK_HOME. Builtins is what builtin.Load makes of Home()/src/std/builtin,
// loaded once per test binary and shared, since a builtin.Set never
// changes. The root is found from this package's own source file, so it
// does not depend on the working directory of the test.
package stdtest

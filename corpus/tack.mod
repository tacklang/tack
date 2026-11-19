// corpus/ holds no package of the port: it is the test corpus, the
// example programs under examples/, with their archives of invalid and
// warned programs, and the goldens under golden/, which tools/corpus runs
// a compiler over. This file makes the directory a module root only so
// that a library check of the port, such as tack --check . at its root,
// skips it, as it skips runtime/: the walk would otherwise check every
// example as a package of the port's own module. Every example compiles
// today, so the check passes either way, but the port's check should not
// depend on what the corpus holds.
module corpus

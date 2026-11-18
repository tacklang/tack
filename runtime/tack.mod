// runtime/ holds no Tack package: it is the source of the files the
// compiler carries inside it, the builtins' documentation, builtin.tck,
// and the C runtime with its header, tack/tack.h. This file makes the
// directory a module root only so that a library check of the port,
// such as tack --check . at its root, skips it: the walk would otherwise
// compile builtin.tck, which is documentation and no code, and fail.
module runtime

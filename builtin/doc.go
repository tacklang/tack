// Package builtin is the compiler's builtins and runtime: the functions,
// types and values Tack provides without a declaration or an import, the
// C that some of them and lists run on, and the file that documents them
// all. Everything here is built into the compiler, so no file a user or
// std holds can add a builtin, change one, or change the runtime.
//
// # The builtins
//
// The resolver needs the builtins' names, to bind a call to one and to
// stop a program from declaring a function with a builtin's name; the
// loader, to stop a package from taking one; the type checker their
// signatures; and the C backend the C names of those with a C body. All of
// them read one Set, Default, which is what keeps the stages from
// disagreeing about what the builtins are. A Set is never changed and is
// passed explicitly to every stage that needs it; a nil *Set has no
// builtins, as a dump of the tokens or the trees needs none.
//
// print and exit have a C body, so a Func each: a name, its parameters'
// names and types, and the C name, tack_builtin_print and
// tack_builtin_exit, that a call lowers to. Funcs lists them, Lookup finds
// one by name, Called finds the one a resolved call calls, and Used lists
// those a resolved program calls anywhere, each once, in Funcs' order.
//
// len, make, append, reserve, pop, remove, swap, replace, put, delete, has,
// drop, add, add_all, insert and fields are generic: a parameter of each
// accepts more than one type, which no Tack declaration describes, so the
// type checker checks each itself and the C backend lowers each in place;
// fields takes a type, which the parser reads as a fields loop's
// ast.FieldsOf. Generic lists them, Names lists every builtin function's
// name, and ParamNames gives any one's parameter names. Types and Values
// list the builtin types and values, true, false, noerror and
// c_allocator, which are keywords, and Constraints the builtin
// constraints, Data, which IsConstraint tells by name.
//
// # The runtime
//
// RuntimeCFiles are the runtime's C files: exit.c, list.c, print.c,
// process.c and unreachable.c, the bodies of print and exit, ordinary
// non-static functions under their C names, the abort an unreachable
// statement lowers to (UnreachableSymbol), the process's start and end,
// which the C main of a program that RunsC calls (StartSymbol and
// EndSymbol), the first ignoring SIGPIPE, and the end a write to stdout
// or stderr that fails with EPIPE makes, as exit(141) does, which print,
// exit and std/fmt's C reach through tack_stdio_failed_, and the allocators and the grow and free functions of lists,
// with the check of an arena's length limit and its testing hook, and a
// pool's tags, the generation its new slots start at and the limit on its
// slots, with their testing hooks, ListSymbols among them. RuntimeHeader is <tack/tack.h>, which every C
// file of a program and of std includes. The driver writes them to each
// build's own temporary directory and compiles against that copy, linking
// the runtime objects a program needs (see command tack); UsesLists says
// whether a program needs the list functions. make install-std writes a
// copy of the header to std/include/tack/tack.h, for extern C and editors,
// which no build reads.
//
// # The documentation
//
// builtin.tck, built into the compiler, declares every builtin type,
// value and function, with a doc comment each, the generic functions with
// stand-in types, Elem, Key, Sized, ListType and StructType, which no
// program can name, and the builtin constraint Data as an interface. It is documentation only: it is never resolved, checked or
// compiled, and a test keeps it one to one with what the compiler
// implements. Source returns it, and Doc returns it parsed, once, in the
// parser's builtin mode (see parser.NewBuiltinParser), the only mode in
// which a var named by a keyword and a type declared as itself parse, so
// that an editor can map a builtin's name to its declaration through
// DocFile.Decl, for its documentation and to go to it. make install-std
// writes a copy to std/builtin/builtin.tck, and the repository keeps one
// there too, which a test keeps byte for byte; neither is ever read by the
// compiler, and the loader never loads std/builtin as a package.
//
// PLANNED.md, beside builtin.tck, keeps builtins that are planned and not
// implemented, out of builtin.tck, which claims only what the compiler
// does.
package builtin

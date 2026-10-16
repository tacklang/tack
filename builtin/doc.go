// Package builtin describes the functions Tack provides without a
// declaration, as the standard library declares them.
//
// A Tack program calls print and exit without declaring them, so something
// has to know their names and signatures. The resolver needs the names, to
// bind a call to a builtin and to stop a program from declaring a function
// with a builtin's name; the loader, to stop a package from taking one; the
// type checker the signatures, to check the number and types of arguments;
// and the C backend the C names, to lower a call. All of them read one Set,
// which is what keeps the stages from disagreeing about what the builtins
// are.
//
// # Where the builtins come from
//
// The compiler holds no list of builtins. Load reads a std/builtin
// directory, $TACK_HOME/src/std/builtin as the driver finds it: every file
// directly in it whose name ends in .tck, in the byte order of their names,
// leaving out names that start with a dot and directories, is lexed and
// parsed with the compiler's own front end, and every declaration in them
// must be a builtin:
//
//   - an extern func, in a file that is package builtin and imports
//     nothing; a var, a type, a func with a Tack body, or a method, is an
//     error;
//   - with its C name given, extern("tack_builtin_print"): one that is a C
//     identifier, not a C keyword, and starts tack_, the prefix a user's
//     extern func may not take, so no C file of a program can mean to
//     define it by declaring an extern func;
//   - not pub or lib, returning nothing, and with every parameter of a
//     basic type, such as int64 or string, but any, which a call to a
//     builtin could not pass, none mut or variadic;
//   - not named after a basic type, a generic builtin, main or _, and
//     declared once, under a C name no other builtin has.
//
// Every problem is reported, at the first stage that has any, as a
// *DiagnosticsError, diagnostics that point into the std files and render
// as a program's do. A directory that cannot be read, or holds no .tck
// file, is an error. Nothing that Load is given makes it panic.
//
// Load also lists the .c files of the directory, in name order, which
// CFiles returns: the builtins' C bodies, ordinary non-static functions
// under their C names, by convention one builtin per file named after it.
// This package does not read them; the driver compiles them and links the
// ones a program needs (see command tack).
//
// # The Set
//
// A Set is never changed once loaded, and is passed explicitly to every
// stage that needs it, so one can be shared by stages and compilations
// alike; nothing here is package-level state that a load changes. A nil
// *Set has no declared builtins. Funcs lists the declared builtins in file
// order and then declaration order, Lookup finds one by name, Called finds
// the one a resolved call calls, and Used lists those a resolved program
// calls anywhere, each once, in Funcs' order.
//
// Each builtin is a Func: a name, the types of its parameters, and its C
// name. No builtin returns a value, so a Func has no result.
//
// len, make, append, reserve, pop and remove are builtins too, but
// generic: len takes any array, view, list or string, make a list type,
// append any list and any number of values of its element type, or a
// view of them spread, reserve any list and a count, pop any list, and
// remove any list and an index, which no Tack declaration can describe, so
// they are not in std/builtin but part of the compiler, listed apart, and
// the type checker checks each itself. A list's C, the grow and free
// functions and c_allocator's, is std/builtin's all the same, in list.c,
// which the driver links for a program that uses a list. Names lists every builtin's name, generic ones included; Generic
// lists the generic ones, the same whatever std/builtin declares.
package builtin

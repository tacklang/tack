// Package typeck checks that every expression is used at the type it has.
//
// It runs after the resolver and before code generation, taking a resolved
// *ast.Program and returning the diagnostics it found. It checks every
// package of the program together, in one Checker, so the passes below
// run over every package-level declaration of every file, in program
// order, and the program has one types.Table, recorded on the Program and
// on every file. The source text notes quote is found through a
// source.Files, by the file a span points into. Running after the resolver
// means every name it sees is known to be defined. Running before code
// generation means a backend never has to decide what a badly typed program
// should emit.
//
// Checking happens in passes. The first gives every type declaration its
// struct type, with no fields yet, recorded in TypeDecl.Type, so any type
// can name any struct, before its declaration included. The second
// resolves every struct's fields, then reports every struct that contains
// itself by value, directly, through another struct or through an array of
// any length, since it would be infinitely large: once per cycle, at the
// struct it is found from, with a note for each field on the way round.
// The third checks the package-level variables, the fourth resolves every
// function's signature, recording the type each declared parameter and
// result type denotes on the tree (Param.Type and Func.ResultTypes), and
// a fifth reports every type a pub or lib declaration exposes that is
// less visible than it, and the last checks each body, or for an extern
// func, whose body is in C, its declaration: its C name must not be a C
// keyword or main. Every type can cross to C. Resolving every signature first is what lets
// a call check against a callee declared later in the file, or in a file
// after it. A call is checked against the function its callee is bound to,
// Ident.Def, not one looked up by name. A type name it
// does not know is reported, and the declaration keeps types.Invalid.
// Anything that has no type only because of an error already reported,
// such as a use of that parameter, is not reported again.
//
// A field's array length must name no variable, not even an immutable
// global holding a constant: it is a constant expression of literals,
// character literals, arithmetic and len of a string literal, and a name
// of a variable in it is reported before the length is checked. So a
// struct's fields never need a global. A global's initializer may be a
// struct literal, which needs the struct's fields, so those are resolved
// early, the first time they are needed, and only once; a field's length
// that writes a literal of its own struct needs them while they are being
// resolved, which is reported as an invalid recursive type. A global is
// checked on first need too, and one needed while it is being checked is
// reported as an initialization cycle: a global's type may name any global
// of its package, declared before or after it, in its file or another, so
// var a [len(a)]int64 needs a while checking it.
//
// Package-level variables are checked before any body, so their types
// are known in every function. A global's initializer must be a constant
// expression, one the checker can compute, and the checker records its
// value in Var.Const. It is checked in two steps. Before it is checked,
// isConstant requires it to be built only from literals, names of
// immutable globals of its own package or an imported one, operators,
// array and struct literals, fields, elements, conversions and len, with
// len taking anything that calls nothing, since an array's length is its
// type's; anything else, a call to a function or another builtin above
// all, is reported then, since no function has a signature yet. Once it
// has checked without errors, fold computes it, and an initializer it
// cannot compute, such as len of a mutable string, is reported with the
// same error, unless it names a global whose own initializer was reported
// and so has no value. fold computes a field or an element from the value of the
// struct, array or string it is part of (foldPart), at an index that is a
// known value, which checking has already found in range. A global named
// in an initializer is checked first, on first need, so the order of
// declarations does not matter, and one needed while it is being checked
// closes an initialization cycle, reported with every global on it, a
// refers to b refers to a. A mutable global is never constant, though len
// of one that is an array is. A global's known value is its Const, never
// its initializer followed again, so a cycle cannot be followed forever,
// and the known value of its field or element is taken from that Const.
//
// Visibility. A field of a struct of another package is used by name, read,
// written or named in a literal, only if it is visible from the package
// being checked: pub, or lib and in the same module, with a field's
// visibility capped by its struct's. A literal of such a struct names every
// field, so it is reported, once, when any field is hidden, with the hidden
// ones listed, and only its values are then checked; declaring a variable
// of it at its zero value, copying one and comparing two need no field by
// name and are allowed. A pub function's parameter and result types, a pub
// global's type and a pub field's type (with the field's visibility capped
// by its struct's) must be pub struct types, or arrays of them, or basic
// types; for lib, lib or pub. The rest of visibility, names of other
// packages and pub or lib where it cannot be, is the resolver's.
//
// A struct type of another package is named qualified in a diagnostic,
// lexer.Token, and a struct of the package whose code is being checked by
// its name alone. A qualified name in a message is written as it was,
// lexer.Count. Every operand is computed, the right side of && and
// || included, as in Go, so an error on either side is reported. Arithmetic
// on character literals is typed, and checked as in a function, below, so
// a step that overflows its type has been reported before the initializer
// is computed. A global without a value gets its type's zero value.
//
// Every expression is checked bottom up by expr, which records its type on
// the node and returns it. types.Invalid always means a problem has already
// been reported, so nothing built on it is reported again. An integer
// constant expression, made only of literals and operators, is computed
// exactly, in the constant package's range, and has types.UntypedInt until
// the place it is used gives it a type: the type it is assigned, passed,
// returned or compared to, or combined with in arithmetic, or int64 where
// nothing does. settle checks its value fits that type and records it. A
// character literal is a constant too, but typed: always a uint8, so an
// untyped constant beside it settles to uint8, and it widens like any uint8.
// It is a known value wherever a constant is.
// Typed integers combine through lossless widening (types.Widens): a value of
// one type can be used where a type it widens to is expected, and an
// operator on two integer types works in the one the other widens to.
//
// A divisor must be proven not zero where it is used, from facts: that a
// variable is not zero within some region, learned from a condition just
// tested. facts.go holds the rules, which are syntactic: a fact comes from
// && || ! and comparisons with a constant (one side or the other of an
// || that is true, or an && that is false, giving the union of what each
// side says about a variable both constrain), holds in an if's branches, a
// loop's body, the right side of && and ||, and the rest of a block after
// an if whose branch leaves it, and only for a region that never assigns the
// variable, which must be a parameter, a local, or an immutable global. A
// counting loop, a three-clause loop that steps a variable it declares
// from a known start by a known amount towards a bound its condition gives,
// without wrapping, also tells its body the start bounds the variable
// (counting.go). A
// switch proves what the chain of ifs it stands for does (switch.go): a
// case is if v1 || v2 ..., in a tagless switch, or if tag == v1 || tag == v2
// ..., in an expression switch, the default the final else, so a case with
// several values proves the union of what each does. Deferred code
// is checked where the defer is written, with the facts in force there,
// which still hold when it runs, at the end of the defer's block (defer.go).
// An error for a missing proof carries notes when a guard in scope says
// something about the operand (explain.go): the guard whose fact a region
// dropped, with the assignment that dropped it, and what the facts would
// still have lacked, or the guard whose fact holds but falls short, an
// index bounded only above or allowed past the end. Every note's span is
// the code it is about, so its text names no position. A fact a region
// drops is kept in force for this, marked with the assignment, and every
// query that proves something skips it; whether it would have been enough
// is found by running the proof again with every fact counted. Each fact
// records the comparison that gave it and its guard, the condition, loop,
// case or early exit whose region it holds in. Two more kinds of fact are
// kept dropped only to be explained: what an if or switch whose branch ends
// in exit would give after it, and the bound a counting loop whose step
// could wrap would count from. Other notes need no fact: a guard in scope
// comparing a field, or bounding a variable used in a larger operand; the
// element whose outer index alone v, ok := covers; the declaration of a
// mutable global, whose error suggests only the checked form, since no
// check proves it; and the operand to convert when a uint64 is compared
// with len. Notes quote source text, so New takes the file's source.
// q, ok := a / b needs no proof, and is allowed where one exists, ok then
// always being true; only a divisor known to be zero is reported. A fact is
// about a variable, never a field: a guard on p.x proves nothing about p.x,
// which needs the two-value form, or copying into a variable that the guard
// then names.
//
// A struct type is a type of its own for each declaration, whatever its
// fields; struct.go checks its uses. A literal names each field exactly
// once, with a value usable as the field's type, in any order; an element
// with no name, a key that is not a bare name, one in parentheses
// included, a name it does not have, or a name given twice is reported,
// and so are the fields left out. A field, x.f, may be read from any struct
// value, and written where x is a mutable variable, or an element or field
// of one, since a struct follows its variable as an array does. Two structs
// of one type compare with == and != when every field's type does.
//
// Array types are added to the Table as they are written, so [3]int64
// written twice is one type. An array's length must be a known value: a
// constant expression, len of an array or of a known string, an immutable
// variable holding one, a field or an element of an immutable global, a
// conversion of one, or arithmetic on those; knownValue computes it. A
// literal lists exactly one element for each of the array's, none with a
// key, and a literal inside it may leave out its type, an array's or a
// struct's, which is then the element type. An index
// must be proven in range, from the same facts as a divisor, which also
// bound a variable: a range loop's index, comparisons with known values,
// and the variable's own type. v, ok := a[i] needs no proof, and is
// allowed where one exists; only a constant index out of range is reported.
//
// A string is indexed the same way, s[i] giving a uint8, but its length is
// known only for a known string (knownString): a literal, an immutable
// variable, local or package-level, initialized with a known string, which
// nothing can assign, or a string field or element of an immutable global.
// Any other string's length is not, so the facts also
// bound a string variable's length (len(s) > 0) and bound a variable by one
// (i < len(s), or a range loop over len(s) or over s). Such a fact holds
// only in a region that assigns neither variable, and string.go uses it to
// prove an index. A constant index into a known string is checked at
// compile time, a negative one
// is always out of range, and a string's bytes are never assigned. Two
// strings compare with all six comparison operators, by their bytes; a
// comparison of strings gives no fact, and is computed only in a
// package-level variable's initializer, where its operands are constant:
// in a function it is left to run time, even of two literals. The same
// holds for == and != on arrays and structs holding strings; on ones that
// hold none, and on bools, they are computed wherever their operands are
// known.
//
// The third pass checks each function body:
//
//   - a call passes the number of arguments its callee declares, each usable
//     as the declared type, whether the callee is a Tack function or a
//     builtin;
//   - a call to a function that returns nothing, or several values, cannot
//     be used as a value;
//   - every operand of an arithmetic operator is an integer, the two having
//     a common type, and the operand of - a signed integer; every operand of
//     && || and ! is a bool; a binary expression whose operator is not
//     defined on its operands is one error, at the first such operand;
//   - typed arithmetic whose operands are known values, as knownValue
//     computes them, gives a value of its type, or the step that does not
//     is reported as overflowing it, here as at package level; so is - on
//     a known smallest value of a signed type. The expression is then
//     Invalid, so nothing around it is reported again. Arithmetic on a
//     value not known, a mutable variable's or a parameter's, wraps at run
//     time and is not checked;
//   - both operands of a comparison have one type, after widening, which
//     supports the operator: an integer or a string supports all six, a
//     bool, an array or a struct only == and !=. A mismatch, or an
//     unsupported operator, is one error covering the whole comparison;
//   - a divisor is proven not zero, or the division is bound with ok, and
//     the quotient of two untyped constants bound with ok settles to the
//     type of the variable it is assigned, or int64 where there is none;
//   - a conversion T(x) converts an integer to an integer type, and only
//     where it cannot fail: a constant or a known value, as knownValue
//     computes it, that fits T, or a type that widens to T. A constant or
//     known value that does not fit T is reported as overflowing it. Any
//     other is v, ok := T(x), which is also allowed where the conversion
//     cannot fail, and reports a constant or known value that does not fit
//     T as T(x) does; no fact makes a conversion compile;
//   - a variable's value is usable as its declared type, and a variable
//     declared without a type takes its value's;
//   - only a mutable variable may be declared without a value, and only a
//     mutable variable, or an element or field of one, may be assigned to;
//     parameters never may;
//   - an assigned value is usable as the variable's type, and a compound
//     assignment such as += has an integer target and a value usable as its
//     type; ++ and -- need a mutable integer variable;
//   - a return carries one value for each result the function declares, each
//     usable as its result's type, and a wrong count is one error;
//   - two names bound to a value that holds a division, a remainder or an
//     index outside any call, but is more than it, as in q, ok := a / b + 1,
//     are one error saying the checked form must be the whole value, which
//     names the first such operation and what to compute from its result;
//     that operation is then checked as bound with ok;
//   - a, b := f() binds a call with exactly that many results, each variable
//     taking its result's type, or a two-value conversion; a, b = f()
//     assigns them to mutable variables they widen to, each named once, with
//     _ discarding a value of any type and allowed more than once; and a call
//     with several results is never used as one value;
//   - an if or loop condition is a bool, and a loop's init and post
//     statements are checked like any other statement;
//   - a switch's tag, an untyped constant being an int64, compares with each
//     case value under the rules of ==, the value settling to the tag's
//     type if it is an untyped constant, and in a tagless switch each case
//     value is a bool condition; two case values with the same known
//     integer value, the same bool constant value, or two equal string
//     literals, are a duplicate, reported with a note at the first;
//   - a range loop ranges over an integer, whose one variable takes its type,
//     or over an array or a string, whose index is an int64 and whose
//     element is the array's element type, or a string's byte, a uint8;
//     both variables are immutable, and two over an integer are an error;
//   - break and continue are inside a loop, and in deferred code inside a
//     loop that is itself inside the deferred code; a return is never in
//     deferred code, since nothing may leave it, and neither is a defer, at
//     any depth, reported at its keyword, but only the outermost: a defer
//     inside one already reported is not reported again;
//   - a function that declares a result ends in a terminating statement: a
//     return, an if with an else whose branches both terminate, a switch
//     with a default whose cases all terminate, or a loop with no condition
//     and no break that leaves it (a break in a nested loop leaves only that
//     one, and one in a switch leaves the loop around it). The rule is syntactic, as in Go, so a call
//     that never comes back, like exit, does not satisfy it.
//
// Before any of this, Check creates the file's types.Table and records it on
// File.Types, and every diagnostic names a type through it, so a type is
// always named by the compilation that gave it its ID. Later stages read the
// same Table from the file.
//
// Checking continues past the first problem, so one run reports all of them,
// in source order, each once. An error is not reported when it follows from
// one already reported in the same file: nothing built on a declaration or
// an operand that failed, which has types.Invalid, is reported, an operator
// is reported once per expression, at the first operand it is not defined
// on, however many it is not defined on, and a condition already reported
// still gives the facts it states, so the proof it was written to give is
// not reported missing. A Checker is single-use: create one with New and call Check
// once.
package typeck

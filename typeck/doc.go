// Package typeck checks that every expression is used at the type it has.
//
// It runs after the resolver and before code generation, taking a resolved
// *ast.File and returning the diagnostics it found. Running after the resolver
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
// the last checks each body. Resolving every signature first is what lets
// a call check against a callee declared later in the file. A type name it
// does not know is reported, and the declaration keeps types.Invalid.
// Anything that has no type only because of an error already reported,
// such as a use of that parameter, is not reported again.
//
// A field's array length may name a package-level constant, and a global's
// initializer may be a struct literal, which needs the struct's fields, so
// a struct's fields and a global are each checked early, the first time the
// other needs them, and only once. Needing one again while it is being
// checked means it depends on itself, which is reported: a struct as an
// invalid recursive type, a global as an initialization cycle.
//
// Package-level variables are checked before any body, so their types
// are known in every function. A global's initializer must be a constant
// expression, made only of literals and operators, and the checker records
// its value in Var.Const. Every operand is computed, the right side of && and
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
// && || ! and comparisons with a constant, holds in an if's branches, a
// loop's body, the right side of && and ||, and the rest of a block after
// an if whose branch leaves it, and only for a region that never assigns the
// variable, which must be a parameter, a local, or an immutable global. A
// counting loop, a three-clause loop that steps a variable it declares
// from a known start by a known amount towards a bound its condition gives,
// without wrapping, also tells its body the start bounds the variable
// (counting.go). A
// switch proves what the chain of ifs it stands for does (switch.go): a
// case is if v1 || v2 ..., in a tagless switch, or if tag == v1 || tag == v2
// ..., in an expression switch, the default the final else. Deferred code
// is checked where the defer is written, with the facts in force there,
// which still hold when it runs, at the end of the defer's block (defer.go).
// q, ok := a / b needs no proof, and is an error where one exists. A fact is
// about a variable, never a field: a guard on p.x proves nothing about p.x,
// which needs the two-value form, or copying into a variable that the guard
// then names.
//
// A struct type is a type of its own for each declaration, whatever its
// fields; struct.go checks its uses. A literal names each field exactly
// once, with a value usable as the field's type, in any order; an element
// with no name, a name it does not have, or a name given twice is reported,
// and so are the fields left out. A field, x.f, may be read from any struct
// value, and written where x is a mutable variable, or an element or field
// of one, since a struct follows its variable as an array does. Two structs
// of one type compare with == and != when every field's type does.
//
// Array types are added to the Table as they are written, so [3]int64
// written twice is one type. An array's length must be a constant: a
// constant expression, len of an array or of a known string, an immutable
// variable holding one, or arithmetic on those; knownValue computes it. A
// literal lists exactly one element for each of the array's, none with a
// key, and a literal inside it may leave out its type, an array's or a
// struct's, which is then the element type. An index
// must be proven in range, from the same facts as a divisor, which also
// bound a variable: a range loop's index, comparisons with known values,
// and the variable's own type. v, ok := a[i] needs no proof, and is an
// error where one exists.
//
// A string is indexed the same way, s[i] giving a uint8, but its length is
// known only for a known string (knownString): a literal, or an immutable
// variable, local or package-level, initialized with a known string, which
// nothing can assign. Any other string's length is not, so the facts also
// bound a string variable's length (len(s) > 0) and bound a variable by one
// (i < len(s), or a range loop over len(s) or over s). Such a fact holds
// only in a region that assigns neither variable, and string.go uses it to
// prove an index. A constant index into a known string is checked at
// compile time, a negative one
// is always out of range, and a string's bytes are never assigned. Two
// strings compare with all six comparison operators, by their bytes; a
// comparison of strings is never folded, even of two literals, and gives no
// fact.
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
//     && || and ! is a bool;
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
//   - a divisor is proven not zero, or the division is bound with ok;
//   - a conversion T(x) converts an integer to an integer type, and only
//     where it cannot fail: a constant or a known value, as knownValue
//     computes it, that fits T, or a type that widens to T. A constant or
//     known value that does not fit T is reported as overflowing it. Any
//     other is v, ok := T(x), which is an error where the conversion cannot
//     fail; no fact makes a conversion compile;
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
//     integer value, or two equal string literals, are a duplicate, reported
//     with a note at the first;
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
// in source order. A Checker is single-use: create one with New and call Check
// once.
package typeck

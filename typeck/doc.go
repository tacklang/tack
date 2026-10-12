// Package typeck checks that every expression is used at the type it has.
//
// It runs after the resolver and before code generation, taking a resolved
// *ast.File and returning the diagnostics it found. Running after the resolver
// means every name it sees is known to be defined. Running before code
// generation means a backend never has to decide what a badly typed program
// should emit.
//
// Checking happens in three passes. The first resolves every function's
// signature, recording the type each declared parameter and result type
// denotes on the tree (Param.Type and Func.ResultTypes). Doing this for every
// function first is what lets a call check against a callee declared later in
// the file. A type name it does not know is reported, and the declaration
// keeps types.Invalid. Anything that has no type only because of an error
// already reported, such as a use of that parameter, is not reported again.
//
// Package-level variables are checked next, before any body, so their types
// are known in every function. A global's initializer must be a constant
// expression, made only of literals and operators, and the checker records
// its value in Var.Const. Every operand is computed, the right side of && and
// || included, as in Go, so an error on either side is reported. Arithmetic
// on character literals is typed, so each step must give a value of its
// type, uint8 unless it widened, or the initializer is reported as
// overflowing it. A global without a value gets its type's zero value.
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
// variable, which must be a parameter, a local, or an immutable global.
// q, ok := a / b needs no proof, and is an error where one exists.
//
// Array types are added to the Table as they are written, so [3]int64
// written twice is one type. An array's length must be a constant: a
// constant expression, len of an array or a string literal, an immutable
// variable holding one, or arithmetic on those; knownValue computes it. A
// literal lists exactly one element for each of the array's, and an index
// must be proven in range, from the same facts as a divisor, which also
// bound a variable: a range loop's variable, comparisons with known values,
// and the variable's own type. v, ok := a[i] needs no proof, and is an
// error where one exists.
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
//   - both operands of a comparison have one type, after widening, which
//     supports the operator: an integer supports all six, a bool only == and
//     !=, and a string none. A mismatch, or an unsupported operator, is one
//     error covering the whole comparison;
//   - a divisor is proven not zero, or the division is bound with ok;
//   - a conversion T(x) converts an integer to an integer type, and only
//     where it cannot fail: a constant that fits T, or a type that widens to
//     T. Any other is v, ok := T(x), which is an error where the conversion
//     cannot fail;
//   - a variable's value is usable as its declared type, and a variable
//     declared without a type takes its value's;
//   - only a mutable variable may be declared without a value, and only a
//     mutable variable may be assigned to; parameters never may;
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
//   - a range loop's count is an integer, and its variable is an immutable
//     variable of the count's type;
//   - break and continue are inside a loop;
//   - a function that declares a result ends in a terminating statement: a
//     return, an if with an else whose branches both terminate, or a loop
//     with no condition and no break that leaves it (a break in a nested
//     loop leaves only that one). The rule is syntactic, as in Go, so a call
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

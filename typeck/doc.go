// Package typeck checks that every expression is used at the type it has.
//
// It runs after the resolver and before code generation, taking a resolved
// *ast.File and returning the diagnostics it found. Running after the resolver
// means every name it sees is known to be defined. Running before code
// generation means a backend never has to decide what a badly typed program
// should emit.
//
// Checking happens in three passes. The first resolves every function's
// signature, recording the Kind each declared parameter and result type
// denotes on the tree (Param.Kind and Func.ResultKind). Doing this for every
// function first is what lets a call check against a callee declared later in
// the file. A type name it does not know is reported, and the declaration
// keeps types.Invalid. Anything that has no type only because of an error
// already reported, such as a use of that parameter, is not reported again.
//
// Package-level variables are checked next, before any body, so their kinds
// are known in every function. A global's initializer must be a constant
// expression, made only of literals and operators. The checker computes its
// value into Var.Const with Go's integer arithmetic (division truncates, and
// a remainder takes the dividend's sign), comparing bools as well as ints
// and applying && || and !. Every operand is computed, the right side of &&
// and || included, as in Go, so a result that overflows int or a division by
// zero is reported wherever it is. A global
// without a value gets its type's zero value.
//
// The third pass checks each function body:
//
//   - a call passes the number of arguments its callee declares, each of the
//     declared type, whether the callee is a Tack function or a builtin;
//   - a call to a function that returns nothing cannot be used as a value;
//   - every operand of an arithmetic operator is an int, and every operand of
//     && || and ! is a bool;
//   - both operands of a comparison have one type, which supports the
//     operator: an int supports all six, a bool only == and !=, and a string
//     none. A mismatch, or an unsupported operator, is one error covering the
//     whole comparison;
//   - a variable's value has its declared type, and a variable declared
//     without a type takes its value's;
//   - only a mutable variable may be declared without a value, and only a
//     mutable variable may be assigned to; parameters never may;
//   - an assigned value has the variable's type;
//   - a return carries a value exactly when the function declares a result,
//     and the value has the result's type;
//   - an if or loop condition is a bool;
//   - break and continue are inside a loop;
//   - a function that declares a result ends in a terminating statement: a
//     return, an if with an else whose branches both terminate, or a loop
//     with no condition and no break that leaves it (a break in a nested
//     loop leaves only that one). The rule is syntactic, as in Go, so a call
//     that never comes back, like exit, does not satisfy it.
//
// Checking continues past the first problem, so one run reports all of them,
// in source order. A Checker is single-use: create one with New and call Check
// once.
package typeck

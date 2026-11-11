// Package typeck checks that every expression is used at the type it has.
//
// It runs after the resolver and before code generation, taking a resolved
// *ast.Program and returning the diagnostics it found. It checks every
// package of the program together, in one Checker, so the passes below
// run over every package-level declaration of every file, in program
// order, and the program has one types.Table, recorded on the Program and
// on every file. The source text notes quote is found through a
// source.Files, by the file a span points into. Running after the resolver
// means every name it sees is bound, to a declaration or, after an error,
// to ast.Unbound. It runs whatever the stages before it reported (see
// After errors below). Running before code generation means a backend
// never has to decide what a badly typed program should emit.
//
// Checking happens in passes. The first gives every type declaration its
// struct or union type, with no fields or cases yet, recorded in
// TypeDecl.Type, so any type can name any struct or union, before its
// declaration included. The second resolves every struct's fields and
// every union's cases, then reports every struct or union that contains
// itself by value, directly, through another struct or union or through
// an array of any length, since it would be infinitely large: once per
// cycle, at the type it is found from, with a note for each field or case
// on the way round. Every type's size can be known from then on, so the
// second pass also checks the size of every array type met so far and of
// every struct and union type (see size limits below).
// The third checks the package-level variables, the fourth resolves every
// function's signature, recording the type each declared parameter and
// result type denotes on the tree (Param.Type and Func.ResultTypes), and
// checks its contract, the facts it ensures (ensures.go), and
// a fifth reports every type a pub or lib declaration exposes that is
// less visible than it, and the last checks each body, or for an extern
// func, whose body is in C, its declaration: its C name must be a C
// identifier, not a C keyword, not main, and, outside the standard
// library, whose packages' paths are std or start std/, not start tack_.
// Every type can cross to C but a list, or a struct or array holding one,
// which crosses only as a mut parameter or a view, and not even so when
// its elements hold lists. A body that checked
// without an error has its moves checked next (move.go). Before the bodies, every extern func of the
// program whose C name an earlier one has is reported, with a note there. Resolving every signature first is what lets
// a call check against a callee declared later in the file, or in a file
// after it. A call is checked against the function its callee is bound to,
// Ident.Def, not one looked up by name. A type name it
// does not know is reported, and the declaration keeps types.Invalid.
// Anything that has no type only because of an error already reported,
// such as a use of that parameter, is not reported again.
//
// Size limits. No value may hold more than 2^31 bytes as the C backend
// lays it out, padding and lowering included (types.Table.Size, for the
// 64-bit targets the backend builds for), and all of a program's globals
// together no more than 2^30, so code and static data stay within what
// clang's default code models link, x86-64's 2 GiB reach included. A type too large is reported
// once, where it is made too large: at the first array type expression
// [n]T that denotes it, or at a struct's or union's name, and only when
// none of its parts is too large itself, since that part was reported
// where it was made; no use of it, and no type holding it, is reported
// again. A first use in a damaged body whose report is dropped does not
// count: the next use is reported instead (see invalid.go). The size of
// an array type met before every struct's fields are resolved is checked
// once they are; any met later, at once. Once every
// global is checked, the globals are added up in program order, package
// by package as the loader lists them, each after the packages it
// imports and the main package last, each package's files in the byte
// order of their names, source order within a file; the first global
// that takes the total past the limit is reported, once, and a global
// whose type is too large is not counted. Only Tack globals count: the
// standard library's are, its C files' statics are not. The C objects the
// backend makes of several values at once, a function's results and a
// variadic call's arguments, are not values and are not checked. See
// size.go.
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
// A local's known value, and an arithmetic operator's once it has its
// type, are remembered (knownVar, knownArithmetic), so a chain of
// operators, each asking about the one below, is computed once.
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
// It is a known value wherever a constant is. A float constant expression,
// one with a float literal among its literals, is computed exactly, as a
// constant.Float, and has types.UntypedFloat until the place it is used
// gives it a float type, which settleFloat checks it rounds to a finite
// value of, or float64 where nothing does; an untyped integer constant
// may settle to a float type too, but an untyped float constant never to
// an integer one. Typed floats combine only with the same float type,
// under + - * and / (floatArithmetic) and the six comparisons, with no
// widening and no proof of a non-zero divisor: a float division gives an
// infinity or NaN. A float is never a subject (see subjectKey, keyOf) and
// never a known value (knownValue), so floats carry no facts, cannot be an
// index or a length, and cannot be a map key, alone or held in a struct,
// an array or a union (holdsFloat). An integer converts to a float, and a
// float to the other float type, in the plain form, which never fails
// (floatConversion); a float converts to an integer only in the checked
// form, v, ok := T(f), since no fact can prove it in range, the backend
// testing the range at run time. A constant converts exactly or is
// reported: to a float it must round to a finite value, and a float
// constant to an integer must be one, of the integer type
// (constConversion), and its value is recorded on the call. A float, or
// a view of one, can be passed as any, and a float crosses to C, as any
// other basic type does; printf takes no float yet (verbProblem).
// Typed integers combine through lossless widening (types.Widens): a value of
// one type can be used where a type it widens to is expected, and an
// arithmetic operator on two integer types works in the one the other
// widens to. A comparison needs no common type: two integers of any types
// compare by their values.
//
// The target-width types, unsafe_size_t and unsafe_uintptr_t, are as wide
// as the target makes them, from 32 to 64 bits, and every rule about them
// holds at any width in that range (types.TargetWidth). Widening into and
// out of them is types.Widens'. A constant of one must lie between 0 and
// types.PortableMax, the most every target holds (settle), and so must
// every step of a constant expression, made of literals, conversions and
// operators (isConstantExpr), or of a package-level initializer, which is
// reported as an overflow with a note that the width is unknown. Anywhere
// else a known value past types.PortableMax is simply not known, since
// whether it wraps depends on the target (fitting), and is computed at run
// time; one below 0 wraps on every target and is an overflow error, as for
// any unsigned type (knownOverflow). T(x) of a known value past
// types.PortableMax but within 64 bits is reported as a conversion that
// can fail, and checked at run time in the two-value form (fitsSomewhere);
// facts never prove it, since they do not change a known value.
// For proofs a target-width type is bounded by its widest values, 0 to the
// largest 64-bit unsigned value (typeBounds), but a counting loop's step
// must stay at or below types.PortableMax not to wrap (stepBounds). An
// index may be of a target-width type, and is proven, or checked, as a
// uint64 one is: by its value, against a length compared by value
// (indexType).
//
// A divisor must be proven not zero where it is used, from facts: that a
// variable is not zero within some region, learned from a condition just
// tested. facts.go holds the rules, which are syntactic: a fact comes from
// && || ! and comparisons with a known value, of the variable's type or
// any other integer type, which bound the variable by value (one side or
// the other of an || that is true, or an && that is false, giving the union of what each
// side says about a variable both constrain), holds in an if's branches, a
// loop's body, the right side of && and ||, and the rest of a block after
// an if whose branch leaves it, and only up to the first write to the
// variable in that region that can reach the use; the variable must be a
// parameter, a local, or an immutable global. A write reaches a use when it
// takes effect before it, a call's mut argument at the call's end and an
// assignment's target at the statement's end, so the call or assignment a
// fact is for does not end it; when it is in a loop inside the region that
// holds the use too; when the use is in deferred code and the write
// outside it; when the use is in a switch's default written before a
// case whose value writes; or when the use is in an assignment's target
// and the write anywhere in that assignment, its value or another
// target, so l[i] = bump(mut i) and i, l[i] = two() end the proof of i
// (see storing). A write in a branch or a case in the region that leaves
// (one ending in a switch only when the switch has a default, whatever
// the order checking runs in, and a case ending in a fallthrough when
// the case it enters leaves; see leavesBody) reaches no use outside its
// if or switch, but for one its break or
// continue, or that of a case it falls into, can reach from the loop in
// the region that it lands in, and
// for deferred code, which a return still runs (see keep and
// leftBefore). The regions of the facts a block's statements give the
// rest of it share that rest, whose writes are found in one walk of the
// block, each region's by a binary search (region.go), so n early exits
// do not walk the rest of their block n times. Every write in the region
// is kept with the fact, and judged at c.at, where checking has reached
// (see reaches);
// a fact with more than a few such writes is judged through an index of
// the positions each reaches, built once, so judging n writes at n uses
// takes about n log n steps, not n times n (reach.go). A
// counting loop's start and a range loop's bound are not proven again on
// a later pass, so any write in their region ends them. A successful
// append, its error proven 0, proves its list's length at least the count
// it added, until a write that can reach the use (appended.go). The
// region is everything that can run between the comparison that gave the
// fact and the use: the rest of its condition (factsFrom drops a left
// side's fact that the right side assigns, and the right side of && and
// || is the region of the left side's facts; the facts of each && or ||
// are found once, as it is checked, and joined by the operator above it,
// so a long chain is not walked again at every operator), the branch that falls
// through past an early exit, a later value of a case and every later
// case's values, every value tried since a switch's tag was evaluated, for
// a fact about the tag, and, for a counting loop's start, the loop's
// condition, which runs again before every pass. A
// call passing the variable, or a part of it, as mut counts as assigning
// it, and its note names the call; a mut parameter is a variable like any
// other, which only its own function can assign while it runs. A
// counting loop, a three-clause loop that steps a variable it declares
// from a known start by a known amount towards a bound its condition gives,
// without wrapping, also tells its body the start bounds the variable
// (counting.go). A loop also keeps a bound on a local or a parameter of
// an integer type declared before it and written in it, put in force for
// the whole loop with no ending (invariant.go): a known value it is at or
// above or at or below, or a difference with another node, that holds at
// the loop's start, by a fact or the known value the variable was last
// given (recordStart), that the loop's init does not write, and that
// every write to it in the loop's condition, post and body, x++, x--,
// x += e, x -= e or x = e, keeps, the value it stores proven within the
// bound, unwrapped, by the facts in force at the write and the bound
// itself (keeps); nothing in the loop may write the other node. Whether
// a write keeps a bound is known only once checking reaches it, so a body
// with a loop that writes a variable declared before it is first checked
// as a probe (findInvariants, a discarded pass like findCallees'), which
// judges every write against the bounds each loop around it may keep and
// records those each loop keeps for the check that counts. The probe's
// facts are those of a single check, each true where it holds, so a bound
// it finds kept holds on every run. A
// switch proves what the chain of ifs it stands for does (switch.go): a
// case is if v1 || v2 ..., in a tagless switch, or if tag == v1 || tag == v2
// ..., in an expression switch, the default the final else, so a case with
// several values proves the union of what each does. The code after a
// switch, as after an else-if chain, gets what each link gives as an early
// exit while every link before it leaves (switchAfterFacts for the first,
// chainFacts for the rest): a case that leaves, its values not matching,
// and the first that does not, when every case after it leaves, its
// values matching. A case's body a
// fallthrough enters is reached by matching and by the fallthrough, and
// gets what both give, joined as || joins its sides (joinFacts): matching
// gives the facts in force where the switch starts, its values' and the
// earlier cases' not matching, and the fallthrough the facts in force at
// it, less those a defer of the falling case writes the variable of,
// since its defers run before the next body. A default written before
// other cases, and every case it falls into, runs after every case's
// values, so each is checked as a late default is, a write in any value
// reaching it (lateBodies). Such bodies are checked
// after every case's values, in source order, each after the case that
// falls into it (checkFallenInto), with none of the facts of the cases
// tried before them not matching in force, the proof-only ones included,
// and their facts are a join no note describes (guardFallthrough). A
// fallthrough must be the last statement of a case of an expression
// switch other than its last, outside any defer in the case
// (checkFallthrough). A case ending in one leaves when the case it enters
// does (leaves), so it adds no path to the code after the switch, and,
// as in Go, counts as terminating there (terminates). Deferred code
// is checked where the defer is written, with the facts in force there,
// which still hold when it runs, at the end of the defer's block (defer.go).
// An error for a missing proof carries notes when a guard in scope says
// something about the operand (explain.go): the guard whose fact a region
// dropped, with the assignment that dropped it, and what the facts would
// still have lacked, or the guard whose fact holds but falls short, an
// index bounded only above or allowed past the end, a conversion's
// operand bounded on one side only or allowed past its type's range
// (conversionProof), and a note on the
// relations in force when they fall short (relationNotes). An unsigned index is
// never negative, so a note never explains a lower bound on one, whether
// missing or dropped, nor on an index the facts in force keep at or above
// 0, a bound a loop keeps included; and a dropped fact whose every bound
// a bound a loop keeps still gives is not explained at all, since what it
// said still holds (see keptBounds). No note says what the facts allow a
// subject that, earlier in its function, a relation tied to another
// variable or the sides of a || constrained (noteRelated), since notes
// follow neither, nor one a stored value's fact holds about (heldValue);
// and one that names a value the facts rule out, by its bounds or by a
// !=, is left out (rulesOut), a note on what they would allow being
// checked with the dropped facts counted. The length of a string, view or
// list subject, len(s), is explained as a subject is, its facts being
// under the key of s (noteKey), named len(s) (noteName) and never
// negative (ownInterval); a length a fact ties to another value, a
// relation, a bound i < len(s) or a store n := len(s), gets no note on
// what the facts allow (noteRelated). Where no value meets the facts, no
// note says what they allow (interval.empty), and a link note names no
// check the relations in force show can never hold (refutes). Every note's span is the code it is about, so its
// text names no position. A fact a write
// ends is kept in force for this, marked where it is asked about with
// the write that ends it there, and every query that proves something
// skips it; whether it would have been enough
// is found by running the proof again with every fact counted. Each fact
// records the comparison that gave it and its guard, the condition, loop,
// case or early exit whose region it holds in. One more kind of fact is
// kept dropped only to be explained: the bound a counting loop whose step
// could wrap would count from. A branch, a block or a case ending in a
// call to the builtin exit leaves, as one ending in return, break or
// continue does, since exit never returns (leaves), so an early exit may
// end in one, and so does one ending in an unreachable statement, which
// is proven never reached. Whether a block leaves, and whether a
// statement terminates, is remembered for each if, switch, fallthrough
// and loop once worked out (leavememo.go), since each link of an else if
// chain is asked about at every link before it; an answer of no is
// worked out again once a switch on a union is found to name every case
// (Switch.Exhaustive), the one thing besides the syntax either reads.
//
// An unreachable statement must be proven unreachable (unreachable.go): a
// block around it has already left or terminated (c.dead), the facts in
// force there with each subject's type bounds leave some subject no value,
// every side of a true || or a false && on the path to it, taken alone,
// does so with the facts there, or the relations in force hold a cycle of
// negative weight. Two kinds of fact count for that proof alone, kept
// apart from the facts in force so no other proof changes (proofOnly): a
// value ruled out, b != k for any k, which a switch's cases and an if
// chain's false == give and only coverage asks about; and the sides of a
// true || or a false &&, beside the hull of them union gives. Each is
// put in force with its region as any fact is, and the writes that could
// end it are found only when an unreachable asks. A failed proof gets
// notes on a guard comparing a field of a mutable variable or a mutable
// global, and on the guard and the write of a fact a write ended that
// would have proven it alone; no other note. The checker records each
// unreachable's file and line on it, for the abort it lowers to. Other notes need no fact: a guard in scope
// comparing a field of a mutable variable, or bounding a variable used in a larger operand; the
// element whose outer index alone v, ok := covers; the declaration of a
// mutable global, whose error suggests only the checked form, since no
// check proves it. Notes quote source text, so New takes the program's
// source.Files.
// q, ok := a / b needs no proof, and is allowed where one exists, ok then
// always being true; only a divisor known to be zero, and the smallest
// value divided by -1 on known values, are reported. A fact is
// about a subject (subject.go): a variable, or a path below an immutable
// variable or parameter, a field, p.x, an element at a known index or an
// immutable name's, p.a[1] or p.a[k], or a chain of those, which nothing
// can change while it is in scope. A guard on a field of a mutable
// variable or a mut parameter proves nothing about it, and its note says
// so; the operation needs the two-value form, or copying the field into a
// variable that the guard then names.
//
// A parameter written mut p T is a mutable reference to the caller's
// variable for the duration of the call, checked in mut.go. Inside the
// function it is a mutable variable: it, its fields and its elements may
// be assigned. A call passes it mut x, and the checker requires x to be a
// place the caller may change: a mutable local, a mut parameter, a field
// of one, or an element of one at an index proven in range, nested in any
// combination. An immutable variable, a plain parameter, an immutable or
// mutable global, a string's byte and anything that is not a place are
// reported, and so is mut written for a plain parameter, left out for a
// mut one, or written anywhere but as a call's argument. mut left out, by
// an argument or a receiver, suggests writing it only when the argument
// could then be passed as mut, and otherwise says why it cannot, with the
// fix that makes it a place when there is one, as mut written gets them
// (mutRootFix, and stringByte for a string's byte): declaring a local,
// or a function's parameter in its signature, with mut, copying a global
// or a closure's parameter, whose signature the call fixes, into a
// mutable local, or moving an own parameter into a mutable variable; a
// string's byte, a view parameter, the variables of a range loop and of
// a type switch, and the binding of a union's payload, recorded in
// bindings, never can be. The place's type
// must be the parameter's exactly: no widening. No reference can be
// stored or returned, since nothing but a call's argument may be mut x.
// No two mut arguments of one call may overlap (exclusive.go): places
// are compared as paths, a variable and its fields and elements, so two
// different fields never overlap, the same place twice or a place and a
// part of it always do, and two elements of one array overlap unless
// their indexes are known values that differ, or the facts in force keep
// them apart. A plain argument that reads a place passed as mut is
// allowed: it is evaluated before the call, and passed as a copy.
// An extern func may take a mut parameter, which C gets as a pointer.
//
// A method, checked in method.go, is a function whose first parameter is
// its receiver, a struct or a union of its package, so its signature and
// body are checked as any function's, and a diagnostic names it by its
// receiver's type and its name, Builder.write. A method call, x.m(args),
// checks its receiver first, whose type gives the method, found among
// those the resolver recorded on the type, of any package
// (TypeDecl.Methods), and recorded in Call.Method: a type with no such
// method, a field called, and a method another package cannot see, its
// visibility capped by its type's as a field's is, are reported; a
// union's case is no method. A union built in place, Expr.eof, is no
// place, so it is never a mut receiver. The receiver is then checked as
// an argument to the method's first parameter: a plain one as a value, a
// temporary included, and mut x as a mut argument, whose place is checked
// as one once the method says it is wanted; mut left out for a mut
// receiver, or written for a plain one, is reported. The arguments
// follow, as for a function, and the receiver is one of the call's mut
// arguments for exclusivity, and of its operands for list borrows and for
// the moves of move.go, a plain receiver borrowing what it is. A method
// named without a call, b.write, is reported, as there are no bound
// method values. A method named through its type, Pair.first, is a
// method expression, checked in methodexpr.go: a function value of the
// method's signature, the receiver its first parameter, recorded on the
// selector (Selector.Method and Selector.Type), or, called,
// Pair.first(p), a call of the method by name, recorded in Call.Method,
// whose arguments are checked against every parameter, the receiver
// first, with no receiver of its own (ast.Call.Receiver). The type must
// be a struct or a union, not a basic type, an interface or a type
// parameter, with a method of that name the package may see; a name
// through a union that is no case is a method expression, and one that
// is neither is reported as missing from both. A method expression is no
// place, and a method used only as one is used, its parameters exempt
// from the unused warning as a function value's are. Whether a binding of a call's results ties them to an error is
// collected before a body is checked (errors.go), but a method call's
// method is known only once its receiver is, and what a function value
// returns only once the value is, so a body binding the results of
// either is checked twice: once to find every callee, and then with
// every tie known. The first pass leaves no trace but what it exists to
// find (findCallees, in firstpass.go): what it reports is dropped, and
// every record of what has been reported or found, a name reported
// alongside an own of it or a type checked for its size among them, is
// set aside while it runs and put back after it, so the second pass
// reports exactly what checking the body once would. A binding of
// nothing but _ of a method call that returns no error is reported here,
// as the resolver reports one of a function's call. A pub or lib method exposes
// only types as visible as it, capped by its type's, its receiver's
// included, as a function does.
//
// A parameter written v []T is a read-only view of elements of type T, the
// second kind of reference a parameter can be, checked in view.go. Only a
// parameter, or a function's only result (below), may have a view type:
// one written anywhere else, a local's, a field's, a global's or an
// array's element, and a view of views, are reported, and so is a
// variable declared from a view's value, x := v, and a view parameter
// written mut. It takes an array of exactly
// its element type that is a place, a variable or a parameter or a field
// or element of one, with no widening, so the callee reads the caller's
// array; an array that is not a place, a call's result or a literal, would
// need a temporary, and is reported. It takes a view of its own type,
// passed on, and the bytes of a string through the conversion []uint8(s),
// which is allowed nowhere but as such an argument and of nothing but a
// string; a string passed without it is reported with the conversion to
// write. A view's len is an int64 that is never a known value; an element,
// v[i], is proven as a string parameter's byte is, against len(v), whose
// facts hold for the whole function, since nothing can assign the view,
// an element of it or pass either as mut, all reported, and the caller
// passes a copy of an array the call could change (codegen/c); v, ok :=
// v[i] needs no proof; and a range over a view gives an int64 index, below
// len(v), and the element. A path below a view parameter, v[k].d, is a
// subject facts can be about, like one below any immutable parameter. A
// view has no ==, which a switch on one needs too, and no conversion, and
// a view argument alongside a mut argument of an overlapping place is
// allowed, as a plain argument is.
//
// A function's or a method's only result may be a view, checked in
// viewresult.go; an extern func's, and one among several results, are
// reported. A return gives it a view parameter, or a list or an array of
// exactly its element type, rooted in a parameter that is not mut, an
// array only through an element of a list or a view, a slice of one, or a
// call returning a view whose arguments are such places; a local, a
// global, a mut parameter, a temporary, a string's bytes, packed variadic
// values and an array the function holds its own copy of are reported. A
// union pattern's binding and a range loop's element variable over owned
// elements refer in place into what they match or range over
// (unionBindings, rangeBindings), and are rooted where that is
// (referent), so a view of one is returned when a view of that could be.
// The view a call returns is second-class: the call is allowed only where
// viewCall marks it, an argument to a view parameter, len's, what a slice
// slices, what an index indexes, what a range ranges over, or a return's
// value in a function returning a view, and anywhere else it is
// reported, but as a statement, which drops the view. Its index is never proven, since no
// fact is about a call. It may point into every argument of its call
// that is not mut and holds data, which it borrows: a call passing it
// alongside a mut argument that may overlap one is reported
// (exclusive.go), and so is an index or a slice of it whose own index or
// bounds pass one, and a range over it cannot change one (move.go).
//
// A variadic parameter, args ...T, the last of its function's, is a view,
// []T, checked in variadic.go: its type is the view of its element type,
// so inside its function it follows every rule above, and it may not be
// mut or have a view as its element. A call to a variadic function passes
// one argument per fixed parameter, then any number of values, each
// checked as a plain argument to a T parameter is, or a spread, f(a,
// v...), alone, checked as an argument to a []T parameter is. Fewer
// arguments than fixed parameters, values beside a spread, and a spread
// to anything not variadic, a function, a builtin, len or a conversion,
// are reported. The values are plain arguments for exclusivity, so one
// overlapping a mut argument is a copy, and nothing about how many there
// are is known inside the callee, which is checked once for every call.
//
// any, checked in any.go, is a reference to a value whose type is known
// only at run time, and second-class like a view, though never a result:
// only a parameter's type,
// a f(a any), and the element type of a variadic or a view parameter,
// ...any and []any, may be written as it; written anywhere else, a local's
// type, a field's, a global's, a result's or an array element's, it is
// reported. An immutable local whose type is left out takes any from its
// value, x := a, v, ok := args[i], a range's element or a type switch's
// variable, and a mutable one is reported. A mut any parameter is
// reported. An argument to an any parameter, or a value of an ...any, is a
// value of a basic type, an untyped constant settled to int64, an any
// passed on, or a view of a basic type, a slice or a call returning one
// allowed there as where a view parameter takes it, whose elements are
// borrowed under the rules a view argument's are; an array, a list, a
// struct or a view of another type is reported. Nothing widens to
// any or from it, and it supports no operator and no conversion, so every
// other use of an any, ==, an expression switch on it, arithmetic, len, an
// index, a range, a conversion, printing, a return, is reported by the
// rule that would report a value of any other wrong type. A value passed
// as any is a plain argument for exclusivity, a copy.
//
// Which type an any holds is asked in assert.go. A type assertion, v, ok
// := x.(T), is a checked form, bound with ok as a division, an index or a
// conversion may be, sharing their rules, and reported anywhere else, as
// x.(type) is anywhere but a switch's tag. x must be an any and T one of
// the basic types one can hold; a view is reported. A type switch, switch
// v := x.(type), checks each case's types the same way, a view of a basic
// type allowed, reports a type named twice, and gives each case's
// variable the case's one type, a view under every view rule included,
// or any; it gives no facts, and is otherwise checked as a switch, its
// default last.
//
// A call to std/fmt's printf, checked in printf.go, has its format read
// when it is a known string (knownString): every verb against the value
// it takes, an unknown verb, a % ending the format, and the number of
// values against the number of verbs, each mistake an error at the value
// or the format. A value of type any matches every verb, and a spread is
// checked for its verbs only. Any other format is printf's C's to handle.
//
// An error, checked in errors.go, is types.Error, a struct no declaration
// gives, of code, an int32, and msg, a string, code 0 being no error: it
// is written as a literal naming both, or noerror, the error that is no
// error, a constant expression, and does what a struct does, its fields
// read and written, compared with ==. A function returns it as its last
// result only, and an error earlier in a result list is reported where
// it is written. A call's error is never dropped: a call statement whose
// callee returns one, and a binding with _ where the error goes, are
// reported. The results a binding takes beside the error are held by the
// error's variable, and each read of one, as a value, a mut argument or
// the target of a compound assignment, ++ or --, is reported unless the
// facts in force prove the error's code is 0: a check is a fact about
// err.code (errorCodeKey), from the conditions facts come from, under the
// region rule, which the notes of explain.go explain when it falls
// short. Which variables an error holds is collected from the whole
// function first, since a loop may bind them after a read. So that a
// check is of the error that came with the values, a binding that holds
// any binds the error and them to local variables, and an error that
// holds variables is assigned only by a binding of a call returning an
// error that assigns every one of them too; anything else that assigns
// it, a mut argument included, is reported, with a note at the binding
// that tied them.
//
// A ?, checked in question.go, follows a call whose last result is an
// error, of a function, a method, a function value, append, reserve or
// put, and is written in the body of a function whose last result is an
// error, outside deferred code; anything else before it, a stored error
// among them, and a ? anywhere else, a package-level variable's value
// included, are reported. It records on the Question the call's results
// before its error, which it gives: one is its type as a value, none
// make it a statement only, and several are bound by a, b := f()?. None
// is held by an error, so none needs a check. The call is checked where
// it ends, as an operand is, and the function's contract must hold at
// the ?, as at a return, the call's writes counted. move.go follows the
// ? as the call: what it gives must have an owner, as a call's value
// must, and a ? statement drops nothing owned.
//
// A function's contract, func f(d int64) ensures d != 0, checked in
// ensures.go, is facts about its parameters, the receiver included, that
// hold whenever it returns. The clause is checked with the signature, so
// a call can use a contract of a function declared after it, in any
// package: it holds only what an if's condition gives facts through,
// comparisons of the parameters, their fields and elements, constants,
// len and has, with !, &&, ||, + and -, each operand of its && giving a
// fact, names no own parameter and nothing but a parameter, and is a
// bool; an extern func's is reported. Its facts are those factsFrom gives
// for it being true. The body proves each at every return, once its
// values are computed, and at the end of the body when that can be
// reached (c.dead counts the blocks a leaving statement, one ending in
// exit included, has made the rest of unreachable), from the facts in
// force.
// A defer written before the return that writes a variable a fact names
// fails it, since deferred code runs after. A call that is a statement,
// or the whole value of a declaration or an assignment, puts the facts in
// force for the rest of its block, as an early exit's are, under a guard
// of its own, each parameter replaced by the caller's local variable or
// parameter passed for it by name, as mut or as a plain argument nothing
// else in the call writes, and not assigned by the statement; any other
// argument drops every fact naming its parameter, as does a write
// anywhere in the statement's targets, and a call through a function
// value has no contract. A return whose one value is such a call proves
// with its facts. No fact is about a field of a mut parameter, so a
// clause about one, a mut receiver's field included, gives none.
//
// An index, a slice's bound, a divisor or a conversion's operand may be
// the length of a string, view or list subject, bounded as it is as an
// operand of arithmetic, so len(s) is proven wherever len(s) + 0 is
// (operandRange), or arithmetic, not only a subject (arith.go): +, - or *
// on two integers, or unary - on one, each operand a known value, a
// subject, the length of a string, view or list subject, or such
// arithmetic, is bounded by
// working its operation out exactly on its operands' bounds, each from
// its type and the facts in force (intervalOf) or, for a length, from
// minLen to maxLen, or the largest int64 when the facts give no upper
// bound (rangeOf). An operation proven not to overflow gives its exact
// result, and one not proven is reported, but rangeOf asks for itself, so
// an operation whose exact bound leaves its type's range on some target
// (portableBounds: a target-width type's top is types.PortableMax) has no
// bound, nor has arithmetic holding it, and any other operand, a call, a
// conversion, / or %, a shift or a bitwise operator, gives none.
// Arithmetic with no bound of its own is still within its type's bounds
// (operandRange), which may be enough, as a uint8 index's are for 256
// elements. A product of two values the facts keep from zero, or the
// negation of one, is not zero either. Each operand is bounded on its
// own, so x - x is not 0. No operand can call anything that writes, so
// every leaf is read inside the use, before c.at, and any write in the
// use ends the facts about what it writes. Arithmetic that adds a known
// amount to one node (linear) is also a query's term (arithTerm), so the
// relations in force bound it as they bound a subject, with the
// conditions that its steps do not wrap dropped where its own bound
// proves them and proven by the graph otherwise (relates). The work of
// rangeOf grows with the arithmetic it reads, once per use.
//
// Where a value's own bounds fall short of proving an index, a slice's
// bound or a constant index into a string (never a divisor), the
// relations in force are asked (relations.go): difference bounds x - y <=
// w between two nodes, each a subject, the length of a string, view or
// list subject, or the constant 0, which every fact in force gives, a
// bound by a known value against 0, i < len(s) and j <= len(s) against
// the length, i <= j and i < j between two subjects, and the difference
// bounds of a comparison of two values, each a node plus or minus a known
// amount (linearFacts), or of a declaration or a plain assignment storing
// one (storedFacts, put in force by checkBlock for the rest of the block).
// A statement storing a known value in a mutable variable, or a value of
// known bounds, a step included, puts the variable within them for the
// rest of the block (rangeStored); those facts, guardValue's, stay out of
// the graph and out of notes.
// A bound through x + c carries the condition that x lies where the sum
// stays in its type, which the overflow proof asks of the sum anyway (stepBounds' bounds, so a
// target-width type's top is types.PortableMax), and counts only once the
// other bounds prove it. A query (relates) builds a graph of the bounds
// that hold at c.at from an index of the facts by node, rebuilt when the
// facts in force change (factGen), finding at most relationCap nodes
// breadth first from the query's two, and takes shortest paths by
// Bellman and Ford; it only proves less for what it leaves out, and its
// int64 weights never wrap. Relations end as facts do: a writeIndex's
// endings take in every node a fact names. explain leaves them out, and relationNotes says
// what a chain lacks: a missing link between two values, the condition
// that rules out a wrap, or the cap. The checker counts the steps its
// provers take (proverWork, in work.go): each known value of an
// arithmetic operator worked out, each edge a relation query looks at,
// each fact read into the index, and each fact a query about one
// subject reads, through an index of the facts in force by subject once
// more than a few are (factindex.go), each local's known value worked
// out, each condition whose facts are worked out rather than taken from
// the && or || chain kept, each node a region's writes are indexed
// from, each write a fact's query judges and each step of the reach
// index it reads, each operator or operand an arithmetic bound
// reads, each if, switch, fallthrough or loop whether it leaves or
// terminates is worked out for, and each statement copied into a region
// for the facts a statement before it gives, which the regions over the
// rest of a block share rather than copy (region.go). The
// counts change nothing it checks;
// the tests read them to bound how checking grows with a program, which,
// unlike a time, does not depend on the machine's load.
//
// A slice, x[i:j], checked in slice.go, is a string for a string, any
// string, and a view of the elements for an array that is a place or a
// view; anything else, and a bound that is not an integer, of any type, is
// reported. A view slice is allowed only where viewSlice marks it, as the
// argument of a view parameter, of len, or of another slice, or a return's
// value in a function returning a view, and is reported anywhere else, a
// checked form's value included. Its bounds must
// be proven, 0 <= i, i <= j and j <= len(x), a bound left out being 0 or
// len(x), by known values, types and facts, and by the relations in force
// (see below and relations.go); a bound that is the length of a string,
// view or list subject is bounded as an operand of arithmetic is
// (leafRange), so len(s) is proven wherever len(s) + 0 is. An error names
// exactly the comparisons missing, and
// notes explain a bound that is a subject as an index's are. A known bound
// negative, past a known length (extentOf: an array's, a known string's,
// or a slice's at known bounds), or past the other is always reported. A
// string's slice may be bound with ok instead, t, ok := s[i:j], needing no
// proof. A slice of a known string at known bounds is a known string
// (knownSlice), and len of a slice at known bounds, calling nothing, is a
// known value. A slice is never a place, so assigning one, an element of
// one, or passing either as mut is reported.
//
// A struct type is a type of its own for each declaration, whatever its
// fields; struct.go checks its uses. A literal names each field exactly
// once, with a value usable as the field's type, in any order; an element
// with no name, a key that is not a bare name, one in parentheses
// included, a name it does not have, or a name given twice is reported,
// and so are the fields left out. A field, x.f, may be read from any struct
// value, and written where x is a mutable variable, or an element or field
// of one, since a struct follows its variable as an array does. Two structs
// of one type compare with == and != when every field's type does, a
// field holding a list included.
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
// only up to a write to either variable that can reach the use, and string.go uses it to
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
// The last pass checks each function body:
//
//   - a call passes the number of arguments its callee declares, each usable
//     as the declared type, whether the callee is a Tack function or a
//     builtin, or, for a variadic callee, at least one per fixed parameter
//     and then values of the element type or one spread (variadic.go); a
//     mut parameter takes mut x, where x is a place of exactly its type,
//     and only a mut parameter does (mut.go); an any parameter takes a
//     value of a basic type, a view of one or an any (any.go); and std/fmt's printf a
//     known format its values match (printf.go);
//   - a type assertion is bound with ok, on an any, to a basic type an any
//     can hold, a view only being a type switch's case, and .(type) is
//     only a switch's tag (assert.go);
//   - a call to a function that returns nothing, or several values, cannot
//     be used as a value, and a call statement cannot drop an error;
//   - a call statement does something: one that only gives a value, a
//     union built, a conversion or a call of len, is reported as not used
//     (onlyValue), unless it has an error of its own;
//   - every operand of an arithmetic or a bitwise operator is an integer,
//     the two having a common type, and the operand of - a signed integer
//     and of ^ any integer; a shift's left operand is an integer, whose
//     type it gives, an untyped constant becoming an int64 beside a typed
//     count, and its count an unsigned integer or an untyped constant that
//     is not negative, a uint64 unless both are untyped (shift); every operand of
//     && || and ! is a bool; a binary expression whose operator is not
//     defined on its operands is one error, at the first such operand;
//   - typed arithmetic, bitwise or shift operators whose operands are
//     known values, as knownValue computes them, give a value of their
//     type, a shift that drops a set bit not doing so, or the step that does not
//     is reported as overflowing it, here as at package level; so is - on
//     a known smallest value of a signed type. The expression is then
//     Invalid, so nothing around it is reported again. The wrapping
//     operators +% -% *% and -%x fold on known values by wrapping at the
//     type's width, and a target-width type's are never known (wrapStep);
//   - plain + - * on integers and unary - on a value not known, and ++ --
//     += -= *= on an integer target, are proven to give a value of their
//     type, below 0 included for an unsigned one, or reported with the
//     check that would prove them, the checked form and the wrapping
//     operator (overflow.go). The proof works the exact result out from
//     the operands' bounds (exactRange, operandBounds), a target-width
//     type's on a 32-bit target, or from the relations in force with each
//     variable's type's bounds counted (linearProven, orderedSub). An
//     operation whose own operand was reported is not reported again. A
//     loop's post is tried again after its body, with its condition in
//     force less what the body writes (recheckPost). v, ok := a + b, a -
//     b, a * b or -a is the checked form of the outermost operation
//     (arithResults); a float's, and a wrapping operator's, is reported;
//   - the wrapping operators take integers only, with a common type, and
//     two untyped constants are reported, naming a conversion of one;
//   - two integer operands of a comparison, of any types, compare by their
//     values with all six operators, once an untyped constant has settled
//     to the other operand's type, which it must fit, so int8(-1) < uint64(0)
//     holds and u < -1, for a uint64 u, is an error. The facts such a
//     comparison gives are about those values too. Any other operands have
//     one type, after widening, which supports the operator: a string all
//     six, a bool, an array, a list, a struct or a union only == and !=,
//     an array or struct of integers included, whose element or field
//     types must match. A mismatch, or an unsupported operator, is one
//     error covering the whole comparison. Values holding lists are read
//     in place, so neither may be a temporary (move.go reports one), nor
//     be passed as mut in the other operand (checkCompareBorrows), and a
//     switch may not take one as its tag;
//   - a divisor is proven not zero, or the division is bound with ok, and
//     the quotient of two untyped constants bound with ok settles to the
//     type of the variable it is assigned, or int64 where there is none;
//   - a signed a / b, and x /= b, whose divisor is proven not zero, is
//     proven not to be its type's smallest value divided by -1: a fact,
//     a known value or the bounds of either operand rules out the divisor
//     being -1 or the dividend being that smallest value (quotientProven,
//     excludes), b != -1 among the facts, which only this proof counts;
//     or it is reported with the check that would prove it, b != -1 or
//     a > the smallest value, and the checked form, with no notes
//     (requireNoQuotientOverflow). q, ok := a / b then gives 0 and false
//     at that quotient too;
//   - a conversion T(x) converts an integer to an integer type, and only
//     where it cannot fail: a constant or a known value, as knownValue
//     computes it, that fits T, a type that widens to T, or a subject, a
//     variable or a path below an immutable one, or the length of one,
//     that the facts in force
//     keep within T's range on every target, 0 to types.PortableMax for
//     a target-width T, by its own bounds or through the relations in
//     force (factsFit, convfacts.go), or arithmetic on those whose bound
//     keeps it so (arith.go). The facts are an index's, from the same
//     places and ended by the same writes; uint64(-(v + 1)) is proven
//     under v < 0, and uint64(-v) is not, -v overflowing at the least
//     int64.
//     A constant or known value that does not fit T is reported as
//     overflowing it. Any other conversion is reported, with the check
//     that would prove a subject, as an index's is, and notes on the facts
//     that fall short (conversionError), unless it is written
//     v, ok := T(x), which is also allowed where the conversion cannot
//     fail, and reports a constant or known value that does not fit T as
//     T(x) does;
//   - a variable's value is usable as its declared type, and a variable
//     declared without a type takes its value's;
//   - only a mutable variable may be declared without a value, and only a
//     mutable variable or a mut parameter, or an element or field of one,
//     may be assigned to; plain parameters never may;
//   - an assigned value is usable as the variable's type, and a compound
//     assignment such as += has an integer target and a value usable as its
//     type; ++ and -- need a mutable integer variable;
//   - a return carries one value for each result the function declares, each
//     usable as its result's type, and a wrong count is one error; an error
//     is only ever the last result declared;
//   - two names bound to a value that holds a division, a remainder, an
//     index or a slice outside any call, but is more than it and is not
//     checked arithmetic itself, as in q, ok := a / b & 1,
//     are one error saying the checked form must be the whole value, which
//     names the first such operation and what to compute from its result;
//     that operation is then checked as bound with ok;
//   - a, b := f() binds a call with exactly that many results, each variable
//     taking its result's type, or a two-value conversion or type
//     assertion; a, b = f()
//     assigns them to mutable variables they widen to, each named once, with
//     _ discarding a value of any type and allowed more than once; and a call
//     with several results is never used as one value; a binding of a call
//     that returns an error puts no _ where the error goes, and the
//     variables beside it are read only once it is checked (errors.go);
//     a, b := f()? binds the results before f's error the same way, and
//     holds nothing to check (question.go);
//   - an if or loop condition is a bool, and a loop's init and post
//     statements are checked like any other statement;
//   - a switch on a union matches its cases, each value a pattern naming
//     one, none twice, every case covered or a default, never both, a
//     clause naming one case binding its payload (union.go);
//   - a switch's tag, an untyped constant being an int64, compares with each
//     case value under the rules of ==, the value settling to the tag's
//     type if it is an untyped constant, and an integer value of any type
//     comparing with an integer tag by value, and in a tagless switch each case
//     value is a bool condition; two case values with the same known
//     integer value, the same bool constant value, or two equal string
//     literals, are a duplicate, reported with a note at the first; a type
//     switch's case values are types, none named twice (assert.go);
//   - a range loop ranges over an integer, whose one variable takes its type,
//     or over an array, a view, a list or a string, whose index is an int64 and
//     whose element is the array's, view's or list's element type, or a string's
//     byte, a uint8;
//     both variables are immutable, and two over an integer are an error;
//   - break and continue are inside a loop, and in deferred code inside a
//     loop that is itself inside the deferred code; a return is never in
//     deferred code, since nothing may leave it, and neither is a defer, at
//     any depth, reported at its keyword, but only the outermost: a defer
//     inside one already reported is not reported again;
//   - a fallthrough is the last statement of a case of an expression
//     switch other than its last, and not in a defer inside the case, a
//     type switch or a switch on a union (switch.go);
//   - a function that declares a result ends in a terminating statement: a
//     return, an if with an else whose branches both terminate, a switch
//     with a default whose cases all terminate, a case ending in a
//     fallthrough counting as one, a loop with no condition
//     and no break that leaves it (a break in a nested loop leaves only that
//     one, and one in a switch leaves the loop around it), a call to
//     the builtin exit, which never returns, or an unreachable, which is
//     proven never reached. The rule is syntactic, as in
//     Go, so a call to a function that never comes back, one that calls
//     exit included, does not satisfy it.
//
// A list, list[T], is an owned, growable list, checked in list.go: make
// makes an empty one through an Allocator, c_allocator the one there is,
// append grows one passed as mut, by any number of values or a spread
// view as a variadic takes them, reserve makes room in one passed as mut
// for a count that is not a negative constant, each returning an error
// that cannot be dropped, pop and remove take an element out of one
// passed as mut, the plain form with a proof that it is there, the
// checked form, x, ok := pop(mut l), with none, its x held by its ok when
// the element type has no zero value, as a result is by the error beside
// it (errors.go), replace swaps a value into any place passed as mut,
// giving the old one, which takes a list out of a struct's field and puts
// it back (replace.go), and a list reads as a view does, len with a string variable's
// facts, a proven or checked index, range, slicing to a view and passing
// whole to a view parameter, and can have its elements written when it is
// mutable. Its elements may be owned, as a view's may, and are then read
// in place and never copied out: the checked form of an index, which
// copies, is reported, and move.go reports a move of one out of its list,
// a spread of them, and a change to the list, or any element of it, while
// a range binds one in place (rangeBindings). An array's and a
// variadic's elements are copyable: no list, and no struct holding one,
// a check put off for a struct's field until every struct's fields are
// known. No type holds itself through a list, or a map, which the cycle
// check reports with the cycles by value. A map, map[K]V, checked in
// maps.go, is owned as a list is, its K any type with == but error and
// type parameters, a check put off for a struct's field (pendingKeys),
// its V anything a list's element can be: put moves a key and a value in,
// its error never dropped, v, ok := m[k] copies a copyable value, has(m,
// k) tested as a condition proves k is in m by a fact like an index's
// (memberFact), ended by any write to m but not to a value it holds, and
// m[k] where no fact proves its key is reported, being a place where one
// does, delete gives its value only with ok or as a
// statement, as pop's checked form does (isTake), a range binds its key
// and value, in place when owned (rangeBindings), and locks the map as a
// list's range does, and len(m) is never known. A function type,
// func(A, mut B) R, checked in funcvalue.go, is the type of a value
// naming a function: a function's name used as a value, or a method
// named through its type, its type recorded in Func.Value, unless the
// function is generic, an extern func, or has a variadic or any parameter
// or a view result, which no function type can say. A call through a variable or a parameter holding
// one is checked against a stand-in function of its type (Call.Dynamic),
// as a call by name is against its function's signature, so every rule
// for arguments, proofs, borrows, moves and results holds for it. A
// function type has no zero value, no ==, and never crosses to C or sits
// in a global. A callee bound to a function, where a variable or a
// parameter of its name holding a function value is visible (Ident.Hidden),
// is reported. A function literal is a closure, allowed only as an
// argument to a scope parameter, and a scope parameter is used only as a
// callee or as an argument to another, so nothing a closure makes is
// stored, returned or held (see closure.go). A closure's body is checked
// where it is written, as a block of the function holding it but with its
// own results, loops, defers and cases, and with only the facts about
// variables nothing can write in force; its captures are recorded on it
// and count as borrowed arguments of the call it is passed to, a capture
// it writes as a mut argument (see checkCaptures and writersIn); a write
// in it is a write where it is written, and a closure is a loop around its
// body for whether a write reaches a use (see isLoop), with no branch in
// it a keep, since its return leaves only it; move.go follows its body
// where it is written and lets it move only its own variables. A list, a map, an
// Allocator, a function value, and anything holding one, has no zero value; a list has
// no ==, and no global holds one. A list owns its buffer, so it moves
// rather than copies, which move.go checks after a body: a moved variable
// is not used again until it is assigned, a parameter that is not own and
// a field are never moved, an own parameter is followed as a local (see
// own.go, which checks how own is written on parameters and arguments,
// and that nothing else in a call names a variable passed as own), a list
// no variable holds is given an owner where it is
// made, a defer reads no list moved later in its block and moves none
// declared outside it, and a range over a list cannot change it. A list
// read in place by one argument cannot share a call with a mut argument
// that may overlap it (exclusive.go). An index or a slice reads its list
// in place too, so its own index or bounds cannot pass as mut a place
// that may overlap the list, and the value assigned to an element of a
// list cannot pass as mut that list or a place holding it
// (checkIndexBorrows, checkSliceBorrows and checkAssignBorrows): each is
// reported instead of any proof the index needs.
//
// Unions (union.go). A union type is a type of its own per declaration,
// whose cases, each a name and a payload's type or none, are resolved
// with the structs' fields, under a field's rules for their types. A
// value is built by naming a case through the union, Expr.int(5), its
// payload used as the case's type as an argument is, or Expr.eof, and the
// call or selector records the union as its type; one built as a
// statement is reported as unused. A union has no zero value, is compared
// with == when every payload's type is, and is owned when a payload holds
// a list. A switch on one matches its cases (checkUnionSwitch): the
// resolver bound each value as a value or a pattern by its names, so a
// pattern's binding reusing a visible name is reported here, as a value
// whose names name nothing is in a switch on anything else
// (unresolvedCase); a clause naming one case gives its binding the
// payload's type, an immutable variable that refers to the payload in
// place, which is reported unused here, and move.go keeps the clause from
// changing the switched place while it does, and the binding from being
// moved. The clauses must cover every case, or the switch have a default,
// but not both; one covering every case is recorded as exhaustive, and is
// a terminating statement when every clause is. A union does not cross to
// C, cannot be passed as any or converted; a pub one exposes its payloads'
// types as a field does. It may have methods, as a struct may, none named
// like one of its cases, and so satisfy interfaces and be a type argument;
// a mut method called on the switched place in a clause binding a payload
// is a mut argument of it, which move.go reports.
//
// Interfaces and generic functions (generic.go). An interface's methods
// have their parameter and result types resolved after the globals, as a
// function's are, and an interface written as any type but a type
// parameter's constraint is reported. A generic function's type
// parameters are each given a type of their own (types.Table.TypeParam)
// before its parameters' types are resolved, which may name them; each
// constraint must name an interface, and each type parameter be mentioned
// by some parameter's type, whole or inside a list, a view, a map's value
// or a function type. The body is checked once, against the constraints,
// never for a call: a value of a type parameter has its constraint's
// methods, a call of one checked against a stand-in method (see
// ast.Func.Interface), and nothing else, and is owned as a list is, since
// its type argument may hold one, so every ownership rule above applies
// to it as written, whatever the type argument: a list, a view, a map's
// value and a function type may be made of one, a result may be one, and
// swap (swap.go) reorders a list of one in place. A call to a generic
// function infers each type parameter from the arguments, in order, by
// matching each argument's type against its parameter's, part by part
// (infer.go), as any type but a view, an any or one made of a type
// parameter, the caller's own type parameters included, satisfying the
// constraint, one type per type parameter; a type that is no struct or
// union has no method. A struct or
// union type satisfies it only with methods visible from the package
// holding the call, as if that code called them, whatever package the
// interface is in; a type parameter passed on is checked for no
// visibility, since its constraint asks for no method the outermost call
// binding a struct or union type did not check. The call records the type
// arguments in Call.TypeArgs, and the callee's parameters' and results'
// types with them in place in Call.ParamTypes and Call.ResultTypes, which
// are its results (ast.CallResults). Once every body checks, the instances of
// generic functions the program needs are found (instance.go): from the
// bodies of the functions that are not generic, then from each instance
// found, its type parameters standing for its type arguments, each
// recorded once in Program.Instances and every file's File.Instances, up
// to MaxInstances, past which the call needing another is reported.
//
// Before any of this, Check records the program's types.Table on
// Program.Types and on every file's File.Types, and every diagnostic names
// a type through it, so a type is always named by the compilation that gave
// it its ID. Later stages read the same Table from the program or a file.
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
//
// # After errors
//
// The checker runs even when the loader or the resolver reported errors,
// so a run reports a program's type errors along with its syntax and
// naming errors, and must report nothing that follows from those (see
// invalid.go). A name the resolver bound to ast.Unbound, because what it
// names was reported or may have been declared by damaged source, has
// types.Invalid, so nothing that uses it is reported: a call to it checks
// its arguments on their own and gives no value, a type written with it
// is invalid, and a global initialized from it is not reported as not
// constant. Bad nodes are skipped. A switch on a union with a bad clause,
// which may have named any case or been the default, is not reported for
// missing cases, and a type switch's bad clause gives its variable no
// type. In a function whose body holds a bad
// statement only what is reported before the first one is kept, and it is
// not reported for not ending in a return, nor checked for moved values,
// since the bad statement may have been a return, a guard, a declaration
// or an assignment. A method its type does not have is not reported
// missing, as a call, a field or an interface's method, when a bad
// declaration of its package may have been it. Nor is anything the
// resolver already reported about a declaration reported again: a
// function without a body does not end in a return, an extern method is
// not checked as an extern func, the receiver of a method on no type the
// package can give methods to has no type, two functions of one name do
// not clash in C, the main function, extern, is not named main in C, and
// a target the resolver called unassignable is not reported again.
//
// None of this lets an invalid program through. It applies only when an
// earlier stage reported an error, counted in Program.Errors; without
// one, a name bound to ast.Unbound is reported as undefined and a bad node
// as a syntax error. Check adds its own errors to Program.Errors and
// records the total on the program's types.Table, and a backend generates
// code only when both are zero (see codegen.Refuse).
//
// A variable that holds an error a call returned or an ok a checked form
// gave, and that nothing reads, is an error, the error f returns is never
// checked: err is not read, as binding it to _ is (see unread.go); the
// resolver leaves every variable a call or a binding of several names gave
// a value to the checker for that reason (ast.Var.UnusedDeferred), and the
// checker warns of one that holds neither as unused.
//
// The checker's warnings, not errors, are: a statement that only gives a
// value, x is not used; a union clause's variable its body never reads,
// declared and not used, which is marked ast.Var.Unused; and, in a
// program with no error, a parameter, a private function or method and a
// private type nothing uses (see unusedecl.go). Every one is about code
// never used, and is marked diag.Diagnostic.Unused (see unusedf). Warnings are kept apart from the errors (Checker's
// warnings, reported by unusedf), so every count of what has been reported
// that decides what is checked next, whether a body is checked for moved
// values or anything is instantiated among them, counts errors alone,
// and Program.Errors counts no warning. Check returns both, the errors
// first. A body checked twice drops the first pass's warnings with its
// errors, and a damaged body drops both after its first bad statement.
package typeck

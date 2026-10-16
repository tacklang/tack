# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## A diagnostic at an implied semicolon points at a trailing comment

When a line ends in a comment, a parse error about the semicolon that the
line end implies is placed at the comment rather than at the newline. In
`exit(42 // c` followed by a newline and `)`, the error points at the `//`,
while the same source without the comment points at the newline. The
comment-as-newline sweep in `parser/comment_test.go` compares diagnostics by
message only because of this.

## A union built in place passed to a mut parameter without mut

`f(Expr.eof)`, where `f` takes a `mut` parameter, is `f takes e as mut;
pass mut Expr.eof`, suggesting a fix that is itself rejected, since a
union built in place is no place. The check reads `Expr.eof` as a field
of a name. A `mut` receiver written without `mut`, `Expr.eof.set()`,
already says `only a variable, a field or an element can be passed as
mut` instead; the parameter's wording should too.

## A function must end in a return even when no path reaches its end

A function with a result must end in a `return`, even when every path
before its end already returns. `func sign(x int64) int64` whose body is
`if x > 0 { return 1 }`, `if x < 0 { return -1 }` and `if x == 0 { return
0 }` is `sign must end in a return`, though the three conditions cover
every value. Tack has no way to say a place is unreachable, and a
`panic("unreachable")`, as Go writes it, would break the no-panic
guarantee. Today such a function ends in a `return` that never runs, as
`append_digits` in `std/bytes` does, or covers the last case with `else`.
The fix is to prove more paths end, or an `unreachable` statement the
compiler must prove unreachable, rejecting it otherwise; both wait for a
stronger prover.

## A plain pop or remove after a length check never compiles

`if len(l) > 0 { x := pop(mut l) }` is `pop may find l empty; check len(l)
> 0 first, or use x, ok := pop(mut l)`, with notes that the guard proves
`len(l) >= 1` but that `l` is passed as `mut` to `pop`, so the proof ends.
Passing a variable as `mut` ends every fact about it, including the fact
the call itself relies on, so the plain forms `x := pop(mut l)` and `x :=
remove(mut l, i)` are never accepted, and the hint asks for a check the
code already makes. Only the checked forms, `x, ok := pop(mut l)` and `x,
ok := remove(mut l, i)`, compile. The same rule rejects `if d != 0 { d = n
/ d }` and `if len(l) > 0 && pop(mut l) > 0`. The fix is a change to the
proof rules, where a fact holds through the call or assignment that
consumes it and ends after it.

## A proof about a variable ends at any later write in the same loop

A fact proved inside a loop body ends when the variable is written
anywhere later in that body, even after the use the fact guards. In

    for {
        if at < 0 || at >= len(nodes) { break }
        switch nodes[at] { case neg(c): at = c ... }
    }

`nodes[at]` is `index at may be out of range`, because `at = c` comes
later in the body. In the same way, `top := len(frames) - 1` with a bounds
check does not allow `frames[top].right = true` when `pop(mut frames)`
comes later in the loop, since passing `frames` as `mut` ends every fact
about it. The workarounds are copying the variable into an immutable
local first (`i := at`), or moving the access into a small function that
checks the bounds itself. Both hide no unsafety, but they push code into
helpers. This is the same proof-rule gap as the plain `pop` entry, and
belongs to the same change: a fact should hold up to the first write that
can reach the use.

## A successful append does not prove the list is not empty

After `err := append(mut l, 1)` and `if err.code != 0 { return }`, the
checker still does not know `len(l) >= 1`, so `10 % len(l)` is `divisor
may be zero` and `l[0]` needs a check. A successful append, or `reserve`,
could record what it proves about the length.

## pop's results cannot be thrown away

`_, _ = pop(mut l)` is `every name is _, so nothing is bound; make the
call on its own`, and the call on its own, `pop(mut l)`, is then the
plain form that never compiles after a length check (see above). So a
program that only wants to drop the last element has to bind it and use
it. Either `pop` alone, as a statement, could be allowed with a checked
meaning (drop the last element if there is one), or `_, _ =` could be
accepted for it.

## A field cannot be taken out of a struct and put back

`mut frames := e.frames`, with `e` a `mut` receiver, is `cannot move
e.frames out of e: a field cannot be moved on its own`, and nothing lets a
function take a list out of a struct for a while and put it back. So a
list kept in a struct to be reused across calls is always reached through
the struct's pointer. In the expression-evaluator benchmark this cost
about 10%: through a pointer, clang must assume a store into the list's
buffer may change its length, so it reloads the length, pointer and
capacity on every push and pop instead of keeping them in registers.
Fixing it is a language decision, for example a swap or take-and-restore
operation on a `mut` field.

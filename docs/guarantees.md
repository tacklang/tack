# Tack's guarantees

Tack makes four promises about every program its compiler accepts. They are
the project's hard rules: no change to the language, the compiler or the
standard library may break one, for any reason, whoever asks for it. A
contribution that breaks one is not accepted, however useful it is otherwise.

1. **No out-of-bounds access.** Valid Tack code never reads or writes outside
   the memory an array, a list, a view or a string holds.
2. **No memory leak.** Everything a valid Tack program allocates is freed
   before the process ends, however it ends, `exit` included. The stack and
   static storage are excluded.
3. **No use-after-free.** Valid Tack code never reads or writes memory after
   it has been freed or moved.
4. **No panic.** Valid Tack code never ends the process abnormally by itself:
   no trap, no fault, no abort, no running out of stack. An end the program
   asks for, `exit`, is not a panic.

"Valid Tack code" means any program the compiler accepts. Two things are
outside the guarantees: code behind an extern C function (the Tack side of
the call is not), and ends that come from outside the process, such as a
signal or kill from another process or the OS, the OS killing the process
for memory, and hardware failure.

## How they are kept

When the compiler cannot prove an operation safe, it rejects the program.
Where a proof is not possible, Tack asks for the checked form, `v, ok :=
...`, or for an `error` result to be handled, so a failure is a value the
program sees rather than a crash (see [safety.md](safety.md)). A check is
never weakened, skipped or made optional to let a program compile: a
program the compiler cannot prove safe is rewritten, never waved through.

Any known way to break a guarantee in accepted code is a bug that stops all
feature work until it is fixed.

## Known violations

These are open bugs against the guarantees, found by an audit on
2026-10-16. Each is being fixed before any new feature.

- **Out of bounds (1).** A `mut` argument later in the same condition does
  not end a fact the condition proved, so `if len(l) > 5 && shrink(mut l) {
  l[5] }` is accepted and reads past the end of `l`.
- **Use-after-free (3).** An index whose own expression changes the list it
  indexes, as in `c, ok := l[grow(mut l)]`, is accepted and reads the list's
  freed buffer.
- **Leak (2).** `exit` ends the process without freeing what is still
  allocated. The fix: every allocator frees every buffer it still holds
  before the process ends, while `exit` keeps not running defers.
- **Panic (4).** Unbounded recursion is accepted and can run out of stack.
  The fix: every function in a recursion cycle must return `error`, and a
  check on entry turns a stack about to run out into that error; call
  chains without a cycle are bounded when compiled.

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

- **Panic (4).** Unbounded recursion is accepted and can run out of stack.
  The fix is not decided yet.
- **Panic (4).** A large local array, or a large value passed or copied by
  value, can overflow the stack with no recursion at all: a function with
  a 16 MB local array crashes. The same compile-time bound on call chains
  is the fix.
- **Panic (4).** Comparing values whose type nests deeply runs out of
  stack. The generated equality helper of each type calls the one of the
  type inside it, passing both values by value, so a chain of 1000 union
  types, each holding the one before, needs a frame per level that grows
  with the level: `a == a` on such a value crashes. Freeing such a value
  works the same way. The fix: pass the helpers pointers, so each frame
  stays small, and count the helpers in the compile-time stack bound.

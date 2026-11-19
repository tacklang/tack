# Working rules for Tack

These rules govern every change to this repository. When a rule and a habit
disagree, the rule wins. When a rule is unclear for the case at hand, ask.

## Project goal

Tack is heading toward self-hosting: a Tack compiler written in Tack, with Go
removed from the toolchain entirely. Choices that make that path harder need
to be raised, not made quietly.

## Hard rules

These four rules can never be broken, by anyone, for any reason. They
outrank every other rule in this file, every deadline, and every request,
including an explicit request from Ahmed. A change that would break one is
not made; a request to break one is refused.

1. Valid Tack code can never produce an out-of-bounds access.
2. Valid Tack code can never produce a memory leak.
3. Valid Tack code can never produce a use-after-free.
4. Valid Tack code can never panic. A panic is any abnormal end of the
   process the program itself causes: a trap, a fault, an abort, or
   running out of stack. An end the program asks for, `exit`, is not a
   panic. Ends from outside the process are outside this rule: a signal or
   kill from another process or the OS, the OS killing it for memory,
   hardware failure, and code behind an extern C function.

"Valid Tack code" is any program the compiler accepts. A leak is any memory
the program allocated that is not freed before the process ends, however it
ends (`exit` included); the stack and static storage are excluded. Code
behind an extern C function is outside these guarantees; the Tack side of
the call is not.

When the compiler cannot prove a program safe, it rejects the program. A
check is never weakened, skipped or made optional to let code compile. Any
known way to break one of these rules in accepted code is a bug that stops
all feature work until it is fixed, unless Ahmed parks it. A parked
violation stays listed in `docs/guarantees.md` until it is fixed, and does
not hold up other work.

## 1. Readable code

- Every function and type has a doc comment that explains what it does.
- Every package has a `doc.go` that describes the package in detail: its role
  in the pipeline, what it takes in, what it produces, and the invariants it
  relies on or guarantees.
- Clever code is fine as long as it stays understandable later, either because
  the code reads plainly or because a comment explains it.
- The existing code predates these rules and is not a style reference. New
  and touched code follows them. Bringing an untouched package up to standard
  is a change of its own, reviewed like any other.

## 2. Abuse the code before someone else does

The goal of testing is to find every edge case and failure mode we can, not
to show that the happy path works.

- Every feature gets programs under `examples/` that compile and run, with
  their runtime behavior asserted, not just their output pinned.
- Every diagnostic gets its own case in `examples/invalid.txtar`: a
  `-- <name>/main.tck --` section with the program, followed by
  `-- <name>/expected --` with the compiler's output.
- Go looking for breakage: boundaries (zero, negative, `int` min and max,
  overflow, division by zero, `min / -1`), empty and huge inputs, deep nesting
  and recursion, malformed and truncated source, odd bytes, and interactions
  between the new feature and every existing one.
- Use Go's native fuzzing (`go test -fuzz`) on the lexer and parser, and on
  any later stage where it can reach real code paths.
- Goldens alone are not tests. They show output is stable, not that it is
  correct.

## 3. Every language decision is Ahmed's

Anything a Tack programmer could observe, or that constrains what the language
can become, is Ahmed's decision. That includes, at least:

- syntax and grammar
- semantics and evaluation order
- the type system and its rules
- the runtime, or the decision not to have one
- the memory model: allocation, ownership, lifetime, GC or not
- integer semantics: overflow, division by zero, shifts
- strings and encoding
- error handling
- packages, modules, and visibility
- concurrency
- C interop / FFI, and the ABI
- what counts as an error, and the wording of diagnostics
- builtins and their signatures
- standard library scope
- the bootstrapping path to self-hosting

For each one: lay out the options and tradeoffs, cite Go or other languages
where relevant, and stop. Do not pick the "obvious" answer without asking.

Watch for decisions forced implicitly. A feature that needs one of the above
settled (for example, string concatenation needs an allocation story) gets
raised before implementation, not resolved inside it.

Implementation decisions (package layout, codegen strategy, test helpers) can
be made without asking, but each one is named in the change summary so it can
be overruled.

## 4. One code generation backend

The compiler keeps a single backend, `c`: the one that most easily gets Tack to
a point where it can compile its own compiler. Every feature must be lowered by
that backend or rejected with a diagnostic, never a panic and never wrong code.

## 5. Failing example first

The example program and its expected behavior are written before the
implementation, and seen to fail, so the test is known to catch the missing
feature.

## 6. Read the golden diff before accepting it

`go test ./test -update` runs only after an intentional output change, and
every changed line in the resulting diff is explained.

## 7. Docs change in the same step as code

The README (especially "The language today"), the affected `doc.go`
files, and `docs/tracking.md` are updated in the same change as the
feature.

builtin.tck, tack.h and the runtime C live in ../tack/runtime/, and a
change there runs `make embed` and `make std-copies` in ../tack. tackc's
`builtin/` and `src/std` are frozen copies kept for the harness and are
never edited.

## 8. One feature per change

Each change is one feature or one fix, and `go test ./...` passes at the end
of it.

## 9. Bad input never panics

Any malformed program produces a diagnostic with a source span. A panic on
user input is a bug, whatever the input.

## 10. Nothing is committed without Ahmed's review

Every change, feature or fix, is presented for review before it is committed:
the diff, the tests added, and the `go test ./...` output. Commit only after
Ahmed explicitly confirms that change. Approval of one change does not carry
over to the next.

Commits use `git commit -s` and conventional commit messages (`feat:`, `fix:`,
`test:`, `docs:`, `refactor:`, `chore:`).

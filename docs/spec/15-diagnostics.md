# 15. Diagnostics

This chapter describes the general rules of what the compiler reports,
not each message: what makes a problem an error, which rejects the
program, or a warning, which does not; notes and the explanations they
add to a diagnostic; that every diagnostic has a source span and that
malformed input never crashes the compiler; how a diagnostic is
rendered (file, line, column, severity, message, source lines); the
order diagnostics are reported in and how many; and which stages run
after an earlier one reported errors, and which problems stop the later
ones. Each message is described with the rule that gives it, in the
chapter that owns that rule; the unused-name reports, for one, belong in
[Declarations and scope](05-declarations.md).

## Errors

A diagnostic is a problem the compiler finds in a program's source,
reported at a span of it. Each has a severity: error, warning or note.

An error rejects the program. A program with an error is not valid: it
is never generated, built or run, and a check of it fails. A program
the compiler cannot prove safe gets an error (see [Safety and
proofs](10-safety.md)).

```tack error
package main

func main() {
    var s string = 1
}
```

```diagnostics
main.tck:4:9: warning: declared and not used: s
main.tck:4:20: error: cannot use int64 as string in declaration of s
```

Malformed input never crashes the compiler: whatever bytes a file
holds, the compiler reports what is wrong with them as diagnostics, each
with a span in the source. A problem that is not in a program's source,
such as a file that cannot be read or a wrong command line, is not a
diagnostic: the compiler reports it on a line of its own that starts
with `tack:`.

## Warnings

A warning does not stop the program from building. This program gets
one, under the unused-name rules of [Declarations and
scope](05-declarations.md):

```tack warn
package main

func main() {
    x := 1
}
```

```diagnostics
main.tck:4:5: warning: declared and not used: x
```

A program with only warnings is valid: it is checked, generated, built
and run as one with no diagnostic is, its warnings reported first.
Code generation, which can find errors of its own, runs only when the
earlier stages reported no error but those [Recovery](#recovery) names,
so its errors are reported only then. A
warning reports code that is suspicious, not wrong: a variable declared
and never read, an import never used, a value computed and discarded.
Warnings are reported beside errors, as the example under
[Errors](#errors) shows.

## Notes

A note adds an explanation to an error or a warning, at a span of its
own: where a name was first declared, which guard failed to prove an
operation and why, or which call passed over a variable. A note never
stands alone. It is reported directly after the diagnostic it explains,
in the order the compiler gives, wherever its span is, and does not
count toward the limit on errors.

```tack error
package main

func twice() {
}

func twice() {
}

func main() {
    twice()
}
```

```diagnostics
main.tck:6:6: error: twice is already declared
main.tck:3:6: note: previous declaration of twice
```

Some errors that a guard does not prove come without a note, though a
note could explain them; [Safety and proofs](10-safety.md) describes
which notes an error for a missing proof has.

## Spans and rendering

Every diagnostic has a span: a range of bytes in one file of the
program, which may be empty, as for a missing token at the end of a
line. A diagnostic is reported at the start of its span, given as a line
and a column, both counted from 1. A line ends at a newline (see
[Source and lexical elements](02-lexical.md)); a column counts bytes, so
a tab is one column. When the program is compiled from a single file,
that file is named as the command line gives it, so `../w.tck` stays
`../w.tck`. When it is compiled from a directory or an import path, a
file is named relative to the current directory when it is in it or
below it, and by its absolute path otherwise.

A diagnostic is rendered as a first line, then the source around its
span:

```text
main.tck:4:12: error: invalid character #
03 |    func main() {
04 |        x := 1 # 2
   |               ^
05 |    }
```

The first line is the file, the line and the column, separated by
colons, then the severity, `error`, `warning` or `note`, and the
message. Under it are the line the span starts on and the line before
and after it, where the file has them, each numbered, and under the
span's line a caret at the span's first byte and a tilde under each
byte of the rest of the span. A
line is shown without its newline, whichever form it has. The compiler
separates the number and the bar from the source line with a tab,
shown above as spaces. A blank line comes before the first diagnostic
and after each one; a note follows its diagnostic in the same form,
with no blank line between them.

## Order and limits

The diagnostics of a run are reported together, sorted by the file each
is in and then by where its span starts, whichever stage found it:

```tack error
package main

func main() {
    print(greeting)
    x := 1
}

func f() {
    y := (1 + 2
}
```

```diagnostics
main.tck:4:11: error: undefined: greeting
main.tck:5:5: warning: declared and not used: x
main.tck:9:16: error: expected ')' found ';'
```

The syntax error is found first, by the parser, and the undefined name
after it, by the resolver, but the report is in source order. Files are
sorted by the name a diagnostic shows for them, in byte order.

The number of errors reported is limited by the `--max-errors` flag, 10
when it is not given, and unlimited when it is 0. Only errors count
toward the limit: the report stops where the first error past it would
be, leaving it out and everything after it, warnings included, and ends
with a line that says some were left out:

```text
too many errors: showing the first 10; --max-errors 0 shows them all
```

The warnings before that point are reported, so a warning never takes
an error's place. The parser stops parsing a file once it has found
more syntax errors than the limit.

## Recovery

Every error is reported once, at its cause. An error that follows from
one already reported is not reported.

The lexer, the parser, the resolver and the type checker each run
whatever an earlier one reported, but for the problems listed below
that stop them, so one run reports a program's syntax errors,
unresolved names, type errors and warnings together. After a syntax
error, the parser skips to the end of the
statement, the next line that starts a declaration, or the brace that
closes the block, and goes on. A name or a type an earlier error leaves
invalid is reported nothing more about. A damaged declaration hides the
name it was declaring, when the parser got that far, and every name of
its package when it did not. A damaged statement hides the names used
after it in its function and, since it may have been a return, a guard
or an assignment, every error the type checker would report after it
there:

```tack error
package main

func main() {
    if := 3
    print(missing)
}
```

```diagnostics
main.tck:4:8: error: expected 'value' found ':='
```

These problems stop the stages after them, so that nothing is reported
past them but what the earlier stages found:

- an error in lexing a file, since what the lexer cannot read is
  discarded, and what follows would only report errors that come from it;
- nesting past the limit (see [Source and lexical
  elements](02-lexical.md)), after which parsing the file stops;
- a package clause that is missing or cannot be parsed;
- a malformed `tack.mod`, and package clauses of a main package that do
  not fit together (see [Packages, modules and
  programs](12-packages.md));
- a directory or import path, given to build, whose package is not a
  main package, `calc is not a main package`.

Code generation runs last. When a program is built, or one of its
stages dumped, code generation runs only when no earlier stage reported
an error, warnings aside, so an error only code generation finds, such
as a field `tack.h` cannot declare under its name, is reported only for
a program that has no other error. When a program is checked, with
`--check`, as every example in this specification is, a missing `main`
function does not count: it is reported, and code generation runs
when it is the only error, so its errors are reported beside it. A
build of the same program reports the missing `main` function, with
what the other stages found, but none of code generation's errors.

```tack error
package main

func main() {
    print(greeting)
    x := 1 # 2
}
```

```diagnostics
main.tck:5:12: error: invalid character #
```

A few problems are found only when a program is built, after every
stage has checked it, since they need its C files compiled: [Extern
C](14-extern.md) describes them.

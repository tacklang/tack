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

Status: skeleton. The sections below are planned.

## Errors

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

## Notes

## Spans and rendering

## Order and limits

## Recovery

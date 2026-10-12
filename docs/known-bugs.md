# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## The diagnostic underline ends in a space

`diag.renderTokenMarker` writes one column past the end of a non-empty span,
so every underline is followed by a trailing space. Fixing it changes every
golden for an invalid program. `diag/render_test.go` pins the current output
until then.

## clang warnings reach the user

The driver runs clang without warning flags and passes its output through,
so a valid program can produce a C warning about the generated code. `x == x`
prints `warning: self-comparison always evaluates to true`, naming the
temporary C file, and the build still succeeds.

## Raw strings decode escape sequences

A backquoted string has its escape sequences decoded, with the double quote
as the extra escapable byte, so `` `\"` `` is a one-byte string, `` `\n` `` is
a newline, and `` `\`` `` is an invalid escape that does not end the string.
In Go a backquoted string is raw. Whether Tack's should be is still listed in
`docs/open-decisions.md` under "Escape sequences in raw strings"; this entry
records the current behavior until that is settled.

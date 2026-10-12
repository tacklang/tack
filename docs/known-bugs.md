# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## The diagnostic underline ends in a space

`diag.renderTokenMarker` writes one column past the end of a non-empty span,
so every underline is followed by a trailing space. Fixing it changes every
golden for an invalid program. `diag/render_test.go` pins the current output
until then.

## Merged comment lines keep inconsistent whitespace

Consecutive `//` lines merge into one comment token. The merged value is
trimmed as a whole, so `// a\n// b` becomes `"a\n b"`: the space after `//`
is removed from the first line and kept on the others.

## Comments are only allowed between top-level declarations

The lexer produces comment tokens anywhere, but the parser only accepts them
between declarations. A comment inside a function body, after a statement, or
before `package` is a parse error: `expected 'statement' found 'comment'`.

## Parentheses are left out of an expression's span

A parenthesised expression leaves no node of its own, so a binary expression
written as `(a) == (b)` spans from `a` to `b`, and a diagnostic about it
underlines `a) == (b`, without its first and last characters.

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

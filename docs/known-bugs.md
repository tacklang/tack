# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## `ast.File.String` drops its JSON error

`ast.File.String` ignores the error from `json.MarshalIndent`. After
resolution, a recursive program has a cycle in its tree (an identifier's
`Def` points back at the function containing it), so marshaling fails. For
`examples/fib`, `tack --res` used to print an empty dump and exit 0. The
driver now reports `tack: failed to render syntax tree: ...` and exits 1,
which is honest but means `--res` cannot show a recursive program at all.
The fix belongs in `ast`: render `Def` as a reference rather than following
it.

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

## A call that returns nothing is reported as type "invalid"

Using a call to a function that returns nothing, or several values, as an
operand or a condition, as in `f() + 1`, `f() == true`, `!f()` or
`if f() {`, is reported as `cannot use invalid as int in +`,
`operator ! is not defined on invalid` or
`condition must be a bool, got invalid`.
"invalid" is the checker's internal name for a missing type, not something a
Tack programmer should see. The same call as an argument, a declared value
or a returned value already gets a proper message.

## Parentheses are left out of an expression's span

A parenthesised expression leaves no node of its own, so a binary expression
written as `(a) == (b)` spans from `a` to `b`, and a diagnostic about it
underlines `a) == (b`, without its first and last characters.

## clang warnings reach the user

The driver runs clang without warning flags and passes its output through,
so a valid program can produce a C warning about the generated code. `x == x`
prints `warning: self-comparison always evaluates to true`, naming the
temporary C file, and the build still succeeds.

## A comment at the end of the file loses its last byte

`handleComment` ends a comment token at `l.pos-1`, which is right when a
newline follows, since the newline has already been consumed, and one byte
short when the comment runs to the end of the input. In `x // hi` with no
trailing newline, `tack --lex` reports the comment as `[2:6]` instead of
`[2:7]`. The comment's value is unaffected.

## Raw strings decode escape sequences

A backquoted string has its escape sequences decoded, with the double quote
as the extra escapable byte, so `` `\"` `` is a one-byte string, `` `\n` `` is
a newline, and `` `\`` `` is an invalid escape that does not end the string.
In Go a backquoted string is raw. Whether Tack's should be is still listed in
`docs/open-decisions.md` under "Escape sequences in raw strings"; this entry
records the current behavior until that is settled.

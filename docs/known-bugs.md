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

## Multiple results report a missing function body

`func f() (int, int) { ... }` is reported as "missing function body" rather
than as an unsupported result list.

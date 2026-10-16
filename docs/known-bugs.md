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

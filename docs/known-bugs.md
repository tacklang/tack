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

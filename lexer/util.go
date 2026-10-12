package lexer

// ValueChecked returns t's value as a T. It panics if the value has another
// type, which means the caller misjudged the token's kind: a compiler bug,
// not a user error.
func ValueChecked[T any](t *Token) T {
	if v, ok := t.val.(T); ok {
		return v
	}
	panic("invalid token value")
}

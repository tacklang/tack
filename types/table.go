package types

// Table describes the types one compilation uses. The type checker creates
// it and records it on the file it checks, and every later stage reads types
// through it, so the name or shape of a type is always looked up in the
// compilation that gave it its ID.
//
// The language has only the basic types today, whose IDs are fixed, so the
// Table has nothing of its own to hold yet. Composite types will be added to
// it as they are first seen, each given the next free ID, so that a type
// written twice gets one ID and == stays type identity.
type Table struct{}

// NewTable returns a Table that knows the basic types.
func NewTable() *Table { return &Table{} }

// Name returns a type's name as a program writes it, for diagnostics. A type
// the Table does not know, including one from another Table, is named
// "invalid" rather than guessed at. A nil Table knows the basic types.
func (*Table) Name(t Type) string {
	if t >= 0 && t < numBasic {
		return basicNames[t]
	}
	return "invalid"
}

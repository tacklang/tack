package ast

import "github.com/tacklang/tack/types"

// Field loops. for f := range fields(T) { ... } is a ForRange whose X is
// a FieldsOf: its body runs once for each field of the struct type T
// whose own mark, whatever its struct's visibility, lets the package
// declaring the function that holds the loop see it, in declaration
// order. T is a type parameter constrained by Data, or a
// struct type that satisfies Data. The loop's Var, f, names the field: it
// is no value, so it can only be used as f.name, the field's name, a
// string, as f.index, its position among the fields listed, an int64, and
// as v.[f], the field f of v, a Selector whose Field is f. The type
// checker checks the body once, as a loop body that may run any number of
// times, with v.[f] of a type parameter of its own constrained by Data,
// FieldsOf.Field; the backend lowers the body once for each field listed,
// with that type parameter bound to the field's type.

// FieldsOf is fields(T), written as a range loop's X: the fields of the
// type T, written as a type, a fields loop ranges over (see ForRange).
// The parser reads fields( as the start of one wherever an expression
// starts, since a call of that name always calls the builtin; the type
// checker reports one anywhere but as a range loop's X.
type FieldsOf struct {
	TypeExpr Expr // T, as written

	// Unrolled lists, for each type Type stands for in the code the
	// backend lowers, the type itself in a function that is not generic
	// and each type argument an instance binds it to in a generic one, in
	// the order the type checker found them, the fields the loop runs its
	// body for. The type checker records it once it has found every
	// instance (see typeck's instance.go), so a backend never computes
	// which fields a package sees. It is left out of the JSON dump.
	Unrolled []Unrolled `json:"-"`

	Span Span // the name fields through the closing parenthesis

	// Type is the type TypeExpr denotes, and Field the type parameter
	// v.[f] has in the loop's body, its own, constrained by Data; both are
	// set by the type checker, and Invalid before or when T was reported.
	Type  types.Type
	Field types.Type

	// Names says the loop's body reads f.name, set by the type checker,
	// so the backend pools the names of the fields it lists.
	Names bool `json:",omitempty"`
}

// Unrolled is the fields a fields loop lists for one type: the type, a
// struct or any other Data type, which has none, and the positions,
// among the struct's fields in declaration order, of those whose own mark
// lets the package declaring the loop's function see them, in order.
type Unrolled struct {
	Fields []int
	Type   types.Type
}

var _ Expr = (*FieldsOf)(nil)

// exprKey marks FieldsOf as an Expr, sealing Expr to this package's types.
func (f *FieldsOf) exprKey() exprKey { return _exprKey }

// astNode marks FieldsOf as a Node, sealing Node to this package's types.
func (f *FieldsOf) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the name fields through the
// closing parenthesis.
func (f *FieldsOf) Spanned() Span { return f.Span }

// FieldsFor returns the positions of the fields the loop lists for typ,
// as recorded in Unrolled, and whether any were recorded for it.
func (f *FieldsOf) FieldsFor(typ types.Type) ([]int, bool) {
	for _, u := range f.Unrolled {
		if u.Type == typ {
			return u.Fields, true
		}
	}
	return nil, false
}

// Record records fields as the fields the loop lists for typ, unless
// some were recorded for it already.
func (f *FieldsOf) Record(typ types.Type, fields []int) {
	if _, ok := f.FieldsFor(typ); !ok {
		f.Unrolled = append(f.Unrolled, Unrolled{Type: typ, Fields: fields})
	}
}

// FieldsLoop returns the FieldsOf s ranges over when s is a fields loop,
// and whether it is one.
func FieldsLoop(s Node) (*ForRange, *FieldsOf, bool) {
	loop, ok := s.(*ForRange)
	if !ok {
		return nil, nil, false
	}
	fo, ok := loop.X.(*FieldsOf)
	return loop, fo, ok
}

package ast

import "github.com/tacklang/tack/types"

// Switches on a type parameter. switch T { case int64: ... } is a Switch
// whose OnType is set: its Tag is an Ident naming T, a type parameter of
// the function the switch is in, or one a case of an enclosing switch on
// a type parameter declares, and each case value is a pattern of a type
// T may stand for, as written: a type named by its name, int64 or
// text.Text; list[E], whose element type E is a name the case declares;
// map[text.Text]V, whose value type V is one; or struct, a StructCase,
// which matches any struct type. Each such E or V is a TypeParam of its
// own, the Def of the name written, declared at the start of the case's
// body as a type switch's variable is, and constrained by Data. The type
// checker records how each value matches in the clause's TypeCases, and
// sets the Switch's Exhaustive when the cases match every type T may
// stand for and there is no default.
//
// In each instance the switch is lowered to the body of the one case
// whose values match the type T stands for, the first in source order,
// the default last, with each E or V the case declares standing for the
// element or value type of that type (see Switch.ArmFor); no other case
// is lowered, and nothing is tested at run time.

// StructCase is the value struct of case struct:, in a switch on a type
// parameter: a pattern matching any struct type.
type StructCase struct {
	Span Span // the keyword struct
}

var _ Expr = (*StructCase)(nil)

// exprKey marks StructCase as an Expr, sealing Expr to this package's types.
func (s *StructCase) exprKey() exprKey { return _exprKey }

// astNode marks StructCase as a Node, sealing Node to this package's types.
func (s *StructCase) astNode() nodeKey { return _nodeKey }

// Spanned returns the span of the keyword struct.
func (s *StructCase) Spanned() Span { return s.Span }

// TypeCaseKind is how a value of a case of a switch on a type parameter
// matches the type the parameter stands for.
type TypeCaseKind int

const (
	// TypeExact matches one type, a TypeCase's Type, exactly.
	TypeExact TypeCaseKind = iota
	// TypeList matches any list type, its element type standing for
	// Type, the type parameter the case declares.
	TypeList
	// TypeMap matches any map type, its value type standing for Type, the
	// type parameter the case declares; a Data map's key is always
	// text.Text.
	TypeMap
	// TypeStruct matches any struct type.
	TypeStruct
)

// TypeCase is how one value of a case of a switch on a type parameter
// matches: its kind, and the type it names, for TypeExact, or the type
// parameter it declares, for TypeList and TypeMap, with a map's key type,
// text.Text, in Key.
type TypeCase struct {
	Kind TypeCaseKind
	Type types.Type
	Key  types.Type
}

// Matches reports whether tc matches typ, a type the switch's type
// parameter stands for, as t describes it, and returns what the type
// parameter tc declares, if any, stands for then.
func (tc TypeCase) Matches(t *types.Table, typ types.Type) (bool, map[types.Type]types.Type) {
	switch tc.Kind {
	case TypeExact:
		return typ == tc.Type, nil
	case TypeList:
		if !t.IsList(typ) {
			return false, nil
		}
		return true, map[types.Type]types.Type{tc.Type: t.Elem(typ)}
	case TypeMap:
		if !t.IsMap(typ) {
			return false, nil
		}
		return true, map[types.Type]types.Type{tc.Type: t.Elem(typ)}
	default:
		return t.IsStruct(typ), nil
	}
}

// ArmFor returns the case of s, a switch on a type parameter checked
// without error, that runs when its type parameter stands for typ, as t
// describes it: the first case, in source order, one of whose values
// matches typ, or the default when none does; and what each type
// parameter that case declares stands for. It returns nil when no case
// runs, which a switch the type checker accepted never has.
func (s *Switch) ArmFor(t *types.Table, typ types.Type) (*CaseClause, map[types.Type]types.Type) {
	for _, cl := range s.Cases {
		for _, tc := range cl.TypeCases {
			if ok, bound := tc.Matches(t, typ); ok {
				return cl, bound
			}
		}
	}
	return s.Default(), nil
}

// CaseTypeName returns the name v, a value of a case of a switch on a
// type parameter, declares a type parameter by: E, written alone as the
// element type of list[E], or V as the value type of map[K]V. It returns
// nil for any other value.
func CaseTypeName(v Expr) *Ident {
	var part Expr
	switch x := v.(type) {
	case *ListType:
		part = x.Elem
	case *MapType:
		part = x.Value
	default:
		return nil
	}
	id, ok := part.(*Ident)
	if !ok || id.Pkg != "" {
		return nil
	}
	return id
}

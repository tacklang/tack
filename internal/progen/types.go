package progen

import (
	"fmt"
	"strings"
)

// Kind says what sort of type a Type is.
type Kind int

// The kinds of type the generator writes.
const (
	KInt Kind = iota
	KBool
	KString
	KStruct
	KArray
	KList
	KMap
	KUnion
	KFunc
	KError
	KView
)

// Type is a Tack type a generated program uses.
type Type struct {
	Int    *IntType    // for KInt
	Struct *StructDecl // for KStruct
	Union  *UnionDecl  // for KUnion
	Elem   *Type       // an array's or a list's element type, a map's value type
	Key    *Type       // a map's key type
	Result *Type       // a function type's result type, nil for none
	Params []*Type     // a function type's parameter types
	Kind   Kind
	Len    int // an array's length
}

// StructDecl is a struct type the program declares, of integer and bool
// fields.
type StructDecl struct {
	Name   string
	Fields []Field
}

// Field is one field of a struct.
type Field struct {
	T    *Type
	Name string
}

// UnionDecl is a union type the program declares: each case has a
// payload of an integer type, or none.
type UnionDecl struct {
	Name  string
	Cases []UnionCase
}

// UnionCase is one case of a union, its payload nil for none.
type UnionCase struct {
	Payload *Type
	Name    string
}

// The types that need no declaration.
var (
	boolType   = &Type{Kind: KBool}
	stringType = &Type{Kind: KString}
)

// intTypeOf returns the Type of the integer type it, one per IntType.
func intTypeOf(it *IntType) *Type {
	return intTypeCache[it]
}

// intTypeCache holds the one Type of each integer type.
var intTypeCache = func() map[*IntType]*Type {
	m := make(map[*IntType]*Type, len(intTypes))
	for _, it := range intTypes {
		m[it] = &Type{Kind: KInt, Int: it}
	}
	return m
}()

// int64Type is the Type of int64.
var int64Type = intTypeOf(Int64)

// String returns the type as Tack writes it.
func (t *Type) String() string {
	switch t.Kind {
	case KInt:
		return t.Int.Name
	case KBool:
		return "bool"
	case KString:
		return "string"
	case KStruct:
		return t.Struct.Name
	case KUnion:
		return t.Union.Name
	case KError:
		return "error"
	default:
		return t.compositeString()
	}
}

// compositeString returns an array, a list, a map, a view or a function
// type as Tack writes it.
func (t *Type) compositeString() string {
	switch t.Kind {
	case KArray:
		return fmt.Sprintf("[%d]%s", t.Len, t.Elem)
	case KList:
		return "list[" + t.Elem.String() + "]"
	case KMap:
		return "map[" + t.Key.String() + "]" + t.Elem.String()
	case KView:
		return "[]" + t.Elem.String()
	default:
		return t.funcString()
	}
}

// funcString returns a function type as Tack writes it.
func (t *Type) funcString() string {
	ps := make([]string, len(t.Params))
	for i, p := range t.Params {
		ps[i] = p.String()
	}
	s := "func(" + strings.Join(ps, ", ") + ")"
	if t.Result != nil {
		s += " " + t.Result.String()
	}
	return s
}

// owned reports whether values of t are owned, a list or a map, which the
// generator never copies, only borrows or passes as mut.
func (t *Type) owned() bool {
	return t.Kind == KList || t.Kind == KMap
}

// sameType reports whether a and b are one type.
func sameType(a, b *Type) bool {
	if a == b {
		return true
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KArray:
		return a.Len == b.Len && sameType(a.Elem, b.Elem)
	case KList:
		return sameType(a.Elem, b.Elem)
	case KMap:
		return sameType(a.Key, b.Key) && sameType(a.Elem, b.Elem)
	case KFunc:
		return a.String() == b.String()
	}
	return false
}

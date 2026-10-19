package ast

// nodeKey is the type astNode returns, the method that seals Node: an empty
// struct, of no use but as the marker's result, whose one value is _nodeKey.
type nodeKey struct{}

var _nodeKey nodeKey

// Node is any element of the syntax tree. The unexported method closes the
// set: only types in this package can be nodes.
type Node interface {
	Spanned() Span
	astNode() nodeKey
}

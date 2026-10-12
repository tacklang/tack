package ast

type nodeKey struct{}

var _nodeKey nodeKey

// Node is any element of the syntax tree. The unexported method closes the
// set: only types in this package can be nodes.
type Node interface {
	Spanned() Span
	astNode() nodeKey
}

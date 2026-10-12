package ast

// Blank is the name _, which binds nothing. It may only stand on the left of
// a MultiVar or MultiAssign, where it discards the result in its position.
const Blank = "_"

package ast

// Blank is the name _, which binds nothing. It may only stand on the left of
// a MultiVar or MultiAssign, where it discards the result in its position,
// as the first of a range loop's two names, or as a Param's name, for a
// parameter that is never read and declares nothing.
const Blank = "_"

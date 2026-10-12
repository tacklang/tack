package ast

// Comment is a line comment kept from the source. Value is its text without
// the leading "//" and with surrounding space trimmed; consecutive comment
// lines arrive as one Comment whose lines are joined by '\n', each trimmed
// the same way, whatever each line's indentation. A blank line between
// comment lines ends the Comment, so the lines on either side of it arrive
// as two.
type Comment struct {
	Value string
	Span  Span
}

// Spanned returns the source range the comment covers.
func (p *Comment) Spanned() Span { return p.Span }

package parser

// Steps returns how many tokens p has looked at (see Parser.steps).
func (p *Parser) Steps() int {
	return p.steps
}

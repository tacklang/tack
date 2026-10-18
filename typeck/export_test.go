package typeck

// ProverWork is how many steps a checker's provers took (see proverWork).
type ProverWork struct {
	Arithmetic, Relations, Indexed int
}

// ProverWork returns how many steps c's provers have taken.
func (c *Checker) ProverWork() ProverWork {
	return ProverWork{c.work.arithmetic, c.work.relations, c.work.indexed}
}

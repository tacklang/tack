package typeck

// ProverWork is how many steps a checker's provers took (see proverWork).
type ProverWork struct {
	Arithmetic, Relations, Indexed, Scanned, Locals, Conditions, Walked, Leaving, Judged int
}

// ProverWork returns how many steps c's provers have taken.
func (c *Checker) ProverWork() ProverWork {
	w := c.work
	return ProverWork{
		w.arithmetic,
		w.relations,
		w.indexed,
		w.scanned,
		w.locals,
		w.conditions,
		w.walked,
		w.leaving,
		w.judged,
	}
}

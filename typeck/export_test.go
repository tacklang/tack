package typeck

// ProverWork is how many steps a checker's provers took (see proverWork).
type ProverWork struct {
	Arithmetic, Relations, Indexed, Scanned, Locals, Conditions, Walked, Leaving, Judged, Ranged int
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
		w.ranged,
	}
}

// CheckEveryBodyTwice makes c check every function body twice, as it
// checks one that binds a method call's or a function value's results,
// so a test can show the first pass leaves no trace (see findCallees).
func (c *Checker) CheckEveryBodyTwice() {
	c.everyBodyTwice = true
}

package typeck

// ProverWork is how many steps a checker's provers took (see proverWork).
type ProverWork struct {
	Arithmetic, Relations, Indexed, Scanned, Locals, Conditions, Walked, Leaving, Judged, Reached, Ranged int
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
		w.reached,
		w.ranged,
	}
}

// CheckEveryBodyTwice makes c check every function body twice, as it
// checks one that binds a method call's or a function value's results,
// so a test can show the first pass leaves no trace (see findCallees).
func (c *Checker) CheckEveryBodyTwice() {
	c.everyBodyTwice = true
}

// ProbeEveryBodyKeepingNothing makes c probe every function body for the
// bounds its loops keep, and put none of them in force, so a test can
// show the probe leaves no other trace (see findInvariants).
func (c *Checker) ProbeEveryBodyKeepingNothing() {
	c.probeEveryBody = true
}

// NeverProbe makes c probe no function body, so that no loop keeps a
// bound (see findInvariants).
func (c *Checker) NeverProbe() {
	c.neverProbe = true
}

// CheckReachIndex makes every query of a fact's endings read their index,
// however few there are, and check it against a scan of them, panicking
// on a difference (see reachOracle), and returns the func that puts back
// what was set. A test calling it must not run in parallel.
func CheckReachIndex() (restore func()) {
	was := reachOracle
	reachOracle = true
	return func() { reachOracle = was }
}

// ReachOracle reports whether every query of a fact's endings is checked
// against a scan of them (see reachOracle), which makes the steps it
// counts grow with the endings times the queries.
func ReachOracle() bool {
	return reachOracle
}

package c_test

import "testing"

// TestCaseExitsExampleIsSanitizerClean runs examples/case-exits: divisions
// after a switch whose cases leave on 0 and -1, in either order, together
// in one case, by a fallthrough, past a default, in a loop, and after an
// else-if chain, at -1, 0, 5 and the smallest value. The sanitizer stops
// the program if a division the compiler accepted reaches C's undefined
// smallest value divided by -1, or a zero divisor.
func TestCaseExitsExampleIsSanitizerClean(t *testing.T) {
	runArithmeticExample(t, "case-exits")
}

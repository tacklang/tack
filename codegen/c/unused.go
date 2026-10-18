package c

// Every Tack function is a static C function, and so is each instance of a
// generic one and each map type's helpers, written whether or not anything
// calls them: a package's functions are all written, though a program
// calls few of std's, and a map type's has, at, put and take helpers are
// written with the type. So are a list's, a map's and a union's free
// helper, written with the type, and the division, shift and slice
// helpers, written for every operator any function uses, a generic one
// with no instance included, and every global is a static variable,
// though only another global's value, which is computed, may read it. C
// warns about a static function or variable nothing names, and about a
// function only its own body names, which a strict build makes an error.
// So each function or global whose name no other function's lowered body
// holds, which is all a static function's name can be named from, is
// declared with __attribute__((unused)), which tells C it may go
// uncalled, and every function some other body names is written as it
// was. A function only an uncalled one names is named all the same, and C
// does not warn about it. A helper only another helper names, as a list's
// free helper names its elements', is declared unused though it is
// called, which changes nothing in what C builds.

// maybeUnused is __attribute__((unused)) and a space, for a function named
// name that no other function's lowered body names, written before its
// declaration, and empty for any other.
func (g *generator) maybeUnused(name string) string {
	for owner := range g.namedBy[name] {
		if owner != name {
			return ""
		}
	}
	return "__attribute__((unused)) "
}

// noteNames records every identifier the lowered body of the function
// named owner holds as named by owner (see maybeUnused).
func (g *generator) noteNames(owner string, body []string) {
	if g.namedBy == nil {
		g.namedBy = make(map[string]map[string]bool)
	}
	for _, line := range body {
		for i := 0; i < len(line); {
			if !isIdentByte(line[i]) {
				i++
				continue
			}
			j := i
			for j < len(line) && isIdentByte(line[j]) {
				j++
			}
			id := line[i:j]
			if g.namedBy[id] == nil {
				g.namedBy[id] = make(map[string]bool)
			}
			g.namedBy[id][owner] = true
			i = j
		}
	}
}

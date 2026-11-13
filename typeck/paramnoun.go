package typeck

import (
	"strconv"

	"github.com/tacklang/tack/ast"
)

// Naming a parameter in a diagnostic. A parameter named _ has no name to
// show, so a message names it by its position instead, counted from 1
// among the parameters written between the parentheses, a method's
// receiver not counted: sink takes its 2nd parameter as own, the 1st
// parameter of extern func take cannot be own. A receiver named _ is the
// receiver. Every other parameter is named as it always was.

// paramNoun names ps[i], a parameter of a signature, where a message
// calls it a parameter: parameter n, or, for one named _, the 2nd
// parameter. recv says ps[0] is a method's receiver.
func paramNoun(ps []*ast.Param, i int, recv bool) string {
	if ps[i].Name != ast.Blank {
		return "parameter " + ps[i].Name
	}
	return "the " + blankPosition(i, recv)
}

// itsParam names ps[i], a parameter of a signature, as its function's:
// its parameter n, or, for one named _, its 2nd parameter. recv says
// ps[0] is a method's receiver.
func itsParam(ps []*ast.Param, i int, recv bool) string {
	if ps[i].Name != ast.Blank {
		return "its parameter " + ps[i].Name
	}
	return "its " + blankPosition(i, recv)
}

// argParamName names ps[i] where a message about a call's argument names
// the parameter it is passed to, as in f takes n as own: its name, or,
// for one named _, its 2nd parameter, or its receiver. ps are the
// parameters the call's arguments are passed to, in order; recv says
// ps[0] is a method's receiver, passed as the first argument, as a
// method expression's call, Pair.first(p), passes it, so the positions
// match those of the same method called on a value.
func argParamName(ps []*ast.Param, i int, recv bool) string {
	if ps[i].Name != ast.Blank {
		return ps[i].Name
	}
	return "its " + blankPosition(i, recv)
}

// blankPosition is how a message names the parameter named _ at index i
// of its signature: receiver, when it is a method's receiver, and
// otherwise its position, 2nd parameter. recv says index 0 is a method's
// receiver, which positions do not count.
func blankPosition(i int, recv bool) string {
	if recv {
		if i == 0 {
			return "receiver"
		}
		i--
	}
	return ordinal(i+1) + " parameter"
}

// ordinal writes n, a positive count, as an English ordinal: 1st, 2nd,
// 3rd, 4th, 11th, 12th, 13th, 21st.
func ordinal(n int) string {
	suffix := "th"
	switch n % 100 {
	case 11, 12, 13:
	default:
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

package progen

import (
	"strconv"
	"strings"
)

// caseSwitch is one clause of an expression switch the generator writes:
// its head, its values' tests, none for the default, its body, and
// whether the body ends in a fallthrough into the next clause.
type caseSwitch struct {
	head  string
	tests []func(*frame) bool
	body  []Stmt
	def   bool
	falls bool
}

// exprSwitchStmt returns an expression switch: on an integer variable,
// whose cases name distinct constants from 0 to 9, a value of every
// integer type, or with no tag, whose cases compare mutable integer
// variables with literals, which no case can repeat since none is a
// constant. It has one to four clauses, maybe a default, written at any
// place among them, and each clause but the last may end in fallthrough,
// entering the next clause's body without testing it. The interpreter
// evaluates the tag once, tries the cases with values in source order,
// each value left to right until one matches, and the default last, and
// runs the bodies from the one that matched for as long as each ends in
// a fallthrough, each body a block of its own whose defers run as it is
// left. It falls back to a declaration when no variable it needs is in
// scope.
func (g *Gen) exprSwitchStmt() Stmt {
	tagged := g.pick(2) == 0
	var tag *Var
	var cands []*Var
	if tagged {
		tag = g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KInt }))
		if tag == nil {
			return g.declStmt()
		}
		tag.Used = true
	} else {
		cands = g.visible(func(v *Var) bool { return v.T.Kind == KInt && v.Mut })
		if len(cands) == 0 {
			return g.declStmt()
		}
	}
	inSwitch := g.inSwitch
	g.inSwitch = true
	defer func() { g.inSwitch = inSwitch }()

	n := 1 + g.pick(4)
	def := -1
	if g.pick(3) == 0 {
		def = g.pick(n)
	}
	used := make(map[int]bool)
	clauses := make([]caseSwitch, n)
	for i := range clauses {
		cl := &clauses[i]
		if i == def {
			cl.head, cl.def = "default:", true
		} else {
			var texts []string
			// A clause's first value is always written, and one after it
			// only while two constants are left for every clause to come.
			for j := 0; j == 0 || j < 3 && len(used) < 10-2*(n-i) && g.pick(3) == 0; j++ {
				text, test := g.caseValue(tag, cands, used)
				texts = append(texts, text)
				cl.tests = append(cl.tests, test)
			}
			cl.head = "case " + strings.Join(texts, ", ") + ":"
		}
		g.push()
		mark := int64(200 + i)
		first := simple(func(f *frame) { f.m.observe(mark) }, "obs("+strconv.FormatInt(mark, 10)+")")
		cl.body = append(append([]Stmt{first}, g.blockIn()...), g.pop()...)
		if i < n-1 && g.pick(2) == 0 {
			cl.falls = true
			cl.body = append(cl.body, Stmt{
				Lines: []string{"fallthrough"},
				Exec:  func(*frame) ctrl { return cNormal },
			})
		}
	}
	return switchCases(tag, clauses, def)
}

// caseValue returns the text of one value of a case, and its test: a
// constant from 0 to 9 that used does not hold yet, which it then holds,
// compared with tag, or, with no tag, one of cands, mutable integer
// variables, compared with a literal of its type. The caller leaves used
// a constant to take.
func (g *Gen) caseValue(tag *Var, cands []*Var, used map[int]bool) (string, func(*frame) bool) {
	if tag != nil {
		k := g.pick(10)
		for used[k] {
			k = (k + 1) % 10
		}
		used[k] = true
		t, kb := tag.T.Int, uint64(k) //nolint:gosec // small
		return strconv.Itoa(k), func(f *frame) bool { return compare(t, f.get(tag).Bits, t, kb) == 0 }
	}
	v := cands[g.pick(len(cands))]
	v.Used = true
	lit := g.intLit(v.T.Int)
	op := cmpOps[g.pick(len(cmpOps))]
	t, le := v.T.Int, lit.Eval
	return v.Name + " " + op + " " + lit.Text, func(f *frame) bool {
		return holds(op, compare(t, f.get(v).Bits, t, le(f).Bits))
	}
}

// switchCases returns the switch on tag, or with no tag when tag is nil,
// with clauses, the default at index def, or -1 for none.
func switchCases(tag *Var, clauses []caseSwitch, def int) Stmt {
	head := "switch {"
	if tag != nil {
		head = "switch " + tag.Name + " {"
	}
	lines := []string{head}
	for _, cl := range clauses {
		lines = append(lines, cl.head)
		for _, s := range cl.body {
			for _, l := range s.Lines {
				lines = append(lines, "    "+l)
			}
		}
	}
	lines = append(lines, "}")
	return Stmt{Lines: lines, Exec: func(f *frame) ctrl {
		start := def
	tried:
		for i, cl := range clauses {
			for _, test := range cl.tests {
				if test(f) {
					start = i
					break tried
				}
			}
		}
		if start < 0 {
			return cNormal
		}
		for _, cl := range clauses[start:] {
			if c := execBlock(f, cl.body); c != cNormal || !cl.falls {
				return c
			}
		}
		return cNormal
	}}
}

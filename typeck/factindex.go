package typeck

import (
	"fmt"
	"iter"
	"slices"
)

// The facts in force, c.active, change only when assume pushes facts or
// release pops them, each counted in c.factGen; supposing swaps in a copy
// that differs only in what each fact's drop and ends say. Most queries
// of them ask about one subject, and a function with n facts in force and
// n such queries would read n facts for each of them. So when more than
// scanLimit facts are in force, a query reads c.factsBy instead: for each
// subject, the positions in c.active of the facts about it, in increasing
// order, built from one scan of c.active the first time it is asked
// after the facts change, and kept until they change again. Pushing and
// popping, which a long chain of && does far more often than it asks,
// costs nothing more.
//
// A fact the index missed could leave a proof unmade, or, through what
// a missing fact would have said to the contrary, made where it should
// not be, so when the tests are built with the tag tackfactoracle (see
// factOracle) every query reads the index, however few facts are in
// force, and checks it against a scan of every one.

// scanLimit is how many facts in force a query reads one by one before
// it reads them through c.factsBy, which costs a map to build.
const scanLimit = 32

// factsAbout returns the facts in force, held or dropped, whose subject
// is k, in the order they were put in force, which is the order of
// c.active. Each fact read counts as one step (see proverWork).
func (c *Checker) factsAbout(k factKey) iter.Seq[fact] {
	if len(c.active) <= scanLimit && !factOracle {
		return func(yield func(fact) bool) {
			for _, f := range c.active {
				c.work.scanned++
				if f.about == k && !yield(f) {
					return
				}
			}
		}
	}
	at := c.factIndex()[k]
	if factOracle {
		c.checkFactIndex(k, at)
	}
	return func(yield func(fact) bool) {
		for _, i := range at {
			c.work.scanned++
			if !yield(c.active[i]) {
				return
			}
		}
	}
}

// factIndex returns c.factsBy, building it again from c.active when the
// facts in force have changed since it was last built.
func (c *Checker) factIndex() map[factKey][]int {
	if c.factsBy != nil && c.factsGen == c.factGen {
		return c.factsBy
	}
	idx := make(map[factKey][]int)
	for i, f := range c.active {
		c.work.scanned++
		idx[f.about] = append(idx[f.about], i)
	}
	c.factsBy, c.factsGen = idx, c.factGen
	return idx
}

// checkFactIndex panics when at, the positions c.factsBy holds for the
// subject k, are not those of the facts about k a scan of every fact in
// force finds, in the same order.
func (c *Checker) checkFactIndex(k factKey, at []int) {
	var want []int
	for i, f := range c.active {
		if f.about == k {
			want = append(want, i)
		}
	}
	if !slices.Equal(at, want) {
		panic(fmt.Sprintf("typeck: the fact index holds %v about %v, a scan finds %v", at, k, want))
	}
}

package progen

import (
	"slices"
	"strconv"
	"strings"
)

// The helper declarations a program gets when it uses them: the
// comparators sort.by takes, an interface every type satisfies, and a
// generic function reversing a list of any element type with swap.
const (
	ascDecl = `func asc(a int64, b int64) bool {
    return a < b
}
`
	descDecl = `func desc(a int64, b int64) bool {
    return a > b
}
`
	anyDecl = `type Any interface {
}
`
	revDecl = `func rev[T Any](mut l list[T]) {
    mut i := 0
    mut j := len(l) - 1
    for 0 <= i && i < j && j < len(l) {
        swap(mut l, i, j)
        next := i + 1
        prev := j - 1
        i = next
        j = prev
    }
}
`
)

// stdStmt calls the standard library, or the generic helper: std/bytes
// on a string's bytes, std/sort on a list, or rev.
func (g *Gen) stdStmt() Stmt {
	switch g.pick(6) {
	case 0:
		return g.bytesStmt()
	case 1:
		return g.sortStmt()
	case 2:
		return g.parseStmt()
	case 3:
		return g.textStmt()
	case 4:
		return g.failureStmt()
	}
	return g.revStmt()
}

// bytesOp is a function of std/bytes on two strings' bytes, and what it
// gives, as the interpreter computes it.
type bytesOp struct {
	eval func(s, sub string) Value
	t    *Type
	name string
}

// bytesOps are the functions of std/bytes the generator calls; sub is
// never empty.
var bytesOps = []bytesOp{
	{name: "index", t: int64Type, eval: func(s, sub string) Value {
		return Value{Bits: uint64(int64(strings.Index(s, sub)))} //nolint:gosec // -1 is meant
	}},
	{name: "count", t: int64Type, eval: func(s, sub string) Value {
		return Value{Bits: uint64(strings.Count(s, sub))} //nolint:gosec // small
	}},
	{name: "has_prefix", t: boolType, eval: func(s, sub string) Value {
		return Value{Bool: strings.HasPrefix(s, sub)}
	}},
	{name: "has_suffix", t: boolType, eval: func(s, sub string) Value {
		return Value{Bool: strings.HasSuffix(s, sub)}
	}},
	{name: "equal", t: boolType, eval: func(s, sub string) Value { return Value{Bool: s == sub} }},
}

// bytesStmt observes a std/bytes function of a string and a nonempty
// literal, each passed as its bytes, []uint8(s).
func (g *Gen) bytesStmt() Stmt {
	g.imports["bytes"] = true
	op := bytesOps[g.pick(len(bytesOps))]
	s := g.stringExpr()
	sub := g.word() + "a"
	se := s.Eval
	return g.observe(Expr{
		Text: "bytes." + op.name + "([]uint8(" + s.Text + "), []uint8(" + strconv.Quote(sub) + "))",
		T:    op.t,
		Eval: func(f *frame) Value { return op.eval(se(f).S, sub) },
	})
}

// sortStmt sorts a writable list with sort.by, ascending or descending,
// or observes whether a list is sorted with sort.is_sorted.
func (g *Gen) sortStmt() Stmt {
	desc := g.pick(2) == 0
	cmp := "asc"
	if desc {
		cmp = "desc"
	}
	if g.pick(2) == 0 {
		return g.isSortedStmt(cmp, desc)
	}
	l := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KList && g.writable(v) }))
	if l == nil {
		return g.newCollection()
	}
	l.Used = true
	g.useSort(cmp)
	return simple(func(f *frame) {
		lst := *f.get(l).List
		slices.SortFunc(lst, func(a, b Value) int { return order(a, b, desc) })
	}, "sort.by(mut "+l.Name+", "+cmp+")")
}

// isSortedStmt observes sort.is_sorted of a list, with the comparator cmp.
func (g *Gen) isSortedStmt(cmp string, desc bool) Stmt {
	l := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KList && !g.noLists }))
	if l == nil {
		return g.newCollection()
	}
	l.Used = true
	g.useSort(cmp)
	return g.observe(Expr{
		Text: "sort.is_sorted(" + l.Name + ", " + cmp + ")", T: boolType,
		Eval: func(f *frame) Value {
			lst := *f.get(l).List
			return Value{
				Bool: slices.IsSortedFunc(lst, func(a, b Value) int { return order(a, b, desc) }),
			}
		},
	})
}

// useSort records that the program imports std/sort and declares the
// comparator cmp, asc or desc.
func (g *Gen) useSort(cmp string) {
	g.imports["sort"] = true
	if cmp == "desc" {
		g.extra("desc", descDecl)
	} else {
		g.extra("asc", ascDecl)
	}
}

// extra records that the program needs the declaration decl, named name.
func (g *Gen) extra(name, decl string) {
	if _, ok := g.extras[name]; !ok {
		g.extras[name] = decl
		g.order = append(g.order, name)
	}
}

// order compares two int64 elements, a before b by -1, as asc orders
// them, or as desc does when desc is set.
func order(a, b Value, desc bool) int {
	c := cmpSigned(asInt64(a.Bits), asInt64(b.Bits))
	if desc {
		return -c
	}
	return c
}

// revStmt reverses a writable list with the generic rev.
func (g *Gen) revStmt() Stmt {
	l := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KList && g.writable(v) }))
	if l == nil {
		return g.newCollection()
	}
	l.Used = true
	g.extra("Any", anyDecl)
	g.extra("rev", revDecl)
	return simple(func(f *frame) { slices.Reverse(*f.get(l).List) }, "rev(mut "+l.Name+")")
}

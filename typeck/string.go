package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// stringElement checks the index of s[i], where s is a string, a view or
// a list, and returns elem, the type of what it reads: a string's byte, a
// uint8, or the view's or list's element. The index must be an integer
// proven in range, unless s[i] is being bound with ok; a constant index is
// checked against a string literal's length here, and a negative one is
// never in range. A view's or a list's length is never known, as a string
// parameter's is not, so its index is proven as one into such a string
// is. write says s[i] is being assigned, which only a list's element can
// be, and only matters for what the error suggests.
func (c *Checker) stringElement(ix *ast.Index, idx, elem types.Type, write bool) types.Type {
	switch {
	case !c.indexType(ix, idx):
	case c.checkedIndex == ix:
		if v, known := c.knownValue(ix.Index); known && c.constantOutside(ix.X, v) {
			c.reportOutside(ix, v)
			return types.Invalid
		}
	default:
		c.requireStringInRange(ix, write)
	}
	return elem
}

// literalLen returns the length of s when it is a string literal. Advice
// names a literal's length, and a variable's as len(s) whether or not it is
// known; proofs use extentOf.
func literalLen(s ast.Expr) (int64, bool) {
	lit, ok := s.(*ast.StrLit)
	if !ok {
		return 0, false
	}
	return int64(len(lit.Value)), true
}

// constantOutside reports whether the constant index v is out of range for
// the string s whatever s holds: it is negative, or s is a known string,
// a literal or an immutable variable holding one, or a slice at known
// bounds, it is not shorter than.
func (c *Checker) constantOutside(s ast.Expr, v constant.Int) bool {
	if n, ok := c.extentOf(s); ok {
		return !c.constantInRange(v, n)
	}
	return v.IsNegative()
}

// reportOutside reports the constant index v of ix as out of range, naming
// the string's length when it is known.
func (c *Checker) reportOutside(ix *ast.Index, v constant.Int) {
	if n, ok := c.extentOf(ix.X); ok {
		c.errorf(ix.Index.Spanned(), "index %s is out of range for a string of %d %s",
			v, n, plural(int(n), "byte"))
		return
	}
	what := "a string"
	switch typ := ast.TypeOf(ix.X); {
	case c.table.IsView(typ):
		what = "a view"
	case c.table.IsList(typ):
		what = "a list"
	}
	c.errorf(ix.Index.Spanned(), "index %s is out of range for %s", v, what)
}

// stringIndexProven reports whether the index of s[i] is known to be in
// range for the string s where it is used: a constant below a known
// string's length, or below the least length the facts in force give s, or
// the relations in force do (see relates); or a subject, a variable or a
// path below an immutable one, at least 0, by its bounds or the relations,
// that is below a known string's length, below the least length of s, or
// below len(s) itself by a fact or the relations.
func (c *Checker) stringIndexProven(ix *ast.Index) bool {
	if v, ok := c.knownValue(ix.Index); ok {
		if n, isKnown := c.extentOf(ix.X); isKnown {
			return c.constantInRange(v, n)
		}
		return !v.IsNegative() && (v.Cmp(c.minLen(ix.X)) < 0 || c.lenAbove(ix.X, v))
	}
	if !c.isSubject(ix.Index) {
		return false
	}
	known := c.intervalOf(ix.Index)
	if (!known.hasLo || known.lo.IsNegative()) && !c.atLeast(ix.Index) {
		return false
	}
	return c.belowStringLen(ix.Index, known, ix.X)
}

// belowStringLen reports whether the subject i, of which known is what the
// facts say, is below the length of the string s: a known string's
// constant length, the least length the facts give s, or len(s) itself,
// by its bounds, a fact or the relations in force.
func (c *Checker) belowStringLen(i ast.Expr, known interval, s ast.Expr) bool {
	if n, isKnown := c.extentOf(s); isKnown {
		return known.hasHi && known.hi.Cmp(constant.FromInt64(n)) < 0 ||
			c.atMostBy(i, nil, constant.FromInt64(n-1))
	}
	if known.hasHi && known.hi.Cmp(c.minLen(s)) < 0 {
		return true
	}
	return c.belowLen(i, s) || c.belowLenBy(i, s)
}

// belowLenBy reports whether the relations in force put the subject i
// below the length of s, a string, view or list subject.
func (c *Checker) belowLenBy(i, s ast.Expr) bool {
	ti, okI := c.queryTerm(i)
	ts, okS := c.lenTerm(s)
	return okI && okS && c.relates(ti, ts, constant.FromInt64(-1))
}

// lenAbove reports whether the relations in force put the length of s, a
// string, view or list subject, above the known value v.
func (c *Checker) lenAbove(s ast.Expr, v constant.Int) bool {
	ts, ok := c.lenTerm(s)
	if !ok {
		return false
	}
	least, ok := v.Add(constant.FromInt64(1))
	if ok {
		least, ok = least.Neg()
	}
	return ok && c.relates(zeroTerm(), ts, least)
}

// minLen returns the least length the facts in force give the string s,
// or the list or view, when s is a subject, a variable or a path below an
// immutable one, and 0 otherwise: a lower bound on len(s), or one when
// len(s) is known not to be zero. A list's length is bounded by a
// successful append to it too (see appendedLen).
func (c *Checker) minLen(s ast.Expr) constant.Int {
	k, ok := c.subjectKey(s)
	if !ok {
		return constant.Int{}
	}
	var least interval
	for _, f := range c.active {
		if f.about == k && c.holds(f) {
			least = least.narrow(f)
		}
	}
	least = c.appendedLen(k, least)
	one := constant.FromInt64(1)
	switch {
	case least.hasLo && least.lo.Cmp(one) >= 0:
		return least.lo
	case least.nonzero:
		return one
	default:
		return constant.Int{}
	}
}

// belowLen reports whether a fact in force says the subject i is below the
// length of the string subject s.
func (c *Checker) belowLen(i, s ast.Expr) bool {
	ik, okI := c.subjectKey(i)
	sk, okS := c.subjectKey(s)
	if !okI || !okS {
		return false
	}
	for _, f := range c.active {
		if f.about == ik && f.lenOf == sk && c.holds(f) {
			return true
		}
	}
	return false
}

// requireStringInRange reports an index into a string that is not proven
// to be in range: a constant that is out of range whatever the string
// holds, or anything else no fact covers. The message says what check
// would prove it, and, unless write says the element is being assigned,
// the checked form.
func (c *Checker) requireStringInRange(ix *ast.Index, write bool) {
	if v, ok := c.knownValue(ix.Index); ok && c.constantOutside(ix.X, v) {
		c.reportOutside(ix, v)
		return
	}
	if c.stringIndexProven(ix) {
		return
	}
	msg, notes := c.stringIndexError(ix, write)
	c.errorNotes(ix.Index.Spanned(), notes, "%s", msg)
}

// stringIndexError returns the error for the index of s[i] that is not
// proven in range, and the notes that explain it. A constant index gets the
// check on the length of s that would prove it, when s is a subject, a
// variable or a path below an immutable one, or a literal; a subject index
// gets indexError's, against len(s) or a literal's length, and only the
// checked form when s is neither, since only a subject's length can be
// guarded. A mutable global s gets the note at its declaration that no
// check proves it, and a field of a mutable variable or a mut parameter
// the note at a guard on its length that the guard proves nothing. The
// checked form is suggested only for a read: an element being assigned,
// write, has none.
func (c *Checker) stringIndexError(ix *ast.Index, write bool) (string, []diag.Diagnostic) {
	bound := c.stringBound(ix.X)
	proven := func() bool { return c.stringIndexProven(ix) }
	var msg string
	var notes []diag.Diagnostic
	if v, isConst := c.knownValue(ix.Index); isConst {
		msg = "index " + v.String() + " may be out of range"
		if bound != "" {
			msg += "; check " + bound + " > " + v.String() + " first"
		}
		if c.isSubject(ix.X) {
			notes = c.explain(c.lengthProof(ix.X, proven))
		}
	} else {
		msg, notes = c.indexError(ix, bound, func(i ast.Expr) []diag.Diagnostic {
			n, known := c.extentOf(ix.X)
			unit := "byte"
			if typ := ast.TypeOf(ix.X); c.table.IsView(typ) || c.table.IsList(typ) {
				unit = "element"
			}
			e := c.newExtent(ix.X, unit, n, known)
			explained := c.explain(c.indexProof(i, e, proven))
			return append(explained, c.indexRelationNotes(i, ix.X)...)
		})
	}
	notes = append(notes, c.indexedNotes(ix.X)...)
	if write {
		return msg, notes
	}
	return msg + orUse(
		"v, ok := ...["+c.indexName(ix.Index)+"]",
		strings.HasSuffix(msg, " first"),
	), notes
}

// stringBound returns the bound a check on an index into the string s
// would compare it with: a literal's length, or len(s) when s is a name or
// a subject, a variable or a path below an immutable one, other than a
// mutable global, which no check proves. It is empty otherwise.
func (c *Checker) stringBound(s ast.Expr) string {
	if n, ok := literalLen(s); ok {
		return fmt.Sprint(n)
	}
	if _, isGlobal := c.mutableGlobal(s); isGlobal {
		return ""
	}
	if id, ok := s.(*ast.Ident); ok {
		return "len(" + id.Name + ")"
	}
	if c.isSubject(s) {
		return "len(" + c.nameOf(s) + ")"
	}
	return ""
}

// indexedNotes returns the notes on why the length of the string s cannot
// be proven: at its declaration, when s is a mutable global, or at the
// guard on its length, when s is a field of a mutable variable or a mut
// parameter.
func (c *Checker) indexedNotes(s ast.Expr) []diag.Diagnostic {
	if v, isGlobal := c.mutableGlobal(s); isGlobal {
		return globalNote(v)
	}
	if _, isField := s.(*ast.Selector); isField && !c.isSubject(s) {
		return c.fieldNotes(s)
	}
	return nil
}

// indexName returns how an error names an index: its known value, its
// name or path, or ... for anything else.
func (c *Checker) indexName(e ast.Expr) string {
	if v, ok := c.knownValue(e); ok {
		return v.String()
	}
	return c.operandName(e)
}

package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// stringElement checks the index of s[i], where s is a string, and returns
// the type of the byte it reads, uint8. The index must be an integer proven
// in range, unless s[i] is being bound with ok; a constant index is checked
// against a string literal's length here, and a negative one is never in
// range.
func (c *Checker) stringElement(ix *ast.Index, idx types.Type) types.Type {
	switch {
	case idx == types.Invalid:
	case !types.IsInteger(idx):
		c.errorf(ix.Index.Spanned(), "index must be an integer, got %s", c.typeName(idx))
	case c.checkedIndex == ix:
		if v, known := c.knownValue(ix.Index); known && c.constantOutside(ix.X, v) {
			c.reportOutside(ix, v)
			return types.Invalid
		}
	default:
		c.requireStringInRange(ix)
	}
	return types.Uint8
}

// literalLen returns the length of s when it is a string literal. Advice
// names a literal's length, and a variable's as len(s) whether or not it is
// known; proofs use knownLen.
func literalLen(s ast.Expr) (int64, bool) {
	lit, ok := s.(*ast.StrLit)
	if !ok {
		return 0, false
	}
	return int64(len(lit.Value)), true
}

// constantOutside reports whether the constant index v is out of range for
// the string s whatever s holds: it is negative, or s is a known string,
// a literal or an immutable variable holding one, it is not shorter than.
func (c *Checker) constantOutside(s ast.Expr, v constant.Int) bool {
	if n, ok := c.knownLen(s); ok {
		return !c.constantInRange(v, n)
	}
	return v.IsNegative()
}

// reportOutside reports the constant index v of ix as out of range, naming
// the string's length when it is known.
func (c *Checker) reportOutside(ix *ast.Index, v constant.Int) {
	if n, ok := c.knownLen(ix.X); ok {
		c.errorf(ix.Index.Spanned(), "index %s is out of range for a string of %d %s",
			v, n, plural(int(n), "byte"))
		return
	}
	c.errorf(ix.Index.Spanned(), "index %s is out of range for a string", v)
}

// stringIndexProven reports whether the index of s[i] is known to be in
// range for the string s where it is used: a constant below a known
// string's length, or below the least length the facts in force give s; or
// a subject, a variable or a path below an immutable one, at least 0 that
// is below a known string's length, below the least length of s, or below
// len(s) itself by a fact.
func (c *Checker) stringIndexProven(ix *ast.Index) bool {
	if v, ok := c.knownValue(ix.Index); ok {
		if n, isKnown := c.knownLen(ix.X); isKnown {
			return c.constantInRange(v, n)
		}
		return !v.IsNegative() && v.Cmp(c.minLen(ix.X)) < 0
	}
	if !c.isSubject(ix.Index) {
		return false
	}
	known := c.intervalOf(ix.Index)
	if !known.hasLo || known.lo.IsNegative() {
		return false
	}
	return c.belowStringLen(ix.Index, known, ix.X)
}

// belowStringLen reports whether the subject i, of which known is what the
// facts say, is below the length of the string s: a known string's
// constant length, the least length the facts give s, or len(s) itself.
func (c *Checker) belowStringLen(i ast.Expr, known interval, s ast.Expr) bool {
	if n, isKnown := c.knownLen(s); isKnown {
		return known.hasHi && known.hi.Cmp(constant.FromInt64(n)) < 0
	}
	if known.hasHi && known.hi.Cmp(c.minLen(s)) < 0 {
		return true
	}
	return c.belowLen(i, s)
}

// minLen returns the least length the facts in force give the string s,
// when s is a subject, a variable or a path below an immutable one, and 0
// otherwise: a lower bound on len(s), or one when len(s) is known not to
// be zero.
func (c *Checker) minLen(s ast.Expr) constant.Int {
	k, ok := c.subjectKey(s)
	if !ok {
		return constant.Int{}
	}
	var least interval
	for _, f := range c.active {
		if f.about == k && f.inForce() {
			least = least.narrow(f)
		}
	}
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
		if f.about == ik && f.lenOf == sk && f.inForce() {
			return true
		}
	}
	return false
}

// requireStringInRange reports an index into a string that is not proven
// to be in range: a constant that is out of range whatever the string
// holds, or anything else no fact covers. The message says what check
// would prove it.
func (c *Checker) requireStringInRange(ix *ast.Index) {
	if v, ok := c.knownValue(ix.Index); ok && c.constantOutside(ix.X, v) {
		c.reportOutside(ix, v)
		return
	}
	if c.stringIndexProven(ix) {
		return
	}
	msg, notes := c.stringIndexError(ix)
	c.errorNotes(ix.Index.Spanned(), notes, "%s", msg)
}

// stringIndexError returns the error for the index of s[i] that is not
// proven in range, and the notes that explain it. A constant index gets the
// check on the length of s that would prove it, when s is a subject, a
// variable or a path below an immutable one, or a literal; a subject index
// gets indexError's, against len(s) or a literal's length, and only the
// checked form when s is neither, since only a subject's length can be
// guarded.
func (c *Checker) stringIndexError(ix *ast.Index) (string, []diag.Diagnostic) {
	var bound string
	if n, ok := literalLen(ix.X); ok {
		bound = fmt.Sprint(n)
	} else if s, ok := ix.X.(*ast.Ident); ok {
		bound = "len(" + s.Name + ")"
	} else if c.isSubject(ix.X) {
		bound = "len(" + c.nameOf(ix.X) + ")"
	}
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
			n, known := c.knownLen(ix.X)
			e := c.newExtent(ix.X, "byte", n, known)
			return c.explain(c.indexProof(i, e, proven))
		})
	}
	return msg + orUse(
		"v, ok := ...["+c.indexName(ix.Index)+"]",
		strings.HasSuffix(msg, " first"),
	), notes
}

// indexName returns how an error names an index: its known value, its
// name or path, or ... for anything else.
func (c *Checker) indexName(e ast.Expr) string {
	if v, ok := c.knownValue(e); ok {
		return v.String()
	}
	return c.operandName(e)
}

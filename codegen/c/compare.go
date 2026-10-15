package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Two integers of any types compare by their values in Tack, where C's
// usual arithmetic conversions would turn a negative signed operand into a
// large unsigned one, so -1 < 0u is false in C. intComparison writes each
// comparison in a form that gives the values' answer on every target,
// without undefined behavior or a warning:
//
//   - two operands of one type compare as they are;
//   - two unsigned ones compare as uint64_t, which holds every value of
//     both, the target-width types' included;
//   - two signed ones, or a signed one and an unsigned one of at most 32
//     bits, compare as int64_t, which holds every value of both;
//   - a signed one and a 64-bit or target-width unsigned one, for which no
//     C type holds every value of both, test the sign first: s < u is
//     (s < 0 || (uint64_t)s < (uint64_t)u), where the cast is reached only
//     when s is not negative, and so keeps its value.
//
// Each operand of two different types is cast explicitly, even where C
// would convert it the same way, since clang warns that a comparison of a
// narrow operand with a constant outside its type's range, as a uint8
// with int64(-2), is always false, and the cast hides the operand's type
// from that warning. The last form reads the signed operand twice and may
// skip the unsigned one, so every operand that is not pure is held in a
// temporary first, in order (see needsOnce and hoist).

// intComparison renders l op r, the comparison op of integers of types xt
// and yt rendered as l and r, so that it compares their values, wrapped in
// one pair of parentheses. Either operand may be read more than once, or
// not at all, when needsOnce says so.
func intComparison(op ast.BinaryOp, xt, yt types.Type, l, r string) string {
	switch {
	case xt == yt:
		return fmt.Sprintf("(%s %s %s)", l, op, r)
	case types.IsUnsigned(xt) && types.IsUnsigned(yt):
		return fmt.Sprintf("((uint64_t)%s %s (uint64_t)%s)", l, op, r)
	case !needsOnce(xt, yt):
		return fmt.Sprintf("((int64_t)%s %s (int64_t)%s)", l, op, r)
	case types.IsUnsigned(yt):
		return signFirst(op, l, r)
	default:
		return signFirst(flip(op), r, l)
	}
}

// needsOnce reports whether intComparison of integers of types xt and yt
// reads an operand more than once or may skip one: one is signed and the
// other an unsigned type no signed type holds every value of, uint64 or a
// target-width one.
func needsOnce(xt, yt types.Type) bool {
	switch {
	case types.IsUnsigned(xt) == types.IsUnsigned(yt):
		return false
	case types.IsUnsigned(xt):
		return types.MaxBits(xt) == 64
	default:
		return types.MaxBits(yt) == 64
	}
}

// signFirst renders s op u, for s of a signed type and u of an unsigned
// one, by testing s's sign first: a negative s is less than every u, and
// a non-negative one compares with u as a uint64_t, which holds both
// values.
func signFirst(op ast.BinaryOp, s, u string) string {
	switch op {
	case ast.Eq, ast.Gt, ast.Ge:
		return fmt.Sprintf("(%s >= 0 && (uint64_t)%s %s (uint64_t)%s)", s, s, op, u)
	default: // ast.Ne, ast.Lt, ast.Le
		return fmt.Sprintf("(%s < 0 || (uint64_t)%s %s (uint64_t)%s)", s, s, op, u)
	}
}

// flip returns the operator that compares the same way with its operands
// swapped: a < b is b > a.
func flip(op ast.BinaryOp) ast.BinaryOp {
	switch op {
	case ast.Lt:
		return ast.Gt
	case ast.Le:
		return ast.Ge
	case ast.Gt:
		return ast.Lt
	case ast.Ge:
		return ast.Le
	default:
		return op
	}
}

// isIntComparison reports whether bin compares two integers.
func isIntComparison(bin *ast.Binary) bool {
	return bin.Op.IsComparison() && types.IsInteger(ast.TypeOf(bin.X)) &&
		types.IsInteger(ast.TypeOf(bin.Y))
}

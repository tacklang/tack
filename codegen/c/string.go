package c

import (
	"fmt"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// stringCompareName is the helper every comparison of two strings calls.
const stringCompareName = "tack_compare_strings"

// emitStringCompare writes tack_compare_strings, which compares two strings
// byte by byte, each byte as unsigned, and gives -1, 0 or 1 as the first is
// less than, equal to or greater than the second; of two strings where one
// is a prefix of the other, the shorter is less. It loops rather than
// calling memcmp or strcmp: strcmp stops at a zero byte, which a Tack
// string may hold, and memcmp is undefined on the null pointer a zero
// string element holds, even for no bytes. A string's bytes are read
// through an unsigned char pointer, so bytes from 0x80 up compare above
// every ASCII byte.
func (g *generator) emitStringCompare() {
	fmt.Fprintf(&g.buf, "static int %s(tack_string a, tack_string b) {\n", stringCompareName)
	g.buf.WriteString("    int64_t n = a.len < b.len ? a.len : b.len;\n")
	g.buf.WriteString("    for (int64_t i = 0; i < n; i++) {\n")
	g.buf.WriteString("        if (a.ptr[i] != b.ptr[i]) {\n")
	g.buf.WriteString("            return a.ptr[i] < b.ptr[i] ? -1 : 1;\n")
	g.buf.WriteString("        }\n")
	g.buf.WriteString("    }\n")
	g.buf.WriteString("    return (a.len > b.len) - (a.len < b.len);\n")
	g.buf.WriteString("}\n\n")
}

// comparesStrings reports whether the program compares two strings, with
// any operator, keys a map by strings, whose helpers compare its keys, or
// compares an array or a struct, among compared, that holds a string,
// whose equality helper then compares strings in turn.
func (g *generator) comparesStrings(compared map[types.Type]bool) bool {
	if slices.Contains(g.keyTypes(), types.String) {
		return true
	}
	for typ := range compared {
		for _, part := range g.parts(typ) {
			if part == types.String {
				return true
			}
		}
	}
	found := false
	g.walk(func(n ast.Node) bool {
		if bin, ok := n.(*ast.Binary); ok && bin.Op.IsComparison() &&
			ast.TypeOf(bin.X) == types.String {
			found = true
		}
		if typ, ok := switchCompares(n); ok && typ == types.String {
			found = true
		}
		return !found
	})
	return found
}

// stringComparison renders l op r, two strings compared with op, as the
// result of tack_compare_strings compared with zero, parenthesized as every
// comparison is.
func stringComparison(op ast.BinaryOp, l, r string) string {
	return fmt.Sprintf("(%s(%s, %s) %s 0)", stringCompareName, l, r, op)
}

package typeck

import (
	"fmt"
	"math/bits"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Size limits. No value may hold more than types.MaxValueSize bytes, 2^31,
// as the C backend lays it out (see types.Table.Size), and all of a
// program's globals together no more than types.MaxGlobalsSize, 2^30.
//
// A type too large is reported once, where it is made too large: at the
// array type expression [n]T first met, or at a struct's or union's
// declaration, and only when none of its parts, an array's element, a
// struct's fields or a union's payloads, is too large itself, since the
// part was reported where it was made. Every later use of the type is
// left alone, but for a use after one in a damaged body whose report was
// dropped (see checkDamagedBody), which is checked as if first.
//
// Sizes can only be known once every struct's fields and every union's
// cases are, so an array type met while they are being resolved is held
// in pendingSizes until checkStructs is done (see checkTypeSizes); from
// then on, an array type is checked as soon as it is met.
//
// The globals are counted in declaration order across the program:
// package by package in the loader's order, every package after the
// packages it imports and the main package last, each package's files in
// the byte order of their names, and each file in source order. The first
// global that takes the total past the limit is reported, once; a global
// whose type is too large is reported as such and not counted.

// sizeSite is a type whose size is to be checked, and where it is made:
// an array type expression's span, or a struct's or union's name.
type sizeSite struct {
	span ast.Span
	typ  types.Type
}

// noteArraySize checks the size of the array type typ, written at span,
// or holds it until every struct's and union's size can be known.
func (c *Checker) noteArraySize(typ types.Type, span ast.Span) {
	if !c.sizesKnown {
		c.pendingSizes = append(c.pendingSizes, sizeSite{typ: typ, span: span})
		return
	}
	c.checkSize(sizeSite{typ: typ, span: span})
}

// checkTypeSizes checks the sizes of the array types held until now, in
// the order they were met, then of every struct and union type, in
// declaration order. Every array type met after it is checked at once.
func (c *Checker) checkTypeSizes() {
	c.sizesKnown = true
	for _, site := range c.pendingSizes {
		c.checkSize(site)
	}
	c.pendingSizes = nil
	for _, typ := range c.structOrder {
		td := c.structs[typ]
		if td == nil {
			td = c.unions[typ]
		}
		c.checkSize(sizeSite{typ: typ, span: td.NameSpan})
	}
}

// checkSize reports site's type at site's span when it is too large and
// none of its parts is, unless it has already been checked. A check whose
// report a damaged body dropped does not count (see forgetSizesAfter).
func (c *Checker) checkSize(site sizeSite) {
	typ := site.typ
	if _, ok := c.sizeChecked[typ]; ok {
		return
	}
	c.sizeChecked[typ] = site.span
	size := c.table.Size(typ)
	if !size.TooLarge() {
		return
	}
	for _, part := range c.sizeParts(typ) {
		if c.table.Size(part).TooLarge() {
			return
		}
	}
	what := "array type " + c.typeName(typ)
	if !c.table.IsArray(typ) {
		what = "type " + c.typeName(typ)
	}
	c.errorf(site.span, "%s is too large: it holds %s bytes, more than the 2^31 a value may hold",
		what, sizeText(size))
}

// forgetSizesAfter forgets the size checks made in fn at or after first,
// its first bad statement, whose reports checkDamagedBody drops, so that
// the next use of each type, elsewhere, is checked and reported in their
// place.
func (c *Checker) forgetSizesAfter(fn *ast.Func, first *ast.BadStmt) {
	for typ, span := range c.sizeChecked {
		if afterBad(span, fn, first) {
			delete(c.sizeChecked, typ)
		}
	}
}

// sizeParts returns the types typ is made of whose sizes add up to its
// own: an array's element type, a struct's fields' types, or a union's
// payloads' types.
func (c *Checker) sizeParts(typ types.Type) []types.Type {
	switch {
	case c.table.IsArray(typ):
		return []types.Type{c.table.Elem(typ)}
	case c.table.IsUnion(typ):
		var parts []types.Type
		for _, uc := range c.table.Cases(typ) {
			if uc.Payload {
				parts = append(parts, uc.Type)
			}
		}
		return parts
	default:
		var parts []types.Type
		for _, f := range c.table.Fields(typ) {
			parts = append(parts, f.Type)
		}
		return parts
	}
}

// sizeText writes the bytes size holds, or, when that does not fit a
// uint64, that it is more than the largest that does.
func sizeText(size types.Size) string {
	if size.Overflow {
		return fmt.Sprintf("more than %d", uint64(1<<64-1))
	}
	return fmt.Sprint(size.Bytes)
}

// checkGlobalsSize adds up the sizes of decls' globals, in order, and
// reports the first one that takes the total past types.MaxGlobalsSize.
// A global whose type is too large, or unknown, is not counted.
func (c *Checker) checkGlobalsSize(decls []ast.Decl) {
	var total uint64
	for _, decl := range decls {
		v, ok := decl.(*ast.Var)
		if !ok || v.Type == types.Invalid {
			continue
		}
		size := c.table.Size(v.Type)
		if size.TooLarge() {
			continue
		}
		sum, carry := bits.Add64(total, size.Bytes, 0)
		if carry != 0 {
			sum = 1<<64 - 1
		}
		total = sum
		if total > types.MaxGlobalsSize {
			restore := c.enter(v)
			c.errorf(v.NameSpan, "global %s makes this program's globals hold %d bytes, "+
				"more than the 2^30 a program's globals may hold", v.Name, total)
			restore()
			return
		}
	}
}

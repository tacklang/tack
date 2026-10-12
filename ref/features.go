// features.go is a top-to-bottom tour of the Go programming language as
// defined by "The Go Programming Language Specification", language version
// go1.27 (May 26, 2026), compiled with go1.27.1.
//
// One function per spec chapter, in spec order; subsections are helpers
// called from the chapter function. Output goes through the builtins print
// and println only, which write to standard error.
//
// Rules:
//   - no imports (not even unsafe), so qualified identifiers, the unsafe
//     package, and size/alignment guarantees are described, not shown;
//   - every runtime panic is caught with recover so the program exits 0;
//   - concurrency is synchronized with channels only;
//   - the file is gofmt-formatted except for spellings gofmt would
//     canonicalize away (0O, 0B, 0X, 1E6, 0123i, redundant parentheses,
//     an empty statement), which are the point of those lines.

/* A general comment. It may span lines.
   It cannot nest: /* is just text in here. */

package main

// ===========================================================================
// Program initialization: package-level variables are initialized before
// init and main, in dependency order rather than declaration order.
// ===========================================================================

var initLog []string // no initializer: zero value, ready before anything else

func trace(name string, v int) int {
	initLog = append(initLog, name)
	return v
}

var (
	initA = trace("initA", initC+1) // depends on initC, so it waits for it
	initB = trace("initB", 2)       // no dependencies: first ready, first initialized
	initC = trace("initC", 10)
)

// Multiple assignment from one call initializes both variables together.
var initD, initE = pair()

func pair() (int, int) { initLog = append(initLog, "initD,initE"); return 4, 5 }

// init cannot be referred to from anywhere; there may be many per package.
func init() {
	initLog = append(initLog, "init()")
}

// ===========================================================================
// Program execution: main takes no arguments and returns no value. When it
// returns, the program exits without waiting for other goroutines.
// ===========================================================================

func main() {
	introductionAndNotation()
	sourceCodeRepresentation()
	lexicalElements()
	constants()
	variables()
	types()
	propertiesOfTypesAndValues()
	blocks()
	declarationsAndScope()
	expressions()
	statements()
	builtinFunctions()
	packages()
	programInitializationAndExecution()
	errorsChapter()
	runTimePanics()
	systemConsiderations()
	println()
	println("done")
}

func section(name string) {
	println()
	println("==== " + name + " ====")
}

func sub(name string) { println("-- " + name) }

// ===========================================================================
// Spec: Introduction / Notation
// ===========================================================================

// The grammar is written in EBNF; nothing to execute. The "…" in spec
// examples is informal shorthand, not Go syntax.
func introductionAndNotation() {
	section("Introduction / Notation")
	println("spec grammar is EBNF; no runtime behavior")
}

// ===========================================================================
// Spec: Source code representation (Characters, Letters and digits)
// ===========================================================================

func sourceCodeRepresentation() {
	section("Source code representation")

	sub("Characters")
	// Source is UTF-8. Each code point is distinct: upper and lower case
	// letters are different characters, and so are composed forms.
	s := "héllo, 世界"
	println("bytes:", len(s), "code points:", len([]rune(s)))

	sub("Letters and digits")
	// "_" counts as a lowercase letter. Any Unicode letter (category L) may
	// start an identifier; Unicode decimal digits (Nd) may follow.
	_x := 1
	عدد := 2 // Arabic letters are category Lo
	x٣ := 3  // ٣ is ARABIC-INDIC DIGIT THREE (Nd), legal after the first letter
	π := 4
	println(_x, عدد, x٣, π)
}

// ===========================================================================
// Spec: Lexical elements
// ===========================================================================

func lexicalElements() {
	section("Lexical elements")
	lexComments()
	lexTokensAndSemicolons()
	lexIdentifiersKeywordsOperators()
	lexIntegerLiterals()
	lexFloatingPointLiterals()
	lexImaginaryLiterals()
	lexRuneLiterals()
	lexStringLiterals()
}

func lexComments() {
	sub("Comments")
	// A general comment with no newline acts like a space.
	a := 1 /* inline */ + 2
	// A general comment containing a newline acts like a newline, so it
	// triggers semicolon insertion after the preceding token.
	b := a /*
	 */
	println(a, b) // a line comment runs to the end of the line
}

func lexTokensAndSemicolons() {
	sub("Tokens and Semicolons")
	// A semicolon is inserted after a line's final token if it is an
	// identifier, a basic literal, one of break continue fallthrough return,
	// or one of ++ -- ) ] }. So a binary operator must end a broken line.
	total := 1 +
		2 +
		3
	x, y := 1, 2
	f := func() int { x++; y++; return x + y } // explicit semicolons; one may be omitted before a closing ) or }
	println(total, f())
}

// Exported identifiers start with an upper-case letter (Lu). Within a single
// package export status changes nothing observable; it governs access from
// other packages.
var (
	ExportedName   = "exported"
	unexportedName = "unexported"
)

func lexIdentifiersKeywordsOperators() {
	sub("Identifiers / Keywords / Operators and punctuation")
	println(ExportedName, unexportedName)
	// The 25 keywords, all used somewhere in this file:
	//   break case chan const continue default defer else fallthrough for
	//   func go goto if import interface map package range return select
	//   struct switch type var
	// (import appears only in this comment: imports are out of scope.)
	//
	// Operators and punctuation:
	//   +  &  += &= && == != ( )
	//   -  |  -= |= || <  <= [ ]
	//   *  ^  *= ^= <- >  >= { }
	//   /  << /= <<= ++ =  := , ;
	//   %  >> %= >>= -- !  ... . :
	//      &^    &^=    ~
	println("25 keywords; operators listed in comment")
}

func lexIntegerLiterals() {
	sub("Integer literals")
	println(42, 4_2)                     // "_" may appear between digits or after a base prefix
	println(0o600, 0o_600, 0o600, 0o600) // leading 0 alone is legacy octal
	println(0xBadFace, 0xBad_Face, 0x_67_7a_2f_cc_40_c6)
	println(0b1011, 0b1011)
	// Untyped integer constants have arbitrary precision; this only needs to
	// fit when it is finally converted.
	const big = 170141183460469231731687303715884105727 // 2^127-1
	println(big>>100, uint64(big>>64))
}

func lexFloatingPointLiterals() {
	sub("Floating-point literals")
	println(0., 72.40, 072.40) // 072.40 is decimal 72.40, not octal
	println(2.71828, 1.e+0, 6.67428e-11, 1e6, .25, .12345e+5)
	println(1_5., 0.15e+0_2)
	// Hex floats need a p exponent (power of two); the mantissa is hex.
	println(0x1p-2, 0x2.p10, 0x1.Fp+0, 0x.8p-0, 0x_1FFFp-16)
}

func lexImaginaryLiterals() {
	sub("Imaginary literals")
	// For backward compatibility, a decimal-digit-only imaginary literal with
	// a leading 0 is decimal: 0123i == 123i.
	println(0i, 123i, 0o123i, 0x0i, 0b101i)
	println(2.71828i, 1.e+0i, 6.67428e-11i, 1e6i, .25i, .12345e+5i, 0x1p-2i)
	println(1_000i == 1000i, 123i == 123i)
}

func lexRuneLiterals() {
	sub("Rune literals")
	println('a', 'ä', '本', '\t', '\000', '\007', '\377', '\x07', '\xff', 'ዤ', '\U00101234')
	println('\a', '\b', '\f', '\n', '\r', '\t', '\v', '\\', '\'')
	// '\"' is illegal in a rune literal; '\'' is illegal in a string literal.
	// Octal \nnn must be < 256; \u and \U must be valid Unicode code points.
	println('"')
}

func lexStringLiterals() {
	sub("String literals")
	raw := `abc\n
	\n` // raw: no escapes, may span lines; carriage returns are discarded
	interp := "\n\"\\\a\b\f\r\t\v"
	println(len(raw), len(interp))
	// The same text spelled five ways:
	println("日本語" == "日本\U00008a9e",
		"日本語" == "\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e",
		"日本語" == `日本語`,
		"日本語" == "日本語")
	// \x and octal escapes produce bytes; \u produces UTF-8 encoded runes,
	// so "\xff" is one (invalid UTF-8) byte while "ÿ" is two bytes.
	println(len("\xff"), len("ÿ"))
}

// ===========================================================================
// Spec: Constants
// ===========================================================================

func constants() {
	section("Constants")
	// Kinds: boolean, rune, integer, floating-point, complex, string.
	const (
		cb = true
		cr = 'x'
		ci = 1 << 62
		cf = 1.5
		cc = 2 + 3i
		cs = "s"
	)
	println(cb, cr, ci, cf, cc, cs)

	// Untyped constants get their default type only when a type is needed:
	// bool, rune (int32), int, float64, complex128, string.
	x := cr // x is rune
	var f32 float32 = cf
	println(x, f32)

	// Typed constants must be representable in their type; untyped ones
	// have arbitrary precision (a compiler must keep at least 256-bit
	// integers and 256-bit float mantissas, rounding beyond that).
	const typed int8 = 100
	const huge = 1 << 100   // fine: untyped
	const back = huge >> 98 // == 4, exact
	const third = 1.0 / 3   // rounded to the internal precision, not float64
	println(typed, back, third*3 == 1)

	// An untyped float constant may be used as an int if it is integral.
	var i int = 2.0
	println(i)

	// Builtins applied to constant operands can yield constants.
	const l = len("hello")
	const re = real(cc)
	println(l, re)
}

// ===========================================================================
// Spec: Variables
// ===========================================================================

type (
	shape  interface{ area() float64 }
	square struct{ s float64 }
)

func (q square) area() float64 { return q.s * q.s }

func variables() {
	section("Variables")
	// A variable of interface type has a static type (the interface) and a
	// dynamic type (the type of the value stored at run time).
	var v shape // static type shape, dynamic type none: v == nil
	println(v == nil)
	v = square{2} // dynamic type square
	println(v.area())

	// Structured variables: array, slice, and struct elements and fields are
	// variables themselves and can be addressed individually.
	arr := [3]int{}
	p := &arr[1]
	*p = 7
	println(arr[1])
	// Declaration forms are in "Variable declarations" and "Short variable
	// declarations"; zero values in "The zero value".
}

// ===========================================================================
// Spec: Types
// ===========================================================================

func types() {
	section("Types")
	// A type may be named (predeclared, defined, type parameter) or a type
	// literal; parentheses around a type are permitted.
	var a int
	var b []string
	println(a, len(b))

	typesBoolean()
	typesNumeric()
	typesString()
	typesArray()
	typesSlice()
	typesStruct()
	typesPointer()
	typesFunction()
	typesInterface()
	typesMap()
	typesChannel()
}

type MyBool bool

func typesBoolean() {
	sub("Boolean types")
	var mb MyBool = 1 < 2 // comparisons yield untyped bool, assignable to any boolean type
	println(mb, bool(mb) && true)
}

func typesNumeric() {
	sub("Numeric types")
	var (
		u8   uint8   = 1<<8 - 1
		u16  uint16  = 1<<16 - 1
		u32  uint32  = 1<<32 - 1
		u64  uint64  = 1<<64 - 1
		i8   int8    = -1 << 7
		i16  int16   = -1 << 15
		i32  int32   = -1 << 31
		i64  int64   = -1 << 63
		f32  float32 = 1.5
		f64  float64 = 2.5
		c64  complex64
		c128 complex128 = 1 + 2i
		by   byte       = 'b' // byte is an alias for uint8
		ru   rune       = 'r' // rune is an alias for int32
		ui   uint             // implementation-specific size: 32 or 64 bits
		in   int              // same size as uint
		up   uintptr          // large enough to hold a pointer's bits
	)
	println(u8, u16, u32, u64)
	println(i8, i16, i32, i64)
	println(f32, f64, c64, c128)
	println(by, ru, ui, in, up)
	// Predeclared numeric types are all distinct (except the aliases), so
	// mixing them requires explicit conversion.
	sum := int64(i32) + i64
	var sameType uint8 = by // byte and uint8 are identical
	println(sum, sameType)
}

func typesString() {
	sub("String types")
	s := "héllo"
	// Strings are immutable; len counts bytes; s[i] is a byte. &s[i] is
	// illegal and s[i] = x does not compile.
	println(len(s), s[1], s[2])
	const cs = "const"
	println(len(cs)) // len of a constant string is a compile-time constant
}

func typesArray() {
	sub("Array types")
	var a [3]int
	b := a // arrays are values: assignment copies all elements
	b[0] = 1
	println(a[0], b[0], len(a))
	var grid [2][3]int // multi-dimensional: an array of arrays
	grid[1][2] = 5
	println(len(grid), len(grid[0]), grid[1][2])
	// The length is part of the type: [3]int and [4]int are different types.
	// An array type cannot contain itself, directly or indirectly, except
	// through pointer, slice, map, function, or channel types.
	type node struct {
		next *node
		kids [2]*node
	}
	var n node
	println(n.next == nil)
}

func typesSlice() {
	sub("Slice types")
	arr := [5]int{0, 1, 2, 3, 4}
	s := arr[1:3] // shares arr's storage
	s[0] = 10
	println(arr[1], len(s), cap(s)) // cap runs to the end of the underlying array
	var nilSlice []int
	empty := []int{}
	println(nilSlice == nil, empty == nil, len(nilSlice), len(empty))
	m := make([]int, 2, 10) // len 2, cap 10
	println(len(m), cap(m))
	// Slices of slices can be jagged: inner lengths vary.
	jag := [][]int{{1}, {1, 2, 3}}
	println(len(jag[0]), len(jag[1]))
}

// Struct features: blank fields, multiple names per line, tags, embedded
// fields as T, *T, and promotion.
type Inner struct {
	ID   int
	Name string
}

func (in Inner) Describe() string { return "Inner:" + in.Name }
func (in *Inner) Rename(n string) { in.Name = n }
func (in Inner) Shadowed() string { return "Inner.Shadowed" }

type Base struct{ Level int }

func (b *Base) Bump() { b.Level++ }

type Outer struct {
	Inner          // embedded field; its field name is Inner
	*Base          // embedded pointer; field name is Base
	x, y   int     // two fields, one line
	_      float32 // blank field: padding, never accessible
	Name   string  `json:"name" db:"n"` // tag: shadows Inner.Name (depth 0 beats depth 1)
	Tagged int     "k:\"v\""            // any string literal is a legal tag; convention is key:"value"
}

func (o Outer) Shadowed() string { return "Outer.Shadowed" } // shadows the promoted method

func typesStruct() {
	sub("Struct types")
	o := Outer{Inner: Inner{ID: 1, Name: "in"}, Base: &Base{}, Name: "out"}
	println(o.Name, o.Inner.Name) // shallower depth wins
	println(o.ID)                 // promoted field from Inner
	println(o.Describe())         // promoted method from Inner
	o.Rename("renamed")           // promoted *Inner method: o is addressable
	println(o.Inner.Name)
	o.Bump() // promoted through *Base
	println(o.Level)
	println(o.Shadowed(), o.Inner.Shadowed())

	// Anonymous struct types: identical if fields, types, tags, and
	// embedding match in order.
	pt := struct{ X, Y int }{1, 2}
	var pt2 struct{ X, Y int } = pt
	println(pt2.X, pt == pt2)

	// Two embedded fields providing the same name at the same depth make
	// that selector ambiguous; it is an error only if used.
	type A struct{ V int }
	type B struct{ V int }
	type AB struct {
		A
		B
	}
	ab := AB{A{1}, B{2}}
	println(ab.A.V, ab.B.V) // ab.V would not compile
}

func typesPointer() {
	sub("Pointer types")
	x := 1
	p := &x
	pp := &p // pointer to pointer
	**pp = 2
	println(x, *p)
	arr := [3]int{1, 2, 3}
	pa := &arr
	println(pa[1], len(pa)) // indexing and len auto-dereference pointers to arrays
	var np *int
	println(np == nil)
}

func typesFunction() {
	sub("Function types")
	var f0 func()
	var f1 func(x int) int = func(x int) int { return x * 2 }
	var f2 func(a, _ int, z float32) bool = func(a, _ int, z float32) bool { return float32(a) < z }
	var f3 func(prefix string, values ...int) int = func(p string, vs ...int) int { return len(vs) }
	var f4 func(int, int, float64) (float64, *[]int) = func(int, int, float64) (float64, *[]int) { return 0, nil }
	var f5 func(n int) func(p *int) = func(n int) func(p *int) { return func(p *int) { *p = n } }
	println(f0 == nil, f1(3), f2(1, 0, 2), f3("x", 1, 2, 3))
	r, ptr := f4(1, 2, 3)
	println(r, ptr == nil)
	v := 0
	f5(9)(&v)
	println(v)
}

func typesInterface() {
	sub("Interface types")
	interfacesBasic()
	interfacesEmbedded()
	interfacesGeneral()
	interfacesImplementing()
}

// Basic interfaces: method sets only.
type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}
type Writer interface {
	Write(p []byte) (n int, err error)
	Close() error
}

type file struct{ data []byte }

func (f *file) Read(p []byte) (int, error)  { return copy(p, f.data), nil }
func (f *file) Write(p []byte) (int, error) { f.data = append(f.data, p...); return len(p), nil }
func (f *file) Close() error                { return nil }

func interfacesBasic() {
	var r Reader = &file{data: []byte("hi")}
	buf := make([]byte, 4)
	n, _ := r.Read(buf)
	println("basic:", n)
	var e interface{} = 3 // empty interface: every type implements it
	var a any = "s"       // any is an alias for interface{}
	_, isInt := e.(int)
	println(isInt, a == "s")
}

// Embedded interfaces: the method set is the union. Close appears in both
// with identical signatures, which is allowed (Go 1.14).
type ReadWriter interface {
	Reader
	Writer
}

func interfacesEmbedded() {
	var rw ReadWriter = &file{}
	rw.Write([]byte("abc"))
	var r Reader = rw // ReadWriter's method set includes Reader's
	buf := make([]byte, 8)
	n, _ := r.Read(buf)
	println("embedded:", n)
}

// General interfaces: type elements (T, ~T, unions) define type sets. They
// may only be used as constraints, not as variable types.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}
type (
	Float  interface{ ~float32 | ~float64 }
	Number interface {
		Integer | Float // a union of interfaces (without methods)
	}
)

type Stringish interface {
	~string
	Len() int // type set: types with underlying string AND a Len method
}
type myStr string

func (s myStr) Len() int { return len(s) }

func lenOf[S Stringish](s S) int { return s.Len() + len(s) }

func sumAll[N Number](xs ...N) N {
	var t N
	for _, x := range xs {
		t += x
	}
	return t
}

func interfacesGeneral() {
	type celsius float64 // ~float64 admits defined types whose underlying type is float64
	println("general:", sumAll(1, 2, 3), sumAll[celsius](1.5, 2), lenOf(myStr("ab")))
	// comparable is the predeclared interface for strictly comparable types
	// (plus, since Go 1.20, ordinary interfaces as type arguments).
	println(isEq(1, 1), isEq("a", "b"))
}

func isEq[T comparable](a, b T) bool { return a == b }

func interfacesImplementing() {
	// T implements interface I if T is in I's type set: for a basic
	// interface, T's method set includes all of I's methods. Implementation
	// is implicit, there is no "implements" declaration.
	var w Writer = &file{} // *file has Write and Close
	println("implementing:", w != nil)
	// file (non-pointer) does not implement Writer: its methods have
	// pointer receivers, so they are not in file's method set.
	// Compile-time assertion idiom:
	var _ Writer = (*file)(nil)
}

func typesMap() {
	sub("Map types")
	m := map[string]int{"a": 1}
	m["b"] = 2
	v, ok := m["missing"] // missing key yields the zero value
	println(len(m), v, ok)
	// Keys must be comparable: structs, arrays, interfaces, pointers, chans.
	type key struct{ x, y int }
	grid := map[key]string{{1, 2}: "p"} // key type elided in the literal
	arrKey := map[[2]int]bool{{1, 2}: true}
	ifKey := map[any]int{1: 1, "1": 2} // dynamic key types must be comparable
	println(grid[key{1, 2}], arrKey[[2]int{1, 2}], ifKey["1"])
	var nm map[string]int
	println(nm == nil, nm["x"], len(nm)) // reading a nil map is fine; writing panics
	big := make(map[int]int, 100)        // capacity hint
	println(len(big))
}

func typesChannel() {
	sub("Channel types")
	ch := make(chan int, 1) // bidirectional, buffered
	var send chan<- int = ch
	var recv <-chan int = ch
	send <- 1
	println(<-recv, cap(ch))
	// <- associates with the leftmost chan possible.
	var c1 chan<- chan int   // send-only channel of (chan int)
	var c2 chan<- <-chan int // send-only channel of (<-chan int)
	var c3 <-chan <-chan int // receive-only channel of (<-chan int)
	var c4 chan (<-chan int) // parentheses force the grouping
	println(c1 == nil, c2 == nil, c3 == nil, c4 == nil)
	var nilCh chan int
	println(len(nilCh), cap(nilCh)) // a nil channel is never ready
}

// ===========================================================================
// Spec: Properties of types and values
// ===========================================================================

func propertiesOfTypesAndValues() {
	section("Properties of types and values")
	propRepresentation()
	propUnderlying()
	propIdentity()
	propAssignability()
	propRepresentability()
	propMethodSets()
}

func propRepresentation() {
	sub("Representation of values")
	// Arrays and structs are self-contained: copies are independent.
	a := [2]int{1, 2}
	b := a
	b[0] = 9
	// Slices, maps, pointers, funcs, chans refer to shared data.
	s := []int{1, 2}
	t := s
	t[0] = 9
	m := map[int]int{}
	n := m
	n[1] = 1
	println(a[0], s[0], len(m))
}

type (
	A1 = string
	A2 = A1
	B1 string
	B2 B1
	B3 []B1
	B4 B3
)

func propUnderlying() {
	sub("Underlying types")
	// Underlying of string, A1, A2, B1, B2 is string; of B3 and B4 is []B1.
	var b2 B2 = "x"
	var s string = string(b2) // conversion allowed: identical underlying types
	var b4 B4 = B4(B3{"y"})
	println(s, len(b4))
}

func propIdentity() {
	sub("Type identity")
	// A named type is always different from any other type; type literals
	// are identical when structurally identical.
	type T0 []string
	type T1 []string
	var x []string
	var t0 T0 = x // OK: []string is unnamed, underlying types identical
	// var t1 T1 = t0 would fail: T0 and T1 are different named types.
	t1 := T1(t0)
	println(len(t1))
	// func(int, float64) *T0 and func(x int, y float64) *[]string differ:
	// *T0 is not *[]string. Parameter names do not matter.
	var f func(a int) int = func(b int) int { return b }
	println(f(1))
}

func propAssignability() {
	sub("Assignability")
	ch := make(chan int)
	var r <-chan int = ch     // bidirectional chan to directional: OK
	var e error = errorf("x") // implements interface
	var p *int = nil          // nil to pointer/func/slice/map/chan/interface
	var f float64 = 1         // representable untyped constant
	type Celsius float64
	var c Celsius = 1.5
	// var g float64 = c would fail: both named, different types.
	println(r != nil, e.Error(), p == nil, f, c)
}

func propRepresentability() {
	sub("Representability")
	// A constant x is representable by T if x is in T's value set (with
	// rounding for floats). Examples: 'a' as byte, 1e3 as int, 1<<63-1 as
	// int64, 1e-1000 rounds to 0 for float64.
	var b byte = 'a'
	var i int = 1e3
	var i64 int64 = 1<<63 - 1
	var f64 float64 = 1e-1000
	var c complex64 = 1 // integral constant as complex
	println(b, i, i64, f64, c)
	// Not representable (compile errors): byte(256), int8(-129), int(1.5).
}

type Counter struct{ n int }

func (c Counter) Get() int { return c.n }
func (c *Counter) Inc()    { c.n++ }

type (
	Getter interface{ Get() int }
	Incer  interface{ Inc() }
)

func propMethodSets() {
	sub("Method sets")
	// Method set of T: value-receiver methods. Method set of *T: all of T's
	// methods plus pointer-receiver methods.
	var c Counter
	var g Getter = c   // Counter has Get
	var g2 Getter = &c // *Counter has Get too
	var i Incer = &c   // only *Counter has Inc; Counter does not implement Incer
	i.Inc()
	println(g.Get(), g2.Get(), c.n) // g holds a copy made before Inc
	// Calling a pointer method on an addressable value is shorthand for (&c).Inc().
	c.Inc()
	println(c.n)
	// Map elements are not addressable: m[k].Inc() does not compile.
	m := map[string]Counter{"a": {}}
	tmp := m["a"]
	tmp.Inc()
	m["a"] = tmp
	println(m["a"].Get())
	// An interface type's method set is its interface.
}

// ===========================================================================
// Spec: Blocks
// ===========================================================================

func blocks() {
	section("Blocks")
	// Universe block (predeclared ids) > package block > file block >
	// function block > explicit {} blocks and the implicit blocks of if,
	// for, switch, and each switch/select clause.
	x := "function"
	{
		x := "explicit block"
		println(x)
	}
	if x := "if block"; true {
		println(x)
	}
	for x := 0; x < 1; x++ {
		x := "for body is nested inside the for statement's implicit block"
		println(x)
	}
	switch y := 1; y {
	case 1:
		x := "case clause is its own implicit block"
		println(x)
	}
	println(x)
}

// ===========================================================================
// Spec: Declarations and scope
// ===========================================================================

func declarationsAndScope() {
	section("Declarations and scope")
	declLabelScopes()
	declBlankIdentifier()
	declPredeclared()
	declExportedAndUniqueness()
	declConstants()
	declIota()
	declTypes()
	declTypeParameters()
	declVariables()
	declShortVariables()
	declFunctions()
	declMethods()
}

func declLabelScopes() {
	sub("Label scopes")
	// Labels have function-body scope and their own namespace: they never
	// conflict with non-label identifiers. Unused labels are an error.
	x := 0
x:
	for {
		x++
		if x == 3 {
			break x
		}
	}
	println(x)
}

func declBlankIdentifier() {
	sub("Blank identifier")
	_ = 1                        // discard a value
	_, b := 1, 2                 // discard one of many
	for _, v := range []int{5} { // discard the index
		println(b, v)
	}
	_ = sideEffect("blank var decl still evaluates its initializer")
	type _ struct{} // declarations with _ introduce no binding
}

func sideEffect(s string) int { println(s); return 0 }

func declPredeclared() {
	sub("Predeclared identifiers")
	// Types: any bool byte comparable complex64 complex128 error float32
	//   float64 int int8 int16 int32 int64 rune string uint uint8 uint16
	//   uint32 uint64 uintptr
	// Constants: true false iota. Zero value: nil.
	// Functions: append cap clear close complex copy delete imag len make
	//   max min new panic print println real recover
	// They live in the universe block, so they can be shadowed:
	{
		len := func(string) int { return -1 }
		true := false
		println(len("abc"), true)
	}
	println(len("abc"), true)
}

func declExportedAndUniqueness() {
	sub("Exported identifiers / Uniqueness of identifiers")
	// Identifiers are unique if spelled differently (or are unexported and
	// in different packages). Case matters, so these are distinct:
	Value, value := 1, 2
	println(Value, value, ExportedName)
}

func declConstants() {
	sub("Constant declarations")
	const Pi float64 = 3.14159265358979323846
	const zero = 0.0 // untyped floating-point constant
	const (
		size int64 = 1024
		eof        = -1 // untyped integer constant
	)
	const a, b, c = 3, 4, "foo" // untyped int, int, string
	const u, v float32 = 0, 3
	// Within a parenthesized list, an omitted expression list repeats the
	// previous one (with its type), textually.
	const (
		r0 = "repeat"
		r1
		r2
	)
	println(Pi, zero, size, eof, a, b, c, u, v, r0, r1, r2)
}

type Weekday int

const (
	Sunday Weekday = iota // typed iota: every following constant is a Weekday
	Monday
	Tuesday
)

type ByteSize float64

const (
	_           = iota             // skip 0
	KB ByteSize = 1 << (10 * iota) // expression repeated with iota = 1, 2, ...
	MB
	GB
)

func declIota() {
	sub("Iota")
	const (
		c0 = iota // 0
		c1 = iota // 1
		c2        // 2: implicit repetition of "= iota"
	)
	const (
		a = 1 << iota // 1
		b = 1 << iota // 2
		c = 3         // iota is 2 here but unused
		d = 1 << iota // 8
	)
	const (
		u         = iota * 42 // untyped int
		v float64 = iota * 42 // float64
		w         = iota * 42 // repeats "float64 = iota * 42"
	)
	const x = iota // 0: each const block (or lone const) resets iota
	const y = iota // 0
	const (
		bit0, mask0 = 1 << iota, 1<<iota - 1 // same iota for the whole line
		bit1, mask1                          // iota = 1
		_, _                                 // iota = 2, skipped
		bit3, mask3                          // iota = 3
	)
	const (
		seven = 7
		late  = iota // 1: iota is the index of the ConstSpec, not a counter of iota uses
	)
	println(c0, c1, c2, a, b, c, d)
	println(u, v, w, x, y)
	println(bit0, mask0, bit1, mask1, bit3, mask3, seven, late)
	println(Sunday, Monday, Tuesday, KB, MB, GB)
}

// Alias declarations bind a new name to an existing type: same type.
type Celsius = float64

// Generic alias (Go 1.24): an alias with type parameters.
type Set[K comparable] = map[K]struct{}

// Type definitions create a new, distinct named type. It does not inherit
// methods of the source type, but does keep the method set of an
// interface type and the methods promoted by embedded fields.
type Temp float64

func (t Temp) F() float64 { return float64(t)*9/5 + 32 }

type Temp2 Temp // has no method F

type Wrapped struct{ Temp } // embedding keeps F via promotion

// Generic type definition; the type is instantiated before use.
type Tree[T interface{ ~int | ~string }] struct {
	left, right *Tree[T]
	value       T
}

func (t *Tree[T]) Insert(v T) *Tree[T] {
	if t == nil {
		return &Tree[T]{value: v}
	}
	if v < t.value { // < is allowed: every type in T's type set is ordered
		t.left = t.left.Insert(v)
	} else {
		t.right = t.right.Insert(v)
	}
	return t
}

func (t *Tree[T]) Walk(yield func(T) bool) bool {
	return t == nil || (t.left.Walk(yield) && yield(t.value) && t.right.Walk(yield))
}

// All adapts Walk to the iterator signature (no result) for range-over-func.
func (t *Tree[T]) All(yield func(T) bool) { t.Walk(yield) }

func declTypes() {
	sub("Type declarations: Alias declarations / Type definitions")
	var c Celsius = 36.6
	var f float64 = c // same type, no conversion needed
	s := Set[string]{"a": {}}
	var m map[string]struct{} = s // generic alias instantiates to an identical type
	println(f, len(m))

	t := Temp(100)
	println(t.F(), Wrapped{t}.F())
	t2 := Temp2(t)
	println(Temp(t2).F()) // convert back to reach F

	var tr *Tree[int]
	for _, v := range []int{5, 2, 8, 1} {
		tr = tr.Insert(v)
	}
	tr.Walk(func(v int) bool { print(v, " "); return true })
	println()

	// Local type declarations, including local generic aliases.
	type local struct{ v int }
	type pairOf[P any] = [2]P
	var pr pairOf[int] = [2]int{1, 2}
	println(local{3}.v, pr[1])
}

// Constraint with a method and type element; also a self-referential
// constraint (P's constraint mentions P).
type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 | ~string
}

type Comparer[T any] interface{ Compare(T) int }

type version int

func (v version) Compare(o version) int { return int(v) - int(o) }

func maxBy[T Comparer[T]](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if x.Compare(m) > 0 {
			m = x
		}
	}
	return m
}

// A constraint written inline; [P any] is shorthand for [P interface{any}].
func firstOf[S interface{ ~[]E }, E any](s S) E { return s[0] }

// Parsing ambiguity: "type T[P *C] …" parses as an array declaration, so a
// trailing comma or an interface wrapper is required.
type (
	PtrBox[P *int,]              struct{ p P }
	PtrBox2[P interface{ *int }] struct{ p P }
)

type BlankParam[_ any] struct{} // blank type parameter

func declTypeParameters() {
	sub("Type parameter declarations: Type constraints / Satisfying a type constraint")
	println(maxBy(version(1), version(3), version(2)))
	println(firstOf([]string{"x", "y"}))
	n := 7
	println(*PtrBox[*int]{&n}.p, *PtrBox2[*int]{&n}.p)
	_ = BlankParam[string]{}

	// Satisfying vs implementing: any implements nothing beyond any, but
	// satisfies comparable (Go 1.20). A comparison of uncomparable dynamic
	// values then panics at run time.
	println(isEq[any](1, 1))
	println(recoverString(func() { isEq[any]([]int{}, []int{}) }))

	// int satisfies Ordered; a defined type with underlying int satisfies ~int.
	type score int
	println(maxOrdered(score(3), score(9)))
}

func maxOrdered[T Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func declVariables() {
	sub("Variable declarations")
	var i int
	var U, V, W float64
	k := 0
	var x, y float32 = -1, -2
	var (
		g        int
		u, v_, s = 2.0, 3.0, "bar"
	)
	re, im := complexSqrt(-1)
	entries := map[string]int{"a": 1}
	_, found := entries["a"] // map lookup, value discarded
	println(i, U, V, W, k, x, y, g, u, v_, s, re, im, found)
	// Untyped constants take their default type; nil cannot initialize an
	// untyped declaration: "var n = nil" is illegal.
	d := 'c' // rune
	println(d)
}

func complexSqrt(x float64) (float64, float64) { return 0, 1 }

func declShortVariables() {
	sub("Short variable declarations")
	i, j := 0, 10
	f := func() int { return 7 }
	ch := make(chan int, 1)
	ch <- 1
	r, w, _ := <-ch, 2, 3
	println(i, j, f(), r, w)

	// Redeclaration: in a multi-variable := at least one new non-blank
	// variable is required; existing ones (same block) are just assigned.
	a, err := 1, errorf("first")
	pBefore := &err
	b, err := 2, errorf("second") // err is reused, b is new
	println(a, b, err.Error(), pBefore == &err)

	// Shadowing: := in an inner block declares a new variable.
	x := 1
	if true {
		x := 2
		x++
		_ = x
	}
	println(x)
}

func declFunctions() {
	sub("Function declarations")
	sa, sb := swapGeneric(1, "a")
	println(minInt(3, 5), sa, sb)
	println(namedResults(3))
	println(terminating(1))
	// A function declaration without a body provides a signature for a
	// function implemented outside Go (assembly); that needs more than one
	// file, so it is not shown.
}

func minInt(x int, y int) int {
	if x < y {
		return x
	}
	return y
}

func swapGeneric[A, B any](a A, b B) (B, A) { return b, a }

func namedResults(n int) (sq, cube int) {
	sq = n * n
	cube = sq * n
	return // bare return returns the named results
}

// A function with results must end in a terminating statement; here the
// if/else whose both branches terminate.
func terminating(x int) string {
	if x > 0 {
		return "positive"
	} else {
		return "non-positive"
	}
}

type Point struct{ x, y float64 }

func (p *Point) Scale(f float64) { p.x *= f; p.y *= f }
func (p Point) Sum() float64     { return p.x + p.y }
func (Point) Kind() string       { return "point" } // receiver name omitted

type Pair[A, B any] struct {
	a A
	b B
}

func (p Pair[A, B]) Swap() Pair[B, A] { return Pair[B, A]{p.b, p.a} }

// Receiver type parameter names need not match the declaration; _ ignores one.
func (p Pair[First, _]) First() First { return p.a }

type List[E any] []E

// Generic method (Go 1.27): type parameters beyond the receiver's.
func (l List[E]) Map[F any](f func(E) F) List[F] {
	r := make(List[F], len(l))
	for i, x := range l {
		r[i] = f(x)
	}
	return r
}

func declMethods() {
	sub("Method declarations")
	p := Point{1, 2}
	p.Scale(2)
	println(p.Sum(), p.Kind())
	pr := Pair[int, string]{1, "one"}
	sw := pr.Swap()
	println(sw.a, sw.b, pr.First())
	l := List[int]{1, 2, 3}
	strs := l.Map(func(i int) string { return string(rune('a' + i - 1)) })  // F inferred
	floats := l.Map[float64](func(i int) float64 { return float64(i) / 2 }) // F explicit
	println(strs[0], strs[2], floats[2])
}

// ===========================================================================
// Spec: Expressions
// ===========================================================================

func expressions() {
	section("Expressions")
	exprOperandsAndQualified()
	exprCompositeLiterals()
	exprFunctionLiterals()
	exprSelectors()
	exprMethodExpressionsAndValues()
	exprIndex()
	exprSlices()
	exprTypeAssertions()
	exprCalls()
	exprInstantiationAndInference()
	exprOperators()
	exprArithmetic()
	exprComparison()
	exprLogical()
	exprAddress()
	exprReceive()
	exprConversions()
	exprStringConversions()
	exprSliceToArray()
	exprConstantExpressions()
	exprOrderOfEvaluation()
}

func exprOperandsAndQualified() {
	sub("Operands / Qualified identifiers / Primary expressions")
	// Operands: literals, (possibly qualified) identifiers, parenthesized
	// expressions. The blank identifier is an operand only on the left of =.
	x := 1
	println(x, Point{1, 1}.Sum(), len("x"))
	// Qualified identifiers (pkg.Name) need an import: out of scope.
	// Generic functions used as operands must be instantiated (or inferable):
	f := minGeneric[int]
	println(f(2, 1))
}

func minGeneric[T Ordered](a, b T) T { return min(a, b) }

type Line struct {
	Object
	p, q Point3D
}
type (
	Object  struct{ name, color string }
	Point3D struct {
		Object
		x, y, z float64
	}
)

func exprCompositeLiterals() {
	sub("Composite literals")
	// Struct: positional (all fields, in order) or keyed (any subset).
	origin := Point3D{}
	line1 := Line{Object{}, origin, Point3D{y: -4, z: 12.3}}
	// Keys may name promoted fields (Go 1.27): name is Line.Object.name.
	line2 := Line{name: "diagonal", q: Point3D{x: 1, y: 1, z: 1}}
	println(line1.q.y, line2.name, line2.Object.name, line2.q.x)

	// Arrays: indexed keys, mixed with positional; [...] counts elements.
	primes := [...]int{2, 3, 5, 7}
	sparse := [10]string{2: "two", 5: "five", "six"} // unkeyed follows the previous key
	vowels := [128]bool{'a': true, 'e': true, 'i': true, 'o': true, 'u': true}
	println(len(primes), sparse[6], len(sparse), vowels['e'], vowels['b'])

	// Slices: like arrays without a length; the length is the max index + 1.
	filter := []float32{4: -1, 0.5, 2: 0}
	println(len(filter), cap(filter))

	// Maps, with element and key types elided in nested literals.
	pts := map[string]Point{"o": {0, 0}, "x": {1, 0}}
	byPt := map[Point]string{{0, 0}: "origin"}
	println(pts["x"].x, byPt[Point{}])

	// Elided types including &T for pointer elements.
	pp := []*Point{{1, 2}, {3, 4}} // same as &Point{1, 2}, &Point{3, 4}
	nested := [][]int{{1}, {2, 3}}
	arrOfArr := [...][2]int{{1, 2}, {3, 4}}
	println(pp[1].y, nested[1][1], arrOfArr[1][0])

	// &T{} allocates and returns a pointer; the variable is unique per evaluation.
	a, b := &Point{}, &Point{}
	println(a != b)

	// Generic type literals need instantiation.
	println(Pair[int, bool]{a: 1}.a)

	// A literal in an if/for/switch header must be parenthesized.
	if p := (Point{1, 2}); p.x == 1 {
		println("parenthesized literal in if header")
	}
}

func exprFunctionLiterals() {
	sub("Function literals")
	add := func(a, b int) int { return a + b }
	println(add(1, 2), func(x int) int { return -x }(5))
	// Closures capture variables, not values.
	n := 0
	inc := func() int { n++; return n }
	inc()
	inc()
	println(n)
	// Per-iteration loop variables (Go 1.22): each closure sees its own i.
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i })
	}
	for _, f := range fs {
		print(f(), " ")
	}
	println()
	// A variable declared outside the loop is shared by all closures.
	var gs []func() int
	shared := 0
	for shared = 0; shared < 3; shared++ {
		gs = append(gs, func() int { return shared })
	}
	for _, g := range gs {
		print(g(), " ")
	}
	println()
}

type T0 struct{ x int }

func (*T0) M0() string { return "M0" }

type T1 struct{ y int }

func (T1) M1() string { return "M1" }

type T2 struct {
	z int
	T1
	*T0
}

func (*T2) M2() string { return "M2" }

func exprSelectors() {
	sub("Selectors")
	t := T2{z: 1, T1: T1{y: 2}, T0: &T0{x: 3}}
	p := &t
	println(t.z, t.y, t.x)          // t.y is t.T1.y; t.x is (*t.T0).x
	println(p.z, p.y, p.x)          // p.z is (*p).z
	println(t.M1(), t.M0(), p.M2()) // t.M0 is (*t.T0).M0; p.M2 needs *T2
	println(t.M2())                 // t addressable: (&t).M2()
	// Selecting through a nil embedded pointer panics.
	var q T2
	println(recoverString(func() { _ = q.x }))
}

type TM struct{ a int }

func (tv TM) Mv(b int) int          { return tv.a + b }
func (tp *TM) Mp(f float32) float32 { return float32(tp.a) * f }

func exprMethodExpressionsAndValues() {
	sub("Method expressions / Method values")
	var t TM = TM{10}
	// Method expression: T.M is a function taking the receiver first.
	fv := TM.Mv     // func(TM, int) int
	fp := (*TM).Mp  // func(*TM, float32) float32
	fpv := (*TM).Mv // value method through pointer type: func(*TM, int) int
	println(fv(t, 1), fp(&t, 2), fpv(&t, 3))
	// Method value: x.M binds the receiver; x is evaluated and copied now.
	mv := t.Mv
	mp := t.Mp // t addressable: binds &t
	t.a = 100
	println(mv(1), mp(1)) // value receiver saw the old copy; pointer sees the update
	// Method values from interfaces.
	var g Getter = Counter{5}
	get := g.Get
	println(get())
	// Generic method expression and value (Go 1.27) must be instantiated.
	l := List[int]{1}
	m := l.Map[string]
	me := List[int].Map[bool]
	println(m(func(int) string { return "s" })[0], me(l, func(int) bool { return true })[0])
}

func exprIndex() {
	sub("Index expressions")
	a := [3]int{1, 2, 3}
	pa := &a
	s := a[:]
	str := "héllo"
	m := map[string]int{"k": 1}
	println(a[2], pa[2], s[0], str[1], m["k"]) // pa[2] is (*pa)[2]; str[1] is a byte
	v, ok := m["nope"]                         // comma-ok form
	println(v, ok)
	// Constant indices are bounds-checked at compile time for arrays and
	// constant strings: a[3] or "abc"[5] do not compile.
	println("abc"[1])
	// Type parameter operands: all types in the type set must support it.
	println(at([]int{4, 5}, 1), at([3]int{6, 7, 8}, 2))
}

func at[S ~[]int | ~[3]int](s S, i int) int { return s[i] }

func exprSlices() {
	sub("Slice expressions")
	a := [5]int{1, 2, 3, 4, 5}
	s := a[1:4]
	println(len(s), cap(s), s[0])
	println(len(a[:]), len(a[2:]), len(a[:3]))
	// Upper bounds are relative to cap, not len, for slices: reslicing
	// can extend into the underlying array.
	t := s[:4]
	println(len(t), t[3])
	// Full slice expression a[low:high:max] sets cap = max-low, so appends
	// reallocate instead of overwriting a[4].
	u := a[1:3:3]
	u = append(u, 99)
	println(cap(a[1:3:3]), a[3], u[2])
	// Strings: slicing yields a string (bytes, not runes).
	str := "héllo"
	println(str[0:3], len(str[1:3]))
	// Slicing a pointer to an array.
	pa := &a
	println(len(pa[1:2]))
	// Out-of-range bounds panic at run time.
	hi := 10
	println(recoverString(func() { _ = s[:hi] }))
}

func exprTypeAssertions() {
	sub("Type assertions")
	var i any = "str"
	s := i.(string)  // single-value form panics on failure
	n, ok := i.(int) // comma-ok form never panics
	println(s, n, ok)
	var r Reader = &file{}
	w, isW := r.(Writer) // assert to another interface: does the dynamic type implement it?
	println(isW, w != nil)
	println(recoverString(func() { _ = i.(int) }))
	var nilIface any
	_, ok = nilIface.(any) // asserting on a nil interface fails, even to any
	println(ok)
}

func exprCalls() {
	sub("Calls / Passing arguments to ... parameters")
	// Arguments are evaluated before the call; a multi-valued call can be
	// the sole argument of another call.
	println(addPair(pairVals()))
	// Variadic: zero or more args form a new slice; s... passes s as is.
	println(variadic("none"), variadic("three", 1, 2, 3))
	s := []int{4, 5}
	println(variadicMut(s...), s[0]) // shares s's backing array
	var nilFn func()
	println(recoverString(func() { nilFn() }))
	// A method call x.m() where m is in the method set of x's type (or &x
	// when x is addressable); covered in Method sets.
}

func pairVals() (int, int) { return 2, 3 }
func addPair(a, b int) int { return a + b }
func variadic(name string, xs ...int) string {
	if xs == nil {
		return name + ":nil"
	}
	return name + ":" + string(rune('0'+len(xs)))
}
func variadicMut(xs ...int) int { xs[0] = 40; return len(xs) }

func Map[T, U any](xs []T, f func(T) U) []U {
	r := make([]U, 0, len(xs))
	for _, x := range xs {
		r = append(r, f(x))
	}
	return r
}

func Double[T Number](x T) T             { return x * 2 }
func Zero[T any]() T                     { var z T; return z }
func Convert[To, From Number](f From) To { return To(f) }
func Identity[T any](x T) T              { return x }

func Scale[S ~[]E, E Number](s S, c E) S {
	r := make(S, len(s))
	for i, v := range s {
		r[i] = v * c
	}
	return r
}

type Vec []float64

func (v Vec) Norm1() float64 {
	t := 0.0
	for _, x := range v {
		t += max(x, -x)
	}
	return t
}

func exprInstantiationAndInference() {
	sub("Instantiations / Type inference / Type unification")
	// Inferred from ordinary arguments.
	println(Double(21), Double(1.5))
	// Untyped constants of different kinds: the "largest" kind wins (Go 1.21).
	println(maxOrdered(1, 2.5))
	// Partial instantiation: To given, From inferred from the argument.
	println(Convert[float64](3))
	// Explicit instantiation required: T appears only in the result.
	println(Zero[int](), Zero[string]() == "")
	// Inference from a generic function passed as an argument (Go 1.21):
	// Double's T is inferred from Map's parameter type func(int) U.
	println(Map([]int{1, 2}, Double[int])[1], Map([]int{1, 2}, Double)[1])
	// Function type inference in any assignment context (Go 1.27).
	var id func(string) string = Identity
	var dbl func(float64) float64
	dbl = Double
	println(id("inferred"), dbl(4))
	// Unification keeps the defined type: S unifies with Vec (not []float64)
	// and E with float64 through Vec's underlying type, so Norm1 survives.
	v := Scale(Vec{1, -2}, 2)
	println(v.Norm1())
	// Instantiating a generic type yields a new, non-generic type.
	type IntTree = Tree[int]
	var it *IntTree
	it = it.Insert(1)
	println(it.value)
}

func exprOperators() {
	sub("Operators / Operator precedence")
	// Five binary precedence levels, high to low:
	//   5  *  /  %  <<  >>  &  &^
	//   4  +  -  |  ^
	//   3  ==  !=  <  <=  >  >=
	//   2  &&
	//   1  ||
	// Unary operators bind tightest. ++ and -- are statements.
	t, f := true, false
	println(
		2+3*4,
		(2+3)*4,
		1<<2+1,
		5&3|8,
		1 < 2 == t,
		t || t && f,
	) // && binds tighter than ||: true, not false
	x := 5
	println(-x, +x, ^x, !true, *&x)
	// Shifts: the right operand may be any integer type (Go 1.13 for
	// signed), or an untyped constant representable by uint. In a
	// non-constant shift, an untyped constant left operand takes the type
	// it would have if the shift were replaced by the left operand alone.
	var s uint = 33
	i := 1 << s          // 1 becomes int
	var j int32 = 1 << s // 1 becomes int32: shifted out, result 0
	k := uint64(1 << s)  // 1 becomes uint64
	var m int = 1.0 << s // 1.0 becomes int
	n := 1.0<<s == j     // 1.0 becomes int32 (the comparison's other side)
	o := 1<<s == 2<<s    // both untyped: int
	const p = 1 << 10.0  // constant shift: 10.0 is representable as uint
	signed := -1
	println(i, j, k, m, n, o, p)
	println(recoverString(func() { _ = 1 << signed })) // negative shift count panics
	// "var u = 1.0<<s" is illegal: 1.0 would default to float64.
}

func exprArithmetic() {
	sub(
		"Arithmetic operators: Integer operators / Integer overflow / Floating-point operators / String concatenation",
	)
	a, b := 7, -3
	println(
		a+b,
		a-b,
		a*b,
		a/b,
		a%b,
		-a/2,
		-a%2,
	) // / truncates toward zero; % takes the dividend's sign
	println(a&6, a|8, a^2, a&^5) // &^ is AND NOT (bit clear)
	println(a<<2, a>>1, -8>>1)   // >> is arithmetic on signed ints
	// Shifting by a power of two equals division except for negatives.
	println(-5/2, -5>>1)
	// The most negative value divided by -1 yields itself (no panic).
	minI8, neg := int8(-128), int8(-1)
	println(minI8/neg, minI8%neg)

	// Overflow: unsigned arithmetic is modulo 2^n, signed wraps two's
	// complement. The compiler may not assume overflow doesn't happen.
	var u8 uint8 = 255
	u8++
	var i8 int8 = 127
	i8++
	println(u8, i8, -minI8)

	// Floats: division by zero gives ±Inf; NaN compares unequal to itself.
	zero := 0.0
	inf := 1 / zero
	nan := zero / zero
	println(inf, -inf, nan == nan, inf > 1e308)
	// An implementation may fuse x*y+z (FMA); an explicit conversion
	// forces rounding: float64(x*y) + z.
	x, y, z := 0.1, 10.0, -1.0
	println(float64(x*y) + z)
	// Complex arithmetic.
	c := (1 + 2i) * (3 - 1i)
	println(c, real(c), imag(c))

	// Strings: + and += concatenate, creating new strings.
	s := "hi"
	s += " there"
	println(s + "!")
}

func exprComparison() {
	sub("Comparison operators")
	// Comparison results are untyped bool.
	type pt struct{ x, y int }
	println(pt{1, 2} == pt{1, 2}, [2]int{1, 2} != [2]int{1, 3}) // fieldwise / elementwise
	// Pointers: equal if they point to the same variable (or both nil).
	v := 1
	p1, p2 := &v, &v
	println(p1 == p2, &pt{} == &pt{})
	// Channels: equal if made by the same make call.
	c1 := make(chan int)
	c2 := c1
	println(c1 == c2, c1 == make(chan int))
	// Interfaces: equal if dynamic types are identical and values are equal.
	var a1, a2 any = 3, 3
	var a3 any = int64(3)
	println(a1 == a2, a1 == a3, a1 == 3) // non-interface operand converted to the interface
	// Comparing interfaces holding uncomparable dynamic types panics.
	var s1, s2 any = []int{}, []int{}
	println(recoverString(func() { _ = s1 == s2 }))
	// Slices, maps, and functions are comparable only to nil.
	var sl []int
	var mp map[int]int
	var fn func()
	println(sl == nil, mp == nil, fn == nil)
	// Ordered: integers, floats, strings (bytewise lexicographic).
	println("abc" < "abd", "Z" < "a", 1.5 >= 1.5)
	// Struct equality compares only non-blank fields.
	type blanky struct {
		a int
		_ int
	}
	println(blanky{a: 1} == blanky{a: 1})
}

func exprLogical() {
	sub("Logical operators")
	calls := 0
	f := func(r bool) bool { calls++; return r }
	_ = f(false) && f(true) // right operand evaluated conditionally
	_ = f(true) || f(true)
	println(calls, !false)
}

func exprAddress() {
	sub("Address operators")
	x := 1
	p := &x
	arr := [2]int{}
	pe := &arr[1]  // element of an addressable array
	ps := &Point{} // composite literal: exception to addressability
	pf := &ps.x    // field selector of an addressable struct (via pointer)
	sl := []int{1}
	pslice := &sl[0] // slice elements are always addressable
	*pe, *pf, *pslice = 5, 6, 7
	println(*p, arr[1], ps.x, sl[0])
	// &m[k], &f(), &"lit", and &(x+1) are not allowed.
	var np *int
	println(recoverString(func() { println(*np) }))
}

func exprReceive() {
	sub("Receive operator")
	ch := make(chan int, 2)
	ch <- 1
	close(ch)
	v, ok := <-ch  // value from buffer, ok == true
	w, ok2 := <-ch // closed and drained: zero value, ok == false
	println(v, ok, w, ok2)
	// Receiving from a nil channel blocks forever (shown in Select statements).
}

type (
	IntSlice      []int
	celsiusTagged struct {
		Deg float64 `unit:"C"`
	}
)
type plainDeg struct{ Deg float64 }

func exprConversions() {
	sub("Conversions")
	// Constant conversions yield typed constants; they must be representable.
	const c = uint8(255)
	println(c, string(rune(0x65e5)), float32(0.49999999), int64(-1))
	// Non-constant numeric conversions.
	f := 3.99
	n := -3.99
	big := 300
	println(int(f), int(n), uint8(big), int8(big)) // truncate toward zero; integers wrap
	var i64 int64 = 1<<40 + 1
	println(float32(i64), int32(i64))
	// Underlying types identical (ignoring struct tags): direct conversion.
	t := celsiusTagged{36.6}
	pd := plainDeg(t)
	println(pd.Deg, len(IntSlice([]int{1, 2})))
	// Pointer types whose base types have identical underlying types.
	var pp *plainDeg = (*plainDeg)(&t)
	pp.Deg = 0
	println(t.Deg)
	// Type parameter conversions: valid for every type in the type set.
	println(
		Convert[int8](100.7),
	) // float -> int8 via a type parameter; out-of-range float->int is implementation-specific
	// Ambiguity: (*Point)(nil) vs *Point(nil); func() types need parens too.
	_ = (*Point)(nil)
	_ = (func())(nil)
	_ = (<-chan int)(nil)
}

func exprStringConversions() {
	sub("Conversions to and from a string type")
	// Integer -> string: the UTF-8 of that code point; invalid ones become
	// "�". (go vet warns about string(int); rune and byte are fine.)
	println(string(rune('a')), string(rune(-1)) == "�", string(rune(0x10FFFF+1)) == "�")
	var b byte = 'x'
	println(string(b))
	// []byte <-> string copies bytes; []rune <-> string decodes/encodes UTF-8.
	bs := []byte("héllo")
	rs := []rune("héllo")
	println(len(bs), len(rs), string(bs[:3]), string(rs[:2]))
	var nilBytes []byte
	println(string(nilBytes) == "", []byte("") != nil)
	bs[0] = 'H' // the string is unaffected: conversion copied
	type myBytes []byte
	type myRunes []rune
	type myString string
	println(string(myBytes("ab")), string(myRunes{0x4e16, 0x754c}), myString("x"))
	// Invalid UTF-8 decodes to U+FFFD per bad byte.
	println(len([]rune("\xff\xfe")), []rune("\xff")[0] == 0xFFFD)
}

func exprSliceToArray() {
	sub("Conversions from slice to array or array pointer")
	s := []int{1, 2, 3, 4}
	a := [3]int(s)    // Go 1.20: copies the first 3 elements
	p := (*[2]int)(s) // Go 1.17: shares the underlying array
	s[0] = 100
	println(a[0], p[0], len(p))
	var nilS []int
	println((*[0]int)(nilS) == nil, len([0]int(nilS))) // nil slice -> nil array pointer
	println(recoverString(func() { _ = [5]int(s) }))   // array longer than the slice panics
}

func exprConstantExpressions() {
	sub("Constant expressions")
	const a = 2 + 3.0            // 5.0 untyped float
	const b = 15 / 4             // 3: integer division of untyped ints
	const c = 15 / 4.0           // 3.75
	const Θ float64 = 3 / 2      // 1.0: the division is integer before conversion
	const Π float64 = 3 / 2.     // 1.5
	const d = 1 << 3.0           // 8: shift count converted to an integer
	const h = "foo" > "bar"      // untyped bool
	const j = true               // untyped bool
	const k = 'w' + 1            // untyped rune
	const l = "hi" + string('!') // string constant
	const m = string(k)          // "x"
	const Σ = 1 - 0.707i         // untyped complex
	const Δ = Σ + 2.0e-4
	const Φ = iota*1i - 1/1i // untyped complex
	const ic = complex(0, 1) // builtins on constants are constant
	const iΘ = complex(0, Θ) // typed complex128
	const Huge = 1 << 100
	const Four int8 = Huge >> 98 // typed result representable: fine
	println(a, b, c, Θ, Π, d, h, j, k, l, m)
	println(Σ, Δ, Φ, ic, iΘ, Four)
	// ^ on untyped constants uses infinite precision: ^1 == -2. For typed
	// unsigned constants the mask is all ones of that size.
	const mask = ^uint8(1)
	println(^1, mask, uint8(^1&0xff))
	// Typed constant expressions must not overflow: int8(127)+1 is an
	// error. Constant division by zero is an error.
	// Constant comparisons and len of arrays are constant.
	var arr [4]int
	const n = len(arr)
	println(n)
}

func exprOrderOfEvaluation() {
	sub("Order of evaluation")
	// Calls, method calls, receives, and logical ops are evaluated in
	// lexical left-to-right order; other operand ordering is unspecified.
	var log []string
	f := func(s string, v int) int { log = append(log, s); return v }
	y := f("a", 1) + f("b", 2)*f("c", 3)
	for _, s := range log {
		print(s)
	}
	println(" ", y)
	// Assignment: indexes and pointer indirections on the left and all
	// expressions on the right are evaluated first, then assigned left to right.
	x := []int{0, 0}
	i := 0
	i, x[i] = 1, 2 // x[i] uses the old i
	println(i, x[0], x[1])
	a, b := 1, 2
	a, b = b, a // swap
	println(a, b)
	// Package-level initialization order is shown in Package initialization.
}

// ===========================================================================
// Spec: Statements
// ===========================================================================

func statements() {
	section("Statements")
	stmtTerminating()
	stmtEmptyLabeledExpression()
	stmtSendIncDecAssignment()
	stmtIf()
	stmtExpressionSwitch()
	stmtTypeSwitch()
	stmtFor()
	stmtRange()
	stmtGo()
	stmtSelect()
	stmtReturn()
	stmtBreakContinue()
	stmtGoto()
	stmtFallthrough()
	stmtDefer()
}

func stmtTerminating() {
	sub("Terminating statements")
	println(term1(), term2(1), term3(), term4(), term5(make(chan int, 1)))
	println(recoverString(func() { term6() }))
}

// A function with results must end in one of: return, goto, panic, a block
// ending in a terminating statement, an if with terminating else, a for
// with no condition and no break, a switch/select with no break whose
// clauses (including default for switch) all terminate, or a labeled
// terminating statement.
func term1() int { return 1 }

func term2(x int) int {
	switch {
	case x > 0:
		return 1
	default:
		return 0
	}
}

func term3() int {
	for {
		return 3
	}
}

func term4() (r int) {
	r = 4
	{
		return
	}
}

func term5(ch chan int) int {
	ch <- 5
	select {
	case v := <-ch:
		return v
	}
}
func term6() int { panic("terminates via panic") }

func stmtEmptyLabeledExpression() {
	sub("Empty statements / Labeled statements / Expression statements")
	// empty statement (gofmt deletes it)
	for i := 0; i < 2; i++ {
	} // empty block
	// Any statement may be labeled; break and continue only target labeled
	// for, switch, or select statements, goto may target any label.
	goto Block
Block:
	{
		println("labeled block reached by goto")
	}
	// Expression statements: calls, method calls, receives. Calls of most
	// builtins are not allowed as statements (len(x) alone is an error);
	// copy, panic, print, println, recover, clear, close, delete are.
	h := func() int { return 0 }
	h()
	ch := make(chan int, 1)
	ch <- 1
	<-ch
	println("expression statements ran")
}

func stmtSendIncDecAssignment() {
	sub("Send statements / IncDec statements / Assignment statements")
	ch := make(chan int, 1)
	ch <- 1 + 2 // channel and value are evaluated before communication
	println(<-ch)
	i := 0
	i++
	i++
	i--
	m := map[string]int{}
	m["k"]++ // operand may be a map index expression
	println(i, m["k"])
	// Assignment operators: op= for every binary arithmetic op.
	x := 100
	x += 1
	x -= 2
	x *= 3
	x /= 2
	x %= 50
	x <<= 2
	x >>= 1
	x &= 0xff
	x |= 1
	x ^= 3
	x &^= 4
	println(x)
	// Tuple assignment, from a multi-valued call, map index, receive, assertion.
	var a, b int
	a, b = pairVals()
	v, ok := m["k"]
	ch <- 9
	r, rok := <-ch
	var e any = 1
	n, nok := e.(int)
	_ = v
	println(a, b, ok, r, rok, n, nok)
	// An untyped constant or nil assigned to an interface takes its default type.
	var any1 any = 1.5
	_, isF := any1.(float64)
	println(isF)
}

func stmtIf() {
	sub("If statements")
	x := 7
	if x > 5 {
		println("plain if")
	}
	if y := x * 2; y > 10 { // init statement; y scoped to the whole if/else chain
		println("init", y)
	} else if y > 5 {
		println("else-if sees y too", y)
	} else {
		println(y)
	}
}

func stmtExpressionSwitch() {
	sub("Switch statements: Expression switches")
	grade := func(score int) string {
		switch { // missing tag means "true"
		case score >= 90:
			return "A"
		case score >= 80:
			return "B"
		default:
			return "C"
		}
	}
	println(grade(95), grade(85), grade(10))
	switch x := 3; x { // init statement and tag
	case 1, 2, 3: // a list of expressions
		println("small", x)
	case 4:
		println("four")
	}
	// Cases are evaluated top to bottom, left to right, and stop at the first match.
	calls := ""
	ev := func(s string, v int) int { calls += s; return v }
	switch 2 {
	case ev("a", 1), ev("b", 2), ev("c", 3):
	}
	println(calls)
	// default may appear anywhere and an empty switch is legal.
	switch {
	default:
		println("default first")
	case false:
	}
	switch {
	}
	// The untyped tag is converted to its default type: switch 1<<40 {} is int.
	// A tag of untyped nil is not allowed.
	var iface any = 1.5
	switch iface { // interface tag: cases compared by interface equality
	case 1.5:
		println("matched float64 1.5")
	}
}

func classify(x any) string {
	switch v := x.(type) {
	case nil:
		return "nil"
	case int, int64: // multiple types: v keeps the type of x (any)
		_ = v
		return "int or int64"
	case string:
		return "string of len " + string(rune('0'+len(v))) // v is string
	case error:
		return "error: " + v.Error()
	case func() int:
		return "func"
	case []int, map[string]int:
		return "slice or map"
	default:
		return "other"
	}
}

func typeName[T any](x any) string {
	switch x.(type) { // without a binding
	case T: // a type parameter may appear in a case
		return "T"
	default:
		return "not T"
	}
}

func stmtTypeSwitch() {
	sub("Switch statements: Type switches")
	println(classify(nil), classify(1), classify(int64(2)), classify("abc"))
	println(
		classify(errorf("x")),
		classify(func() int { return 0 }),
		classify([]int{}),
		classify(2.5),
	)
	println(typeName[int](1), typeName[string](1))
	// With an init statement; fallthrough is not permitted in type switches.
	switch r := any(Counter{}); v := r.(type) {
	case Getter:
		println("Counter implements Getter", v.Get())
	}
}

func stmtFor() {
	sub("For statements: single condition / for clause")
	n := 0
	for n < 3 { // condition only (a "while" loop)
		n++
	}
	for { // no condition: equivalent to true
		n++
		if n == 5 {
			break
		}
	}
	for i := 0; i < 3; i++ { // init; cond; post
		n += i
	}
	for i := 0; i < 2; { // empty post
		i++
	}
	for ; n < 20; n++ { // empty init
	}
	println(n)
	// Each iteration has its own i (Go 1.22): the post statement copies the
	// previous iteration's value into the new variable before running.
	var ptrs []*int
	for i := 0; i < 3; i++ {
		ptrs = append(ptrs, &i)
	}
	println(*ptrs[0], *ptrs[1], *ptrs[2], ptrs[0] != ptrs[1])
	// Changes made inside the body are carried into the next iteration.
	for i := 0; i < 6; i++ {
		i++
		print(i, " ")
	}
	println()
}

func countTo(n int) func(func() bool) {
	return func(yield func() bool) {
		for range n {
			if !yield() {
				return
			}
		}
	}
}

func evens(limit int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < limit; i += 2 {
			if !yield(i) {
				return // the loop body did break/return: stop
			}
		}
	}
}

func enumerate[E any](s []E) func(func(int, E) bool) {
	return func(yield func(int, E) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

func stmtRange() {
	sub("For statements: range clause")
	arr := [3]string{"a", "b", "c"}
	for i, v := range arr { // the array is copied once before the loop
		print(i, v, " ")
	}
	println()
	for i := range &arr { // pointer to array: no copy
		arr[i] += "!"
	}
	println(arr[0])
	sl := []int{10, 20, 30}
	for i, v := range sl {
		if i == 0 {
			sl[2] = 99 // slices are not copied: later iterations see this
		}
		print(v, " ")
	}
	println()
	for range sl { // no variables
	}
	var nilSl []int
	for range nilSl {
		println("never")
	}
	// Strings: byte index and rune; invalid UTF-8 yields U+FFFD, 1 byte.
	for i, r := range "aé\xff" {
		print(i, ":", r, " ")
	}
	println()
	// Indexing a string gives bytes; ranging gives runes.
	s := "é"
	println(len(s), s[0], s[1])
	// Maps: order unspecified; entries added during iteration may or may not
	// be visited; deleted ones not yet reached are not. Sum to stay deterministic.
	m := map[string]int{"x": 1, "y": 2, "z": 3}
	total := 0
	for k, v := range m {
		total += v
		_ = k
	}
	println(total)
	// Channels: values until the channel is closed.
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)
	for v := range ch {
		print(v, " ")
	}
	println()
	// Integers (Go 1.22): 0..n-1, with the type of n.
	for i := range 3 {
		print(i, " ")
	}
	for i := range int8(2) { // i is int8
		print(i, " ")
	}
	for range 0 {
		println("never")
	}
	println()
	// Function iterators (Go 1.23): all three yield signatures.
	c := 0
	for range countTo(3) { // func(yield func() bool)
		c++
	}
	for v := range evens(100) { // func(yield func(V) bool)
		if v > 4 {
			break // makes yield return false
		}
		print(v, " ")
	}
	for i, v := range enumerate([]string{"p", "q"}) { // func(yield func(K, V) bool)
		print(i, v, " ")
	}
	println(c)
	// Method values work as iterators too.
	var tr *Tree[string]
	for _, w := range []string{"m", "c", "x"} {
		tr = tr.Insert(w)
	}
	for v := range tr.All {
		print(v)
	}
	println()
	// Assigning to existing variables with = instead of :=.
	var i, v int
	for i, v = range []int{7, 8} {
	}
	println(i, v)
	// Range over a type parameter whose type set has a single underlying
	// slice type behaves like ranging over that slice.
	println(sumSlice(IntSlice{1, 2, 3}))
}

func sumSlice[S ~[]int](s S) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}

func stmtGo() {
	sub("Go statements")
	// The function value and arguments are evaluated in the calling
	// goroutine; the call runs independently. Results are discarded.
	done := make(chan int)
	x := 1
	go func(v int) { done <- v * 10 }(x)
	x = 2
	println(<-done) // 10: the argument was evaluated before x changed
	// Fan-out / fan-in, deterministic by ordering through channels.
	results := make([]chan int, 3)
	for i := range results {
		results[i] = make(chan int, 1)
		go func() { results[i] <- i * i }() // per-iteration i
	}
	for _, r := range results {
		print(<-r, " ")
	}
	println()
}

func stmtSelect() {
	sub("Select statements")
	ch := make(chan int, 1)
	// default runs when no communication can proceed.
	select {
	case v := <-ch:
		println("got", v)
	default:
		println("default: nothing ready")
	}
	// Send case.
	select {
	case ch <- 1:
		println("sent")
	default:
	}
	// Receive with comma-ok and assignment to existing variables.
	var v int
	var ok bool
	select {
	case v, ok = <-ch:
		println("received", v, ok)
	}
	// Nil channels are never ready, which disables their cases.
	var nilCh chan int
	select {
	case <-nilCh:
		println("never")
	case nilCh <- 1:
		println("never")
	default:
		println("nil channel cases skipped")
	}
	// Blocking select with no default waits for a goroutine.
	data := make(chan string)
	quit := make(chan struct{})
	go func() {
		data <- "from goroutine"
		close(quit)
	}()
	for running := true; running; {
		select {
		case s := <-data:
			println(s)
		case <-quit:
			running = false
		}
	}
	// All channel and send expressions are evaluated once on entry, in
	// source order, even for cases that are not chosen.
	evalLog := ""
	chOf := func(s string, c chan int) chan int { evalLog += s; return c }
	ready := make(chan int, 1)
	ready <- 1
	select {
	case <-chOf("a", ready):
	case chOf("b", make(chan int)) <- len(evalLog):
	}
	println(evalLog)
	// select {} would block forever.
}

func stmtReturn() {
	sub("Return statements")
	n, name := ret2()
	println(ret1(), n, name, ret3(2))
	a, b := ret4()
	println(a, b)
}

func ret1() int              { return 1 }         // explicit value
func ret2() (int, string)    { return pairInt() } // a multi-valued call
func pairInt() (int, string) { return 2, "two" }
func ret3(x int) (y int) { // bare return with named result
	y = x * 3
	return
}

func ret4() (a, b int) {
	a = 1
	{
		a := 10 // shadows the result; a bare return here would not compile
		return a, 2
	}
}

func stmtBreakContinue() {
	sub("Break statements / Continue statements")
	found := -1
OuterLoop:
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			switch {
			case i*j == 6:
				found = i*10 + j
				break OuterLoop // a plain break would only leave the switch
			}
		}
	}
	println(found)
	sum := 0
RowLoop:
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			if j > i {
				continue RowLoop
			}
			sum += j
		}
	}
	println(sum)
	ch := make(chan int, 1)
	ch <- 1
Sel:
	for {
		select {
		case <-ch:
			break Sel // leaves the for, not just the select
		}
	}
	println("broke out of select loop")
}

func stmtGoto() {
	sub("Goto statements")
	i := 0
loop:
	if i < 3 {
		i++
		goto loop
	}
	println(i)
	// goto must not jump over a variable declaration into its scope, and
	// must not jump into a block from outside it.
	goto end
end:
	println("jumped forward")
}

func stmtFallthrough() {
	sub("Fallthrough statements")
	switch 1 {
	case 1:
		print("one ")
		fallthrough // transfers to the first statement of the next clause, without testing it
	case 2:
		print("two ")
		fallthrough
	default:
		print("default ")
	case 3:
		print("three") // not reached: no fallthrough in default
	}
	println()
}

func stmtDefer() {
	sub("Defer statements")
	println(deferOrder())
	println(deferArgs())
	println(deferNamed())
	println(deferLoop())
}

func deferOrder() string {
	s := ""
	func() {
		for i := range 3 {
			defer func() { s += string(rune('0' + i)) }() // LIFO
		}
	}()
	return s
}

func deferArgs() string {
	out := ""
	func() {
		x := 1
		defer func(v int) { out += string(rune('0'+v)) + string(rune('0'+x)) }(
			x,
		) // v captured now, x read later
		x = 2
	}()
	return out
}

func deferNamed() (result int) {
	defer func() { result *= 10 }() // runs after result is set by return
	return 4
}

func deferLoop() int {
	n := 0
	func() {
		var nilFn func()
		defer func() { recover(); n++ }()
		defer nilFn() // a nil function value panics when invoked, not when deferred
	}()
	return n
}

// ===========================================================================
// Spec: Built-in functions
// ===========================================================================

func builtinFunctions() {
	section("Built-in functions")
	sub("Appending to and copying slices")
	s0 := []int{0, 0}
	s1 := append(s0, 2)
	s2 := append(s1, 3, 5, 7)
	s3 := append(s2, s0...)
	s4 := append(s3[3:6], s3[2:]...) // overlapping is fine
	var t []any
	t = append(t, 42, 3.1415, "foo")
	var b []byte
	b = append(b, "bar"...) // special case: append string to []byte
	println(len(s1), len(s2), len(s3), len(s4), len(t), string(b))
	// append reuses the backing array when cap allows, otherwise reallocates.
	base := make([]int, 1, 2)
	x := append(base, 1)
	y := append(base, 2) // overwrites x[1]: same backing array
	println(x[1], y[1])
	a := [...]int{0, 1, 2, 3, 4, 5, 6, 7}
	s := make([]int, 6)
	bs := make([]byte, 5)
	n1 := copy(s, a[0:])            // min(len) elements
	n2 := copy(s, s[2:])            // overlap allowed
	n3 := copy(bs, "Hello, World!") // special case: string source
	println(n1, n2, n3, s[0], string(bs))

	sub("Clear")
	m := map[string]int{"a": 1, "b": 2}
	clear(m) // deletes all entries
	sl := []int{1, 2, 3}
	clear(sl) // zeroes elements, keeps len
	println(len(m), len(sl), sl[0])
	clear(sl[:0]) // no-op on empty
	println(clearGeneric(map[int]int{1: 1}))

	sub("Close")
	ch := make(chan int, 1)
	ch <- 7
	close(ch)
	v, ok := <-ch
	println(v, ok)
	println(recoverString(func() { close(ch) }))
	var nilCh chan int
	println(recoverString(func() { close(nilCh) }))
	// Closing a receive-only channel does not compile.

	sub("Manipulating complex numbers")
	c64 := complex(1, -2) // untyped constants: complex128
	var re float32 = 3
	c := complex(re, 1) // float32 args: complex64
	println(c64, real(c64), imag(c64), c, real(c))
	const cc = complex(1.5, 0) // constant
	var fromC float64 = real(cc)
	println(fromC, imag(2i))

	sub("Deletion of map elements")
	dm := map[int]string{1: "a", 2: "b"}
	delete(dm, 1)
	delete(dm, 99) // missing key: no-op
	var nilMap map[int]string
	delete(nilMap, 1) // nil map: no-op
	println(len(dm))

	sub("Length and capacity")
	arr := [5]int{}
	pa := &arr
	str := "héllo"
	bufCh := make(chan int, 4)
	bufCh <- 1
	println(len(arr), cap(arr), len(pa), cap(pa), len(str), len(dm), len(bufCh), cap(bufCh))
	// len(arr) is a constant when the expression has no channel receives or
	// (non-constant) function calls.
	const lc = len([10]int{})
	var nilSlice []int
	println(lc, len(nilSlice), cap(nilSlice))

	sub("Making slices, maps and channels")
	ms := make([]int, 3)      // len = cap = 3
	ms2 := make([]int, 0, 10) // len 0, cap 10
	mm := make(map[string]int)
	mm2 := make(map[string]int, 100)
	mc := make(chan int)     // unbuffered
	mc2 := make(chan int, 5) // buffered
	println(len(ms), cap(ms2), len(mm), len(mm2), cap(mc), cap(mc2))
	negLen := -1
	println(recoverString(func() { _ = make([]int, negLen) }))
	println(makeGeneric[[]string](2))

	sub("Min and max")
	println(min(3), min(4, 1, 2), max(1.5, 2), max("a", "b", "ab"))
	const cm = max(1, 2.5) // constant if all args constant
	nan := 0.0
	nan /= nan
	println(cm, min(nan, 1) != min(nan, 1)) // any NaN argument gives NaN
	negZero := 0.0
	negZero = -negZero
	println(1 / min(negZero, 0.0)) // -0.0 < 0.0 for min/max: -Inf

	sub("Allocation")
	p := new(int) // new(T): pointer to zero value
	q := new(Point)
	r := new(42)     // new(expr): pointer to a copy of the value, typed int
	f := new(1.5)    // untyped float constant becomes float64
	bb := new(1 < 2) // untyped bool becomes bool
	pp := new(Point{1, 2})
	println(*p, q.x, *r, *f, *bb, pp.y)
	// new(nil) is not allowed.

	sub("Handling panics")
	quot, err := safeDiv(1, 0)
	println(quot, err.Error())
	println(recoverOnlyInDefer())
	println(repanic())
	println(panicNil())

	sub("Bootstrapping")
	print("print: no spaces, no newline;", 1, true, "\n")
	println("println:", "spaces", "and", "newline", 1, true, 2.5)
}

func clearGeneric[M ~map[int]int](m M) int { clear(m); return len(m) }

func makeGeneric[S ~[]E, E any](n int) int { return len(make(S, n)) }

func safeDiv(a, b int) (q int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errorf("recovered: " + describe(r))
		}
	}()
	return a / b, nil
}

func recoverOnlyInDefer() bool {
	// recover returns nil when not called directly by a deferred function.
	return recover() == nil
}

func repanic() (s string) {
	defer func() { s = "outer caught: " + describe(recover()) }()
	defer func() {
		r := recover()
		panic("re-panic after " + describe(r)) // a new panic from a deferred call
	}()
	panic("first")
}

func panicNil() string {
	var got any
	func() {
		defer func() { got = recover() }()
		panic(nil) // the spec is silent; the gc runtime turns this into a run-time error value
	}()
	if e, ok := got.(error); ok {
		return "panic(nil) recovered as error: " + e.Error()
	}
	return "panic(nil) recovered as nil"
}

// ===========================================================================
// Spec: Packages
// ===========================================================================

func packages() {
	section("Packages")
	sub("Source file organization / Package clause")
	// This file: a package clause, then (no) imports, then top-level
	// declarations. Package main with func main makes an executable.
	println("package main")
	sub("Import declarations")
	println("out of scope: this file imports nothing")
	sub("An example package")
	// The spec's concurrent prime sieve, adapted to stop after n primes.
	// Leftover goroutines stay blocked; the program exits when main returns.
	println(firstPrimes(10))
}

func generate(ch chan<- int) {
	for i := 2; ; i++ {
		ch <- i
	}
}

func filter(src <-chan int, dst chan<- int, prime int) {
	for i := range src {
		if i%prime != 0 {
			dst <- i
		}
	}
}

func firstPrimes(n int) string {
	out := ""
	ch := make(chan int)
	go generate(ch)
	for range n {
		prime := <-ch
		out += itoa(prime) + " "
		ch1 := make(chan int)
		go filter(ch, ch1, prime)
		ch = ch1
	}
	return out
}

// ===========================================================================
// Spec: Program initialization and execution
// ===========================================================================

func programInitializationAndExecution() {
	section("Program initialization and execution")
	sub("The zero value")
	var (
		zb   bool
		zi   int
		zf   float64
		zc   complex128
		zs   string
		zp   *int
		zfn  func()
		zif  any
		zsl  []int
		zm   map[int]int
		zch  chan int
		zarr [2]string
		zst  struct {
			a int
			b string
			c *int
		}
	)
	println(zb, zi, zf, zc, zs == "")
	println(zp == nil, zfn == nil, zif == nil, zsl == nil, zm == nil, zch == nil)
	println(zarr[1] == "", zst.a, zst.b == "", zst.c == nil)
	// Allocation via new, make, and composite literals with omitted fields
	// also yields zero values.
	println(*new(float32), make([]bool, 1)[0], Point{}.x)

	sub("Package initialization / Program initialization / Program execution")
	// initA depends on initC, so the order is initB, initC, initA; then
	// initD/initE (one call); then init().
	for _, s := range initLog {
		print(s, " ")
	}
	println()
	println(initA, initB, initC, initD, initE)
}

// ===========================================================================
// Spec: Errors
// ===========================================================================

// error is a predeclared interface: interface{ Error() string }.
type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

func errorf(msg string) error { return &stringError{msg} }

type codeError struct {
	code int
	err  error
}

func (e codeError) Error() string { return itoa(e.code) + ": " + e.err.Error() }
func (e codeError) Unwrap() error { return e.err } // conventional wrapping method

func mayFail(fail bool) (int, error) {
	if fail {
		return 0, codeError{404, errorf("not found")}
	}
	return 1, nil
}

func errorsChapter() {
	section("Errors")
	if _, err := mayFail(true); err != nil {
		println(err.Error())
		if ce, ok := err.(codeError); ok {
			println(ce.code, ce.Unwrap().Error())
		}
	}
	v, err := mayFail(false)
	println(v, err == nil)
	// An interface holding a typed nil pointer is not nil.
	var p *stringError
	var e error = p
	println(p == nil, e == nil)
}

// ===========================================================================
// Spec: Run-time panics
// ===========================================================================

// Run-time errors panic with a value of the implementation-defined
// interface type runtime.Error, which has a RuntimeError() method. That
// interface can be matched structurally without importing runtime.
type runtimeError interface {
	error
	RuntimeError()
}

func describe(r any) string {
	switch v := r.(type) {
	case nil:
		return "<no panic>"
	case runtimeError:
		return "runtime error value: " + v.Error()
	case error:
		return "error: " + v.Error()
	case string:
		return v
	default:
		return "non-string panic value"
	}
}

func recoverString(f func()) (s string) {
	defer func() { s = "recovered " + describe(recover()) }()
	f()
	return "no panic"
}

func runTimePanics() {
	section("Run-time panics")
	var nilMap map[string]int
	idx := 5
	var nilPtr *Point
	var iface any = "str"
	zero := 0
	ch := make(chan int)
	close(ch)
	sl := []int{1, 2}
	var unhashable any = []int{}
	cases := []struct {
		name string
		f    func()
	}{
		{"nil map write", func() { nilMap["k"] = 1 }},
		{"index out of range", func() { _ = sl[idx] }},
		{"nil pointer dereference", func() { println(nilPtr.x) }},
		{"failed type assertion", func() { _ = iface.(int) }},
		{"integer divide by zero", func() { _ = 1 / zero }},
		{"send on closed channel", func() { ch <- 1 }},
		{"unhashable map key", func() { m := map[any]int{}; m[unhashable] = 1 }},
		{"user panic with error", func() { panic(errorf("custom")) }},
	}
	for _, c := range cases {
		println(c.name+":", recoverString(c.f))
	}
}

// ===========================================================================
// Spec: System considerations (Package unsafe, Size and alignment guarantees)
// ===========================================================================

func systemConsiderations() {
	section("System considerations")
	// Package unsafe must be imported, and size/alignment can only be
	// observed through unsafe.Sizeof/Alignof/Offsetof: out of scope.
	println("unsafe and size/alignment: out of scope (require import \"unsafe\")")
}

// itoa formats an int without the standard library.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

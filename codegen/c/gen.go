package c

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// init registers the C backend with codegen, where the driver finds it
// by name.
func init() { codegen.Register(New()) }

// backend is the C code generation backend: it lowers a checked program to
// one C translation unit and gives the clang commands that build it. It
// holds no state, so one value serves every build.
type backend struct{}

// New returns the C code generation backend.
func New() codegen.Backend { return backend{} }

// Name returns c, the name the driver's --backend flag selects this
// backend by.
func (backend) Name() string { return "c" }

// Ext returns .c, the extension of the file the generated C is written
// to.
func (backend) Ext() string { return ".c" }

// BuildCmd runs clang with clangFlags, its warnings off, compiling src
// and linking it with objs. A warning about generated C names a temporary
// file and code whoever runs tack never wrote, and a valid program, such
// as one comparing a variable with itself, can cause one. Errors are still
// reported. The backend's own tests compile with -Wall -Wextra -Werror
// instead, so a warning the backend causes is caught there. opt becomes
// clang's -O flag. include, the standard library's include directory, is
// searched, with -I, for the <tack/tack.h> the generated source includes.
func (backend) BuildCmd(src, include string, objs []string, out string, opt codegen.Opt) []string {
	cmd := append([]string{"clang"}, clangFlags(opt)...)
	cmd = append(cmd, "-I", include, "-o", out, src)
	return append(cmd, objs...)
}

// clangFlags returns the flags every clang command of the backend that
// compiles C starts with: -w, warnings off (see BuildCmd);
// -fno-finite-loops, so clang marks no loop mustprogress and never
// assumes, as C11 6.8.5p6 lets it, that a loop ends, which keeps a loop
// that never ends in the generated C, the runtime's, std's and a
// package's own (clang marks no C function mustprogress with or without
// the flag; it does so only in C++); and -O at opt.
func clangFlags(opt codegen.Opt) []string {
	return []string{"-w", "-fno-finite-loops", "-O" + string(opt)}
}

// CompileCmd runs clang as BuildCmd does, with clangFlags, to compile
// src, a C file of the program's, to the object obj. The header is
// included before the file's first line, with -include, so every
// definition is checked against its extern func's prototype whether the
// file includes tack.h or not, and a stale tack.h beside the file, which
// an #include "tack.h" would find first, is skipped by its include guard.
// header's directory is also searched, with -I, for a file that includes
// tack.h by name, and after it include, the standard library's include
// directory, for the <tack/tack.h> that header includes when a signature
// uses a string, and that a file may include itself. The driver passes
// every path absolute, so none can be read as an option. The file is
// optimized at opt, as the generated source is.
func (backend) CompileCmd(src, header, include, obj string, opt codegen.Opt) []string {
	return compileCmd(src, obj, opt,
		"-I", filepath.Dir(header), "-I", include, "-include", header)
}

// CompileStdCmd runs clang as CompileCmd does, with the same flags and at
// the same level, to compile src, a C file of the standard library, to the
// object obj, with include, the standard library's include directory, on
// the include path, where the file finds <tack/tack.h>. No header is
// included first: a std file is compiled once for every program, so it
// cannot depend on one program's tack.h.
func (backend) CompileStdCmd(src, include, obj string, opt codegen.Opt) []string {
	return compileCmd(src, obj, opt, "-I", include)
}

// VersionCmd runs clang --version, whose output names clang's version, its
// default target and the directory it is installed in, which the driver
// keys the std objects it caches on.
func (backend) VersionCmd() []string { return []string{"clang", "--version"} }

// compileCmd is the clang command that compiles src to the object obj at
// opt, with clangFlags, as BuildCmd builds, and the flags extra before
// the output.
func compileCmd(src, obj string, opt codegen.Opt, extra ...string) []string {
	cmd := append([]string{"clang"}, clangFlags(opt)...)
	cmd = append(cmd, "-c")
	cmd = append(cmd, extra...)
	return append(cmd, "-o", obj, src)
}

// Generate lowers prog, which must have been type-checked with builtins,
// to one C translation unit. A program codegen.Refuse refuses, one not
// checked or with errors, is reported and nothing generated.
func (backend) Generate(prog *ast.Program, builtins *builtin.Set) ([]byte, []diag.Diagnostic) {
	if refused := codegen.Refuse(prog); refused != nil {
		return nil, refused
	}
	g := &generator{
		prog:     prog,
		builtins: builtins,
		decls:    prog.Decls(),
		globals:  make(map[*ast.Var]bool),
		pkgOf:    make(map[ast.Decl]*ast.Package),
		structs:  make(map[types.Type]*ast.TypeDecl),
		inPlace:  make(map[*ast.Var]bool),
		thunked:  make(map[*ast.Func]bool),
	}
	for _, pkg := range prog.Packages {
		for _, decl := range pkg.Decls() {
			g.pkgOf[decl] = pkg
			if td, ok := decl.(*ast.TypeDecl); ok && td.Interface() == nil {
				g.structs[td.Type] = td
			}
		}
	}
	return g.generate()
}

// walk walks every file of the program with v, as ast.Walk walks one.
func (g *generator) walk(v ast.Visitor) {
	for _, f := range g.prog.Files() {
		ast.Walk(f, v)
	}
}

// str is one entry in the string pool: the C identifier it is emitted under
// and the bytes it holds.
type str struct {
	label string
	value string
}

// generator is the state of lowering one program to C: the program and
// the builtins it was checked with, what is known of its declarations
// before any is lowered, the string pool, what lowering one body notes for
// the rest, and the buffer the translation unit is written to. Generate
// makes one for each program and uses it once.
type generator struct {
	prog     *ast.Program
	builtins *builtin.Set                 // the builtins a call can name
	decls    []ast.Decl                   // the program's package-level declarations, in order
	pkgOf    map[ast.Decl]*ast.Package    // the package of each of decls
	structs  map[types.Type]*ast.TypeDecl // the declaration of each struct and union type
	strPool  []str
	globals  map[*ast.Var]bool // the package-level variables

	// globalInits are the statements C's main runs first, which give each
	// global too deep for a C initializer its value (see deepGlobal).
	globalInits []string

	// inPlace holds the variables declared as a pointer to the value they
	// refer to, read through it: the payloads a switch on a union binds
	// (see union.go).
	inPlace map[*ast.Var]bool

	// thunks are the functions of several results named as values, in
	// the order first met, whose thunks are written after the
	// prototypes, and thunked marks each (see funcvalue.go).
	thunks  []*ast.Func
	thunked map[*ast.Func]bool

	// namedBy holds, for each identifier a lowered function body holds,
	// the C names of the functions whose bodies hold it (see unused.go).
	namedBy map[string]map[string]bool

	// scopes holds a deferScope for each block enclosing the statement
	// being generated, innermost last; see defer.go.
	scopes []*deferScope
	// frame is the function body being lowered; see defer.go.
	frame *deferFrame

	// spill is the expression being rendered, which keeps every
	// subexpression's C shallow, or nil between expressions; see
	// order.go.
	spill *spillScope

	// heldIndexes maps each index of the target of the assignment being
	// lowered that was evaluated into a temporary before its value to
	// that temporary, which elementRef renders in its place (see
	// holdTargetIndexes).
	heldIndexes map[*ast.Index]string

	buf    bytes.Buffer
	temps  int // how many temporaries have been named, for unique names
	labels int // how many blocks' labels have been named, likewise

	// deferring is whether a deferred body is being lowered; see defer.go.
	deferring bool
}

// generate lowers the program to its C translation unit. It first rejects
// a program whose types tack.h could not name, notes the globals and fills
// the string pool, then lowers every declaration and generic instance
// before it writes anything, so a construct it cannot lower gives
// diagnostics and no output. Only then does it write the file, in C's
// order of declaration: the preamble, the types, the pooled constants and
// the globals, the result structs, the prototypes and thunks, the
// functions, and the entry point (see emitEntryPoint).
func (g *generator) generate() ([]byte, []diag.Diagnostic) {
	// tack.h is written only for a build with C files, but a name it
	// cannot give is a problem with the program whatever is built.
	if diags := headerDiags(g.prog); len(diags) > 0 {
		return nil, diags
	}

	// Globals are known before anything is generated, since a body names a
	// global differently from a local, and a global's computed value may
	// hold strings to pool.
	for _, decl := range g.decls {
		if v, ok := decl.(*ast.Var); ok {
			g.globals[v] = true
		}
	}

	// The pool is filled first: generating a body needs the label of every
	// string it mentions.
	g.collectStrings()

	// Everything is generated before anything is written, so a construct the
	// backend cannot lower produces a diagnostic instead of a half-written
	// file.
	globals, sigs, bodies, diags := g.generateDecls()
	if len(diags) > 0 {
		return nil, diags
	}
	instSigs, instBodies, diags := g.generateInstances()
	if len(diags) > 0 {
		return nil, diags
	}
	sigs, bodies = append(sigs, instSigs...), append(bodies, instBodies...)
	// C's main, which emitEntryPoint writes, calls Tack's; a library has
	// neither.
	if !g.isLibrary() {
		g.noteNames("main", []string{g.mangle(nil, "main")})
	}
	builtins := g.builtins.Used(g.prog)

	var includes []string
	if g.usesType(types.UnsafeSize) || builtinsTake(builtins, types.UnsafeSize) {
		includes = append(includes, "#include <stddef.h>")
	}
	g.emitPreamble(includes, g.usesFloats())
	g.emitTypes()
	g.emitConstantPool(builtins, globals)
	g.emitGlobals(globals)
	g.emitResultStructs()
	g.emitCNameMacro()
	g.emitPrototypes()
	g.emitThunks()

	for i, sig := range sigs {
		g.emitFunc(sig, bodies[i])
	}

	g.emitEntryPoint()

	return g.buf.Bytes(), nil
}

// emitPreamble writes the headers. <stdbool.h> and <stdint.h> are always
// included, and includes, <stddef.h> when a type needs it, follow them; a
// freestanding C implementation has all three, so the generated C needs
// no C library. The builtins' bodies, which may, are compiled apart, from
// the runtime's C files. Last comes the runtime's <tack/tack.h>, which defines what every
// program shares: tack_string, and TACK_C_STR_LEN, which reads the length
// of a pooled string off its array, the array's size less the trailing
// zero emitConstantPool adds, rather than writing it out beside it. A
// program with floats then turns off FP_CONTRACT, which would let C fuse
// a * b + c into one operation, rounding once where Tack rounds twice, and
// so give a different result at one optimization level than at another.
func (g *generator) emitPreamble(includes []string, floats bool) {
	g.buf.WriteString("// Generated by the Tack compiler. Do not edit.\n\n")
	g.buf.WriteString("#include <stdbool.h>\n")
	g.buf.WriteString("#include <stdint.h>\n")
	for _, inc := range includes {
		g.buf.WriteString(inc + "\n")
	}
	g.buf.WriteString(stdHeader + "\n")
	if floats {
		g.buf.WriteString("#pragma STDC FP_CONTRACT OFF\n\n")
	}
}

// usesFloats reports whether the program has anything of a float type
// (see usesType).
func (g *generator) usesFloats() bool {
	return g.usesType(types.Float32) || g.usesType(types.Float64)
}

// usesType reports whether the program has anything of the basic type
// typ: a variable, a parameter, a result, an expression, or an element or
// field of an array or struct type. Only unsafe_size_t needs asking, since
// C declares its type, size_t, in a header of its own, <stddef.h>, which a
// program that never uses it does not include.
func (g *generator) usesType(typ types.Type) bool {
	table := g.prog.Types
	for i := range table.Count() {
		if slices.Contains(g.parts(types.Added(i)), typ) {
			return true
		}
	}
	found := false
	g.walk(func(n ast.Node) bool {
		found = found || hasType(n, typ)
		return !found
	})
	return found
}

// hasType reports whether the node n itself is of the basic type typ: a
// function with a result of it, a parameter or variable of it, or an
// expression of it.
func hasType(n ast.Node, typ types.Type) bool {
	switch x := n.(type) {
	case *ast.Func:
		return slices.Contains(x.ResultTypes, typ)
	case *ast.Param:
		return x.Type == typ
	case *ast.Var:
		return x.Type == typ
	case ast.Expr:
		return ast.TypeOf(x) == typ
	default:
		return false
	}
}

// poolCheckedZero pools the empty string when value is v, ok := a[i] on an
// array of strings, or t, ok := s[i:j], either of which gives one when out
// of range, or v, ok := x.(string), which gives one on a mismatch.
func (g *generator) poolCheckedZero(value ast.Expr) {
	if ix, ok := value.(*ast.Index); ok && ix.Type == types.String {
		g.addStr("")
	}
	if a, ok := value.(*ast.TypeAssert); ok && a.Type == types.String {
		g.addStr("")
	}
	if s, ok := value.(*ast.Slice); ok && s.Type == types.String {
		g.addStr("")
	}
}

// poolVar pools what the variable v needs: the empty string when it is a
// string starting from its zero value, and, for a global whose initializer
// slices a string, every string in its computed value.
func (g *generator) poolVar(v *ast.Var) {
	if v.Value == nil && v.Type == types.String {
		g.addStr("")
	}
	if g.globals[v] && v.Value != nil && holdsSlice(v.Value) {
		g.poolConst(v.Const)
	}
}

// poolConst pools every string in c, the computed value of a global: a
// string, or one inside an array's elements, a struct's fields or a
// union's payload.
func (g *generator) poolConst(c any) {
	switch v := c.(type) {
	case string:
		g.addStr(v)
	case []any:
		for _, x := range v {
			g.poolConst(x)
		}
	case ast.UnionConst:
		g.poolConst(v.Payload)
	}
}

// holdsSlice reports whether e holds a slice, at any depth.
func holdsSlice(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		_, found = n.(*ast.Slice)
		return !found
	})
	return found
}

// collectStrings pools every string literal in the file, deduplicated, and
// the empty string if a string variable starts from its zero value, and
// the computed value of a global whose initializer slices a string, which
// may be no literal's. The
// variables of a MultiVar, of a range loop and of a type switch's cases
// have no value of their own but never start from zero, so only the call,
// the ranged expression, the tag, the case values and the bodies are
// visited. A program that calls append pools the message of
// the error a failed append gives.
func (g *generator) collectStrings() {
	var visit ast.Visitor
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.StrLit:
			g.addStr(x.Value)
		case *ast.Var:
			g.poolVar(x)
		case *ast.MultiVar:
			g.poolCheckedZero(x.Value)
			ast.Walk(x.Value, visit)
			return false
		case *ast.MultiAssign:
			g.poolCheckedZero(x.Value)
		case *ast.ForRange:
			ast.Walk(x.X, visit)
			for _, stmt := range x.Body {
				ast.Walk(stmt, visit)
			}
			return false
		case *ast.Switch:
			walkSwitchValues(x, visit)
			return false
		}
		return true
	}
	g.walk(visit)
	for _, msg := range g.listErrors() {
		g.addStr(msg)
	}
}

// walkSwitchValues walks s with v, as ast.Walk does, but for the
// variables a type switch declares, its binding and each case's, which
// take the tag's value.
func walkSwitchValues(s *ast.Switch, v ast.Visitor) {
	ast.Walk(s.Tag, v)
	for _, cl := range s.Cases {
		for _, value := range cl.Values {
			ast.Walk(value, v)
		}
		for _, stmt := range cl.Body {
			ast.Walk(stmt, v)
		}
	}
}

// emitConstantPool writes the pooled strings as byte arrays, and then the
// helpers the program's code calls: builtins, the prototypes of the
// builtin extern funcs it calls, the division helpers and the slice
// helpers. Bytes rather than C
// string literals so that any byte survives without an escaping scheme,
// and a trailing zero so an empty literal is still a legal array; the zero
// is never written out, since every write is length-bounded. A string
// no function's lowered body and no global names, one whose only use is
// a length the backend computed, len("abc"), is declared
// __attribute__((unused)), as an unnamed function is (see maybeUnused).
func (g *generator) emitConstantPool(builtins []builtin.Func, globals []global) {
	for _, s := range g.strPool {
		named := func(v global) bool { return namesIdent(v.decl, s.label) }
		unused := "__attribute__((unused)) "
		if g.namedBy[s.label] != nil || slices.ContainsFunc(globals, named) {
			unused = ""
		}
		fmt.Fprintf(&g.buf, "%sstatic const unsigned char %s[%d] = {%s};\n",
			unused, s.label, len(s.value)+1, g.toBytes(s.value))
	}

	g.buf.WriteString("\n")

	// Each builtin is declared only when the program calls it, under its C
	// name; its body is in the runtime, which the driver links only for a
	// builtin some call needs.
	for _, fn := range builtins {
		g.buf.WriteString(g.builtinPrototype(fn) + "\n")
	}
	if len(builtins) > 0 {
		g.buf.WriteString("\n")
	}

	// Division and remainder by -1 are written out, since the smallest int
	// divided by -1 overflows and C leaves that undefined: the remainder is
	// 0, and the quotient, which the checker keeps from that one value, is
	// the negation. A shift's count is
	// tested against the width, which C leaves undefined at or past it.
	// Each helper is emitted only when the program uses its operator.
	for _, use := range g.divisions() {
		if use.op.IsShift() {
			g.emitShift(use.op, use.typ)
		} else {
			g.emitDivision(use.op, use.typ)
		}
	}
	if g.narrowsFloats() {
		g.emitFloatNarrowing()
	}

	// Each slice helper is emitted only when the program slices its type,
	// with or without a high bound.
	for _, h := range g.sliceHelpers() {
		g.emitSliceHelper(h)
	}
}

// division is one operator that goes through a helper, / % << or >>,
// applied in one integer type.
type division struct {
	op  ast.BinaryOp
	typ types.Type
}

// narrowsFloats reports whether the program converts a float64 to a
// float32 at run time, which emitFloatNarrowing writes the helper for.
func (g *generator) narrowsFloats() bool {
	found := false
	g.walk(func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok && narrowing(call) {
			found = true
		}
		return !found
	})
	return found
}

// narrowing reports whether call converts a float64 to a float32 at run
// time: a conversion to float32 of a float64 that is no constant.
func narrowing(call *ast.Call) bool {
	return call.Conv == types.Float32 && call.Const == nil && len(call.Args) == 1 &&
		ast.TypeOf(call.Args[0]) == types.Float64
}

// floatNarrowing is the name of the helper that converts a float64 to a
// float32.
const floatNarrowing = "tack_f32_of_f64"

// emitFloatNarrowing writes the helper that converts a float64 to a
// float32, rounding to the nearest float32, ties to even, as IEEE 754
// does. C leaves converting a value outside float's range undefined, so
// one at or past the midpoint between float's largest value and 2^128,
// 0x1.ffffffp+127, which rounds to an infinity, ties to even included, is
// given the infinity outright, and only a value C can convert is cast; NaN
// is in range, and casts to NaN.
func (g *generator) emitFloatNarrowing() {
	fmt.Fprintf(
		&g.buf,
		"%sstatic float %s(double d) {\n",
		g.maybeUnused(floatNarrowing),
		floatNarrowing,
	)
	g.buf.WriteString("    if (d >= 0x1.ffffffp+127) {\n        return __builtin_inff();\n    }\n")
	g.buf.WriteString(
		"    if (d <= -0x1.ffffffp+127) {\n        return -__builtin_inff();\n    }\n",
	)
	g.buf.WriteString("    return (float)d;\n}\n\n")
}

// divisions lists each division, remainder and shift the program does, by
// operator and type, in a fixed order, so the helpers are emitted the same
// way every time.
func (g *generator) divisions() []division {
	used := make(map[division]bool)
	g.walk(func(n ast.Node) bool {
		if d, ok := divisionIn(n); ok {
			used[d] = true
		}
		return true
	})

	var out []division
	for _, op := range []ast.BinaryOp{ast.Div, ast.Rem, ast.Shl, ast.Shr} {
		for typ := types.Type(0); typ < types.UntypedInt; typ++ {
			if used[division{op, typ}] {
				out = append(out, division{op, typ})
			}
		}
	}
	return out
}

// divisionIn returns the division, remainder or shift node n does at
// runtime, if it does one: an operator expression that is not a constant,
// or a compound assignment, which works in its target's type.
func divisionIn(n ast.Node) (division, bool) {
	isDivision := func(op ast.BinaryOp) bool { return op == ast.Div || op == ast.Rem || op.IsShift() }
	switch x := n.(type) {
	case *ast.Binary:
		return division{x.Op, x.Type}, isDivision(x.Op) && x.Const == nil
	case *ast.Assign:
		return division{x.Op, ast.TypeOf(x.Target)}, isDivision(x.Op)
	default:
		return division{}, false
	}
}

// emitDivision writes the helper for op in typ. A signed division by -1 is
// written out, since the type's smallest value divided by -1 overflows and C
// leaves that undefined, for the remainder too, which is 0. A program that
// compiles never divides the smallest value by -1, which the checker
// proves or the checked form tests first, but the quotient still does not
// reach C's division by -1: it is the negation, on uint64_t. An unsigned
// division cannot overflow, but goes through a helper too, so every
// division looks the same.
func (g *generator) emitDivision(op ast.BinaryOp, typ types.Type) {
	t := g.cType(typ)
	name := divisionName(op, typ)
	fmt.Fprintf(&g.buf, "%sstatic %s %s(%s a, %s b) {\n", g.maybeUnused(name), t, name, t, t)
	switch {
	case types.IsUnsigned(typ) && op == ast.Div:
		g.buf.WriteString("    return a / b;\n")
	case types.IsUnsigned(typ):
		g.buf.WriteString("    return a % b;\n")
	case op == ast.Div:
		fmt.Fprintf(&g.buf, "    return b == -1 ? (%s)(0 - (uint64_t)a) : (%s)(a / b);\n", t, t)
	default:
		fmt.Fprintf(&g.buf, "    return b == -1 ? 0 : (%s)(a %% b);\n", t)
	}
	g.buf.WriteString("}\n\n")
}

// emitShift writes the helper for the shift op in typ, which takes the
// count as a uint64_t, the widest unsigned type, so a count of any
// unsigned type converts to it unchanged. A count at or past the width
// shifts every bit out, which C leaves undefined: << gives 0, and >> 0, or
// -1 for a negative value. << shifts on uint64_t, as + adds, and keeps the
// low bits, so a signed value wraps rather than overflowing. A signed >>
// is written out as an arithmetic shift, a negative value's complement
// being shifted instead, so it never relies on how C shifts a negative
// value; a count past the width is one less than the width there, which
// gives the same 0 or -1.
//
//	static int8_t tack_shr_int8(int8_t a, uint64_t n) {
//	    if (n >= sizeof (int8_t) * 8) {
//	        n = sizeof (int8_t) * 8 - 1;
//	    }
//	    return a < 0 ? (int8_t)~(~a >> n) : (int8_t)(a >> n);
//	}
func (g *generator) emitShift(op ast.BinaryOp, typ types.Type) {
	t := g.cType(typ)
	name := divisionName(op, typ)
	fmt.Fprintf(&g.buf, "%sstatic %s %s(%s a, uint64_t n) {\n", g.maybeUnused(name), t, name, t)
	switch {
	case op == ast.Shl:
		fmt.Fprintf(&g.buf, "    return n >= sizeof (%s) * 8 ? 0 : (%s)((uint64_t)a << n);\n", t, t)
	case types.IsUnsigned(typ):
		fmt.Fprintf(&g.buf, "    return n >= sizeof (%s) * 8 ? 0 : (%s)(a >> n);\n", t, t)
	default:
		fmt.Fprintf(
			&g.buf,
			"    if (n >= sizeof (%s) * 8) {\n        n = sizeof (%s) * 8 - 1;\n    }\n",
			t,
			t,
		)
		fmt.Fprintf(&g.buf, "    return a < 0 ? (%s)~(~a >> n) : (%s)(a >> n);\n", t, t)
	}
	g.buf.WriteString("}\n\n")
}

// divisionName is the name of the helper for op in typ, such as
// tack_div_int8 or tack_shl_uint64.
func divisionName(op ast.BinaryOp, typ types.Type) string {
	names := map[ast.BinaryOp]string{ast.Div: "div", ast.Rem: "rem", ast.Shl: "shl", ast.Shr: "shr"}
	return "tack_" + names[op] + "_" + types.NewTable().Name(typ)
}

// global is a package-level variable lowered to C: its C name, and its
// declaration, one line, written without __attribute__((unused)), which
// is known only once every body is generated (see emitGlobals).
type global struct {
	name string
	decl string
}

// generateDecls generates every top-level declaration: each global as one
// line of C, and each function's body, returned alongside the functions'
// signatures in source order. An extern func's body is in C, and its
// prototype declares it; a generic function is never generated itself,
// but once for each of its instances (see generateInstances).
func (g *generator) generateDecls() (
	globals []global,
	sigs []string,
	bodies [][]string,
	diags []diag.Diagnostic,
) {
	for _, decl := range g.decls {
		switch d := decl.(type) {
		case *ast.Var:
			line, diags := g.generateGlobal(d)
			if len(diags) > 0 {
				return nil, nil, nil, diags
			}
			globals = append(globals, global{name: g.mangle(d, d.Name), decl: line})

		case *ast.Func:
			if !hasBody(d) {
				continue
			}
			body, diags := g.generateFuncBody(d)
			if len(diags) > 0 {
				return nil, nil, nil, diags
			}
			sigs = append(sigs, g.signature(d))
			bodies = append(bodies, body)

		case *ast.TypeDecl:
			// Its C struct comes from the type table, with every other type.

		default:
			return nil, nil, nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  fmt.Sprintf("unknown declaration type: %T", decl),
				Span:     decl.Spanned(),
			}}
		}
	}
	return globals, sigs, bodies, nil
}

// hasBody reports whether fn's body is generated as it is declared: it
// is neither an extern func nor a generic function.
func hasBody(fn *ast.Func) bool {
	return !fn.Extern && !fn.Generic()
}

// emitGlobals writes the package-level variable declarations, each that
// no function's lowered body names declared with __attribute__((unused)),
// as a function is (see maybeUnused): C warns about a static variable
// nothing names, and one may be named by no C, though Tack reads it, when
// only another global's value, which is computed, or a function never
// generated, a generic one with no instance, reads it.
func (g *generator) emitGlobals(globals []global) {
	if len(globals) == 0 {
		return
	}
	for _, v := range globals {
		g.buf.WriteString(g.maybeUnused(v.name) + v.decl + "\n")
	}
	g.buf.WriteString("\n")
}

// generateGlobal declares a package-level variable as a static C variable,
// const unless it is mutable, initialized with the value the type checker
// computed rather than its source expression. The checker has already ruled
// out overflow and division by zero, so the value is exactly what Tack means.
func (g *generator) generateGlobal(v *ast.Var) (string, []diag.Diagnostic) {
	value, ok := g.constOf(v.Type, v.Const)
	qualifier := "static const"
	if ok && cDepth(value) > maxDepth {
		// Too deep for clang to take as an initializer: the variable
		// starts zeroed, and C's main assigns its value before Tack's main
		// runs (see deepGlobal).
		ok = g.deepGlobal(v)
		value, qualifier = "{}", "static"
	}
	if v.Const == nil && v.Value == nil && g.isComposite(v.Type) {
		// An array or a struct with no value starts with every element or
		// field zero.
		value, ok = "{}", true
	}
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("package-level variable %s has no computed value", v.Name),
			Span:     v.NameSpan,
		}}
	}

	if v.Mutable {
		qualifier = "static"
	}
	return fmt.Sprintf(
		"%s %s %s = %s;",
		qualifier,
		g.cType(v.Type),
		g.mangle(v, v.Name),
		value,
	), nil
}

// intLiteral renders an integer constant as a C literal. A negative one is
// parenthesised, so a - before it cannot form --. The smallest int64 is
// written as an expression, since C has no literal for it: the literal after
// the minus sign would not fit. One too large for an int64 gets a u suffix,
// so C reads it as unsigned rather than warning that it does not fit.
func intLiteral(v constant.Int) string {
	switch {
	case v.Cmp(constant.Min()) == 0:
		return "(-9223372036854775807 - 1)"
	case v.IsNegative():
		return "(" + v.String() + ")"
	case !v.Fits(64, true):
		return v.String() + "u"
	default:
		return v.String()
	}
}

// typedConstLiteral renders c, the value an operator expression of type
// typ computed to, as constLiteral does, but as a float's literal when typ
// is a float type (see floatConst), whatever kind of constant c is.
func typedConstLiteral(c any, typ types.Type) (string, bool) {
	if c != nil && types.IsFloat(typ) {
		return floatConst(c, typ), true
	}
	return constLiteral(c)
}

// floatConst renders c, an untyped constant's exact value, a
// constant.Float or a constant.Int, given the float type typ, as the C
// literal of the value of typ it rounds to (see floatLiteral).
func floatConst(c any, typ types.Type) string {
	var v constant.Float
	switch k := c.(type) {
	case constant.Float:
		v = k
	case constant.Int:
		v = constant.FloatFromInt(k)
	}
	if typ == types.Float32 {
		f, _ := v.Float32()
		return floatLiteral(f)
	}
	f, _ := v.Float64()
	return floatLiteral(f)
}

// floatLiteral renders f, a float32 or a float64 the type checker found
// finite, as a C hexadecimal float literal, 0x1.8p+00, which C reads as
// exactly that value, with no decimal rounding in between: the f suffix
// makes a float32's a float. A negative one is parenthesised, as a
// negative integer is.
func floatLiteral[F float32 | float64](f F) string {
	s := strconv.FormatFloat(float64(f), 'x', -1, 64)
	if _, is32 := any(f).(float32); is32 {
		s += "f"
	}
	if math.Signbit(float64(f)) {
		return "(" + s + ")"
	}
	return s
}

// constLiteral renders the value a constant expression computed to, and
// reports whether there was one: an operator expression that is not a
// constant has none, and is lowered operator by operator instead.
func constLiteral(c any) (string, bool) {
	switch v := c.(type) {
	case constant.Int:
		return intLiteral(v), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
	}
}

// constValue renders a computed constant of a basic type as a C constant
// expression, usable to initialize a static variable; constOf renders an
// array's or a struct's. The smallest int64 is written as an expression,
// since C has no literal for it: the literal after the minus sign would not
// fit.
func (g *generator) constValue(c any) (string, bool) {
	switch v := c.(type) {
	case constant.Int:
		return intLiteral(v), true
	case bool:
		return strconv.FormatBool(v), true
	case string:
		return "{" + g.strParts(v) + "}", true
	case float32:
		return floatLiteral(v), true
	case float64:
		return floatLiteral(v), true
	default:
		return "", false
	}
}

// emitResultStructs defines, for each function with more than one result, the
// C struct it returns them in: one field for each result, named r0, r1, and
// so on, in order. C returns one value, and a struct is one value that holds
// all of them.
//
// An instance of a generic function has a results struct of its own, its
// members of its type arguments' types, and the generic function none.
func (g *generator) emitResultStructs() {
	for _, decl := range g.decls {
		fn, ok := decl.(*ast.Func)
		if ok && !fn.Generic() {
			g.emitResultStruct(fn)
		}
	}
	for _, inst := range g.prog.Instances {
		g.withInstance(inst, func() { g.emitResultStruct(inst.Func) })
	}
}

// emitResultStruct defines fn's results struct, if it has more than one
// result (see emitResultStructs).
func (g *generator) emitResultStruct(fn *ast.Func) {
	if len(fn.ResultTypes) < 2 {
		return
	}
	g.buf.WriteString("typedef struct {\n")
	for i, typ := range fn.ResultTypes {
		fmt.Fprintf(&g.buf, "    %s r%d;\n", g.cType(typ), i)
	}
	fmt.Fprintf(&g.buf, "} %s;\n\n", g.resultsName(fn))
}

// resultsName is the name of the struct fn, a function with several
// results, returns them in: tack_results_ and what follows tack_ in fn's
// own name, as mangle gives it. It starts tack_results_ where a mangled
// name starts with tack_ and a digit, so it collides with none, and two are
// told apart as mangled names are.
func (g *generator) resultsName(fn *ast.Func) string {
	return resultsOf(g.funcCName(fn))
}

// resultsOf returns the name of the results struct of the function whose
// C name is cname (see resultsName).
func resultsOf(cname string) string {
	return "tack_results_" + strings.TrimPrefix(cname, "tack_")
}

// emitPrototypes forward declares every Tack function. Tack lets a function
// call one declared later in the file; C does not, without a prototype.
// One that may go uncalled is declared so (see maybeUnused).
func (g *generator) emitPrototypes() {
	if len(g.decls) == 0 {
		return
	}

	for _, decl := range g.decls {
		fn, ok := decl.(*ast.Func)
		switch {
		case !ok || fn.Generic():
		case fn.Extern:
			fmt.Fprintf(&g.buf, "extern %s TACK_C_NAME(%q);\n",
				strings.TrimPrefix(g.signature(fn), "static "), fn.CName)
		default:
			g.buf.WriteString(g.maybeUnused(g.funcCName(fn)) + g.signature(fn) + ";\n")
		}
	}
	for _, inst := range g.prog.Instances {
		g.withInstance(inst, func() {
			g.buf.WriteString(
				g.maybeUnused(g.funcCName(inst.Func)) + g.signature(inst.Func) + ";\n",
			)
		})
	}

	g.buf.WriteString("\n")
}

// hasExterns reports whether the program declares an extern func.
func (g *generator) hasExterns() bool {
	for _, decl := range g.decls {
		if fn, ok := decl.(*ast.Func); ok && fn.Extern {
			return true
		}
	}
	return false
}

// emitCNameMacro defines TACK_C_NAME, which binds an extern func's
// prototype, declared under its mangled name, to the C symbol its body is
// defined under. The symbol is written with the platform's prefix for C
// names, __USER_LABEL_PREFIX__, an underscore on macOS and nothing on
// Linux, so the label is exactly the symbol a C definition of that name
// gets. Emitted only when the program has an extern func.
func (g *generator) emitCNameMacro() {
	if !g.hasExterns() {
		return
	}
	g.buf.WriteString("#define TACK_C_STR(x) #x\n")
	g.buf.WriteString("#define TACK_C_XSTR(x) TACK_C_STR(x)\n")
	g.buf.WriteString(
		"#define TACK_C_NAME(name) __asm__(TACK_C_XSTR(__USER_LABEL_PREFIX__) name)\n\n",
	)
}

// emitFunc writes a function whose signature is sig, and whose body,
// already lowered, is body, turning its indentation markers into spaces.
func (g *generator) emitFunc(sig string, body []string) {
	g.buf.WriteString(sig + " {\n")
	depth := 1
	for _, line := range body {
		switch line {
		case indentIn:
			depth++
		case indentOut:
			depth--
		default:
			for range depth {
				g.buf.WriteString("    ")
			}
			g.buf.WriteString(line)
			g.buf.WriteByte('\n')
		}
	}
	g.buf.WriteString("}\n\n")
}

// emitEntryPoint writes C's main, which exists only to call Tack's, once
// it has stored its argc and argv in tack_argc_ and tack_argv_, the
// globals the runtime's tack.h declares and this source defines, for std/os to
// read, and given each global too deep for a C initializer its value (see
// deepGlobal).
//
// A library, checked with --check, has no Tack main and so no C main; the
// statements that would give its globals their values are written as the
// body of tack_init_, unused, so that its C still shows them.
func (g *generator) emitEntryPoint() {
	if g.isLibrary() {
		if len(g.globalInits) == 0 {
			// Every function ends in a blank line, for the next one; the
			// file ends in one newline.
			if bytes.HasSuffix(g.buf.Bytes(), []byte("\n\n")) {
				g.buf.Truncate(g.buf.Len() - 1)
			}
			return
		}
		g.buf.WriteString("static void tack_init_(void) __attribute__((unused));\n")
		g.buf.WriteString("static void tack_init_(void) {\n")
		for _, init := range g.globalInits {
			g.buf.WriteString("    " + init + "\n")
		}
		g.buf.WriteString("}\n")
		return
	}
	g.buf.WriteString("int tack_argc_;\nchar **tack_argv_;\n\n")
	g.buf.WriteString("int main(int argc, char **argv) {\n")
	g.buf.WriteString("    tack_argc_ = argc;\n    tack_argv_ = argv;\n")
	for _, init := range g.globalInits {
		g.buf.WriteString("    " + init + "\n")
	}
	fmt.Fprintf(&g.buf, "    %s();\n", g.mangle(nil, "main"))
	g.buf.WriteString("    return 0;\n")
	g.buf.WriteString("}\n")
}

// isLibrary reports whether the program is a library, whose last package
// is not a main package: the driver's --check loads one, and it has no
// entry point.
func (g *generator) isLibrary() bool {
	return g.prog.Main().Name != "main"
}

// signature returns fn's C declaration, without a body or semicolon: a
// static function under its C name (see funcCName) taking each parameter,
// a method's receiver first, as paramType gives it.
func (g *generator) signature(fn *ast.Func) string {
	result := g.resultType(fn)

	if len(fn.Params) == 0 {
		return fmt.Sprintf("static %s %s(void)", result, g.funcCName(fn))
	}

	params := make([]string, len(fn.Params))
	for i, param := range fn.Params {
		params[i] = fmt.Sprintf("%s %s", g.paramType(param), paramName(param.Name))
	}
	return fmt.Sprintf(
		"static %s %s(%s)", result, g.funcCName(fn), strings.Join(params, ", "),
	)
}

// paramType returns the C type param is passed as: its type's, or for a
// mut parameter a pointer to it, the caller's variable.
func (g *generator) paramType(param *ast.Param) string {
	if param.Mut {
		return g.cType(param.Type) + " *"
	}
	return g.cType(param.Type)
}

// resultType returns the C type fn returns: void when it declares no
// result, its one result's type, or its results struct.
func (g *generator) resultType(fn *ast.Func) string {
	switch len(fn.ResultTypes) {
	case 0:
		return "void"
	case 1:
		return g.cType(fn.ResultTypes[0])
	default:
		return g.resultsName(fn)
	}
}

// generateFuncBody lowers fn's body, the outermost block of fn, as its
// frame (see funcFrame), led by a (void) cast of each parameter the
// lowered body never names (see unusedParams), and records what it names
// (see noteNames).
func (g *generator) generateFuncBody(fn *ast.Func) ([]string, []diag.Diagnostic) {
	body, diags := g.funcFrame(fn)
	if len(diags) > 0 {
		return nil, diags
	}
	g.noteNames(g.funcCName(fn), body)
	return append(unusedParams(fn, body), body...), nil
}

// unusedParams returns a statement casting each parameter of fn to void,
// in order, (void)p_x;, for each one body, fn's lowered body, never
// names. Tack asks nothing of a parameter, so a function may ignore one,
// as a callback or a generic function often does, and one whose every
// use lowers to a constant, len of an array parameter, is not named in C
// either; the cast reads it, so C's unused-parameter warning, which a
// strict build makes an error, never fires. A parameter the body names
// gets none, so most functions are lowered as they were.
func unusedParams(fn *ast.Func, body []string) []string {
	var casts []string
	for _, p := range fn.Params {
		name := paramName(p.Name)
		used := false
		for _, line := range body {
			if namesIdent(line, name) {
				used = true
				break
			}
		}
		if !used {
			casts = append(casts, "(void)"+name+";")
		}
	}
	return casts
}

// namesIdent reports whether line, a line of C, holds the identifier name
// whole: not as part of a longer identifier.
func namesIdent(line, name string) bool {
	for i := 0; ; {
		k := strings.Index(line[i:], name)
		if k < 0 {
			return false
		}
		start, end := i+k, i+k+len(name)
		if (start == 0 || !isIdentByte(line[start-1])) &&
			(end == len(line) || !isIdentByte(line[end])) {
			return true
		}
		i = start + 1
	}
}

// isIdentByte reports whether b can be part of a C identifier.
func isIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// generateBlock lowers a block of fn other than a loop's body; see block.
func (g *generator) generateBlock(body []ast.Stmt, fn *ast.Func) ([]string, []diag.Diagnostic) {
	return g.block(body, fn, false)
}

// generateLoopBody lowers the body of a loop of fn, the block a break or
// continue leaves; see block.
func (g *generator) generateLoopBody(body []ast.Stmt, fn *ast.Func) ([]string, []diag.Diagnostic) {
	return g.block(body, fn, true)
}

// block lowers a block of fn, in a deferScope of its own, loop being
// whether it is a loop's body. It ends in the cleanup chain of the defers
// it reaches and the owned locals it declares, and, for fn's body, the
// first block a frame opens, fn's own parameters, which falling off its
// end runs and its exits jump into.
func (g *generator) block(body []ast.Stmt, fn *ast.Func, loop bool) ([]string, []diag.Diagnostic) {
	sc := &deferScope{loop: loop}
	g.scopes = append(g.scopes, sc)
	defer func() { g.scopes = g.scopes[:len(g.scopes)-1] }()
	if g.frame != nil && g.frame.owner != nil {
		g.addParamFrees(g.frame.owner)
		g.frame.owner = nil
	}

	lines := make([]string, 0)
	for _, stmt := range body {
		got, diags := g.generateStmt(stmt, fn)
		if len(diags) > 0 {
			return nil, diags
		}
		lines = append(lines, got...)
	}
	if len(sc.entries) == 0 {
		return lines, nil
	}
	end, diags := g.chain(sc, body, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	return append(lines, end...), nil
}

// generateStmt lowers a statement of fn to C lines.
func (g *generator) generateStmt(stmt ast.Stmt, fn *ast.Func) ([]string, []diag.Diagnostic) {
	switch x := stmt.(type) {
	case *ast.Return:
		return g.generateReturn(x, fn)

	case *ast.If:
		return g.generateIf(x, fn)

	case *ast.For:
		return g.generateFor(x, fn)

	case *ast.ForRange:
		return g.generateForRange(x, fn)

	case *ast.Switch:
		return g.generateSwitch(x, fn)

	case *ast.Defer:
		return nil, g.addDefer(x)

	case *ast.Break:
		return g.generateJump("break;"), nil

	case *ast.Continue:
		return g.generateJump("continue;"), nil

	case *ast.Unreachable:
		return []string{"TACK_UNREACHABLE(" + cStringLiteral(x.Where) + ");"}, nil

	default:
		return g.generateSimpleStmt(stmt)
	}
}

// generateSimpleStmt lowers a statement that holds no other statement: a
// declaration, an assignment, an increment or decrement, or an expression.
func (g *generator) generateSimpleStmt(stmt ast.Stmt) ([]string, []diag.Diagnostic) {
	switch x := stmt.(type) {
	case *ast.Var:
		return g.generateVar(x)
	case *ast.Assign:
		return g.generateAssign(x)
	case *ast.IncDec:
		return g.generateIncDec(x)
	case *ast.ExprStmt:
		return g.generateExprStmt(x)
	case *ast.MultiVar:
		return g.generateMultiVar(x)
	case *ast.MultiAssign:
		return g.generateMultiAssign(x)
	default:
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown statement type: %T", stmt),
			Span:     stmt.Spanned(),
		}}
	}
}

// generateReturn lowers a return, with each value converted to the type of
// fn's result in the same position. Several values are returned together, as
// one value of fn's results struct. A return that leaves blocks with defers
// to run takes deferredReturn's form instead.
func (g *generator) generateReturn(ret *ast.Return, fn *ast.Func) ([]string, []diag.Diagnostic) {
	value, diags := g.returnValue(ret, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	if route := g.stops(len(g.scopes)); len(route) > 0 {
		return g.deferredReturn(value, route), nil
	}
	if value == "" {
		return []string{"return;"}, nil
	}
	return []string{fmt.Sprintf("return %s;", value)}, nil
}

// returnValue renders the value a return gives, as one C expression: its
// one value, a compound literal of fn's results struct holding several, or
// "" for a bare return. A view result is rendered as viewArg renders a
// view argument, a list or an array as a view of its elements in place,
// never copied.
func (g *generator) returnValue(ret *ast.Return, fn *ast.Func) (string, []diag.Diagnostic) {
	if len(ret.Values) == 0 {
		return "", nil
	}

	values := make([]string, len(ret.Values))
	for i, v := range ret.Values {
		var (
			value string
			diags []diag.Diagnostic
		)
		if g.prog.Types.IsView(fn.ResultTypes[i]) {
			value, diags = g.viewArg(v, fn.ResultTypes[i], nil)
		} else {
			value, diags = g.expr(v, fn.ResultTypes[i])
		}
		if len(diags) > 0 {
			return "", diags
		}
		values[i] = value
	}

	if len(values) == 1 {
		return values[0], nil
	}
	return g.sequenced(ret.Values, values, func(ops []string) string {
		return fmt.Sprintf("(%s){%s}", g.resultsName(fn), strings.Join(ops, ", "))
	}), nil
}

// parenthesised reports whether e is lowered to C wrapped in a pair of
// parentheses of its own: a binary expression, unless it is a constant,
// which is written as its value, or a division, a remainder or a shift,
// which calls a helper.
func parenthesised(e ast.Expr) bool {
	b, ok := e.(*ast.Binary)
	return ok && b.Const == nil && b.Op != ast.Div && b.Op != ast.Rem && !b.Op.IsShift()
}

// condHeader writes the opening line of an if whose condition is cond,
// rendered from e. A binary expression is already parenthesised, and a
// second pair makes clang warn about an equality test, so it only gets the
// statement's own.
func condHeader(keyword string, e ast.Expr, cond string) string {
	if parenthesised(e) {
		return fmt.Sprintf("%s %s {", keyword, cond)
	}
	return fmt.Sprintf("%s (%s) {", keyword, cond)
}

// generateFor lowers a loop to a C for (;;) that tests the loop's
// condition, when it has one, at the top of every pass and breaks when it
// does not hold:
//
//	for (;;) {
//	    if (!(cond)) {
//	        break;
//	    }
//	    body
//	}
//
// The C loop's controlling expression is always omitted, so it is a
// constant: C11 6.8.5p6 lets a C compiler assume that a loop whose
// controlling expression is not a constant, and whose body does no I/O,
// no volatile access and no synchronization, ends, and clang deletes such
// a loop that never does, or treats the way into it as unreachable. A
// Tack loop runs for as long as its condition holds, forever if it always
// does, so none of its C loops may carry that assumption. The condition
// is evaluated exactly when, and as often as, a while's would be. Tack's
// break and continue mean what C's do, so they lower directly. A loop
// with an init or post statement takes generateClauseFor's form instead.
func (g *generator) generateFor(loop *ast.For, fn *ast.Func) ([]string, []diag.Diagnostic) {
	if loop.Init != nil || loop.Post != nil {
		return g.generateClauseFor(loop, fn)
	}

	var test []string
	if loop.Cond != nil {
		cond, diags := g.expr(loop.Cond, types.Bool)
		if len(diags) > 0 {
			return nil, diags
		}
		test = breakUnless(loop.Cond, cond)
	}

	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}

	return loopFor("", test, body), nil
}

// loopFor returns the C for of a lowered loop, which has no controlling
// expression (see generateFor): for (;;), or for (;; post) when post is
// not empty, around test, the lines that test the loop's condition, and
// then body.
func loopFor(post string, test, body []string) []string {
	header := "for (;;) {"
	if post != "" {
		header = fmt.Sprintf("for (;; %s) {", post)
	}
	pass := make([]string, 0, len(test)+len(body))
	pass = append(append(pass, test...), body...)
	lines := append([]string{header}, indent(pass)...)
	return append(lines, "}")
}

// breakUnless returns the lines a lowered loop begins every pass with
// when it has a condition: an if that leaves the loop with break unless
// cond, rendered from e, holds. It stands in for the controlling
// expression the C loop does without; see generateFor. A binary
// expression is already parenthesised, so it gets no second pair.
func breakUnless(e ast.Expr, cond string) []string {
	if !parenthesised(e) {
		cond = "(" + cond + ")"
	}
	return []string{"if (!" + cond + ") {", "    break;", "}"}
}

// generateClauseFor lowers a three-clause loop to a C block holding the init
// statement and a C for whose post expression is the post statement, and
// which tests the condition at the top of every pass, as generateFor's
// loop does, rather than in its controlling expression, which it leaves
// out so that the C compiler cannot assume the loop ends:
//
//	{
//	    init;
//	    for (;; post) {
//	        if (!(cond)) {
//	            break;
//	        }
//	        body
//	    }
//	}
//
// The order is a C for's with the condition in it: the init statement,
// then the condition, then, each time it holds, the body and the post
// statement, then the condition again.
// The block scopes a variable the init declares to the loop, as Tack does,
// and ends in the chain that frees one that owns a list.
// Keeping the post in the for, rather than at the end of the body, is what
// makes continue run it. The post's lines are joined with the comma
// operator, so the post stays one C expression even if its Tack statement
// lowers to more than one C statement. A loop with no condition has no
// test, and one with no post statement is for (;;).
func (g *generator) generateClauseFor(loop *ast.For, fn *ast.Func) ([]string, []diag.Diagnostic) {
	// The block holding the loop is a scope of its own, whose chain frees
	// a list the init statement declares once the loop is done.
	sc := &deferScope{}
	g.scopes = append(g.scopes, sc)
	defer func() { g.scopes = g.scopes[:len(g.scopes)-1] }()

	init, inner, diags := g.clauseFor(loop, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	if len(sc.entries) > 0 {
		end, endDiags := g.chain(sc, []ast.Stmt{loop}, fn)
		if len(endDiags) > 0 {
			return nil, endDiags
		}
		inner = append(inner, end...)
	}
	lines := append([]string{"{"}, indent(append(init, inner...))...)
	return append(lines, "}"), nil
}

// clauseFor lowers the parts of a three-clause loop for
// generateClauseFor: the init statement's lines, with the temporaries an
// assignment of several values as the post statement needs, and the C for
// around the body.
func (g *generator) clauseFor(loop *ast.For, fn *ast.Func) ([]string, []string, []diag.Diagnostic) {
	var init []string
	if loop.Init != nil {
		lines, diags := g.generateSimpleStmt(loop.Init)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		init = lines
	}

	var test []string
	if loop.Cond != nil {
		c, diags := g.expr(loop.Cond, types.Bool)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		test = breakUnless(loop.Cond, c)
	}

	post := ""
	if m, ok := loop.Post.(*ast.MultiAssign); ok {
		decls, p, diags := g.multiAssignPost(m)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		init = append(init, decls...)
		post = p
	} else if loop.Post != nil {
		p, diags := g.postExpr(loop.Post)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		post = p
	}

	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, nil, diags
	}

	return init, loopFor(post, test, body), nil
}

// generateForRange lowers a range loop to a C block that evaluates the
// ranged expression once, before a counting for, and declares the loop's
// variables at the top of each iteration. Over an integer, the count is
// held in a temporary:
//
//	{
//	    const int64_t tack_n0 = count;
//	    for (int64_t tack_k1 = 0;; tack_k1++) {
//	        if (!(tack_k1 < tack_n0)) {
//	            break;
//	        }
//	        const int64_t v_i = tack_k1;
//	        body
//	    }
//	}
//
// The counter is tested at the top of each pass, not in the for's
// controlling expression, for the reason generateFor gives: the count
// bounds the loop, but a body that never ends would let the C compiler
// assume it does.
// The loop variable is a fresh const each iteration, as Tack's is immutable.
// The counter never overflows, since it is only incremented while below the
// count. The temporaries' names start with tack_ and have one underscore,
// which no mangled Tack name has. An array or a string is lowered by
// collectionRange.
func (g *generator) generateForRange(
	loop *ast.ForRange,
	fn *ast.Func,
) ([]string, []diag.Diagnostic) {
	steps, place, diags := g.exprSteps(loop.X, false)
	if len(diags) > 0 {
		return nil, diags
	}
	x, addr := withSteps(steps, place), withSteps(steps, "&"+place)
	xType := ast.TypeOf(loop.X)
	isMap := g.prog.Types.IsMap(xType)
	if g.bindsInPlace(loop) {
		g.inPlace[loop.Elem] = true
	}
	if isMap && loop.Var != nil && g.owned(loop.Var.Type) {
		g.inPlace[loop.Var] = true
	}
	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	if isMap {
		return g.mapRange(loop, x, body), nil
	}
	if !types.IsInteger(xType) {
		return g.collectionRange(loop, x, addr, body), nil
	}

	typ := g.cType(xType)
	n, k := g.temp("n"), g.temp("k")
	if loop.Var != nil {
		body = append(
			[]string{fmt.Sprintf("const %s %s = %s;", typ, declName(loop.Var), k)},
			body...)
	}
	return rangeBlock(fmt.Sprintf("const %s %s = %s;", typ, n, x), typ, k, n, body), nil
}

// collectionRange lowers a range loop over an array or a string, whose
// lowered value is x, its address addr, and lowered body body. The value is copied into a
// const temporary, which every iteration reads, so the body assigning the
// array, or the variable holding it, changes nothing the loop sees:
//
//	{
//	    const tack_array_3 tack_r0 = a;
//	    for (int64_t tack_k1 = 0;; tack_k1++) {
//	        if (!(tack_k1 < 4)) {
//	            break;
//	        }
//	        const int64_t v_i = tack_k1;
//	        const int64_t v_x = tack_r0.e[tack_k1];
//	        body
//	    }
//	}
//
// An array's length is a constant; a string's is read from the copy, as
// tack_r0.len, and its bytes as tack_r0.ptr[k]. An element of an owned
// type, of a list or a view, is bound in place, as a pointer to it, const
// tack_list_8 *const v_x = &tack_r0.ptr[tack_k1], and read through it. When the array cannot
// change while the loop runs (see rangesInPlace), x is a place, and the
// temporary is a pointer to it instead, const tack_array_3 *const tack_r0
// = &a, through which each element is read: the same values a copy gives,
// without making one. Over an array with
// no element variable nothing reads the copy, so the value is only
// evaluated, cast to void. A _ index gets no C variable.
func (g *generator) collectionRange(loop *ast.ForRange, x, addr string, body []string) []string {
	xType := ast.TypeOf(loop.X)
	r, k := g.temp("r"), g.temp("k")
	n, field := r+".len", "ptr"
	if g.prog.Types.IsArray(xType) {
		n, field = strconv.FormatInt(g.prog.Types.Len(xType), 10), "e"
	}
	eval := fmt.Sprintf("const %s %s = %s;", g.cType(xType), r, x)
	read := r
	if g.rangesInPlace(loop) {
		eval = fmt.Sprintf("const %s *const %s = %s;", g.cType(xType), r, addr)
		read = "(*" + r + ")"
	}

	var vars []string
	if loop.Var != nil && loop.Var.Name != ast.Blank {
		vars = append(vars, fmt.Sprintf("const int64_t %s = %s;", declName(loop.Var), k))
	}
	switch {
	case g.bindsInPlace(loop):
		vars = append(vars, fmt.Sprintf("const %s *const %s = &%s.%s[%s];",
			g.cType(loop.Elem.Type), declName(loop.Elem), read, field, k))
	case bindsElem(loop):
		vars = append(vars, fmt.Sprintf("const %s %s = %s.%s[%s];",
			g.cType(loop.Elem.Type), declName(loop.Elem), read, field, k))
	}

	if field == "e" && !bindsElem(loop) {
		eval = fmt.Sprintf("(void)%s;", x)
	}
	return rangeBlock(eval, "int64_t", k, n, append(vars, body...))
}

// bindsInPlace reports whether a range loop binds its element to a
// variable that refers to the element in place, a pointer to it read as
// (*v_x): an element of an owned type, of a list or a view, which is
// never copied out of it. The type checker keeps the loop's body from
// changing the list, or any element of it, while it runs.
func (g *generator) bindsInPlace(loop *ast.ForRange) bool {
	return bindsElem(loop) && g.owned(loop.Elem.Type)
}

// bindsElem reports whether a range loop binds its element to a variable,
// not _.
func bindsElem(loop *ast.ForRange) bool {
	return loop.Elem != nil && loop.Elem.Name != ast.Blank
}

// rangesInPlace reports whether a range loop can read the elements of the
// array it ranges over where they are rather than from a copy, since the
// array cannot change while the loop runs: the loop binds the element, the
// array is a place, a variable or a field or element of one, and the body
// never assigns, increments, decrements or passes as mut that variable or
// any part of it, nor, when it is a mutable global, which any function
// can assign, calls a function. Any index in the place is evaluated once,
// before the first iteration, as the copy's would be.
func (g *generator) rangesInPlace(loop *ast.ForRange) bool {
	root := ast.Root(loop.X)
	if root == nil || !g.prog.Types.IsArray(ast.TypeOf(loop.X)) || !bindsElem(loop) {
		return false
	}
	v, isVar := root.Def.(*ast.Var)
	global := isVar && g.globals[v] && v.Mutable
	for _, stmt := range loop.Body {
		if writesTo(stmt, root.Def) || global && stmtCallsFunc(stmt) {
			return false
		}
	}
	return true
}

// writesTo reports whether stmt, at any depth, assigns, increments,
// decrements or passes as mut decl or a part of it.
func writesTo(stmt ast.Stmt, decl ast.Decl) bool {
	found := false
	ast.Walk(stmt, func(n ast.Node) bool {
		var targets []ast.Expr
		switch x := n.(type) {
		case *ast.Assign:
			targets = []ast.Expr{x.Target}
		case *ast.IncDec:
			targets = []ast.Expr{x.Target}
		case *ast.MultiAssign:
			targets = x.Targets
		case *ast.MutArg:
			targets = []ast.Expr{x.X}
		}
		for _, t := range targets {
			if root := ast.Root(t); root != nil && root.Def == decl {
				found = true
			}
		}
		return !found
	})
	return found
}

// stmtCallsFunc reports whether stmt, at any depth, calls a Tack function.
func stmtCallsFunc(stmt ast.Stmt) bool {
	found := false
	ast.Walk(stmt, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && callsFunc(e) {
			found = true
		}
		return !found
	})
	return found
}

// rangeBlock assembles a lowered range loop: a C block holding eval, which
// evaluates what the loop ranges over, then a for that counts k, of the C
// type typ, up from 0 and runs body for as long as k is below n. The for
// has no controlling expression and tests k at the top of every pass, as
// generateFor's loop tests its condition, so that a body that never ends
// does not let the C compiler assume the loop does.
func rangeBlock(eval, typ, k, n string, body []string) []string {
	header := fmt.Sprintf("for (%s %s = 0;; %s++) {", typ, k, k)
	pass := make([]string, 0, 3+len(body))
	pass = append(pass, fmt.Sprintf("if (!(%s < %s)) {", k, n), "    break;", "}")
	pass = append(pass, body...)
	inner := append([]string{header}, indent(pass)...)
	inner = append(inner, "}")
	lines := append([]string{"{", "    " + eval}, indent(inner)...)
	return append(lines, "}")
}

// temp returns a fresh name for a temporary of the generated C, built from
// prefix: tack_ followed by prefix and a number unique in the file.
func (g *generator) temp(prefix string) string {
	name := fmt.Sprintf("tack_%s%d", prefix, g.temps)
	g.temps++
	return name
}

// stripParens removes the parentheses a binary expression is rendered with,
// for a condition placed where a second pair would make clang warn on an
// equality test, such as a switch case's test.
func stripParens(e ast.Expr, rendered string) string {
	if parenthesised(e) {
		return rendered[1 : len(rendered)-1]
	}
	return rendered
}

// postExpr renders a loop's post statement as one C expression: the
// expression statements it lowers to, without their semicolons, joined with
// the comma operator.
func (g *generator) postExpr(post ast.Stmt) (string, []diag.Diagnostic) {
	lines, diags := g.generateSimpleStmt(post)
	if len(diags) > 0 {
		return "", diags
	}
	exprs := make([]string, len(lines))
	for i, line := range lines {
		expr, ok := strings.CutSuffix(line, ";")
		if !ok {
			return "", []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "post statement does not lower to a C expression",
				Span:     post.Spanned(),
			}}
		}
		exprs[i] = expr
	}
	return strings.Join(exprs, ", "), nil
}

// generateIf lowers stmt, an if and the chain of else ifs after it, each
// branch's condition and block in source order, in a loop, so a chain of
// any length costs no stack. An else branch that is a single if, as an
// else if is, declares nothing and defers nothing, so it has no cleanup
// and no scope of its own (see block). A chain of up to maxChain branches
// is C's if, else if and else; a longer one is flatChain's.
func (g *generator) generateIf(stmt *ast.If, fn *ast.Func) ([]string, []diag.Diagnostic) {
	branches, stmt, diags := g.ifBranches(stmt, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	var els []string
	if stmt.Else != nil {
		var diags []diag.Diagnostic
		if els, diags = g.generateBlock(stmt.Else, fn); len(diags) > 0 {
			return nil, diags
		}
	}
	if len(branches) > maxChain {
		return g.flatChain(branches, stmt.Else != nil, els), nil
	}
	var lines []string
	for i, b := range branches {
		header := condHeader("if", b.e, b.cond)
		if i > 0 {
			header = "} else " + header
		}
		lines = append(append(lines, header), indent(b.then)...)
	}
	if stmt.Else != nil {
		lines = append(append(lines, "} else {"), indent(els)...)
	}
	return append(lines, "}"), nil
}

// ifBranches lowers the condition and block of stmt and of each else if
// after it, in order, and returns them and the chain's last if, whose
// else branch, if any, is not an if.
func (g *generator) ifBranches(
	stmt *ast.If,
	fn *ast.Func,
) ([]ifBranch, *ast.If, []diag.Diagnostic) {
	var branches []ifBranch
	for {
		cond, diags := g.expr(stmt.Cond, types.Bool)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		then, diags := g.generateBlock(stmt.Then, fn)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		branches = append(branches, ifBranch{e: stmt.Cond, cond: cond, then: then})
		next, ok := elseIf(stmt)
		if !ok {
			return branches, stmt, nil
		}
		stmt = next
	}
}

// ifBranch is one branch of a chain of ifs, lowered: its condition, the
// expression and its C, and its block.
type ifBranch struct {
	e    ast.Expr
	cond string
	then []string
}

// maxChain is how many branches a chain of else ifs may have before it is
// lowered flat (see flatChain). clang parses else if as an if nested in
// the else, and runs out of stack at a few thousand of them; no ordinary
// program has a chain anywhere near this long, so none of their C
// changes.
const maxChain = 64

// flatChain lowers a long chain of ifs, branches, with an else block els
// when hasElse, as a run of ifs one after another, none nested, each
// branch's block ending in a jump past the rest of the chain, and the
// else block after them all:
//
//	if (a) {
//	    ...
//	    goto tack_c0;
//	}
//	if (b) {
//	    ...
//	    goto tack_c0;
//	}
//	{
//	    ...
//	}
//	tack_c0:;
//
// So the conditions run in order, each only until one holds, exactly one
// branch runs, as in the chain, and a return, break or continue in a
// branch leaves before its jump. C sees every path a chain whose every
// branch returns can take, as it does an else if. The jump is forward,
// out of the branch's block, after the block's own cleanup.
func (g *generator) flatChain(branches []ifBranch, hasElse bool, els []string) []string {
	end := g.temp("c")
	var lines []string
	for _, b := range branches {
		lines = append(lines, condHeader("if", b.e, b.cond), indentIn)
		lines = append(append(lines, b.then...), "goto "+end+";", indentOut, "}")
	}
	if hasElse {
		lines = append(append(append(lines, "{"), indent(els)...), "}")
	}
	return append(lines, end+":;")
}

// elseIf returns the if that is s's else branch, when that branch is a
// single if, as an else if is.
func elseIf(s *ast.If) (*ast.If, bool) {
	if len(s.Else) != 1 {
		return nil, false
	}
	next, ok := s.Else[0].(*ast.If)
	return next, ok
}

// generateAssign sets a local to a new value. A place of an owned type
// frees its old value once the new one is computed (see ownedAssign).
// The target's indexes are evaluated before the value, as Tack defines
// it, where the order could show (see holdTargetIndexes).
func (g *generator) generateAssign(a *ast.Assign) ([]string, []diag.Diagnostic) {
	held, diags := g.holdTargetIndexes(a.Target, a.Value)
	if len(diags) > 0 {
		return nil, diags
	}
	value, diags := g.expr(a.Value, ast.TypeOf(a.Value))
	if len(diags) > 0 {
		g.heldIndexes = nil
		return nil, diags
	}
	name, diags := g.targetRef(a.Target)
	g.heldIndexes = nil
	if len(diags) > 0 {
		return nil, diags
	}
	if typ := ast.TypeOf(a.Target); a.Op == ast.InvalidOp && g.owned(typ) {
		return append(held, g.ownedAssign(typ, name, value)...), nil
	}
	if a.Op != ast.InvalidOp {
		// x op= v is x = x op v, spelled out the way Tack defines it, x
		// read before v is evaluated.
		operands := []ast.Expr{a.Target, a.Value}
		value = g.sequenced(operands, []string{name, value}, func(ops []string) string {
			return arith(a.Op, ast.TypeOf(a.Target), ops[0], ops[1])
		})
	}
	return append(held, fmt.Sprintf("%s = %s;", name, value)), nil
}

// holdTargetIndexes evaluates the indexes of an array's or a list's
// elements on target's path into temporaries, before value, when the
// order could show, a call in value changing what an index reads, and
// records each in heldIndexes for targetRef. It returns the statements
// that do so, or none when the order cannot show. Only an index's value
// is held, never an element's address: the store reads its container
// after value, which nothing in value can move, since a mut argument
// reaching the container alongside the target is an error, and no global
// or function value holds a list. A map's key needs no holding: has
// proves only a constant or a variable that cannot change.
func (g *generator) holdTargetIndexes(target, value ast.Expr) ([]string, []diag.Diagnostic) {
	var indexes []*ast.Index
	for x := target; ; {
		switch t := x.(type) {
		case *ast.Index:
			if !g.prog.Types.IsMap(ast.TypeOf(t.X)) {
				indexes = append(indexes, t)
			}
			x = t.X
			continue
		case *ast.Selector:
			x = t.X
			continue
		}
		break
	}
	operands := make([]ast.Expr, 0, len(indexes)+1)
	for _, ix := range indexes {
		operands = append(operands, ix.Index)
	}
	if !g.orderShows(append(operands, value)) {
		return nil, nil
	}
	g.heldIndexes = make(map[*ast.Index]string, len(indexes))
	var lines []string
	// The outermost index is first on the path, so the indexes are held
	// innermost first, left to right as they are written.
	for i := len(indexes) - 1; i >= 0; i-- {
		ix := indexes[i]
		if g.pure(ix.Index) {
			continue
		}
		index, diags := g.expr(ix.Index, types.Invalid)
		if len(diags) > 0 {
			g.heldIndexes = nil
			return nil, diags
		}
		t := g.temp("o")
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.operandType(ix.Index), t))
		lines = append(lines, fmt.Sprintf("%s = %s;", t, index))
		g.heldIndexes[ix] = t
	}
	return lines, nil
}

// targetRef renders an assignment target, or the place a mut argument
// names, as a C lvalue: a variable or a mut parameter, (*p_x), or an
// element of one, x.e[i], or of a list, x.ptr[i], or a field of one, x.f_name. The checker has
// proven every index in range.
func (g *generator) targetRef(target ast.Expr) (string, []diag.Diagnostic) {
	switch t := target.(type) {
	case *ast.Ident:
		switch def := t.Def.(type) {
		case *ast.Var:
			return g.varRef(def), nil
		case *ast.Param:
			if def.Mut {
				return g.paramRef(def), nil
			}
		}
	case *ast.Index:
		return g.elementRef(t)
	case *ast.Selector:
		base, diags := g.targetRef(t.X)
		if len(diags) > 0 {
			return "", diags
		}
		return base + "." + memberName(ast.TypeOf(t.X), t.Name), nil
	}
	return "", []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  "cannot assign to this expression",
		Span:     target.Spanned(),
	}}
}

// elementRef renders an element being assigned, or passed as mut, ix, as
// a C lvalue: x.e[i] for an array, x.ptr[i] for a list, and the value
// under a key for a map (see mapElementRef), its index the temporary
// holding it when the assignment held it (see holdTargetIndexes).
func (g *generator) elementRef(ix *ast.Index) (string, []diag.Diagnostic) {
	base, diags := g.targetRef(ix.X)
	if len(diags) > 0 {
		return "", diags
	}
	if g.prog.Types.IsMap(ast.TypeOf(ix.X)) {
		return g.mapElementRef(ix, base)
	}
	index, held := g.heldIndexes[ix]
	if !held {
		index, diags = g.expr(ix.Index, types.Invalid)
		if len(diags) > 0 {
			return "", diags
		}
	}
	if g.prog.Types.IsList(ast.TypeOf(ix.X)) {
		return fmt.Sprintf("%s.ptr[%s]", base, index), nil
	}
	return fmt.Sprintf("%s.e[%s]", base, index), nil
}

// generateIncDec lowers x++ to x = (x + 1) and x-- to x = (x - 1), the way
// a compound assignment is written out.
func (g *generator) generateIncDec(s *ast.IncDec) ([]string, []diag.Diagnostic) {
	name, diags := g.targetRef(s.Target)
	if len(diags) > 0 {
		return nil, diags
	}
	op := ast.Add
	if s.Dec {
		op = ast.Sub
	}
	return []string{fmt.Sprintf("%s = %s;", name, arith(op, ast.TypeOf(s.Target), name, "1"))}, nil
}

// generateExprStmt lowers an expression statement: a call to a named
// function or a method, pop and remove among them, which drop what they
// take (see dropTake), and replace, which drops the value it moves out
// (see dropReplace); or an expression that only gives a value, which the
// type checker warns about (see droppedValue).
func (g *generator) generateExprStmt(stmt *ast.ExprStmt) ([]string, []diag.Diagnostic) {
	if onlyValue(stmt.X) {
		return g.droppedValue(stmt.X)
	}
	call, ok := stmt.X.(*ast.Call)
	if !ok {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", stmt.X),
			Span:     stmt.Spanned(),
		}}
	}

	if call.Receiver() != nil {
		return g.generateCall("", call)
	}
	if isBuiltin(call, "pop") || isBuiltin(call, "remove") {
		return g.dropTake(call)
	}
	if isBuiltin(call, "replace") {
		return g.dropReplace(call)
	}
	if isBuiltin(call, "delete") {
		return g.dropDelete(call)
	}
	if isBuiltin(call, "swap") {
		return g.swapStmt(call)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", call.Fn),
			Span:     stmt.Spanned(),
		}}
	}

	return g.generateCall(id.Name, call)
}

// onlyValue reports whether e, an expression statement, only gives a
// value, as the type checker reads one (see typeck's onlyValue): a union
// built, a conversion, or a call of the builtin len.
func onlyValue(e ast.Expr) bool {
	if _, _, _, built := ast.Construction(e); built {
		return true
	}
	call, ok := e.(*ast.Call)
	return ok && (call.Conv != types.Invalid || isBuiltin(call, "len"))
}

// droppedValue lowers e, an expression statement that only gives a value
// (see onlyValue): e is evaluated, everything it calls with it, and its
// value dropped. The type checker rejects dropping an owned value, which
// nothing would free, so one is an internal error here.
func (g *generator) droppedValue(e ast.Expr) ([]string, []diag.Diagnostic) {
	typ := ast.TypeOf(e)
	if g.owned(typ) {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "internal error: a dropped value owns what it holds",
			Span:     e.Spanned(),
		}}
	}
	value, diags := g.expr(e, typ)
	if len(diags) > 0 {
		return nil, diags
	}
	return []string{fmt.Sprintf("(void)(%s);", value)}, nil
}

// generateVar declares a local. An immutable variable becomes a const C
// local, so the C compiler holds it to the same rule, unless it owns a
// list, which a move empties and its block's end frees. A variable without a
// value starts from its type's zero value. One that owns a list adds its
// free to its block's cleanup chain (see list.go).
func (g *generator) generateVar(v *ast.Var) ([]string, []diag.Diagnostic) {
	value := g.zeroValue(v.Type)
	if v.Value != nil {
		expr, diags := g.expr(v.Value, v.Type)
		if len(diags) > 0 {
			return nil, diags
		}
		value = expr
	}

	decl := fmt.Sprintf("%s %s = %s;", g.cType(v.Type), declName(v), value)
	if !v.Mutable && !g.owned(v.Type) {
		decl = "const " + decl
	}
	g.addFrees(v)
	return []string{decl}, nil
}

// generateMultiVar declares the variables of a MultiVar. Its source is
// evaluated once into const temporaries, and each variable is then declared
// from the value in its position: const unless the variable is mutable or
// owns a list, as generateVar does, which adds its free to the block's
// cleanup chain. A _ gets no variable.
func (g *generator) generateMultiVar(m *ast.MultiVar) ([]string, []diag.Diagnostic) {
	src, diags := g.multiValues(m.Value)
	if len(diags) > 0 {
		return nil, diags
	}

	names, lines := g.holdTemps(src)
	for i, v := range m.Vars {
		if v.Name == ast.Blank {
			continue
		}
		decl := fmt.Sprintf("%s %s = %s;", g.cType(v.Type), declName(v), src.field(names, i))
		if !v.Mutable && !g.owned(v.Type) {
			decl = "const " + decl
		}
		lines = append(lines, decl)
	}
	g.addFrees(m.Vars...)
	return lines, nil
}

// holdTemps names a const temporary for each of src's temps and declares
// it, initialized, returning the names and the declarations.
func (g *generator) holdTemps(src multiSource) (names, lines []string) {
	for _, t := range src.temps {
		name := g.temp("r")
		lines = append(lines, fmt.Sprintf("%s = %s;", t.decl("const ", name), t.initOf(names)))
		names = append(names, name)
	}
	return names, lines
}

// generateMultiAssign lowers an assignment of several values the way
// generateMultiVar lowers a declaration: the source in const temporaries,
// then each target assigned the value in its position.
func (g *generator) generateMultiAssign(m *ast.MultiAssign) ([]string, []diag.Diagnostic) {
	src, diags := g.multiValues(m.Value)
	if len(diags) > 0 {
		return nil, diags
	}

	names, lines := g.holdTemps(src)
	assigns, diags := g.fieldAssigns(m, src, names)
	if len(diags) > 0 {
		return nil, diags
	}
	for _, a := range assigns {
		lines = append(lines, a+";")
	}
	return lines, nil
}

// multiAssignPost lowers an assignment of several values used as a
// three-clause loop's post statement, which must be one C expression. The
// temporaries cannot be declared there, so they are returned as
// declarations for the loop's block, and the expression assigns the source
// to them, then each value to its target, joined with the comma operator.
func (g *generator) multiAssignPost(
	m *ast.MultiAssign,
) (decls []string, post string, diags []diag.Diagnostic) {
	src, diags := g.multiValues(m.Value)
	if len(diags) > 0 {
		return nil, "", diags
	}

	var names, exprs []string
	for _, t := range src.temps {
		name := g.temp("r")
		decls = append(decls, t.decl("", name)+";")
		exprs = append(exprs, fmt.Sprintf("%s = %s", name, t.initOf(names)))
		names = append(names, name)
	}
	assigns, diags := g.fieldAssigns(m, src, names)
	if len(diags) > 0 {
		return nil, "", diags
	}
	return decls, strings.Join(append(exprs, assigns...), ", "), nil
}

// fieldAssigns renders, for each target of m other than _, the C assignment
// of the value in its position, read from src's temporaries, named tmps,
// without a semicolon, after the frees of what a target of an owned type
// held.
func (g *generator) fieldAssigns(
	m *ast.MultiAssign,
	src multiSource,
	tmps []string,
) ([]string, []diag.Diagnostic) {
	assigns := make([]string, 0, len(m.Targets))
	for i, target := range m.Targets {
		if ast.IsBlank(target) {
			continue
		}
		ref, diags := g.targetRef(target)
		if len(diags) > 0 {
			return nil, diags
		}
		// The results are held in temporaries already, so the old value
		// of an owned target is freed just before it is replaced.
		for _, free := range g.frees(ast.TypeOf(target), ref) {
			assigns = append(assigns, strings.TrimSuffix(free, ";"))
		}
		assigns = append(assigns, fmt.Sprintf("%s = %s", ref, src.field(tmps, i)))
	}
	return assigns, nil
}

// multiSource is what gives a MultiVar or MultiAssign its values: temps,
// each a C expression of a C type evaluated once, in order, into a
// temporary, and field, which renders the value in position i read from
// those temporaries, named tmps.
type multiSource struct {
	field func(tmps []string, i int) string
	temps []multiTemp
}

// multiTemp is one temporary a multiSource is held in: its C type and the
// expression that initializes it. When ref is set, init is the address of
// a place of type typ, and the temporary is a pointer to const typ, read
// through; when mut is set, it is the address of a place the source
// changes, and the temporary a pointer to typ itself.
type multiTemp struct {
	// initFrom, when set, renders the initializer instead of init, from
	// the names of the temporaries before it, in order, which it reads.
	initFrom func(prev []string) string

	typ, init string
	ref, mut  bool
}

// initOf returns t's initializer, given the names of the temporaries held
// before it, in order.
func (t multiTemp) initOf(prev []string) string {
	if t.initFrom != nil {
		return t.initFrom(prev)
	}
	return t.init
}

// decl declares the temporary name, without a semicolon or an initializer:
// as a typ, qualified by qual, which is "const " or empty, or, when it is
// a ref, as a pointer to const typ, or a mut, as a pointer to typ, which
// the pointer itself is when qual is "const ".
func (t multiTemp) decl(qual, name string) string {
	switch {
	case t.ref:
		return fmt.Sprintf("const %s *%s%s", t.typ, qual, name)
	case t.mut:
		return fmt.Sprintf("%s *%s%s", t.typ, qual, name)
	default:
		return fmt.Sprintf("%s%s %s", qual, t.typ, name)
	}
}

// multiValues lowers the source of a MultiVar or MultiAssign: a call to a
// function with several results, held as its results struct and read field
// by field; a conversion that can fail, held as the value converted and
// read as the converted value, or 0 when it does not fit, and whether it
// fit; a division, held as its two operands and read as
// the result, or 0 when the divisor is zero, and whether it was not; an
// index or a slice; or a type assertion (see assertValues).
func (g *generator) multiValues(value ast.Expr) (multiSource, []diag.Diagnostic) {
	if bin, ok := value.(*ast.Binary); ok && (bin.Op == ast.Div || bin.Op == ast.Rem) {
		return g.divisionValues(bin)
	}
	if src, ok, diags := g.arithValues(value); ok {
		return src, diags
	}
	if ix, ok := value.(*ast.Index); ok {
		return g.indexValues(ix)
	}
	if s, ok := value.(*ast.Slice); ok {
		return g.sliceValues(s)
	}
	if a, ok := value.(*ast.TypeAssert); ok {
		return g.assertValues(a)
	}
	c, ok := value.(*ast.Call)
	if !ok {
		return multiSource{}, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", value),
			Span:     value.Spanned(),
		}}
	}

	if c.Conv != types.Invalid {
		return g.conversionValues(c)
	}

	return g.callValues(c)
}

// callValues lowers a, b := f(): the call, held as its results struct,
// read field by field, or the checked form of pop or remove (see
// takeValues).
func (g *generator) callValues(c *ast.Call) (multiSource, []diag.Diagnostic) {
	if isBuiltin(c, "pop") || isBuiltin(c, "remove") {
		return g.takeValues(c)
	}
	if isBuiltin(c, "delete") {
		return g.deleteValues(c)
	}
	fn := ast.Callee(c)
	if fn == nil {
		return multiSource{}, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a call of several results must call a declared function",
			Span:     c.Fn.Spanned(),
		}}
	}
	call, diags := g.callExpr(fn.Name, c)
	results := g.funcResultsName(fn.Value)
	if c.Dynamic == nil {
		_, cname := g.target(fn, c.TypeArgs)
		results = resultsOf(cname)
	}
	return multiSource{
		temps: []multiTemp{{typ: results, init: call}},
		field: func(tmps []string, i int) string { return fmt.Sprintf("%s.r%d", tmps[0], i) },
	}, diags
}

// divisionValues lowers q, ok := a / b (or a % b). Both operands are
// evaluated, the dividend first, whatever the divisor turns out to be, so a
// call in either always runs. ok is false for a zero divisor, and, for a
// signed division, for the smallest value of its type divided by -1,
// which overflows; q is then 0. A remainder by -1 is 0, and does not fail.
func (g *generator) divisionValues(bin *ast.Binary) (multiSource, []diag.Diagnostic) {
	if bin.Const != nil {
		return g.constantValues(bin)
	}
	a, diags := g.expr(bin.X, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	b, diags := g.expr(bin.Y, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	t := g.cType(bin.Type)
	return multiSource{
		temps: []multiTemp{{typ: t, init: a}, {typ: t, init: b}},
		field: func(tmps []string, i int) string {
			ok := divisionOk(bin, tmps[0], tmps[1])
			if i == 1 {
				return ok
			}
			return fmt.Sprintf("(%s ? %s : 0)", ok, arith(bin.Op, bin.Type, tmps[0], tmps[1]))
		},
	}, nil
}

// divisionOk returns the C condition under which bin, a checked division
// or remainder of the dividend a by the divisor b, both held in
// temporaries, succeeds: b is not zero, and, for a signed division, a is
// not its type's smallest value or b is not -1.
func divisionOk(bin *ast.Binary, a, b string) string {
	if bin.Op != ast.Div || types.IsUnsigned(bin.Type) {
		return fmt.Sprintf("(%s != 0)", b)
	}
	least := constant.FromInt64(math.MinInt64 >> (64 - types.MaxBits(bin.Type)))
	return fmt.Sprintf("(%s != 0 && (%s != -1 || %s != %s))", b, b, a, intLiteral(least))
}

// arithValues lowers v, ok := a + b, a - b, a * b or -a on integers, and
// reports whether value is one of those. Each operand is evaluated once,
// left first, into a temporary of the operation's type, whatever the
// result, so a call in either always runs. ok is clang's overflow builtin
// on them, which computes the exact result and says whether it is a value
// of the type the result is stored in, never overflowing in C. The value
// is the operation wrapped at the width, which is the exact result when
// ok is true, or 0 when it is not. A constant's is its value and true.
func (g *generator) arithValues(value ast.Expr) (multiSource, bool, []diag.Diagnostic) {
	var operands []ast.Expr
	var op ast.BinaryOp
	typ := ast.TypeOf(value)
	switch x := value.(type) {
	case *ast.Binary:
		if x.Op != ast.Add && x.Op != ast.Sub && x.Op != ast.Mul {
			return multiSource{}, false, nil
		}
		operands, op = []ast.Expr{x.X, x.Y}, x.Op
	case *ast.Unary:
		if x.Op != ast.Neg {
			return multiSource{}, false, nil
		}
		operands, op = []ast.Expr{x.X}, ast.Sub
	default:
		return multiSource{}, false, nil
	}
	if !types.IsInteger(typ) {
		return multiSource{}, false, nil
	}
	if constValueOf(value) {
		src, diags := g.constantValues(value)
		return src, true, diags
	}
	t := g.cType(typ)
	var temps []multiTemp
	for _, x := range operands {
		v, diags := g.expr(x, types.Invalid)
		if len(diags) > 0 {
			return multiSource{}, true, diags
		}
		temps = append(temps, multiTemp{typ: t, init: v})
	}
	builtins := map[ast.BinaryOp]string{
		ast.Add: "__builtin_add_overflow", ast.Sub: "__builtin_sub_overflow", ast.Mul: "__builtin_mul_overflow",
	}
	// The flag is a temporary of its own, so the builtin runs once.
	temps = append(temps, multiTemp{typ: "bool", initFrom: func(prev []string) string {
		l, r := "("+t+")0", prev[0]
		if len(prev) == 2 {
			l, r = prev[0], prev[1]
		}
		return fmt.Sprintf("!%s(%s, %s, &(%s){0})", builtins[op], l, r, t)
	}})
	return multiSource{
		temps: temps,
		field: func(tmps []string, i int) string {
			fits := tmps[len(tmps)-1]
			if i == 1 {
				return fits
			}
			l, r := "0", tmps[0]
			if len(tmps) == 3 {
				l, r = tmps[0], tmps[1]
			}
			return fmt.Sprintf("(%s ? %s : 0)", fits, arith(op, typ, l, r))
		},
	}, true, nil
}

// constValueOf reports whether value, an operation, is a constant whose
// value the checker computed.
func constValueOf(value ast.Expr) bool {
	switch x := value.(type) {
	case *ast.Binary:
		return x.Const != nil
	case *ast.Unary:
		return x.Const != nil
	default:
		return false
	}
}

// constantValues lowers a checked division, or checked arithmetic, of
// constants, whose value the checker has computed, its divisor not being
// zero and its result fitting its type: the value,
// written as a constant with no temporary, since evaluating it does
// nothing, and true.
func (g *generator) constantValues(bin ast.Expr) (multiSource, []diag.Diagnostic) {
	v, diags := g.expr(bin, types.Invalid)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	return multiSource{
		field: func(_ []string, i int) string {
			if i == 1 {
				return "true"
			}
			return v
		},
	}, nil
}

// conversionValues lowers v, ok := T(x). A constant's is its value and
// true. Otherwise x is held in a temporary, and the value is T's cast of
// it when it fits T, and 0 when it does not, so a cast is only ever made
// of a value T holds: C leaves converting a float outside an integer
// type's range undefined.
func (g *generator) conversionValues(c *ast.Call) (multiSource, []diag.Diagnostic) {
	if c.Const != nil {
		v, _ := typedConstLiteral(c.Const, c.Conv)
		return multiSource{field: func(_ []string, i int) string {
			if i == 1 {
				return "true"
			}
			return v
		}}, nil
	}
	arg, diags := g.expr(c.Args[0], types.Invalid)
	from, to := ast.TypeOf(c.Args[0]), c.Conv
	return multiSource{
		temps: []multiTemp{{typ: g.cType(from), init: arg}},
		field: func(tmps []string, i int) string {
			fits := fitsExpr(from, to, tmps[0])
			if i == 1 {
				return fits
			}
			cast := fmt.Sprintf("(%s)%s", g.cType(to), tmps[0])
			if from == types.Float64 && to == types.Float32 {
				cast = g.convert(from, to, tmps[0])
			}
			return fmt.Sprintf("(%s ? %s : 0)", fits, cast)
		},
	}, diags
}

// fitsExpr renders a C test of whether v, of the integer type from, is also
// a value of the integer type to; or, of a float type, truncates to one
// (see floatFitsExpr). Anything converts to a float.
func fitsExpr(from, to types.Type, v string) string {
	switch {
	case types.IsFloat(to):
		return "true"
	case types.IsFloat(from):
		return floatFitsExpr(from, to, v)
	}
	if types.TargetWidth(from) || types.TargetWidth(to) {
		return targetFitsExpr(from, to, v)
	}
	lo, hi := bounds(to)
	return withinExpr(from, v, lo, hi)
}

// floatFitsExpr renders a C test of whether v, of the float type from,
// truncates toward zero to a value of the integer type to, comparing v
// with bounds that are values of from exactly, so the test is exact: NaN
// fails every comparison, and an infinity one of them. An unsigned n-bit
// to takes -1 < v < 2^n. A signed one takes v < 2^(n-1) above, and below
// v > -2^(n-1) - 1 when from holds that value, and v >= -2^(n-1) when it
// does not, as int64 from a float64 or int32 from a float32: from then has
// no value between the two, -2^(n-1) being a power of two whose next value
// down is further from it than 1. A target-width to's bound is 2^n of its
// real width n, computed in C as (F)(SIZE_MAX / 2 + 1) * 2, both steps
// exact.
func floatFitsExpr(from, to types.Type, v string) string {
	lit := func(x float64) string {
		if from == types.Float32 {
			return floatLiteral(float32(x))
		}
		return floatLiteral(x)
	}
	if types.TargetWidth(to) {
		return fmt.Sprintf("(%s > %s && %s < (%s)(%s / 2 + 1) * %s)",
			v, lit(-1), v, basicCType(from), maxMacros[to], lit(2))
	}
	n := types.Bits(to)
	if types.IsUnsigned(to) {
		return fmt.Sprintf("(%s > %s && %s < %s)", v, lit(-1), v, lit(math.Ldexp(1, n)))
	}
	half := math.Ldexp(1, n-1)
	precision := 53
	if from == types.Float32 {
		precision = 24
	}
	lower := fmt.Sprintf("%s >= %s", v, lit(-half))
	if n <= precision {
		lower = fmt.Sprintf("%s > %s", v, lit(-half-1))
	}
	return fmt.Sprintf("(%s && %s < %s)", lower, v, lit(half))
}

// targetFitsExpr is fitsExpr when from or to is a target-width type, whose
// largest value C knows, as SIZE_MAX or UINTPTR_MAX, but the compiler does
// not. A from that widens to to always fits. Neither type is ever negative, so a signed from is tested against 0.
// A from that can exceed the target-width to's narrowest largest value,
// types.PortableMax, is tested against its real largest, and a
// target-width from against a to's largest value below the largest
// uint64. Each such v is compared as a uint64_t, a value of any of these
// types that is not negative being one, so the comparison is never
// between a signed and an unsigned type, and never one clang can tell is
// always true on a target where the types are as wide as each other.
func targetFitsExpr(from, to types.Type, v string) string {
	if types.Widens(from, to) {
		return "true"
	}
	var tests []string
	fromMin, fromMax := bounds(from)
	if fromMin.IsNegative() {
		tests = append(tests, v+" >= 0")
	}
	_, toMax := bounds(to)
	switch {
	case types.TargetWidth(to) && fromMax.Cmp(constant.FromUint64(types.PortableMax)) > 0:
		tests = append(tests, fmt.Sprintf("(uint64_t)%s <= %s", v, maxMacros[to]))
	case types.TargetWidth(from) && toMax.Cmp(constant.FromUint64(math.MaxUint64)) < 0:
		tests = append(tests, fmt.Sprintf("(uint64_t)%s <= %s", v, intLiteral(toMax)))
	}
	if len(tests) == 0 {
		return "true"
	}
	return "(" + strings.Join(tests, " && ") + ")"
}

// maxMacros names the C macro, from <stdint.h>, for the largest value of
// each target-width type.
var maxMacros = map[types.Type]string{
	types.UnsafeSize:    "SIZE_MAX",
	types.UnsafeUintptr: "UINTPTR_MAX",
}

// withinExpr renders a C test of whether v, of the integer type from, lies
// in [lo, hi]. Only the bounds a value of from can actually fall outside
// are tested, since clang warns about a comparison that is always true; an
// empty range, hi below lo, is never met.
func withinExpr(from types.Type, v string, lo, hi constant.Int) string {
	if hi.Cmp(lo) < 0 {
		return "false"
	}
	fromMin, fromMax := bounds(from)

	var tests []string
	if fromMin.Cmp(lo) < 0 {
		tests = append(tests, fmt.Sprintf("%s >= %s", v, intLiteral(lo)))
	}
	if fromMax.Cmp(hi) > 0 {
		tests = append(tests, fmt.Sprintf("%s <= %s", v, intLiteral(hi)))
	}
	if len(tests) == 0 {
		return "true"
	}
	return "(" + strings.Join(tests, " && ") + ")"
}

// bounds returns the smallest and largest values of a sized integer type.
// A target-width type's largest is the largest it can have on any target,
// a 64-bit one's.
func bounds(typ types.Type) (lo, hi constant.Int) {
	n := types.MaxBits(typ)
	if types.IsUnsigned(typ) {
		return constant.Int{}, constant.FromUint64(math.MaxUint64 >> (64 - n))
	}
	return constant.FromInt64(
			math.MinInt64 >> (64 - n),
		), constant.FromInt64(
			math.MaxInt64 >> (64 - n),
		)
}

// zeroValue is the C expression for a type's zero value, or, for a list,
// a map, an Allocator or a view, which have none in Tack, the zeroed value
// the ret variable of a function returning one starts as, which it never
// returns, and the value v, ok := pop(mut l) or v, ok := delete(mut m, k)
// gives with ok false, which is never read. A map is a C struct, zeroed
// an empty map with no buffer, which freeing leaves alone. A function
// value is a C function pointer, so its 0 is a null pointer, which is
// likewise never called. The empty string
// points at a pooled empty array rather than NULL, so writing it never hands
// libc a null pointer. An array or a struct is zeroed in full, so a string
// in it starts with a null pointer and a length of 0, which print's C body
// never passes on. An any's zero value, which v, ok := args[i] gives out
// of range, is a zeroed tack_any: type id 0, TACK_TYPE_INVALID, which
// holds nothing, and a null pointer, never read.
func (g *generator) zeroValue(typ types.Type) string {
	table := g.prog.Types
	if g.isComposite(typ) || table.IsList(typ) || table.IsMap(typ) || table.IsView(typ) ||
		typ == types.Allocator || typ == types.Any {
		return fmt.Sprintf("(%s){}", g.cType(typ))
	}
	switch typ {
	case types.String:
		return g.stringValue("")
	case types.Bool:
		return "false"
	default:
		return "0"
	}
}

// indentIn and indentOut are the marker lines indent puts around a block.
// No line of C holds a NUL byte, so neither can be mistaken for code.
const (
	indentIn  = "\x00indent"
	indentOut = "\x00dedent"
)

// indent shifts a nested block one level right. Function bodies are emitted at
// one level already, so this is what keeps branches readable. It only marks
// the block: emitFunc turns the markers into spaces once, as it writes each
// line. Prefixing every line here instead rebuilt each line once per level
// around it, so deep nesting cost the cube of its depth.
func indent(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, indentIn)
	out = append(out, lines...)
	return append(out, indentOut)
}

// generateCall lowers a call statement: to a builtin, as a call to its C
// function, or to a declared function.
func (g *generator) generateCall(name string, call *ast.Call) ([]string, []diag.Diagnostic) {
	if fn, ok := g.builtins.Called(call); ok {
		expr, diags := g.builtinCall(fn, call)
		if len(diags) > 0 {
			return nil, diags
		}
		return []string{expr + ";"}, nil
	}
	expr, diags := g.callExpr(name, call)
	if len(diags) > 0 {
		return nil, diags
	}
	return []string{expr + ";"}, nil
}

// callExpr renders a call to a declared function as a C expression, an
// argument to a view parameter as a view (see viewArg), a value passed as
// any as a tack_any of it (see boxed), and the values for a variadic
// parameter as one view of them (see packed). Every argument, each value
// for a variadic among them, is an operand evaluated left to right (see
// sequenced) before it is made an any and the values are packed.
//
// A method call is lowered as the call of its method's C function with
// the receiver as the first argument (see lowered), so the receiver is
// the first operand evaluated, a mut one as the address of its place.
func (g *generator) callExpr(name string, call *ast.Call) (string, []diag.Diagnostic) {
	if call.Receiver() != nil && call.Method == nil {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a method call must be checked before code is generated for it",
			Span:     call.Spanned(),
		}}
	}
	if call.Dynamic != nil {
		return g.dynamicCall(call)
	}
	typeArgs := call.TypeArgs
	call = lowered(call)
	callee := g.mangle(nil, name)
	var fn *ast.Func
	if f := ast.Callee(call); f != nil {
		fn, callee = g.target(f, typeArgs)
		fn = atCall(fn, call)
	}
	args, diags := g.callArgs(call, fn)
	if len(diags) > 0 {
		return "", diags
	}
	params := paramsOf(fn)
	// box makes an any of each argument passed as one, once the order of
	// evaluation has held the arguments it needs to (see boxed).
	box := func(ops []string) []string {
		out := slices.Clone(ops)
		for i, a := range call.Args {
			if g.boxesAt(call, params, i) {
				out[i] = g.boxed(a, ops[i], ops[i] != args[i])
			}
		}
		return out
	}
	build := func(ops []string) string {
		return fmt.Sprintf("%s(%s)", callee, strings.Join(box(ops), ", "))
	}
	if packs(call, params) {
		fixed, view := len(params)-1, params[len(params)-1].Type
		build = func(ops []string) string {
			ops = box(ops)
			all := append(slices.Clone(ops[:fixed]), g.packed(view, ops[fixed:]))
			return fmt.Sprintf("%s(%s)", callee, strings.Join(all, ", "))
		}
	}
	return g.sequencedAs(call.Args, args, g.argTypes(call, params), build), nil
}

// dynamicCall renders call, a call through a function value (see
// ast.Call.Dynamic), as a C expression: the value converted back to its
// function's pointer type and called with the arguments, rendered as a
// call by name renders them. The value is an operand evaluated first,
// before the arguments, left to right, so an argument that changes the
// variable holding it, through a mut argument, changes what a later call
// calls but not this one.
func (g *generator) dynamicCall(call *ast.Call) (string, []diag.Diagnostic) {
	fn := call.Dynamic
	value, diags := g.expr(call.Fn, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	args, diags := g.callArgs(call, fn)
	if len(diags) > 0 {
		return "", diags
	}
	operands := append([]ast.Expr{call.Fn}, call.Args...)
	rendered := append([]string{value}, args...)
	ctypes := append([]string{funcPtr}, g.argTypes(call, fn.Params)...)
	build := func(ops []string) string {
		return fmt.Sprintf("%s(%s)", g.dynamicCallee(fn.Value, ops[0]), strings.Join(ops[1:], ", "))
	}
	return g.sequencedAs(operands, rendered, ctypes, build), nil
}

// expr renders an expression as C. want is the type the context requires;
// passing types.Invalid means any type the expression happens to have.
// An expression a context renders on its own keeps its C shallow by
// holding deep parts in temporaries, assigned first (see exprSteps); one
// rendered as part of another leaves that to the outer one (see spilled).
func (g *generator) expr(e ast.Expr, want types.Type) (string, []diag.Diagnostic) {
	// In an instance of a generic function, a type made of a type
	// parameter and the type it stands for are one type.
	table := g.prog.Types
	if got := table.Bound(ast.TypeOf(e)); !types.Widens(got, table.Bound(want)) &&
		want != types.Invalid {
		return "", []diag.Diagnostic{
			{
				Severity: diag.Error,
				Message: fmt.Sprintf(
					"cannot use %s as %s",
					g.prog.Types.Name(got),
					g.prog.Types.Name(want),
				),
				Span: e.Spanned(),
			},
		}
	}

	if lit, ok := g.literal(e); ok {
		return lit, nil
	}
	if g.spill != nil {
		return g.spilled(e)
	}
	steps, x, diags := g.exprSteps(e, false)
	return withSteps(steps, x), diags
}

// exprNode lowers an expression that is not a literal, by its kind.
func (g *generator) exprNode(e ast.Expr) (string, []diag.Diagnostic) {
	switch x := e.(type) {
	case *ast.Ident:
		return g.identExpr(x)
	case *ast.Binary:
		return g.binaryExpr(x)
	case *ast.Unary:
		return g.unaryExpr(x)
	case *ast.Call:
		return g.callValue(x)
	case *ast.CompositeLit:
		return g.compositeLit(x)
	case *ast.Index, *ast.Slice:
		return g.elementExpr(x)
	case *ast.Selector:
		return g.selectorExpr(x)
	case *ast.MutArg, *ast.OwnArg:
		return g.argument(x)
	default:
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", e),
			Span:     e.Spanned(),
		}}
	}
}

// literal renders a literal, or an expression the checker computed the
// value of, as C, and reports whether e was one.
func (g *generator) literal(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Binary:
		return typedConstLiteral(x.Const, x.Type)
	case *ast.Unary:
		return typedConstLiteral(x.Const, x.Type)
	case *ast.Call:
		// len of a string literal; len of an array still names the array,
		// so lenValue writes it.
		if len(x.Args) == 1 {
			if _, isLit := x.Args[0].(*ast.StrLit); isLit {
				return constLiteral(x.Const)
			}
		}
		return "", false
	default:
		return g.plainLiteral(e)
	}
}

// plainLiteral renders an int, character, string or bool literal,
// noerror, a zeroed tack_error, or c_allocator, std's TACK_C_ALLOCATOR, as C, and reports whether e was one. A negative int is parenthesised, so a minus
// sign before it can never make C's decrement operator, and the smallest int
// is written as an expression, since C has no literal for it.
func (g *generator) plainLiteral(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		if types.IsFloat(x.Type) {
			return floatConst(x.Value, x.Type), true
		}
		return intLiteral(x.Value), true
	case *ast.FloatLit:
		return floatConst(x.Value, x.Type), true
	case *ast.CharLit:
		// A plain C int literal: C converts it to whatever type its
		// context needs, and its value, 0 to 255, survives every
		// conversion the checker allows a uint8.
		return strconv.Itoa(int(x.Value)), true
	case *ast.StrLit:
		return g.stringValue(x.Value), true
	case *ast.BoolLit:
		return strconv.FormatBool(x.Value), true
	case *ast.NoErrorLit:
		return g.zeroValue(types.Error), true
	case *ast.CAllocatorLit:
		return "TACK_C_ALLOCATOR", true
	default:
		return "", false
	}
}

// identExpr renders a reference to a parameter or variable as the C name it
// was declared under, read through the pointer for a mut parameter or a
// union's payload a switch binds, or, for a variable whose value the name
// moves, as the move (see moveValue), and a function named as a value as
// a function pointer (see funcValueRef).
func (g *generator) identExpr(id *ast.Ident) (string, []diag.Diagnostic) {
	switch def := id.Def.(type) {
	case *ast.Func:
		if def != nil && def.Value != types.Invalid {
			return g.funcValueRef(def), nil
		}
	case *ast.Param:
		if id.Move {
			return g.moveValue(def.Type, g.paramRef(def)), nil
		}
		return g.paramRef(def), nil
	case *ast.Var:
		if id.Move {
			return g.moveValue(def.Type, g.varRef(def)), nil
		}
		if g.inPlace[def] {
			return "(*" + varName(def.Name) + ")", nil
		}
		return g.varRef(def), nil
	}
	return "", []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  fmt.Sprintf("unbound identifier: %s", id.Name),
		Span:     id.Span,
	}}
}

// binaryExpr renders an infix operation as C.
func (g *generator) binaryExpr(bin *ast.Binary) (string, []diag.Diagnostic) {
	left, diags := g.expr(bin.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	// The right side of && and || runs only when the left does not
	// decide, so what holds its deep parts stays inside it.
	var right string
	if bin.Op.IsLogical() {
		right, diags = g.otherScope(bin.Y)
	} else {
		right, diags = g.expr(bin.Y, types.Invalid)
	}
	if len(diags) > 0 {
		return "", diags
	}
	if bin.Op.IsLogical() {
		// && and || are sequenced in C as in Tack, the right side running
		// only when the left does not decide.
		return fmt.Sprintf("(%s %s %s)", left, bin.Op, right), nil
	}
	operands := []ast.Expr{bin.X, bin.Y}
	build := func(ops []string) string {
		return g.binaryOf(bin, ops[0], ops[1])
	}
	if isIntComparison(bin) && needsOnce(ast.TypeOf(bin.X), ast.TypeOf(bin.Y)) {
		// The comparison reads one operand twice and may skip the other,
		// so each that is not pure is evaluated first, once, in order.
		return g.hoist(operands, []string{left, right}, g.operandTypes(operands), build), nil
	}
	return g.sequenced(operands, []string{left, right}, build), nil
}

// binaryOf renders bin, other than && and ||, applied to its operands
// rendered as left and right.
func (g *generator) binaryOf(bin *ast.Binary, left, right string) string {
	if bin.Op.IsInteger() {
		return arith(bin.Op, bin.Type, left, right)
	}
	if g.hasEqualityHelper(ast.TypeOf(bin.X)) {
		return g.arrayComparison(bin, left, right)
	}
	if ast.TypeOf(bin.X) == types.String {
		return stringComparison(bin.Op, left, right)
	}
	if isIntComparison(bin) {
		return intComparison(bin.Op, ast.TypeOf(bin.X), ast.TypeOf(bin.Y), left, right)
	}
	// Parenthesised so that Tack's grouping survives regardless of how C
	// would have parsed the same operators.
	return fmt.Sprintf("(%s %s %s)", left, bin.Op, right)
}

// arith renders the arithmetic l op r in the type typ. A float's is C's
// own. An integer type's is done so that it
// wraps on overflow, as Tack defines it, where C leaves signed overflow
// undefined and promotes narrow types to int, where it can overflow too.
// + - and * are done on uint64_t, which C defines to wrap, and the result
// converted to typ, which keeps its low bits: C leaves that conversion to
// the implementation for a signed typ, and clang keeps the low bits; the
// sanitizer tests in wrap_test.go check it. / and % go through a helper per
// type, which handles the smallest value divided by -1, the one overflow
// division has, and so do << and >>, which handle a count at or past the
// width (see emitShift). & | ^ and &^ are done on uint64_t too, which keeps
// every bit a signed value's two's complement form has. The wrapping
// operators +% -% and *% are + - and * done the same way, which is what
// they mean; plain + - and * are proven not to overflow, so wrapping them
// changes nothing. Each operand appears once, so neither is evaluated
// twice.
func arith(op ast.BinaryOp, typ types.Type, l, r string) string {
	op = op.Unwrapped()
	if types.IsFloat(typ) {
		// + - * and / on floats are IEEE 754's, which C defines for every
		// operand, a zero divisor included.
		return fmt.Sprintf("(%s %s %s)", l, op, r)
	}
	switch op {
	case ast.Div, ast.Rem, ast.Shl, ast.Shr:
		return fmt.Sprintf("%s(%s, %s)", divisionName(op, typ), l, r)
	case ast.AndNot:
		return fmt.Sprintf("((%s)((uint64_t)%s & ~(uint64_t)%s))", basicCType(typ), l, r)
	default:
		return fmt.Sprintf("((%s)((uint64_t)%s %s (uint64_t)%s))", basicCType(typ), l, op, r)
	}
}

// unaryExpr renders ! - or ^. A ! gets no parentheses of its own: C's ! binds
// tighter than any binary operator, as Tack's does, and its operand comes out
// already grouped, since every binary expression is parenthesised. A - is
// parenthesised, so that two in a row never read as C's decrement operator.
func (g *generator) unaryExpr(u *ast.Unary) (string, []diag.Diagnostic) {
	x, diags := g.expr(u.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	switch u.Op {
	case ast.Neg, ast.NegWrap:
		if types.IsFloat(u.Type) {
			// A float's negation flips its sign, -0 included.
			return "(-" + x + ")", nil
		}
		// -x is 0 - x, done on uint64_t so that negating the smallest value
		// wraps to itself instead of being undefined, as -%x means it to;
		// a plain -x is proven not to be that value.
		return fmt.Sprintf("((%s)(0 - (uint64_t)%s))", g.cType(u.Type), x), nil
	case ast.Complement:
		// ^x flips every bit, done on uint64_t, as & is, and converted back,
		// which keeps the low bits.
		return fmt.Sprintf("((%s)~(uint64_t)%s)", g.cType(u.Type), x), nil
	}
	return u.Op.String() + x, nil
}

// callValue renders a call used where a value is wanted, rejecting a callee
// that is neither a plain name nor a method and a builtin, which has no
// value to give. A union's construction, Expr.int(5), is a compound
// literal of the union (see constructionValue).
func (g *generator) callValue(call *ast.Call) (string, []diag.Diagnostic) {
	if _, _, _, built := ast.Construction(call); built {
		return g.constructionValue(call)
	}
	if call.Conv != types.Invalid {
		return g.conversionValue(call)
	}
	if call.Receiver() != nil {
		return g.callExpr("", call)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", call.Fn),
			Span:     call.Spanned(),
		}}
	}
	if fn, isFunc := id.Def.(*ast.Func); isFunc && fn == nil {
		if value := g.genericValue(id.Name); value != nil {
			return value(call)
		}
	}
	if _, isBuiltin := g.builtins.Lookup(id.Name); isBuiltin {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s returns no value", id.Name),
			Span:     call.Spanned(),
		}}
	}
	return g.callExpr(id.Name, call)
}

// conversionValue renders a conversion whose single value is used. One
// of a constant, to or from a float, is written as the value the checker
// computed. Any other the checker allows here cannot fail: an integer one
// cannot lose anything, and one to a float rounds, so a C cast does the
// whole job, but for a float64 to a float32, which goes through the
// helper that keeps the cast within float's range (see
// emitFloatNarrowing).
func (g *generator) conversionValue(call *ast.Call) (string, []diag.Diagnostic) {
	if call.Const != nil {
		v, _ := typedConstLiteral(call.Const, call.Conv)
		return v, nil
	}
	arg, diags := g.expr(call.Args[0], types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	return g.convert(ast.TypeOf(call.Args[0]), call.Conv, arg), nil
}

// convert renders the C conversion of v, of type from, to the type to,
// which the checker has found cannot fail or has tested: a cast, or the
// narrowing helper for a float64 to a float32.
func (g *generator) convert(from, to types.Type, v string) string {
	if from == types.Float64 && to == types.Float32 {
		return floatNarrowing + "(" + v + ")"
	}
	return fmt.Sprintf("((%s)%s)", g.cType(to), v)
}

// genericValue returns what renders a call to the generic builtin named
// name, len, make, append, reserve, pop, remove, replace, put or has, or nil for
// any other name.
func (g *generator) genericValue(name string) func(*ast.Call) (string, []diag.Diagnostic) {
	switch name {
	case "len":
		return g.lenValue
	case "make":
		return g.makeValue
	case "append":
		return g.appendValue
	case "reserve":
		return g.reserveValue
	case "pop":
		return g.popValue
	case "remove":
		return g.removeValue
	case "replace":
		return g.replaceValue
	default:
		return g.mapValue(name)
	}
}

// mapValue returns what renders a call to the generic builtin named name
// that works on a map, put or has, or nil for any other name. delete is
// rendered by deleteValues and dropDelete, since it gives two values or
// none.
func (g *generator) mapValue(name string) func(*ast.Call) (string, []diag.Diagnostic) {
	switch name {
	case "put":
		return g.putValue
	case "has":
		return g.hasValue
	default:
		return nil
	}
}

// stringValue is a string literal as a tack_string value.
func (g *generator) stringValue(s string) string {
	return "(tack_string){" + g.strParts(s) + "}"
}

// strParts is the members of s, a pooled string, as a tack_string: its
// array and that array's length, TACK_C_STR_LEN of it.
func (g *generator) strParts(s string) string {
	label := g.strLabel(s)
	return label + ", TACK_C_STR_LEN(" + label + ")"
}

// cType returns the C type a Tack type lowers to: a basic type's own, or for
// an array, a view, a list, a struct or a union the C struct emitTypes
// defines for it.
func (g *generator) cType(typ types.Type) string {
	typ = g.prog.Types.Bound(typ)
	if g.prog.Types.IsArray(typ) {
		return g.arrayName(typ)
	}
	if g.prog.Types.IsView(typ) {
		return g.viewName(typ)
	}
	if g.prog.Types.IsList(typ) {
		return g.listName(typ)
	}
	if g.prog.Types.IsMap(typ) {
		return g.mapName(typ)
	}
	if g.prog.Types.IsFunc(typ) {
		return funcPtr
	}
	if g.prog.Types.IsStruct(typ) || g.prog.Types.IsUnion(typ) {
		return g.structName(typ)
	}
	return basicCType(typ)
}

// arrayName is the name of the C struct an array type lowers to, made from
// its ID, so it starts tack_array_ where a mangled name starts with tack_
// and the package, and a temporary's has no second underscore.
func arrayNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_array_%d", typ)
}

// basicCType returns the C type a basic Tack type lowers to.
func basicCType(typ types.Type) string {
	if name, ok := cTypes[typ]; ok {
		return name
	}
	return "void"
}

// cTypes maps each Tack type a value can have to the C type it lowers to.
var cTypes = map[types.Type]string{
	types.Int8:   "int8_t",
	types.Int16:  "int16_t",
	types.Int32:  "int32_t",
	types.Int64:  "int64_t",
	types.Uint8:  "uint8_t",
	types.Uint16: "uint16_t",
	types.Uint32: "uint32_t",
	types.Uint64: "uint64_t",
	types.String: "tack_string",
	types.Bool:   "bool",
	types.Any:    "tack_any",
	types.Error:  "tack_error",

	types.Allocator: "tack_allocator",

	types.Float32: "float",
	types.Float64: "double",

	types.UnsafeSize:    "size_t",
	types.UnsafeUintptr: "uintptr_t",
}

// paramName keeps parameters clear of every other name the generated file
// defines. Tack has no reserved words, so a parameter may legally be called
// exit or int, each of which collides with something already emitted.
func paramName(name string) string { return "p_" + name }

// paramRef is how the parameter param is read and written: by its C name,
// or, for a mut parameter, through the pointer it is, (*p_x), which C's
// postfix operators can follow as they follow a name.
func (g *generator) paramRef(param *ast.Param) string {
	if param.Mut {
		return "(*" + paramName(param.Name) + ")"
	}
	return paramName(param.Name)
}

// argument renders e, a mut argument (see mutArg) or an own one, whose
// name the checker marked as moving its value, so that it renders as the
// move: the value taken, the variable emptied.
func (g *generator) argument(e ast.Expr) (string, []diag.Diagnostic) {
	if m, ok := e.(*ast.MutArg); ok {
		return g.mutArg(m)
	}
	x := ast.ArgOperand(e)
	return g.expr(x, ast.TypeOf(x))
}

// mutArg renders a mut argument as the address of the place it names: a
// mut parameter passed on is the pointer it already holds, and any other
// place, a variable, a field or an element, is &place. The checker has
// proven every index in the place in range.
func (g *generator) mutArg(m *ast.MutArg) (string, []diag.Diagnostic) {
	if id, ok := m.X.(*ast.Ident); ok {
		if param, isParam := id.Def.(*ast.Param); isParam && param.Mut {
			return paramName(param.Name), nil
		}
	}
	place, diags := g.targetRef(m.X)
	if len(diags) > 0 {
		return "", diags
	}
	return "&" + place, nil
}

// varRef is the C name of a variable: the mangled package-level name for a
// global, the prefixed local name otherwise.
func (g *generator) varRef(v *ast.Var) string {
	if g.globals[v] {
		return g.mangle(v, v.Name)
	}
	return varName(v.Name)
}

// varName keeps local variables clear of every other name the generated file
// defines, for the same reason paramName does. The distinct prefix keeps a
// variable and a parameter apart even if Tack ever let them share a name.
func varName(name string) string { return "v_" + name }

// declName returns the C name of the local v as its declaration writes
// it: with __attribute__((unused)) after it when the front end warned
// that v is never read (see ast.Var.Unused), so a strict C build does not
// make that warning an error. Every other local is declared as it was.
func declName(v *ast.Var) string {
	if v.Unused {
		return varName(v.Name) + " __attribute__((unused))"
	}
	return varName(v.Name)
}

// funcCName returns the C name of fn: a function's mangled name, and a
// method's mangled from its receiver type's name and its own joined by a
// period, Builder.write, as if that were one name, so
// tack_4_main13_Builder_2ewrite for write on Builder in package main. No
// function, type or global name holds a period, and mangle tells every
// two (path, name) pairs apart, so a method's C name is never a
// function's, a type's, a global's or another method's, even one of a
// package whose path ends with the type's name, calc/Builder's write.
func (g *generator) funcCName(fn *ast.Func) string {
	if fn.Generic() {
		args := make([]types.Type, len(fn.TypeParams))
		for i, tp := range fn.TypeParams {
			args[i] = tp.Type
		}
		return g.instanceCName(fn, args)
	}
	if fn.Method && fn.RecvType != nil {
		return g.mangle(fn, methodCName(fn.RecvType.Name, fn.Name))
	}
	return g.mangle(fn, fn.Name)
}

// methodCName returns the name a method, name on the type typeName, is
// mangled as: the two joined by a period, which no other name holds.
func methodCName(typeName, name string) string {
	return typeName + "." + name
}

// lowered returns call as code generation lowers it: a method call, x.m(args)
// or mut x.m(args), as a call of its method with the receiver as the first
// argument, m(x, args) or m(mut x, args), which is how the method's C
// function takes it, so every rule for a call's arguments, their order,
// the address of a mut one, the view and the copies an argument may need,
// holds for the receiver too. Any other call is returned as it is.
func lowered(call *ast.Call) *ast.Call {
	sel, ok := call.Fn.(*ast.Selector)
	if !ok || call.Method == nil {
		return call
	}
	return &ast.Call{
		Fn:           &ast.Ident{Name: call.Method.Name, Def: call.Method, Span: sel.NameSpan},
		Args:         call.Operands(),
		Span:         call.Span,
		EllipsisSpan: call.EllipsisSpan,
		Spread:       call.Spread,
	}
}

// mangle returns the C identifier of decl, a package-level declaration
// named name, in its package or, for nil, the main package: tack_ and the
// parts of its package's import path and its name, each written by
// mangledPart, so tack_4_calc5_lexer5_First for First in calc/lexer, and
// tack_4_main4_main for main in a program built from one file, whose path
// is main.
//
// No two (path, name) pairs give one C name. mangledPart's escaping can be
// read back, since a letter or digit stands for itself and an underscore
// always starts a three-byte escape. The whole name can then be read back
// part by part: after tack_, the run of digits up to the first underscore
// is a part's length, the bytes after that underscore are exactly that many
// escaped bytes, and the next part follows them. A part is consumed by its
// length, never by scanning, so digits or underscores inside it cannot
// mislead the reader. That recovers the path's parts and the name, and
// since path parts hold no slash, the path itself. A map with a left
// inverse is injective. No other name the generator makes collides with
// one either: after tack_, a mangled name has a digit, 1 to 9 since a path
// part is never empty, and every other name starting tack_ (tack_string,
// tack_builtin_print, tack_str0, tack_div_int8, tack_eq_3, tack_array_3, tack_results_...,
// tack_fnptr, tack_fn_3, tack_fnres_3, tack_value_..., a library's tack_init_, and temporaries
// such as tack_r0) has a letter; locals and parameters
// start v_ and p_.
func (g *generator) mangle(decl ast.Decl, name string) string {
	pkg := g.pkgOf[decl]
	if pkg == nil {
		pkg = g.prog.Main()
	}
	return "tack_" + mangledParts(pkg.Path, name)
}

// mangledParts writes each part of path, then name, as mangledPart does,
// with nothing between them.
func mangledParts(path, name string) string {
	var b strings.Builder
	for _, part := range strings.Split(path, "/") {
		b.WriteString(mangledPart(part))
	}
	b.WriteString(mangledPart(name))
	return b.String()
}

// mangledPart writes c as its length after escaping, an underscore, and c
// escaped: ASCII letters and digits kept, every other byte written as an
// underscore and its two lowercase hex digits. An escaped part can be read
// back exactly, since an underscore always starts a three-byte escape, and
// the length says where it ends.
func mangledPart(c string) string {
	var e strings.Builder
	for i := range len(c) {
		b := c[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' {
			e.WriteByte(b)
			continue
		}
		fmt.Fprintf(&e, "_%02x", b)
	}
	return strconv.Itoa(e.Len()) + "_" + e.String()
}

// seenStr reports whether the string pool already holds s.
func (g *generator) seenStr(s string) bool {
	for _, x := range g.strPool {
		if x.value == s {
			return true
		}
	}
	return false
}

// addStr adds s to the string pool, under the next label, tack_str0 then
// tack_str1 and on, unless the pool holds it already: each distinct
// string is pooled once, however often the program uses it.
func (g *generator) addStr(s string) {
	if g.seenStr(s) {
		return
	}
	g.strPool = append(g.strPool, str{fmt.Sprintf("tack_str%d", len(g.strPool)), s})
}

// strLabel returns the label of s in the string pool, or the empty string
// when s was never pooled. collectStrings pools every string a body can
// use before any is lowered, so an empty label is a string it missed, and
// the C written with it does not compile.
func (g *generator) strLabel(s string) string {
	for _, x := range g.strPool {
		if x.value == s {
			return x.label
		}
	}
	return ""
}

// toBytes renders s as C array initialiser bytes, with the trailing zero that
// emitConstantPool's declared length accounts for.
func (g *generator) toBytes(s string) string {
	parts := make([]string, len(s)+1)
	for i := 0; i < len(s); i++ {
		parts[i] = strconv.Itoa(int(s[i]))
	}
	parts[len(s)] = "0"
	return strings.Join(parts, ", ")
}

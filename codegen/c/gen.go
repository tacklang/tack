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

func init() { codegen.Register(New()) }

type backend struct{}

// New returns the C code generation backend.
func New() codegen.Backend { return backend{} }

func (backend) Name() string { return "c" }

func (backend) Ext() string { return ".c" }

// BuildCmd runs clang with its warnings off, compiling src and linking it
// with objs. A warning about generated C names a temporary file and code
// the user never wrote, and a valid program, such as one comparing a
// variable with itself, can cause one. Errors are still reported. The
// backend's own tests compile with -Wall -Wextra -Werror instead, so a
// warning the backend causes is caught there. opt becomes clang's -O
// flag. include, the standard library's include directory, is searched,
// with -I, for the <tack/tack.h> the generated source includes.
func (backend) BuildCmd(src, include string, objs []string, out string, opt codegen.Opt) []string {
	return append(
		[]string{"clang", "-w", "-O" + string(opt), "-I", include, "-o", out, src},
		objs...)
}

// CompileCmd runs clang as BuildCmd does, with its warnings off, to
// compile src, a C file of the program's, to the object obj. The header is
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
// opt, with its warnings off, as BuildCmd builds, and the flags extra
// before the output.
func compileCmd(src, obj string, opt codegen.Opt, extra ...string) []string {
	cmd := []string{"clang", "-w", "-O" + string(opt), "-c"}
	cmd = append(cmd, extra...)
	return append(cmd, "-o", obj, src)
}

// Generate lowers prog, which must have been type-checked with builtins,
// to one C translation unit.
func (backend) Generate(prog *ast.Program, builtins *builtin.Set) ([]byte, []diag.Diagnostic) {
	g := &generator{
		prog:     prog,
		builtins: builtins,
		decls:    prog.Decls(),
		globals:  make(map[*ast.Var]bool),
		pkgOf:    make(map[ast.Decl]*ast.Package),
		structs:  make(map[types.Type]*ast.TypeDecl),
		inPlace:  make(map[*ast.Var]bool),
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

	// scopes holds a deferScope for each block enclosing the statement
	// being generated, innermost last; see defer.go.
	scopes []*deferScope
	// frame is the function body being lowered; see defer.go.
	frame *deferFrame

	// spill is the expression being rendered, which keeps every
	// subexpression's C shallow, or nil between expressions; see
	// order.go.
	spill *spillScope

	buf    bytes.Buffer
	temps  int // how many temporaries have been named, for unique names
	labels int // how many blocks' labels have been named, likewise

	// deferring is whether a deferred body is being lowered; see defer.go.
	deferring bool
}

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
	builtins := g.builtins.Used(g.prog)

	var includes []string
	if g.usesType(types.UnsafeSize) || builtinsTake(builtins, types.UnsafeSize) {
		includes = append(includes, "#include <stddef.h>")
	}
	g.emitPreamble(includes)
	g.emitTypes()
	g.emitConstantPool(builtins)
	g.emitGlobals(globals)
	g.emitResultStructs()
	g.emitCNameMacro()
	g.emitPrototypes()

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
// std/builtin. Last comes std's <tack/tack.h>, which defines what every
// program shares: tack_string, and TACK_C_STR_LEN, which reads the length
// of a pooled string off its array, the array's size less the trailing
// zero emitConstantPool adds, rather than writing it out beside it.
func (g *generator) emitPreamble(includes []string) {
	g.buf.WriteString("// Generated by the Tack compiler. Do not edit.\n\n")
	g.buf.WriteString("#include <stdbool.h>\n")
	g.buf.WriteString("#include <stdint.h>\n")
	for _, inc := range includes {
		g.buf.WriteString(inc + "\n")
	}
	g.buf.WriteString(stdHeader + "\n")
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
// is never written out, since every write is length-bounded.
func (g *generator) emitConstantPool(builtins []builtin.Func) {
	for _, s := range g.strPool {
		fmt.Fprintf(&g.buf, "static const unsigned char %s[%d] = {%s};\n",
			s.label, len(s.value)+1, g.toBytes(s.value))
	}

	g.buf.WriteString("\n")

	// Each builtin is declared only when the program calls it, under its C
	// name; its body is in std/builtin, which the driver links only for a
	// builtin some call needs.
	for _, fn := range builtins {
		g.buf.WriteString(g.builtinPrototype(fn) + "\n")
	}
	if len(builtins) > 0 {
		g.buf.WriteString("\n")
	}

	// Division and remainder by -1 are written out, since the smallest int
	// divided by -1 overflows and C leaves that undefined: the quotient wraps
	// to the smallest int, and the remainder is 0. Each helper is emitted only
	// when the program uses its operator.
	for _, use := range g.divisions() {
		g.emitDivision(use.op, use.typ)
	}

	// Each slice helper is emitted only when the program slices its type,
	// with or without a high bound.
	for _, h := range g.sliceHelpers() {
		g.emitSliceHelper(h)
	}
}

// division is one operator, / or %, applied in one integer type.
type division struct {
	op  ast.BinaryOp
	typ types.Type
}

// divisions lists each division and remainder the program does, by
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
	for _, op := range []ast.BinaryOp{ast.Div, ast.Rem} {
		for typ := types.Type(0); typ < types.UntypedInt; typ++ {
			if used[division{op, typ}] {
				out = append(out, division{op, typ})
			}
		}
	}
	return out
}

// divisionIn returns the division or remainder node n does at runtime, if
// it does one: an operator expression that is not a constant, or a compound
// assignment, which works in its target's type.
func divisionIn(n ast.Node) (division, bool) {
	isDivision := func(op ast.BinaryOp) bool { return op == ast.Div || op == ast.Rem }
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
// leaves that undefined: the quotient wraps to the smallest value, and the
// remainder is 0. An unsigned division cannot overflow, but goes through a
// helper too, so every division looks the same.
func (g *generator) emitDivision(op ast.BinaryOp, typ types.Type) {
	t := g.cType(typ)
	fmt.Fprintf(&g.buf, "static %s %s(%s a, %s b) {\n", t, divisionName(op, typ), t, t)
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

// divisionName is the name of the helper for op in typ, such as
// tack_div_int8.
func divisionName(op ast.BinaryOp, typ types.Type) string {
	name := "div"
	if op == ast.Rem {
		name = "rem"
	}
	return "tack_" + name + "_" + types.NewTable().Name(typ)
}

// generateDecls generates every top-level declaration: each global as one
// line of C, and each function's body, returned alongside the functions'
// signatures in source order. An extern func's body is in C, and its
// prototype declares it; a generic function is never generated itself,
// but once for each of its instances (see generateInstances).
func (g *generator) generateDecls() (
	globals []string,
	sigs []string,
	bodies [][]string,
	diags []diag.Diagnostic,
) {
	for _, decl := range g.decls {
		switch d := decl.(type) {
		case *ast.Var:
			global, diags := g.generateGlobal(d)
			if len(diags) > 0 {
				return nil, nil, nil, diags
			}
			globals = append(globals, global)

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

// emitGlobals writes the package-level variable declarations.
func (g *generator) emitGlobals(globals []string) {
	if len(globals) == 0 {
		return
	}
	for _, global := range globals {
		g.buf.WriteString(global + "\n")
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
			g.buf.WriteString(g.signature(fn) + ";\n")
		}
	}
	for _, inst := range g.prog.Instances {
		g.withInstance(inst, func() { g.buf.WriteString(g.signature(inst.Func) + ";\n") })
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
// it has given each global too deep for a C initializer its value (see
// deepGlobal).
func (g *generator) emitEntryPoint() {
	g.buf.WriteString("int main(void) {\n")
	for _, init := range g.globalInits {
		g.buf.WriteString("    " + init + "\n")
	}
	fmt.Fprintf(&g.buf, "    %s();\n", g.mangle(nil, "main"))
	g.buf.WriteString("    return 0;\n")
	g.buf.WriteString("}\n")
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
// frame; see funcFrame.
func (g *generator) generateFuncBody(fn *ast.Func) ([]string, []diag.Diagnostic) {
	return g.funcFrame(fn)
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
// it reaches and the owned locals it declares, which falling off its end
// runs and its exits jump into.
func (g *generator) block(body []ast.Stmt, fn *ast.Func, loop bool) ([]string, []diag.Diagnostic) {
	sc := &deferScope{loop: loop}
	g.scopes = append(g.scopes, sc)
	defer func() { g.scopes = g.scopes[:len(g.scopes)-1] }()

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
// which is written as its value, or a division or remainder, which calls a
// helper.
func parenthesised(e ast.Expr) bool {
	b, ok := e.(*ast.Binary)
	return ok && b.Const == nil && b.Op != ast.Div && b.Op != ast.Rem
}

// condHeader writes the opening line of an if or while whose condition is
// cond, rendered from e. A binary expression is already parenthesised, and a
// second pair makes clang warn about an equality test, so it only gets the
// statement's own.
func condHeader(keyword string, e ast.Expr, cond string) string {
	if parenthesised(e) {
		return fmt.Sprintf("%s %s {", keyword, cond)
	}
	return fmt.Sprintf("%s (%s) {", keyword, cond)
}

// generateFor lowers a loop to a C while, or to for (;;) when it has no
// condition. Tack's break and continue mean what C's do, so they lower
// directly. A loop with an init or post statement takes generateClauseFor's
// form instead.
func (g *generator) generateFor(loop *ast.For, fn *ast.Func) ([]string, []diag.Diagnostic) {
	if loop.Init != nil || loop.Post != nil {
		return g.generateClauseFor(loop, fn)
	}

	header := "for (;;) {"
	if loop.Cond != nil {
		cond, diags := g.expr(loop.Cond, types.Bool)
		if len(diags) > 0 {
			return nil, diags
		}
		header = condHeader("while", loop.Cond, cond)
	}

	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}

	lines := append([]string{header}, indent(body)...)
	return append(lines, "}"), nil
}

// generateClauseFor lowers a three-clause loop to a C block holding the init
// statement and a C for whose post expression is the post statement:
//
//	{
//	    init;
//	    for (; cond; post) {
//	        body
//	    }
//	}
//
// The block scopes a variable the init declares to the loop, as Tack does,
// and ends in the chain that frees one that owns a list.
// Keeping the post in the for, rather than at the end of the body, is what
// makes continue run it. The post's lines are joined with the comma
// operator, so the post stays one C expression even if its Tack statement
// lowers to more than one C statement.
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

	cond := ""
	if loop.Cond != nil {
		c, diags := g.expr(loop.Cond, types.Bool)
		if len(diags) > 0 {
			return nil, nil, diags
		}
		cond = stripParens(loop.Cond, c)
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

	inner := append([]string{fmt.Sprintf("for (; %s; %s) {", cond, post)}, indent(body)...)
	return init, append(inner, "}"), nil
}

// generateForRange lowers a range loop to a C block that evaluates the
// ranged expression once, before a counting for, and declares the loop's
// variables at the top of each iteration. Over an integer, the count is
// held in a temporary:
//
//	{
//	    const int64_t tack_n0 = count;
//	    for (int64_t tack_k1 = 0; tack_k1 < tack_n0; tack_k1++) {
//	        const int64_t v_i = tack_k1;
//	        body
//	    }
//	}
//
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
	if g.bindsInPlace(loop) {
		g.inPlace[loop.Elem] = true
	}
	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	xType := ast.TypeOf(loop.X)
	if !types.IsInteger(xType) {
		return g.collectionRange(loop, x, addr, body), nil
	}

	typ := g.cType(xType)
	n, k := g.temp("n"), g.temp("k")
	if loop.Var != nil {
		body = append(
			[]string{fmt.Sprintf("const %s %s = %s;", typ, varName(loop.Var.Name), k)},
			body...)
	}
	return rangeBlock(fmt.Sprintf("const %s %s = %s;", typ, n, x),
		fmt.Sprintf("for (%s %s = 0; %s < %s; %s++) {", typ, k, k, n, k), body), nil
}

// collectionRange lowers a range loop over an array or a string, whose
// lowered value is x, its address addr, and lowered body body. The value is copied into a
// const temporary, which every iteration reads, so the body assigning the
// array, or the variable holding it, changes nothing the loop sees:
//
//	{
//	    const tack_array_3 tack_r0 = a;
//	    for (int64_t tack_k1 = 0; tack_k1 < 4; tack_k1++) {
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
		vars = append(vars, fmt.Sprintf("const int64_t %s = %s;", varName(loop.Var.Name), k))
	}
	switch {
	case g.bindsInPlace(loop):
		vars = append(vars, fmt.Sprintf("const %s *const %s = &%s.%s[%s];",
			g.cType(loop.Elem.Type), varName(loop.Elem.Name), read, field, k))
	case bindsElem(loop):
		vars = append(vars, fmt.Sprintf("const %s %s = %s.%s[%s];",
			g.cType(loop.Elem.Type), varName(loop.Elem.Name), read, field, k))
	}

	if field == "e" && !bindsElem(loop) {
		eval = fmt.Sprintf("(void)%s;", x)
	}
	return rangeBlock(eval,
		fmt.Sprintf("for (int64_t %s = 0; %s < %s; %s++) {", k, k, n, k), append(vars, body...))
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
// evaluates what the loop ranges over, then a for with the header header
// around body.
func rangeBlock(eval, header string, body []string) []string {
	inner := append([]string{header}, indent(body)...)
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
// for a condition in a C for header, where a second pair would make clang
// warn on an equality test.
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
func (g *generator) generateAssign(a *ast.Assign) ([]string, []diag.Diagnostic) {
	value, diags := g.expr(a.Value, ast.TypeOf(a.Value))
	if len(diags) > 0 {
		return nil, diags
	}
	name, diags := g.targetRef(a.Target)
	if len(diags) > 0 {
		return nil, diags
	}
	if typ := ast.TypeOf(a.Target); a.Op == ast.InvalidOp && g.owned(typ) {
		return g.ownedAssign(typ, name, value), nil
	}
	if a.Op != ast.InvalidOp {
		// x op= v is x = x op v, spelled out the way Tack defines it, x
		// read before v is evaluated.
		operands := []ast.Expr{a.Target, a.Value}
		value = g.sequenced(operands, []string{name, value}, func(ops []string) string {
			return arith(a.Op, ast.TypeOf(a.Target), ops[0], ops[1])
		})
	}
	return []string{fmt.Sprintf("%s = %s;", name, value)}, nil
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
// a C lvalue: x.e[i] for an array, x.ptr[i] for a list.
func (g *generator) elementRef(ix *ast.Index) (string, []diag.Diagnostic) {
	base, diags := g.targetRef(ix.X)
	if len(diags) > 0 {
		return "", diags
	}
	index, diags := g.expr(ix.Index, types.Invalid)
	if len(diags) > 0 {
		return "", diags
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

// generateExprStmt lowers an expression statement, which can only be a call
// to a named function or a method today, pop and remove among them, which
// drop what they take (see dropTake).
func (g *generator) generateExprStmt(stmt *ast.ExprStmt) ([]string, []diag.Diagnostic) {
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

	decl := fmt.Sprintf("%s %s = %s;", g.cType(v.Type), varName(v.Name), value)
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
		decl := fmt.Sprintf("%s %s = %s;", g.cType(v.Type), varName(v.Name), src.field(names, i))
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
		arg, diags := g.expr(c.Args[0], types.Invalid)
		from, to := ast.TypeOf(c.Args[0]), c.Conv
		return multiSource{
			temps: []multiTemp{{typ: g.cType(from), init: arg}},
			field: func(tmps []string, i int) string {
				fits := fitsExpr(from, to, tmps[0])
				if i == 1 {
					return fits
				}
				return fmt.Sprintf("(%s ? (%s)%s : 0)", fits, g.cType(to), tmps[0])
			},
		}, diags
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
	fn := ast.Callee(c)
	if fn == nil {
		return multiSource{}, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a call of several results must call a declared function",
			Span:     c.Fn.Spanned(),
		}}
	}
	call, diags := g.callExpr(fn.Name, c)
	_, cname := g.target(fn, c.TypeArgs)
	return multiSource{
		temps: []multiTemp{{typ: resultsOf(cname), init: call}},
		field: func(tmps []string, i int) string { return fmt.Sprintf("%s.r%d", tmps[0], i) },
	}, diags
}

// divisionValues lowers q, ok := a / b (or a % b). Both operands are
// evaluated, the dividend first, whatever the divisor turns out to be, so a
// call in either always runs.
func (g *generator) divisionValues(bin *ast.Binary) (multiSource, []diag.Diagnostic) {
	if bin.Const != nil {
		return g.constantDivisionValues(bin)
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
			if i == 1 {
				return fmt.Sprintf("(%s != 0)", tmps[1])
			}
			return fmt.Sprintf(
				"(%s != 0 ? %s : 0)",
				tmps[1],
				arith(bin.Op, bin.Type, tmps[0], tmps[1]),
			)
		},
	}, nil
}

// constantDivisionValues lowers a checked division of two constants, whose
// value the checker has computed, its divisor not being zero: the value,
// written as a constant with no temporary, since evaluating it does
// nothing, and true.
func (g *generator) constantDivisionValues(bin *ast.Binary) (multiSource, []diag.Diagnostic) {
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

// fitsExpr renders a C test of whether v, of the integer type from, is also
// a value of the integer type to.
func fitsExpr(from, to types.Type, v string) string {
	if types.TargetWidth(from) || types.TargetWidth(to) {
		return targetFitsExpr(from, to, v)
	}
	lo, hi := bounds(to)
	return withinExpr(from, v, lo, hi)
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
// an Allocator or a view, which have none in Tack, the zeroed value the
// ret variable of a function returning one starts as, which it never
// returns. The empty string
// points at a pooled empty array rather than NULL, so writing it never hands
// libc a null pointer. An array or a struct is zeroed in full, so a string
// in it starts with a null pointer and a length of 0, which print's C body
// never passes on. An any's zero value, which v, ok := args[i] gives out
// of range, is a zeroed tack_any: type id 0, TACK_TYPE_INVALID, which
// holds nothing, and a null pointer, never read.
func (g *generator) zeroValue(typ types.Type) string {
	if g.isComposite(typ) || g.prog.Types.IsList(typ) || g.prog.Types.IsView(typ) ||
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
	typeArgs := call.TypeArgs
	call = lowered(call)
	callee := g.mangle(nil, name)
	var fn *ast.Func
	if f := ast.Callee(call); f != nil {
		fn, callee = g.target(f, typeArgs)
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

// expr renders an expression as C. want is the type the context requires;
// passing types.Invalid means any type the expression happens to have.
// An expression a context renders on its own keeps its C shallow by
// holding deep parts in temporaries, assigned first (see exprSteps); one
// rendered as part of another leaves that to the outer one (see spilled).
func (g *generator) expr(e ast.Expr, want types.Type) (string, []diag.Diagnostic) {
	if got := ast.TypeOf(e); !types.Widens(got, want) && want != types.Invalid {
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
	case *ast.MutArg:
		return g.mutArg(x)
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
		return constLiteral(x.Const)
	case *ast.Unary:
		return constLiteral(x.Const)
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
		return intLiteral(x.Value), true
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
// moves, as the move (see moveValue).
func (g *generator) identExpr(id *ast.Ident) (string, []diag.Diagnostic) {
	switch def := id.Def.(type) {
	case *ast.Param:
		return g.paramRef(def), nil
	case *ast.Var:
		if id.Move {
			return g.moveValue(def.Type, g.varRef(def)), nil
		}
		if g.inPlace[def] {
			return "(*" + varName(def.Name) + ")", nil
		}
		return g.varRef(def), nil
	default:
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unbound identifier: %s", id.Name),
			Span:     id.Span,
		}}
	}
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
	if bin.Op.IsArithmetic() {
		return arith(bin.Op, bin.Type, left, right)
	}
	if g.isComposite(ast.TypeOf(bin.X)) {
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

// arith renders the arithmetic l op r in the integer type typ so that it
// wraps on overflow, as Tack defines it, where C leaves signed overflow
// undefined and promotes narrow types to int, where it can overflow too.
// + - and * are done on uint64_t, which C defines to wrap, and the result
// converted to typ, which keeps its low bits: C leaves that conversion to
// the implementation for a signed typ, and clang keeps the low bits; the
// sanitizer tests in wrap_test.go check it. / and % go through a helper per
// type, which handles the smallest value divided by -1, the one overflow
// division has. Each operand appears once, so neither is evaluated twice.
func arith(op ast.BinaryOp, typ types.Type, l, r string) string {
	switch op {
	case ast.Div, ast.Rem:
		return fmt.Sprintf("%s(%s, %s)", divisionName(op, typ), l, r)
	default:
		return fmt.Sprintf("((%s)((uint64_t)%s %s (uint64_t)%s))", basicCType(typ), l, op, r)
	}
}

// unaryExpr renders ! or -. A ! gets no parentheses of its own: C's ! binds
// tighter than any binary operator, as Tack's does, and its operand comes out
// already grouped, since every binary expression is parenthesised. A - is
// parenthesised, so that two in a row never read as C's decrement operator.
func (g *generator) unaryExpr(u *ast.Unary) (string, []diag.Diagnostic) {
	x, diags := g.expr(u.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	if u.Op == ast.Neg {
		// -x is 0 - x, done on uint64_t so that negating the smallest value
		// wraps to itself instead of being undefined.
		return fmt.Sprintf("((%s)(0 - (uint64_t)%s))", g.cType(u.Type), x), nil
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
		// The checker allows a single-value conversion only where it
		// cannot lose anything, so a C cast does the whole job.
		arg, diags := g.expr(call.Args[0], types.Invalid)
		if len(diags) > 0 {
			return "", diags
		}
		return fmt.Sprintf("((%s)%s)", g.cType(call.Conv), arg), nil
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

// genericValue returns what renders a call to the generic builtin named
// name, len, make, append, reserve, pop or remove, or nil for any other
// name.
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
		return arrayName(typ)
	}
	if g.prog.Types.IsView(typ) {
		return viewName(typ)
	}
	if g.prog.Types.IsList(typ) {
		return listName(typ)
	}
	if g.prog.Types.IsStruct(typ) || g.prog.Types.IsUnion(typ) {
		return g.structName(typ)
	}
	return basicCType(typ)
}

// arrayName is the name of the C struct an array type lowers to, made from
// its ID, so it starts tack_array_ where a mangled name starts with tack_
// and the package, and a temporary's has no second underscore.
func arrayName(typ types.Type) string {
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
// and temporaries such as tack_r0) has a letter; locals and parameters
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

func (g *generator) seenStr(s string) bool {
	for _, x := range g.strPool {
		if x.value == s {
			return true
		}
	}
	return false
}

func (g *generator) addStr(s string) {
	if g.seenStr(s) {
		return
	}
	g.strPool = append(g.strPool, str{fmt.Sprintf("tack_str%d", len(g.strPool)), s})
}

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

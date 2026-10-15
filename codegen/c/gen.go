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
	}
	for _, pkg := range prog.Packages {
		for _, decl := range pkg.Decls() {
			g.pkgOf[decl] = pkg
			if td, ok := decl.(*ast.TypeDecl); ok {
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
	structs  map[types.Type]*ast.TypeDecl // the declaration of each struct type
	strPool  []str
	globals  map[*ast.Var]bool // the package-level variables

	// scopes holds a deferScope for each block enclosing the statement
	// being generated, innermost last; see defer.go.
	scopes []*deferScope
	// frame is the function body being lowered; see defer.go.
	frame *deferFrame

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
	globals, funcs, bodies, diags := g.generateDecls()
	if len(diags) > 0 {
		return nil, diags
	}
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

	for i, fn := range funcs {
		g.emitFunc(fn, bodies[i])
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
// of range.
func (g *generator) poolCheckedZero(value ast.Expr) {
	if ix, ok := value.(*ast.Index); ok && ix.Type == types.String {
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
// string, or one inside an array's elements or a struct's fields.
func (g *generator) poolConst(c any) {
	switch v := c.(type) {
	case string:
		g.addStr(v)
	case []any:
		for _, x := range v {
			g.poolConst(x)
		}
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
// variables of a MultiVar, and of a range loop, have no value of their own
// but never start from zero, so only the call, or the ranged expression and
// the body, are visited.
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
		}
		return true
	}
	g.walk(visit)
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
// line of C, and each function's body, returned alongside the functions in
// source order.
func (g *generator) generateDecls() (
	globals []string,
	funcs []*ast.Func,
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
			if d.Extern {
				// Its body is in C; its prototype declares it.
				continue
			}
			body, diags := g.generateFuncBody(d)
			if len(diags) > 0 {
				return nil, nil, nil, diags
			}
			funcs = append(funcs, d)
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
	return globals, funcs, bodies, nil
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
	if v.Const == nil && v.Value == nil && g.isComposite(v.Type) {
		// An array or a struct with no value starts with every element or
		// field zero.
		value, ok = "{0}", true
	}
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("package-level variable %s has no computed value", v.Name),
			Span:     v.NameSpan,
		}}
	}

	qualifier := "static const"
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
func (g *generator) emitResultStructs() {
	for _, decl := range g.decls {
		fn, ok := decl.(*ast.Func)
		if !ok || len(fn.ResultTypes) < 2 {
			continue
		}

		g.buf.WriteString("typedef struct {\n")
		for i, typ := range fn.ResultTypes {
			fmt.Fprintf(&g.buf, "    %s r%d;\n", g.cType(typ), i)
		}
		fmt.Fprintf(&g.buf, "} %s;\n\n", g.resultsName(fn))
	}
}

// resultsName is the name of the struct fn, a function with several
// results, returns them in: tack_results_ and what follows tack_ in fn's
// own name, as mangle gives it. It starts tack_results_ where a mangled
// name starts with tack_ and a digit, so it collides with none, and two are
// told apart as mangled names are.
func (g *generator) resultsName(fn *ast.Func) string {
	return "tack_results_" + strings.TrimPrefix(g.mangle(fn, fn.Name), "tack_")
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
		case !ok:
		case fn.Extern:
			fmt.Fprintf(&g.buf, "extern %s TACK_C_NAME(%q);\n",
				strings.TrimPrefix(g.signature(fn), "static "), fn.CName)
		default:
			g.buf.WriteString(g.signature(fn) + ";\n")
		}
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

func (g *generator) emitFunc(fn *ast.Func, body []string) {
	g.buf.WriteString(g.signature(fn) + " {\n")
	if len(body) > 0 {
		fmt.Fprintf(&g.buf, "    %s\n", strings.Join(body, "\n    "))
	}
	g.buf.WriteString("}\n\n")
}

// emitEntryPoint writes C's main, which exists only to call Tack's.
func (g *generator) emitEntryPoint() {
	g.buf.WriteString("int main(void) {\n")
	fmt.Fprintf(&g.buf, "    %s();\n", g.mangle(nil, "main"))
	g.buf.WriteString("    return 0;\n")
	g.buf.WriteString("}\n")
}

func (g *generator) signature(fn *ast.Func) string {
	result := g.resultType(fn)

	if len(fn.Params) == 0 {
		return fmt.Sprintf("static %s %s(void)", result, g.mangle(fn, fn.Name))
	}

	params := make([]string, len(fn.Params))
	for i, param := range fn.Params {
		params[i] = fmt.Sprintf("%s %s", g.paramType(param), paramName(param.Name))
	}
	return fmt.Sprintf(
		"static %s %s(%s)", result, g.mangle(fn, fn.Name), strings.Join(params, ", "),
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
// it reaches, which falling off its end runs and its exits jump into.
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
	if len(sc.defers) == 0 {
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
// "" for a bare return.
func (g *generator) returnValue(ret *ast.Return, fn *ast.Func) (string, []diag.Diagnostic) {
	if len(ret.Values) == 0 {
		return "", nil
	}

	values := make([]string, len(ret.Values))
	for i, v := range ret.Values {
		value, diags := g.expr(v, fn.ResultTypes[i])
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
// The block scopes a variable the init declares to the loop, as Tack does.
// Keeping the post in the for, rather than at the end of the body, is what
// makes continue run it. The post's lines are joined with the comma
// operator, so the post stays one C expression even if its Tack statement
// lowers to more than one C statement.
func (g *generator) generateClauseFor(loop *ast.For, fn *ast.Func) ([]string, []diag.Diagnostic) {
	var init []string
	if loop.Init != nil {
		lines, diags := g.generateSimpleStmt(loop.Init)
		if len(diags) > 0 {
			return nil, diags
		}
		init = lines
	}

	cond := ""
	if loop.Cond != nil {
		c, diags := g.expr(loop.Cond, types.Bool)
		if len(diags) > 0 {
			return nil, diags
		}
		cond = stripParens(loop.Cond, c)
	}

	post := ""
	if m, ok := loop.Post.(*ast.MultiAssign); ok {
		decls, p, diags := g.multiAssignPost(m)
		if len(diags) > 0 {
			return nil, diags
		}
		init = append(init, decls...)
		post = p
	} else if loop.Post != nil {
		p, diags := g.postExpr(loop.Post)
		if len(diags) > 0 {
			return nil, diags
		}
		post = p
	}

	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}

	inner := append([]string{fmt.Sprintf("for (; %s; %s) {", cond, post)}, indent(body)...)
	inner = append(inner, "}")
	lines := append([]string{"{"}, indent(append(init, inner...))...)
	return append(lines, "}"), nil
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
	x, diags := g.expr(loop.X, types.Invalid)
	if len(diags) > 0 {
		return nil, diags
	}
	body, diags := g.generateLoopBody(loop.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	xType := ast.TypeOf(loop.X)
	if !types.IsInteger(xType) {
		return g.collectionRange(loop, x, body), nil
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
// lowered value is x and lowered body body. The value is copied into a
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
// tack_r0.len, and its bytes as tack_r0.ptr[k]. When the array cannot
// change while the loop runs (see rangesInPlace), x is a place, and the
// temporary is a pointer to it instead, const tack_array_3 *const tack_r0
// = &a, through which each element is read: the same values a copy gives,
// without making one. Over an array with
// no element variable nothing reads the copy, so the value is only
// evaluated, cast to void. A _ index gets no C variable.
func (g *generator) collectionRange(loop *ast.ForRange, x string, body []string) []string {
	xType := ast.TypeOf(loop.X)
	r, k := g.temp("r"), g.temp("k")
	n, field := r+".len", "ptr"
	if g.prog.Types.IsArray(xType) {
		n, field = strconv.FormatInt(g.prog.Types.Len(xType), 10), "e"
	}
	eval := fmt.Sprintf("const %s %s = %s;", g.cType(xType), r, x)
	read := r
	if g.rangesInPlace(loop) {
		eval = fmt.Sprintf("const %s *const %s = &%s;", g.cType(xType), r, x)
		read = "(*" + r + ")"
	}

	var vars []string
	if loop.Var != nil && loop.Var.Name != ast.Blank {
		vars = append(vars, fmt.Sprintf("const int64_t %s = %s;", varName(loop.Var.Name), k))
	}
	if loop.Elem != nil && loop.Elem.Name != ast.Blank {
		vars = append(vars, fmt.Sprintf("const %s %s = %s.%s[%s];",
			g.cType(loop.Elem.Type), varName(loop.Elem.Name), read, field, k))
	}

	if field == "e" && !bindsElem(loop) {
		eval = fmt.Sprintf("(void)%s;", x)
	}
	return rangeBlock(eval,
		fmt.Sprintf("for (int64_t %s = 0; %s < %s; %s++) {", k, k, n, k), append(vars, body...))
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

func (g *generator) generateIf(stmt *ast.If, fn *ast.Func) ([]string, []diag.Diagnostic) {
	cond, diags := g.expr(stmt.Cond, types.Bool)
	if len(diags) > 0 {
		return nil, diags
	}

	then, diags := g.generateBlock(stmt.Then, fn)
	if len(diags) > 0 {
		return nil, diags
	}

	lines := append([]string{condHeader("if", stmt.Cond, cond)}, indent(then)...)

	if stmt.Else == nil {
		return append(lines, "}"), nil
	}

	els, diags := g.generateBlock(stmt.Else, fn)
	if len(diags) > 0 {
		return nil, diags
	}

	lines = append(lines, "} else {")
	lines = append(lines, indent(els)...)
	return append(lines, "}"), nil
}

// generateAssign sets a local to a new value.
func (g *generator) generateAssign(a *ast.Assign) ([]string, []diag.Diagnostic) {
	value, diags := g.expr(a.Value, ast.TypeOf(a.Value))
	if len(diags) > 0 {
		return nil, diags
	}
	name, diags := g.targetRef(a.Target)
	if len(diags) > 0 {
		return nil, diags
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
// element of one, x.e[i], or a field of one, x.f_name. The checker has
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
		base, diags := g.targetRef(t.X)
		if len(diags) > 0 {
			return "", diags
		}
		index, diags := g.expr(t.Index, types.Invalid)
		if len(diags) > 0 {
			return "", diags
		}
		return fmt.Sprintf("%s.e[%s]", base, index), nil
	case *ast.Selector:
		base, diags := g.targetRef(t.X)
		if len(diags) > 0 {
			return "", diags
		}
		return base + "." + fieldName(t.Name), nil
	}
	return "", []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  "cannot assign to this expression",
		Span:     target.Spanned(),
	}}
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
// to a named function today.
func (g *generator) generateExprStmt(stmt *ast.ExprStmt) ([]string, []diag.Diagnostic) {
	call, ok := stmt.X.(*ast.Call)
	if !ok {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", stmt.X),
			Span:     stmt.Spanned(),
		}}
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
// local, so the C compiler holds it to the same rule. A variable without a
// value starts from its type's zero value.
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
	if !v.Mutable {
		decl = "const " + decl
	}
	return []string{decl}, nil
}

// generateMultiVar declares the variables of a MultiVar. Its source is
// evaluated once into const temporaries, and each variable is then declared
// from the value in its position: const unless the variable is mutable, as
// generateVar does. A _ gets no variable.
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
		if !v.Mutable {
			decl = "const " + decl
		}
		lines = append(lines, decl)
	}
	return lines, nil
}

// holdTemps names a const temporary for each of src's temps and declares
// it, initialized, returning the names and the declarations.
func (g *generator) holdTemps(src multiSource) (names, lines []string) {
	for _, t := range src.temps {
		name := g.temp("r")
		names = append(names, name)
		lines = append(lines, fmt.Sprintf("%s = %s;", t.decl("const ", name), t.init))
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
		names = append(names, name)
		decls = append(decls, t.decl("", name)+";")
		exprs = append(exprs, fmt.Sprintf("%s = %s", name, t.init))
	}
	assigns, diags := g.fieldAssigns(m, src, names)
	if len(diags) > 0 {
		return nil, "", diags
	}
	return decls, strings.Join(append(exprs, assigns...), ", "), nil
}

// fieldAssigns renders, for each target of m other than _, the C assignment
// of the value in its position, read from src's temporaries, named tmps,
// without a semicolon.
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
// through.
type multiTemp struct {
	typ, init string
	ref       bool
}

// decl declares the temporary name, without a semicolon or an initializer:
// as a typ, qualified by qual, which is "const " or empty, or, when it is
// a ref, as a pointer to const typ, which the pointer itself is when qual
// is "const ".
func (t multiTemp) decl(qual, name string) string {
	if t.ref {
		return fmt.Sprintf("const %s *%s%s", t.typ, qual, name)
	}
	return fmt.Sprintf("%s%s %s", qual, t.typ, name)
}

// multiValues lowers the source of a MultiVar or MultiAssign: a call to a
// function with several results, held as its results struct and read field
// by field; a conversion that can fail, held as the value converted and
// read as the converted value, or 0 when it does not fit, and whether it
// fit; or a division, held as its two operands and read as
// the result, or 0 when the divisor is zero, and whether it was not.
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
// read field by field.
func (g *generator) callValues(c *ast.Call) (multiSource, []diag.Diagnostic) {
	id, _ := c.Fn.(*ast.Ident)
	var fn *ast.Func
	if id != nil {
		fn, _ = id.Def.(*ast.Func)
	}
	if fn == nil {
		return multiSource{}, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a call of several results must call a declared function",
			Span:     c.Fn.Spanned(),
		}}
	}
	call, diags := g.callExpr(id.Name, c)
	return multiSource{
		temps: []multiTemp{{typ: g.resultsName(fn), init: call}},
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

// zeroValue is the C expression for a type's zero value. The empty string
// points at a pooled empty array rather than NULL, so writing it never hands
// libc a null pointer. An array or a struct is zeroed in full, so a string
// in it starts with a null pointer and a length of 0, which print's C body
// never passes on.
func (g *generator) zeroValue(typ types.Type) string {
	if g.isComposite(typ) {
		return fmt.Sprintf("(%s){0}", g.cType(typ))
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

// indent shifts a nested block one level right. Function bodies are emitted at
// one level already, so this is what keeps branches readable.
func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = "    " + line
	}
	return out
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
// argument to a view parameter as a view (see viewArg).
func (g *generator) callExpr(name string, call *ast.Call) (string, []diag.Diagnostic) {
	callee := g.mangle(nil, name)
	var fn *ast.Func
	if id, ok := call.Fn.(*ast.Ident); ok {
		if f, ok := id.Def.(*ast.Func); ok && f != nil {
			fn, callee = f, g.mangle(f, f.Name)
		}
	}
	args, diags := g.callArgs(call, fn)
	if len(diags) > 0 {
		return "", diags
	}
	var params []*ast.Param
	if fn != nil {
		params = fn.Params
	}
	return g.sequencedAs(call.Args, args, g.argTypes(call, params), func(ops []string) string {
		return fmt.Sprintf("%s(%s)", callee, strings.Join(ops, ", "))
	}), nil
}

// expr renders an expression as C. want is the type the context requires;
// passing types.Invalid means any type the expression happens to have.
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
	return g.exprNode(e)
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

// plainLiteral renders an int, character, string or bool literal as C, and
// reports whether e was one. A negative int is parenthesised, so a minus
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
	default:
		return "", false
	}
}

// identExpr renders a reference to a parameter or variable as the C name it
// was declared under, read through the pointer for a mut parameter.
func (g *generator) identExpr(id *ast.Ident) (string, []diag.Diagnostic) {
	switch def := id.Def.(type) {
	case *ast.Param:
		return g.paramRef(def), nil
	case *ast.Var:
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
	right, diags := g.expr(bin.Y, types.Invalid)
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
// that is not a plain name and a builtin, which has no value to give.
func (g *generator) callValue(call *ast.Call) (string, []diag.Diagnostic) {
	if call.Conv != types.Invalid {
		// The checker allows a single-value conversion only where it
		// cannot lose anything, so a C cast does the whole job.
		arg, diags := g.expr(call.Args[0], types.Invalid)
		if len(diags) > 0 {
			return "", diags
		}
		return fmt.Sprintf("((%s)%s)", g.cType(call.Conv), arg), nil
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("unknown expression type: %T", call.Fn),
			Span:     call.Spanned(),
		}}
	}
	if fn, isFunc := id.Def.(*ast.Func); isFunc && fn == nil && id.Name == "len" {
		return g.lenValue(call)
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
// an array, a view or a struct the C struct emitTypes defines for it.
func (g *generator) cType(typ types.Type) string {
	if g.prog.Types.IsArray(typ) {
		return arrayName(typ)
	}
	if g.prog.Types.IsView(typ) {
		return viewName(typ)
	}
	if g.prog.Types.IsStruct(typ) {
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

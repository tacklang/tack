package c

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/cname"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// HeaderName is the name of the header the C files of a program include:
// the driver writes Header's output under it.
const HeaderName = "tack.h"

// guardName is the macro tack.h's include guard defines, which no name in
// it may be.
const guardName = "TACK_H"

// stdGuardName is the macro the include guard of std's tack.h defines.
// std's header defines tack_string and the string helpers, and the
// generated source and a program's tack.h include it, so these names are
// reserved in tack.h as if it defined them itself.
const stdGuardName = "TACK_STD_TACK_H"

// stdHeader is the line that includes std's tack.h, which the generated
// source always writes and a program's tack.h writes when a signature uses
// a string. clang finds it on the include path, at the standard library's
// include directory, which CompileCmd and BuildCmd are given.
const stdHeader = "#include <tack/tack.h>\n"

// Header returns tack.h for prog, which must have been generated without
// diagnostics: the C declarations the C files linked with prog compile
// against. It defines every type an extern func's signature uses, and
// every type those are made of, each after the types it holds, then the
// results struct of each extern func with several results, then a
// prototype for every extern func under its C name, package by package,
// so clang checks each C definition against its Tack declaration, with
// that declaration in a comment beside it.
func (backend) Header(prog *ast.Program) []byte {
	return newHeader(prog).render()
}

// headerDiags returns the problems with the names tack.h would give prog's
// types and fields: one C cannot declare, or one given to two things.
func headerDiags(prog *ast.Program) []diag.Diagnostic {
	return newHeader(prog).diags
}

// header is what tack.h declares for a program, and what is wrong with it.
type header struct {
	prog    *ast.Program
	structs map[types.Type]*ast.TypeDecl // each struct type's declaration
	pkgOf   map[types.Type]*ast.Package  // and its package

	externs []*ast.Func  // every extern func, in program order
	order   []types.Type // the arrays, views and structs to define, each after its parts
	seen    map[types.Type]bool
	names   map[string]owner // what each name tack.h declares belongs to

	diags []diag.Diagnostic

	strings bool // whether tack_string is used, which std's tack.h defines
	sizes   bool // whether size_t is, which <stddef.h> declares
}

// owner is what a name in tack.h belongs to, as a diagnostic describes it,
// and where that is declared.
type owner struct {
	what string
	span ast.Span
	fn   bool // an extern func, whose duplicates the type checker reports
}

// newHeader works out what tack.h declares for prog: the types every
// extern func's signature uses, the names each gets, and any problem with
// those names.
func newHeader(prog *ast.Program) *header {
	h := &header{
		prog:    prog,
		structs: make(map[types.Type]*ast.TypeDecl),
		pkgOf:   make(map[types.Type]*ast.Package),
		seen:    make(map[types.Type]bool),
		names: map[string]owner{
			"tack_string":    {what: "tack.h's string type"},
			"TACK_C_STR_LEN": {what: "a tack.h string helper"},
			"TACK_STR":       {what: "a tack.h string helper"},
			"TACK_STR_N":     {what: "a tack.h string helper"},
			guardName:        {what: "tack.h's include guard"},
			stdGuardName:     {what: "the include guard of std's tack.h"},
		},
	}
	for _, pkg := range prog.Packages {
		for _, decl := range pkg.Decls() {
			switch d := decl.(type) {
			case *ast.TypeDecl:
				h.structs[d.Type], h.pkgOf[d.Type] = d, pkg
			case *ast.Func:
				if d.Extern {
					h.externs = append(h.externs, d)
				}
			}
		}
	}
	for _, fn := range h.externs {
		h.useSignature(fn)
	}
	for _, fn := range h.externs {
		h.claim(fn.CName, owner{
			what: fmt.Sprintf("extern func %s of package %s", fn.Name, h.pkgOfFunc(fn).Path),
			span: fn.NameSpan,
			fn:   true,
		})
	}
	return h
}

// pkgOfFunc returns the package that declares fn.
func (h *header) pkgOfFunc(fn *ast.Func) *ast.Package {
	for _, pkg := range h.prog.Packages {
		for _, decl := range pkg.Decls() {
			if decl == fn {
				return pkg
			}
		}
	}
	return h.prog.Main()
}

// useSignature records every type fn's parameters and results use, and
// fn's results struct if it has several results.
func (h *header) useSignature(fn *ast.Func) {
	for _, p := range fn.Params {
		h.use(p.Type, p.TypeExpr.Spanned())
	}
	for i, typ := range fn.ResultTypes {
		h.use(typ, fn.Results[i].Spanned())
	}
	if len(fn.ResultTypes) > 1 {
		h.claim(resultsCName(fn), owner{
			what: "the results of extern func " + fn.Name,
			span: fn.NameSpan,
		})
	}
}

// use records that tack.h needs typ, used at site, and every type it is
// made of, which are defined before it, and the header that declares a
// basic type's C type when <stdint.h> does not: <stddef.h> for size_t.
func (h *header) use(typ types.Type, site ast.Span) {
	table := h.prog.Types
	switch {
	case typ == types.String:
		h.strings = true
		return
	case typ == types.UnsafeSize:
		h.sizes = true
		return
	case h.seen[typ] || !table.IsArray(typ) && !table.IsStruct(typ) && !table.IsView(typ):
		return
	}
	h.seen[typ] = true
	h.useParts(typ, site)
	h.order = append(h.order, typ)
}

// useParts records that tack.h needs the types typ, an array, view or
// struct type used at site, is made of, and claims typ's name.
func (h *header) useParts(typ types.Type, site ast.Span) {
	table := h.prog.Types
	switch {
	case table.IsArray(typ):
		h.use(table.Elem(typ), site)
		h.claim(h.cName(typ), owner{what: "array type " + table.Name(typ), span: site})
	case table.IsView(typ):
		h.use(table.Elem(typ), site)
		h.claim(h.cName(typ), owner{what: "view type " + table.Name(typ), span: site})
	default:
		for _, f := range table.Fields(typ) {
			h.use(f.Type, site)
		}
		h.claimStruct(typ)
	}
}

// claimStruct claims the name of the struct type typ, and checks its name
// and its fields' names are ones C can declare.
func (h *header) claimStruct(typ types.Type) {
	td, pkg := h.structs[typ], h.pkgOf[typ]
	if td == nil {
		return
	}
	name := h.cName(typ)
	if cname.IsKeyword(name) {
		h.errorf(td.NameSpan, "type %s cannot be named %s in C: it is a C keyword", td.Name, name)
	} else {
		h.claim(name, owner{
			what: fmt.Sprintf("type %s of package %s", td.Name, pkg.Path),
			span: td.NameSpan,
		})
	}
	st, ok := td.TypeExpr.(*ast.StructType)
	if !ok {
		return
	}
	for _, f := range st.Fields {
		if cname.IsKeyword(f.Name) {
			h.errorf(f.NameSpan, "field %s of type %s cannot be named %s in C: it is a C keyword",
				f.Name, td.Name, f.Name)
		}
	}
}

// claim records that name in tack.h belongs to o, and reports a clash if
// it belongs to something else already. Two extern funcs with one C name
// are left to the type checker.
func (h *header) claim(name string, o owner) {
	prev, taken := h.names[name]
	switch {
	case !taken:
		h.names[name] = o
	case prev.fn && o.fn:
	case prev.span == (ast.Span{}):
		h.errorf(o.span, "%s cannot be named %s in tack.h: that is %s", o.what, name, prev.what)
	default:
		h.diags = append(h.diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s and %s are both named %s in tack.h", o.what, prev.what, name),
			Span:     o.span,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  prev.what + " is declared here",
				Span:     prev.span,
			}},
		})
	}
}

// errorf reports an error at span.
func (h *header) errorf(span ast.Span, format string, args ...any) {
	h.diags = append(h.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
	})
}

// cName is the name tack.h gives typ: a basic type's C type, as the
// generated source writes it; a struct type's Tack name, after its
// package's name and an underscore unless it is in package main, so
// Token of package lexer is lexer_Token; an array type tack_array_,
// its length, an underscore, and its element type's name, the Tack name
// of a basic type, so [4]int64 is tack_array_4_int64 and [2][3]Point is
// tack_array_2_tack_array_3_Point; and a view type tack_view_ and its
// element type's name, likewise, so []int64 is tack_view_int64 and
// [][2]Point is tack_view_tack_array_2_Point.
func (h *header) cName(typ types.Type) string {
	table := h.prog.Types
	switch {
	case table.IsArray(typ):
		return fmt.Sprintf("tack_array_%d_%s", table.Len(typ), h.elemName(table.Elem(typ)))
	case table.IsView(typ):
		return "tack_view_" + h.elemName(table.Elem(typ))
	case table.IsStruct(typ):
		td, pkg := h.structs[typ], h.pkgOf[typ]
		if td == nil || pkg == nil {
			return table.Name(typ)
		}
		if pkg == h.prog.Main() {
			return td.Name
		}
		return pkg.Name + "_" + td.Name
	default:
		return basicCType(typ)
	}
}

// elemName is how the tack.h name of an array or view type names its
// element type, elem: by its Tack name if it is a basic type, and by its
// own tack.h name otherwise.
func (h *header) elemName(elem types.Type) string {
	table := h.prog.Types
	if table.IsArray(elem) || table.IsStruct(elem) {
		return h.cName(elem)
	}
	return table.Name(elem)
}

// resultsCName is the name tack.h gives the struct fn, an extern func with
// several results, returns them in: tack_results_ and fn's C name.
func resultsCName(fn *ast.Func) string {
	return "tack_results_" + fn.CName
}

// render writes tack.h.
func (h *header) render() []byte {
	var b bytes.Buffer
	b.WriteString("// Generated by the Tack compiler. Do not edit.\n//\n")
	b.WriteString("// It declares every extern func of the program, for the C files that\n")
	b.WriteString("// define them, and the types their signatures use.\n\n")
	fmt.Fprintf(&b, "#ifndef %s\n#define %s\n\n", guardName, guardName)
	b.WriteString("#include <stdbool.h>\n#include <stdint.h>\n")
	if h.sizes {
		b.WriteString("#include <stddef.h>\n")
	}
	if h.strings {
		b.WriteString(stdHeader)
	}
	for _, typ := range h.order {
		b.WriteString("\n" + h.typeDef(typ))
	}
	for _, fn := range h.externs {
		if len(fn.ResultTypes) > 1 {
			b.WriteString("\ntypedef struct {\n")
			for i, typ := range fn.ResultTypes {
				fmt.Fprintf(&b, "    %s r%d;\n", h.cName(typ), i)
			}
			fmt.Fprintf(&b, "} %s;\n", resultsCName(fn))
		}
	}
	h.renderPrototypes(&b)
	b.WriteString("\n#endif\n")
	return b.Bytes()
}

// typeDef is tack.h's definition of typ, an array, view or struct type:
// the same layout as the generated source's, so a value passes between the
// two unchanged. An array is a struct holding a C array, e, of at least one
// element; a view a pointer to its first element, ptr, to const, and its
// number of elements, len; a struct has a member per field, under the
// field's Tack name, or one unused char if it has none.
func (h *header) typeDef(typ types.Type) string {
	return fmt.Sprintf(
		"typedef struct {\n    %s\n} %s;\n",
		strings.Join(h.members(typ), "\n    "),
		h.cName(typ),
	)
}

// members returns the members of tack.h's definition of typ, as typeDef
// describes them, each a declaration ending in a semicolon.
func (h *header) members(typ types.Type) []string {
	table := h.prog.Types
	switch {
	case table.IsArray(typ):
		return []string{
			fmt.Sprintf("%s e[%d];", h.cName(table.Elem(typ)), max(table.Len(typ), 1)),
		}
	case table.IsView(typ):
		return []string{fmt.Sprintf("const %s *ptr;", h.cName(table.Elem(typ))), "int64_t len;"}
	}
	fields := table.Fields(typ)
	if len(fields) == 0 {
		return []string{"char unused;"}
	}
	members := make([]string, len(fields))
	for i, f := range fields {
		members[i] = fmt.Sprintf("%s %s;", h.cName(f.Type), f.Name)
	}
	return members
}

// renderPrototypes writes a prototype for every extern func, under a
// comment naming its package.
func (h *header) renderPrototypes(b *bytes.Buffer) {
	for _, pkg := range h.prog.Packages {
		var protos []string
		for _, decl := range pkg.Decls() {
			if fn, ok := decl.(*ast.Func); ok && fn.Extern {
				protos = append(protos, h.prototype(fn)+" // "+h.tackSignature(fn))
			}
		}
		if len(protos) > 0 {
			fmt.Fprintf(b, "\n// package %s\n%s\n", pkg.Path, strings.Join(protos, "\n"))
		}
	}
}

// prototype is the prototype of fn, an extern func, as tack.h declares it:
// its C name, and the C type of each parameter and result. A parameter's
// name follows its type in a comment, not as a C name, since a Tack name
// can be a C keyword or one of tack.h's type names, and no Tack name can
// hold the */ that would end the comment. A mut parameter is a pointer to
// its type, T *.
func (h *header) prototype(fn *ast.Func) string {
	result := "void"
	switch len(fn.ResultTypes) {
	case 0:
	case 1:
		result = h.cName(fn.ResultTypes[0])
	default:
		result = resultsCName(fn)
	}
	params := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = h.cName(p.Type)
		if p.Mut {
			params[i] += " *"
		}
		params[i] += " /* " + p.Name + " */"
	}
	if len(params) == 0 {
		params = []string{"void"}
	}
	return fmt.Sprintf("%s %s(%s);", result, fn.CName, strings.Join(params, ", "))
}

// tackSignature is fn's declaration as Tack writes it, a type of another
// package qualified by that package's name, which tack.h shows beside its
// prototype, so a clang error that quotes the prototype also shows what it
// declares.
func (h *header) tackSignature(fn *ast.Func) string {
	pkg := h.pkgOfFunc(fn)
	name := func(typ types.Type) string {
		return h.prog.Types.NameWith(typ, func(st types.Type, n string) string {
			if p := h.pkgOf[st]; p != nil && p != pkg {
				return p.Name + "." + n
			}
			return n
		})
	}
	params := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = p.Name + " " + name(p.Type)
		if p.Mut {
			params[i] = "mut " + params[i]
		}
	}
	results := make([]string, len(fn.ResultTypes))
	for i, r := range fn.ResultTypes {
		results[i] = name(r)
	}
	sig := fmt.Sprintf("extern func %s(%s)", fn.Name, strings.Join(params, ", "))
	switch len(results) {
	case 0:
		return sig
	case 1:
		return sig + " " + results[0]
	default:
		return sig + " (" + strings.Join(results, ", ") + ")"
	}
}

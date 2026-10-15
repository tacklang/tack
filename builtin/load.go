package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/cname"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// packageName is the package every file of std/builtin is in.
const packageName = "builtin"

// cNamePrefix starts every builtin's C name. It is the prefix user extern
// funcs may not take, so no C file of a program can mean to define a
// builtin by declaring an extern func.
const cNamePrefix = "tack_"

// DiagnosticsError is what Load returns when the Tack files of std/builtin
// do not lex, do not parse, or declare something that is not a builtin.
// Its diagnostics point into those files, which Files holds, so they render
// as a program's do.
type DiagnosticsError struct {
	Files *source.Set
	Diags []diag.Diagnostic
}

// Error returns the diagnostics rendered as the compiler renders any.
func (e *DiagnosticsError) Error() string {
	return diag.RenderAllPretty(e.Files, e.Diags)
}

// Load reads the builtins declared in dir, a std/builtin directory: every
// file directly in it whose name ends in .tck, in the byte order of their
// names, leaving out a name that starts with a dot and a directory, as the
// loader does for a package. It also lists the .c files there, which define
// the builtins' bodies. A directory that cannot be read, or holds no .tck
// file, is an error; files that do not declare builtins, and only
// builtins, are a *DiagnosticsError naming them.
func Load(dir string) (*Set, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read the builtins: %w", err)
	}
	var files []*source.File
	var cFiles []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		switch filepath.Ext(name) {
		case ".tck":
			data, readErr := os.ReadFile(full) //nolint:gosec // reading std is the point
			if readErr != nil {
				return nil, fmt.Errorf("cannot read the builtins: %w", readErr)
			}
			files = append(files, &source.File{
				ID: source.FileID(full), Filename: full, Data: data,
			})
		case ".c":
			cFiles = append(cFiles, full)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .tck files in %s, which declares the builtins", dir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Filename < files[j].Filename })
	sort.Strings(cFiles)

	funcs, err := declare(files)
	if err != nil {
		return nil, err
	}
	return &Set{dir: dir, funcs: funcs, cFiles: cFiles}, nil
}

// declare lexes and parses files, the Tack files of std/builtin, and
// returns the builtins they declare, in order. Every problem in every file
// is reported, at the first stage that has any, as a *DiagnosticsError.
func declare(files []*source.File) ([]Func, error) {
	set := source.NewSet()
	for _, f := range files {
		set.Add(f)
	}
	fail := func(diags []diag.Diagnostic) error {
		return &DiagnosticsError{Files: set, Diags: diags}
	}

	tokens := make([][]lexer.Token, len(files))
	var diags []diag.Diagnostic
	for i, f := range files {
		lex := lexer.New(f)
		diags = append(diags, lex.Lex()...)
		tokens[i] = lex.Tokens()
	}
	if len(diags) > 0 {
		return nil, fail(diags)
	}

	trees := make([]*ast.File, 0, len(files))
	for i, f := range files {
		tree, fileDiags := parser.NewParser(f.ID, tokens[i]).Parse()
		diags = append(diags, fileDiags...)
		if tree != nil {
			trees = append(trees, tree)
		}
	}
	if len(diags) > 0 {
		return nil, fail(diags)
	}

	d := &declarer{byName: make(map[string]*ast.Func), byCName: make(map[string]*ast.Func)}
	for _, tree := range trees {
		d.file(tree)
	}
	if len(d.diags) > 0 {
		return nil, fail(d.diags)
	}
	return d.funcs, nil
}

// declarer checks the trees of std/builtin and collects the builtins they
// declare.
type declarer struct {
	funcs   []Func
	byName  map[string]*ast.Func // the declaration of each builtin, by name
	byCName map[string]*ast.Func // likewise, by C name
	diags   []diag.Diagnostic
}

// errorf records an error at span.
func (d *declarer) errorf(span ast.Span, format string, args ...any) {
	d.diags = append(d.diags, diag.Diagnostic{
		Severity: diag.Error, Message: fmt.Sprintf(format, args...), Span: span,
	})
}

// file checks one file of std/builtin and adds the builtins it declares. A
// file is package builtin, imports nothing, and declares nothing but
// extern funcs.
func (d *declarer) file(f *ast.File) {
	if f.Package != packageName {
		d.errorf(f.PackageSpan, "a file of std/builtin must be package builtin, not %s", f.Package)
	}
	for _, imp := range f.Imports {
		d.errorf(imp.Span, "a file of std/builtin cannot import a package")
	}
	for _, decl := range f.Decls {
		switch x := decl.(type) {
		case *ast.Func:
			d.fn(x)
		case *ast.Var:
			d.errorf(x.NameSpan,
				"std/builtin declares only builtins, as extern funcs; var %s is not one", x.Name)
		case *ast.TypeDecl:
			d.errorf(x.NameSpan,
				"std/builtin declares only builtins, as extern funcs; type %s is not one", x.Name)
		case *ast.Import:
			d.errorf(x.Span, "a file of std/builtin cannot import a package")
		default:
			d.errorf(decl.Spanned(), "std/builtin declares only builtins, as extern funcs")
		}
	}
}

// fn checks fn, a function declared in std/builtin, and adds it as a
// builtin if it is one: an extern func with a C name of its own, starting
// tack_, named nothing a builtin cannot be named, neither pub nor lib, and
// returning nothing, whose parameters are each of a basic type but any and
// none is mut or variadic.
func (d *declarer) fn(fn *ast.Func) {
	before := len(d.diags)
	switch {
	case !fn.Extern:
		d.errorf(
			fn.NameSpan,
			"builtin %s must be an extern func; its body is C, in a .c file of std/builtin",
			fn.Name,
		)
		return
	case fn.Vis != ast.Private:
		d.errorf(fn.VisSpan, "builtin %s cannot be %s; every builtin is visible everywhere",
			fn.Name, fn.Vis)
	}
	d.checkName(fn)
	d.checkCName(fn)
	if len(fn.Results) > 0 {
		d.errorf(
			fn.Results[0].Spanned(),
			"builtin %s cannot return a value; builtins return nothing",
			fn.Name,
		)
	}
	params := make([]types.Type, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = d.param(fn, p)
	}
	if len(d.diags) > before {
		return
	}
	d.byName[fn.Name] = fn
	d.byCName[fn.CName] = fn
	d.funcs = append(d.funcs, Func{Name: fn.Name, CName: fn.CName, Params: params})
}

// checkName reports a builtin whose name no builtin can have: a basic
// type's, a generic builtin's, main, _, or another builtin's.
func (d *declarer) checkName(fn *ast.Func) {
	_, isType := types.Lookup(fn.Name)
	switch {
	case isType:
		d.errorf(fn.NameSpan, "builtin %s cannot be named after a basic type", fn.Name)
	case isGeneric(fn.Name):
		d.errorf(fn.NameSpan, "builtin %s cannot be declared: the compiler implements %s itself",
			fn.Name, fn.Name)
	case fn.Name == "main" || fn.Name == "_":
		d.errorf(fn.NameSpan, "a builtin cannot be named %s", fn.Name)
	}
	if prev, dup := d.byName[fn.Name]; dup {
		d.diags = append(d.diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("builtin %s is declared twice", fn.Name),
			Span:     fn.NameSpan,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("builtin %s is first declared here", fn.Name),
				Span:     prev.NameSpan,
			}},
		})
	}
}

// checkCName reports a builtin without a C name of its own, given with
// extern("..."), or whose C name is not a C identifier, is a C keyword,
// does not start tack_, or is another builtin's.
func (d *declarer) checkCName(fn *ast.Func) {
	if fn.CNameSpan == (ast.Span{}) {
		d.errorf(fn.ExternSpan, "builtin %s has no C name; give it one, extern(%q)",
			fn.Name, cNamePrefix+packageName+"_"+fn.Name)
		return
	}
	at, name := fn.CNameSpan, fn.CName
	switch {
	case !cname.IsIdent(name):
		d.errorf(at, "builtin %s cannot be named %s in C: it is not a C identifier",
			fn.Name, strconv.Quote(name))
		return
	case cname.IsKeyword(name):
		d.errorf(at, "builtin %s cannot be named %s in C: it is a C keyword", fn.Name, name)
		return
	case !strings.HasPrefix(name, cNamePrefix):
		d.errorf(at, "builtin %s cannot be named %s in C: a builtin's C name starts %s",
			fn.Name, name, cNamePrefix)
		return
	}
	if prev, dup := d.byCName[name]; dup {
		d.diags = append(d.diags, diag.Diagnostic{
			Severity: diag.Error,
			Message: fmt.Sprintf("builtins %s and %s both have the C name %s",
				fn.Name, prev.Name, name),
			Span: at,
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  fmt.Sprintf("builtin %s is declared here", prev.Name),
				Span:     prev.NameSpan,
			}},
		})
	}
}

// param returns the type of p, a parameter of the builtin fn, reporting a
// mut parameter, a variadic one, one whose type is not a basic type's
// name, and an any one.
func (d *declarer) param(fn *ast.Func, p *ast.Param) types.Type {
	if p.Mut {
		d.errorf(p.MutSpan, "parameter %s of builtin %s cannot be mut", p.Name, fn.Name)
	}
	if p.Variadic {
		d.errorf(p.EllipsisSpan, "parameter %s of builtin %s cannot be variadic", p.Name, fn.Name)
	}
	id, ok := p.TypeExpr.(*ast.Ident)
	if !ok {
		at := p.Span
		if p.TypeExpr != nil {
			at = p.TypeExpr.Spanned()
		}
		d.errorf(at,
			"parameter %s of builtin %s must be of a basic type, such as int64 or string",
			p.Name, fn.Name)
		return types.Invalid
	}
	typ, known := types.Lookup(id.Name)
	switch {
	case !known:
		d.errorf(id.Span,
			"parameter %s of builtin %s must be of a basic type, such as int64 or string, not %s",
			p.Name, fn.Name, id.Name)
	case typ == types.Any:
		// A call to a builtin passes each argument as itself, and an any
		// argument needs its type id and address made at the call.
		d.errorf(id.Span, "parameter %s of builtin %s cannot be any", p.Name, fn.Name)
		return types.Invalid
	}
	return typ
}

// isGeneric reports whether name is a generic builtin's.
func isGeneric(name string) bool {
	for _, g := range generic {
		if g == name {
			return true
		}
	}
	return false
}

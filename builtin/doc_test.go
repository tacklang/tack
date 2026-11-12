package builtin_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// standIns are the types the builtins' file declares for documentation
// only, which no program can name: what a generic builtin's parameter
// accepts, which no Tack type says.
var standIns = []string{"Elem", "Key", "Sized", "ListType", "StructType"}

// doc returns the builtins' file, parsed, failing t if it does not parse.
func doc(t *testing.T) *builtin.DocFile {
	t.Helper()
	d, err := builtin.Doc()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// counted returns how many times each name in names occurs.
func counted(names []string) map[string]int {
	n := make(map[string]int)
	for _, name := range names {
		n[name]++
	}
	return n
}

// checkOneEach reports each name of want that got does not hold exactly
// once, and each name of got that want does not hold, as kind.
func checkOneEach(t *testing.T, kind string, got, want []string) {
	t.Helper()
	n := counted(got)
	for _, name := range want {
		if n[name] != 1 {
			t.Errorf("builtin.tck declares %s %s %d times, want once", kind, name, n[name])
		}
	}
	for name := range n {
		if !slices.Contains(want, name) {
			t.Errorf(
				"builtin.tck declares %s %s, which the compiler does not implement",
				kind,
				name,
			)
		}
	}
}

// The builtins' file and the compiler agree one to one: every builtin
// function the compiler implements is declared there exactly once, with
// its parameters' names, in order; every builtin value is a var, every
// builtin type a type and every builtin constraint an interface declared
// exactly once; and the file declares nothing else but the stand-in
// types, each an interface, and imports nothing.
func TestDocFileMatchesTheCompiler(t *testing.T) {
	d := doc(t)
	if d.Tree.Package != "builtin" {
		t.Errorf("builtin.tck is package %s, want builtin", d.Tree.Package)
	}
	if len(d.Tree.Imports) > 0 {
		t.Errorf("builtin.tck imports %d packages, want none", len(d.Tree.Imports))
	}
	funcs, vars, typeNames, constraints := docDecls(t, d)
	checkOneEach(t, "func", funcs, builtin.Default().Names())
	checkOneEach(t, "var", vars, builtin.Values())
	checkOneEach(t, "type", typeNames, builtin.Types())
	checkOneEach(t, "constraint", constraints, builtin.Constraints())
	for _, name := range standIns {
		if _, ok := d.Decl(name); !ok {
			t.Errorf("builtin.tck does not declare stand-in %s", name)
		}
	}
}

// docDecls returns the names of the functions, vars, types and
// constraints, stand-ins aside, d declares, checking each function (see
// checkFunc), that no var has a value or is mut, and that each stand-in
// and each constraint is an interface, and reporting anything else
// declared.
func docDecls(t *testing.T, d *builtin.DocFile) (funcs, vars, typeNames, constraints []string) {
	t.Helper()
	for _, decl := range d.Tree.Decls {
		switch x := decl.(type) {
		case *ast.Func:
			funcs = append(funcs, x.Name)
			checkFunc(t, x)
		case *ast.Var:
			vars = append(vars, x.Name)
			if x.Value != nil || x.Mutable {
				t.Errorf("builtin.tck declares var %s with a value or as mut", x.Name)
			}
		case *ast.TypeDecl:
			switch kind := typeKind(t, x); kind {
			case "constraint":
				constraints = append(constraints, x.Name)
			case "type":
				typeNames = append(typeNames, x.Name)
			}
		default:
			t.Errorf("builtin.tck declares %T, which is no builtin", decl)
		}
	}
	return funcs, vars, typeNames, constraints
}

// typeKind returns what td, a type the builtins' file declares, is: a
// constraint or a stand-in, each checked to be an interface, or a type.
func typeKind(t *testing.T, td *ast.TypeDecl) string {
	t.Helper()
	kind := "type"
	switch {
	case builtin.IsConstraint(td.Name):
		kind = "constraint"
	case slices.Contains(standIns, td.Name):
		kind = "stand-in"
	default:
		return kind
	}
	if _, ok := td.TypeExpr.(*ast.InterfaceType); !ok {
		t.Errorf("%s %s is not an interface", kind, td.Name)
	}
	return kind
}

// checkFunc checks fn, a function the builtins' file declares: it has no
// body and is no method, and its parameters have the names the compiler
// gives them; one with a C body has their types too.
func checkFunc(t *testing.T, fn *ast.Func) {
	t.Helper()
	if !fn.NoBody || fn.Extern || fn.Method {
		t.Errorf("builtin.tck declares %s with a body, as extern or as a method", fn.Name)
	}
	want, ok := builtin.ParamNames(fn.Name)
	if !ok {
		return // reported by the caller
	}
	got := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		got[i] = p.Name
	}
	if !slices.Equal(got, want) {
		t.Errorf("builtin.tck declares %s(%s), the compiler %s(%s)",
			fn.Name, strings.Join(got, ", "), fn.Name, strings.Join(want, ", "))
	}
	if c, hasC := builtin.Default().Lookup(fn.Name); hasC {
		checkParamTypes(t, fn, c)
	}
}

// checkParamTypes checks the parameters of fn, the declaration of the
// builtin c, which has a C body, have c's types.
func checkParamTypes(t *testing.T, fn *ast.Func, c builtin.Func) {
	t.Helper()
	for i, p := range fn.Params {
		id, isName := p.TypeExpr.(*ast.Ident)
		if !isName || i >= len(c.Params) {
			t.Errorf("builtin.tck declares parameter %s of %s with no basic type", p.Name, fn.Name)
			continue
		}
		if typ, _ := types.Lookup(id.Name); typ != c.Params[i] {
			t.Errorf("builtin.tck declares parameter %s of %s as %s, the compiler as type %d",
				p.Name, fn.Name, id.Name, c.Params[i])
		}
	}
}

// refresh is how to refresh std's copies of the files built into the
// compiler, which a failing check names.
const refresh = "run make std-copies to refresh it"

// std's copies of the builtins' file and the runtime header are the files
// built into the compiler, byte for byte, so that the repository's std,
// which tests use as TACK_HOME, and an editor browsing it show what the
// compiler holds.
func TestStdCopiesMatch(t *testing.T) {
	for _, c := range []struct {
		path string
		want []byte
	}{
		{"../src/std/builtin/builtin.tck", builtin.Source()},
		{"../src/std/include/tack/tack.h", builtin.RuntimeHeader()},
	} {
		got, err := os.ReadFile(c.path)
		if err != nil {
			t.Errorf("%s: %v; %s", c.path, err, refresh)
			continue
		}
		if !bytes.Equal(got, c.want) {
			t.Errorf("%s differs from the copy built into the compiler; %s", c.path, refresh)
		}
	}
}

// The forms only the builtins' file may hold, a var named by a keyword
// and a type declared as itself, are errors when it is parsed as any
// other file is.
func TestDocFileFormsFailOutsideBuiltinMode(t *testing.T) {
	for _, src := range []string{
		"package builtin\n\nvar true bool\n",
		"package builtin\n\nvar noerror error\n",
		"package builtin\n\nvar c_allocator Allocator\n",
		"package builtin\n\ntype int32 int32\n",
		"package builtin\n\ntype T T\n",
	} {
		f := &source.File{ID: "x.tck", Filename: "x.tck", Data: []byte(src)}
		lex := lexer.New(f)
		if diags := lex.Lex(); len(diags) > 0 {
			t.Fatalf("%q does not lex: %v", src, diags)
		}
		if _, diags := parser.NewParser(f.ID, lex.Tokens()).Parse(); len(diags) == 0 {
			t.Errorf("%q parsed outside builtin mode", src)
		}
		if _, diags := parser.NewBuiltinParser(f.ID, lex.Tokens()).Parse(); len(diags) > 0 {
			t.Errorf("%q does not parse in builtin mode: %v", src, diags)
		}
	}
	src := builtin.Source()
	f := &source.File{ID: "x.tck", Filename: "x.tck", Data: src}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatal(diags)
	}
	if _, diags := parser.NewParser(f.ID, lex.Tokens()).Parse(); len(diags) == 0 {
		t.Error("builtin.tck parsed outside builtin mode")
	}
}

// Only this package and the parser's own tests make a parser in builtin
// mode: nothing that parses a program's or std's files can.
func TestOnlyBuiltinParsesInBuiltinMode(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() != ".." && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go"):
			return nil
		}
		return checkNoBuiltinParser(t, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// checkNoBuiltinParser reports the Go file at path when it makes a parser
// in builtin mode and is not in this package or the parser's.
func checkNoBuiltinParser(t *testing.T, path string) error {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // reading the module's own sources
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	dir := filepath.Base(filepath.Dir(path))
	if bytes.Contains(data, []byte("NewBuiltinParser(")) && dir != "builtin" && dir != "parser" {
		t.Errorf("%s makes a parser in builtin mode", path)
	}
	return nil
}

// TestCopyError checks CopyError accepts the compiler's own builtins' file
// and rejects any other, naming the copy and how to refresh it.
func TestCopyError(t *testing.T) {
	if err := builtin.CopyError("builtin.tck", builtin.Source()); err != nil {
		t.Errorf("the compiler's own file: %v", err)
	}
	edited := append(append([]byte{}, builtin.Source()...), "// edited\n"...)
	err := builtin.CopyError("std/builtin/builtin.tck", edited)
	if err == nil || !strings.Contains(err.Error(), "std/builtin/builtin.tck differs") ||
		!strings.Contains(err.Error(), "make std-copies") {
		t.Errorf("an edited copy: %v", err)
	}
}

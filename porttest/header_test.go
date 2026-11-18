package porttest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// The backend's header, H's, checked before the lowering is ported:
// tack.h as Go's backend renders it, Header, which Go's --emit-header
// writes only after lowering the whole program, compared with the port's
// --port-header (see TestHeaderRender); the names tack.h may not give
// and the any type ids, Go's reservedNames and anyIDs, compared with the
// port's --port-reserved (see TestReservedTables); and the header's walk
// of deep and long types, through --emit, whose check of tack.h comes
// before any lowering, and --port-header (see TestHeaderDeep).

// reservedSources are the declarations of Go's backend TestReservedTables
// builds its dump of the tables from, by file of codegen/c and name.
var reservedSources = map[string][]string{
	"reserved.go": {"reservation", "reservedNames"},
	"any.go":      {"anyIDs", "viewAnyID"},
	"header.go":   {"guardName", "stdGuardName"},
}

// reservedMain is the main function of the program TestReservedTables
// runs: every reserved name, a tab, 1 for an object-like macro or 0, a
// tab and what defines it, and every type of anyIDs, "any", a tab, its
// name, a tab, its macro, a tab and the macro of a view of it, a line
// each, sorted, as the port's --port-reserved writes them, unsorted.
const reservedMain = `
func main() {
	var lines []string
	for name, r := range reservedNames {
		obj := "0"
		if r.objectMacro {
			obj = "1"
		}
		lines = append(lines, name+"\t"+obj+"\t"+r.what)
	}
	for typ, m := range anyIDs {
		lines = append(lines, "any\t"+(*types.Table)(nil).Name(typ)+"\t"+m+"\t"+viewAnyID(typ))
	}
	sort.Strings(lines)
	fmt.Print(strings.Join(lines, "\n") + "\n")
}
`

// reservedProgram returns the source of a program of Go's own
// declarations of the tables, reservedSources, copied from codegen/c by
// go/parser as they are, and reservedMain, which writes them.
//
//nolint:gocognit,cyclop // a walk of a file's declarations and their specs; split, it reads no plainer
func reservedProgram() (string, error) {
	var b bytes.Buffer
	b.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"sort\"\n\t\"strings\"\n\n" +
		"\t\"github.com/tacklang/tack/types\"\n)\n\n")
	for _, file := range slices.Sorted(func(yield func(string) bool) {
		for f := range reservedSources {
			if !yield(f) {
				return
			}
		}
	}) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(env.root, "codegen", "c", file), nil,
			parser.SkipObjectResolution)
		if err != nil {
			return "", fmt.Errorf("parsing codegen/c/%s: %w", file, err)
		}
		found := map[string]bool{}
		for _, d := range f.Decls {
			var name string
			switch d := d.(type) {
			case *ast.FuncDecl:
				name = d.Name.Name
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						name = sp.Name.Name
					case *ast.ValueSpec:
						name = sp.Names[0].Name
					}
				}
			}
			if !slices.Contains(reservedSources[file], name) {
				continue
			}
			found[name] = true
			if err := format.Node(&b, fset, d); err != nil {
				return "", fmt.Errorf("printing %s of codegen/c/%s: %w", name, file, err)
			}
			b.WriteString("\n\n")
		}
		for _, want := range reservedSources[file] {
			if !found[want] {
				return "", fmt.Errorf("codegen/c/%s declares no %s", file, want)
			}
		}
	}
	b.WriteString(reservedMain)
	return b.String(), nil
}

// TestReservedTables compares the port's --port-reserved, its tables of
// the names tack.h may not give and of the any type ids, with Go's own:
// a program made of Go's declarations of them, copied from codegen/c's
// source as they are, built with go run into a package that exists only
// in an overlay, so the tree is not written to, and run. Each side's
// lines are sorted, Go's map having no order. It runs only as TestPort
// does.
func TestReservedTables(t *testing.T) {
	ready(t)
	src, err := reservedProgram()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	if werr := os.WriteFile(mainFile, []byte(src), 0o600); werr != nil {
		t.Fatal(werr)
	}
	pkgDir := filepath.Join(env.root, "porttest", "internal-reserved-tables")
	overlay, err := json.Marshal(map[string]map[string]string{
		"Replace": {filepath.Join(pkgDir, "main.go"): mainFile},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(dir, "overlay.json")
	if werr := os.WriteFile(overlayFile, overlay, 0o600); werr != nil {
		t.Fatal(werr)
	}
	cmd := exec.CommandContext(t.Context(), "go", "run", "-overlay", overlayFile, pkgDir)
	cmd.Dir = env.root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	want, err := cmd.Output()
	if err != nil {
		t.Fatalf("running Go's tables: %v\n%s\nsource:\n%s", err, stderr.Bytes(), src)
	}
	port := runCompiler(t, env.portRun, env.portCache, input{name: "reserved", dir: dir, file: "x"},
		[]string{"--port-reserved", "x"})
	if port.code != 0 || len(port.stderr) > 0 {
		t.Fatalf("the port exited %d, writing to standard error:\n%s", port.code, port.stderr)
	}
	lines := strings.Split(strings.TrimSuffix(string(port.stdout), "\n"), "\n")
	slices.Sort(lines)
	got := strings.Join(lines, "\n") + "\n"
	if d := firstDiff(want, []byte(got)); d != "" {
		t.Errorf("the tables differ: %s", d)
	}
	t.Logf("%d entries compared", strings.Count(string(want), "\n"))
}

// headerRenderSuite is the suite of TestHeaderRender's inputs in the
// divergence list, whose entries record the port's tack.h as its
// standard output (see checkHeader).
const headerRenderSuite = "header-render"

// headerRenderDir holds the programs TestHeaderRender renders besides
// emit-header's inputs: externs of every kind of parameter and result,
// in every mode, names C cannot take where tack.h writes them only in a
// comment, and types tack.h names oddly.
func headerRenderDir() string { return filepath.Join(env.root, "porttest", "testdata", "header") }

// headerRenderInputs returns emit-header's inputs and the programs of
// headerRenderDir, each run from its directory.
func headerRenderInputs() ([]input, error) {
	ins, err := emitHeaderInputs()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(headerRenderDir())
	if err != nil {
		return nil, fmt.Errorf("listing the header's programs: %w", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tck") {
			ins = append(ins, input{name: "header/" + e.Name(), dir: headerRenderDir(), file: e.Name()})
		}
	}
	return ins, nil
}

// goHeader returns tack.h as Go's backend's Header renders it for in,
// loaded as --check loads it, resolved with a missing main allowed and
// type-checked, and whether every stage reported no error; Header is
// only ever asked for such a program.
func goHeader(t *testing.T, in input) ([]byte, bool) {
	t.Helper()
	res, ok := loadAs(t, in, true)
	if !ok || res.Program == nil || diag.HasErrors(res.Diags) {
		return nil, false
	}
	prog := res.Program
	r := resolver.New(prog, builtin.Default())
	r.MainOptional = true
	if diag.HasErrors(r.Resolve()) {
		return nil, false
	}
	if diag.HasErrors(typeck.New(prog, res.Files, builtin.Default()).Check()) {
		return nil, false
	}
	return c.New().Header(prog), true
}

// checkHeader compares the port's --port-header of in, run by port, with
// goHeader's, for an input Go accepts with no error, and reports whether
// in is one; any other passes at once. A decline fails: the type
// checker checks every such program.
func checkHeader(t *testing.T, in input, port string) bool {
	t.Helper()
	want, ok := goHeader(t, in)
	if !ok {
		return false
	}
	got := runCompiler(t, port, env.portCache, in, []string{"--port-header", in.file})
	if got.code != 0 || len(got.stderr) > 0 {
		t.Fatalf("the port exited %d, writing to standard error:\n%s", got.code, got.stderr)
	}
	if dv, listed := divergenceFor(t, stage{name: headerRenderSuite}, in); listed {
		// A known divergence records the port's tack.h, which Go's must
		// not be.
		if bytes.Equal(want, dv.want.stdout) {
			t.Errorf("divergence %s is stale: Go's backend renders its recorded tack.h; remove the entry from %s",
				dv.id, divergencesFile)
		}
		want = dv.want.stdout
	}
	if d := firstDiff(want, got.stdout); d != "" {
		t.Errorf("tack.h differs: %s\ncommand: (cd %s && tack --port-header %s)", d, in.dir, in.file)
	}
	return true
}

// TestHeaderRender compares the port's --port-header, tack.h as its
// backend renders it, with Go's backend's Header run in process, on
// every input of emit-header and every program of headerRenderDir that
// Go accepts with no error, whatever its header's diagnostics, which
// Header renders past, or with the tack.h a known divergence of the
// suite header-render records. It runs only as TestPort does.
func TestHeaderRender(t *testing.T) {
	ready(t)
	ins, err := headerRenderInputs()
	if err != nil {
		t.Fatal(err)
	}
	var compared int
	checkDivergencesListed(t, headerRenderSuite, ins)
	t.Run("cases", func(t *testing.T) {
		for _, in := range asSuite(ins, headerRenderSuite) {
			t.Run(in.name, func(t *testing.T) {
				if checkHeader(t, in, env.portRun) {
					compared++
				}
			})
		}
	})
	t.Logf("%d of %d inputs compared", compared, len(ins))
	if compared == 0 {
		t.Error("no input compared")
	}
}

// headerTypeDepth is how deeply TestHeaderDeep nests an extern's array
// types: as deeply as the parser lets a type nest, less a few levels for
// what is around it.
const headerTypeDepth = deepLimit - 10

// headerDeepPrograms returns the programs TestHeaderDeep runs, by file
// name, each of whose header the walk must make without a frame a level
// or a link: an extern taking arrays nested headerTypeDepth deep, of a
// struct whose field tack.h cannot name, and a view of them, whose name
// tack.h writes; a chain of structs n links long held by value, the
// bottom one's field a macro of tack.h's, and one with no bad name; and
// n externs each of its own array type.
func headerDeepPrograms(n int) map[string]string {
	arrays := rep("[1]", headerTypeDepth)
	many := func() string {
		var b strings.Builder
		b.WriteString("package main\n\n")
		for i := range n / 10 {
			fmt.Fprintf(&b, "extern func f%d(x [%d]int64, mut l list[[%d]int64]) (int64, error)\n", i, i+1, i+1)
		}
		b.WriteString("\nfunc main() {\n}\n")
		return b.String()
	}
	return map[string]string{
		"header-arrays.tck": "package main\n\ntype P struct {\n    NULL int64\n}\n\n" +
			"extern func f(x " + arrays + "P)\n\nfunc main() {\n}\n",
		"header-array-view.tck": "package main\n\ntype P struct {\n    x int64\n}\n\n" +
			"extern func f(x []" + rep("[1]", headerTypeDepth-1) + "P, y " + arrays + "int64) " +
			arrays + "bool\n\nfunc main() {\n}\n",
		"header-chain-bad.tck":   structChain(n, "TACK_NO_ERROR int64", "extern(\"f\") func f(x S%d)\n"),
		"header-chain-clean.tck": structChain(n, "v [2]int64", "extern(\"f\") func f(mut x S%d) S%[1]d\n"),
		"header-many.tck":        many(),
	}
}

// TestHeaderDeep runs headerDeepPrograms, the chains typeChainLength
// links long, through --emit, compared as emit-single compares its
// cases, the port's stack held to deepStack KB and to pairStack KB:
// --emit checks tack.h's names before it lowers anything, so a program
// whose header has an error is compared now, and one with none
// declines until the lowering is ported. Each is rendered through
// --port-header too, compared with Go's Header. It runs only as TestPort
// does.
func TestHeaderDeep(t *testing.T) {
	ready(t)
	srcs := headerDeepPrograms(typeChainLength)
	t.Run("emit", func(t *testing.T) {
		runEmitDeep(t, emitSingleStage, srcs, env.portRun, env.portPairs)
	})
	t.Run("render", func(t *testing.T) {
		dir := t.TempDir()
		for file, src := range srcs {
			t.Run(file, func(t *testing.T) {
				t.Parallel()
				sub := filepath.Join(dir, strings.TrimSuffix(file, ".tck"))
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sub, file), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				in := input{name: file, dir: sub, file: file}
				for _, port := range []string{env.portRun, env.portPairs} {
					if !checkHeader(t, in, port) {
						t.Fatal("Go rejects the program")
					}
				}
			})
		}
	})
}

// TestHeaderDeepPortOnly runs headerDeepPrograms with chains chainLength
// links long, which Go's backend, quadratic in them, would take minutes
// on, on the port alone, through --check and --port-header: with its
// stack held to deepStack KB it must exit and write exactly what it does
// with bigStack KB, and never be killed. A --check the port declines,
// for a program whose header has no error, is let be. It runs only as
// TestPort does.
//
//nolint:cyclop // a run of steps, each failing or going on; split, it reads no plainer
func TestHeaderDeepPortOnly(t *testing.T) {
	ready(t)
	dir := t.TempDir()
	big, err := writeStackWrapper(dir, "port-big", bigStack)
	if err != nil {
		t.Fatal(err)
	}
	for file, src := range headerDeepPrograms(chainLength) {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			sub := filepath.Join(dir, strings.TrimSuffix(file, ".tck"))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, file), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			in := input{name: file, dir: sub, file: file}
			for _, args := range [][]string{checkArgs(in), {"--port-header", file}} {
				got := runCompiler(t, env.portRun, env.portCache, in, args)
				want := runCompiler(t, big, env.portCache, in, args)
				if got.code != want.code || !bytes.Equal(got.stdout, want.stdout) ||
					!bytes.Equal(got.stderr, want.stderr) {
					t.Errorf("%s: at %d KB the port exited %d, writing\n%s\nat %d KB it exited %d, writing\n%s",
						strings.Join(args, " "), deepStack, got.code, got.stderr, bigStack, want.code, want.stderr)
				}
				if args[0] == "--port-header" && got.code != 0 {
					t.Errorf("--port-header exited %d: %s", got.code, got.stderr)
				}
			}
		})
	}
}

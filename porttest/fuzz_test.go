package porttest

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
)

// fuzzLexSeeds is the directory of FuzzLex's committed inputs, which
// FuzzLexDiff starts from beside the corpus.
var fuzzLexSeeds = filepath.Join("lexer", "testdata", "fuzz", "FuzzLex")

// fuzzFile is the name, in each input's own temporary directory, that
// FuzzLexDiff writes the input to and gives both compilers.
const fuzzFile = "fuzz.tck"

// FuzzLexDiff runs the port and the Go compiler with --lex on each input,
// written to a file of its own, and compares them as the lex-single stage
// does: standard output, standard error and the exit status, byte for
// byte, a not implemented answer skipping unless lex-single is required.
// Before that it lexes the input with this module's lexer, in process,
// so the fuzz engine's coverage, which cannot see into the compilers'
// processes, has the Go lexer's to guide it.
//
// It is seeded with FuzzLex's committed inputs, and, when it fuzzes,
// with every single file of the corpus too. Run as a test, without
// -fuzz, it checks the committed inputs alone: TestPort's lex-single
// already runs the corpus, so make port-test does not run it twice.
//
// It runs only as TestPort does, with TACK_PORT_TEST=1 and -count, and
// skips otherwise; the binaries are the ones TestMain gets, built once
// per process, so a run that should build them once in all passes
// TACK_GO and TACK_PORT_BIN, as make port-fuzz does.
func FuzzLexDiff(f *testing.F) {
	ready(f)
	seeds, err := readFuzzSeeds(filepath.Join(env.root, fuzzLexSeeds))
	if err != nil {
		f.Fatal(err)
	}
	for _, s := range seeds {
		f.Add(s)
	}
	fuzzDiff(f, "lex-single", goLex)
}

// FuzzParseDiff runs the port and the Go compiler with --ast on each
// input, written to a file of its own, and compares them as the
// ast-single stage does: standard error and the exit status byte for
// byte, and standard output as decoded JSON, a not implemented answer
// skipping unless ast-single is required. Before that it lexes and
// parses the input with this module's lexer and parser, in process, as
// the loader does for --ast, so the fuzz engine's coverage has the Go
// parser's to guide it.
//
// It is seeded with its committed inputs, testdata/fuzz/FuzzParseDiff,
// which go test reads itself, and, when it fuzzes, with every single file
// of the corpus too; run as a test it checks the committed inputs alone,
// since TestPort's ast-single runs the corpus already. It runs as
// FuzzLexDiff does.
func FuzzParseDiff(f *testing.F) {
	ready(f)
	fuzzDiff(f, "ast-single", goParse)
}

// FuzzResDiff runs the port and the Go compiler with --res on each input,
// written to a file of its own, and compares them as the res-single
// stage does, a not implemented answer skipping unless res-single is
// required. Before that it lexes, parses and resolves the input in
// process, as a single file with no imports loaded, so the fuzz engine's
// coverage has the Go resolver's to guide it; an import of std is
// resolved by the compilers alone.
//
// It is seeded with its committed inputs, testdata/fuzz/FuzzResDiff, and,
// when it fuzzes, with every single file of the corpus too; run as a test
// it checks the committed inputs alone, since TestPort's res-single runs
// the corpus already. It runs as FuzzLexDiff does.
func FuzzResDiff(f *testing.F) {
	ready(f)
	fuzzDiff(f, "res-single", goResolve)
}

// fuzzDiff runs the fuzz test f, FuzzLexDiff, FuzzParseDiff or
// FuzzResDiff, whose
// committed seeds its caller has added: it adds every single file of the
// corpus too when fuzzing, and runs each input through inProcess, for
// the fuzz engine's coverage, and then through both compilers, compared
// as the stage named stage compares them.
func fuzzDiff(f *testing.F, stage string, inProcess func([]byte)) {
	st := stageNamed(stage)
	if st == nil {
		f.Fatalf("no %s stage", stage)
	}
	ins, err := singleFiles()
	if err != nil {
		f.Fatal(err)
	}
	if !fuzzing() {
		ins = nil
	}
	for _, in := range ins {
		src, err := os.ReadFile(filepath.Join(in.dir, filepath.FromSlash(in.file)))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		inProcess(src)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, fuzzFile), src, 0o600); err != nil {
			t.Fatal(err)
		}
		runCase(t, *st, input{name: fuzzFile, dir: dir, file: fuzzFile})
	})
}

// fuzzing reports whether go test was asked to fuzz, -fuzz given a
// pattern, as it passes it on, -test.fuzz.
func fuzzing() bool {
	f := flag.Lookup("test.fuzz")
	return f != nil && f.Value.String() != ""
}

// goLex lexes src with this module's lexer, as FuzzLex does, and drops
// the result: it is there for the coverage it gives the fuzz engine. The
// lexer's own invariants are FuzzLex's to check.
func goLex(src []byte) {
	f := &source.File{ID: fuzzFile, Filename: fuzzFile, Data: src}
	lexer.New(f).Lex()
}

// goParseMaxErrors is how many errors goParse's parse goes past, the
// driver's default --max-errors, as the loader's parse for --ast does.
const goParseMaxErrors = 10

// goParse lexes src with this module's lexer and, when it lexes, parses
// it with this module's parser, as the loader does for --ast, and drops
// the result: it is there for the coverage it gives the fuzz engine. The
// parser's own invariants are its fuzz tests' to check.
func goParse(src []byte) {
	f := &source.File{ID: fuzzFile, Filename: fuzzFile, Data: src}
	lex := lexer.New(f)
	if len(lex.Lex()) > 0 {
		return
	}
	p := parser.NewParser(f.ID, lex.Tokens())
	p.Limit(goParseMaxErrors)
	p.Parse()
}

// goResolve parses src as goParse does and, when it parses to a tree,
// resolves it with this module's resolver as a program of that one file,
// dropping the result: it is there for the coverage it gives the fuzz
// engine.
func goResolve(src []byte) {
	f := &source.File{ID: fuzzFile, Filename: fuzzFile, Data: src}
	lex := lexer.New(f)
	if len(lex.Lex()) > 0 {
		return
	}
	p := parser.NewParser(f.ID, lex.Tokens())
	p.Limit(goParseMaxErrors)
	tree, _ := p.Parse()
	if tree == nil {
		return
	}
	resolver.New(ast.SingleFile(tree), builtin.Default()).Resolve()
}

// readFuzzSeeds returns the inputs of every file in dir, each a go test
// fuzz v1 file holding one string or []byte value, as FuzzLex's are. A
// file of any other shape is an error, so a seed is never dropped
// unseen.
func readFuzzSeeds(dir string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading the fuzz seeds: %w", err)
	}
	var seeds [][]byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p) //nolint:gosec // G304: a fixed directory of this module
		if err != nil {
			return nil, fmt.Errorf("reading a fuzz seed: %w", err)
		}
		seed, err := parseFuzzSeed(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		seeds = append(seeds, seed)
	}
	return seeds, nil
}

// parseFuzzSeed returns the one value of data, a go test fuzz v1 file:
// its header line, then string("...") or []byte("..."), quoted as Go
// quotes a string.
func parseFuzzSeed(data []byte) ([]byte, error) {
	lines := strings.Split(strings.TrimSpace(string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))), "\n")
	if len(lines) != 2 || lines[0] != "go test fuzz v1" {
		return nil, fmt.Errorf("not a go test fuzz v1 file of one value")
	}
	v := lines[1]
	for _, prefix := range []string{"string(", "[]byte("} {
		if strings.HasPrefix(v, prefix) && strings.HasSuffix(v, ")") {
			s, err := strconv.Unquote(v[len(prefix) : len(v)-1])
			if err != nil {
				return nil, fmt.Errorf("unquoting the value: %w", err)
			}
			return []byte(s), nil
		}
	}
	return nil, fmt.Errorf("the value is neither a string nor a []byte: %s", v)
}

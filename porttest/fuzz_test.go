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

	"github.com/tacklang/tack/lexer"
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
	st := stageNamed("lex-single")
	if st == nil {
		f.Fatal("no lex-single stage")
	}
	seeds, err := readFuzzSeeds(filepath.Join(env.root, fuzzLexSeeds))
	if err != nil {
		f.Fatal(err)
	}
	for _, s := range seeds {
		f.Add(s)
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
		goLex(src)
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

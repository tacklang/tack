package porttest

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/progen"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// emitFuzzSeeds is how many seeds FuzzEmitDiff starts from, each the
// bytes progen's own fuzz targets are seeded with for that seed.
const emitFuzzSeeds = 8

// FuzzEmitDiff generates a program from each input with progen, which
// reads its choices from the input's bytes, a valid, terminating program
// the backend lowers, writes it to main.tck in a temporary directory of
// its own and runs both compilers on it with --emit, compared as
// emit-single compares its cases, then with --check, compared as
// check-clean compares its cases, and then with --emit-header, each
// compiler in a fresh copy of the directory, compared as emit-header
// compares its cases, the tack.h written included: every program
// declares extern funcs, so each writes one. A not implemented answer
// skips, the runs after it unmade, unless the stage is required or every
// emit stage the program needs is. Before that it puts the program
// through this module's front end and C backend, in process, as progen's
// FuzzGeneratedFrontEnd does, so the fuzz engine's coverage, which cannot
// see into the compilers' processes, has the Go backend's to guide it.
// No program progen writes reaches a known divergence (see
// testdata/divergences.txtar), and an input of no suite never takes an
// entry, so each is compared with Go's output; a program holding a rune
// whose class Go's unicode package and the port's std/unicode differ on
// skips first (see skipUnicodeDrift). A program's
// classification is never cached, so fuzzing for hours cannot fill the
// cache.
//
// It is seeded with progen's seeds, each a byte string drawn from
// progen.NewSeeded for seeds 0 to emitFuzzSeeds-1, of 64 bytes and 48
// more for each seed, as FuzzGenerated and FuzzGeneratedFrontEnd are; run
// as a test it checks them alone. It runs as FuzzLexDiff does.
func FuzzEmitDiff(f *testing.F) {
	ready(f)
	sts := make([]*stage, 0, len(fuzzEmitStages))
	for _, name := range fuzzEmitStages {
		st := stageNamed(name)
		if st == nil {
			f.Fatalf("no %s stage", name)
		}
		sts = append(sts, st)
	}
	for seed := range uint64(emitFuzzSeeds) {
		f.Add(progenSeed(seed, 64+int(seed)*48))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := progen.Generate(progen.NewBytes(data))
		skipUnicodeDrift(t, []byte(p.Source))
		goBackend(p.Source)
		dir := t.TempDir()
		if err := os.WriteFile(
			filepath.Join(dir, "main.tck"),
			[]byte(p.Source),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		in := input{name: "main.tck", dir: dir, file: "main.tck", uncached: true}
		for _, st := range sts {
			if st.run != nil {
				st.run(t, *st, in, env.portRun)
			} else {
				runCase(t, *st, in)
			}
		}
	})
}

// fuzzEmitStages are the stages, by name, whose comparison FuzzEmitDiff
// makes of each program, in the order it makes them: --emit, --check and
// --emit-header.
var fuzzEmitStages = []string{emitSingleStage, cleanStage, emitHeaderStage}

// progenSeed returns n bytes drawn from progen.NewSeeded(seed), as
// progen's seedBytes does.
func progenSeed(seed uint64, n int) []byte {
	src := progen.NewSeeded(seed)
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(src.Intn(256))
	}
	return b
}

// goBackend loads src as main.tck, with the test std, resolves, checks
// and lowers it with this module's C backend, and drops the result: it
// is there for the coverage it gives the fuzz engine. A program a stage
// rejects stops there; whether the generator's programs are valid is
// progen's to check.
func goBackend(src string) {
	fsys := fstest.MapFS{"main.tck": {Data: []byte(src)}}
	l := &loader.Loader{
		FS:       loader.FromFS(fsys),
		Builtins: stdtest.MustBuiltins(),
		Std:      inProcessStd(),
	}
	res, err := l.File("main.tck")
	if err != nil || len(res.Diags) > 0 {
		return
	}
	if d := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); diag.HasErrors(d) {
		return
	}
	if d := typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check(); diag.HasErrors(d) {
		return
	}
	if _, d := c.New().Generate(res.Program, stdtest.MustBuiltins()); len(d) > 0 {
		return
	}
	c.New().Header(res.Program)
}

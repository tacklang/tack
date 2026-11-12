package progen_test

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/progen"
)

// TestShowProgram prints the program of seed 1, for reading.
func TestShowProgram(t *testing.T) {
	p := progen.Generate(progen.NewSeeded(1))
	t.Log("\n" + p.Source)
	out, ok := p.Expect(200000)
	t.Log(out, ok)
}

// rejectionSeeds is how many seeds TestRejectionRate puts through the
// front end.
const rejectionSeeds = 6000

// TestRejectionRate puts many generated programs through the front end
// alone, which is quick, and fails when it rejects more than one in a
// hundred: a rejected program is a bug in the generator, or a finding,
// and each rejection is logged with its program.
func TestRejectionRate(t *testing.T) {
	rejected, shown := 0, 0
	for seed := range uint64(rejectionSeeds) {
		p := progen.Generate(progen.NewSeeded(seed))
		_, _, _, msg := frontEnd(p.Source)
		if msg == "" {
			continue
		}
		rejected++
		if shown < 12 {
			shown++
			t.Logf("seed %d rejected:\n%s", seed, msg)
		}
	}
	t.Logf("%d of %d rejected", rejected, rejectionSeeds)
	if rejected*100 > rejectionSeeds {
		t.Errorf("%d of %d generated programs rejected", rejected, rejectionSeeds)
	}
}

// numbered returns src with line numbers, for a log.
func numbered(src string) string {
	var b strings.Builder
	for i, l := range strings.Split(src, "\n") {
		fmt.Fprintf(&b, "%3d  %s\n", i+1, l)
	}
	return b.String()
}

// generatedSeeds is how many fixed seeds TestGeneratedPrograms builds
// and runs; FuzzGenerated goes further.
const generatedSeeds = 16

// TestGeneratedPrograms puts the programs of fixed seeds through every
// oracle (see check): generatedSeeds of them, or as many as the
// environment variable PROGEN_SEEDS says, starting at PROGEN_FIRST.
func TestGeneratedPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs programs with clang")
	}
	skipWithoutClang(t)
	first, n := envUint("PROGEN_FIRST", 0), envUint("PROGEN_SEEDS", generatedSeeds)
	for seed := first; seed < first+n; seed++ {
		p, o := check(t, progen.NewSeeded(seed))
		report(t, fmt.Sprintf("seed %d", seed), p, o)
	}
}

// report fails t for what checking p, named what, found: a rejection or
// an oracle's failure. A program too slow to run is not a failure.
func report(t *testing.T, what string, p *progen.Program, o outcome) {
	t.Helper()
	switch {
	case o.rejected != "":
		t.Errorf("%s: the compiler rejected a generated program:\n%s\n%s",
			what, o.rejected, numbered(p.Source))
	case o.failure != "":
		t.Errorf("%s: %s\n%s", what, o.failure, numbered(p.Source))
	}
}

// envUint returns the environment variable name as a number, or def when
// it is unset or not one.
func envUint(name string, def uint64) uint64 {
	n, err := strconv.ParseUint(os.Getenv(name), 10, 64)
	if err != nil {
		return def
	}
	return n
}

// skipWithoutClang skips t when clang cannot be run.
func skipWithoutClang(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
}

// TestCoverage logs, over many seeds, how big the generated programs are
// and how often each kind of statement appears, so a gap in what the
// generator writes shows.
func TestCoverage(t *testing.T) {
	const seeds = 1000
	totals := map[string]int{}
	lines, funcs := 0, 0
	for seed := range uint64(seeds) {
		p := progen.Generate(progen.NewSeeded(seed))
		lines += strings.Count(p.Source, "\n")
		funcs += p.Funcs
		for k, n := range p.Stats {
			totals[k] += n
		}
	}
	t.Logf("over %d programs: %d lines, %d functions each on average", seeds, lines/seeds,
		funcs/seeds)
	for k, n := range totals {
		t.Logf("%-16s %6d", k, n)
	}
}

// TestFeatureCoverage logs, over many seeds, how many programs use each
// feature that is no statement kind of its own, by the text that shows it.
func TestFeatureCoverage(t *testing.T) {
	const seeds = 1000
	marks := map[string]*regexp.Regexp{}
	for name, re := range map[string]string{
		"method": `\) m[0-9]+\(`, "mut receiver": `func \(mut r`, "own param": `own p[0-9]+ `,
		"own arg": `\(own |, own `, "view param": `p[0-9]+ \[\]int64`, "func param": ` func\(`,
		"func value var": `mut fv`, "error result": `, error\) \{`, "error check": `\.code != 0 \{`,
		"defer": `defer \{`, "generic rev": `rev\(mut`, "sort.by": `sort\.by\(`,
		"is_sorted": `sort\.is_sorted\(`, "bytes": `bytes\.`, "union switch": `switch `,
		"map put": `put\(mut`, "swap": `swap\(mut`, "drop": `drop\(own`, "arena": `add_all\(mut`, "pool": `insert\(mut`, "region": `region r[0-9]+ \{`, "parse_int": `conv\.parse_int\(`,
		"text": `text\.from_bytes\(`, "errors.Failure": `errors\.from_bytes\(`,
		"iface generic": `\]\(x T\)`, "array slice": `[a-z][0-9]+\[[0-9]+:[0-9]+\]`,
		"string order": `[a-z][0-9]+ (<|<=|>|>=) "`, "has proof": `if has\(`,
		"index proof": `0 <= [a-z][0-9]+ && `, "unreachable": `\n *unreachable\n`,
		"question": `\)\?`, "question value": `\)\?[ )]`, "fallthrough": `\n *fallthrough\n`,
		"method expr": `S[0-9]+\.m[0-9]+[^(0-9]`, "method expr call": `S[0-9]+\.m[0-9]+\(`,
	} {
		marks[name] = regexp.MustCompile(re)
	}
	counts := map[string]int{}
	for seed := range uint64(seeds) {
		src := progen.Generate(progen.NewSeeded(seed)).Source
		for name, re := range marks {
			if re.MatchString(src) {
				counts[name]++
			}
		}
	}
	for name := range marks {
		t.Logf("%-16s in %4d of %d programs", name, counts[name], seeds)
	}
}

// FuzzGenerated generates a program from the fuzz engine's bytes, each
// byte a choice the generator makes (see progen.Bytes), so a mutation of
// the input is a change to the program's structure, and puts it through
// every oracle (see check). A rejected program fails too: it is a
// generator bug, or a compiler that rejects a valid program.
func FuzzGenerated(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seedBytes(seed, 64+int(seed)*48))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		skipWithoutClang(t)
		p, o := check(t, progen.NewBytes(data))
		switch {
		case o.rejected != "":
			t.Fatalf("the compiler rejected a generated program:\n%s\n%s", o.rejected,
				numbered(p.Source))
		case o.failure != "":
			t.Fatalf("%s\n%s", o.failure, numbered(p.Source))
		}
	})
}

// FuzzGeneratedFrontEnd is FuzzGenerated without clang: it generates a
// program from the fuzz engine's bytes as FuzzGenerated does and puts it
// through the front end and the C backend alone, the C never compiled
// and no binary run, so it gets through many more programs in a given
// time (see frontEndOnly). It needs no clang, and can run for hours.
func FuzzGeneratedFrontEnd(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seedBytes(seed, 64+int(seed)*48))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := progen.Generate(progen.NewBytes(data))
		if msg := frontEndOnly(p); msg != "" {
			t.Fatalf("%s\n%s", msg, numbered(p.Source))
		}
	})
}

// frontEndOnly puts p through the oracles that need no C compiler, and
// returns what went wrong, or "": the front end accepts it, valid by
// construction, and the C backend generates its C and its header, with
// no stage panicking; and the interpreter's run of it does not panic and
// gives the same checksum, or runs past its budget, each time it is run.
// With no binary to compare it with, the checksum itself is not checked:
// a run that differs from the one before shows state the interpreter
// keeps between runs, which would make FuzzGenerated's comparison fail
// for no fault of the compiler's.
func frontEndOnly(p *progen.Program) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	_, csrc, header, rejected := frontEnd(p.Source)
	switch {
	case rejected != "":
		return "the compiler rejected a generated program:\n" + rejected
	case len(csrc) == 0 || len(header) == 0:
		return "the C backend generated no C or no header"
	}
	want, ok := p.Expect(stepBudget)
	if again, okAgain := p.Expect(stepBudget); again != want || okAgain != ok {
		return fmt.Sprintf("the interpreter gave %q (within budget: %v), then %q (%v)",
			want, ok, again, okAgain)
	}
	return ""
}

// seedBytes returns n pseudo-random bytes from seed, a starting input for
// FuzzGenerated and FuzzGeneratedFrontEnd.
func seedBytes(seed uint64, n int) []byte {
	src := progen.NewSeeded(seed)
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(src.Intn(256))
	}
	return b
}

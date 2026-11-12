package progen_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/progen"
)

// TestDump writes the program of PROGEN_DUMP's seed to PROGEN_OUT.
func TestDump(t *testing.T) {
	out := os.Getenv("PROGEN_OUT")
	if out == "" {
		t.Skip("PROGEN_OUT not set")
	}
	p := progen.Generate(progen.NewSeeded(envUint("PROGEN_DUMP", 0)))
	if err := os.WriteFile(out, []byte(p.Source), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestTrace builds the program of PROGEN_TRACE's seed changed to print
// every value it observes, runs it, and reports the first value that
// differs from the interpreter's, with the program, for diagnosing a
// checksum that does not match.
func TestTrace(t *testing.T) {
	seed, set := os.LookupEnv("PROGEN_TRACE")
	if !set {
		t.Skip("PROGEN_TRACE not set")
	}
	n, _ := strconv.ParseUint(seed, 10, 64)
	p := progen.Generate(progen.NewSeeded(n))
	src, want := p.Trace(stepBudget)
	prog, csrc, header, rejected := frontEnd(src)
	if rejected != "" {
		t.Fatal(rejected)
	}
	bin, csrcPath, err := build(t.TempDir(), "-O0", prog, csrc, header)
	if err != nil {
		t.Fatal(err)
	}
	out, _, _, err := runBin(bin, csrcPath, "")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(out), "\n")
	for i, w := range want {
		if i >= len(got) || got[i] != strconv.FormatInt(w, 10) {
			t.Fatalf("observation %d: program %v, interpreter %d (of %d)\nprogram:     %v\n"+
				"interpreter: %v\n%s", i, safeIndex(got, i), w, len(want),
				got[:min(len(got), i+4)], want[:min(len(want), i+4)], numbered(src))
		}
	}
	t.Logf("all %d observations agree", len(want))
}

// safeIndex returns s[i], or "none" past its end.
func safeIndex(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "none"
}

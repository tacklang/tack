package porttest

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// malformedVar is the environment variable that asks for TestMalformed:
// how many accepted programs it breaks, a whole number from 1 up. Unset
// or empty, TestMalformed skips.
const malformedVar = "PORTTEST_MALFORMED"

// The variants TestMalformed makes of each program: malformedCuts
// truncations, at each sixth of its length, and malformedMutations byte
// mutations.
const (
	malformedCuts      = 5
	malformedMutations = 8
)

// malformedBytes are the bytes a mutation writes in place of one: the
// bytes the lexer and the parser treat apart, a 0 byte, bytes that are
// no UTF-8, and a letter and a digit.
var malformedBytes = []byte("\x00\xff\x80{}()[]\"'`\n;:=.,-+*/<>!&|?0x9_aZ ")

// malformedStage is how TestMalformed compares the two compilers: as
// emit-single compares its cases, every stream byte for byte. It is no
// stage of TestPort, so no divergence applies to its inputs.
var malformedStage = stage{name: "malformed", stdout: compareBytes}

// TestMalformed breaks programs the Go compiler accepts and checks that
// the port answers each break as Go does: $PORTTEST_MALFORMED of the
// .tck files under examples/ whose --emit Go passes with exit status 0,
// spread evenly over them, each cut short at each sixth of its length
// and mutated malformedMutations times, a byte replaced by one of
// malformedBytes, a byte deleted, a run of up to 40 bytes repeated or
// deleted, chosen by a generator seeded with the file's name, so a run
// is the same each time. Each variant is written alone in a directory of
// its own and run with --check --max-errors 0 and with --emit, both
// compilers compared byte for byte as emit-single compares its cases, but
// for the --emit of a variant whose package is not main, which
// malformedWant compares with Go's --res (see the package doc, Known
// divergences). A variant the port declines skips. A crash, a
// sanitizer's report, a hang past compilerTimeout or other output than
// Go's fails; when Go's is wrong, the variant is a Go bug the port must
// not copy, to be raised for a decision. make port-sanitize runs it with
// the port built with the sanitizers when the variable is set. It runs
// only as TestPort does.
func TestMalformed(t *testing.T) {
	ready(t)
	v := os.Getenv(malformedVar)
	if v == "" {
		t.Skip("the malformed input sweep runs only with " + malformedVar +
			" set to the programs it breaks")
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("%s is %q, not a whole number from 1 up", malformedVar, v)
	}
	progs, err := acceptedPrograms(t, n)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, prog := range progs {
		src, err := os.ReadFile(
			filepath.Join(env.root, filepath.FromSlash(prog)),
		) //nolint:gosec // G304: a file of the corpus
		if err != nil {
			t.Fatal(err)
		}
		for i, variant := range malformedVariants(prog, src) {
			sub := filepath.Join(dir, fmt.Sprintf("%x-%d", fnvHash(prog), i))
			in, err := writeVariant(
				sub,
				filepath.Base(prog),
				fmt.Sprintf("%s/%d", prog, i),
				variant,
			)
			if err != nil {
				t.Fatal(err)
			}
			runMalformed(t, in)
		}
	}
}

// writeVariant writes variant as file in the directory sub, which it
// makes, and returns the input, named name, that runs it.
func writeVariant(sub, file, name string, variant []byte) (input, error) {
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return input{}, fmt.Errorf("making a variant's directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(sub, file), variant, 0o600); err != nil {
		return input{}, fmt.Errorf("writing a variant: %w", err)
	}
	return input{name: name, dir: sub, file: file}, nil
}

// runMalformed runs in, a variant of TestMalformed, with --check
// --max-errors 0 and with --emit, each in a parallel subtest, comparing
// the port with what malformedWant says Go gives.
func runMalformed(t *testing.T, in input) {
	for _, args := range [][]string{{"--check", "--max-errors", "0", in.file}, {"--emit", in.file}} {
		t.Run(in.name+"/"+args[0], func(t *testing.T) {
			t.Parallel()
			port := runCompiler(t, env.portRun, env.portCache, in, args)
			if port.notImplemented() {
				t.Skip(string(port.stderr))
			}
			want, viaRes := malformedWant(t, in, args)
			if viaRes {
				t.Log("no main package: compared with Go's --res")
			}
			compareResults(t, malformedStage, in, args, want, port)
		})
	}
}

// noMainPackage matches the resolver's diagnostic for a program whose
// package is not main, as a rendered line.
var noMainPackage = regexp.MustCompile(`(?m)^[^\n]*:\d+:\d+: error: main package not found$`)

// malformedWant returns what the port must give for in with args, and
// whether it is Go's --res rather than Go's run with args. It is Go's
// run, but for --emit of a program the resolver finds no main package
// in, which Go's --res shows: there Go's driver checks the program the
// resolver bound nothing of on and reports errors it does not have, and
// the port stops at the resolver's "main package not found", a decided
// divergence (see the package doc, Known divergences), so the port must
// give what --res gives, the loader's and the resolver's diagnostics and
// their exit status, nothing written to standard output.
func malformedWant(t *testing.T, in input, args []string) (result, bool) {
	t.Helper()
	if args[0] == "--emit" {
		res := runCompiler(t, env.goBin, env.goCache, in, []string{"--res", in.file})
		if noMainPackage.Match(res.stderr) {
			return res, true
		}
	}
	return runCompiler(t, env.goBin, env.goCache, in, args), false
}

// TestMalformedWant checks both ways malformedWant takes, on a program
// whose package is not main, where Go's --emit reports errors its --res
// does not and the port's --emit gives what --res gives, and on one in
// package main with an error, where Go's --emit is what the port must
// give. It runs only as TestPort does.
func TestMalformedWant(t *testing.T) {
	ready(t)
	cases := []struct {
		name, src string
		viaRes    bool
	}{
		{"no-main", "package Zain\n\nfunc f(n string) {\n    print(n)\n}\n", true},
		{"main", "package main\n\nfunc main() {\n    print(1)\n}\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, err := writeVariant(t.TempDir(), "main.tck", c.name, []byte(c.src))
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--emit", in.file}
			want, viaRes := malformedWant(t, in, args)
			if viaRes != c.viaRes {
				t.Fatalf("malformedWant took Go's --res: %v, want %v", viaRes, c.viaRes)
			}
			emit := runCompiler(t, env.goBin, env.goCache, in, args)
			if differs := !bytes.Equal(emit.stderr, want.stderr); differs != c.viaRes {
				t.Fatalf("Go's --emit and the output wanted differ: %v, want %v", differs, c.viaRes)
			}
			port := runCompiler(t, env.portRun, env.portCache, in, args)
			compareResults(t, malformedStage, in, args, want, port)
		})
	}
}

// acceptedPrograms returns n of the .tck files under examples/, named
// from the module's root, whose --emit the Go compiler passes with exit
// status 0, spread evenly over them, or all of them when there are no
// more. A mutation may still rename a program's package, which
// malformedWant allows for.
func acceptedPrograms(t *testing.T, n int) ([]string, error) {
	ins, err := emitSingleInputs()
	if err != nil {
		return nil, err
	}
	var accepted []string
	for _, in := range ins {
		if r := runCompiler(
			t,
			env.goBin,
			env.goCache,
			in,
			[]string{"--emit", in.file},
		); r.code == 0 {
			accepted = append(accepted, in.file)
		}
	}
	step := max(len(accepted)/n, 1)
	var out []string
	for i := 0; i < len(accepted) && len(out) < n; i += step {
		out = append(out, accepted[i])
	}
	return out, nil
}

// malformedVariants returns the variants of src, the file prog, that
// TestMalformed runs: its truncations, then its mutations, drawn from a
// generator seeded with prog.
func malformedVariants(prog string, src []byte) [][]byte {
	var out [][]byte
	for k := 1; k <= malformedCuts; k++ {
		out = append(out, src[:len(src)*k/(malformedCuts+1)])
	}
	if len(src) == 0 {
		return out
	}
	rnd := rand.New(
		rand.NewPCG(fnvHash(prog), 0),
	) //nolint:gosec // G404: a reproducible choice, not a secret
	for range malformedMutations {
		b := append([]byte(nil), src...)
		i := rnd.IntN(len(b))
		j := min(len(b), i+1+rnd.IntN(40))
		switch rnd.IntN(4) {
		case 0:
			b[i] = malformedBytes[rnd.IntN(len(malformedBytes))]
		case 1:
			b = append(b[:i], b[i+1:]...)
		case 2:
			b = append(b[:j], append(append([]byte(nil), b[i:j]...), b[j:]...)...)
		default:
			b = append(b[:i], b[j:]...)
		}
		out = append(out, b)
	}
	return out
}

// fnvHash returns the 64-bit FNV-1a hash of s.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s)) //nolint:errcheck // a hash's Write never fails
	return h.Sum64()
}

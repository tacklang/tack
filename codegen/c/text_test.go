package c_test

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hashInputs returns the inputs TestTextHashMatchesFNV1a hashes: no
// bytes, each byte value alone, every byte value ascending and
// descending, and 200 strings of random bytes, of random lengths up to
// 64, from a fixed seed, so a failure repeats.
func hashInputs() [][]byte {
	inputs := make([][]byte, 0, 1+256+2+200)
	inputs = append(inputs, []byte{})
	for c := range 256 {
		inputs = append(inputs, []byte{byte(c)})
	}
	up, down := make([]byte, 256), make([]byte, 256)
	for i := range 256 {
		up[i], down[i] = byte(i), byte(255-i)
	}
	inputs = append(inputs, up, down)
	r := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		b := make([]byte, r.IntN(65))
		for i := range b {
			b[i] = byte(r.IntN(256))
		}
		inputs = append(inputs, b)
	}
	return inputs
}

// tackBytes returns a Tack string literal holding exactly the bytes of b,
// each written as a \x escape.
func tackBytes(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		fmt.Fprintf(&s, "\\x%02x", c)
	}
	s.WriteByte('"')
	return s.String()
}

// TestTextHashMatchesFNV1a builds a program that hashes each of
// hashInputs with std/text's hash, which computes FNV-1a's XOR with
// arithmetic since Tack has no bitwise operators, and checks every hash
// equals the one Go's hash/fnv gives, an implementation written apart
// from Tack's.
func TestTextHashMatchesFNV1a(t *testing.T) {
	inputs := hashInputs()
	var src strings.Builder
	src.WriteString(`package main

import fmt from "std/fmt"
import text from "std/text"

func hash_of(s []uint8) uint64 {
    t, err := text.from_bytes(s, c_allocator)
    if err.code != 0 {
        print("from_bytes failed\n")
        return 0
    }
    return t.hash()
}

func main() {
`)
	var want strings.Builder
	for _, in := range inputs {
		fmt.Fprintf(&src, "    fmt.printf(\"%%d\\n\", hash_of([]uint8(%s)))\n", tackBytes(in))
		h := fnv.New64a()
		h.Write(in)
		fmt.Fprintf(&want, "%d\n", h.Sum64())
	}
	src.WriteString("}\n")
	bin := buildWithStd(t, src.String())
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("hash program failed: %v\n%s", err, out)
	}
	got, exp := strings.Split(string(out), "\n"), strings.Split(want.String(), "\n")
	if len(got) != len(exp) {
		t.Fatalf("printed %d lines, want %d:\n%s", len(got), len(exp), out)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Errorf("hash of %x is %s, want %s", inputs[i], got[i], exp[i])
		}
	}
}

// TestTextExampleIsSanitizerClean builds examples/text, its C file and
// std's C with clang's warnings as errors and the address and undefined
// behavior sanitizers on, every finding fatal, and checks it writes
// exactly its output, every check ok: std/text, written in Tack, reads no
// view past its end and leaks no buffer, which the example's own count of
// live buffers checks, through its failed appends and from_bytes too.
func TestTextExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "text")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/text/output expects a check to fail")
	}
	bin := buildWithStdAndC(
		t,
		src,
		csrc,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("text failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	if string(out) != want {
		t.Errorf("text wrote\n%q\nwant\n%q", out, want)
	}
}

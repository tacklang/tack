package progen

import "math/rand/v2"

// Source makes every choice the generator makes. Intn returns a number in
// [0, n) for n > 0. A generator only ever asks, so a Source decides the
// whole program: the same Source gives the same program.
type Source interface {
	Intn(n int) int
}

// Bytes is a Source that reads its choices from a byte string, as a fuzz
// engine's input: each choice takes one byte, two when n is above 256,
// modulo n, so a mutation of one byte changes one choice and what follows
// from it. Once the bytes run out every choice is 0, which the generator
// always makes the simplest option, the one that ends a list or a block,
// so a short input is a small program and every input is a finite one.
type Bytes struct {
	data []byte
	pos  int
}

// NewBytes returns a Bytes reading data.
func NewBytes(data []byte) *Bytes {
	return &Bytes{data: data}
}

// Intn returns the next choice, in [0, n).
func (b *Bytes) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	v := b.next()
	if n > 256 {
		v = v<<8 | b.next()
	}
	return v % n
}

// next returns the next byte as an int, or 0 once there are none.
func (b *Bytes) next() int {
	if b.pos >= len(b.data) {
		return 0
	}
	v := int(b.data[b.pos])
	b.pos++
	return v
}

// Seeded is a Source of pseudo-random choices from a seed, the same for
// the same seed on every run, for go test's fixed programs.
type Seeded struct {
	r *rand.Rand
}

// NewSeeded returns a Seeded drawing from seed.
func NewSeeded(seed uint64) *Seeded {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // not crypto
	return &Seeded{r: r}
}

// Intn returns the next choice, in [0, n).
func (s *Seeded) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	return s.r.IntN(n)
}

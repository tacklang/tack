package compilertest

import (
	"testing"
	"testing/fstest"
)

// TestInterimEveryNameImportable pins the rule until pub and lib exist:
// every package-level name of a package is usable from a package that
// imports it, whatever its case, and so is every field of its structs: a
// lowercase function, type, field and global are all read here, a mutable
// global is read and written, and a struct literal of another package's
// type names every field. When visibility is checked, most of this must
// be rejected: this test is to be replaced then, not updated to pass.
func TestInterimEveryNameImportable(t *testing.T) {
	files := fstest.MapFS{
		"m/tack.mod": {Data: []byte("module m\n")},
		"m/shapes/shapes.tck": {Data: []byte(`package shapes

type point struct {
    x int64
    y int64
}

var origin = point{x: 1, y: 2}

mut count int64 = 0

func twice(n int64) int64 {
    return n * 2
}

func read() int64 {
    return count
}
`)},
		"m/main.tck": {Data: []byte(`package main

import shapes from "m/shapes"

func main() {
    p := shapes.point{x: 3, y: 4}
    shapes.count = shapes.twice(p.x) + shapes.origin.y
    exit(shapes.count + shapes.read())
}
`)},
	}
	if got := run(t, frontEndBackend, program{files: files, target: "m"}); got.stage != "ok" {
		t.Errorf("a program using every kind of name of another package failed:\n%s", got)
	}
}

package compilertest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
)

// maxOrderFiles bounds how many files FuzzFileOrder splits its input into.
const maxOrderFiles = 8

// frontEnd is what loading and resolving one package produced: the stage
// that ended the run, empty if resolution succeeded, and the messages of
// its diagnostics and their notes, sorted, without their positions.
type frontEnd struct {
	stage    string
	messages []string
}

// runFrontEnd loads files as the package in directory p and resolves it.
// A panic is returned as an error rather than crashing the fuzzer, which
// reports it as a failure.
func runFrontEnd(files fstest.MapFS) (out frontEnd, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	res, err := (&loader.Loader{FS: loader.FromFS(files)}).Dir("p")
	if err != nil {
		return frontEnd{stage: "error: " + err.Error()}, nil
	}
	diags, stage := res.Diags, string(res.Stage)
	if len(diags) == 0 {
		diags, stage = resolver.New(res.Program).Resolve(), "resolve"
	}
	if len(diags) == 0 {
		return frontEnd{}, nil
	}
	out.stage = stage
	for _, d := range diags {
		out.messages = append(out.messages, d.Message)
		for _, r := range d.Related {
			out.messages = append(out.messages, r.Message)
		}
	}
	slices.Sort(out.messages)
	return out, nil
}

// orderedFiles returns parts as the files of directory p, named so that
// they sort in the order given, or the reverse of it when reverse is set.
func orderedFiles(parts [][]byte, reverse bool) fstest.MapFS {
	files := fstest.MapFS{}
	for i, part := range parts {
		k := i
		if reverse {
			k = len(parts) - 1 - i
		}
		files[fmt.Sprintf("p/f%d.tck", k)] = &fstest.MapFile{Data: part}
	}
	return files
}

// FuzzFileOrder splits its input at each NUL byte into the files of one
// package, at most maxOrderFiles of them, and loads and resolves that
// package twice, with the files named so that they sort in opposite
// orders. Whether resolution succeeds, the stage that stops it if not, and
// what its diagnostics say, positions aside, must not depend on that
// order: every package-level name of every file is visible in all of them.
// The seeds are the multi-file examples and the multi-file invalid cases.
func FuzzFileOrder(f *testing.F) {
	for _, seed := range orderSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		parts := bytes.Split(data, []byte{0})
		if len(parts) > maxOrderFiles {
			parts = parts[:maxOrderFiles]
		}
		forward, err := runFrontEnd(orderedFiles(parts, false))
		if err != nil {
			t.Fatal(err)
		}
		backward, err := runFrontEnd(orderedFiles(parts, true))
		if err != nil {
			t.Fatal(err)
		}
		if forward.stage != backward.stage || !slices.Equal(forward.messages, backward.messages) {
			t.Errorf("file order changed the result:\nin order: %s %q\nreversed: %s %q",
				forward.stage, forward.messages, backward.stage, backward.messages)
		}
	})
}

// orderSeeds returns the Tack files of each multi-file example and each
// multi-file invalid case, joined by NUL bytes.
func orderSeeds(f *testing.F) [][]byte {
	f.Helper()
	var seeds [][]byte
	paths, _ := filepath.Glob(filepath.Join(examplesDir, "*", "*.tck"))
	byDir := make(map[string][][]byte)
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		byDir[filepath.Dir(p)] = append(byDir[filepath.Dir(p)], src)
	}
	for _, srcs := range byDir {
		if len(srcs) > 1 {
			seeds = append(seeds, bytes.Join(srcs, []byte{0}))
		}
	}
	cases, err := invalid.LoadTree(invalid.TreePath)
	if err != nil {
		f.Fatal(err)
	}
	for _, c := range cases {
		var srcs [][]byte
		for _, file := range c.Files {
			if filepath.Ext(file.Path) == ".tck" {
				srcs = append(srcs, file.Data)
			}
		}
		seeds = append(seeds, bytes.Join(srcs, []byte{0}))
	}
	return seeds
}

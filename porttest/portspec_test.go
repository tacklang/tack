package porttest

import (
	"sort"
	"testing"
)

// portSpecDir is where both trees keep the language specification,
// relative to each tree's root: the port's copy, which the port
// maintains (decided 2026-11-19), and this module's frozen one.
const portSpecDir = "docs/spec"

// portSpecDivergences lists the paths, relative to the port's source
// tree, of the files of its docs/spec allowed to differ from this
// module's frozen copy, each with the reason it may. An entry whose file
// is the same on both sides, or on neither, fails TestPortSpec, so the
// list cannot outlive its reasons.
var portSpecDivergences = map[string]string{
	portSpecDir + "/README.md": "the port's copy is the maintained one (decided 2026-11-19): its " +
		"index names tools/spec and make test-spec as the check of the examples, links only what " +
		"the port's tree holds, and lists the chapters' links to tackc's documents not yet " +
		"resolved there; the frozen copy's names this module's TestSpecExamples and " +
		"TestSpecIndex, and says the port's copy is the maintained one",
}

// TestPortSpec checks the port's copy of the language specification,
// portSpecDir, against this module's frozen copy: the same files, by
// path, both ways, each with the same bytes and the same executable bit,
// but for the paths portSpecDivergences lists, which must differ. The
// port maintains the spec and its checker, tools/spec, which the spec
// stage runs (see runSpec); this module's TestSpecExamples checks the
// frozen copy with the frozen compiler. It needs the port's source tree,
// env.home, which TestMain requires whatever TACK_PORT_BIN says. It runs
// only as TestPort does, with TACK_PORT_TEST=1 and -count.
func TestPortSpec(t *testing.T) {
	ready(t)
	files, err := specFiles(env.home, env.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range comparePortFiles(env.home, env.root, files, portSpecDivergences, "portSpecDivergences") {
		t.Error(msg)
	}
}

// specFiles returns every regular file below portSpecDir in portRoot,
// the port's source tree, or in frozenRoot, this module's root, each
// paired with the same path in the other tree, sorted. A file one tree
// has and the other has not is listed too, and comparing it fails. Any
// entry below either directory that is no regular file or directory, a
// symbolic link among them, is an error.
func specFiles(portRoot, frozenRoot string) ([]portFile, error) {
	paths := map[string]bool{}
	for _, root := range []string{portRoot, frozenRoot} {
		found, err := treeFiles(root, portSpecDir)
		if err != nil {
			return nil, err
		}
		for _, p := range found {
			paths[p] = true
		}
	}
	out := make([]portFile, 0, len(paths))
	for p := range paths {
		out = append(out, portFile{port: p, frozen: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].port < out[j].port })
	return out, nil
}

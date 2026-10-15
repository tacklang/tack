package main

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// listsWant is what examples/lists prints when every check passes.
var listsWant = strings.Join([]string{
	"nothing live at the start", "empty list", "empty view", "empty checked index",
	"empty list freed", "appends succeed", "len after appends", "elements", "one buffer",
	"append and index freed", "len after growth", "values after growth", "total after growth",
	"growth freed", "fill", "element writes", "writes through mut", "append a struct",
	"field of an element", "writes freed", "fill for checked", "checked in range",
	"checked past the end", "checked negative", "checked uint64 index", "checked freed",
	"fill for ranges", "range index and element", "range with no variables",
	"range sees element writes", "ranges freed", "fill for views", "whole list as a view",
	"slice as a view", "open slices", "full slice", "len of a slice",
	"spread into a variadic", "views freed",
	"fill for moves", "moved by declaration", "moved by assignment", "one buffer after moves",
	"returned list", "list beside an error", "three buffers", "moves freed",
	"fill for conditional", "moved on one path", "moved on one path freed",
	"fill for conditional", "not moved freed", "first list", "old list freed on assignment",
	"freed for an empty list", "grown again", "second list", "freed on a binding",
	"assigned to itself", "reassign freed", "list in a struct", "element of a field",
	"field borrowed", "struct moved", "two lists in a struct", "field reassigned",
	"struct returned", "structs freed", "fill for deferred", "defer reads the list",
	"deferred result", "deferred freed", "iteration starts empty", "iteration starts empty",
	"iteration starts empty", "iteration starts empty", "loop freed every iteration",
	"loops freed", "early return", "early return freed", "no early return",
	"no early return freed", "append fails", "list unchanged after a failure",
	"first growth allowed", "a later growth fails", "elements kept after a failure",
	"appends again", "build after failures", "built", "failing freed",
	"lists of other types", "elements of other types", "bytes and errors", "kinds freed",
	"values evaluated once", "values in order", "order freed",
}, " ok\n") + " ok\n"

// externListsWant is what examples/extern-lists prints when every check
// passes.
var externListsWant = "appended by C ok\n" +
	"C's elements read in Tack ok\n" +
	"doubled in C, viewed by C ok\n" +
	"slice viewed by C ok\n" +
	"appended in Tack after C ok\n" +
	"freed after C grew it ok\n" +
	"structs appended by C ok\n" +
	"structs read in Tack ok\n" +
	"structs freed ok\n" +
	"append in C fails ok\n" +
	"nothing after a failure ok\n"

// TestBuiltListsPassesEveryCheck builds the list examples at every
// optimization level and checks each binary passes every check, the
// same at every level: examples/lists, whose own count of c_allocator's
// live buffers shows every list freed after each check, through moves,
// borrows, mut parameters, conditional moves, reassignment, structs
// holding lists, defers, loops, early returns, breaks and continues, and
// appends made to fail; and examples/extern-lists, whose C grows lists
// through std's macros.
func TestBuiltListsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, tc := range []struct{ name, target, want string }{
		{"lists", example("lists"), listsWant},
		{"extern-lists", example("extern-lists"), externListsWant},
	} {
		for _, opt := range codegen.Opts {
			t.Run(tc.name+"/"+string(opt), func(t *testing.T) {
				bin := filepath.Join(t.TempDir(), tc.name)
				r := runTack(t, "-O"+string(opt), "--out", bin, tc.target)
				if r.code != exitOK || r.stderr != "" {
					t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
				}
				if out, code := runBuilt(t, bin); out != tc.want || code != 0 {
					t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, tc.want)
				}
			})
		}
	}
}

// A C file of the program that calls a function std defines, a tack_ name
// only std may give, links the std object defining it, even in a program
// with no list and no builtin call; one that calls none links no std
// object.
func TestStdObjectsLinkedForProgramC(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	main := "package main\n\nextern func live() int64\n\nfunc main() {\n" +
		"    if live() == 0 {\n    }\n}\n"
	for _, tt := range []struct {
		name, c string
		want    []string
	}{
		{
			"calls std",
			"#include <tack/tack.h>\n#include \"tack.h\"\nint64_t live(void) { return tack_c_allocator_live_(); }\n",
			[]string{"c0.o", "std-list.o"},
		},
		{"calls nothing", "#include \"tack.h\"\nint64_t live(void) { return 0; }\n", []string{"c0.o"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeDir(t, map[string]string{"main.tck": main, "live.c": tt.c})
			out := filepath.Join(t.TempDir(), "objs")
			r := runTack(t, "--backend", objsBackend, "--out", out, filepath.Join(dir, "main.tck"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if got := linkedNames(t, out); !slices.Equal(got, tt.want) {
				t.Errorf("linked %q, want %q", got, tt.want)
			}
		})
	}
}

// A program with a list built against a std whose std/builtin defines no
// list functions fails, naming the first one missing and the directory.
func TestListsWithoutADefinition(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	home := writeDir(t, map[string]string{
		"src/std/builtin/builtin.tck": "package builtin\n\n" +
			"extern(\"tack_builtin_a\") func a()\n",
		"src/std/builtin/a.c": "void tack_builtin_a(void) {}\n",
	})
	dir := filepath.Join(home, "src", "std", "builtin")
	r := runTackEnv(t, t.Context(), envAt(t, home), "--out", filepath.Join(t.TempDir(), "bin"),
		program(t, "    l := make(list[int64], c_allocator)\n    if len(l) == 0 {\n    }\n"))
	assertFailure(t, r,
		"lists have no C definition: no C file in "+dir+" defines tack_list_grow_")
}

package main

import (
	"os"
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

// listBulkWant is what examples/list-bulk prints when every check passes.
var listBulkWant = strings.Join([]string{
	"nothing live at the start", "three values", "one buffer for three", "past doubling",
	"an exact fit when doubling is short", "one more doubles", "constants take the element type",
	"bytes appended", "narrower values widen", "widened values", "values freed",
	"spread an array", "spread a slice of an array", "spread a view", "spread a variadic",
	"spread another list", "spread a slice of a list", "slice appended",
	"spread a string's bytes", "spread a literal's bytes", "three buffers", "spreads freed",
	"no values append nothing", "nothing allocated for nothing", "nothing after something",
	"nothing moves no buffer", "nothing freed", "reserving nothing allocates nothing",
	"reserve on an empty list", "appends into reserved room", "no reallocation in between",
	"reserve doubles", "reserve doubles past what it needs",
	"an exact fit when doubling is short, elements kept", "room already is no growth",
	"reserve starts at 4", "reserving freed", "reserve then append grows by doubling",
	"amortized freed", "negative reserve fails", "unchanged by a negative reserve",
	"the smallest int64 is negative too", "negative freed", "reserve int64 max on an empty list",
	"len plus n overflows", "len plus n is int64 max", "bytes past what a buffer holds",
	"unchanged by huge reserves", "huge freed", "values fail together",
	"nothing written by failed values", "nothing written by a failed spread", "failed reserve",
	"what fits needs no allocation", "appends again", "failures on an empty list",
	"failing freed", "values evaluated once, in order", "order freed", "a field appended to",
	"a field's elements", "a moved struct's list appended to", "fields freed",
	"appended in a defer", "deferred result", "deferred freed",
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
	"reserved by C ok\n" +
	"bulk appended by C ok\n" +
	"C's bulk elements read in Tack, in reserved room ok\n" +
	"negative reserve in C fails ok\n" +
	"bulk append in C fails whole ok\n" +
	"reserve in C fails ok\n" +
	"spread in Tack after C ok\n" +
	"bulk freed ok\n" +
	"append in C fails ok\n" +
	"nothing after a failure ok\n"

// TestBuiltListsPassesEveryCheck builds the list examples at every
// optimization level and checks each binary passes every check, the
// same at every level: examples/lists, whose own count of c_allocator's
// live buffers shows every list freed after each check, through moves,
// borrows, mut parameters, conditional moves, reassignment, structs
// holding lists, defers, loops, early returns, breaks and continues, and
// appends made to fail; examples/extern-lists, whose C grows lists
// through std's macros; and examples/list-bulk, whose appends of several
// values and reserves, made to fail too, grow a list once, all or
// nothing, as its C reads the capacity to show.
func TestBuiltListsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, tc := range []struct{ name, target, want string }{
		{"lists", example("lists"), listsWant},
		{"extern-lists", example("extern-lists"), externListsWant},
		{"list-bulk", example("list-bulk"), listBulkWant},
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

// TestBuiltExitFreesWritesItsOutput builds examples/exit-frees at every
// optimization level and checks each binary writes exactly
// examples/exit-frees/output and exits with status 3: exit, called deep
// in a chain of calls with lists of every owned kind live in every frame,
// frees every buffer c_allocator holds before the process ends, which the
// example's C checks in a destructor that runs after exit.
func TestBuiltExitFreesWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "exit-frees", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/exit-frees/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "exit-frees")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("exit-frees"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 3 {
				t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 3", out, code, want)
			}
		})
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

// TestBuiltOwnedListsWritesItsOutput builds examples/owned-lists at every
// optimization level and checks each binary writes exactly
// examples/owned-lists/output, every check ok: lists of lists, of structs
// holding lists and of unions holding lists, nested three deep, read,
// grown, matched and passed in place, elements replaced, popped and
// removed at every edge, and appends made to fail part way, with
// c_allocator's count of live buffers back to zero after each group.
func TestBuiltOwnedListsWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "owned-lists", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/owned-lists/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "owned-lists")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("owned-lists"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
			}
		})
	}
}

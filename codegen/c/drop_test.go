package c_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// dropWant is how many checks examples/drop prints, live.c's check at
// process end included.
const dropWant = 51

// TestDropExampleIsSanitizerClean builds examples/drop under each of
// sanitizerFlags and checks every check it prints passed: drop frees a
// list, a list of lists, a list of maps of lists, a struct, a union, a
// map and an own parameter at once, which c_allocator's count of live
// buffers shows right after each drop, and its block's end, a loop's
// next iteration, a break, a continue, a return, a ?, deferred code and
// exit free none of them again, which the sanitizers check, in branches,
// a switch, a closure, a closure's own parameter and a generic
// function's instances alike.
func TestDropExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "drop", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), dropWant)
		})
	}
}

// TestDropFreesStructFieldsLastFirst checks the C a drop of a struct
// holding lists lowers to: the free its block's end would emit, the last
// field first, then the variable emptied, so the block's end frees
// nothing more.
func TestDropFreesStructFieldsLastFirst(t *testing.T) {
	got := generate(t, "package main\n\ntype Bag struct {\n    a list[int64]\n    b list[int64]\n}\n\n"+
		"func main() {\n"+
		"    bag := Bag{a: make(list[int64], c_allocator), b: make(list[int64], c_allocator)}\n"+
		"    drop(own bag)\n}\n")
	a := strings.Index(got, "tack_list_free_(v_bag.f_a.ptr")
	b := strings.Index(got, "tack_list_free_(v_bag.f_b.ptr")
	empty := -1
	if a >= 0 {
		empty = strings.Index(got[a:], "v_bag = (")
	}
	if a < 0 || b < 0 || empty < 0 || b > a {
		t.Errorf("drop(own bag) does not free b, then a, then empty bag:\n%s", got)
	}
}

// TestDropOfCopyableInstanceIsNothing checks that an instance of a
// generic function whose type argument owns nothing lowers drop(own t)
// to nothing: no free, and t, a copy, left as it is.
func TestDropOfCopyableInstanceIsNothing(t *testing.T) {
	got := generate(t, "package main\n\ntype Any interface {\n}\n\nfunc discard[T Any](own t T) {\n"+
		"    drop(own t)\n}\n\nfunc main() {\n    discard(7)\n}\n")
	if strings.Contains(got, "tack_list_free_") || strings.Contains(got, "p_t = (") {
		t.Errorf("drop of an int64 instance frees or empties something:\n%s", got)
	}
}

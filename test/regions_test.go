package compilertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
)

// regionParseCases are the region cases of examples/invalid.txtar that
// the parser rejects, since region is a keyword; every other region case
// parses and is rejected by the checker.
var regionParseCases = map[string]bool{
	"region-as-func-name": true,
	"region-as-name":      true,
}

// regionNoNoteCases are the region cases whose error is not about a
// value or allocator leaving a region block, so no note names a region,
// and the two an existing rule already rejects, which may keep that
// rule's diagnostic: a closure passed to a parameter that is not scope,
// and a fallthrough that is not a case's last statement; and the two
// where r goes to a make or a call already reported, whose error is the
// only one.
var regionNoNoteCases = map[string]bool{
	"region-allocator-callee-reported":     true,
	"region-allocator-make-reported":       true,
	"region-allocator-closure-plain-param": true,
	"region-fallthrough-inside":            true,
	"region-allocator-extern-scope":        true,
	"region-allocator-scope-param-global":  true,
	"region-allocator-scope-param-return":  true,
	"region-as-func-name":                  true,
	"region-as-name":                       true,
	"region-missing-return":                true,
}

// TestRegionCasesAreRejectedWhereTheyShouldBe checks every region case of
// examples/invalid.txtar for what holds whatever wording its diagnostics
// are given: it is rejected; by the parser for region used as a name, and
// otherwise only by the checker, so the program parsed, region block and
// all; and every escape from a region carries a note, which names the
// region. TestInvalid pins the wording once it is set; this test holds
// before that.
// The note encodes the design's diagnostics rule that every escape case
// gets a note naming the region.
func TestRegionCasesAreRejectedWhereTheyShouldBe(t *testing.T) {
	path := filepath.FromSlash(invalid.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, cases, err := invalid.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	seen := 0
	for _, c := range cases {
		if !strings.HasPrefix(c.Name, "region-") {
			continue
		}
		seen++
		t.Run(c.Name, func(t *testing.T) {
			got := runSource(t, frontEndBackend, c.Name+"/main.tck", c.Source)
			switch {
			case got.stage == "ok":
				t.Fatalf("the program compiled; it must be rejected")
			case regionParseCases[c.Name]:
				if !strings.Contains(got.stage, "parse") {
					t.Errorf("rejected at %s, want the parser to reject region as a name:\n%s", got.stage, got.output)
				}
			case strings.Contains(got.stage, "parse"):
				t.Errorf("rejected at %s, want it to parse and the checker to reject it:\n%s", got.stage, got.output)
			case !strings.Contains(got.stage, "typeck"):
				t.Errorf("rejected at %s, want the checker to reject it:\n%s", got.stage, got.output)
			}
			if !strings.Contains(got.output, ": error: ") {
				t.Errorf("no error reported:\n%s", got.output)
			}
			if !regionNoNoteCases[c.Name] && !strings.Contains(got.output, ": note: ") {
				t.Errorf("no note names the region:\n%s", got.output)
			}
		})
	}
	if seen < 38 {
		t.Errorf("found %d region cases in %s, want at least 38", seen, path)
	}
}

// regionValidPrograms are near misses of the region cases of
// examples/invalid.txtar that must compile: what the escape rule lets
// leave a region, or lets be done inside one.
var regionValidPrograms = []struct {
	name, src string
}{
	{"copyable result assigned outside", `package main

func main() {
    mut n int64 = 0
    region r {
        mut l := make(list[int64], r)
        err := append(mut l, 4)
        if err.code != 0 {
            exit(1)
        }
        n = len(l)
    }
    if n != 1 {
        exit(1)
    }
}
`},
	{"c_allocator list made inside returned", `package main

func f() list[int64] {
    region r {
        tmp := make(list[int64], r)
        mut out := make(list[int64], c_allocator)
        err := append(mut out, len(tmp))
        if err.code != 0 {
            return out
        }
        return out
    }
}

func main() {
    l := f()
    if len(l) != 1 {
        exit(1)
    }
}
`},
	{"outer region value used and grown in an inner one", `package main

func main() {
    region r {
        mut a := make(list[list[int64]], r)
        region s {
            mut b := make(list[int64], s)
            err := append(mut b, 1)
            err2 := append(mut a, make(list[int64], r))
            if err.code != 0 || err2.code != 0 || len(b) != 1 {
                exit(1)
            }
        }
        if len(a) != 1 {
            exit(1)
        }
    }
}
`},
	{"region allocator passed to a scope parameter and a closure", `package main

func fill(mut l list[int64], scope a Allocator) error {
    mut tmp := make(list[int64], a)
    err := append(mut tmp, 1)
    if err.code != 0 {
        return err
    }
    return append(mut l, len(tmp))
}

func each(n int64, scope f func(int64)) {
    for i := range n {
        f(i)
    }
}

func main() {
    region r {
        mut l := make(list[int64], r)
        err := fill(mut l, r)
        mut sum int64 = 0
        each(3, func(i int64) {
            k := make(list[int64], r)
            sum = sum +% i +% len(k)
        })
        if err.code != 0 || sum != 3 {
            exit(1)
        }
    }
}
`},
	{"c_allocator stored in a global and passed to a plain parameter", `package main

mut g Allocator = c_allocator

func take(a Allocator) int64 {
    l := make(list[int64], a)
    return len(l)
}

func main() {
    region r {
        g = c_allocator
        l := make(list[int64], r)
        if take(g) != 0 || len(l) != 0 {
            exit(1)
        }
    }
}
`},
	{"c_allocator passed to a scope Allocator parameter", `package main

func count(scope a Allocator) int64 {
    l := make(list[int64], a)
    return len(l)
}

func main() {
    if count(c_allocator) != 0 {
        exit(1)
    }
}
`},
	{"region list crosses to C as mut and as a view", `package main

extern("c_push") func push(mut l list[int64], x int64) bool
extern("c_sum") func sum(v []int64) int64

func main() {
    region r {
        mut l := make(list[int64], r)
        if !push(mut l, 1) || sum(l) != 1 {
            exit(1)
        }
    }
}
`},
	{"? and return inside a region", `package main

func fail(b bool) error {
    if b {
        return error{code: 2, msg: "fail"}
    }
    return noerror
}

func f(b bool) (int64, error) {
    region r {
        l := make(list[int64], r)
        fail(b)?
        return len(l), noerror
    }
}

func main() {
    n, err := f(false)
    if err.code != 0 || n != 0 {
        exit(1)
    }
}
`},
}

// TestRegionNearMissesCompile checks every program of
// regionValidPrograms gets through the whole pipeline: the escape rule
// rejects what leaves a region, not what only looks like it.
// Assumption: a list made inside a region through c_allocator is not from
// the region, c_allocator may be passed to a scope Allocator parameter,
// and a closure capturing r may write a variable declared inside the
// region block.
func TestRegionNearMissesCompile(t *testing.T) {
	for _, tc := range regionValidPrograms {
		t.Run(tc.name, func(t *testing.T) {
			got := runSource(t, frontEndBackend, "main.tck", []byte(tc.src))
			if got.stage != "ok" {
				t.Errorf("the program did not compile, at the %s stage:\n%s\n%s", got.stage, got.output, tc.src)
			}
		})
	}
}

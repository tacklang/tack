package c_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// regionsWant is how many checks examples/regions prints, the check its
// live.c makes as the process ends among them.
const regionsWant = 47

// leakChecked is the environment every region program runs in: the leak
// sanitizer, part of the address sanitizer, on whatever the caller's
// ASAN_OPTIONS say, so a chunk or a region table no end gave back is a
// finding, as a read after one was given back is.
func leakChecked() []string {
	return append(os.Environ(), "ASAN_OPTIONS=detect_leaks=1")
}

// runRegionChecks runs bin as runChecks does, under leakChecked: it
// exits 0 having printed want checks, each "ok", and none "FAIL".
func runRegionChecks(t *testing.T, bin string, want int) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = leakChecked()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", filepath.Base(bin), err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != want {
		t.Errorf("printed\n%s\nwant %d checks, every one ok", out, want)
	}
}

// runRegionStatus runs bin under leakChecked and returns what it wrote
// to stdout and its exit status, failing the test when it could not be
// run at all.
func runRegionStatus(t *testing.T, bin string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = leakChecked()
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running %s: %v", bin, err)
		return "", 0
	}
}

// TestRegionsExampleIsSanitizerClean builds examples/regions, its C file
// and std's C at -O2, and with the address, undefined behavior and leak
// sanitizers at -O0 and -O1, every finding fatal, and checks every check
// it prints passed: lists, lists of lists and maps made through a region
// and freed in one step at its end, with c_allocator's count of live
// buffers read before, inside and after; nested regions, 200 deep among
// them; a region in a loop of 100000 passes holding no more on the last
// than on the first; break, continue, return and ? out of a region, and
// defers inside and around one; copyable values computed inside one
// leaving it; std functions and closures given the region's allocator as
// scope; a region's lists crossing to C as mut and as a view; c_allocator
// refusing a region's chunks; the region table failing to grow, which
// opens a region with the dead handle; and exit inside nested regions,
// after which nothing is live.
func TestRegionsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "regions", "live.c")
	t.Run("-O2", func(t *testing.T) {
		runRegionChecks(t, buildWithStdAndC(t, src, csrc, "-O2"), regionsWant)
	})
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		runRegionChecks(t, buildWithStdAndC(t, src, csrc, flags...), regionsWant)
	})
}

// regionHooksC is the C half of the programs below: c_allocator's count
// of live buffers and its refusals, and a destructor that reports, as
// the process ends, whether c_allocator still holds any buffer, a
// region's chunk or the region table's own buffer among them.
const regionHooksC = `#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

int64_t region_live(void) {
    return tack_c_allocator_live_();
}

void region_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

__attribute__((destructor)) static void region_check_nothing_live(void) {
    int64_t n = tack_c_allocator_live_();
    if (n == 0) {
        fputs("nothing live at process end ok\n", stdout);
    } else {
        fprintf(stdout, "nothing live at process end FAIL: %lld live\n", (long long)n);
    }
    fflush(stdout);
}
`

// regionHooksPrelude declares regionHooksC's functions and a list maker
// taking its allocator as scope, for the programs of the tests below.
const regionHooksPrelude = `package main

extern("region_live") func live() int64
extern("region_fail_after") func fail_after(n int64)

func fillIn(n int64, scope a Allocator) list[int64] {
    mut l := make(list[int64], a)
    for i := range n {
        err := append(mut l, i +% 1)
        if err.code != 0 {
            return l
        }
    }
    return l
}

`

// regionEndCases are programs that end with regions open, or after they
// have all ended, and the status each must exit with: whichever way the
// process ends, every chunk and the region table are given back exactly
// once, so the destructor of regionHooksC finds nothing live and the
// sanitizers find no double free and no leak.
var regionEndCases = []struct {
	name, body string
	code       int
}{
	{
		// exit runs no region's end: release_all is the one owner of
		// every chunk, each region's and the table's.
		"exit inside regions nested 300 deep",
		`func down(depth int64) {
    region r {
        l := fillIn(depth +% 2, r)
        mut m := make(map[int64]list[int64], r)
        e := put(mut m, depth, fillIn(3, r))
        if e.code != 0 || len(l) != depth +% 2 {
            exit(1)
        }
        if depth == 300 {
            if live() == 0 {
                exit(2)
            }
            exit(3)
        }
        down(depth +% 1)
    }
}

func main() {
    down(0)
}
`, 3,
	},
	{
		// A region opened while c_allocator refuses every request may
		// hold the dead handle, or a table that could not grow: at exit
		// the table's buffer still has exactly one owner.
		// Assumption: opening regions 3000 deep makes the table grow at
		// least once, so some region below is opened while a grow fails.
		"exit with dead regions open",
		`func down(depth int64) {
    if depth == 1500 {
        fail_after(0)
    }
    region r {
        l := fillIn(2, r)
        if depth < 1500 && len(l) != 2 {
            exit(1)
        }
        if depth == 3000 {
            exit(4)
        }
        down(depth +% 1)
    }
}

func main() {
    down(0)
}
`, 4,
	},
	{
		// Returning from main after every region has ended leaves no
		// chunk and no table buffer behind.
		"return from main after regions",
		`func main() {
    for i := range 50 {
        region r {
            l := fillIn(i, r)
            region s {
                k := fillIn(1000, s)
                if len(k) != 1000 || len(l) != i {
                    exit(1)
                }
            }
        }
    }
}
`, 0,
	},
	{
		// A failed table grow followed by a successful one leaves one
		// table, freed once at the end.
		"return from main after a failed table grow",
		`func down(depth int64, mut dead int64) {
    if depth == 2048 {
        return
    }
    fail_after(0)
    region p {
        fail_after(-1)
        mut l := make(list[int64], p)
        e := append(mut l, 1)
        if e.code == 1 {
            dead = dead +% 1
        }
    }
    region r {
        l := fillIn(1, r)
        down(depth +% 1, mut dead)
        if len(l) != 1 {
            exit(1)
        }
    }
}

func main() {
    mut dead int64 = 0
    down(0, mut dead)
    if dead == 0 {
        exit(5)
    }
}
`, 0,
	},
}

// TestRegionEndsGiveEveryChunkBackOnce builds each of regionEndCases
// under the sanitizers at -O0 and -O1 with regionHooksC, and checks each
// exits with its status having printed only the destructor's report that
// nothing is live: exit inside nested regions, with dead regions open,
// and a return from main after regions, after a failed table grow among
// them, each give every chunk and the region table back exactly once.
func TestRegionEndsGiveEveryChunkBackOnce(t *testing.T) {
	const end = "nothing live at process end ok\n"
	for _, tc := range regionEndCases {
		t.Run(tc.name, func(t *testing.T) {
			src := regionHooksPrelude + tc.body
			atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
				out, code := runRegionStatus(t, buildWithStdAndC(t, src, regionHooksC, flags...))
				if out != end || code != tc.code {
					t.Errorf("wrote %q and exited %d, want %q and %d", out, code, end, tc.code)
				}
			})
		})
	}
}

// regionAbortC is the C half of TestRegionFreedAtUnreachable:
// region_abort reaches TACK_UNREACHABLE, as the generated C of an
// unreachable statement does, from a handler for SIGABRT that reports how
// many buffers c_allocator still holds once the abort comes, then lets it
// end the process.
const regionAbortC = `#include <signal.h>
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

int64_t region_live(void) {
    return tack_c_allocator_live_();
}

void region_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

static void report(int sig) {
    printf("live %lld\n", (long long)tack_c_allocator_live_());
    fflush(stdout);
    signal(sig, SIG_DFL);
    raise(sig);
}

void region_abort(void) {
    signal(SIGABRT, report);
    TACK_UNREACHABLE("main.tck:1");
}
`

// regionAbortMain calls region_abort inside three nested regions, each
// holding lists, the innermost one's grown past several chunks.
const regionAbortMain = `extern("region_abort") func abort()

func main() {
    region r {
        a := fillIn(10, r)
        region s {
            b := fillIn(100000, s)
            region t {
                c := fillIn(5, t)
                if len(a) +% len(b) +% len(c) != 100015 || live() == 0 {
                    exit(1)
                }
                print("held\n")
                abort()
            }
        }
    }
}
`

// TestRegionFreedAtUnreachable builds regionAbortMain under the
// sanitizers and checks that reaching TACK_UNREACHABLE with regions open
// gives every chunk and the region table back before the process ends by
// SIGABRT: the abort releases every allocator, as exit does, and a
// region's chunks are c_allocator's buffers.
func TestRegionFreedAtUnreachable(t *testing.T) {
	src := regionHooksPrelude + regionAbortMain
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		bin := buildWithStdAndC(t, src, regionAbortC, flags...)
		cmd := exec.CommandContext(t.Context(), bin)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("ran with %v, want an abort", err)
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGABRT {
			t.Errorf("ended with %v, want SIGABRT", exitErr)
		}
		if want := "held\nlive 0\n"; stdout.String() != want {
			t.Errorf(
				"stdout %q, want %q: the abort did not give every region's chunks back",
				stdout.String(),
				want,
			)
		}
	})
}

// regionGenerationC is the C half of TestRegionGenerationsSaturate,
// through the runtime's testing hook tack_region_generation_near_max_: it
// makes every region opened after it take generations counting up from
// 2^48 - 1 - left, so a test reaches the largest generation without
// opening 2^48 regions, and 2^48 - 1 is the dead handle's.
const regionGenerationC = regionHooksC + `
void region_near_max(int64_t left) {
    tack_region_generation_near_max_((uint64_t)left);
}
`

// regionGenerationMain opens 12 regions in turn once the generations are
// 4 from the largest: the first 4 work, and once one opens with the
// dead handle every one after it does, its allocations failing with code
// 1, so no generation wraps around to one a handle held before. It also
// checks the regions around a dead one, outer and inner, still end
// cleanly, and that nothing is live after them.
const regionGenerationMain = `extern("region_near_max") func near_max(left int64)

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func main() {
    region w {
        l := fillIn(1, w)
        check(len(l) == 1, "a region before the hook works")
    }
    base := live()
    near_max(4)
    mut worked int64 = 0
    mut dead int64 = 0
    mut revived int64 = 0
    mut other int64 = 0
    for range 12 {
        region r {
            mut l := make(list[int64], r)
            e := append(mut l, 1)
            if e.code == 0 {
                worked = worked +% 1
                if dead > 0 {
                    revived = revived +% 1
                }
            } else if e.code == 1 {
                dead = dead +% 1
            } else {
                other = other +% 1
            }
        }
    }
    check(worked == 4 && dead == 8 && revived == 0 && other == 0,
        "region generations saturate to the dead handle and never wrap")
    check(live() == base, "saturated regions leave nothing live")
}
`

// TestRegionGenerationsSaturate builds regionGenerationMain under the
// sanitizers at -O0 and -O1 and checks every check it prints passed,
// the destructor's among them: region generations are 48 bits and
// saturate, a region opened at the largest getting the dead handle.
func TestRegionGenerationsSaturate(t *testing.T) {
	src := regionHooksPrelude + regionGenerationMain
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		runRegionChecks(t, buildWithStdAndC(t, src, regionGenerationC, flags...), 4)
	})
}

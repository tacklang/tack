package c_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// strictCFlags are the flags every test build that links a program takes
// before its own: C11 and every warning an error, as the builds before
// them took, and -fstrict-flex-arrays=3. Tack lowers an array to a struct
// whose last member is the C array e, and clang, by default, takes a
// struct's last array to be a flexible array member, of any length, so
// the undefined behavior sanitizer does not check an index into it. With
// -fstrict-flex-arrays=3 only a member declared [] is flexible, and every
// build that turns the sanitizers on checks each array index against the
// array's length (see TestSanitizersCatchIndexPastAnArray). The flag sits
// here, not beside the sanitizer flags, so no sanitizer build can be
// written without it; in a build without the sanitizers it changes
// nothing a program in bounds can observe.
var strictCFlags = []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fstrict-flex-arrays=3"}

// useAfterReturn follows -fsanitize=address in every build that turns the
// address sanitizer on. It gives every function's locals and parameters a
// frame of their own, poisoned once the function returns, so a read
// through a pointer into them after the return is reported whatever
// ASAN_OPTIONS says: without it the check runs only while
// detect_stack_use_after_return is on, which a run's environment can
// turn off (see TestSanitizersCatchUseAfterReturn). It is not in
// strictCFlags, since clang takes it only with -fsanitize=address.
const useAfterReturn = "-fsanitize-address-use-after-return=always"

// strayIndexSrc reads its [3]int64 parameter at an index the type checker
// proves in range; TestSanitizersCatchIndexPastAnArray edits the C it
// lowers to so the read is at index 50.
const strayIndexSrc = `package main

func pick(a [3]int64, i int64) int64 {
    if i >= 0 && i < 3 {
        return a[i]
    }
    return 0
}

func main() {
    a := [3]int64{1, 2, 3}
    if pick(a, 2) != 3 { exit(1) }
}
`

// TestSanitizersCatchIndexPastAnArray builds strayIndexSrc's C, its
// array read moved by hand to index 50 of a [3]int64, under the sanitizer
// builds the runtime tests use, at -O0 and -O1, and checks the
// undefined behavior sanitizer stops the program at the read. An array
// lowers to a struct whose last member is the C array e, and clang takes
// a struct's last array to be flexible, its length unchecked, unless told
// otherwise: a sanitizer build that misses this read misses every read
// past an array the backend could emit, so the sanitizer runs would be no
// evidence that accepted code stays in bounds.
func TestSanitizersCatchIndexPastAnArray(t *testing.T) {
	code := generate(t, strayIndexSrc)
	const read = "p_a.e[p_i]"
	if strings.Count(code, read) != 1 {
		t.Fatalf("generated C does not read %s exactly once:\n%s", read, code)
	}
	code = strings.Replace(code, read, "p_a.e[p_i + 48]", 1)

	for _, flags := range [][]string{
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		sanitizers,
	} {
		t.Run(flags[0], func(t *testing.T) {
			out, err := exec.Command(buildCWith(t, code, flags...)).CombinedOutput()
			if err == nil {
				t.Fatalf("the program read past its array and exited 0:\n%s", out)
			}
			const want = "index 50 out of bounds for type 'int64_t[3]'"
			if !strings.Contains(string(out), want) {
				t.Errorf("output does not report %q: %v\n%s", want, err, out)
			}
		})
	}
}

// danglingViewC returns a view of its by-value array parameter, which
// ends with the call, and reads through it after the call returns, as
// accepted Tack code must never do: with no argument through dangling,
// which clang may inline, and with one through danglingOutOfLine, which it
// may not.
const danglingViewC = `#include <stdint.h>
#include <stdio.h>

typedef struct {
    int64_t e[3];
} array3;

typedef struct {
    int64_t *ptr;
    int64_t len;
} view;

static view dangling(array3 a) {
    return (view){a.e, 3};
}

__attribute__((noinline)) static view danglingOutOfLine(array3 a) {
    return (view){a.e, 3};
}

int main(int argc, char **argv) {
    (void)argv;
    const array3 a = {{1, 2, 3}};
    view v = argc > 1 ? danglingOutOfLine(a) : dangling(a);
    printf("%lld\n", (long long)v.ptr[2]);
    return 0;
}
`

// TestSanitizersCatchUseAfterReturn builds danglingViewC under the
// sanitizer builds the runtime tests use and checks the address sanitizer
// stops the program at the read through the dangling view: the out of
// line call at -O0, -O1 and -O2, the inlinable one at -O0. The program
// runs with detect_stack_use_after_return=0 in ASAN_OPTIONS, so the
// catch holds only because the builds take useAfterReturn, which turns
// the check on whatever the environment says. At -O1 and above clang
// inlines dangling and folds its parameter into the caller's array,
// which lives on, so no build can catch that read; that is why every
// sanitizer build runs at -O0 as well (see atO0Too).
func TestSanitizersCatchUseAfterReturn(t *testing.T) {
	cases := []struct {
		name   string
		levels []string
		args   []string
	}{
		{"inlinable", []string{"-O0"}, nil},
		{"out of line", []string{"-O0", "-O1", "-O2"}, []string{"out-of-line"}},
	}
	for _, tc := range cases {
		for _, level := range tc.levels {
			t.Run(tc.name+"/"+level, func(t *testing.T) {
				flags := append([]string{level}, sanitizers[1:]...)
				cmd := exec.Command(buildCWith(t, danglingViewC, flags...), tc.args...)
				cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_stack_use_after_return=0")
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatalf("the program read through a dangling view and exited 0:\n%s", out)
				}
				const want = "AddressSanitizer: stack-use-after-return"
				if !strings.Contains(string(out), want) {
					t.Errorf("output does not report %q: %v\n%s", want, err, out)
				}
			})
		}
	}
}

// atO0Too runs check in a subtest under flags and, when flags build above
// -O0, in a subtest before it under the same flags at -O0, named for the
// level each builds at. At -O1 and above clang inlines a small function
// and can fold its by-value parameter into the caller's copy, which
// lives on, so a pointer into the parameter kept past the call reads live
// memory and the address sanitizer has nothing to report; at -O0 the
// parameter is the callee's own and the read is reported (see
// TestSanitizersCatchUseAfterReturn). Every build that turns the address
// sanitizer on at a higher level runs through atO0Too, or lists an -O0
// row of its own.
func atO0Too(t *testing.T, flags []string, check func(t *testing.T, flags []string)) {
	t.Helper()
	runs := [][]string{flags}
	if i := slices.IndexFunc(flags, isLevel); i >= 0 && flags[i] != "-O0" {
		o0 := slices.Clone(flags)
		o0[i] = "-O0"
		runs = [][]string{o0, flags}
	}
	for _, run := range runs {
		level := "-O0"
		if i := slices.IndexFunc(run, isLevel); i >= 0 {
			level = run[i]
		}
		t.Run(level, func(t *testing.T) { check(t, run) })
	}
}

// isLevel reports whether flag is one of clang's optimization levels, as
// -O0 or -O2.
func isLevel(flag string) bool {
	return strings.HasPrefix(flag, "-O")
}

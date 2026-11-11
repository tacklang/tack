package c_test

import (
	"os/exec"
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
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
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

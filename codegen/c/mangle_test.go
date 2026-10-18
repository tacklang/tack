package c_test

import (
	"strings"
	"testing"
)

// TestMainPackageNamesAreLengthPrefixed checks the main package's names,
// a function, a global, a struct and a results struct, are mangled by its
// path, main, like any package's, and that no name keeps the old
// tack_main_ form.
func TestMainPackageNamesAreLengthPrefixed(t *testing.T) {
	got := generate(t, `package main

type point_2d struct {
    x int64
}

var origin = point_2d{x: 0}

func pair() (int64, int64) {
    return 1, 2
}

func main() {
    a, b := pair()
    code, fits := int32(a + b + origin.x)
    if fits { exit(code) }
}
`)
	for _, want := range []string{
		"static void tack_4_main4_main(void) {",
		"    tack_4_main4_main();",
		"} tack_4_main10_point_5f2d;",
		"static const tack_4_main10_point_5f2d tack_4_main6_origin = {0};",
		"static tack_results_4_main4_pair tack_4_main4_pair(void)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "tack_main_") || strings.Contains(got, "tack_results_main_") {
		t.Errorf("generated C keeps a tack_main_ name:\n%s", got)
	}
}

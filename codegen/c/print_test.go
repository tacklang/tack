package c_test

import (
	"testing"
)

// TestPrintWritesExactlyItsArgument checks print adds nothing to what it is
// given: no newline, no separator, and nothing at all for an empty string.
func TestPrintWritesExactlyItsArgument(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		lines []string
	}{
		{"no newline", "a", []string{`print("a")`}},
		{"consecutive prints join", "abc", []string{`print("a")`, `print("b")`, `print("c")`}},
		{"explicit newline", "a\nb\n", []string{`print("a\n")`, `print("b\n")`}},
		{"empty string", "", []string{`print("")`}},
		{"only a newline", "\n", []string{`print("\n")`}},
		{
			"empty between others",
			"ab",
			[]string{`print("a")`, `print("")`, `print("b")`},
		},
		{"zero value", "", []string{"mut s string", "print(s)"}},
		{
			"in a loop",
			"xxx",
			[]string{"for range 3 {", `    print("x")`, "}"},
		},
		{
			"in a three-clause post",
			"x-x-",
			[]string{"for mut i := 0; i < 2; print(\"-\") {", "    i++", `    print("x")`, "}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, status := run(t, program(tt.lines...))
			if status != 0 {
				t.Fatalf("exit status = %d, want 0", status)
			}
			if out != tt.want {
				t.Errorf("program printed %q, want %q", out, tt.want)
			}
		})
	}
}

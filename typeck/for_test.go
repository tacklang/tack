package typeck_test

import (
	"testing"
)

// TestAcceptsLoops lists loops that must check cleanly.
func TestAcceptsLoops(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"forever with break", inMain("for {\n        break\n    }")},
		{"bool condition", mainLines("mut i = 0", "for i < 3 {", "    i = i + 1", "}")},
		{
			"logical condition",
			mainLines("mut i = 0", "for i < 3 && !false {", "    i = i + 1", "    continue", "}"),
		},
		{
			"break inside an if",
			mainLines(
				"for true {",
				"    if 1 < 2 {",
				"        break",
				"    } else {",
				"        continue",
				"    }",
				"}",
			),
		},
		{
			"nested",
			inMain("for {\n        for {\n            break\n        }\n        break\n    }"),
		},
		{
			"checks the body",
			mainLines("mut s = \"\"", "for false {", "    s = \"x\"", "    print(s)", "}"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsInvalidLoops checks the condition is a bool, jumps are inside a
// loop, and the body is checked like any other block.
func TestRejectsInvalidLoops(t *testing.T) {
	type want struct{ msg, at string }
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			"int64 condition",
			inMain("for 1 {\n    }"),
			[]want{{"condition must be a bool, got int64", "1"}},
		},
		{
			"string condition",
			inMain("for \"s\" {\n    }"),
			[]want{{"condition must be a bool, got string", "\"s\""}},
		},
		{"break outside a loop", inMain("break"), []want{{"break is not in a loop", "break"}}},
		{
			"continue outside a loop",
			inMain("continue"),
			[]want{{"continue is not in a loop", "continue"}},
		},
		{
			"break in an if outside a loop",
			inMain("if true {\n        break\n    }"),
			[]want{{"break is not in a loop", "break"}},
		},
		{
			"after a loop",
			inMain("for {\n        break\n    }\n    continue"),
			[]want{{"continue is not in a loop", "continue"}},
		},
		{
			"error in the body",
			inMain("for true {\n        exit(\"s\")\n    }"),
			[]want{{"cannot use string as int32 in argument to exit", "\"s\""}},
		},
		{
			"break in another function",
			"package main\n\nfunc f() {\n    break\n}\n\nfunc main() {\n    for {\n        f()\n    }\n}\n",
			[]want{{"break is not in a loop", "break"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// TestLoopTermination pins Go's rule: a function with a result may end in a
// loop only when the loop has no condition and no break that leaves it.
func TestLoopTermination(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		terminates bool
	}{
		{"forever", "for {\n    }", true},
		{
			"forever returning inside",
			"for {\n        if n > 3 {\n            return n\n        }\n    }",
			true,
		},
		{"forever with continue", "for {\n        continue\n    }", true},
		{"forever with break", "for {\n        break\n    }", false},
		{
			"forever with break in an if",
			"for {\n        if n > 1 {\n            break\n        }\n    }",
			false,
		},
		{
			"forever with break in an else",
			"for {\n        if n > 1 {\n        } else {\n            break\n        }\n    }",
			false,
		},
		{
			"forever with break in a nested loop",
			"for {\n        for {\n            break\n        }\n    }",
			true,
		},
		{
			"forever with break in an inner loop's if",
			"for {\n        for n > 0 {\n            if n > 1 {\n                break\n            }\n        }\n    }",
			true,
		},
		{"condition true", "for true {\n        return 1\n    }", false},
		{"condition", "for n > 0 {\n        return 1\n    }", false},
		{
			"if branches ending in loops",
			"if n > 0 {\n        for {\n        }\n    } else {\n        return 2\n    }",
			true,
		},
		{"loop then return", "for n > 0 {\n    }\n    return 0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\nfunc f(n int64) int64 {\n    " + tt.body + "\n}\n\nfunc main() {\n    code, fits := int32(f(1))\n    if fits { exit(code) }\n}\n"
			_, diags := checkTree(t, src)
			if tt.terminates && len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
			if !tt.terminates && (len(diags) != 1 || diags[0].Message != "f must end in a return") {
				t.Errorf("got %v, want only %q", diags, "f must end in a return")
			}
		})
	}
}

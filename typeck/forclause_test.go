package typeck_test

import "testing"

// invalidClauseCases are three-clause loops the checker must reject, with the
// diagnostics each must produce.
var invalidClauseCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"immutable counter",
		inMain("for i := 0; i < 3; i++ {\n    }"),
		[]spanned{{"cannot assign to immutable variable i", "i"}},
	},
	{
		"int64 condition",
		inMain("for mut i := 0; i; i++ {\n    }"),
		[]spanned{{"condition must be a bool, got int64", "i"}},
	},
	{
		"bad init",
		inMain("for mut i := \"s\"; ; i++ {\n        print(i)\n        break\n    }"),
		[]spanned{{"cannot use string as int64 in ++", "i"}},
	},
	{
		"bad post",
		inMain("for mut i := 0; i < 3; i += true {\n    }"),
		[]spanned{{"cannot use bool as int64 in +=", "true"}},
	},
	{
		"error in post",
		inMain("for mut i := 0; i < 3; exit(\"s\") {\n        i++\n    }"),
		[]spanned{{"cannot use string as int64 in argument to exit", "\"s\""}},
	},
}

// TestThreeClauseLoops checks each clause is checked like the statement or
// condition it is, and the terminating rule follows the condition.
func TestThreeClauseLoops(t *testing.T) {
	ok := []string{
		inMain("for mut i := 0; i < 3; i++ {\n        exit(i)\n    }"),
		mainLines("mut i = 5", "for i = 0; i < 3; i += 1 {", "}", "exit(i)"),
		inMain("for ;; {\n        break\n    }"),
		"package main\n\nfunc f() int64 {\n    for mut i := 0; ; i++ {\n        if i > 3 {\n            return i\n        }\n    }\n}\n\nfunc main() {\n    exit(f())\n}\n",
	}
	for _, src := range ok {
		if _, diags := checkTree(t, src); len(diags) > 0 {
			t.Errorf("%q: got %v, want no diagnostics", src, diags)
		}
	}

	notTerminating := "package main\n\nfunc f() int64 {\n    for mut i := 0; i < 9; i++ {\n        return i\n    }\n}\n\nfunc main() {\n    exit(f())\n}\n"
	if _, diags := checkTree(
		t,
		notTerminating,
	); len(diags) != 1 ||
		diags[0].Message != "f must end in a return" {
		t.Errorf("loop with a condition: got %v, want f must end in a return", diags)
	}

	for _, tt := range invalidClauseCases {
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

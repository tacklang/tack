package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// switchShape renders a switch as its tag, or "-" for none, then each case
// as "case v1, v2/n" or "default/n", n being how many statements its body
// holds, joined by "; ".
func switchShape(t *testing.T, s *ast.Switch) string {
	t.Helper()
	parts := make([]string, 1, 1+len(s.Cases))
	parts[0] = "-"
	if s.Tag != nil {
		parts[0] = structShape(t, s.Tag)
	}
	for _, cl := range s.Cases {
		values := make([]string, len(cl.Values))
		for i, v := range cl.Values {
			values[i] = structShape(t, v)
		}
		head := "default"
		if !cl.Default {
			head = "case " + strings.Join(values, ", ")
		}
		parts = append(parts, head+"/"+string(rune('0'+len(cl.Body))))
	}
	return strings.Join(parts, "; ")
}

// firstSwitch parses body as main's statements and returns the first one
// as a switch.
func firstSwitch(t *testing.T, body string) *ast.Switch {
	t.Helper()
	stmts := mainBody(t, body)
	s, ok := stmts[0].(*ast.Switch)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Switch", stmts[0])
	}
	return s
}

// parsedSwitches pairs switches with the shape switchShape gives them.
var parsedSwitches = []struct {
	src  string
	want string
}{
	{"switch x {\n    }", "x"},
	{"switch {\n    }", "-"},
	{"switch x {}", "x"},
	{"switch {}", "-"},
	{"switch x {\n    case 1:\n    }", "x; case 1/0"},
	{"switch x {\n    case 1, 2, 3:\n        f()\n        g()\n    }", "x; case 1, 2, 3/2"},
	{
		"switch x {\n    case 1:\n        f()\n    default:\n        g()\n    case 2:\n    }",
		"x; case 1/1; default/1; case 2/0",
	},
	{"switch {\n    case a < b, c:\n        f()\n    }", "-; case (a < b), c/1"},
	{"switch {\n    default:\n    }", "-; default/0"},
	{"switch f() + 1 {\n    case g(), 2 * 3:\n    }", "(f() + 1); case g(), (2 * 3)/0"},
	{"switch x { case 1: f(); case 2: g() }", "x; case 1/1; case 2/1"},
	{"switch x { case 1: f(); g(); default: }", "x; case 1/2; default/0"},
	{
		"switch p {\n    case Point{x: 1}, (Point{x: 2}):\n    }",
		"p; case Point{x: 1}, Point{x: 2}/0",
	},
	{"switch (Point{x: 1}) {\n    }", "Point{x: 1}"},
	{"switch a[i].x {\n    case b.y[0]:\n    }", "a[i].x; case b.y[0]/0"},
	{
		"switch x {\n    case 1:\n        switch y {\n        case 2:\n        }\n    }",
		"x; case 1/1",
	},
	{
		"switch x {\n    case 1:\n        for {\n            break\n        }\n        continue\n    }",
		"x; case 1/2",
	},
}

// TestParsesSwitches checks both forms, every way of writing a case, and
// a default in any position.
func TestParsesSwitches(t *testing.T) {
	for _, tt := range parsedSwitches {
		t.Run(tt.src, func(t *testing.T) {
			if got := switchShape(t, firstSwitch(t, "    "+tt.src)); got != tt.want {
				t.Errorf("shape = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSwitchSpans checks the switch spans its keyword through its closing
// brace, and each case its keyword through its colon.
func TestSwitchSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    switch x {\n    case 1, 2:\n        f()\n    default:\n    }\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	s := file.Decls[0].(*ast.Func).Body[0].(*ast.Switch)
	want := "switch x {\n    case 1, 2:\n        f()\n    default:\n    }"
	if got := src[s.Span.Start:s.Span.End]; got != want {
		t.Errorf("switch spans %q, want %q", got, want)
	}
	for i, wantCase := range []string{"case 1, 2:", "default:"} {
		cl := s.Cases[i]
		if got := src[cl.Span.Start:cl.Span.End]; got != wantCase {
			t.Errorf("case %d spans %q, want %q", i, got, wantCase)
		}
	}
	if s.Default() != s.Cases[1] {
		t.Errorf("Default() = %v, want the second case", s.Default())
	}
}

// malformedSwitches pairs malformed or truncated switches, and stray
// pieces of one, with the message each must be reported with.
var malformedSwitches = []struct {
	src  string
	want string
}{
	{"switch", "expected 'value' found '}'"},
	{"switch x", "expected '{' found ';'"},
	{"switch x\n    {\n    }", "expected '{' found ';'"},
	{"switch {", "expected '}' found 'EOF'"},
	{"switch x {\n    case", "expected 'value' found '}'"},
	{"switch x {\n    case 1", "expected ':' found ';'"},
	{"switch x {\n    case 1,\n    }", "expected 'value' found '}'"},
	{"switch x {\n    case 1, :\n    }", "expected 'value' found ':'"},
	{"switch x {\n    case :\n    }", "expected 'value' found ':'"},
	{"switch x {\n    case 1 2:\n    }", "expected ':' found '2'"},
	{"switch x {\n    default\n    }", "expected ':' found '}'"},
	{"switch x {\n    default 1:\n    }", "expected ':' found '1'"},
	{"switch x {\n    default:\n    default:\n    }", "multiple defaults in switch"},
	{"switch {\n    default:\n    case true:\n    default:\n    }", "multiple defaults in switch"},
	{"switch x {\n    f()\n    }", "expected 'case or default' found 'f'"},
	{"switch x {\n    ;\n    }", "expected 'case or default' found ';'"},
	{"switch x {\n    case 1: f() case 2:\n    }", "expected ';' found 'case'"},
	{"switch x := 1; x {\n    }", "expected '{' found ':='"},
	{"switch f(); x {\n    }", "expected '{' found ';'"},
	{"switch x {\n    } else {\n    }", "expected ';' found 'else'"},
	{"switch Point{x: 1} {\n    }", "a struct literal in a switch header must be in parentheses"},
	{"switch case {\n    }", "expected 'value' found 'case'"},
	{"switch x {\n    case case:\n    }", "expected 'value' found 'case'"},
	{
		"switch x {\n    case 1:\n        type T struct {}\n    }",
		"types can only be declared at package level",
	},
	{"case 1:", "expected 'statement' found 'case'"},
	{"default:", "expected 'statement' found 'default'"},
	{"fallthrough", "expected 'statement' found 'fallthrough'"},
	{
		"switch x {\n    case 1:\n        fallthrough\n    }",
		"expected 'statement' found 'fallthrough'",
	},
}

// TestRejectsMalformedSwitches checks every malformed or truncated switch,
// and a case or default outside one, is a parse error.
func TestRejectsMalformedSwitches(t *testing.T) {
	for _, tt := range malformedSwitches {
		t.Run(tt.src, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.src+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

// TestTruncatedSwitchesNeverParse cuts a switch off after every byte and
// checks each prefix is a parse error, never a panic or a tree.
func TestTruncatedSwitchesNeverParse(t *testing.T) {
	src := "package main\n\nfunc main() {\n    switch f(1) {\n    case 1, 2:\n        g()\n    default:\n        h()\n    }\n}\n"
	end := strings.LastIndex(src, "}")
	for n := strings.Index(src, "switch"); n < end; n++ {
		file, diags := parse(t, src[:n])
		if file != nil || len(diags) == 0 {
			t.Errorf("prefix %q parsed, want an error", src[:n])
		}
	}
}

// TestParsesDeeplyNestedSwitches checks a switch nested a thousand deep, in
// a case body each time, parses.
func TestParsesDeeplyNestedSwitches(t *testing.T) {
	const depth = 1000
	body := strings.Repeat("switch x {\ncase 1:\n", depth) + strings.Repeat("}\n", depth)
	s := firstSwitch(t, body)
	for range depth - 1 {
		s = s.Cases[0].Body[0].(*ast.Switch)
	}
	if len(s.Cases[0].Body) != 0 {
		t.Errorf("innermost case has %d statements, want none", len(s.Cases[0].Body))
	}
}

// TestParsesHugeCaseLists checks a case with ten thousand values and a
// switch with ten thousand cases parse whole.
func TestParsesHugeCaseLists(t *testing.T) {
	const n = 10000
	values := make([]string, n)
	cases := make([]string, n)
	for i := range n {
		values[i] = "x"
		cases[i] = "case x:\n"
	}
	s := firstSwitch(t, "switch y {\ncase "+strings.Join(values, ", ")+":\n}")
	if len(s.Cases) != 1 || len(s.Cases[0].Values) != n {
		t.Errorf("got %d cases, want one of %d values", len(s.Cases), n)
	}
	s = firstSwitch(t, "switch y {\n"+strings.Join(cases, "")+"}")
	if len(s.Cases) != n {
		t.Errorf("got %d cases, want %d", len(s.Cases), n)
	}
}

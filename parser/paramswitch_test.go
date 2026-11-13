package parser_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// paramSwitchSrc switches on its function's type parameter, with a case
// of each shape of value, and in the case of the list on the type
// parameter that case declares, inside a closure.
const paramSwitchSrc = `package main

func f[T Data](v T) {
    switch T {
    case bool:
    case int8, int16:
    case text.Text:
    case list[E]:
        run(func() {
            switch E {
            case uint8:
            default:
            }
        })
    case map[text.Text]V:
    case struct:
    default:
    }
}
`

// TestParsesParamSwitch checks a switch on a type parameter of the
// function it is in is marked OnType with its name as the tag, each case
// value parsed as a type, struct as a StructCase, and that a switch on
// the name a case's list[E] declares, in that case's body, is one too.
func TestParsesParamSwitch(t *testing.T) {
	file, diags := parse(t, paramSwitchSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	sw, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.Switch)
	if !ok || !sw.OnType {
		t.Fatalf("%T %+v is no switch on a type parameter", sw, sw)
	}
	if id, ok := sw.Tag.(*ast.Ident); !ok || id.Name != "T" {
		t.Errorf("tag %+v, want the name T", sw.Tag)
	}
	want := []string{"bool", "int8,int16", "text.Text", "list[E]", "map[text.Text]V", "struct", ""}
	if got := caseTexts(paramSwitchSrc, sw); !slices.Equal(got, want) {
		t.Fatalf("cases name %q, want %q", got, want)
	}
	if _, ok := sw.Cases[5].Values[0].(*ast.StructCase); !ok {
		t.Errorf("struct is %T, want *ast.StructCase", sw.Cases[5].Values[0])
	}
	if name := ast.CaseTypeName(sw.Cases[3].Values[0]); name == nil || name.Name != "E" {
		t.Errorf("list[E] declares %+v, want E", name)
	}
	if name := ast.CaseTypeName(sw.Cases[4].Values[0]); name == nil || name.Name != "V" {
		t.Errorf("map[text.Text]V declares %+v, want V", name)
	}
	if inner := innerSwitch(sw.Cases[3].Body[0]); inner == nil || !inner.OnType {
		t.Errorf("the switch on E inside the case of list[E] is %+v, want one on a type parameter",
			inner)
	}
}

// caseTexts returns what each case of sw, a switch of src, names, its
// values as written joined by commas.
func caseTexts(src string, sw *ast.Switch) []string {
	out := make([]string, 0, len(sw.Cases))
	for _, cl := range sw.Cases {
		var values []string
		for _, v := range cl.Values {
			values = append(values, src[v.Spanned().Start:v.Spanned().End])
		}
		out = append(out, strings.Join(values, ","))
	}
	return out
}

// innerSwitch returns the last switch n holds, at any depth, or nil.
func innerSwitch(n ast.Node) *ast.Switch {
	var inner *ast.Switch
	ast.Walk(n, func(n ast.Node) bool {
		if s, ok := n.(*ast.Switch); ok {
			inner = s
		}
		return true
	})
	return inner
}

// TestParamSwitchNeedsTypeParam checks a switch on a name that is no type
// parameter in scope, or on a type parameter's name with anything after
// it, is an expression switch, its values expressions: a type parameter
// of another function, a name the case of another switch declares, and
// T followed by an operator.
func TestParamSwitchNeedsTypeParam(t *testing.T) {
	for _, tt := range []struct {
		name, src string
	}{
		{"other function", "func g[T Data](v T) {\n}\n\nfunc f(T int64) {\n" +
			"    switch T {\n    case 1:\n    }\n}\n"},
		{"other case", "func f[T Data](v T) {\n    switch T {\n    case list[E]:\n" +
			"    default:\n        switch E {\n        case 1:\n        }\n    }\n}\n"},
		{"operator after", "func f[T Data](v T) {\n    switch T + 1 {\n    case 2:\n    }\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file, diags := parse(t, "package main\n\n"+tt.src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			found := false
			for _, d := range file.Decls {
				ast.Walk(d, func(n ast.Node) bool {
					if s, ok := n.(*ast.Switch); ok && !s.OnType {
						found = true
						if _, isLit := s.Cases[0].Values[0].(*ast.IntLit); !isLit {
							t.Errorf("case value %T, want an expression", s.Cases[0].Values[0])
						}
					}
					return true
				})
			}
			if !found {
				t.Error("no expression switch parsed")
			}
		})
	}
}

// paramSwitchParseErrors are malformed switches on a type parameter, each
// with the message its first error must have.
var paramSwitchParseErrors = []struct {
	name, src, msg string
}{
	{
		"value case", "func f[T Data](v T) {\n    switch T {\n    case 1:\n    }\n}\n",
		"expected 'type name' found '1'",
	},
	{
		"struct type", "func f[T Data](v T) {\n    switch T {\n    case struct{}:\n    }\n}\n",
		"expected ':' found '{'",
	},
	{
		"unclosed list", "func f[T Data](v T) {\n    switch T {\n    case list[E:\n    }\n}\n",
		"expected ']' found ':'",
	},
	{
		"no colon", "func f[T Data](v T) {\n    switch T {\n    case int64\n    }\n}\n",
		"expected ':' found ';'",
	},
	{
		"empty case", "func f[T Data](v T) {\n    switch T {\n    case :\n    }\n}\n",
		"expected 'type name' found ':'",
	},
}

// TestRejectsMalformedParamSwitch checks each malformed switch on a type
// parameter is reported with its message.
func TestRejectsMalformedParamSwitch(t *testing.T) {
	for _, tt := range paramSwitchParseErrors {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := parse(t, "package main\n\n"+tt.src)
			if len(diags) == 0 {
				t.Fatal("parse reported nothing")
			}
			if diags[0].Message != tt.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.msg)
			}
		})
	}
}

// paramSwitchSeeds are sources about switches on type parameters the
// fuzzer starts from.
var paramSwitchSeeds = []string{
	paramSwitchSrc,
	"package main\nfunc f[T Data, U Data](v T, u U) {\nswitch T {\ncase list[E]:\nswitch E {\n" +
		"case map[text.Text]V:\nswitch V {\ncase struct, bool:\n}\n}\n}\nswitch U {\n}\n}\n",
	"package main\nfunc f[T Data](v T) {\nswitch T {\ncase list[list[E]], map[string]V, [2]T:\n" +
		"fallthrough\ncase struct\n}\n}\n",
	"package main\nfunc f[T Data](v T) {\nswitch T {\ncase list[\n}\nswitch T {\ndefault:\ndefault:\n}\n}\n",
	"package main\nfunc f[T Data](v T) {\nswitch x := T {\n}\nswitch T.(type) {\n}\nswitch T\n}\n",
}

// FuzzParseParamSwitch checks the parser never panics or hangs on sources
// near switches on type parameters, and reports every bad node it
// recovers from.
func FuzzParseParamSwitch(f *testing.F) {
	for _, src := range paramSwitchSeeds {
		f.Add(src)
	}
	for _, tt := range paramSwitchParseErrors {
		f.Add("package main\n\n" + tt.src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		checkParse(t, src)
	})
}

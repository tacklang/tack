package typeck_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// fieldsBody is a generic function whose fields loop body has an error,
// a break, and calls %CALLS% in main.
const fieldsBody = `package main

type A struct {
    x int64
}

type B struct {
    l list[int64]
    a A
    m map[int64]int64
}

func g[T Data](v T) {
    for f := range fields(T) {
        print(f.name)
        break
    }
}

func main() {
%CALLS%
}
`

// TestFieldsLoopCheckedOnce checks a fields loop's body is checked once,
// whatever calls bind its type parameter to: the same error is reported,
// once, with no call, with a call binding a struct with fields, one with
// none and a basic type, and with several.
func TestFieldsLoopCheckedOnce(t *testing.T) {
	var want []string
	for _, calls := range []string{
		"",
		"    g(A{x: 1})",
		"    g(5)\n    g(A{x: 1})\n    g(true)",
	} {
		diags := check(t, strings.Replace(fieldsBody, "%CALLS%", calls, 1))
		got := messages(diags)
		if want == nil {
			want = got
			const breakMsg = "break is not allowed in a fields loop"
			if len(want) != 1 || !strings.Contains(want[0], breakMsg) {
				t.Fatalf("with no call: %v", want)
			}
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("with calls %q: %v, want %v", calls, got, want)
		}
	}
}

// messages returns the message of each error in diags, in order.
func messages(diags []diag.Diagnostic) []string {
	var msgs []string
	for _, d := range diags {
		if d.Severity == diag.Error {
			msgs = append(msgs, d.Message)
		}
	}
	return msgs
}

// TestDataRejectsAtTheBindingCall checks a call binding a Data type
// parameter to a struct with a field that is not Data is reported at the
// call, naming the field deepest in it that is not, and the generic body
// is not reported.
func TestDataRejectsAtTheBindingCall(t *testing.T) {
	src := strings.Replace(strings.Replace(fieldsBody, "        break\n", "", 1),
		"%CALLS%", "    g(B{l: make(list[int64], c_allocator), a: A{x: 1}, "+
			"m: make(map[int64]int64, c_allocator)})", 1)
	got := messages(check(t, src))
	want := []string{"B does not satisfy Data: the key type of map[int64]int64 is int64, " +
		"and a Data map's key is text.Text"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestFieldsLoopRecordsFields checks the fields each fields loop lists are
// recorded for each type it is lowered for, in the order instances are
// found: a struct's fields in declaration order, none for a basic type,
// and those of a nested instance's type too.
func TestFieldsLoopRecordsFields(t *testing.T) {
	src := `package main

type A struct {
    x int64
    y int64
}

type B struct {
    a A
    n int64
}

func deep[T Data](v T) int64 {
    mut n := 0
    for f := range fields(T) {
        n = n +% 1 +% deep(v.[f])
    }
    return n
}

func main() {
    b := B{a: A{x: 1, y: 2}, n: 3}
    for f := range fields(B) {
        print(f.name)
    }
    print("\n")
    if deep(b) == 4 {
        print("ok\n")
    }
}
`
	file, diags := checkFile(t, src)
	if len(diags) > 0 {
		t.Fatalf("check reported %+v", diags)
	}
	var loops []*ast.FieldsOf
	ast.Walk(file, func(n ast.Node) bool {
		if _, fo, ok := ast.FieldsLoop(n); ok {
			loops = append(loops, fo)
		}
		return true
	})
	if len(loops) != 2 {
		t.Fatalf("found %d fields loops, want 2", len(loops))
	}
	deep, inMain := loops[0], loops[1]
	got := make([]string, 0, len(deep.Unrolled))
	for _, u := range deep.Unrolled {
		got = append(got, file.Types.Name(u.Type)+fieldList(u.Fields))
	}
	if want := []string{"B[0 1]", "A[0 1]", "int64[]"}; !slices.Equal(got, want) {
		t.Errorf("deep's loop lists %v, want %v", got, want)
	}
	if len(inMain.Unrolled) != 1 || !inMain.Names || deep.Names {
		t.Errorf("main's loop records %+v, names %v; deep's names %v",
			inMain.Unrolled, inMain.Names, deep.Names)
	}
}

// fieldList writes fields as a list of positions, [0 1].
func fieldList(fields []int) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = string(rune('0' + f))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

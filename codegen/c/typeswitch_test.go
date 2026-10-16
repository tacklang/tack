package c_test

import (
	"os/exec"
	"strings"
	"testing"
)

// assertProgram declares f(a any, xs ...any), whose body is lines, and a
// main that does nothing.
func assertProgram(lines ...string) string {
	return "package main\n\nfunc use(a any) {\n}\n\nfunc f(a any, xs ...any) {\n    " +
		strings.Join(lines, "\n    ") + "\n}\n\nfunc main() {\n}\n"
}

// assertCTests are type assertions and type switches, and the C each must
// contain.
var assertCTests = []struct {
	name string
	src  string
	want []string
}{
	{
		"assertion",
		assertProgram("v, ok := a.(int64)", "use(v)", "use(ok)"),
		[]string{
			"const tack_any tack_r0 = p_a;",
			"const int64_t v_v = ((tack_r0.type == TACK_TYPE_INT64) ? *(const int64_t *)tack_r0.ptr : 0);",
			"const bool v_ok = (tack_r0.type == TACK_TYPE_INT64);",
		},
	},
	{
		"string assertion, zero value pooled",
		assertProgram("s, ok := a.(string)", "use(s)", "use(ok)"),
		[]string{
			"? *(const tack_string *)tack_r0.ptr : (tack_string){tack_str0, TACK_C_STR_LEN(tack_str0)});",
		},
	},
	{
		"bool assertion assigned",
		assertProgram(
			"mut b bool = true",
			"mut ok bool = true",
			"b, ok = a.(bool)",
			"use(b)",
			"use(ok)",
		),
		[]string{
			"v_b = ((tack_r0.type == TACK_TYPE_BOOL) ? *(const bool *)tack_r0.ptr : false);",
			"v_ok = (tack_r0.type == TACK_TYPE_BOOL);",
		},
	},
	{
		"checked element of anys, zeroed out of range",
		assertProgram("e, ok := xs[1]", "use(e)", "use(ok)"),
		[]string{"? p_xs.ptr[tack_r0] : (tack_any){});"},
	},
	{
		"type switch",
		assertProgram(
			"switch v := a.(type) {",
			"case unsafe_size_t:",
			"    use(v)",
			"case int8, string:",
			"    use(v)",
			"case bool:",
			"default:",
			"    use(v)",
			"}",
		),
		[]string{
			"const tack_any tack_s0 = p_a;",
			"if (tack_s0.type == TACK_TYPE_UNSAFE_SIZE_T) {\n            const size_t v_v = *(const size_t *)tack_s0.ptr;",
			"} else if ((tack_s0.type == TACK_TYPE_INT8) || (tack_s0.type == TACK_TYPE_STRING)) {\n" +
				"            const tack_any v_v = tack_s0;",
			"} else if (tack_s0.type == TACK_TYPE_BOOL) {\n        } else {\n            const tack_any v_v = tack_s0;",
		},
	},
	{
		"type switch without a binding",
		assertProgram(
			"if len(xs) > 0 {",
			"    switch xs[0].(type) {",
			"    case uint64:",
			"        use(1)",
			"    }",
			"}",
		),
		[]string{
			"const tack_any tack_s0 = p_xs.ptr[0];",
			"if (tack_s0.type == TACK_TYPE_UINT64) {",
		},
	},
	{
		"type switch with no cases",
		assertProgram("switch a.(type) {", "}"),
		[]string{"(void)(p_a);"},
	},
}

// TestGeneratesTypeAssertions checks the C each of assertCTests generates,
// and that a type switch never becomes a C switch, or declares a variable
// a case does not read.
func TestGeneratesTypeAssertions(t *testing.T) {
	for _, tt := range assertCTests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate(t, tt.src)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("want\n%s\nin\n%s", want, got)
				}
			}
			if strings.Contains(got, "switch (") {
				t.Errorf("generated a C switch:\n%s", got)
			}
		})
	}
	got := generate(t, assertCTests[4].src)
	if n := strings.Count(got, "v_v ="); n != 3 {
		t.Errorf(
			"declared %d case variables, want 3, none in the case that reads none:\n%s",
			n,
			got,
		)
	}
}

// typeSwitchChecks is a program that asserts and switches on values of
// every type an any can hold, at their edges, and on the empty any an
// index out of range gives, and exits with the number of the first check
// that fails, or 0.
const typeSwitchChecks = `package main

func kind(a any) int64 {
    switch v := a.(type) {
    case int8:
        if v == -128 {
            return 1
        }
    case int16:
        if v == 32767 {
            return 2
        }
    case int32:
        if v == -2147483648 {
            return 3
        }
    case int64:
        if v == 9223372036854775807 {
            return 4
        }
    case uint8:
        if v == 255 {
            return 5
        }
    case uint16:
        if v == 65535 {
            return 6
        }
    case uint32:
        if v == 4294967295 {
            return 7
        }
    case uint64:
        if v == 18446744073709551615 {
            return 8
        }
    case unsafe_size_t:
        if v == 4294967295 {
            return 9
        }
    case unsafe_uintptr_t:
        if v == 0 {
            return 10
        }
    case bool:
        if v {
            return 11
        }
    case string:
        if len(v) == 3 && v[2] == 'c' {
            return 12
        }
    default:
        return 13
    }
    return 0
}

func empty(xs ...any) int64 {
    e, ok := xs[len(xs)]
    if ok {
        return -1
    }
    n, isInt := e.(int64)
    s, isString := e.(string)
    if isInt || isString || n != 0 || len(s) != 0 {
        return -2
    }
    return kind(e)
}

func main() {
    var i8 int8 = -128
    var i16 int16 = 32767
    var i32 int32 = -2147483648
    var u8 uint8 = 255
    var u16 uint16 = 65535
    var u32 uint32 = 4294967295
    var u64 uint64 = 18446744073709551615
    var sz unsafe_size_t = 4294967295
    var up unsafe_uintptr_t = 0
    if kind(i8) != 1 || kind(i16) != 2 || kind(i32) != 3 || kind(9223372036854775807) != 4 {
        exit(1)
    }
    if kind(u8) != 5 || kind(u16) != 6 || kind(u32) != 7 || kind(u64) != 8 {
        exit(2)
    }
    if kind(sz) != 9 || kind(up) != 10 || kind(true) != 11 || kind("abc") != 12 {
        exit(3)
    }
    if kind(false) != 0 || kind("ab") != 0 || kind(int8(0)) != 0 {
        exit(4)
    }
    if empty() != 13 || empty(1, "s") != 13 {
        exit(5)
    }
}
`

// TestTypeSwitchesRunUnderSanitizers builds typeSwitchChecks under the
// address and undefined behavior sanitizers, so a read through an any's
// pointer at the wrong type or of the empty any would stop it, and checks
// every check passes.
func TestTypeSwitchesRunUnderSanitizers(t *testing.T) {
	bin := buildWith(
		t,
		typeSwitchChecks,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Errorf("running: %v\n%s", err, out)
	}
}

// typeSwitchDefers is a program whose type switches' cases defer code
// reading the case's variable, in a loop, left by return, continue and
// break, and a defer that switches; it exits 1 or 2 when a check fails.
const typeSwitchDefers = `package main

mut seen int64

func note(n int64) {
    seen = seen * 10 + n
}

func f(a any, xs ...any) int64 {
    defer {
        switch v := a.(type) {
        case int64:
            note(v)
        }
    }
    for _, x := range xs {
        switch v := x.(type) {
        case int64:
            defer {
                note(v)
            }
            if v == 3 {
                return v
            }
            continue
        case string:
            defer {
                note(len(v))
            }
            break
        }
    }
    return 0
}

func main() {
    seen = 0
    r := f(7, 1, 2, 3, 4)
    if r != 3 || seen != 1237 {
        exit(1)
    }
    seen = 0
    r2 := f(5, 1, "ab", 9)
    if r2 != 0 || seen != 125 {
        exit(2)
    }
}
`

// TestTypeSwitchDefersRunUnderSanitizers builds typeSwitchDefers under the
// sanitizers and checks each defer read its case's variable when it ran.
func TestTypeSwitchDefersRunUnderSanitizers(t *testing.T) {
	bin := buildWith(
		t,
		typeSwitchDefers,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Errorf("running: %v\n%s", err, out)
	}
}

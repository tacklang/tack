package cname_test

import (
	"testing"

	"github.com/tacklang/tack/cname"
)

func TestIsIdent(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want bool
	}{
		{"add", true},
		{"_x", true},
		{"x9", true},
		{"tack_4_main", true},
		{"A_Z", true},
		{"", false},
		{"9x", false},
		{"a-b", false},
		{"a b", false},
		{"é", false},
		{"a\x00", false},
		{"a.b", false},
	} {
		if got := cname.IsIdent(tc.s); got != tc.want {
			t.Errorf("IsIdent(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestIsKeyword(t *testing.T) {
	for _, kw := range []string{"int", "while", "bool", "true", "asm", "typeof", "_Bool", "restrict"} {
		if !cname.IsKeyword(kw) {
			t.Errorf("IsKeyword(%q) = false, want true", kw)
		}
	}
	for _, s := range []string{"add", "main", "int64_t", "printf", "Int", "whiles", ""} {
		if cname.IsKeyword(s) {
			t.Errorf("IsKeyword(%q) = true, want false", s)
		}
	}
}

package lexer

import "testing"

// TestKeywordSpellingsAreDistinct checks no two keywords share a spelling,
// which LookupKeyword's reverse map relies on, and that LookupKeyword finds
// each keyword by its spelling and nothing else.
func TestKeywordSpellingsAreDistinct(t *testing.T) {
	seen := make(map[string]KeywordType)
	for kw, name := range keywordNames {
		if other, ok := seen[name]; ok {
			t.Errorf("%v and %v are both spelled %q", other, kw, name)
		}
		seen[name] = kw
		if got, ok := LookupKeyword(name); !ok || got != kw {
			t.Errorf("LookupKeyword(%q) = %v, %v, want %v, true", name, got, ok, kw)
		}
	}
	for _, ident := range []string{"", "x", "Func", "funcs", "fun", " func", "func\x00"} {
		if got, ok := LookupKeyword(ident); ok || got != InvalidKeyword {
			t.Errorf("LookupKeyword(%q) = %v, %v, want no keyword", ident, got, ok)
		}
	}
}

package cname

// IsIdent reports whether s is a C identifier: one or more ASCII letters,
// digits and underscores, not starting with a digit.
func IsIdent(s string) bool {
	if s == "" || isDigit(s[0]) {
		return false
	}
	for i := range len(s) {
		if b := s[i]; b != '_' && !isLetter(b) && !isDigit(b) {
			return false
		}
	}
	return true
}

// isLetter reports whether b is an ASCII letter.
func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// isDigit reports whether b is an ASCII digit.
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// IsKeyword reports whether s is a C keyword, which no declaration can be
// named: see the package doc for which words those are.
func IsKeyword(s string) bool {
	return keywords[s]
}

// keywords holds C23's keywords, C11's among them, and asm and typeof,
// which clang reserves in its GNU modes.
var keywords = map[string]bool{
	"alignas": true, "alignof": true, "asm": true, "auto": true, "bool": true,
	"break": true, "case": true, "char": true, "const": true, "constexpr": true,
	"continue": true, "default": true, "do": true, "double": true, "else": true,
	"enum": true, "extern": true, "false": true, "float": true, "for": true,
	"goto": true, "if": true, "inline": true, "int": true, "long": true,
	"nullptr": true, "register": true, "restrict": true, "return": true,
	"short": true, "signed": true, "sizeof": true, "static": true,
	"static_assert": true, "struct": true, "switch": true, "thread_local": true,
	"true": true, "typedef": true, "typeof": true, "typeof_unqual": true,
	"union": true, "unsigned": true, "void": true, "volatile": true, "while": true,
	"_Alignas": true, "_Alignof": true, "_Atomic": true, "_BitInt": true,
	"_Bool": true, "_Complex": true, "_Decimal128": true, "_Decimal32": true,
	"_Decimal64": true, "_Generic": true, "_Imaginary": true, "_Noreturn": true,
	"_Static_assert": true, "_Thread_local": true,
}

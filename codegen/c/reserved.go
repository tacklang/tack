package c

import "fmt"

// reservation is a name tack.h cannot give a type of package main or an
// extern func, because tack.h, std's tack.h or a standard header tack.h
// includes defines it, and what defines it, as a diagnostic describes it.
type reservation struct {
	what string
	// objectMacro is whether the name is an object-like macro, which
	// replaces the name wherever it appears, so no field of a struct tack.h
	// defines can have it either. A function-like macro expands only where
	// its name is followed by (, which neither a member's declaration nor
	// p.name is, and a typedef name does not clash with a member's.
	objectMacro bool
}

// reservedNames holds every name reserved in tack.h: tack.h's include
// guard; what std's tack.h defines, its include guard, tack_string and
// the string helpers; and the macros the C standard specifies that
// <stdbool.h>, <stdint.h> and <stddef.h> define, C23's and Annex K's
// among them. Each is reserved whether or not tack.h includes the header
// that defines it, so whether a name is allowed does not depend on what
// some extern signature uses.
//
// Of the standard headers' macros, bool, true and false are left out: they
// are C keywords, reported as such. For the macros named after a width N,
// N is 8, 16, 32 and 64, the widths every target clang builds for has; a
// target with other exact-width types defines more.
var reservedNames = func() map[string]reservation {
	names := map[string]reservation{
		guardName:        {what: "tack.h's include guard", objectMacro: true},
		stdGuardName:     {what: "the include guard of std's tack.h", objectMacro: true},
		"tack_string":    {what: "tack.h's string type"},
		"TACK_C_STR_LEN": {what: "a tack.h string helper"},
		"TACK_STR":       {what: "a tack.h string helper"},
		"TACK_STR_N":     {what: "a tack.h string helper"},
	}
	add := func(header string, object bool, list ...string) {
		for _, name := range list {
			names[name] = reservation{what: "a macro of <" + header + ">", objectMacro: object}
		}
	}
	add("stdbool.h", true, "__bool_true_false_are_defined")
	add("stddef.h", true, "NULL")
	add("stddef.h", false, "offsetof", "unreachable")
	for _, n := range []int{8, 16, 32, 64} {
		for _, kind := range []string{"", "_LEAST", "_FAST"} {
			// INT8, INT_LEAST8, INT_FAST8 and their unsigned twins.
			signed := fmt.Sprintf("INT%s%d", kind, n)
			unsigned := fmt.Sprintf("UINT%s%d", kind, n)
			add("stdint.h", true,
				signed+"_MIN", signed+"_MAX", signed+"_WIDTH",
				unsigned+"_MAX", unsigned+"_WIDTH")
		}
		add("stdint.h", false, fmt.Sprintf("INT%d_C", n), fmt.Sprintf("UINT%d_C", n))
	}
	add("stdint.h", true,
		"INTPTR_MIN", "INTPTR_MAX", "INTPTR_WIDTH", "UINTPTR_MAX", "UINTPTR_WIDTH",
		"INTMAX_MIN", "INTMAX_MAX", "INTMAX_WIDTH", "UINTMAX_MAX", "UINTMAX_WIDTH",
		"PTRDIFF_MIN", "PTRDIFF_MAX", "PTRDIFF_WIDTH",
		"SIG_ATOMIC_MIN", "SIG_ATOMIC_MAX", "SIG_ATOMIC_WIDTH",
		"SIZE_MAX", "SIZE_WIDTH", "RSIZE_MAX",
		"WCHAR_MIN", "WCHAR_MAX", "WCHAR_WIDTH",
		"WINT_MIN", "WINT_MAX", "WINT_WIDTH")
	add("stdint.h", false, "INTMAX_C", "UINTMAX_C")
	return names
}()

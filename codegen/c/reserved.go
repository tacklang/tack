package c

import (
	"fmt"

	"github.com/tacklang/tack/types"
)

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
// the string helpers, tack_error and the error helpers, tack_any, its
// type ids and its helpers, tack_allocator, TACK_C_ALLOCATOR, the list
// functions and macros and c_allocator's testing hooks; and the
// macros the C standard specifies that <stdbool.h>, <stdint.h> and
// <stddef.h> define, C23's and Annex K's among them. Each is reserved whether or not tack.h includes the header
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
		"tack_any":       {what: "tack.h's any type"},
		"TACK_ANY_IS":    {what: "a tack.h any helper"},
		"tack_error":     {what: "tack.h's error type"},
		"TACK_ERROR":     {what: "a tack.h error helper"},
		"TACK_NO_ERROR":  {what: "a tack.h error helper", objectMacro: true},

		"tack_allocator":               {what: "tack.h's allocator type"},
		"TACK_C_ALLOCATOR":             {what: "a tack.h allocator helper", objectMacro: true},
		"tack_list_grow_":              {what: "a tack.h list function"},
		"tack_list_free_":              {what: "a tack.h list function"},
		"tack_c_allocator_live_":       {what: "a tack.h allocator function"},
		"tack_c_allocator_fail_after_": {what: "a tack.h allocator function"},
		"TACK_LIST_LEN":                {what: "a tack.h list helper"},
		"TACK_LIST_AT":                 {what: "a tack.h list helper"},
		"TACK_LIST_APPEND":             {what: "a tack.h list helper"},
		"TACK_LIST_APPEND_N":           {what: "a tack.h list helper"},
		"TACK_LIST_RESERVE":            {what: "a tack.h list helper"},
		"TACK_LIST_POP":                {what: "a tack.h list helper"},
		"TACK_LIST_REMOVE":             {what: "a tack.h list helper"},
	}
	for _, id := range anyIDs {
		names[id] = reservation{what: "a tack.h any type id", objectMacro: true}
	}
	names["TACK_TYPE_INVALID"] = reservation{what: "a tack.h any type id", objectMacro: true}
	for typ := range anyIDs {
		names["tack_any_"+(*types.Table)(nil).Name(typ)] = reservation{what: "a tack.h any helper"}
	}
	names["tack_any_signed"] = reservation{what: "a tack.h any helper"}
	names["tack_any_unsigned"] = reservation{what: "a tack.h any helper"}
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

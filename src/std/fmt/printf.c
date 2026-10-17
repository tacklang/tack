// printf: std/fmt's formatted printing, the body of fmt.printf. Its
// arguments arrive as a view of tack_any values, each a type id and a
// pointer to a value of that type, valid for the call only: they are read
// through std's tack.h unpack helpers, which check the id first, and
// never written or kept.
//
// The format is read byte by byte. A % starts a verb, which is the UTF-8
// sequence after it, as long as its first byte says, so an unknown verb
// that is not ASCII is written back whole; the compiler's check of a
// known format reads it the same way. Integers are formatted here, into a
// buffer on the stack, rather than by C's printf, so the only thing taken
// from the C library is the one function that writes bytes, out: a target
// with no C library replaces that alone.
//
// Every mistake in the format is written into the output, as Go's fmt
// writes it, and none is undefined: %!d(string=hi) for a verb given a
// value it does not take, or an unknown verb; %!d(MISSING) for a verb
// with no argument left; %!(EXTRA int64=5, bool=true) after the output
// for arguments no verb took; %!(NOVERB) for a % that ends the format. An
// argument whose id is no type's, which Tack never passes, is written as
// invalid, with no value, and a view %v does not take, any but a []uint8,
// as its type alone, %!v([]int64), since printf writes no view's elements
// but bytes.
//
// A []uint8 is written as a string's bytes, for %s and %v. The any holds a
// pointer to the view, a tack_view_uint8, which the program's tack.h
// declares because std/fmt's own write takes one.
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

// out writes the n bytes at p to standard output, nothing for n == 0, so
// p may then be null, as a zero string's bytes are.
static void out(const unsigned char *p, int64_t n) {
    if (n > 0) {
        fwrite(p, 1, (size_t)n, stdout);
    }
}

// out_part writes the bytes of s from from up to to. The pointer is moved
// only when there is something to write, since a zero string's is null,
// which C does not let even 0 be added to.
static void out_part(tack_string s, int64_t from, int64_t to) {
    if (to > from) {
        out(s.ptr + from, to - from);
    }
}

// out_lit writes the string literal lit.
#define out_lit(lit) out((const unsigned char *)(lit), (int64_t)(sizeof (lit) - 1))

// type_name returns the Tack name of the type id, or "invalid" for an id
// that is no type's.
static const char *type_name(uint32_t id) {
    static const char *const names[] = {
        [TACK_TYPE_BOOL] = "bool",
        [TACK_TYPE_INT8] = "int8",
        [TACK_TYPE_INT16] = "int16",
        [TACK_TYPE_INT32] = "int32",
        [TACK_TYPE_INT64] = "int64",
        [TACK_TYPE_UINT8] = "uint8",
        [TACK_TYPE_UINT16] = "uint16",
        [TACK_TYPE_UINT32] = "uint32",
        [TACK_TYPE_UINT64] = "uint64",
        [TACK_TYPE_UNSAFE_SIZE_T] = "unsafe_size_t",
        [TACK_TYPE_UNSAFE_UINTPTR_T] = "unsafe_uintptr_t",
        [TACK_TYPE_STRING] = "string",
        [TACK_TYPE_VIEW_BOOL] = "[]bool",
        [TACK_TYPE_VIEW_INT8] = "[]int8",
        [TACK_TYPE_VIEW_INT16] = "[]int16",
        [TACK_TYPE_VIEW_INT32] = "[]int32",
        [TACK_TYPE_VIEW_INT64] = "[]int64",
        [TACK_TYPE_VIEW_UINT8] = "[]uint8",
        [TACK_TYPE_VIEW_UINT16] = "[]uint16",
        [TACK_TYPE_VIEW_UINT32] = "[]uint32",
        [TACK_TYPE_VIEW_UINT64] = "[]uint64",
        [TACK_TYPE_VIEW_UNSAFE_SIZE_T] = "[]unsafe_size_t",
        [TACK_TYPE_VIEW_UNSAFE_UINTPTR_T] = "[]unsafe_uintptr_t",
        [TACK_TYPE_VIEW_STRING] = "[]string",
    };
    if (id == TACK_TYPE_INVALID || id > TACK_TYPE_VIEW_STRING) {
        return "invalid";
    }
    return names[id];
}

// out_cstr writes the zero-terminated s.
static void out_cstr(const char *s) {
    int64_t n = 0;
    while (s[n] != 0) {
        n++;
    }
    out((const unsigned char *)s, n);
}

// out_uint writes v in base 10 or 16, lowercase, after a - when negative
// is true. 20 digits hold the largest uint64_t in base 10, 16 in base 16.
static void out_uint(uint64_t v, unsigned base, bool negative) {
    static const char digits[] = "0123456789abcdef";
    unsigned char buf[21];
    int i = (int)sizeof buf;
    do {
        buf[--i] = (unsigned char)digits[v % base];
        v /= base;
    } while (v != 0);
    if (negative) {
        buf[--i] = '-';
    }
    out(buf + i, (int64_t)sizeof buf - i);
}

// out_int writes a, an integer of any type, in base, and reports whether
// a held an integer. A negative value's magnitude is taken as 0 - its bits
// in uint64_t, which is right for the smallest int64_t too, whose
// magnitude no int64_t holds.
static bool out_int(tack_any a, unsigned base) {
    int64_t s;
    uint64_t u;
    if (tack_any_signed(a, &s)) {
        out_uint(s < 0 ? 0 - (uint64_t)s : (uint64_t)s, base, s < 0);
        return true;
    }
    if (tack_any_unsigned(a, &u)) {
        out_uint(u, base, false);
        return true;
    }
    return false;
}

// out_bytes writes the bytes of a, a []uint8 or a string, and reports
// whether a held one of those. A view's pointer is null when it has no
// elements, which out never moves.
static bool out_bytes(tack_any a) {
    tack_string s;
    if (TACK_ANY_IS(a, VIEW_UINT8)) {
        tack_view_uint8 b = *(const tack_view_uint8 *)a.ptr;
        out(b.ptr, b.len);
        return true;
    }
    if (tack_any_string(a, &s)) {
        out(s.ptr, s.len);
        return true;
    }
    return false;
}

// out_value writes a as %v does, and reports whether a held a type %v
// takes: every basic type an any can hold, and a []uint8.
static bool out_value(tack_any a) {
    bool b;
    if (tack_any_bool(a, &b)) {
        if (b) {
            out_lit("true");
        } else {
            out_lit("false");
        }
        return true;
    }
    if (out_bytes(a)) {
        return true;
    }
    return out_int(a, 10);
}

// takes_value reports whether %v takes a's type, without writing it.
static bool takes_value(tack_any a) {
    return (a.type != TACK_TYPE_INVALID && a.type <= TACK_TYPE_STRING) ||
           TACK_ANY_IS(a, VIEW_UINT8);
}

// out_verb writes a as the verb at v, n bytes long, says, and reports
// whether the verb is one printf knows and takes a's type.
static bool out_verb(const unsigned char *v, int64_t n, tack_any a) {
    uint8_t c;
    if (n != 1) {
        return false;
    }
    switch (v[0]) {
    case 'd':
        return out_int(a, 10);
    case 'x':
        return out_int(a, 16);
    case 's':
        return out_bytes(a);
    case 't':
        return TACK_ANY_IS(a, BOOL) && out_value(a);
    case 'c':
        if (!tack_any_uint8(a, &c)) {
            return false;
        }
        out(&c, 1);
        return true;
    case 'v':
        return out_value(a);
    default:
        return false;
    }
}

// out_typed writes a's type name, then = and a as %v writes it, or the
// name alone for a type %v does not take, invalid for an id that is no
// type's: what a marker shows of a value.
static void out_typed(tack_any a) {
    out_cstr(type_name(a.type));
    if (takes_value(a)) {
        out_lit("=");
        out_value(a);
    }
}

// verb_len returns the length of the verb starting with the byte b, with
// n bytes left in the format: the length of the UTF-8 sequence b starts,
// 2, 3 or 4 for a leading byte and 1 for any other, at most n.
static int64_t verb_len(unsigned char b, int64_t n) {
    int64_t len = 1;
    if (b >= 0xF8) {
        len = 1;
    } else if (b >= 0xF0) {
        len = 4;
    } else if (b >= 0xE0) {
        len = 3;
    } else if (b >= 0xC0) {
        len = 2;
    }
    return len < n ? len : n;
}

void tack_fmt_printf(tack_string format, tack_view_any args) {
    const unsigned char *f = format.ptr;
    int64_t next = 0;  // the argument the next verb takes
    int64_t start = 0; // the first byte not written yet
    int64_t i = 0;
    while (i < format.len) {
        if (f[i] != '%') {
            i++;
            continue;
        }
        out_part(format, start, i);
        i++;
        if (i == format.len) {
            out_lit("%!(NOVERB)");
            start = i;
            break;
        }
        int64_t n = verb_len(f[i], format.len - i);
        const unsigned char *verb = f + i;
        i += n;
        start = i;
        if (n == 1 && verb[0] == '%') {
            out_lit("%");
            continue;
        }
        if (next >= args.len) {
            out_lit("%!");
            out(verb, n);
            out_lit("(MISSING)");
            continue;
        }
        tack_any a = args.ptr[next++];
        if (!out_verb(verb, n, a)) {
            out_lit("%!");
            out(verb, n);
            out_lit("(");
            out_typed(a);
            out_lit(")");
        }
    }
    out_part(format, start, i);
    if (next < args.len) {
        out_lit("%!(EXTRA ");
        for (int64_t k = next; k < args.len; k++) {
            if (k > next) {
                out_lit(", ");
            }
            out_typed(args.ptr[k]);
        }
        out_lit(")");
    }
}

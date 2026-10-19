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
//
// A float is written as Go's fmt writes it: %v and %g as the shortest
// decimal that reads back as the same float, %e and %f with six digits
// after the point, NaN as NaN and the infinities as +Inf and -Inf. The
// digits are worked out exactly, on a decimal of up to 800 digits, as
// Go's strconv does on its slow path (see out_float), so they depend on
// no C library and no locale; std/conv's append_float is the same
// algorithm in Tack, and the tests check the two against each other and
// against Go.
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
        [TACK_TYPE_FLOAT32] = "float32",
        [TACK_TYPE_FLOAT64] = "float64",
        [TACK_TYPE_VIEW_FLOAT32] = "[]float32",
        [TACK_TYPE_VIEW_FLOAT64] = "[]float64",
    };
    if (id == TACK_TYPE_INVALID || id > TACK_TYPE_VIEW_FLOAT64) {
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

// decimal is a nonnegative number in decimal: d[:nd] are its digits,
// ASCII '0' to '9', with no trailing zero, and the point is dp digits from
// d's start, before it when dp is negative; nd of 0 is zero. trunc says
// nonzero digits past the 800 d holds were dropped, which rounding half to
// even counts as more than half. Every float64 has at most 767
// significant digits, so a float's value is always held exactly.
typedef struct {
    unsigned char d[800];
    int nd;
    int dp;
    bool trunc;
} decimal;

// trim drops a's trailing zeros, and makes its point 0 when no digit is
// left.
static void trim(decimal *a) {
    while (a->nd > 0 && a->d[a->nd - 1] == '0') {
        a->nd--;
    }
    if (a->nd == 0) {
        a->dp = 0;
    }
}

// assign makes a the integer v.
static void assign(decimal *a, uint64_t v) {
    unsigned char buf[24];
    int n = 0;
    while (v > 0) {
        uint64_t q = v / 10;
        buf[n++] = (unsigned char)('0' + (v - 10 * q));
        v = q;
    }
    a->nd = 0;
    a->trunc = false;
    while (n > 0) {
        a->d[a->nd++] = buf[--n];
    }
    a->dp = a->nd;
    trim(a);
}

// right_shift divides a by 2^k, k at most 60: it reads digits until their
// value reaches 2^k, then writes a quotient digit for each digit read,
// carrying the remainder, and finally the remainder's digits, as many as
// fit, setting trunc for a nonzero one that does not.
static void right_shift(decimal *a, unsigned k) {
    int r = 0;
    int w = 0;
    uint64_t n = 0;
    for (; (n >> k) == 0; r++) {
        if (r >= a->nd) {
            if (n == 0) {
                a->nd = 0;
                return;
            }
            while ((n >> k) == 0) {
                n *= 10;
                r++;
            }
            break;
        }
        n = n * 10 + (uint64_t)(a->d[r] - '0');
    }
    a->dp -= r - 1;
    uint64_t mask = (UINT64_C(1) << k) - 1;
    for (; r < a->nd; r++) {
        unsigned char c = a->d[r];
        a->d[w++] = (unsigned char)('0' + (n >> k));
        n = (n & mask) * 10 + (uint64_t)(c - '0');
    }
    while (n > 0) {
        uint64_t q = n >> k;
        n &= mask;
        if (w < (int)sizeof a->d) {
            a->d[w++] = (unsigned char)('0' + q);
        } else if (q > 0) {
            a->trunc = true;
        }
        n *= 10;
    }
    a->nd = w;
    trim(a);
}

// left_shift multiplies a by 2^k, k at most 60, from its last digit up,
// into a buffer long enough for the at most 19 digits the product gains,
// then copies back as many digits as fit, setting trunc for a nonzero one
// that does not.
static void left_shift(decimal *a, unsigned k) {
    unsigned char tmp[820];
    int w = (int)sizeof tmp;
    uint64_t n = 0;
    for (int r = a->nd - 1; r >= 0; r--) {
        n += (uint64_t)(a->d[r] - '0') << k;
        uint64_t q = n / 10;
        tmp[--w] = (unsigned char)('0' + (n - 10 * q));
        n = q;
    }
    while (n > 0) {
        uint64_t q = n / 10;
        tmp[--w] = (unsigned char)('0' + (n - 10 * q));
        n = q;
    }
    a->dp += (int)sizeof tmp - w - a->nd;
    int nd = 0;
    for (int i = w; i < (int)sizeof tmp; i++) {
        if (nd < (int)sizeof a->d) {
            a->d[nd++] = tmp[i];
        } else if (tmp[i] != '0') {
            a->trunc = true;
        }
    }
    a->nd = nd;
    trim(a);
}

// shift multiplies a by 2^k, dividing for a negative k, at most 60 places
// at a time.
static void shift(decimal *a, int k) {
    if (a->nd == 0) {
        return;
    }
    for (; k > 60; k -= 60) {
        left_shift(a, 60);
    }
    for (; k < -60; k += 60) {
        right_shift(a, 60);
    }
    if (k > 0) {
        left_shift(a, (unsigned)k);
    } else if (k < 0) {
        right_shift(a, (unsigned)-k);
    }
}

// should_round_up reports whether rounding a to nd digits rounds up: the
// first digit dropped is 5 or more, but for exactly half, a 5 and no digit
// after it, which rounds up past dropped digits and otherwise to even.
static bool should_round_up(const decimal *a, int nd) {
    if (nd < 0 || nd >= a->nd) {
        return false;
    }
    if (a->d[nd] == '5' && nd + 1 == a->nd) {
        if (a->trunc) {
            return true;
        }
        return nd > 0 && (a->d[nd - 1] - '0') % 2 != 0;
    }
    return a->d[nd] >= '5';
}

// round_down truncates a to nd digits.
static void round_down(decimal *a, int nd) {
    if (nd < 0 || nd >= a->nd) {
        return;
    }
    a->nd = nd;
    trim(a);
}

// round_up rounds a up to nd digits: the last kept digit that is not 9
// gains one, and a run of nines becomes a 1 one place further up.
static void round_up(decimal *a, int nd) {
    if (nd < 0 || nd >= a->nd) {
        return;
    }
    for (int i = nd - 1; i >= 0; i--) {
        if (a->d[i] < '9') {
            a->d[i]++;
            a->nd = i + 1;
            return;
        }
    }
    a->d[0] = '1';
    a->nd = 1;
    a->dp++;
}

// round rounds a to nd digits, to nearest, half to even.
static void round_to(decimal *a, int nd) {
    if (should_round_up(a, nd)) {
        round_up(a, nd);
    } else {
        round_down(a, nd);
    }
}

// round_shortest rounds d, the exact value of the float mant *
// 2^(exp - mantbits), to the fewest digits that still read back as that
// float, as Go's roundShortest does: it finds the halfway points to the
// floats on either side, lower and upper, which a round to even reading
// would still take to this float when its mantissa is even, and walks d's
// digits until one differs from both bounds, then rounds there, to
// nearest, or the one way the bounds allow.
static void round_shortest(decimal *d, uint64_t mant, int exp, int mantbits, int bias) {
    if (mant == 0) {
        d->nd = 0;
        return;
    }
    int minexp = bias + 1;
    if (exp > minexp && 332 * (d->dp - d->nd) >= 100 * (exp - mantbits)) {
        return; // d is already as short as it can be.
    }
    decimal upper;
    assign(&upper, mant * 2 + 1);
    shift(&upper, exp - mantbits - 1);
    uint64_t mantlo = mant - 1;
    int explo = exp;
    if (mant <= UINT64_C(1) << mantbits && exp != minexp) {
        mantlo = mant * 2 - 1;
        explo = exp - 1;
    }
    decimal lower;
    assign(&lower, mantlo * 2 + 1);
    shift(&lower, explo - mantbits - 1);
    bool inclusive = mant % 2 == 0;
    int upperdelta = 0;
    for (int ui = 0;; ui++) {
        int mi = ui - upper.dp + d->dp;
        if (mi >= d->nd) {
            break;
        }
        int li = ui - upper.dp + lower.dp;
        unsigned char l = li >= 0 && li < lower.nd ? lower.d[li] : '0';
        unsigned char m = mi >= 0 ? d->d[mi] : '0';
        unsigned char u = ui < upper.nd ? upper.d[ui] : '0';
        bool okdown = l != m || (inclusive && li + 1 == lower.nd);
        if (upperdelta == 0 && m + 1 < u) {
            upperdelta = 2;
        } else if (upperdelta == 0 && m != u) {
            upperdelta = 1;
        } else if (upperdelta == 1 && (m != '9' || u != '0')) {
            upperdelta = 2;
        }
        bool okup = upperdelta > 0 && (inclusive || upperdelta > 1 || ui + 1 < upper.nd);
        if (okdown && okup) {
            round_to(d, mi + 1);
            return;
        }
        if (okdown) {
            round_down(d, mi + 1);
            return;
        }
        if (okup) {
            round_up(d, mi + 1);
            return;
        }
    }
}

// out_zeros writes n zeros.
static void out_zeros(int n) {
    for (int i = 0; i < n; i++) {
        out_lit("0");
    }
}

// out_e writes d with prec digits after the point and an exponent, as
// %e does: -d.ddddde±dd, the exponent of at least two digits, 0 having
// exponent 0.
static void out_e(const decimal *d, bool neg, int prec) {
    if (neg) {
        out_lit("-");
    }
    out(d->nd != 0 ? d->d : (const unsigned char *)"0", 1);
    if (prec > 0) {
        out_lit(".");
        int m = d->nd < prec + 1 ? d->nd : prec + 1;
        if (m > 1) {
            out(d->d + 1, m - 1);
        }
        out_zeros(prec + 1 - (m > 1 ? m : 1));
    }
    int exp = d->nd == 0 ? 0 : d->dp - 1;
    if (exp < 0) {
        out_lit("e-");
        exp = -exp;
    } else {
        out_lit("e+");
    }
    if (exp >= 100) {
        out_uint((uint64_t)exp, 10, false);
    } else {
        unsigned char two[2] = {(unsigned char)('0' + exp / 10), (unsigned char)('0' + exp % 10)};
        out(two, 2);
    }
}

// out_f writes d with prec digits after the point, as %f does:
// -ddd.dddddd, the integer part 0 when it has no digit.
static void out_f(const decimal *d, bool neg, int prec) {
    if (neg) {
        out_lit("-");
    }
    if (d->dp > 0) {
        int m = d->nd < d->dp ? d->nd : d->dp;
        out(d->d, m);
        out_zeros(d->dp - m);
    } else {
        out_lit("0");
    }
    if (prec > 0) {
        out_lit(".");
        int lz = -d->dp > 0 ? -d->dp : 0;
        if (lz > prec) {
            lz = prec;
        }
        int off = d->dp + lz;
        int m = d->nd - off > 0 ? d->nd - off : 0;
        if (m > prec - lz) {
            m = prec - lz;
        }
        out_zeros(lz);
        if (m > 0) {
            out(d->d + off, m);
        }
        out_zeros(prec - lz - m);
    }
}

// out_float writes v, a float64, or a float32 when f32 says so, in the
// format fmt, 'e', 'f' or 'g', with prec digits, or, for a negative prec,
// the fewest that read back as v, as Go's strconv.AppendFloat writes it:
// %g as %e when its exponent is below -4 or at least the precision, 6 for
// the shortest form, and as %f otherwise.
static void out_float(double v, bool f32, unsigned char fmt, int prec) {
    uint64_t bits;
    int mantbits;
    int expbits;
    int bias;
    if (f32) {
        union {
            float f;
            uint32_t u;
        } x = {(float)v};
        bits = x.u;
        mantbits = 23;
        expbits = 8;
        bias = -127;
    } else {
        union {
            double f;
            uint64_t u;
        } x = {v};
        bits = x.u;
        mantbits = 52;
        expbits = 11;
        bias = -1023;
    }
    bool neg = (bits >> (expbits + mantbits)) != 0;
    int exp = (int)(bits >> mantbits) & ((1 << expbits) - 1);
    uint64_t mant = bits & ((UINT64_C(1) << mantbits) - 1);
    if (exp == (1 << expbits) - 1) {
        if (mant != 0) {
            out_lit("NaN");
        } else if (neg) {
            out_lit("-Inf");
        } else {
            out_lit("+Inf");
        }
        return;
    }
    if (exp == 0) {
        exp++;
    } else {
        mant |= UINT64_C(1) << mantbits;
    }
    exp += bias;
    decimal d;
    assign(&d, mant);
    shift(&d, exp - mantbits);
    bool shortest = prec < 0;
    if (shortest) {
        round_shortest(&d, mant, exp, mantbits, bias);
        prec = fmt == 'e' ? (d.nd > 1 ? d.nd - 1 : 0) : fmt == 'f' ? (d.nd - d.dp > 0 ? d.nd - d.dp : 0) : d.nd;
    } else if (fmt == 'e') {
        round_to(&d, prec + 1);
    } else if (fmt == 'f') {
        round_to(&d, d.dp + prec);
    } else {
        prec = prec == 0 ? 1 : prec;
        round_to(&d, prec);
    }
    if (fmt == 'g') {
        int eprec = shortest ? 6 : (prec > d.nd && d.nd >= d.dp ? d.nd : prec);
        int x = d.dp - 1;
        if (x < -4 || x >= eprec) {
            fmt = 'e';
            prec = (prec > d.nd ? d.nd : prec) - 1;
        } else {
            fmt = 'f';
            prec = (prec > d.dp ? d.nd : prec) - d.dp;
            prec = prec > 0 ? prec : 0;
        }
    }
    if (fmt == 'e') {
        out_e(&d, neg, prec);
    } else {
        out_f(&d, neg, prec);
    }
}

// out_float_any writes a, a float32 or a float64, as out_float does in
// the format fmt with prec digits, and reports whether a held a float.
static bool out_float_any(tack_any a, unsigned char fmt, int prec) {
    float f;
    double d;
    if (tack_any_float32(a, &f)) {
        out_float(f, true, fmt, prec);
        return true;
    }
    if (tack_any_float64(a, &d)) {
        out_float(d, false, fmt, prec);
        return true;
    }
    return false;
}

// out_value writes a as %v does, and reports whether a held a type %v
// takes: every basic type an any can hold, and a []uint8.
static bool out_value(tack_any a) {
    bool b;
    if (out_float_any(a, 'g', -1)) {
        return true;
    }
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
           TACK_ANY_IS(a, FLOAT32) || TACK_ANY_IS(a, FLOAT64) || TACK_ANY_IS(a, VIEW_UINT8);
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
    case 'g':
        return out_float_any(a, 'g', -1);
    case 'e':
        return out_float_any(a, 'e', 6);
    case 'f':
        return out_float_any(a, 'f', 6);
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

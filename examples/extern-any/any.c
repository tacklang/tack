// The C half of examples/extern-any. Each any arrives as a tack_any, a
// type id and a pointer to a value of that type; a variadic ...any as a
// view of them. C checks the id before it reads, with TACK_ANY_IS or an
// unpack helper, reads only during the call, and neither writes through
// the pointer nor keeps it.
#include "tack.h"

uint32_t kind(tack_any a) {
    return a.type;
}

bool holds_int64(tack_any a) {
    return TACK_ANY_IS(a, INT64);
}

// is_string reports whether a holds a string of n bytes equal to p's.
static bool is_string(tack_any a, const char *p, int64_t n) {
    tack_string s;
    if (!tack_any_string(a, &s) || s.len != n) {
        return false;
    }
    for (int64_t i = 0; i < n; i++) {
        if (s.ptr[i] != (unsigned char)p[i]) {
            return false;
        }
    }
    return true;
}

bool edges(tack_view_any args) {
    bool b = true;
    int8_t i8 = 0;
    int16_t i16 = 0;
    int32_t i32 = 0;
    int64_t i64 = 0;
    uint8_t u8 = 0;
    uint16_t u16 = 0;
    uint32_t u32 = 0;
    uint64_t u64 = 0;
    size_t size = 0;
    uintptr_t addr = 1;
    return args.len == 13 &&
        tack_any_bool(args.ptr[0], &b) && !b &&
        tack_any_int8(args.ptr[1], &i8) && i8 == INT8_MIN &&
        tack_any_int16(args.ptr[2], &i16) && i16 == INT16_MIN &&
        tack_any_int32(args.ptr[3], &i32) && i32 == INT32_MIN &&
        tack_any_int64(args.ptr[4], &i64) && i64 == INT64_MIN &&
        tack_any_uint8(args.ptr[5], &u8) && u8 == UINT8_MAX &&
        tack_any_uint16(args.ptr[6], &u16) && u16 == UINT16_MAX &&
        tack_any_uint32(args.ptr[7], &u32) && u32 == UINT32_MAX &&
        tack_any_uint64(args.ptr[8], &u64) && u64 == UINT64_MAX &&
        tack_any_unsafe_size_t(args.ptr[9], &size) && size == 4294967295u &&
        tack_any_unsafe_uintptr_t(args.ptr[10], &addr) && addr == 0 &&
        is_string(args.ptr[11], "a\0b", 3) &&
        is_string(args.ptr[12], "", 0);
}

bool mismatches(tack_any a) {
    int refused = 0;
    bool b = true;
    int8_t i8 = 1;
    int16_t i16 = 1;
    int32_t i32 = 1;
    int64_t i64 = 1;
    uint8_t u8 = 1;
    uint16_t u16 = 1;
    uint32_t u32 = 1;
    uint64_t u64 = 1;
    size_t size = 1;
    uintptr_t addr = 1;
    tack_string s = TACK_STR("x");
    refused += !tack_any_bool(a, &b);
    refused += !tack_any_int8(a, &i8);
    refused += !tack_any_int16(a, &i16);
    refused += !tack_any_int32(a, &i32);
    refused += !tack_any_int64(a, &i64);
    refused += !tack_any_uint8(a, &u8);
    refused += !tack_any_uint16(a, &u16);
    refused += !tack_any_uint32(a, &u32);
    refused += !tack_any_uint64(a, &u64);
    refused += !tack_any_unsafe_size_t(a, &size);
    refused += !tack_any_unsafe_uintptr_t(a, &addr);
    refused += !tack_any_string(a, &s);
    // Exactly one helper takes a; every other left its value alone.
    int untouched = (b == true) + (i8 == 1) + (i16 == 1) + (i32 == 1) + (i64 == 1) +
        (u8 == 1) + (u16 == 1) + (u32 == 1) + (u64 == 1) + (size == 1) + (addr == 1) +
        (s.len == 1);
    return refused == 11 && untouched >= 11;
}

int64_t sum_signed(tack_view_any args) {
    uint64_t sum = 0;
    for (int64_t i = 0; i < args.len; i++) {
        int64_t v;
        if (tack_any_signed(args.ptr[i], &v)) {
            sum += (uint64_t)v;
        }
    }
    return (int64_t)sum;
}

uint64_t sum_unsigned(tack_view_any args) {
    uint64_t sum = 0;
    for (int64_t i = 0; i < args.len; i++) {
        uint64_t v;
        if (tack_any_unsigned(args.ptr[i], &v)) {
            sum += v;
        }
    }
    return sum;
}

int64_t count(tack_view_any args) {
    return args.len;
}

int64_t set_then_read(int64_t *x, tack_any a) {
    *x = 99;
    int64_t v;
    if (!tack_any_int64(a, &v)) {
        return -1;
    }
    return v;
}

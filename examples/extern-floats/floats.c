// The C half of examples/extern-floats. A float32 arrives as a float and
// a float64 as a double, by value, alone, in a struct or an array, or as
// a pointer for a mut parameter; a view of float64 as tack_view_float64,
// which tack.h declares. Nothing here uses the C library: the square root
// is Newton's method, and the infinity and the NaN are clang's builtins.
// An any holding a float has its own type id, read with TACK_ANY_IS or
// the unpack helpers, tack_any_float32 and tack_any_float64.
#include "tack.h"

double ext_root(double x) {
    if (x < 0) {
        return __builtin_nan("");
    }
    if (x == 0) {
        return 0;
    }
    double g = x > 1 ? x : 1;
    for (int i = 0; i < 200; i++) {
        double next = (g + x / g) / 2;
        if (next >= g) {
            break;
        }
        g = next;
    }
    return g;
}

Vec ext_scale(Vec v, float k) {
    return (Vec){v.x * k, v.y * k};
}

double ext_dot(tack_view_float64 a, tack_view_float64 b) {
    int64_t n = a.len < b.len ? a.len : b.len;
    double sum = 0;
    for (int64_t i = 0; i < n; i++) {
        sum += a.ptr[i] * b.ptr[i];
    }
    return sum;
}

void ext_halve(Vec *v) {
    v->x /= 2;
    v->y /= 2;
}

float ext_sum4(tack_array_4_float32 a) {
    float sum = 0;
    for (int i = 0; i < 4; i++) {
        sum += a.e[i];
    }
    return sum;
}

Sample ext_bump(Sample s) {
    s.value *= 2;
    s.count++;
    return s;
}

double ext_infinity(void) {
    return __builtin_inf();
}

float ext_nan32(void) {
    return __builtin_nanf("");
}

uint32_t ext_kind(tack_any a) {
    return a.type;
}

double ext_twice(tack_any a) {
    double d = 0;
    if (!tack_any_float64(a, &d)) {
        return -1;
    }
    return d * 2;
}

float ext_halves(tack_any a) {
    float f = 0;
    if (!tack_any_float32(a, &f)) {
        return -1;
    }
    return f / 2;
}

double ext_first(tack_any a) {
    if (!TACK_ANY_IS(a, VIEW_FLOAT64)) {
        return -1;
    }
    tack_view_float64 v = *(const tack_view_float64 *)a.ptr;
    if (v.len == 0) {
        return -1;
    }
    return v.ptr[0];
}

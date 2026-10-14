// The C half of examples/extern-views. Each view arrives as a struct of a
// pointer to its first element and its length; the function reads the
// elements during the call, and neither writes through the pointer nor
// keeps it.
#include "tack.h"

int64_t total(tack_view_int64 v) {
    int64_t sum = 0;
    for (int64_t i = 0; i < v.len; i++) {
        sum += v.ptr[i];
    }
    return sum;
}

int64_t countByte(tack_view_uint8 b, uint8_t c) {
    int64_t n = 0;
    for (int64_t i = 0; i < b.len; i++) {
        if (b.ptr[i] == c) {
            n++;
        }
    }
    return n;
}

int64_t sumYs(tack_view_Point v) {
    int64_t sum = 0;
    for (int64_t i = 0; i < v.len; i++) {
        sum += v.ptr[i].y;
    }
    return sum;
}

int64_t sum_cells(tack_view_tack_array_2_int64 v) {
    int64_t sum = 0;
    for (int64_t i = 0; i < v.len; i++) {
        sum += v.ptr[i].e[0] + v.ptr[i].e[1];
    }
    return sum;
}

int64_t lengths(tack_view_string v) {
    int64_t n = 0;
    for (int64_t i = 0; i < v.len; i++) {
        n += v.ptr[i].len;
    }
    return n;
}

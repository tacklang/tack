// The C half of examples/extern-variadics. Each variadic parameter
// arrives as a view, a struct of a pointer to the first value and the
// number of values; the function reads the values during the call, and
// neither writes through the pointer nor keeps it. With no values, len is
// 0 and ptr is not read.
#include "tack.h"

int64_t total(tack_view_int64 nums) {
    uint64_t sum = 0;
    for (int64_t i = 0; i < nums.len; i++) {
        sum += (uint64_t)nums.ptr[i];
    }
    return (int64_t)sum;
}

int64_t sum_lengths(int64_t base, tack_view_string words) {
    int64_t n = base;
    for (int64_t i = 0; i < words.len; i++) {
        n += words.ptr[i].len;
    }
    return n;
}

bool is_empty(tack_view_int64 nums) {
    return nums.len == 0;
}

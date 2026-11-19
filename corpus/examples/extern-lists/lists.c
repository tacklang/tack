// The C half of examples/extern-lists. A list arrives as a pointer to the
// caller's tack_list_ struct: C reads its length, capacity and elements,
// writes its elements, appends to it and reserves room in it through
// std's macros, only during the call, and never frees its buffer, changes
// its allocator or keeps the pointer.
// A view arrives as a pointer to its first element and its length.
#include "tack.h"

bool count_up(tack_list_int64 *l, int64_t n) {
    for (int64_t i = 1; i <= n; i++) {
        if (!TACK_LIST_APPEND(l, i)) {
            return false;
        }
    }
    return true;
}

void double_all(tack_list_int64 *l) {
    for (int64_t i = 0; i < TACK_LIST_LEN(*l); i++) {
        TACK_LIST_AT(*l, i) *= 2;
    }
}

bool add_point(tack_list_Point *l, int64_t x, int64_t y) {
    Point p = {x, y};
    return TACK_LIST_APPEND(l, p);
}

bool append_three(tack_list_int64 *l, int64_t a, int64_t b, int64_t c) {
    const int64_t values[3] = {a, b, c};
    return TACK_LIST_APPEND_N(l, values, 3);
}

bool append_view(tack_list_int64 *l, tack_view_int64 v) {
    return TACK_LIST_APPEND_N(l, v.ptr, v.len);
}

bool reserve_room(tack_list_int64 *l, int64_t n) {
    return TACK_LIST_RESERVE(l, n);
}

int64_t list_cap(tack_list_int64 *l) {
    return l->cap;
}

int64_t sum_view(tack_view_int64 v) {
    int64_t sum = 0;
    for (int64_t i = 0; i < v.len; i++) {
        sum += v.ptr[i];
    }
    return sum;
}

int64_t extern_lists_live(void) {
    return tack_c_allocator_live_();
}

void extern_lists_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

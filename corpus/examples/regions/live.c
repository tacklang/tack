// The C half of examples/regions: the program's window on c_allocator's
// testing hooks, which std's tack.h declares and std's C defines, so the
// example can count the chunks a region takes and make c_allocator refuse
// them; C reading and growing a region's list; and a check, in a
// destructor that runs as the process ends, after exit, that c_allocator
// holds no buffer by then: every chunk, and the region table, was given
// back.
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

int64_t regions_live(void) {
    return tack_c_allocator_live_();
}

void regions_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

int64_t regions_sum(tack_view_int64 v) {
    int64_t sum = 0;
    for (int64_t i = 0; i < v.len; i++) {
        sum += v.ptr[i];
    }
    return sum;
}

bool regions_push(tack_list_int64 *l, int64_t x) {
    return TACK_LIST_APPEND(l, x);
}

// regions_check_nothing_live prints whether c_allocator holds no buffer
// as the process ends. It runs after exit has freed every buffer, and
// after stdout's last write from Tack, which it follows.
__attribute__((destructor)) static void regions_check_nothing_live(void) {
    int64_t n = tack_c_allocator_live_();
    if (n == 0) {
        fputs("nothing live at process end ok\n", stdout);
    } else {
        fprintf(stdout, "nothing live at process end FAIL: %lld live\n", (long long)n);
    }
    fflush(stdout);
}

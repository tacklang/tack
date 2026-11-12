// The C half of examples/pools: the program's window on c_allocator's
// testing hooks, which std's tack.h declares and std's C defines, so the
// example can assert that remove and drop free what they free at once,
// that nothing is freed twice, and make an allocation fail on purpose;
// and a check, in a destructor that runs as the process ends, after the
// exit main ends with while a pool is still held, that c_allocator holds
// no buffer by then.
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

int64_t pools_live(void) {
    return tack_c_allocator_live_();
}

void pools_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

// pools_check_nothing_live prints whether c_allocator holds no buffer as
// the process ends. It runs after exit has freed every buffer still held.
__attribute__((destructor)) static void pools_check_nothing_live(void) {
    int64_t n = tack_c_allocator_live_();
    if (n == 0) {
        fputs("nothing live at process end ok\n", stdout);
    } else {
        fprintf(stdout, "nothing live at process end FAIL: %lld live\n", (long long)n);
    }
    fflush(stdout);
}

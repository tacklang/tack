// The C half of examples/big: c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can make an
// allocation fail on purpose and assert that every buffer an Int made
// was freed.
#include <tack/tack.h>

#include "tack.h"

int64_t big_live(void) {
    return tack_c_allocator_live_();
}

void big_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

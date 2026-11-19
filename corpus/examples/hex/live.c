// The C half of examples/hex: c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can make an
// allocation fail on purpose and assert that every buffer it made was
// freed.
#include <tack/tack.h>

#include "tack.h"

int64_t hex_live(void) {
    return tack_c_allocator_live_();
}

void hex_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

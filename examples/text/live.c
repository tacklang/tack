// The C half of examples/text: c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can assert that
// every buffer a Text made or grew was freed exactly once, and make an
// allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t text_live(void) {
    return tack_c_allocator_live_();
}

void text_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

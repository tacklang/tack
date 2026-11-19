// The C half of examples/closures: the program's window on c_allocator's
// testing hooks, which std's tack.h declares and std's C defines, so the
// example can assert that a closure reading a captured list copies and
// frees nothing, that lists a closure makes or replaces are freed exactly
// once, and make an allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t closures_live(void) {
    return tack_c_allocator_live_();
}

void closures_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

// The C half of examples/owned-lists: the program's window on
// c_allocator's testing hooks, which std's tack.h declares and std's C
// defines, so the example can assert that every element a list held was
// freed exactly once, and make an allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t owned_live(void) {
    return tack_c_allocator_live_();
}

void owned_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

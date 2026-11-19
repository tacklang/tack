// The C half of examples/own-params: the program's window on
// c_allocator's testing hooks, which std's tack.h declares and std's C
// defines, so the example can assert that every value moved into a
// function by an own parameter is freed exactly once, by whoever owns it
// last, and make an allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t own_live(void) {
    return tack_c_allocator_live_();
}

void own_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

// The C half of examples/bytes: what Tack cannot see of a list, its
// capacity, read through a mut parameter during the call only, and
// c_allocator's testing hooks, which std's tack.h declares and std's C
// defines, so the example can assert that every list std/bytes made or
// grew was freed, and make an allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t bytes_cap(tack_list_uint8 *b) {
    return b->cap;
}

int64_t bytes_live(void) {
    return tack_c_allocator_live_();
}

void bytes_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

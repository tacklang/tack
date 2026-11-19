// The C half of examples/list-bulk: what Tack cannot see of a list, its
// capacity and the address of its buffer, read through a mut parameter
// during the call only, and c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can assert that
// every list it made was freed, and make an allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t bulk_cap(tack_list_int64 *l) {
    return l->cap;
}

int64_t bulk_cap_bytes(tack_list_uint8 *b) {
    return b->cap;
}

uintptr_t bulk_buf(tack_list_int64 *l) {
    return (uintptr_t)l->ptr;
}

int64_t bulk_live(void) {
    return tack_c_allocator_live_();
}

void bulk_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

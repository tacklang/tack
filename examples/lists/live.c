// The C half of examples/lists: the program's window on c_allocator's
// testing hooks, which std's tack.h declares and std's C defines, so the
// example can assert that every list it made was freed, and make an
// allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t lists_live(void) {
    return tack_c_allocator_live_();
}

void lists_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

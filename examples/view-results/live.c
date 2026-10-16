// The C half of examples/view-results: the program's window on
// c_allocator's testing hook, which std's tack.h declares and std's C
// defines, so the example can assert that reading a view a call returns
// allocates nothing, and that every list was freed exactly once.
#include <tack/tack.h>

#include "tack.h"

int64_t viewres_live(void) {
    return tack_c_allocator_live_();
}

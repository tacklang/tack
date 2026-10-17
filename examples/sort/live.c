// The C half of examples/sort: c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can assert that
// sorting owned elements frees nothing and leaks nothing.
#include <tack/tack.h>

#include "tack.h"

int64_t sort_live(void) {
    return tack_c_allocator_live_();
}

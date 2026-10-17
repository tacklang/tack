// The C half of examples/list-equality: c_allocator's count of live
// buffers, which std's tack.h declares and std's C defines, so the
// example can assert that comparing lists allocates nothing and that
// every list it made was freed exactly once.
#include <tack/tack.h>

#include "tack.h"

int64_t listeq_live(void) {
    return tack_c_allocator_live_();
}

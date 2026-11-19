// The C half of examples/unions: the program's window on c_allocator's
// testing hook, which std's tack.h declares and std's C defines, so the
// example can assert that every list a union held was freed exactly once.
#include <tack/tack.h>

#include "tack.h"

int64_t unions_live(void) {
    return tack_c_allocator_live_();
}

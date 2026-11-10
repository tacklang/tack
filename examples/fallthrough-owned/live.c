// The C half of examples/fallthrough-owned: the program's window on
// c_allocator's testing hooks, which std's tack.h declares and std's C
// defines, so the example can assert that every list a case made was
// freed exactly once, whether its body was left by a fallthrough, a
// return or a ? that failed.
#include <tack/tack.h>

#include "tack.h"

int64_t ftowned_live(void) {
    return tack_c_allocator_live_();
}

// The C half of examples/question-owned: the program's window on
// c_allocator's testing hooks, which std's tack.h declares and std's C
// defines, so the example can assert that every owned value a ? left
// behind was freed exactly once.
#include <tack/tack.h>

#include "tack.h"

int64_t qowned_live(void) {
    return tack_c_allocator_live_();
}

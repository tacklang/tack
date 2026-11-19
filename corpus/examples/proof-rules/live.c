// The C half of examples/proof-rules: the program's window on
// c_allocator's count of live buffers, which std's tack.h declares and
// std's C defines, so the example can assert that a pop or a remove
// written as a statement frees the element it drops.
#include <tack/tack.h>

#include "tack.h"

int64_t proof_live(void) {
    return tack_c_allocator_live_();
}

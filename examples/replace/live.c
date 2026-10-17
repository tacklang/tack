// The C half of examples/replace: the program's window on c_allocator's
// count of live buffers, which std's tack.h declares and std's C defines,
// so the example can assert that every value replace moves out or in is
// freed exactly once.
#include <tack/tack.h>

#include "tack.h"

int64_t replace_live(void) {
    return tack_c_allocator_live_();
}

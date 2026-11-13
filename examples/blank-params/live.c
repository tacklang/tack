// The C half of examples/blank-params: the program's window on
// c_allocator's count of live buffers, which std's tack.h declares and
// std's C defines, so the example can assert that what an own parameter
// named _ is given is freed exactly once, and an extern func whose
// Tack declaration names a parameter _.
#include <tack/tack.h>

#include "tack.h"

int64_t go_live(void) {
    return tack_c_allocator_live_();
}

int64_t go_second(int64_t first, int64_t second) {
    (void)first;
    return second;
}

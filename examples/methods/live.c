// The C half of examples/methods: the program's window on c_allocator's
// testing hook, which std's tack.h declares and std's C defines, so the
// example can assert that every list its methods made was freed.
#include <tack/tack.h>

#include "tack.h"

int64_t methods_live(void) {
    return tack_c_allocator_live_();
}

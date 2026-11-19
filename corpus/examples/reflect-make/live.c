// The C half of examples/reflect-make: the program's window on
// c_allocator's testing hooks, which std's tack.h declares and std's C
// defines, so the example can assert that every buffer the values make
// gives, and the values filled and cloned from them, hold was freed
// exactly once, and make an allocation fail on purpose.
#include <tack/tack.h>

#include "tack.h"

int64_t go_live(void) {
    return tack_c_allocator_live_();
}

void go_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

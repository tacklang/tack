// The C half of examples/tack-test/fail: a buffer of c_allocator's held
// past the end of the test that took it, which no Tack code can do, so
// the runner's check of what a test leaves allocated has something to
// find; and c_allocator's hook that makes it refuse, which the runner
// resets after every test. exit frees the buffer, if no test released
// it, as it frees every buffer c_allocator holds.
#include <tack/tack.h>

#include "tack.h"

// held is the buffer fail_hold took, or null.
static void *held = 0;

void fail_hold(void) {
    int64_t cap = 0;
    if (held == 0) {
        held = tack_list_grow_(0, 0, 8, 1, 1, TACK_C_ALLOCATOR, &cap);
    }
}

void fail_release(void) {
    if (held != 0) {
        tack_list_free_(held, 8, 1, 1, TACK_C_ALLOCATOR);
        held = 0;
    }
}

void fail_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

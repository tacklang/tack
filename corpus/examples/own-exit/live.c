// The C half of examples/own-exit: the program's window on c_allocator's
// count of live buffers, and a check, in a destructor that runs as the
// process ends, after exit, that c_allocator holds no buffer by then.
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

int64_t own_exit_live(void) {
    return tack_c_allocator_live_();
}

// own_exit_check_nothing_live prints whether c_allocator holds no buffer
// as the process ends. It runs after exit has freed every buffer, and
// after stdout's last write from Tack, which it follows.
__attribute__((destructor)) static void own_exit_check_nothing_live(void) {
    int64_t n = tack_c_allocator_live_();
    if (n == 0) {
        fputs("nothing live at process end ok\n", stdout);
    } else {
        fprintf(stdout, "nothing live at process end FAIL: %lld live\n", (long long)n);
    }
    fflush(stdout);
}

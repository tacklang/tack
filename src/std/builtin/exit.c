#include <stdlib.h>

#include <tack/tack.h>

// exit: end the process, running no defers. Every allocator first frees
// every buffer it still holds, so nothing the program allocated outlives
// it, whatever still held it. The status is an int32_t, which C's exit
// takes as an int with no narrowing; only its low bits reach the OS.
void tack_builtin_exit(int32_t code) {
    tack_allocators_release_all_();
    exit(code);
}

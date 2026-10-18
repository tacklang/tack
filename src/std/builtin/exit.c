#include <stdlib.h>

#include <tack/tack.h>

// exit: end the process, running no defers. Every allocator first frees
// every buffer it still holds, so nothing the program allocated outlives
// it, whatever still held it. The status is an int32_t, which C's exit
// takes as an int with no narrowing; only its low bits reach the OS. It
// never returns, which _Noreturn tells C, as the generated C's prototype
// does: a function with a result may end in a call to it.
_Noreturn void tack_builtin_exit(int32_t code) {
    tack_allocators_release_all_();
    exit(code);
}

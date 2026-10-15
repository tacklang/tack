#include <stdlib.h>

#include <tack/tack.h>

// exit: end the process, running no defers. The status is an int32_t,
// which C's exit takes as an int with no narrowing; only its low bits
// reach the OS.
void tack_builtin_exit(int32_t code) {
    exit(code);
}

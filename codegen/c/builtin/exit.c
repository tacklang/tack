#include <stdlib.h>

// exit: end the process, running no defers. C's exit takes an int, so the
// status is narrowed in the open; only its low bits reach the OS.
static void tack_builtin_exit(int64_t code) {
    exit((int)code);
}

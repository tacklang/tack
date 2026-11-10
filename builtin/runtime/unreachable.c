#include <stdio.h>
#include <stdlib.h>

#include <tack/tack.h>

// unreachable: the abort of an unreachable statement the checker proved
// no run reaches, so reaching one is a compiler bug. It writes msg, which
// TACK_UNREACHABLE builds and which already ends in a newline, to stderr,
// frees every allocator's buffers, as exit does, so nothing the program
// allocated outlives it, and aborts, running no defers. It never returns,
// which _Noreturn tells C: a function with a result may end in it.
_Noreturn void tack_unreachable_(const char *msg) {
    fputs(msg, stderr);
    tack_allocators_release_all_();
    abort();
}

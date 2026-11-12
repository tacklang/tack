#include <errno.h>
#include <stdio.h>
#include <stdlib.h>

#include <tack/tack.h>

// exit: end the process, running no defers. stdout is flushed first, here
// rather than by C's exit, which would not report its failure: a flush
// that finds stdout's pipe with no reader ends the process through
// tack_stdio_failed_, with status 141 rather than code; errno is cleared
// before it, as before every checked write. Exit then marks the process
// as ending (tack_ending_), so a destructor that writes during C's exit
// and finds the broken pipe cannot call exit again. Every allocator
// then frees every buffer it still holds, so nothing the program
// allocated outlives it, whatever still held it. The status is an
// int32_t, which C's exit takes as an int with no narrowing; only its low
// bits reach the OS. It never returns, which _Noreturn tells C, as the
// generated C's prototype does: a function with a result may end in a
// call to it.
_Noreturn void tack_builtin_exit(int32_t code) {
    errno = 0;
    if (fflush(stdout) == EOF) {
        tack_stdio_failed_();
    }
    tack_ending_();
    tack_allocators_release_all_();
    exit(code);
}

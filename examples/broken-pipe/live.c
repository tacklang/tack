// The C half of examples/broken-pipe: the program's window on
// c_allocator's count of live buffers, a note written to descriptor 3,
// and a check, in a destructor that runs as the process ends, that
// c_allocator holds no buffer by then.
//
// Descriptor 3 is the test's report pipe, apart from standard output and
// standard error, either of which a case breaks. Each note and the
// check's line are one write(2) of a few bytes, which a pipe takes whole;
// a write that fails, or a descriptor 3 that is not open, as when the
// program runs with nothing there, loses the line, and the test, which
// reads the pipe, then finds it missing.
#include <errno.h>
#include <stdio.h>
#include <unistd.h>
#include <wchar.h>

#include <tack/tack.h>

#include "tack.h"

int64_t pipe_live(void) {
    return tack_c_allocator_live_();
}

// pipe_wide makes stdout wide-oriented, so every byte write to it fails,
// glibc's fwrite returning 0 without setting errno, and leaves errno
// EPIPE, as an earlier failed write to some other pipe would.
void pipe_wide(void) {
    (void)fwide(stdout, 1);
    errno = EPIPE;
}

void pipe_note(tack_view_uint8 s) {
    if (s.len > 0) {
        (void)write(3, s.ptr, (size_t)s.len);
    }
}

// pipe_check_nothing_live writes to descriptor 3 whether c_allocator
// holds no buffer as the process ends: after a return from main, after
// exit, and after the end a broken pipe makes, which frees every buffer
// as exit does.
__attribute__((destructor)) static void pipe_check_nothing_live(void) {
    static const char ok[] = "nothing live at process end ok\n";
    static const char fail[] = "nothing live at process end FAIL\n";
    if (tack_c_allocator_live_() == 0) {
        (void)write(3, ok, sizeof ok - 1);
    } else {
        (void)write(3, fail, sizeof fail - 1);
    }
}

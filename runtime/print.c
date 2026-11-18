#include <errno.h>
#include <stdio.h>

#include <tack/tack.h>

// print: write s to stdout exactly as given. An empty string is not
// written: a zeroed one holds a null pointer, which C does not let fwrite
// take even with nothing to write. A write that fails is handed to
// tack_stdio_failed_, which ends the process when stdout's pipe has no
// reader, and otherwise loses the bytes. errno is cleared first, so a
// write that fails without setting it, as fwrite on a wide-oriented
// stream does, is not taken for an earlier call's EPIPE.
void tack_builtin_print(tack_string s) {
    if (s.len <= 0) {
        return;
    }
    errno = 0;
    if (fwrite(s.ptr, 1, (size_t)s.len, stdout) < (size_t)s.len) {
        tack_stdio_failed_();
    }
}

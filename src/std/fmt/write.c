// write: std/fmt's byte output, the bodies of fmt.write and fmt.ewrite.
// The bytes arrive as a tack_view_uint8, a pointer to the first and their
// number, valid for the call only: they are read, never written or kept.
//
// write writes them with one fwrite to C's stdout, the stream print and
// printf write through, so the output of the three interleaves in the
// order of the calls. ewrite does the same to C's stderr, the stream
// eprintf writes through, flushing both ways around it, as every write to
// standard error does: stdout first, as C++ ties cerr to cout, so what
// was written to it before is ahead, and stderr after, so nothing of the
// write is left for a later stdout write to overtake. So where both
// streams reach one place, their bytes arrive there in the order of the
// calls, however the C library buffers either stream. Every byte
// is written as it is, 0 and 0xff included, and none is added. A view of
// nothing may have a null pointer, which C does not let fwrite take even
// with nothing to write, so an empty view writes nothing, though ewrite
// still flushes both streams. Like print, neither reports an error: a
// write or flush of either stream that fails is handed to
// tack_stdio_failed_, which ends the process as exit(141) does when the
// stream's pipe has no reader, as print's failures are, and otherwise
// loses the bytes, as for a closed descriptor. errno is cleared before
// each call, so a failure that sets none, as fwrite on a wide-oriented
// stream, is not taken for an earlier call's EPIPE. A partial or interrupted
// write is left to C's stdio, as print leaves it.
#include <errno.h>
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

// put writes the bytes of b to f, nothing for an empty view, handing a
// failure to tack_stdio_failed_.
static void put(tack_view_uint8 b, FILE *f) {
    if (b.len <= 0) {
        return;
    }
    errno = 0;
    if (fwrite(b.ptr, 1, (size_t)b.len, f) < (size_t)b.len) {
        tack_stdio_failed_();
    }
}

// flush flushes f, handing a failure to tack_stdio_failed_.
static void flush(FILE *f) {
    errno = 0;
    if (fflush(f) == EOF) {
        tack_stdio_failed_();
    }
}

void tack_fmt_write(tack_view_uint8 b) {
    put(b, stdout);
}

void tack_fmt_ewrite(tack_view_uint8 b) {
    flush(stdout);
    put(b, stderr);
    flush(stderr);
}

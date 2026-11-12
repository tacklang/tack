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
// still flushes both streams. Like print, neither reports an error: what the
// stream does with the bytes is not checked, so a closed descriptor or a
// failed write loses them, and a pipe with no reader raises SIGPIPE, as
// it does for print. A partial or interrupted write is left to C's
// stdio, as print leaves it.
#include <stdio.h>

#include "tack.h"

// put writes the bytes of b to f, nothing for an empty view.
static void put(tack_view_uint8 b, FILE *f) {
    if (b.len > 0) {
        fwrite(b.ptr, 1, (size_t)b.len, f);
    }
}

void tack_fmt_write(tack_view_uint8 b) {
    put(b, stdout);
}

void tack_fmt_ewrite(tack_view_uint8 b) {
    fflush(stdout);
    put(b, stderr);
    fflush(stderr);
}

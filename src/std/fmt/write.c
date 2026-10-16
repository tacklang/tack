// write: std/fmt's byte output, the body of fmt.write. The bytes arrive as
// a tack_view_uint8, a pointer to the first and their number, valid for
// the call only: they are read, never written or kept.
//
// They are written with one fwrite to C's stdout, the stream print and
// printf write through, so the output of the three interleaves in the
// order of the calls. Every byte is written as it is, 0 and 0xff
// included, and none is added. A view of nothing may have a null
// pointer, which C does not let fwrite take even with nothing to write,
// so an empty view writes nothing and calls nothing. Like print, it
// reports no error: what stdout does with the bytes is not checked.
#include <stdio.h>

#include "tack.h"

void tack_fmt_write(tack_view_uint8 b) {
    if (b.len > 0) {
        fwrite(b.ptr, 1, (size_t)b.len, stdout);
    }
}

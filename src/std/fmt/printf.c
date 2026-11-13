// printf: std/fmt's formatted printing, the body of fmt.printf and of
// fmt.eprintf, which differ only in their stream, stdout or stderr. Both
// format through format.h, std/fmt's formatter, which writes to a sink:
// the stream and, for stderr, a buffer the call's output is gathered in.
// format.h says how a format is read and how each value is written.
#include <stdio.h>

#include "format.h"

// tack_fmt_printf is printf's body: format to stdout, each piece handed
// to it as it is made.
void tack_fmt_printf(tack_string format, tack_view_any args) {
    sink o = {.f = stdout, .buf = NULL, .cap = 0, .n = 0};
    format_to(&o, format, args);
}

// tack_fmt_eprintf is eprintf's body: format to stderr, gathered so a
// short call is one write, flushing both ways around it: stdout first, so
// what was written to it before the call is ahead of the call's output
// where both streams reach one place, and stderr after, so nothing of
// the call is left for a later stdout write to overtake, however the C
// library buffers stderr.
void tack_fmt_eprintf(tack_string format, tack_view_any args) {
    flush_stream(stdout);
    unsigned char buf[gather_cap];
    sink o = {.f = stderr, .buf = buf, .cap = gather_cap, .n = 0};
    format_to(&o, format, args);
    flush_stream(stderr);
}

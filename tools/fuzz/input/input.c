// The C half of package input: the input libFuzzer is running, which
// tools/fuzz/entry.c sets, read into a Tack list, and the abort a
// target reports a broken invariant with.
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include "fuzz.h"
#include "tack.h"

// running says an input is set; data and size are that input, which
// libFuzzer owns and keeps valid for the run.
static bool running;
static const uint8_t *data;
static size_t size;

void fuzz_set_input(const uint8_t *d, size_t n) {
    data = d;
    size = n;
    running = true;
}

void fuzz_clear_input(void) {
    data = NULL;
    size = 0;
    running = false;
}

// fuzz_input appends the running input's bytes to out, all or none,
// and returns true; or false when no input is running, the input is too
// large for an int64_t count, or out cannot grow.
bool fuzz_input(tack_list_uint8 *out) {
    if (!running || size > (size_t)INT64_MAX) {
        return false;
    }
    return TACK_LIST_APPEND_N(out, data, (int64_t)size);
}

// put_escaped writes the bytes of v to standard error, all of them by
// its length, so a 0 byte among them does not end them, up to 4096: a
// printable ASCII byte as itself but for the backslash, written \\, a
// line feed as itself, since a message holds several lines, a tab as \t,
// and any other byte as \x and two hex digits. Past 4096 bytes it writes
// ... instead of the rest.
static void put_escaped(tack_view_uint8 v) {
    int64_t n = v.len < 0 ? 0 : v.len;
    for (int64_t i = 0; i < n && i < 4096; i++) {
        uint8_t c = v.ptr[i];
        if (c == '\\') {
            fputs("\\\\", stderr);
        } else if (c == '\n' || (c >= 0x20 && c < 0x7f)) {
            fputc(c, stderr);
        } else if (c == '\t') {
            fputs("\\t", stderr);
        } else {
            fprintf(stderr, "\\x%02x", c);
        }
    }
    if (n > 4096) {
        fputs("...", stderr);
    }
}

// fuzz_fail writes what and then detail to standard error, framed so it
// is found in libFuzzer's output and escaped (see put_escaped), since
// either may quote the input, and aborts, which libFuzzer reports as a
// crash and saves the input of.
void fuzz_fail(tack_view_uint8 what, tack_view_uint8 detail) {
    fputs("tack fuzz: invariant broken: ", stderr);
    put_escaped(what);
    put_escaped(detail);
    fputc('\n', stderr);
    fflush(stderr);
    abort();
}

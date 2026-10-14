#include <stdio.h>

// print: write s to stdout exactly as given. An empty string is not
// written: a zeroed one holds a null pointer, which C does not let fwrite
// take even with nothing to write.
static void tack_builtin_print(tack_string s) {
    if (s.len > 0) {
        fwrite(s.ptr, 1, s.len, stdout);
    }
}

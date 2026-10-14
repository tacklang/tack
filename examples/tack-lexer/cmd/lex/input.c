// The C half of cmd/lex: reading standard input, which Tack cannot do.
#include <stdio.h>
#include <stdlib.h>

#include "tack.h"

// readStdin reads standard input to its end into one buffer from malloc
// and returns it as a string, with true; or an empty string and false if
// it cannot allocate or read. The buffer is never freed: a string C
// returns must stay valid for the rest of the program, and nothing yet
// says who owns it.
tack_results_readStdin readStdin(void) {
    tack_results_readStdin fail = {{NULL, 0}, false};
    size_t cap = 4096;
    size_t len = 0;
    unsigned char *buf = malloc(cap);
    if (buf == NULL) {
        return fail;
    }
    for (;;) {
        if (len == cap) {
            unsigned char *grown = realloc(buf, cap * 2);
            if (grown == NULL) {
                free(buf);
                return fail;
            }
            buf = grown;
            cap *= 2;
        }
        size_t got = fread(buf + len, 1, cap - len, stdin);
        len += got;
        if (got == 0) {
            if (ferror(stdin)) {
                free(buf);
                return fail;
            }
            break;
        }
    }
    return (tack_results_readStdin){TACK_STR_N(buf, len), true};
}

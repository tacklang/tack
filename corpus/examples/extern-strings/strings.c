// The C half of examples/extern-strings. Each function builds its string
// with a helper tack.h defines, so no length is counted by hand. Every
// string's bytes are static: Tack keeps a string C returns for the rest of
// the program.
#include "tack.h"

static const unsigned char letters[] = "abcdefghij";
static unsigned char buf[16];
static const tack_string name = TACK_STR("global");

tack_string literal(void) {
    return TACK_STR("a\0b");
}

tack_string empty(void) {
    return TACK_STR("");
}

tack_string prefix(int64_t n) {
    if (n > (int64_t)TACK_C_STR_LEN(letters)) {
        n = (int64_t)TACK_C_STR_LEN(letters);
    }
    return TACK_STR_N(letters, n);
}

tack_string buffer(void) {
    buf[0] = 'x';
    buf[1] = 'y';
    buf[2] = 'z';
    return TACK_STR_N(buf, 3);
}

int64_t size(void) {
    return (int64_t)TACK_C_STR_LEN("hello");
}

tack_string global(void) {
    return name;
}

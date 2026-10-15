// The C half of examples/extern-errors. Every error is built with
// TACK_ERROR or TACK_NO_ERROR, from std's tack.h; a message must be a
// string whose bytes outlive the program's use of it, as a literal's do.
#include "tack.h"

tack_error fail(int32_t code) {
    if (code == 0) {
        return TACK_NO_ERROR;
    }
    return TACK_ERROR(code, "failed in C");
}

tack_results_half half(int64_t n) {
    if (n % 2 != 0) {
        return (tack_results_half){0, TACK_ERROR(9, "odd")};
    }
    return (tack_results_half){n / 2, TACK_NO_ERROR};
}

int32_t code_of(tack_error e) {
    return e.code;
}

int64_t msg_len(tack_error e) {
    return e.msg.len;
}

void clear(Point *p) {
    p->x = 0;
    p->err = TACK_NO_ERROR;
}

int64_t codes(tack_view_error errs) {
    int64_t sum = 0;
    for (int64_t i = 0; i < errs.len; i++) {
        sum += errs.ptr[i].code;
    }
    return sum;
}

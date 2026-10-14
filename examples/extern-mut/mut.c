// The C half of examples/extern-mut. Each mut parameter arrives as a
// pointer to the caller's variable, which the function may read and write
// during the call, and must not keep.
#include "tack.h"

void fill(int64_t *n, int64_t v) {
    *n = v;
}

void grow(Point *p, int64_t k) {
    p->x *= k;
    p->y *= k;
}

void clear(tack_array_4_int64 *a) {
    for (int i = 0; i < 4; i++) {
        a->e[i] = 0;
    }
}

bool toggle(bool *b) {
    bool was = *b;
    *b = !was;
    return was;
}

void order_pair(int64_t *a, int64_t *b) {
    if (*a > *b) {
        int64_t t = *a;
        *a = *b;
        *b = t;
    }
}

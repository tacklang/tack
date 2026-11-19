// The C half of examples/extern-guide, the worked example of
// docs/extern.md.
#include "tack.h"

int64_t count(tack_string s, uint8_t c) {
    int64_t n = 0;
    for (int64_t i = 0; i < s.len; i++) {
        if (s.ptr[i] == c) {
            n++;
        }
    }
    return n;
}

int64_t point_sum(Point p) {
    return (int64_t)((uint64_t)p.x + (uint64_t)p.y);
}

tack_results_c_divmod c_divmod(int64_t a, int64_t b) {
    return (tack_results_c_divmod){a / b, a % b};
}

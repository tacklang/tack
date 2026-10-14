// The C half of examples/extern-sizes. unsafe_size_t and unsafe_uintptr_t
// arrive as size_t and uintptr_t, whatever their width on the target.
#include "tack.h"

static char object;

size_t sizeOfSpan(void) {
    return sizeof(Span);
}

uintptr_t marker(void) {
    return (uintptr_t)&object;
}

bool isMarker(uintptr_t p) {
    return p == (uintptr_t)&object;
}

Span span(size_t n) {
    return (Span){n, (uintptr_t)&object};
}

tack_results_lengths lengths(tack_array_2_unsafe_size_t a) {
    return (tack_results_lengths){a.e[0] + a.e[1], 2};
}

void halve(size_t *n) {
    *n /= 2;
}

size_t largest(void) {
    return SIZE_MAX;
}

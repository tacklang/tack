// The C half of examples/extern. tack.h, which the compiler generates,
// declares each extern func of main.tck, so a definition here that does
// not match its Tack declaration fails to compile.
#include "tack.h"

int64_t add(int64_t a, int64_t b) {
    return (int64_t)((uint64_t)a + (uint64_t)b);
}

bool flip(bool b) {
    return !b;
}

bool both(bool a, bool b) {
    return a && b;
}

int64_t widen(int8_t x) {
    return x;
}

uint8_t low(uint64_t x) {
    return (uint8_t)(x & 0xff);
}

int64_t halves(int16_t a, uint16_t b, int32_t c, uint32_t d) {
    return (int64_t)(a / 2) + (int64_t)(b / 2) + (int64_t)(c / 2) + (int64_t)(d / 2);
}

uint64_t largest(void) {
    return UINT64_MAX;
}

int64_t smallest(void) {
    return INT64_MIN;
}

static int32_t counter;

void count(void) {
    counter++;
}

int32_t counted(void) {
    return counter;
}

// The C half of examples/extern-values. tack.h declares Point, Empty,
// Shape, the arrays and the results struct with the same layout as the
// Tack types, under the names the C below uses.
#include "tack.h"

int64_t length(tack_string s) {
    return s.len;
}

int64_t sum(tack_string s) {
    int64_t total = 0;
    for (int64_t i = 0; i < s.len; i++) {
        total += s.ptr[i];
    }
    return total;
}

tack_string greeting(void) {
    return TACK_STR("hello from C");
}

int64_t manhattan(Point p) {
    return (p.x < 0 ? -p.x : p.x) + (p.y < 0 ? -p.y : p.y);
}

Point mirror(Point p) {
    return (Point){.x = p.y, .y = p.x};
}

int64_t perimeter(Shape s) {
    int64_t n = 0;
    for (int i = 0; i < 3; i++) {
        Point a = s.corners.e[i], b = s.corners.e[(i + 1) % 3];
        n += manhattan((Point){.x = b.x - a.x, .y = b.y - a.y});
    }
    return s.closed ? n / 2 + s.name.len : 0;
}

Shape grow(Shape s, int64_t by) {
    for (int i = 0; i < 3; i++) {
        s.corners.e[i].x *= by;
        s.corners.e[i].y *= by;
    }
    s.name = TACK_STR("grown");
    s.closed = false;
    return s;
}

int64_t total(tack_array_4_int64 a) {
    int64_t n = 0;
    for (int i = 0; i < 4; i++) {
        n += a.e[i];
    }
    return n;
}

tack_array_4_int64 squares(int64_t n) {
    tack_array_4_int64 a = {{0}};
    for (int64_t i = 0; i <= n && i < 4; i++) {
        a.e[i] = i * i;
    }
    return a;
}

tack_results_nothing nothing(tack_array_0_int64 a, Empty e) {
    return (tack_results_nothing){a, e};
}

tack_results_divmod divmod(int64_t a, int64_t b) {
    if (b == 0) {
        return (tack_results_divmod){0, 0, false};
    }
    return (tack_results_divmod){a / b, a % b, true};
}

tack_array_2_string names(void) {
    return (tack_array_2_string){{TACK_STR("ada"), {0, 0}}};
}

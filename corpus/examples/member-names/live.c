// The C half of examples/member-names: the program's window on
// c_allocator's testing hook, which std's tack.h declares and std's C
// defines, so the example can assert that every buffer was freed exactly
// once, and a reader of a struct whose fields are named like Tack's
// builtins, which tack.h declares under those names.
#include <tack/tack.h>

#include "tack.h"

int64_t names_live(void) {
    return tack_c_allocator_live_();
}

int64_t names_sum(Wire w) {
    return w.int64 + w.len + w.span;
}

// The C half of examples/tack-test/cexit: C's own exit, called from
// inside a test, which ends the process without Tack's exit, so the
// runner cannot know the status it was asked for. Code behind an extern
// func is outside Tack's guarantees; the runner still reports the test.
#include <stdlib.h>

#include "tack.h"

void cexit_quit(int32_t code) {
    exit(code);
}

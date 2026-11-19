// The C half of tools/corpus: closing the descriptors this process got
// from whatever started it, so that no program a run starts inherits
// one (see close_inherited in main.tck).
#include <stdint.h>
#include <sys/resource.h>
#include <sys/syscall.h>
#include <unistd.h>

// corpus_close_inherited closes every descriptor from 3 up, with
// close_range when the kernel has it, and one at a time up to the limit
// of open files otherwise, and returns 0, or 1 when it had to close them
// one at a time.
int64_t corpus_close_inherited(void) {
#ifdef SYS_close_range
    if (syscall(SYS_close_range, 3u, ~0u, 0u) == 0) {
        return 0;
    }
#endif
    struct rlimit lim;
    long top = 65536;
    if (getrlimit(RLIMIT_NOFILE, &lim) == 0 && lim.rlim_cur != RLIM_INFINITY && lim.rlim_cur < 1048576) {
        top = (long)lim.rlim_cur;
    }
    for (long fd = 3; fd < top; fd++) {
        close((int)fd);
    }
    return 1;
}

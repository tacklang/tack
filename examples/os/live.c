// The C half of examples/os: c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can assert that
// every list std/os made or grew was freed exactly once, and make an
// allocation fail on purpose; a note on the standard error for a check
// the machine cannot run; and what its rename and stat checks need that
// std/os does not do: making a symbolic link, finding a second file
// system to rename across, setting a file's size, and finding whether
// a file larger than the disk can be made safely.
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <unistd.h>

#include <tack/tack.h>

#include "tack.h"

int64_t os_live(void) {
    return tack_c_allocator_live_();
}

void os_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

void os_note(tack_string s) {
    fwrite(s.ptr, 1, (size_t)s.len, stderr);
}

// c_str copies the bytes of v and a 0 after them into buf, which holds n
// bytes, and returns 0, or ENAMETOOLONG when they do not fit, or EINVAL
// when v holds a 0 byte.
static int c_str(tack_view_uint8 v, char *buf, int64_t n) {
    if (v.len < 0 || v.len >= n) {
        return ENAMETOOLONG;
    }
    if (v.len > 0) {
        if (memchr(v.ptr, 0, (size_t)v.len) != NULL) {
            return EINVAL;
        }
        memcpy(buf, v.ptr, (size_t)v.len);
    }
    buf[v.len] = 0;
    return 0;
}

int64_t os_symlink(tack_view_uint8 target, tack_view_uint8 path) {
    char t[4096];
    char p[4096];
    int e = c_str(target, t, sizeof t);
    if (e == 0) {
        e = c_str(path, p, sizeof p);
    }
    if (e != 0) {
        return -e;
    }
    if (symlink(t, p) != 0) {
        return -errno;
    }
    return 0;
}

int64_t os_other_fs(tack_view_uint8 dir) {
    char d[4096];
    if (c_str(dir, d, sizeof d) != 0) {
        return 0;
    }
    struct stat a;
    struct stat b;
    if (stat(d, &a) != 0 || stat("/dev/shm", &b) != 0) {
        return 0;
    }
    if (!S_ISDIR(b.st_mode) || a.st_dev == b.st_dev || access("/dev/shm", W_OK) != 0) {
        return 0;
    }
    return 1;
}

int64_t os_truncate(tack_view_uint8 path, int64_t size) {
    char p[4096];
    int e = c_str(path, p, sizeof p);
    if (e != 0) {
        return -e;
    }
    if (truncate(p, (off_t)size) != 0) {
        return -errno;
    }
    return 0;
}

// probe_size is how large os_sparse makes its probe file, and
// probe_most how many bytes of disk the probe may take for its file
// system to count as making holes.
enum { probe_size = 1 << 20, probe_most = 1 << 16 };

int64_t os_sparse(tack_view_uint8 path) {
    struct rlimit rl;
    if (getrlimit(RLIMIT_FSIZE, &rl) != 0 || rl.rlim_cur != RLIM_INFINITY) {
        return 0;
    }
    char p[4096];
    if (c_str(path, p, sizeof p) != 0 || truncate(p, probe_size) != 0) {
        return 0;
    }
    struct stat st;
    if (stat(p, &st) != 0 || st.st_size != probe_size) {
        return 0;
    }
    return (int64_t)st.st_blocks * 512 < probe_most;
}

// The C half of examples/os: c_allocator's testing hooks, which std's
// tack.h declares and std's C defines, so the example can assert that
// every list std/os made or grew was freed exactly once, and make an
// allocation fail on purpose; std/os's error for ENOMEM, which no call
// can be made to give on demand, and for any errno; a note on the
// standard error for a check the machine cannot run; and what its
// rename, stat and run checks need that std/os does not do: making a
// symbolic or hard link, changing the current directory, finding a
// second file system to rename across, setting a file's size or its
// mode, finding whether a file larger than the disk can be made safely,
// and, for its modes checks, setting the umask and reading a file's
// mode.
#include <errno.h>
#include <signal.h>
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

tack_error os_enomem_error(void) {
    return tack_os_error(ENOMEM);
}

tack_error os_errno_error(int64_t e) {
    return tack_os_error(e);
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

int64_t os_link(tack_view_uint8 target, tack_view_uint8 path) {
    char t[4096];
    char p[4096];
    int e = c_str(target, t, sizeof t);
    if (e == 0) {
        e = c_str(path, p, sizeof p);
    }
    if (e != 0) {
        return -e;
    }
    if (link(t, p) != 0) {
        return -errno;
    }
    return 0;
}

int64_t os_chdir(tack_view_uint8 path) {
    char p[4096];
    int e = c_str(path, p, sizeof p);
    if (e != 0) {
        return -e;
    }
    if (chdir(p) != 0) {
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

int64_t os_chmod(tack_view_uint8 path, int64_t mode) {
    char p[4096];
    int e = c_str(path, p, sizeof p);
    if (e != 0) {
        return -e;
    }
    if (chmod(p, (mode_t)mode) != 0) {
        return -errno;
    }
    return 0;
}

int64_t os_umask(int64_t mask) {
    return (int64_t)umask((mode_t)mask);
}

int64_t os_mode(tack_view_uint8 path) {
    char p[4096];
    int e = c_str(path, p, sizeof p);
    if (e != 0) {
        return -e;
    }
    struct stat st;
    if (lstat(p, &st) != 0) {
        return -errno;
    }
    return (int64_t)(st.st_mode & 07777);
}

// fsize_saved and xfsz_saved are the file size limit and SIGXFSZ's
// action before os_limit_fsize set a limit, which os_restore_fsize
// puts back, so no later check, and no child one runs, sees either
// changed.
static struct rlimit fsize_saved;
static struct sigaction xfsz_saved;

int64_t os_limit_fsize(int64_t limit) {
    if (limit < 0 || getrlimit(RLIMIT_FSIZE, &fsize_saved) != 0) {
        return limit < 0 ? -EINVAL : -errno;
    }
    struct sigaction ign;
    memset(&ign, 0, sizeof ign);
    ign.sa_handler = SIG_IGN;
    if (sigaction(SIGXFSZ, &ign, &xfsz_saved) != 0) {
        return -errno;
    }
    struct rlimit rl = fsize_saved;
    rl.rlim_cur = (rlim_t)limit;
    if (setrlimit(RLIMIT_FSIZE, &rl) != 0) {
        int e = errno;
        sigaction(SIGXFSZ, &xfsz_saved, NULL);
        return -e;
    }
    return 0;
}

int64_t os_restore_fsize(void) {
    if (setrlimit(RLIMIT_FSIZE, &fsize_saved) != 0) {
        return -errno;
    }
    if (sigaction(SIGXFSZ, &xfsz_saved, NULL) != 0) {
        return -errno;
    }
    return 0;
}

int64_t os_xfsz_action(void) {
    struct sigaction now;
    if (sigaction(SIGXFSZ, NULL, &now) != 0) {
        return -errno;
    }
    if (now.sa_handler == SIG_DFL) {
        return 0;
    }
    if (now.sa_handler == SIG_IGN) {
        return 1;
    }
    return 2;
}

int64_t os_fsize_limit(void) {
    struct rlimit rl;
    if (getrlimit(RLIMIT_FSIZE, &rl) != 0) {
        return -errno;
    }
    return rl.rlim_cur == RLIM_INFINITY ? -1 : (int64_t)rl.rlim_cur;
}

// os: std/os's C, a thin layer over the POSIX calls std/os's Tack makes.
//
// Each function makes one call into the C library, or a fixed few that
// are one operation, as list_dir's opendir, readdir and closedir are, and
// decides nothing: retrying, looping, sorting, which errors mean what,
// and every allocation's failure are std/os's Tack. A function that can
// fail returns 0 or more on success and the errno, negated, on failure,
// and tack_os_error turns an errno into the error Tack returns.
//
// C allocates nothing. What the OS gives back, a file's bytes, a
// directory's names, an argument or a variable's value, is appended to a
// list Tack owns and passed as mut, with TACK_LIST_APPEND_N, which grows it
// through the list's own allocator, so Tack frees every byte; when that
// append fails, the function returns -no_room, which no errno is, so
// that an allocation that failed is told apart from a kernel ENOMEM. A
// path or a name arrives as a view, its bytes not followed by a 0, and
// is copied, with a 0 after it, into a buffer on the stack for the call;
// std/os's Tack has already refused a 0 byte inside it, and one too long
// for the buffer is ENAMETOOLONG, which the OS would give for most such
// paths anyway.
//
// Every file descriptor and directory stream this file opens is closed on
// every path out of the call that opened it, except the descriptors
// tack_os_open and tack_os_pipe return, which std/os's Tack closes, and
// each is opened close-on-exec, so no child a later call starts inherits
// it. The one thing here the C library may allocate for itself is
// tack_os_spawn's posix_spawn_file_actions_t and posix_spawnattr_t, which
// it destroys on every path.
//
// The runtime ignores SIGPIPE, so a write here to a pipe with no reader
// fails with EPIPE, an error returned, rather than kill the process. An
// ignored signal stays ignored across exec, so tack_os_spawn sets SIGPIPE
// back to its default action in the child, which then behaves as it
// would started from a shell: yes writing into a closed pipe dies of it.
//
// The calls are POSIX.1-2008's, which the C library declares in the mode
// the driver compiles every C file in, clang's default. The file cannot
// ask for them itself with a feature-test macro: the program's tack.h is
// included before its first line, and its headers have already settled
// what the C library declares.

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <poll.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include <tack/tack.h>

#include "tack.h"

// path_max is the size of the buffer a path or a name is copied into,
// its 0 included: PATH_MAX where the C library defines it, 4096 where it
// does not.
#ifdef PATH_MAX
enum { path_max = PATH_MAX };
#else
enum { path_max = 4096 };
#endif

// chunk is the most bytes tack_os_read reads with one call.
enum { chunk = 65536 };

// c_path copies the bytes of p into buf, which holds path_max bytes, and
// a 0 after them, and returns 0; or ENAMETOOLONG when they and the 0 do
// not fit, or EINVAL when p holds a 0 byte, which std/os's Tack checks
// first, so it is never reached from Tack.
static int c_path(tack_view_uint8 p, char *buf) {
    if (p.len < 0 || p.len >= path_max) {
        return ENAMETOOLONG;
    }
    if (p.len > 0) {
        if (memchr(p.ptr, 0, (size_t)p.len) != NULL) {
            return EINVAL;
        }
        memcpy(buf, p.ptr, (size_t)p.len);
    }
    buf[p.len] = 0;
    return 0;
}

// no_room is what a function here returns, negated, when a list Tack
// gave it cannot grow: an allocation of the program's that failed, which
// tack_os_error gives as out of memory. It is no errno on any target os
// builds for, whose errnos are below 256, so a kernel's ENOMEM, the OS
// refusing a call for want of its own memory, stays the OS's error.
enum { no_room = 1 << 20 };

// append_bytes appends the n bytes at p to l and returns 0, or -no_room
// when l cannot grow to hold them, l left as it was.
static int64_t append_bytes(tack_list_uint8 *l, const char *p, size_t n) {
    if (!TACK_LIST_APPEND_N(l, (const uint8_t *)p, (int64_t)n)) {
        return -no_room;
    }
    return 0;
}

int64_t tack_os_open(tack_view_uint8 path, int32_t mode) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    int flags = O_RDONLY | O_CLOEXEC;
    if (mode == 1) {
        flags = O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC;
    }
    int fd = open(buf, flags, 0644);
    if (fd < 0) {
        return -errno;
    }
    return fd;
}

int64_t tack_os_close(int32_t fd) {
    if (close(fd) != 0) {
        return -errno;
    }
    return 0;
}

int64_t tack_os_read(int32_t fd, tack_list_uint8 *l) {
    char buf[chunk];
    ssize_t n = read(fd, buf, sizeof buf);
    if (n < 0) {
        return -errno;
    }
    int64_t e = append_bytes(l, buf, (size_t)n);
    if (e != 0) {
        return e;
    }
    return n;
}

int64_t tack_os_write(int32_t fd, tack_view_uint8 data) {
    if (data.len <= 0) {
        return 0;
    }
    ssize_t n = write(fd, data.ptr, (size_t)data.len);
    if (n < 0) {
        return -errno;
    }
    return n;
}

int64_t tack_os_remove(tack_view_uint8 path) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    if (remove(buf) != 0) {
        return -errno;
    }
    return 0;
}

int64_t tack_os_make_dir(tack_view_uint8 path) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    if (mkdir(buf, 0755) != 0) {
        return -errno;
    }
    return 0;
}

int64_t tack_os_rename(tack_view_uint8 old_path, tack_view_uint8 new_path) {
    char old_buf[path_max];
    char new_buf[path_max];
    int e = c_path(old_path, old_buf);
    if (e == 0) {
        e = c_path(new_path, new_buf);
    }
    if (e != 0) {
        return -e;
    }
    if (rename(old_buf, new_buf) != 0) {
        return -errno;
    }
    return 0;
}

// st_size is an off_t, a signed integer type, which an int64_t holds
// every value of on every target os builds for; the assertions prove it,
// so tack_os_stat's conversion is exact and never wraps.
_Static_assert((off_t)-1 < 0, "off_t is signed");
_Static_assert(sizeof(off_t) <= sizeof(int64_t), "an int64_t holds every off_t");

// fill_info sets info from st: its size, whether it is a directory, and
// its device and inode, which an unsigned 64-bit integer holds on every
// target os builds for, as the assertions prove.
_Static_assert(sizeof(dev_t) <= sizeof(uint64_t), "a uint64_t holds every dev_t");
_Static_assert(sizeof(ino_t) <= sizeof(uint64_t), "a uint64_t holds every ino_t");
static void fill_info(os_Info *info, const struct stat *st) {
    info->length = (int64_t)st->st_size;
    info->directory = S_ISDIR(st->st_mode);
    info->device = (uint64_t)st->st_dev;
    info->inode = (uint64_t)st->st_ino;
    info->found = true;
}

int64_t tack_os_stat(tack_view_uint8 path, os_Info *info) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    struct stat st;
    if (stat(buf, &st) != 0) {
        return -errno;
    }
    fill_info(info, &st);
    return 0;
}

int64_t tack_os_lstat(tack_view_uint8 path, os_Info *info) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    struct stat st;
    if (lstat(buf, &st) != 0) {
        return -errno;
    }
    fill_info(info, &st);
    return 0;
}

int64_t tack_os_getwd(tack_list_uint8 *dir) {
    char buf[path_max];
    if (getcwd(buf, sizeof buf) == NULL) {
        return -errno;
    }
    return append_bytes(dir, buf, strlen(buf));
}

int64_t tack_os_list_dir(tack_view_uint8 path, tack_list_uint8 *names) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    DIR *d = opendir(buf);
    if (d == NULL) {
        return -errno;
    }
    int64_t r = 0;
    for (;;) {
        errno = 0;
        struct dirent *ent = readdir(d);
        if (ent == NULL) {
            if (errno != 0) {
                r = -errno;
            }
            break;
        }
        r = append_bytes(names, ent->d_name, strlen(ent->d_name) + 1);
        if (r != 0) {
            break;
        }
    }
    if (closedir(d) != 0 && r == 0) {
        r = -errno;
    }
    return r;
}

int64_t tack_os_make_temp_dir(tack_list_uint8 *tmpl) {
    if (tmpl->len == 0 || tmpl->ptr[tmpl->len - 1] != 0 ||
        memchr(tmpl->ptr, 0, (size_t)(tmpl->len - 1)) != NULL) {
        return -EINVAL;
    }
    if (mkdtemp((char *)tmpl->ptr) == NULL) {
        return -errno;
    }
    return 0;
}

int64_t tack_os_env(tack_view_uint8 name, tack_list_uint8 *value) {
    char buf[path_max];
    int e = c_path(name, buf);
    if (e != 0) {
        return -e;
    }
    const char *v = getenv(buf);
    if (v == NULL) {
        return 0;
    }
    int64_t r = append_bytes(value, v, strlen(v));
    if (r != 0) {
        return r;
    }
    return 1;
}

int64_t tack_os_argc(void) {
    return tack_argc_;
}

int64_t tack_os_arg(int64_t i, tack_list_uint8 *arg) {
    if (i < 0 || i >= tack_argc_ || tack_argv_[i] == NULL) {
        return -EINVAL;
    }
    return append_bytes(arg, tack_argv_[i], strlen(tack_argv_[i]));
}

// environ is the process's environment, which a child run starts with.
extern char **environ;

// max_args is the most arguments tack_os_spawn passes a child, its
// program's name included; the array of their pointers is on its stack.
enum { max_args = 8192 };

int64_t tack_os_pipe(tack_array_2_int32 *fds) {
    int p[2];
    if (pipe(p) != 0) {
        return -errno;
    }
    // macOS has no pipe2, so close-on-exec is set after; the process runs
    // no other thread that could start a child in between.
    if (fcntl(p[0], F_SETFD, FD_CLOEXEC) != 0 || fcntl(p[1], F_SETFD, FD_CLOEXEC) != 0) {
        int e = errno;
        close(p[0]);
        close(p[1]);
        return -e;
    }
    fds->e[0] = p[0];
    fds->e[1] = p[1];
    return 0;
}

// sigpipe_default makes a child started with attr begin with SIGPIPE's
// default action, though this process ignores it, and returns 0, or the
// errno of the call that failed.
static int sigpipe_default(posix_spawnattr_t *attr) {
    sigset_t sigs;
    if (sigemptyset(&sigs) != 0 || sigaddset(&sigs, SIGPIPE) != 0) {
        return errno;
    }
    int e = posix_spawnattr_setsigdefault(attr, &sigs);
    if (e == 0) {
        e = posix_spawnattr_setflags(attr, POSIX_SPAWN_SETSIGDEF);
    }
    return e;
}

int64_t tack_os_spawn(tack_view_uint8 flat, int64_t argc, int32_t out, int32_t err) {
    if (argc < 1 || argc > max_args) {
        return -E2BIG;
    }
    if (flat.len < argc || flat.ptr[flat.len - 1] != 0) {
        return -EINVAL;
    }
    char *argv[max_args + 1];
    int64_t n = 0;
    int64_t at = 0;
    while (at < flat.len && n < argc) {
        argv[n++] = (char *)flat.ptr + at;
        const uint8_t *end = memchr(flat.ptr + at, 0, (size_t)(flat.len - at));
        at = (int64_t)(end - flat.ptr) + 1;
    }
    if (n != argc || at != flat.len) {
        return -EINVAL;
    }
    argv[n] = NULL;
    posix_spawnattr_t attr;
    int e = posix_spawnattr_init(&attr);
    if (e != 0) {
        return -e;
    }
    posix_spawn_file_actions_t fa;
    e = posix_spawn_file_actions_init(&fa);
    if (e != 0) {
        posix_spawnattr_destroy(&attr);
        return -e;
    }
    e = sigpipe_default(&attr);
    if (e == 0) {
        e = posix_spawn_file_actions_addopen(&fa, 0, "/dev/null", O_RDONLY, 0);
    }
    if (e == 0) {
        e = posix_spawn_file_actions_adddup2(&fa, out, 1);
    }
    if (e == 0) {
        e = posix_spawn_file_actions_adddup2(&fa, err, 2);
    }
    pid_t pid = 0;
    if (e == 0) {
        e = posix_spawnp(&pid, argv[0], &fa, &attr, argv, environ);
    }
    posix_spawn_file_actions_destroy(&fa);
    posix_spawnattr_destroy(&attr);
    if (e != 0) {
        return -e;
    }
    return pid;
}

int64_t tack_os_poll2(int32_t a, int32_t b) {
    struct pollfd p[2] = {{.fd = a, .events = POLLIN}, {.fd = b, .events = POLLIN}};
    if (poll(p, 2, -1) < 0) {
        return -errno;
    }
    int64_t mask = 0;
    if (a >= 0 && p[0].revents != 0) {
        mask += 1;
    }
    if (b >= 0 && p[1].revents != 0) {
        mask += 2;
    }
    return mask;
}

int64_t tack_os_wait(int64_t pid) {
    int status = 0;
    if (waitpid((pid_t)pid, &status, 0) < 0) {
        return -errno;
    }
    if (WIFEXITED(status)) {
        return WEXITSTATUS(status);
    }
    if (WIFSIGNALED(status)) {
        return 256 + WTERMSIG(status);
    }
    return -EINVAL;
}

int64_t tack_os_eintr(void) {
    return EINTR;
}

// tack_os_error returns the error std/os gives for e, an errno or
// no_room: error{code: 1, msg: "out of memory"} for no_room, a list that
// could not grow, as every std allocation failure is, and code 3 and the
// message strerror gives for an errno, ENOMEM's "Cannot allocate memory"
// included, as a literal, since a Tack error's message must outlive every
// use of it, which strerror's text is not promised to. The texts are
// macOS's, given on every target, so an error's message does not depend
// on the C library: glibc's differ for a few, EBUSY's among them. An
// errno not listed is "unknown OS error".
tack_error tack_os_error(int64_t e) {
    switch (e) {
    case no_room:
        return TACK_ERROR(1, "out of memory");
    case ENOMEM:
        return TACK_ERROR(3, "Cannot allocate memory");
    case EPERM:
        return TACK_ERROR(3, "Operation not permitted");
    case ENOENT:
        return TACK_ERROR(3, "No such file or directory");
    case EINTR:
        return TACK_ERROR(3, "Interrupted system call");
    case EIO:
        return TACK_ERROR(3, "Input/output error");
    case E2BIG:
        return TACK_ERROR(3, "Argument list too long");
    case ENOEXEC:
        return TACK_ERROR(3, "Exec format error");
    case EBADF:
        return TACK_ERROR(3, "Bad file descriptor");
    case ECHILD:
        return TACK_ERROR(3, "No child processes");
    case EAGAIN:
        return TACK_ERROR(3, "Resource temporarily unavailable");
    case EACCES:
        return TACK_ERROR(3, "Permission denied");
    case EFAULT:
        return TACK_ERROR(3, "Bad address");
    case EBUSY:
        return TACK_ERROR(3, "Resource busy");
    case EEXIST:
        return TACK_ERROR(3, "File exists");
    case EXDEV:
        return TACK_ERROR(3, "Cross-device link");
    case ENOTDIR:
        return TACK_ERROR(3, "Not a directory");
    case EISDIR:
        return TACK_ERROR(3, "Is a directory");
    case EINVAL:
        return TACK_ERROR(3, "Invalid argument");
    case ENFILE:
        return TACK_ERROR(3, "Too many open files in system");
    case EMFILE:
        return TACK_ERROR(3, "Too many open files");
    case ETXTBSY:
        return TACK_ERROR(3, "Text file busy");
    case EFBIG:
        return TACK_ERROR(3, "File too large");
    case ENOSPC:
        return TACK_ERROR(3, "No space left on device");
    case ESPIPE:
        return TACK_ERROR(3, "Illegal seek");
    case EROFS:
        return TACK_ERROR(3, "Read-only file system");
    case EMLINK:
        return TACK_ERROR(3, "Too many links");
    case EPIPE:
        return TACK_ERROR(3, "Broken pipe");
    case ERANGE:
        return TACK_ERROR(3, "Result too large");
    case ENAMETOOLONG:
        return TACK_ERROR(3, "File name too long");
    case ENOTEMPTY:
        return TACK_ERROR(3, "Directory not empty");
    case ELOOP:
        return TACK_ERROR(3, "Too many levels of symbolic links");
    default:
        return TACK_ERROR(3, "unknown OS error");
    }
}

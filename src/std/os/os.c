// os: std/os's C, a thin layer over the POSIX calls std/os's Tack makes.
//
// Each function makes one call into the C library, or a fixed few that
// are one operation, as list_dir's opendir, readdir and closedir are, and
// decides nothing, but for a broken pipe on standard output or standard
// error (see below): retrying, looping, sorting, which errors mean what,
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
// tack_os_open, tack_os_open_dir_at and tack_os_pipe return, which
// std/os's Tack closes, and
// each is opened close-on-exec, so no child a later call starts inherits
// it. The one thing here the C library may allocate for itself is
// tack_os_spawn's posix_spawn_file_actions_t and posix_spawnattr_t, which
// it destroys on every path, and the DIR tack_os_list_dir reads a
// directory through, and tack_os_read_names where Linux's getdents64 is
// not there, which each closes before it returns. A child started in a directory of its own
// enters it through posix_spawn_file_actions_addchdir_np; this process's
// own directory never changes.
//
// The runtime ignores SIGPIPE, so a write here to a pipe with no reader
// fails with EPIPE, an error returned, rather than kill the process,
// but for tack_os_write_std's, to standard output or standard error,
// which follows the runtime's rule for those two (see the runtime's
// process.c) and hands a failure to tack_stdio_failed_, as print does:
// EPIPE ends the process as exit(141) does, the one decision made here,
// so that a broken pipe on either stream ends the process one way
// whatever wrote to it (per the owner's ruling, 2026-11-18); and
// SIGXFSZ, so a write past the process's file-size limit, RLIMIT_FSIZE,
// fails with EFBIG, "File too large", the same way. An ignored signal
// stays ignored across exec, so tack_os_spawn sets both back to their
// default actions in the child, which then behaves as it would started
// from a shell or from Go: yes writing into a closed pipe dies of
// SIGPIPE, and one writing past the limit of SIGXFSZ.
//
// The calls are POSIX.1-2008's, which the C library declares in the mode
// the driver compiles every C file in, clang's default, but for
// posix_spawn_file_actions_addchdir_np, an extension glibc declares
// there too, under _DEFAULT_SOURCE. The file cannot
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
#include <sys/syscall.h>
#include <stddef.h>
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

int64_t tack_os_open(tack_view_uint8 path, int32_t mode, int32_t perm) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    int flags = O_RDONLY | O_CLOEXEC;
    if (mode == 1) {
        flags = O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC;
    } else if (mode == 2) {
        flags = O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC;
    }
    int fd = open(buf, flags, (mode_t)perm);
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

int64_t tack_os_make_dir(tack_view_uint8 path, int32_t perm) {
    char buf[path_max];
    int e = c_path(path, buf);
    if (e != 0) {
        return -e;
    }
    if (mkdir(buf, (mode_t)perm) != 0) {
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

// signals_default makes a child started with attr begin with SIGPIPE's
// and SIGXFSZ's default actions, though this process ignores both, and
// returns 0, or the errno of the call that failed.
static int signals_default(posix_spawnattr_t *attr) {
    sigset_t sigs;
    if (sigemptyset(&sigs) != 0 || sigaddset(&sigs, SIGPIPE) != 0 ||
        sigaddset(&sigs, SIGXFSZ) != 0) {
        return errno;
    }
    int e = posix_spawnattr_setsigdefault(attr, &sigs);
    if (e == 0) {
        e = posix_spawnattr_setflags(attr, POSIX_SPAWN_SETSIGDEF);
    }
    return e;
}

// spawn_failed is what tack_os_spawn adds to an errno, before negating
// it, for a program that cannot be started, as against a start that
// cannot be set up: above every errno and above no_room.
enum { spawn_failed = 1 << 21 };

int64_t tack_os_spawn_failed(void) {
    return spawn_failed;
}

// setup_error returns the errno a call setting a start up failed with,
// e, negated, but -no_room for ENOMEM, which such a call gives only when
// the C library could not allocate memory of its own.
static int64_t setup_error(int e) {
    if (e == ENOMEM) {
        return -no_room;
    }
    return -e;
}

int64_t tack_os_spawn(tack_view_uint8 prog, tack_view_uint8 dir, int32_t in_dir, tack_view_uint8 pwd, tack_list_uint8 *envbuf, tack_view_uint8 flat, int64_t argc, int32_t in, int32_t out, int32_t err) {
    if (argc < 1 || argc > max_args) {
        return -E2BIG;
    }
    if (flat.len < argc || flat.ptr[flat.len - 1] != 0) {
        return -EINVAL;
    }
    char path[path_max];
    int pe = c_path(prog, path);
    if (pe != 0) {
        return -(spawn_failed + (int64_t)pe);
    }
    char wd[path_max];
    char **env = environ;
    if (in_dir) {
        int de = c_path(dir, wd);
        if (de != 0) {
            return -(spawn_failed + (int64_t)de);
        }
        // pwd is PWD= and the directory made absolute, and a 0, the
        // variable a child started in a directory of its own gets in
        // place of any PWD this process has, as Go's exec gives it; the
        // array of the child's variables is made in envbuf, through its
        // own allocator, as many as this process has and two more.
        if (pwd.len < 5 || pwd.ptr[pwd.len - 1] != 0) {
            return -EINVAL;
        }
        int64_t count = 0;
        for (char **e = environ; *e != NULL; e++) {
            count++;
        }
        int64_t need = (count + 2) * (int64_t)sizeof(char *);
        if (!TACK_LIST_RESERVE(envbuf, need)) {
            return -no_room;
        }
        char **envp = (char **)(void *)envbuf->ptr;
        int64_t m = 0;
        for (char **e = environ; *e != NULL; e++) {
            if (strncmp(*e, "PWD=", 4) != 0) {
                envp[m++] = *e;
            }
        }
        envp[m++] = (char *)pwd.ptr;
        envp[m] = NULL;
        env = envp;
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
        return setup_error(e);
    }
    posix_spawn_file_actions_t fa;
    e = posix_spawn_file_actions_init(&fa);
    if (e != 0) {
        posix_spawnattr_destroy(&attr);
        return setup_error(e);
    }
    e = signals_default(&attr);
    if (e == 0) {
        e = posix_spawn_file_actions_adddup2(&fa, in, 0);
    }
    if (e == 0) {
        e = posix_spawn_file_actions_adddup2(&fa, out, 1);
    }
    if (e == 0) {
        e = posix_spawn_file_actions_adddup2(&fa, err, 2);
    }
    // The child enters dir after its descriptors are set and before the
    // program is looked for, so a relative prog or PATH entry is taken
    // from dir; a dir it cannot enter fails the start, as a program that
    // cannot be run does.
    if (e == 0 && in_dir) {
        e = posix_spawn_file_actions_addchdir_np(&fa, wd);
    }
    if (e != 0) {
        posix_spawn_file_actions_destroy(&fa);
        posix_spawnattr_destroy(&attr);
        return setup_error(e);
    }
    pid_t pid = 0;
    e = posix_spawnp(&pid, path, &fa, &attr, argv, env);
    posix_spawn_file_actions_destroy(&fa);
    posix_spawnattr_destroy(&attr);
    if (e != 0) {
        return -(spawn_failed + (int64_t)e);
    }
    return pid;
}

void tack_os_flush_stdio(void) {
    errno = 0;
    if (fflush(stdout) == EOF) {
        tack_stdio_failed_();
    }
    errno = 0;
    if (fflush(stderr) == EOF) {
        tack_stdio_failed_();
    }
}

void tack_os_flush_stdout(void) {
    errno = 0;
    if (fflush(stdout) == EOF) {
        tack_stdio_failed_();
    }
}

int64_t tack_os_write_std(int32_t fd, tack_view_uint8 data) {
    if (fd != 1 && fd != 2) {
        return -EBADF;
    }
    if (data.len <= 0) {
        return 0;
    }
    errno = 0;
    ssize_t n = write(fd, data.ptr, (size_t)data.len);
    if (n < 0) {
        int e = errno;
        // The runtime's rule for a broken pipe on standard output or
        // standard error: EPIPE ends the process as exit(141) does, and
        // tack_stdio_failed_ returns only when the process is ending
        // already, the error then returned.
        tack_stdio_failed_();
        return -(int64_t)e;
    }
    return n;
}

int64_t tack_os_can_execute(tack_view_uint8 path) {
    char buf[path_max];
    if (c_path(path, buf) != 0) {
        return 0;
    }
    struct stat st;
    if (stat(buf, &st) != 0 || S_ISDIR(st.st_mode)) {
        return 0;
    }
    if (faccessat(AT_FDCWD, buf, X_OK, AT_EACCESS) == 0) {
        return 1;
    }
    // Go's findExecutable falls back to the mode's execute bits where
    // faccessat is not there, ENOSYS, or a seccomp filter refuses it,
    // EPERM; any other failure means the file may not be run.
    if ((errno == ENOSYS || errno == EPERM) && (st.st_mode & 0111) != 0) {
        return 1;
    }
    return 0;
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

int64_t tack_os_poll_out(int32_t fd) {
    struct pollfd p = {.fd = fd, .events = POLLOUT};
    if (poll(&p, 1, -1) < 0) {
        return -errno;
    }
    return 0;
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
#ifdef WCOREDUMP
        if (WCOREDUMP(status)) {
            return 512 + WTERMSIG(status);
        }
#endif
        return 256 + WTERMSIG(status);
    }
    return -EINVAL;
}

int64_t tack_os_eintr(void) {
    return EINTR;
}

// symlink_found is what tack_os_open_dir_at returns, negated, for a
// name that is a symbolic link: above every errno, no_room and
// spawn_failed.
enum { symlink_found = 1 << 22 };

int64_t tack_os_errno_named(int64_t k) {
    switch (k) {
    case 0:
        return ENOENT;
    case 1:
        return EISDIR;
    case 2:
        return EPERM;
    case 3:
        return EACCES;
    case 4:
        return ENOTDIR;
    case 5:
        return EINVAL;
    case 6:
        return symlink_found;
    case 7:
        return EAGAIN;
    case 8:
        return EWOULDBLOCK;
    }
    return 0;
}

int64_t tack_os_unlink_at(int32_t fd, tack_view_uint8 name, int32_t dir) {
    char buf[path_max];
    int e = c_path(name, buf);
    if (e != 0) {
        return -e;
    }
    int r;
    do {
        r = unlinkat(fd, buf, dir ? AT_REMOVEDIR : 0);
    } while (r != 0 && errno == EINTR);
    return r == 0 ? 0 : -errno;
}

int64_t tack_os_open_dir_at(int32_t fd, tack_view_uint8 name) {
    char buf[path_max];
    int e = c_path(name, buf);
    if (e != 0) {
        return -e;
    }
    int d;
    do {
        d = openat(fd, buf, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    } while (d < 0 && errno == EINTR);
    if (d >= 0) {
        return d;
    }
    int oe = errno;
    if (oe == ENOTSUP || oe == EOPNOTSUPP) {
        // Go's rootOpenDir: either means a part of the path that is no
        // directory.
        return -ENOTDIR;
    }
    if (oe == ELOOP || oe == EMLINK || oe == ENOTDIR) {
        // Go's checkSymlink: a name readlinkat reads is a symbolic link,
        // which is no error of its own.
        char one[1];
        if (readlinkat(fd, buf, one, sizeof one) >= 0) {
            return -(int64_t)symlink_found;
        }
    }
    return -oe;
}

int64_t tack_os_read_names(int32_t fd, int64_t done, int64_t max, tack_list_uint8 *names, tack_array_1_int64 *resume) {
    int64_t n = 0;
#ifdef SYS_getdents64
    // Linux: the descriptor's own offset says where the last batch
    // ended, as Go's Readdirnames reads on from it. A batch that ended
    // within what one getdents64 gave left in resume where its next
    // entry is, which is sought only here, when the listing reads on, so
    // a directory closed and reopened, as Go's is, seeks nothing.
    (void)done;
    if (resume->e[0] >= 0) {
        if (lseek(fd, (off_t)resume->e[0], SEEK_SET) < 0) {
            return -errno;
        }
        resume->e[0] = -1;
    }
    // Aligned for the records getdents64 writes in it.
    union {
        struct linux_dirent64 {
            uint64_t d_ino;
            int64_t d_off;
            unsigned short d_reclen;
            unsigned char d_type;
            char d_name[];
        } first;
        char bytes[8192];
    } buf;
    // The least a record can be: its fixed fields and a name of one byte
    // and its 0, as Go's direntReclen checks.
    const long min_reclen = (long)offsetof(struct linux_dirent64, d_name) + 2;
    while (n < max) {
        long got;
        do {
            got = syscall(SYS_getdents64, fd, buf.bytes, sizeof buf.bytes);
        } while (got < 0 && errno == EINTR);
        if (got < 0) {
            return -errno;
        }
        if (got == 0) {
            break;
        }
        long at = 0;
        while (at < got) {
            if (got - at < min_reclen) {
                return -EIO;
            }
            struct linux_dirent64 head;
            memcpy(&head, buf.bytes + at, offsetof(struct linux_dirent64, d_name));
            if (head.d_reclen < min_reclen || head.d_reclen > got - at) {
                return -EIO;
            }
            const char *name = buf.bytes + at + offsetof(struct linux_dirent64, d_name);
            size_t room = (size_t)head.d_reclen - offsetof(struct linux_dirent64, d_name);
            size_t len = strnlen(name, room);
            if (len == room) {
                return -EIO;
            }
            at += head.d_reclen;
            if ((len == 1 && name[0] == '.') || (len == 2 && name[0] == '.' && name[1] == '.')) {
                continue;
            }
            int64_t r = append_bytes(names, name, len + 1);
            if (r != 0) {
                return r;
            }
            n++;
            if (n == max) {
                if (at < got) {
                    // A negative offset, which a FUSE file system can
                    // give, cannot be sought; the rest of the buffer
                    // would be lost, so it is an error, as a malformed
                    // record is.
                    if (head.d_off < 0) {
                        return -EIO;
                    }
                    resume->e[0] = head.d_off;
                }
                break;
            }
        }
    }
#else
    // Elsewhere: a stream of a duplicate, read from the start, past the
    // done names this open has read already.
    int d = fcntl(fd, F_DUPFD_CLOEXEC, 0);
    if (d < 0) {
        return -errno;
    }
    DIR *dir = fdopendir(d);
    if (dir == NULL) {
        int e = errno;
        close(d);
        return -e;
    }
    rewinddir(dir);
    int64_t seen = 0;
    int64_t r = 0;
    while (n < max) {
        errno = 0;
        struct dirent *ent = readdir(dir);
        if (ent == NULL) {
            if (errno != 0) {
                r = -errno;
            }
            break;
        }
        if (strcmp(ent->d_name, ".") == 0 || strcmp(ent->d_name, "..") == 0) {
            continue;
        }
        if (seen < done) {
            seen++;
            continue;
        }
        r = append_bytes(names, ent->d_name, strlen(ent->d_name) + 1);
        if (r != 0) {
            break;
        }
        n++;
    }
    if (closedir(dir) != 0 && r == 0) {
        r = -errno;
    }
    if (r != 0) {
        return r;
    }
#endif
    return n;
}

// tack_os_error returns the error std/os gives for e, an errno or
// no_room: error{code: 1, msg: "out of memory"} for no_room, a list that
// could not grow, as every std allocation failure is, and code 3 and the
// message strerror gives for an errno, ENOMEM's "Cannot allocate memory"
// included, as a literal, since a Tack error's message must outlive
// every use of it, which strerror's text is not promised to. The table
// holds every errno Linux defines, in Linux's order, so no errno Linux
// gives is unknown. An errno macOS defines too has macOS's text, given
// on every target, so an error's message does not depend on the C
// library: glibc's differ for a few, EBUSY's and ENXIO's among them. One
// only Linux defines has glibc's text, and its case is there only where
// the C library defines it. Linux's aliases, EWOULDBLOCK, EDEADLOCK and
// EOPNOTSUPP, are EAGAIN, EDEADLK and ENOTSUP. An errno not listed,
// which on Linux none is, is "unknown OS error". The texts are those of
// Go's syscall tables for Linux and macOS, which are the C libraries'
// with their first letter made small.
tack_error tack_os_error(int64_t e) {
    switch (e) {
    case no_room:
        return TACK_ERROR(1, "out of memory");
    case EPERM:
        return TACK_ERROR(3, "Operation not permitted");
    case ENOENT:
        return TACK_ERROR(3, "No such file or directory");
    case ESRCH:
        return TACK_ERROR(3, "No such process");
    case EINTR:
        return TACK_ERROR(3, "Interrupted system call");
    case EIO:
        return TACK_ERROR(3, "Input/output error");
    case ENXIO:
        return TACK_ERROR(3, "Device not configured");
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
    case ENOMEM:
        return TACK_ERROR(3, "Cannot allocate memory");
    case EACCES:
        return TACK_ERROR(3, "Permission denied");
    case EFAULT:
        return TACK_ERROR(3, "Bad address");
    case ENOTBLK:
        return TACK_ERROR(3, "Block device required");
    case EBUSY:
        return TACK_ERROR(3, "Resource busy");
    case EEXIST:
        return TACK_ERROR(3, "File exists");
    case EXDEV:
        return TACK_ERROR(3, "Cross-device link");
    case ENODEV:
        return TACK_ERROR(3, "Operation not supported by device");
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
    case ENOTTY:
        return TACK_ERROR(3, "Inappropriate ioctl for device");
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
    case EDOM:
        return TACK_ERROR(3, "Numerical argument out of domain");
    case ERANGE:
        return TACK_ERROR(3, "Result too large");
    case EDEADLK:
        return TACK_ERROR(3, "Resource deadlock avoided");
    case ENAMETOOLONG:
        return TACK_ERROR(3, "File name too long");
    case ENOLCK:
        return TACK_ERROR(3, "No locks available");
    case ENOSYS:
        return TACK_ERROR(3, "Function not implemented");
    case ENOTEMPTY:
        return TACK_ERROR(3, "Directory not empty");
    case ELOOP:
        return TACK_ERROR(3, "Too many levels of symbolic links");
    case ENOMSG:
        return TACK_ERROR(3, "No message of desired type");
    case EIDRM:
        return TACK_ERROR(3, "Identifier removed");
#ifdef ECHRNG
    case ECHRNG:
        return TACK_ERROR(3, "Channel number out of range");
#endif
#ifdef EL2NSYNC
    case EL2NSYNC:
        return TACK_ERROR(3, "Level 2 not synchronized");
#endif
#ifdef EL3HLT
    case EL3HLT:
        return TACK_ERROR(3, "Level 3 halted");
#endif
#ifdef EL3RST
    case EL3RST:
        return TACK_ERROR(3, "Level 3 reset");
#endif
#ifdef ELNRNG
    case ELNRNG:
        return TACK_ERROR(3, "Link number out of range");
#endif
#ifdef EUNATCH
    case EUNATCH:
        return TACK_ERROR(3, "Protocol driver not attached");
#endif
#ifdef ENOCSI
    case ENOCSI:
        return TACK_ERROR(3, "No CSI structure available");
#endif
#ifdef EL2HLT
    case EL2HLT:
        return TACK_ERROR(3, "Level 2 halted");
#endif
#ifdef EBADE
    case EBADE:
        return TACK_ERROR(3, "Invalid exchange");
#endif
#ifdef EBADR
    case EBADR:
        return TACK_ERROR(3, "Invalid request descriptor");
#endif
#ifdef EXFULL
    case EXFULL:
        return TACK_ERROR(3, "Exchange full");
#endif
#ifdef ENOANO
    case ENOANO:
        return TACK_ERROR(3, "No anode");
#endif
#ifdef EBADRQC
    case EBADRQC:
        return TACK_ERROR(3, "Invalid request code");
#endif
#ifdef EBADSLT
    case EBADSLT:
        return TACK_ERROR(3, "Invalid slot");
#endif
#ifdef EBFONT
    case EBFONT:
        return TACK_ERROR(3, "Bad font file format");
#endif
    case ENOSTR:
        return TACK_ERROR(3, "Not a STREAM");
    case ENODATA:
        return TACK_ERROR(3, "No message available on STREAM");
    case ETIME:
        return TACK_ERROR(3, "STREAM ioctl timeout");
    case ENOSR:
        return TACK_ERROR(3, "No STREAM resources");
#ifdef ENONET
    case ENONET:
        return TACK_ERROR(3, "Machine is not on the network");
#endif
#ifdef ENOPKG
    case ENOPKG:
        return TACK_ERROR(3, "Package not installed");
#endif
    case EREMOTE:
        return TACK_ERROR(3, "Too many levels of remote in path");
    case ENOLINK:
        return TACK_ERROR(3, "ENOLINK (Reserved)");
#ifdef EADV
    case EADV:
        return TACK_ERROR(3, "Advertise error");
#endif
#ifdef ESRMNT
    case ESRMNT:
        return TACK_ERROR(3, "Srmount error");
#endif
#ifdef ECOMM
    case ECOMM:
        return TACK_ERROR(3, "Communication error on send");
#endif
    case EPROTO:
        return TACK_ERROR(3, "Protocol error");
    case EMULTIHOP:
        return TACK_ERROR(3, "EMULTIHOP (Reserved)");
#ifdef EDOTDOT
    case EDOTDOT:
        return TACK_ERROR(3, "RFS specific error");
#endif
    case EBADMSG:
        return TACK_ERROR(3, "Bad message");
    case EOVERFLOW:
        return TACK_ERROR(3, "Value too large to be stored in data type");
#ifdef ENOTUNIQ
    case ENOTUNIQ:
        return TACK_ERROR(3, "Name not unique on network");
#endif
#ifdef EBADFD
    case EBADFD:
        return TACK_ERROR(3, "File descriptor in bad state");
#endif
#ifdef EREMCHG
    case EREMCHG:
        return TACK_ERROR(3, "Remote address changed");
#endif
#ifdef ELIBACC
    case ELIBACC:
        return TACK_ERROR(3, "Can not access a needed shared library");
#endif
#ifdef ELIBBAD
    case ELIBBAD:
        return TACK_ERROR(3, "Accessing a corrupted shared library");
#endif
#ifdef ELIBSCN
    case ELIBSCN:
        return TACK_ERROR(3, ".lib section in a.out corrupted");
#endif
#ifdef ELIBMAX
    case ELIBMAX:
        return TACK_ERROR(3, "Attempting to link in too many shared libraries");
#endif
#ifdef ELIBEXEC
    case ELIBEXEC:
        return TACK_ERROR(3, "Cannot exec a shared library directly");
#endif
    case EILSEQ:
        return TACK_ERROR(3, "Illegal byte sequence");
#ifdef ERESTART
    case ERESTART:
        return TACK_ERROR(3, "Interrupted system call should be restarted");
#endif
#ifdef ESTRPIPE
    case ESTRPIPE:
        return TACK_ERROR(3, "Streams pipe error");
#endif
    case EUSERS:
        return TACK_ERROR(3, "Too many users");
    case ENOTSOCK:
        return TACK_ERROR(3, "Socket operation on non-socket");
    case EDESTADDRREQ:
        return TACK_ERROR(3, "Destination address required");
    case EMSGSIZE:
        return TACK_ERROR(3, "Message too long");
    case EPROTOTYPE:
        return TACK_ERROR(3, "Protocol wrong type for socket");
    case ENOPROTOOPT:
        return TACK_ERROR(3, "Protocol not available");
    case EPROTONOSUPPORT:
        return TACK_ERROR(3, "Protocol not supported");
    case ESOCKTNOSUPPORT:
        return TACK_ERROR(3, "Socket type not supported");
    case ENOTSUP:
        return TACK_ERROR(3, "Operation not supported");
    case EPFNOSUPPORT:
        return TACK_ERROR(3, "Protocol family not supported");
    case EAFNOSUPPORT:
        return TACK_ERROR(3, "Address family not supported by protocol family");
    case EADDRINUSE:
        return TACK_ERROR(3, "Address already in use");
    case EADDRNOTAVAIL:
        return TACK_ERROR(3, "Can't assign requested address");
    case ENETDOWN:
        return TACK_ERROR(3, "Network is down");
    case ENETUNREACH:
        return TACK_ERROR(3, "Network is unreachable");
    case ENETRESET:
        return TACK_ERROR(3, "Network dropped connection on reset");
    case ECONNABORTED:
        return TACK_ERROR(3, "Software caused connection abort");
    case ECONNRESET:
        return TACK_ERROR(3, "Connection reset by peer");
    case ENOBUFS:
        return TACK_ERROR(3, "No buffer space available");
    case EISCONN:
        return TACK_ERROR(3, "Socket is already connected");
    case ENOTCONN:
        return TACK_ERROR(3, "Socket is not connected");
    case ESHUTDOWN:
        return TACK_ERROR(3, "Can't send after socket shutdown");
    case ETOOMANYREFS:
        return TACK_ERROR(3, "Too many references: can't splice");
    case ETIMEDOUT:
        return TACK_ERROR(3, "Operation timed out");
    case ECONNREFUSED:
        return TACK_ERROR(3, "Connection refused");
    case EHOSTDOWN:
        return TACK_ERROR(3, "Host is down");
    case EHOSTUNREACH:
        return TACK_ERROR(3, "No route to host");
    case EALREADY:
        return TACK_ERROR(3, "Operation already in progress");
    case EINPROGRESS:
        return TACK_ERROR(3, "Operation now in progress");
    case ESTALE:
        return TACK_ERROR(3, "Stale NFS file handle");
#ifdef EUCLEAN
    case EUCLEAN:
        return TACK_ERROR(3, "Structure needs cleaning");
#endif
#ifdef ENOTNAM
    case ENOTNAM:
        return TACK_ERROR(3, "Not a XENIX named type file");
#endif
#ifdef ENAVAIL
    case ENAVAIL:
        return TACK_ERROR(3, "No XENIX semaphores available");
#endif
#ifdef EISNAM
    case EISNAM:
        return TACK_ERROR(3, "Is a named type file");
#endif
#ifdef EREMOTEIO
    case EREMOTEIO:
        return TACK_ERROR(3, "Remote I/O error");
#endif
    case EDQUOT:
        return TACK_ERROR(3, "Disc quota exceeded");
#ifdef ENOMEDIUM
    case ENOMEDIUM:
        return TACK_ERROR(3, "No medium found");
#endif
#ifdef EMEDIUMTYPE
    case EMEDIUMTYPE:
        return TACK_ERROR(3, "Wrong medium type");
#endif
    case ECANCELED:
        return TACK_ERROR(3, "Operation canceled");
#ifdef ENOKEY
    case ENOKEY:
        return TACK_ERROR(3, "Required key not available");
#endif
#ifdef EKEYEXPIRED
    case EKEYEXPIRED:
        return TACK_ERROR(3, "Key has expired");
#endif
#ifdef EKEYREVOKED
    case EKEYREVOKED:
        return TACK_ERROR(3, "Key has been revoked");
#endif
#ifdef EKEYREJECTED
    case EKEYREJECTED:
        return TACK_ERROR(3, "Key was rejected by service");
#endif
    case EOWNERDEAD:
        return TACK_ERROR(3, "Previous owner died");
    case ENOTRECOVERABLE:
        return TACK_ERROR(3, "State not recoverable");
#ifdef ERFKILL
    case ERFKILL:
        return TACK_ERROR(3, "Operation not possible due to RF-kill");
#endif
#ifdef EHWPOISON
    case EHWPOISON:
        return TACK_ERROR(3, "Memory page has hardware error");
#endif
    default:
        return TACK_ERROR(3, "unknown OS error");
    }
}

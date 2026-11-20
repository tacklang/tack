// The launcher of tools/spec: the process a run example's program is
// started through, spec --launch <seconds> <program>, which runs the
// program as tackc's runSpecBinary does and writes what it did on its
// own standard output, a pipe only the checker reads (see launch in
// main.tck and run_program in run.tck).
//
// It forks the program into a process group of its own, in the
// launcher's directory, with /dev/null as its standard input and a pipe
// each as its standard output and standard error, every other
// descriptor closed, SIGPIPE's and SIGXFSZ's default actions, which the
// Tack runtime ignores in the launcher, and SIGKILL as its parent-death
// signal, so that it dies with the launcher however the launcher ends.
// A run finishes when both pipes have closed, which a process the
// program started may hold open after it exits, and then the program
// has exited; it is looked at every 5 ms with waitpid. A run that has
// not finished <seconds> after it started is stopped: the program's
// group and the program are killed with SIGKILL, the pipes closed and
// the program reaped. Once a run has ended either way the group is
// killed with SIGKILL, so a process the program started dies with it
// unless it left the group. It holds no pipe the program could keep
// open beyond the deadline, so no run keeps the checker waiting.
//
// SIGINT, SIGTERM and SIGHUP, as a Ctrl-C or a harness's timeout sends
// them, kill the program's group and the program with SIGKILL, and then
// end the launcher as the signal would have. The launcher itself gets
// SIGTERM should the checker die first, so a checker killed any way
// takes the program's group with it, unless the checker died before the
// launcher could ask for that; the program's parent-death signal covers
// the launcher killed with SIGKILL, though not what the program started.
//
// What it writes is a record of the run, which the program cannot
// reach: for each read of the program's standard output, a line o <n>
// and the n bytes read, and for each of its standard error, e <n> and
// the bytes; and then one last line, exit <n> or signal <n>, how the
// program ended, or timeout, or failed and why the launcher could not
// do its part, a program that cannot be started among it. It exits 0
// once that is written, and 125, writing why on standard error, when it
// cannot write it.
//
// With TACK_SPEC_GROUPS set to a file, it appends to it a line start
// <pgid> once the program's group exists and end <pgid> once the group
// has been killed at the run's end, so that a harness can kill the
// groups of a run it stops and check that each is empty once its run
// ended (see tackc's porttest spec stage and the port's make
// test-spec-tool).
//
// It reads nothing under /proc. It follows the pattern of tools/corpus's
// launch.c, which it duplicates in part; both should move onto std/os's
// run_streams once that API lands.
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
#include "tack.h"

// launch_args holds the arguments spec_launch_arg collected, NULL ended:
// the seconds of the deadline and the program.
static char *launch_args[3];
static int64_t launch_n;

// launch_child is the program's process ID once it is forked, which the
// signal handler kills the group of, and 0 before.
static volatile pid_t launch_child;

// spec_launch_arg appends a copy of a, as a C string, to the launcher's
// arguments, and returns 0, or 1 when there is no room, no memory, or a
// holds a 0 byte.
int64_t spec_launch_arg(tack_view_uint8 a) {
    if (launch_n >= 2 || memchr(a.ptr, 0, (size_t)a.len) != NULL) {
        return 1;
    }
    char *s = malloc((size_t)a.len + 1);
    if (s == NULL) {
        return 1;
    }
    memcpy(s, a.ptr, (size_t)a.len);
    s[a.len] = 0;
    launch_args[launch_n] = s;
    launch_n++;
    launch_args[launch_n] = NULL;
    return 0;
}

// spec_launch_free frees the arguments spec_launch_arg collected.
void spec_launch_free(void) {
    for (int64_t i = 0; i < launch_n; i++) {
        free(launch_args[i]);
        launch_args[i] = NULL;
    }
    launch_n = 0;
}

// write_all writes the n bytes of p to fd, and returns 0, or 1 when it
// cannot.
static int write_all(int fd, const char *p, size_t n) {
    while (n > 0) {
        ssize_t w = write(fd, p, n);
        if (w < 0 && errno == EINTR) {
            continue;
        }
        if (w <= 0) {
            return 1;
        }
        p += w;
        n -= (size_t)w;
    }
    return 0;
}

// record writes one line of the record, line, on standard output, and
// returns 0, or 1 when it cannot.
static int record(const char *line) {
    return write_all(1, line, strlen(line));
}

// log_group appends what, start or end, and the group pgid as a line to
// the file TACK_SPEC_GROUPS names, when it names one. A file that cannot
// be written is a failure of the launcher's: it returns 1, and 0
// otherwise.
static int log_group(const char *what, pid_t pgid) {
    const char *p = getenv("TACK_SPEC_GROUPS");
    if (p == NULL || *p == 0) {
        return 0;
    }
    char line[64];
    snprintf(line, sizeof line, "%s %ld\n", what, (long)pgid);
    int fd = open(p, O_WRONLY | O_CREAT | O_APPEND | O_CLOEXEC, 0600);
    if (fd < 0) {
        return 1;
    }
    int bad = write_all(fd, line, strlen(line));
    if (close(fd) != 0) {
        bad = 1;
    }
    return bad;
}

// kill_run kills the program's group and the program, pid, with
// SIGKILL, the program itself too in case it has not entered its group
// yet.
static void kill_run(pid_t pid) {
    kill(-pid, SIGKILL);
    kill(pid, SIGKILL);
}

// on_signal is the handler of SIGINT, SIGTERM and SIGHUP: it kills the
// program's group and the program, once there is one, and then ends the
// launcher as sig would have, by its default action. It calls only
// async-signal-safe functions.
static void on_signal(int sig) {
    pid_t pid = launch_child;
    if (pid > 0) {
        kill_run(pid);
    }
    signal(sig, SIG_DFL);
    raise(sig);
}

// catch_signals installs on_signal for SIGINT, SIGTERM and SIGHUP, and
// returns 0, or 1 when it cannot.
static int catch_signals(void) {
    struct sigaction sa;
    memset(&sa, 0, sizeof sa);
    sa.sa_handler = on_signal;
    sigemptyset(&sa.sa_mask);
    int sigs[3] = {SIGINT, SIGTERM, SIGHUP};
    for (int i = 0; i < 3; i++) {
        if (sigaction(sigs[i], &sa, NULL) != 0) {
            return 1;
        }
    }
    return 0;
}

// fail_record writes the record's last line, failed, why and the
// message of errno e, and returns 0, or 125 when it cannot write it.
static int64_t fail_record(const char *why, int e) {
    char line[512];
    snprintf(line, sizeof line, "failed %s: %s\n", why, strerror(e));
    if (record(line)) {
        fprintf(stderr, "spec --launch: cannot write its record: %s\n", strerror(errno));
        return 125;
    }
    return 0;
}

// cloexec_pipe makes a pipe whose two ends close on exec, and returns 0,
// or 1 when it cannot.
static int cloexec_pipe(int p[2]) {
    if (pipe(p) != 0) {
        return 1;
    }
    if (fcntl(p[0], F_SETFD, FD_CLOEXEC) != 0 || fcntl(p[1], F_SETFD, FD_CLOEXEC) != 0) {
        int e = errno;
        close(p[0]);
        close(p[1]);
        errno = e;
        return 1;
    }
    return 0;
}

// now_ms returns the monotonic clock in milliseconds.
static int64_t now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (int64_t)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

// start_child is the child's half of the fork, parent the launcher's
// process ID: it asks for SIGKILL when the launcher dies, and exits at
// once when it already has; enters a process group of its own; takes
// in, out and err as its standard streams and report as descriptor 3,
// closed on exec; closes every other descriptor; sets SIGPIPE, SIGXFSZ,
// SIGINT, SIGTERM and SIGHUP back to their default actions and unblocks
// every signal; and runs prog. On a failure it writes errno to report, a
// pipe the parent reads, and exits 127.
static void start_child(pid_t parent, const char *prog, int in, int out, int err, int report) {
    if (prctl(PR_SET_PDEATHSIG, SIGKILL) != 0) {
        int e = errno;
        (void)write(report, &e, sizeof e);
        _exit(127);
    }
    if (getppid() != parent) {
        _exit(127);
    }
    setpgid(0, 0);
    if (dup2(in, 0) < 0 || dup2(out, 1) < 0 || dup2(err, 2) < 0 || dup2(report, 3) < 0) {
        int e = errno;
        (void)write(report, &e, sizeof e);
        _exit(127);
    }
#ifdef SYS_close_range
    syscall(SYS_close_range, 4u, ~0u, 0u);
#endif
    fcntl(3, F_SETFD, FD_CLOEXEC);
    int sigs[5] = {SIGPIPE, SIGXFSZ, SIGINT, SIGTERM, SIGHUP};
    for (int i = 0; i < 5; i++) {
        signal(sigs[i], SIG_DFL);
    }
    sigset_t none;
    sigemptyset(&none);
    sigprocmask(SIG_SETMASK, &none, NULL);
    char *argv[2] = {(char *)prog, NULL};
    execv(prog, argv);
    int e = errno;
    (void)write(3, &e, sizeof e);
    _exit(127);
}

// copy_some reads what fd has and writes it to the record as a chunk
// tagged tag, o or e, and returns 1 while the pipe is open, 0 once it
// has closed, and -1 when reading or writing fails.
static int copy_some(int fd, char tag) {
    char buf[65536];
    ssize_t r = read(fd, buf, sizeof buf);
    if (r < 0) {
        return errno == EINTR || errno == EAGAIN ? 1 : -1;
    }
    if (r == 0) {
        return 0;
    }
    char head[32];
    snprintf(head, sizeof head, "%c %ld\n", tag, (long)r);
    if (record(head) || write_all(1, buf, (size_t)r)) {
        return -1;
    }
    return 1;
}

// reap kills the program's group and the program, pid, and then reaps
// the program, storing how it ended in status: killed before it is
// reaped, so the group's ID cannot have been given to another.
static void reap(pid_t pid, int *status) {
    kill_run(pid);
    while (waitpid(pid, status, 0) < 0 && errno == EINTR) {
    }
}

// end_run kills what is left of the program's group, pid, once the
// program has been reaped, logs the group's end, and writes line, the
// record's last line, returning 0, or 125 when it cannot.
static int64_t end_run(pid_t pid, const char *line) {
    // What is left of the group dies with the run.
    kill(-pid, SIGKILL);
    launch_child = 0;
    if (log_group("end", pid)) {
        return fail_record("cannot write TACK_SPEC_GROUPS", errno);
    }
    if (record(line)) {
        fprintf(stderr, "spec --launch: cannot write its record: %s\n", strerror(errno));
        return 125;
    }
    return 0;
}

// run_launched runs prog for at most secs seconds, as the comment at
// the top says, writing the record, and returns 0, or 125 when it cannot
// write it.
static int64_t run_launched(const char *prog, int64_t secs) {
    int in = open("/dev/null", O_RDONLY | O_CLOEXEC);
    if (in < 0) {
        return fail_record("cannot open /dev/null", errno);
    }
    int po[2], pe[2], pr[2];
    if (cloexec_pipe(po) || cloexec_pipe(pe) || cloexec_pipe(pr)) {
        return fail_record("cannot make a pipe", errno);
    }
    pid_t self = getpid();
    int64_t start = now_ms();
    pid_t pid = fork();
    if (pid < 0) {
        return fail_record("cannot fork", errno);
    }
    if (pid == 0) {
        start_child(self, prog, in, po[1], pe[1], pr[1]);
    }
    launch_child = pid;
    // The child enters its group itself too; whichever runs first makes it.
    setpgid(pid, pid);
    close(in);
    close(po[1]);
    close(pe[1]);
    close(pr[1]);
    int status = 0;
    if (log_group("start", pid)) {
        int e = errno;
        reap(pid, &status);
        kill(-pid, SIGKILL);
        launch_child = 0;
        close(pr[0]);
        close(po[0]);
        close(pe[0]);
        return fail_record("cannot write TACK_SPEC_GROUPS", e);
    }
    int child_errno = 0;
    ssize_t got = read(pr[0], &child_errno, sizeof child_errno);
    close(pr[0]);
    if (got == (ssize_t)sizeof child_errno) {
        reap(pid, &status);
        close(po[0]);
        close(pe[0]);
        char line[512];
        snprintf(line, sizeof line, "failed cannot start the program: %s\n", strerror(child_errno));
        return end_run(pid, line);
    }
    int64_t deadline = start + secs * 1000;
    struct pollfd fds[2] = {{.fd = po[0], .events = POLLIN}, {.fd = pe[0], .events = POLLIN}};
    char tags[2] = {'o', 'e'};
    int open_pipes = 2;
    int timed_out = 0;
    int reaped = 0;
    const char *failure = NULL;
    int failure_errno = 0;
    while (open_pipes > 0 && failure == NULL) {
        int64_t left = deadline - now_ms();
        if (left <= 0) {
            timed_out = 1;
            break;
        }
        int n = poll(fds, 2, left > 1000 ? 1000 : (int)left);
        if (n < 0 && errno != EINTR) {
            failure = "cannot poll the program's pipes";
            failure_errno = errno;
            break;
        }
        for (int i = 0; i < 2 && n > 0; i++) {
            if (fds[i].fd < 0 || fds[i].revents == 0) {
                continue;
            }
            int c = copy_some(fds[i].fd, tags[i]);
            if (c < 0) {
                failure = "cannot copy the program's output";
                failure_errno = errno;
                break;
            }
            if (c == 0) {
                close(fds[i].fd);
                fds[i].fd = -1;
                open_pipes--;
            }
        }
    }
    while (!timed_out && failure == NULL) {
        pid_t w = waitpid(pid, &status, WNOHANG);
        if (w == pid) {
            reaped = 1;
            break;
        }
        if (w < 0 && errno != EINTR) {
            failure = "cannot wait for the program";
            failure_errno = errno;
            break;
        }
        if (now_ms() >= deadline) {
            timed_out = 1;
            break;
        }
        struct timespec tick = {.tv_sec = 0, .tv_nsec = 5000000};
        nanosleep(&tick, NULL);
    }
    if (!reaped) {
        reap(pid, &status);
    }
    for (int i = 0; i < 2; i++) {
        if (fds[i].fd >= 0) {
            close(fds[i].fd);
        }
    }
    char line[512];
    if (failure != NULL) {
        snprintf(line, sizeof line, "failed %s: %s\n", failure, strerror(failure_errno));
    } else if (timed_out) {
        snprintf(line, sizeof line, "timeout\n");
    } else if (WIFEXITED(status)) {
        snprintf(line, sizeof line, "exit %d\n", WEXITSTATUS(status));
    } else {
        snprintf(line, sizeof line, "signal %d\n", WTERMSIG(status));
    }
    return end_run(pid, line);
}

// spec_launch runs the launcher with the arguments spec_launch_arg
// handed over, and frees them, and returns its exit status: 0 once its
// record is written, and 125 when it cannot be.
int64_t spec_launch(void) {
    if (launch_n != 2) {
        spec_launch_free();
        return fail_record("spec --launch takes <seconds> <program>", EINVAL);
    }
    // The checker is the parent; should it die, the launcher's SIGTERM
    // handler takes the program's group with it.
    pid_t parent = getppid();
    if (prctl(PR_SET_PDEATHSIG, SIGTERM) != 0 || catch_signals()) {
        int64_t r = fail_record("cannot set the launcher's signals up", errno);
        spec_launch_free();
        return r;
    }
    if (getppid() != parent) {
        spec_launch_free();
        return fail_record("the checker ended before the launcher started", ESRCH);
    }
    char *end = NULL;
    long secs = strtol(launch_args[0], &end, 10);
    if (end == launch_args[0] || *end != 0 || secs <= 0 || secs > 3600) {
        spec_launch_free();
        return fail_record("the deadline is not a number of seconds from 1 to 3600", EINVAL);
    }
    int64_t r = run_launched(launch_args[1], (int64_t)secs);
    spec_launch_free();
    return r;
}

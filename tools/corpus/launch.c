// The launcher of tools/corpus: the process the sh wrapper of a run
// replaces itself with, which starts the record's program and judges how
// it and everything it started ended (see launch in main.tck and
// run_program in runexec.tck).
//
// It makes itself a child subreaper, so that every descendant of the
// program, one that left its process group or session among them, is
// reparented to it when its parent ends, forks the program into a
// session of its own, and waits for it against the record's deadline,
// looking every 5 ms with waitpid, so that a zombie is never taken for
// a live process. It then writes one result file in the run's io
// directory, result:
//
//   exit <n>, signal <n> or deadline: how the program's own process
//   ended, or that it was still alive at the deadline, when it and its
//   process group are killed;
//   outlived, a second line, when a descendant of the program was still
//   alive when the program's own process ended: the launcher then waits
//   for the descendants until the deadline and kills what is left;
//   no-children-file, a third line, when it had to kill what was left
//   and the kernel gives no /proc/self/task/<tid>/children to find it
//   by, so that only the program's process group could be killed.
//
// The one file it reads under /proc is /proc/self/task/<tid>/children,
// the list of its own children, to kill them. A launcher that cannot do
// its part writes why to wrapper-failed in the io directory and exits
// 125, writing no result, which the runner takes as the wrapper's
// failure.
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
#include "tack.h"

// launch_args holds the arguments corpus_launch_arg collected, NULL
// ended: the io directory, the seconds of the deadline, the limit of
// open files or an empty string, and the program's argv.
static char **launch_args;
static int64_t launch_n;
static int64_t launch_cap;

// corpus_launch_arg appends a copy of a, as a C string, to the
// launcher's arguments, and returns 0, or 1 when there is no room or a
// holds a 0 byte.
int64_t corpus_launch_arg(tack_view_uint8 a) {
    if (memchr(a.ptr, 0, (size_t)a.len) != NULL) {
        return 1;
    }
    if (launch_n + 2 > launch_cap) {
        int64_t cap = launch_cap == 0 ? 16 : launch_cap * 2;
        char **grown = realloc(launch_args, (size_t)cap * sizeof *grown);
        if (grown == NULL) {
            return 1;
        }
        launch_args = grown;
        launch_cap = cap;
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

// launch_free frees the arguments corpus_launch_arg collected.
static void launch_free(void) {
    for (int64_t i = 0; i < launch_n; i++) {
        free(launch_args[i]);
    }
    free(launch_args);
    launch_args = NULL;
    launch_n = 0;
    launch_cap = 0;
}

// io_path writes the path of the file name in the io directory dir to
// buf, of size n, and returns 0, or 1 when it does not fit.
static int io_path(char *buf, size_t n, const char *dir, const char *name) {
    int w = snprintf(buf, n, "%s/%s", dir, name);
    return w < 0 || (size_t)w >= n;
}

// write_note writes text to the file name in the io directory dir,
// created, and returns 0, or 1 when it cannot.
static int write_note(const char *dir, const char *name, const char *text) {
    char p[4096];
    if (io_path(p, sizeof p, dir, name)) {
        return 1;
    }
    int fd = open(p, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0600);
    if (fd < 0) {
        return 1;
    }
    size_t len = strlen(text);
    ssize_t w = write(fd, text, len);
    int bad = w < 0 || (size_t)w != len;
    if (close(fd) != 0) {
        bad = 1;
    }
    return bad;
}

// fail_launch writes why to wrapper-failed in the io directory dir and
// returns 125, the launcher's status for its own failure.
static int64_t fail_launch(const char *dir, const char *why) {
    char line[512];
    snprintf(line, sizeof line, "%s: %s\n", why, strerror(errno));
    write_note(dir, "wrapper-failed", line);
    launch_free();
    return 125;
}

// now_ns returns CLOCK_MONOTONIC's time in nanoseconds.
static int64_t now_ns(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return (int64_t)t.tv_sec * 1000000000 + t.tv_nsec;
}

// nap sleeps 5 ms.
static void nap(void) {
    struct timespec t = {0, 5000000};
    nanosleep(&t, NULL);
}

// reap_some reaps every child that has ended and returns 1 while a child
// is still alive, 0 when none is left, and -1 when waitpid fails another
// way.
static int reap_some(void) {
    for (;;) {
        int st;
        pid_t p = waitpid(-1, &st, WNOHANG);
        if (p > 0) {
            continue;
        }
        if (p == 0) {
            return 1;
        }
        return errno == ECHILD ? 0 : -1;
    }
}

// kill_children kills every child of this process the kernel lists in
// /proc/self/task/<tid>/children, the launcher being one thread, and
// returns 0, or 1 when the file cannot be read.
static int kill_children(void) {
    char p[64];
    snprintf(p, sizeof p, "/proc/self/task/%ld/children", (long)getpid());
    FILE *f = fopen(p, "re");
    if (f == NULL) {
        return 1;
    }
    long pid;
    while (fscanf(f, "%ld", &pid) == 1) {
        kill((pid_t)pid, SIGKILL);
    }
    fclose(f);
    return 0;
}

// end_all kills the process group pgid and then every child left, reaped
// and listed again until none is left, and returns 0, or 1 when the
// children file is missing, when it waits up to two seconds for what is
// left instead and leaves the rest.
static int end_all(pid_t pgid) {
    kill(-pgid, SIGKILL);
    int64_t give_up = now_ns() + 2000000000;
    for (;;) {
        int left = reap_some();
        if (left <= 0) {
            return 0;
        }
        if (kill_children() != 0) {
            while (reap_some() > 0 && now_ns() < give_up) {
                nap();
            }
            return 1;
        }
        nap();
    }
}

// start_program is the forked child: a session of its own, the default
// disposition and an empty mask for every signal, at most nofile open
// files when it is above 0, and then the program, argv, or a note in
// wrapper-failed in dir and status 125 when it cannot.
static void start_program(const char *dir, long nofile, char **argv) {
    sigset_t none;
    sigemptyset(&none);
    sigprocmask(SIG_SETMASK, &none, NULL);
    for (int s = 1; s < NSIG; s++) {
        signal(s, SIG_DFL);
    }
    if (setsid() < 0) {
        fail_launch(dir, "cannot start a session");
        _exit(125);
    }
    if (nofile > 0) {
        struct rlimit lim = {(rlim_t)nofile, (rlim_t)nofile};
        if (setrlimit(RLIMIT_NOFILE, &lim) != 0) {
            fail_launch(dir, "cannot limit open files");
            _exit(125);
        }
    }
    execvp(argv[0], argv);
    fail_launch(dir, "cannot run the program");
    _exit(125);
}

// corpus_launch runs the program the collected arguments name (see the
// comment at the top) and returns the launcher's exit status: 0 when it
// wrote its result, 125 when it could not.
int64_t corpus_launch(void) {
    if (launch_n < 4) {
        launch_free();
        return 125;
    }
    const char *dir = launch_args[0];
    char *end;
    double secs = strtod(launch_args[1], &end);
    if (*end != 0 || secs <= 0 || secs > 1000) {
        errno = EINVAL;
        return fail_launch(dir, "bad seconds");
    }
    long nofile = 0;
    if (launch_args[2][0] != 0) {
        nofile = strtol(launch_args[2], &end, 10);
        if (*end != 0 || nofile <= 0) {
            errno = EINVAL;
            return fail_launch(dir, "bad limit");
        }
    }
    if (prctl(PR_SET_CHILD_SUBREAPER, 1L, 0L, 0L, 0L) != 0) {
        return fail_launch(dir, "cannot become a child subreaper");
    }
    int64_t deadline = now_ns() + (int64_t)(secs * 1e9);
    pid_t child = fork();
    if (child < 0) {
        return fail_launch(dir, "cannot fork");
    }
    if (child == 0) {
        start_program(dir, nofile, launch_args + 3);
    }
    // The program's descriptors are its own: none of them is held open
    // here past this point.
    for (int fd = 0; fd <= 4; fd++) {
        close(fd);
    }
    int st = 0;
    int ended = 0;
    while (!ended) {
        pid_t p = waitpid(child, &st, WNOHANG);
        if (p == child) {
            ended = 1;
        } else if (p < 0) {
            return fail_launch(dir, "cannot wait for the program");
        } else if (now_ns() >= deadline) {
            break;
        } else {
            nap();
        }
    }
    char result[128];
    int no_children = 0;
    if (!ended) {
        kill(-child, SIGKILL);
        kill(child, SIGKILL);
        if (waitpid(child, &st, 0) != child) {
            return fail_launch(dir, "cannot reap the program");
        }
        no_children = end_all(child);
        snprintf(result, sizeof result, "deadline\n%s", no_children ? "no-children-file\n" : "");
    } else {
        int left = reap_some();
        if (left < 0) {
            return fail_launch(dir, "cannot wait for the program's descendants");
        }
        int outlived = left > 0;
        while (left > 0 && now_ns() < deadline) {
            nap();
            left = reap_some();
        }
        if (left > 0) {
            no_children = end_all(child);
        }
        if (WIFSIGNALED(st)) {
            snprintf(result, sizeof result, "signal %d\n", WTERMSIG(st));
        } else {
            snprintf(result, sizeof result, "exit %d\n", WEXITSTATUS(st));
        }
        if (outlived) {
            strncat(result, "outlived\n", sizeof result - strlen(result) - 1);
        }
        if (no_children) {
            strncat(result, "no-children-file\n", sizeof result - strlen(result) - 1);
        }
    }
    if (write_note(dir, "result", result) != 0) {
        return fail_launch(dir, "cannot write the result");
    }
    launch_free();
    return 0;
}

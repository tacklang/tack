// process: the start of the process, its end after Tack's main returns,
// and the end a broken pipe on standard output or standard error makes.
//
// tack_start_ ignores two signals whose default action kills the process
// at once, freeing nothing, for a write that fails: SIGPIPE, below, and
// SIGXFSZ, which a write past the process's file-size limit,
// RLIMIT_FSIZE, raises (per the owner's ruling, 2026-11-15). With
// SIGXFSZ ignored, such a write fails with EFBIG instead: std/os returns
// it as an error, "File too large", and print, exit and std/fmt's
// writes to a file past the limit lose the bytes, as for any failure
// but EPIPE, ENOSPC's among them, since tack_stdio_failed_ ends the
// process only for EPIPE.
//
// A write to a pipe or socket with no reader raises SIGPIPE, whose
// default action kills the process at once, freeing nothing. So the
// generated C's main calls tack_start_ first, which ignores SIGPIPE, and
// such a write fails with EPIPE instead. A write to standard output or
// standard error that fails with EPIPE then ends the process as
// exit(141) does: every allocator frees every buffer it still holds, no
// defer runs, and the status is 141, 128 plus SIGPIPE's number, 13, the
// status a shell reports for a process SIGPIPE killed. print, exit and
// std/fmt's C call tack_stdio_failed_ after every write and flush of
// either stream that fails, which ends the process so when the failure
// is EPIPE. A write to any other descriptor, std/os's, gets EPIPE as an
// error it returns.
//
// C's stdout is buffered, so a broken pipe may only be found when the
// buffer is flushed: by a later write, by exit, which flushes stdout
// itself before C's exit would, or, when Tack's main returns, by
// tack_end_, which the generated main calls then. Either way the status
// is 141, whatever exit was asked for.
//
// An ignored signal stays ignored across exec, so std/os's run sets
// SIGPIPE and SIGXFSZ back to their default actions in every child it
// starts: a child behaves as it would started from a shell, or from Go,
// whose runtime handles both itself and so leaves their default actions
// to a child. For the same reason a
// program may start with SIGCHLD ignored, by a parent that ignored it,
// and the kernel then reaps every child the program starts as it ends,
// so std/os's run could not wait for one. tack_start_ sets SIGCHLD back
// to its default action, as Go's runtime, which handles SIGCHLD itself,
// makes it in effect for a Go program.
//
// Either end records its status, which tack_exit_status_ returns, so an
// atexit handler, std/testing's, can tell what ended the process.
#include <errno.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#include <tack/tack.h>

// broken_pipe_status is the status a broken pipe ends the process with.
enum { broken_pipe_status = 141 };

// ending is whether the process is ending already, through exit or
// through tack_stdio_failed_. C's exit runs the C library's own flush of
// stdout, which finds a broken pipe again and reports it to no one, and
// any destructor an extern C file has; should one of those reach
// tack_stdio_failed_, it returns, losing the bytes, rather than call exit
// inside exit.
static bool ending = false;

// exit_status is the status the process is ending with, once exit was
// called or a broken pipe found, which exit_known says (see
// tack_exit_status_).
static int32_t exit_status = 0;
static bool exit_known = false;

// tack_start_ ignores SIGPIPE and SIGXFSZ and sets SIGCHLD to its
// default action. signal fails only for a signal number that is not
// one, or one whose action cannot be changed, and none of the three is
// either, so its results are not checked.
void tack_start_(void) {
    (void)signal(SIGPIPE, SIG_IGN);
    (void)signal(SIGXFSZ, SIG_IGN);
    (void)signal(SIGCHLD, SIG_DFL);
}

// tack_end_ flushes stdout once Tack's main has returned, so a broken
// pipe the flush finds ends the process with status 141, where C's exit
// would flush it and return 0.
void tack_end_(void) {
    errno = 0;
    if (fflush(stdout) == EOF) {
        tack_stdio_failed_();
    }
}

// tack_ending_ marks the process as ending with status, for exit.
void tack_ending_(int32_t status) {
    ending = true;
    exit_status = status;
    exit_known = true;
}

// tack_exit_status_ stores the status the process is ending with in
// *status and reports whether exit or a broken pipe has begun to end it.
bool tack_exit_status_(int32_t *status) {
    *status = exit_status;
    return exit_known;
}

// tack_stdio_failed_ ends the process as exit(141) does when errno, which
// the caller cleared before the failed write or flush of stdout or
// stderr, is EPIPE, and returns otherwise, the bytes lost.
void tack_stdio_failed_(void) {
    if (errno != EPIPE || ending) {
        return;
    }
    ending = true;
    exit_status = broken_pipe_status;
    exit_known = true;
    tack_allocators_release_all_();
    exit(broken_pipe_status);
}

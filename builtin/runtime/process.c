// process: the start of the process, its end after Tack's main returns,
// and the end a broken pipe on standard output or standard error makes.
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
// SIGPIPE back to its default action in every child it starts: a child
// behaves as it would started from a shell.
#include <errno.h>
#include <signal.h>
#include <stdbool.h>
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

// tack_start_ ignores SIGPIPE. signal fails only for a signal number that
// is not one, or one that cannot be ignored, and SIGPIPE is neither, so
// its result is not checked.
void tack_start_(void) {
    (void)signal(SIGPIPE, SIG_IGN);
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

// tack_ending_ marks the process as ending, for exit.
void tack_ending_(void) {
    ending = true;
}

// tack_stdio_failed_ ends the process as exit(141) does when errno, which
// the caller cleared before the failed write or flush of stdout or
// stderr, is EPIPE, and returns otherwise, the bytes lost.
void tack_stdio_failed_(void) {
    if (errno != EPIPE || ending) {
        return;
    }
    ending = true;
    tack_allocators_release_all_();
    exit(broken_pipe_status);
}

// testing: std/testing's runner and reports, in C. The main tack test
// generates calls start, then, for each test, begin and end around it,
// with fail and returned between them for what it reports, and finish
// last; testing.tck says what each does. Every report goes to stderr in
// the form of a diagnostic of the compiler's:
//
//     file:line:col: fail: test_x: message
//     <the excerpt: the source around the place, a marker under it>
//     <an empty line>
//
// formatted, for errorf and fatalf, by std/fmt's formatter, format.h,
// which this file includes, as fmt.eprintf formats, and gathered in a
// buffer on the stack, so a short report is one write. stdout is flushed
// first, so a test's own output is ahead of its report, and stderr
// after, as eprintf does. Nothing here allocates.
//
// The strings start and begin are given are kept, not copied: a Tack
// string's bytes are a literal's, in static storage, a part of one
// included, so they live as long as the process. start's names, a view,
// are only read during the call, to count the tests that will run, so
// a view of any array may be passed.
//
// The end of the process is checked too: start registers check_end with
// atexit, which runs when exit ends the process. When a test is running
// then, it called exit, which ends every test: unless the end is a broken
// pipe's, status 141, which is kept and reported by nothing, since what
// it would write has nowhere to go, the test is reported at its
// declaration, with the number of tests left that would have run, the
// run's last line follows, FAIL and the counts, as finish writes it, and
// the process ends at once with status 1, through _exit. exit has freed
// every buffer by then (tack_allocators_release_all_).
#include <stdlib.h>
#include <unistd.h>

#include "../fmt/format.h"

// state is the run: the package, the pattern and whether to write a line
// for each test, how many of start's names hold the pattern, how many
// tests began, and how many ran to their end and failed; whether check_end
// is registered; and the running test, when running says one is: its
// name, its declaration's place and excerpt, and how many buffers
// c_allocator held when it began.
static struct {
    tack_string pkg;
    tack_string pattern;
    bool verbose;
    int64_t matching;
    int64_t begun;
    int64_t ran;
    int64_t failed;
    bool registered;

    bool running;
    tack_string name;
    tack_string file;
    int64_t line;
    int64_t col;
    tack_string excerpt;
    int64_t live;
} state;

// contains reports whether the n bytes at s hold the m bytes at sub,
// which m == 0 always is; either pointer is read only for a length above
// 0.
static bool contains(const unsigned char *s, int64_t n, const unsigned char *sub, int64_t m) {
    if (m == 0) {
        return true;
    }
    for (int64_t i = 0; i + m <= n; i++) {
        int64_t j = 0;
        while (j < m && s[i + j] == sub[j]) {
            j++;
        }
        if (j == m) {
            return true;
        }
    }
    return false;
}

// matches reports whether the test named name runs: whether it holds the
// pattern.
static bool matches(tack_string name) {
    return contains(name.ptr, name.len, state.pattern.ptr, state.pattern.len);
}

// open_report flushes stdout and starts a report in o, gathered in buf,
// of gather_cap bytes, on stderr: the place file, line and col, fail:,
// and the running test's name.
static void open_report(sink *o, unsigned char *buf, tack_string file, int64_t line, int64_t col) {
    flush_stream(stdout);
    *o = (sink){.f = stderr, .buf = buf, .cap = gather_cap, .n = 0};
    out_part(o, file, 0, file.len);
    out_lit(o, ":");
    out_uint(o, (uint64_t)line, 10, false);
    out_lit(o, ":");
    out_uint(o, (uint64_t)col, 10, false);
    out_lit(o, ": fail: ");
    out_part(o, state.name, 0, state.name.len);
}

// close_report ends the report in o: the newline that ends its line, the
// excerpt and an empty line, and flushes it and stderr.
static void close_report(sink *o, tack_string excerpt) {
    out_lit(o, "\n");
    out_part(o, excerpt, 0, excerpt.len);
    out_lit(o, "\n");
    flush(o);
    flush_stream(stderr);
}

// write_line writes the zero-terminated prefix, then s, then a newline,
// to stderr, as one report.
static void write_line(const char *prefix, tack_string s) {
    unsigned char buf[gather_cap];
    flush_stream(stdout);
    sink o = {.f = stderr, .buf = buf, .cap = gather_cap, .n = 0};
    out_cstr(&o, prefix);
    out_part(&o, s, 0, s.len);
    out_lit(&o, "\n");
    flush(&o);
    flush_stream(stderr);
}

// plural writes n and then noun, with an s when n is not 1.
static void plural(sink *o, int64_t n, const char *noun) {
    out_uint(o, n < 0 ? 0 - (uint64_t)n : (uint64_t)n, 10, n < 0);
    out_lit(o, " ");
    out_cstr(o, noun);
    if (n != 1) {
        out_lit(o, "s");
    }
}

// left returns how many tests after the running one would still have
// run: those of start's names that hold the pattern, less those begun,
// the running one among them, and 0 should run be given more than start
// was.
static int64_t left(void) {
    return state.matching > state.begun ? state.matching - state.begun : 0;
}

// out_failed writes the run's last line for failed of total tests failed
// to o: FAIL, the package, and the counts.
static void out_failed(sink *o, int64_t failed, int64_t total) {
    out_lit(o, "FAIL ");
    out_part(o, state.pkg, 0, state.pkg.len);
    out_lit(o, ": ");
    out_uint(o, (uint64_t)failed, 10, false);
    out_lit(o, " of ");
    plural(o, total, "test");
    out_lit(o, " failed");
}

// check_end is the atexit handler start registers: see the top of the
// file. After the report of the test that ended the run comes the run's
// last line, as finish writes it: that test failed with those before it
// that failed, of every test that would have run. It writes with the process ending, when a write that fails
// returns rather than end the process again (see tack_stdio_failed_).
static void check_end(void) {
    if (!state.running) {
        return;
    }
    int32_t status;
    bool known = tack_exit_status_(&status);
    if (known && status == 141) {
        return;
    }
    unsigned char buf[gather_cap];
    sink o;
    open_report(&o, buf, state.file, state.line, state.col);
    if (known) {
        out_lit(&o, " called exit(");
        out_uint(&o, status < 0 ? 0 - (uint64_t)(int64_t)status : (uint64_t)status, 10, status < 0);
        out_lit(&o, "); ");
    } else {
        out_lit(&o, " ended the process from C; ");
    }
    int64_t n = left();
    plural(&o, n, "test");
    out_lit(&o, " did not run");
    close_report(&o, state.excerpt);
    o = (sink){.f = stderr, .buf = buf, .cap = gather_cap, .n = 0};
    out_failed(&o, state.failed + 1, state.matching);
    out_lit(&o, "\n");
    flush(&o);
    flush_stream(stderr);
    _exit(1);
}

void tack_testing_start(tack_string pkg, tack_string pattern, bool verbose, tack_view_string names) {
    state.pkg = pkg;
    state.pattern = pattern;
    state.verbose = verbose;
    state.matching = 0;
    for (int64_t i = 0; i < names.len; i++) {
        if (matches(names.ptr[i])) {
            state.matching++;
        }
    }
    // check_end is registered once, however often start is called. atexit
    // fails only when it cannot register another handler, which leaves an
    // exit in a test reported only by the status it asked for.
    if (!state.registered) {
        state.registered = atexit(check_end) == 0;
    }
}

bool tack_testing_contains(tack_view_uint8 s, tack_view_uint8 sub) {
    return contains(s.ptr, s.len, sub.ptr, sub.len);
}

void tack_testing_begin(tack_string name, tack_string file, int64_t line, int64_t col, tack_string excerpt) {
    state.running = true;
    state.begun++;
    state.name = name;
    state.file = file;
    state.line = line;
    state.col = col;
    state.excerpt = excerpt;
    state.live = tack_c_allocator_live_();
}

void tack_testing_fail(tack_string file, int64_t line, int64_t col, tack_string excerpt, tack_string format,
                       tack_view_any args) {
    unsigned char buf[gather_cap];
    sink o;
    open_report(&o, buf, file, line, col);
    out_lit(&o, ": ");
    format_to(&o, format, args);
    close_report(&o, excerpt);
}

void tack_testing_returned(tack_string file, int64_t line, int64_t col, tack_string excerpt, int32_t code,
                           tack_string msg) {
    unsigned char buf[gather_cap];
    sink o;
    open_report(&o, buf, file, line, col);
    out_lit(&o, ": returned error ");
    out_uint(&o, code < 0 ? 0 - (uint64_t)(int64_t)code : (uint64_t)code, 10, code < 0);
    out_lit(&o, ": ");
    out_part(&o, msg, 0, msg.len);
    close_report(&o, excerpt);
}

void tack_testing_end(bool failed) {
    int64_t leaked = tack_c_allocator_live_() - state.live;
    tack_c_allocator_fail_after_(-1);
    if (leaked > 0) {
        failed = true;
        unsigned char buf[gather_cap];
        sink o;
        open_report(&o, buf, state.file, state.line, state.col);
        out_lit(&o, ": ");
        plural(&o, leaked, "buffer");
        if (leaked == 1) {
            out_lit(&o, " allocated during the test was not freed");
        } else {
            out_lit(&o, " allocated during the test were not freed");
        }
        close_report(&o, state.excerpt);
    }
    state.ran++;
    if (failed) {
        state.failed++;
    }
    if (state.verbose) {
        write_line(failed ? "FAIL " : "ok ", state.name);
    }
    state.running = false;
}

int32_t tack_testing_finish(void) {
    unsigned char buf[gather_cap];
    flush_stream(stdout);
    sink o = {.f = stderr, .buf = buf, .cap = gather_cap, .n = 0};
    int32_t status = 0;
    if (state.ran == 0 && state.pattern.len > 0) {
        out_lit(&o, "tack test: -run \"");
        out_part(&o, state.pattern, 0, state.pattern.len);
        out_lit(&o, "\" matched no test in ");
        out_part(&o, state.pkg, 0, state.pkg.len);
        status = 1;
    } else if (state.failed == 0) {
        out_lit(&o, "ok ");
        out_part(&o, state.pkg, 0, state.pkg.len);
        out_lit(&o, ": ");
        plural(&o, state.ran, "test");
    } else {
        out_failed(&o, state.failed, state.ran);
        status = 1;
    }
    out_lit(&o, "\n");
    flush(&o);
    flush_stream(stderr);
    return status;
}

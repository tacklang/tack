// The libFuzzer entry of every target of tools/fuzz: it runs the
// target's program once for each input libFuzzer generates.
//
// The C the compiler emits for a program, tack --emit, gives every Tack
// function internal linkage, static, under a name mangled from its
// package's import path, and defines C's main, which calls tack_start_,
// stores argc and argv, calls Tack's main and then tack_end_. This file
// includes the emitted C, prog.c, which fuzz.sh writes into the
// directory of the target's build and puts on the include path, with C's
// main renamed to fuzz_program_main, so that libFuzzer's own main is the
// process's, and calls fuzz_program_main for each input, as if the
// program were run once on it. No Tack name is spelled here, so the
// mangling can change without this file changing.
//
// tack_start_ ignores SIGPIPE and SIGXFSZ and sets SIGCHLD's default
// action. Its first call replaces the handler libFuzzer installs for
// SIGXFSZ (-handle_xfsz), which only reports a write past the file-size
// limit; that is harmless, since a target writes no file, and a write
// past the limit would fail with EFBIG instead. Doing it again for each
// input changes nothing. tack_end_ flushes standard output, which a
// target never writes to. No target's main takes the input as an
// argument: it reads it through package input, which fuzz_set_input
// below makes it the one being run.
//
// After each input, every buffer the program got from c_allocator must
// have been given back. The runtime links every buffer it holds into a
// list of its own (runtime/list.c), a root LeakSanitizer scans, so a
// buffer Tack never frees is not a leak to LeakSanitizer: the runtime's
// count of them, tack_c_allocator_live_, is checked instead, and a
// buffer still held aborts, which libFuzzer reports as a crash and saves
// the input of. LeakSanitizer still finds what C allocates with malloc
// and loses, the runtime's own C and std's included.
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#define main fuzz_program_main
#include "prog.c"
#undef main

#include "input/fuzz.h"

int LLVMFuzzerInitialize(int *argc, char ***argv);
int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size);

// fuzz_argc and fuzz_argv are the arguments libFuzzer's main was given,
// which each run of the program is given in turn.
static int fuzz_argc;
static char **fuzz_argv;

// LLVMFuzzerInitialize keeps the arguments libFuzzer's main was given,
// once, before the first input.
int LLVMFuzzerInitialize(int *argc, char ***argv) {
    fuzz_argc = *argc;
    fuzz_argv = *argv;
    return 0;
}

// LLVMFuzzerTestOneInput runs the program on data, size bytes. A broken
// invariant aborts inside it (see package input), and so does a buffer
// of c_allocator still held once it has run; a return is a pass.
int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    fuzz_set_input(data, size);
    (void)fuzz_program_main(fuzz_argc, fuzz_argv);
    fuzz_clear_input();
    int64_t live = tack_c_allocator_live_();
    if (live != 0) {
        fprintf(stderr, "tack fuzz: leak: c_allocator still holds %lld buffer(s) once the input has run\n",
            (long long)live);
        fflush(stderr);
        abort();
    }
    return 0;
}

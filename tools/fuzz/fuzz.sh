#!/bin/sh
# fuzz.sh builds and runs the fuzz targets of tools/fuzz under clang's
# libFuzzer, with the address, leak and undefined behavior sanitizers,
# run from the root of the port's tree, the directory make runs it in:
#
#   sh tools/fuzz/fuzz.sh build TACK TARGET...  what make fuzz-build does
#   sh tools/fuzz/fuzz.sh run TARGET SECONDS     what make fuzz does
#   sh tools/fuzz/fuzz.sh regress TARGET...      what make fuzz-regress does
#
# A target is a package main of tools/fuzz, tools/fuzz/<target>, whose
# main runs once for each input (see package input). Everything is
# written into $BIN, bin/fuzz unless FUZZ_BIN names another directory.
# build writes, for each target, into $BIN/<target>/: the target's C
# as TACK, a Tack compiler, emits it with --emit, the program's tack.h
# as --port-header renders it, the C files of the target's packages,
# which --port-loaded lists, std's and package input's, and the binary,
# fuzz, linked from them, the runtime's C files, runtime/*.c, and
# tools/fuzz/entry.c, which includes the emitted C and hands each input
# to the target's main.
# Everything is compiled with the flags the driver compiles with,
# codegen's append_clang_flags, at -O1 rather than -O3 so a report's
# stack is readable, and with $SAN_FLAGS. It needs clang, with
# libFuzzer, and no Go.
#
# run fuzzes TARGET for SECONDS seconds: libFuzzer's -max_total_time,
# with -rss_limit_mb=2048, so an input that needs more memory is a
# finding, and -timeout, so is one that takes longer than $TIMEOUT
# seconds. The inputs it finds are kept in $BIN/corpus/<target>, and
# each crash, leak, timeout or out of memory is saved into
# $BIN/crashes/<target>/ for its reproducer. It starts from that
# corpus, the target's committed inputs, tools/fuzz/regress/<target>, and
# the seeds, every .tck file of corpus/examples and every .tck section of
# its txtar archives, which seeds writes into $BIN/<target>/seeds.
#
# regress replays, for each TARGET, every input of
# tools/fuzz/regress/<target> and every seed, once each, with no
# fuzzing, and fails when any of them fails, or when the regress
# directory holds no input libFuzzer reads, which it does not of an
# empty file. An input that once failed and was fixed is committed
# there, so every run checks it again.
#
# Leaks are caught two ways. entry.c aborts after any input that leaves
# a buffer of c_allocator held, by the runtime's count of them: that is
# every leak of memory Tack allocates, which LeakSanitizer cannot see,
# since the runtime keeps every buffer it holds on a list of its own, a
# root. LeakSanitizer finds memory C allocates with malloc and loses:
# libFuzzer runs its check after each input whose mallocs and frees do
# not balance, of the starting corpus and mutated alike, and once more at
# exit. The sanitizers' options are set here, whatever the environment
# holds, so an inherited LSAN_OPTIONS=detect_leaks=0 cannot turn it off.
#
# Every run's stack is $STACK_KB kilobytes, ulimit -s. The parser
# recurses to the nesting limit of a thousand levels on the C stack (see
# the README's Known differences), and the stack is sized so that the
# harness has about the headroom over what the sanitized target needs
# that the default 8 MB gives a user over what the compiler, built at
# -O3, needs. Measured on the costliest shapes at the limit, a thousand
# nested ifs, switches or struct literals: the compiler's --ast fails
# at 2 MB and passes at 3 MB, so 8 MB is about 2.7 to 4 times its need;
# the sanitized parse target fails at 12 MB and passes at 16 MB, and 40
# MB is 2.5 to 3.3 times that. An input that overflows 40 MB here is one
# whose stack a user's build would likely overflow too.
set -eu

# BIN is where everything is written: $FUZZ_BIN when it is set and not
# empty, as tackc's porttest sets it to a directory of its own, and
# otherwise bin/fuzz, under the ignored bin/.
BIN=${FUZZ_BIN:-bin/fuzz}

# SAN_FLAGS are what every compile and link adds to the driver's flags:
# libFuzzer's coverage, and its main when linking, the address sanitizer
# with the leak sanitizer, and the undefined behavior sanitizer, every
# report ending the run.
SAN_FLAGS="-g -fno-omit-frame-pointer -fsanitize=fuzzer,address,undefined -fno-sanitize-recover=all"

# CLANG_FLAGS are codegen's append_clang_flags at -O1.
CLANG_FLAGS="-w -fno-finite-loops -mllvm -indvars-predicate-loops=false -O1"

# MAX_LEN is the longest input libFuzzer makes when fuzzing; it holds
# every seed whole but two example programs, conv and os, which a run
# reads cut to it. REGRESS_MAX_LEN is regress's, long enough to replay
# every seed whole. TIMEOUT is how many seconds one input may take:
# the sanitized parse target is several hundred times slower than the
# compiler at -O3 on a program nested past the limit, measured, 6 to 15
# seconds on 1001 nested switches, which the compiler parses in 0.02,
# and about 14 on 2000, rising linearly, so a minute leaves room for
# the inputs that fit in MAX_LEN on a loaded machine. RSS_MB is how many
# megabytes the process may use; STACK_KB is the stack, in kilobytes
# (see above).
MAX_LEN=65536
REGRESS_MAX_LEN=1048576
TIMEOUT=60
RSS_MB=2048
STACK_KB=40960

# The sanitizers' options, replacing any the environment holds: a leak
# LeakSanitizer finds is a report, and each report prints its stack.
ASAN_OPTIONS=detect_leaks=1
LSAN_OPTIONS=detect_leaks=1
UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1
export ASAN_OPTIONS LSAN_OPTIONS UBSAN_OPTIONS

# die writes its arguments, after fuzz.sh:, to standard error and exits 1.
die() {
    echo "fuzz.sh: $*" >&2
    exit 1
}

# known fails unless $1 is a target, a directory of tools/fuzz holding a
# main.tck, named with lowercase letters alone.
known() {
    case "$1" in
    '' | *[!a-z]*) die "no fuzz target '$1': a target is named with lowercase letters" ;;
    esac
    [ -f "tools/fuzz/$1/main.tck" ] || die "no fuzz target '$1': tools/fuzz/$1/main.tck is not there"
}

# build_one builds target $2 with the compiler $1 into $BIN/$2/fuzz.
build_one() {
    tack=$1
    t=$2
    out=$BIN/$t
    rm -rf "$out"
    mkdir -p "$out/obj"
    TACK_HOME="$PWD" "$tack" --emit "./tools/fuzz/$t" > "$out/prog.c"
    TACK_HOME="$PWD" "$tack" --port-header "./tools/fuzz/$t" > "$out/tack.h"
    TACK_HOME="$PWD" "$tack" --port-loaded "./tools/fuzz/$t" > "$out/loaded"
    sed -n 's/^cfile //p' "$out/loaded" > "$out/cfiles"
    n=0
    while IFS= read -r c; do
        n=$((n + 1))
        # shellcheck disable=SC2086
        clang $CLANG_FLAGS $SAN_FLAGS -c -I "$out" -I runtime -include "$out/tack.h" \
            -o "$out/obj/c$n.o" "$c"
    done < "$out/cfiles"
    for c in runtime/*.c; do
        n=$((n + 1))
        # shellcheck disable=SC2086
        clang $CLANG_FLAGS $SAN_FLAGS -c -I runtime -o "$out/obj/c$n.o" "$c"
    done
    # shellcheck disable=SC2086
    clang $CLANG_FLAGS $SAN_FLAGS -c -I runtime -I tools/fuzz \
        -I "$out" -o "$out/obj/entry.o" tools/fuzz/entry.c
    # shellcheck disable=SC2086
    clang $CLANG_FLAGS $SAN_FLAGS -o "$out/fuzz" "$out"/obj/*.o
    echo "fuzz.sh: built $out/fuzz"
}

# seeds writes every .tck file of corpus/examples, and every .tck
# section of its txtar archives, into $1, one file each, named for where
# it came from with each / a -, and every section numbered, with no
# .tck suffix, so that no check of the tree, tack --check ., reads one.
# Each target has a directory of its own, so runs of two targets at once
# never read one another's while it is written.
seeds() {
    rm -rf "$1"
    mkdir -p "$1"
    find corpus/examples -name '*.tck' | while read -r f; do
        g="${f#corpus/examples/}"
        cp "$f" "$1/$(echo "${g%.tck}" | tr / -)"
    done
    for a in corpus/examples/*.txtar; do
        awk -v dir="$1" -v base="$(basename "$a" .txtar)" '
            /^-- .* --$/ {
                if (out != "") close(out)
                out = ""
                if ($0 ~ /\.tck --$/) {
                    n++
                    out = dir "/" base "-" n
                    printf "" > out
                }
                next
            }
            out != "" { print > out }
        ' "$a"
    done
}

# regress_one replays tools/fuzz/regress/$1 and the seeds through
# $BIN/$1/fuzz, each input once.
regress_one() {
    t=$1
    dir=tools/fuzz/regress/$t
    [ -x "$BIN/$t/fuzz" ] || die "$BIN/$t/fuzz is not built; run make fuzz-build"
    [ -d "$dir" ] || die "$dir is not there"
    count=$(find "$dir" -type f -size +0 | wc -l)
    [ "$count" -gt 0 ] || die "$dir holds no input to replay"
    seeds "$BIN/$t/seeds"
    seeds=$(find "$BIN/$t/seeds" -type f -size +0 | wc -l)
    [ "$seeds" -gt 0 ] || die "$BIN/$t/seeds holds no seed"
    mkdir -p "$BIN/regress-artifacts/$t"
    # -runs=0 runs every input of the directories given once, and makes
    # no new one.
    if ! (ulimit -s "$STACK_KB" && "$BIN/$t/fuzz" -runs=0 -max_len="$REGRESS_MAX_LEN" -timeout="$TIMEOUT" \
        -rss_limit_mb="$RSS_MB" -artifact_prefix="$BIN/regress-artifacts/$t/" \
        "$dir" "$BIN/$t/seeds") > "$BIN/regress-$t.log" 2>&1; then
        cat "$BIN/regress-$t.log" >&2
        die "$t: an input of $dir or of the seeds fails"
    fi
    echo "fuzz.sh: $t: $count inputs of $dir and $seeds seeds pass"
}

cmd=${1-}
[ $# -gt 0 ] && shift
case "$cmd" in
build)
    [ $# -ge 2 ] || die "usage: fuzz.sh build TACK TARGET..."
    tack=$1
    case "$tack" in
    */*) ;;
    *) tack=$(command -v "$1") || die "no compiler $1 on PATH" ;;
    esac
    shift
    for t in "$@"; do
        known "$t"
    done
    for t in "$@"; do
        build_one "$tack" "$t"
    done
    ;;
run)
    [ $# -eq 2 ] || die "usage: fuzz.sh run TARGET SECONDS"
    t=$1
    known "$t"
    case "$2" in
    '' | *[!0-9]*) die "FUZZTIME must be a whole number of seconds, not '$2'" ;;
    esac
    [ -x "$BIN/$t/fuzz" ] || die "$BIN/$t/fuzz is not built; run make fuzz-build"
    seeds "$BIN/$t/seeds"
    mkdir -p "$BIN/corpus/$t" "$BIN/crashes/$t"
    ulimit -s "$STACK_KB"
    exec "$BIN/$t/fuzz" -max_total_time="$2" -max_len="$MAX_LEN" -timeout="$TIMEOUT" \
        -rss_limit_mb="$RSS_MB" -print_final_stats=1 -artifact_prefix="$BIN/crashes/$t/" \
        "$BIN/corpus/$t" "tools/fuzz/regress/$t" "$BIN/$t/seeds"
    ;;
regress)
    [ $# -ge 1 ] || die "usage: fuzz.sh regress TARGET..."
    for t in "$@"; do
        known "$t"
    done
    for t in "$@"; do
        regress_one "$t"
    done
    ;;
*)
    die "usage: fuzz.sh build|run|regress ..."
    ;;
esac

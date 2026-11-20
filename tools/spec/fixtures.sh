#!/bin/sh
# fixtures.sh runs tools/spec on its own fixtures and fails unless it
# writes on each what the fixture's want holds and leaves nothing
# running. make test-spec-tool runs it as
#
#     sh tools/spec/fixtures.sh <spec> <compiler> <tree> <work>
#
# spec is the built checker, compiler the compiler it checks, tree this
# source tree, absolute, and work a directory it writes into, its
# outputs and the group logs. The fixtures, each a directory below
# tools/spec/testdata with spec/, a spec, and want, are:
#
#   - fail, run with compiler: a spec whose index, directory and
#     chapters fail every way the checker reports with a real compiler,
#     beside examples that pass;
#   - fake, run with the stand-in compiler fake/tack: the failures only a
#     compiler that misbehaves can give (see fake/tack).
#
# For each it runs the checker with TACK_SPEC_GROUPS set to a log of its
# own, and fails unless what the checker writes, with "<tree>/" taken
# out, followed by a line exit <status>, is want, byte for byte, diff
# showing what differs; and unless every process group the log names
# has an end line, and is empty within 2 seconds of the run's end, a
# group that is not being killed. It needs coreutils, sed, diff and
# grep, and no Go.

if [ "$#" -ne 4 ]; then
    echo "usage: sh tools/spec/fixtures.sh <spec> <compiler> <tree> <work>" >&2
    exit 2
fi
spec=$1
compiler=$2
tree=$3
work=$4
failed=0

# gone reports whether the process group $1 is empty, looking every
# tenth of a second for 2 seconds, since a process killed at its run's
# end is gone only once whoever inherited it has reaped it.
gone() {
    i=0
    while kill -0 -- "-$1" 2>/dev/null; do
        i=$((i + 1))
        if [ "$i" -gt 20 ]; then
            return 1
        fi
        sleep 0.1
    done
    return 0
}

# check runs the checker on the fixture $1 with the compiler $2, and
# sets failed when what it wrote is not the fixture's want or a group
# the run started is left.
check() {
    name=$1
    dir=$tree/tools/spec/testdata/$name
    out=$work/spec-$name.out
    groups=$work/spec-$name.groups
    : > "$groups"
    { TACK_SPEC_GROUPS="$groups" "$spec" "$2" "$tree" "$dir/spec"; echo "exit $?"; } > "$out" 2>&1
    if ! sed "s|$tree/||" "$out" | diff -u "$dir/want" -; then
        echo "fixtures.sh: tools/spec's output on testdata/$name/spec is not testdata/$name/want" >&2
        failed=1
    fi
    while read -r what pgid; do
        case $what in
        start)
            if ! grep -qx "end $pgid" "$groups"; then
                echo "fixtures.sh: testdata/$name: the launcher logged no end for process group $pgid" >&2
                kill -KILL -- "-$pgid" 2>/dev/null
                failed=1
            fi
            ;;
        end)
            if ! gone "$pgid"; then
                echo "fixtures.sh: testdata/$name: process group $pgid outlived its run, killed" >&2
                kill -KILL -- "-$pgid" 2>/dev/null
                failed=1
            fi
            ;;
        *)
            echo "fixtures.sh: testdata/$name: a line of the group log is not start or end: $what $pgid" >&2
            failed=1
            ;;
        esac
    done < "$groups"
    if ! grep -q '^start ' "$groups"; then
        echo "fixtures.sh: testdata/$name: no program ran, so no group was checked" >&2
        failed=1
    fi
}

check fail "$compiler"
check fake "$tree/tools/spec/testdata/fake/tack"
exit "$failed"

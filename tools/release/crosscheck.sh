#!/bin/sh
# crosscheck.sh checks a release bootstrapped from the previous release
# against the same release bootstrapped from Go:
#
#   sh tools/release/crosscheck.sh TREE GOSTAGE1 OUT
#
# TREE is the stamped copy make release built, bin/release/tree, whose
# bin/bootstrap/stage2 is stage 2 by way of the previous release;
# GOSTAGE1 is stage 1 built from TREE by the reference Go compiler at the
# go-bootstrap tag (see go-stage1.sh, run in TREE). It builds TREE's
# ./driver at -O3 with GOSTAGE1 into OUT, with TACK_HOME TREE and a fresh
# cache, as make bootstrap builds stage 2, refusing an OUT that resolves
# to TREE's stage 2 itself, removing OUT first and failing when the
# build fails or writes no OUT, and fails unless OUT is
# TREE's stage 2 byte for byte, cmp naming the first byte that differs:
# a previous release and Go that build different compilers from the same
# source mean one of them is wrong, and nothing is released. The release
# workflow runs it while the go-bootstrap tag can still build the source
# (see RELEASING.md). It needs clang and coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "crosscheck: $*" >&2
	exit 1
}

[ $# -eq 3 ] || die "usage: tools/release/crosscheck.sh TREE GOSTAGE1 OUT"
tree=$(realpath -e "$1") || die "no tree $1"
gostage1=$(realpath -e "$2") || die "no stage 1 $2"
out=$(realpath -m "$3")
stage2=$tree/bin/bootstrap/stage2
[ -f "$stage2" ] || die "$tree has no bin/bootstrap/stage2: run make release first"
[ "$out" != "$(realpath -e "$stage2")" ] ||
	die "OUT, $3, is the tree's own stage 2: it would be compared with itself"
# OUT is removed first, so only what this build writes is compared: a
# stage 1 that writes nothing, or bytes left there by an earlier run,
# cannot pass.
rm -f "$out"
rm -rf "$out.cache"
(cd "$tree" && TACK_HOME="$tree" TACK_CACHE="$out.cache" "$gostage1" -O3 --out "$out" ./driver) ||
	die "Go's stage 1 failed to build stage 2"
rm -rf "$out.cache"
[ -f "$out" ] || die "Go's stage 1 exited 0 but wrote no $out"
sha256sum "$stage2" "$out"
cmp "$stage2" "$out" || die "stage 2 from Go's stage 1 is not stage 2 from the previous release's"
echo "crosscheck: stage 2 is the same bytes from the previous release and from Go"

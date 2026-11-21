#!/bin/sh
# go-stage1.sh builds stage 1 of a first release, the one release with no
# previous release to bootstrap from, with Go:
#
#   sh tools/release/go-stage1.sh FROZEN OUT
#
# run from the root of the port's tree. FROZEN is a checkout of tackc,
# the frozen Go compiler, at its tag go-bootstrap; OUT is where stage 1 is
# written. It builds the reference Go compiler, FROZEN's ./tack with the
# files its package builtin embeds, builtin.tck, the runtime header and
# the runtime's C files, replaced by this tree's runtime/ through go
# build's -overlay, as tackc's make port-go-bin does, so that what it
# builds links the port's runtime. That compiler then builds ./driver at
# -O3, with TACK_HOME this tree and a fresh build cache, into OUT. The
# reference is written to OUT.go, beside OUT, and the overlay and cache
# to OUT.work, removed first. The reference is built with -trimpath and
# -buildvcs=false, so its bytes depend on FROZEN's files and the Go that
# builds it, not on where FROZEN or this tree is checked out, nor on
# git's state there.
#
# The overlay maps FROZEN's builtin/builtin.tck to runtime/builtin.tck,
# builtin/runtime/tack/tack.h to runtime/tack/tack.h, each runtime/*.c
# to builtin/runtime/ under its name, and each C file of FROZEN's
# builtin/runtime/ that runtime/ lacks to nothing, so the reference
# embeds the port's C files and no others. A path holding a character
# JSON would need to escape, a quote, a backslash or a control
# character, is refused rather than written into the overlay unescaped.
#
# The release workflow runs it for the first release, whose stage 1 it
# is, and, while the go-bootstrap tag can still build this tree, for
# every later one, to check that stage 2 is the same from either stage 1
# (see crosscheck.sh and RELEASING.md). It needs go, clang and
# coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "go-stage1: $*" >&2
	exit 1
}

[ $# -eq 2 ] || die "usage: tools/release/go-stage1.sh FROZEN OUT"
frozen=$(realpath -e "$1") || die "no tackc checkout at $1"
out=$(realpath -m "$2")
root=$(realpath -e .)
[ -f "$frozen/builtin/runtime.go" ] || die "$frozen is no tackc checkout: it has no builtin/runtime.go"
[ -f runtime/builtin.tck ] && [ -f runtime/tack/tack.h ] || die "run it from the root of the port's tree"

work=$out.work
rm -rf "$work"
mkdir -p "$work"

# jsonpath fails on a path JSON would need to escape, and writes it
# quoted otherwise.
jsonpath() {
	case $1 in
	*'"'* | *'\'* | *[[:cntrl:]]*) die "cannot write $1 into the overlay: it holds a quote, a backslash or a control character" ;;
	esac
	printf '"%s"' "$1"
}

{
	printf '{"Replace":{'
	printf '%s:%s' "$(jsonpath "$frozen/builtin/builtin.tck")" "$(jsonpath "$root/runtime/builtin.tck")"
	printf ',%s:%s' "$(jsonpath "$frozen/builtin/runtime/tack/tack.h")" "$(jsonpath "$root/runtime/tack/tack.h")"
	for c in runtime/*.c; do
		[ -f "$c" ] || continue
		printf ',%s:%s' "$(jsonpath "$frozen/builtin/runtime/${c#runtime/}")" "$(jsonpath "$root/$c")"
	done
	for c in "$frozen"/builtin/runtime/*.c; do
		[ -f "$c" ] || continue
		[ -f "runtime/${c##*/}" ] && continue
		printf ',%s:""' "$(jsonpath "$c")"
	done
	printf '}}\n'
} >"$work/overlay.json"

echo "go-stage1: building the reference Go compiler from $frozen"
(cd "$frozen" && go build -trimpath -buildvcs=false -overlay "$work/overlay.json" -o "$out.go" ./tack)
echo "go-stage1: building stage 1 with it"
TACK_HOME="$root" TACK_CACHE="$work/cache" "$out.go" -O3 --out "$out" ./driver
rm -rf "$work"

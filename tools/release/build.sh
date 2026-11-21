#!/bin/sh
# build.sh does the work of the Makefile's release target, run from the
# root of the port's tree:
#
#   sh tools/release/build.sh TACK OUT
#
# with VERSION, the release's tag, and SOURCE_DATE_EPOCH from the
# environment (see package.sh). TACK is stage 1's compiler: the previous
# release's, or, for the first release, the one go-stage1.sh builds. OUT
# is the directory everything is built in, removed first:
#
#   OUT/tree       a copy of this tree, all of it but bin/ and .git,
#                  stamped with VERSION (see stamp.sh)
#   OUT/stage1     the copy's ./driver built by TACK at -O3
#   OUT/tree/bin/bootstrap/stage2, stage3
#                  the copy's make bootstrap run with OUT/stage1, which
#                  fails unless the two are the same bytes
#   OUT/dist/tack-VERSION-linux-x86_64.tar.xz
#                  the tarball, with stage 3 as bin/tack (see package.sh)
#
# This is make install's chain: TACK need not build the same bytes the
# new source does, as a previous release does not once the lowering or
# the runtime has changed since, so it builds an intermediate stage 1
# from the new source, which then builds stage 2 and stage 3, both from
# the new source by a compiler of the new source, which must agree. Each
# build has TACK_HOME the copy and a cache of its own, made anew. Stage 3
# must print tack VERSION to --version, and pass glibc-check.sh against
# tools/release/glibc-floor, needing no glibc above the floor and no
# library but libc.so.6, before the tarball is made. It prints the
# SHA-256 of stage 1, stage 3 and the tarball. It never writes to this
# tree outside OUT. It needs make, clang, llvm-readelf or readelf, GNU
# tar, xz, sed and coreutils, and no Go.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "release: $*" >&2
	exit 1
}

[ $# -eq 2 ] || die "usage: VERSION=vX.Y.Z SOURCE_DATE_EPOCH=N tools/release/build.sh TACK OUT"
tack=$(command -v "$1") || die "no compiler $1"
tack=$(realpath -e "$tack")
out=$(realpath -m "$2")
root=$(realpath -e .)
[ -f "$root/driver/version.tck" ] && [ -f "$root/tools/release/stamp.sh" ] || die "run it from the root of the port's tree"
case "$out/" in
"$root/bin/"*) ;;
"$root/"*) die "OUT, $out, is inside this tree but not below its bin/" ;;
esac

rm -rf "$out"
mkdir -p "$out/tree" "$out/dist"
for f in "$root"/* "$root"/.[!.]* "$root"/..?*; do
	[ -e "$f" ] || [ -L "$f" ] || continue
	case ${f##*/} in
	bin | .git) continue ;;
	esac
	cp -R "$f" "$out/tree/"
done
sh "$root/tools/release/stamp.sh" "$out/tree"

echo "release: building stage 1 with $tack"
(cd "$out/tree" && TACK_HOME="$out/tree" TACK_CACHE="$out/cache1" \
	"$tack" -O3 --out "$out/stage1" ./driver)
make -C "$out/tree" -s bootstrap TACK="$out/stage1"
stage3=$out/tree/bin/bootstrap/stage3
got=$("$stage3" --version)
[ "$got" = "tack $VERSION" ] || die "stage 3 says '$got' to --version, not 'tack $VERSION'"
sh "$root/tools/release/glibc-check.sh" "$stage3" "$root/tools/release/glibc-floor"

sh "$root/tools/release/package.sh" "$out/tree" "$stage3" "$out/dist/tack-$VERSION-linux-x86_64.tar.xz"
sha256sum "$out/stage1" "$stage3" "$out/dist/tack-$VERSION-linux-x86_64.tar.xz"

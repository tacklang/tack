#!/bin/sh
# package.sh writes a release's tarball:
#
#   sh tools/release/package.sh TREE COMPILER OUT
#
# with VERSION, the release's tag, and SOURCE_DATE_EPOCH, the time every
# entry is dated, in seconds since 1970, from the environment. TREE is the
# tree the compiler was built from, COMPILER the compiler, stage 3 of the
# release's bootstrap, and OUT the tarball written,
# tack-VERSION-linux-x86_64.tar.xz by the release target. It holds one
# directory, tack-VERSION-linux-x86_64, laid out as a TACK_HOME, so that
# extracting it and pointing TACK_HOME at that directory, with its bin on
# PATH, is an install, and as much of a source tree as tools/install.sh
# reads, so that tools/get-tack.sh installs it with that script:
#
#   bin/tack                     the compiler, mode 755
#   src/std/                     the standard library, TREE's src/std, with
#                                std/builtin/builtin.tck and
#                                std/include/tack/tack.h written from
#                                runtime/; they stay writable, as in the
#                                tree, since install.sh copies std and
#                                then writes them over, read-only
#   runtime/builtin.tck          the builtins' documentation and the
#   runtime/tack/tack.h          runtime header, which install.sh copies
#   runtime/tack.mod             TREE's, which makes runtime/ a module
#                                root, so that tack --check . of a tree
#                                holding the release, as bin/from-release
#                                (see the Makefile), skips builtin.tck,
#                                as it skips the tree's own
#   tools/install.sh             TREE's, which installs the rest
#   LICENSE                      TREE's
#   README                       tools/release/README.txt, @VERSION@
#                                replaced with VERSION
#
# Every other directory is mode 755 and every other file 644, whatever
# the umask and the modes TREE's copies have. The tarball is the same
# bytes from the same inputs, wherever and by whom it is made: GNU tar's
# gnu format, which writes no extended headers with access or change
# times, its entries sorted by name in the C locale, each dated
# SOURCE_DATE_EPOCH and owned by user and group 0 with no names, then xz
# at preset 6 with one thread, whose output does not depend on the
# machine. A different tar or xz may write other bytes: the release
# workflow takes both from its fedora:45 image, pinned by digest, and
# logs their versions, which dnf may update (see RELEASING.md).
#
# It writes into a directory beside OUT, removed first and last, and
# renames the tarball into place only once it is complete. It needs GNU
# tar, xz, sed and coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "package: $*" >&2
	exit 1
}

[ $# -eq 3 ] || die "usage: VERSION=vX.Y.Z SOURCE_DATE_EPOCH=N tools/release/package.sh TREE COMPILER OUT"
tree=$1
compiler=$2
out=$3
[ -n "${VERSION:-}" ] || die "VERSION is not set"
printf '%s' "$VERSION" | grep -Eqx 'v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})' ||
	die "VERSION must be a release tag, vMAJOR.MINOR.PATCH, not '$VERSION'"
case ${SOURCE_DATE_EPOCH:-} in
'' | *[!0-9]*) die "SOURCE_DATE_EPOCH must be set to a number of seconds, the time of the release's commit" ;;
esac
for f in LICENSE tools/install.sh tools/release/README.txt runtime/builtin.tck runtime/tack/tack.h runtime/tack.mod; do
	[ -f "$tree/$f" ] || die "$tree has no $f"
done
[ -d "$tree/src/std" ] || die "$tree has no src/std"
[ -f "$compiler" ] && [ -x "$compiler" ] || die "$compiler is no executable file"

name=tack-$VERSION-linux-x86_64
work=$out.work
rm -rf "$work"
trap 'rm -rf "$work"' EXIT
top=$work/$name
mkdir -p "$top/bin" "$top/src" "$top/runtime/tack" "$top/tools"
cp "$compiler" "$top/bin/tack"
cp -R "$tree/src/std" "$top/src/std"
mkdir -p "$top/src/std/builtin" "$top/src/std/include/tack"
cp "$tree/runtime/builtin.tck" "$top/src/std/builtin/builtin.tck"
cp "$tree/runtime/tack/tack.h" "$top/src/std/include/tack/tack.h"
cp "$tree/runtime/builtin.tck" "$top/runtime/builtin.tck"
cp "$tree/runtime/tack/tack.h" "$top/runtime/tack/tack.h"
cp "$tree/runtime/tack.mod" "$top/runtime/tack.mod"
cp "$tree/tools/install.sh" "$top/tools/install.sh"
cp "$tree/LICENSE" "$top/LICENSE"
sed "s/@VERSION@/$VERSION/g" "$tree/tools/release/README.txt" >"$top/README"

find "$work" -type d -exec chmod 755 {} +
find "$work" -type f -exec chmod 644 {} +
chmod 755 "$top/bin/tack"
if find "$work" ! -type d ! -type f | grep -q .; then
	die "$tree's copies hold an entry that is no file or directory, which the tarball cannot hold the same everywhere"
fi

(cd "$work" && LC_ALL=C TZ=UTC tar --create --format=gnu --sort=name \
	--mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
	--file=out.tar "$name")
# Written to a file first, not piped, so that a tar that fails fails the
# script, which sh's pipelines would not.
xz -T1 -6 --check=crc64 "$work/out.tar"
mv -f "$work/out.tar.xz" "$out"
echo "package: wrote $out"

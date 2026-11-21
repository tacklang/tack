#!/bin/sh
# sums.sh writes DIR/SHA256SUMS, the SHA-256 of each tarball of a
# release in DIR, each tack-*.tar.xz there:
#
#   sh tools/release/sums.sh DIR
#
# one line for each, sha256sum's: the hex digest, two spaces and the
# file's name, sorted by name in the C locale, so the file is the same
# bytes for the same tarballs. It fails when DIR holds no tarball. It
# needs coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "sums: $*" >&2
	exit 1
}

[ $# -eq 1 ] || die "usage: tools/release/sums.sh DIR"
cd "$1"
set -- tack-*.tar.xz
[ -f "$1" ] || die "$PWD holds no tarball, tack-*.tar.xz"
# Written to a file first, not piped, so that a sha256sum that fails
# fails the script, which sh's pipelines would not.
LC_ALL=C sha256sum "$@" >SHA256SUMS.unsorted
LC_ALL=C sort -k 2 SHA256SUMS.unsorted >SHA256SUMS.new
rm -f SHA256SUMS.unsorted
mv -f SHA256SUMS.new SHA256SUMS
cat SHA256SUMS

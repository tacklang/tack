#!/bin/sh
# check-artifact.sh checks the tarball the publish job downloads against
# the digest the build job took of it, before anything is signed:
#
#   sh tools/release/check-artifact.sh DIR NAME SHA256
#
# DIR must hold exactly one file, NAME, a regular file whose SHA-256 is
# SHA256, the build job's output, taken once the release was built and
# the tarball copied out of the reach of the tests' user, before any test
# ran. Anything else fails: another file beside it, another name, other
# bytes. It needs coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "check-artifact: $*" >&2
	exit 1
}

[ $# -eq 3 ] || die "usage: tools/release/check-artifact.sh DIR NAME SHA256"
dir=$1
name=$2
want=$3
printf '%s' "$want" | grep -Eqx '[0-9a-f]{64}' || die "'$want' is not a SHA-256"
case $name in
tack-v*-linux-x86_64.tar.xz) ;;
*) die "'$name' is not a release tarball's name" ;;
esac
[ "$(ls -A "$dir")" = "$name" ] || die "$dir holds $(ls -A "$dir" | tr '\n' ' '), not $name alone"
[ -f "$dir/$name" ] && [ ! -L "$dir/$name" ] || die "$dir/$name is not a regular file"
(cd "$dir" && printf '%s  %s\n' "$want" "$name" | sha256sum --check --strict --quiet) ||
	die "$name is not the tarball the build job made: its SHA-256 is not $want"
echo "check-artifact: $name is the build job's, $want"

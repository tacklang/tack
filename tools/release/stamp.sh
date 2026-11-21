#!/bin/sh
# stamp.sh writes a release's version into a copy of the port's tree:
#
#   sh tools/release/stamp.sh TREE
#
# with the version in the environment, as VERSION, never pasted into a
# command. VERSION must be a release tag, vMAJOR.MINOR.PATCH, each number
# 0 or a run of digits starting 1 to 9, at most nine digits each (see
# check_version); anything else, a prerelease or build suffix, a missing
# v, a leading zero, white space, a quote or a newline among them, fails
# before anything is written, so nothing but such a tag ever reaches
# Tack source.
#
# It rewrites TREE's driver/version.tck, whose one line
#
#   var tack_version = "devel"
#
# becomes var tack_version = "VERSION", and each line
#
#   stdout "tack devel\n"
#
# of TREE's corpus/runs/port-only.txtar, the run records of tack
# --version, becomes stdout "tack VERSION\n", so the release's compiler
# prints its version and is held to it. It fails, writing nothing, unless
# version.tck holds that line exactly once and the archive at least once:
# a tree that is not as committed, or one stamped already, is refused.
# TREE must never be the committed tree itself: the release target runs
# it on a copy (see build.sh). It needs coreutils, grep and sed.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "stamp: $*" >&2
	exit 1
}

# check_version fails unless $1 is vMAJOR.MINOR.PATCH, as above.
check_version() {
	printf '%s' "$1" | grep -Eqx 'v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})' ||
		die "VERSION must be a release tag, vMAJOR.MINOR.PATCH such as v0.1.0, not '$1'"
	# grep reads lines, so a version with a newline in it could match on
	# one line alone; there must be none.
	case $1 in
	*'
'*) die "VERSION must be one line" ;;
	esac
}

[ $# -eq 1 ] || die "usage: VERSION=vX.Y.Z tools/release/stamp.sh TREE"
[ -n "${VERSION:-}" ] || die "VERSION is not set"
check_version "$VERSION"
tree=$1
src=$tree/driver/version.tck
runs=$tree/corpus/runs/port-only.txtar
devel_line='var tack_version = "devel"'
devel_out='stdout "tack devel\n"'

n=$(grep -cxF "$devel_line" "$src") || die "$src does not hold the line: $devel_line"
[ "$n" -eq 1 ] || die "$src holds the line $devel_line $n times, not once"
m=$(grep -cxF "$devel_out" "$runs") || die "$runs holds no line: $devel_out"

sed "s/^var tack_version = \"devel\"\$/var tack_version = \"$VERSION\"/" "$src" >"$src.new"
sed "s/^stdout \"tack devel\\\\n\"\$/stdout \"tack $VERSION\\\\n\"/" "$runs" >"$runs.new"
grep -qxF "var tack_version = \"$VERSION\"" "$src.new" || die "rewriting $src failed"
k=$(grep -cxF "stdout \"tack $VERSION\\n\"" "$runs.new") || k=0
[ "$k" -eq "$m" ] || die "rewriting $runs changed $k lines, not $m"
mv -f "$src.new" "$src"
mv -f "$runs.new" "$runs"
echo "stamp: $tree is version $VERSION"

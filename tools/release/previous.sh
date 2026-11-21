#!/bin/sh
# previous.sh names the release a new one bootstraps from:
#
#   sh tools/release/previous.sh TAG < TAGS
#
# TAGS is the tags of the published releases, one a line, as the
# workflow lists them, drafts and prereleases left out. It writes the
# highest of them below TAG, by SemVer's order of MAJOR, MINOR and PATCH
# compared as numbers, or nothing when there is none, which makes the
# release a first release. TAG and each listed tag must be
# vMAJOR.MINOR.PATCH (see stamp.sh); any other line fails, as does an
# empty one, so a listing that went wrong is never read as no release.
# It fails when TAG is not above every listed tag, TAG itself among
# them: a release is never built from itself, nor from a newer one, and
# a tag below the highest release is refused before anything is built,
# so the first release's Go path can never run for a lower tag than a
# release there is (decided 2026-11-21). It needs grep, sort and
# coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "previous: $*" >&2
	exit 1
}

pattern='v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})'
[ $# -eq 1 ] || die "usage: tools/release/previous.sh TAG < TAGS"
printf '%s' "$1" | grep -Eqx "$pattern" || die "TAG must be vMAJOR.MINOR.PATCH, not '$1'"
case $1 in
*'
'*) die "TAG must be one line" ;;
esac

# key writes tag $1 as three numbers padded to nine digits, which sort
# compares as text in SemVer's order.
key() {
	v=${1#v}
	printf '%09d.%09d.%09d\n' "${v%%.*}" "$(echo "$v" | cut -d. -f2)" "${v##*.}"
}

want=$(key "$1")
best=
bestkey=
while IFS= read -r t || [ -n "$t" ]; do
	printf '%s' "$t" | grep -Eqx "$pattern" || die "a release's tag is not vMAJOR.MINOR.PATCH: '$t'"
	k=$(key "$t")
	if [ "$(printf '%s\n%s\n' "$k" "$want" | LC_ALL=C sort | tail -n 1)" = "$k" ]; then
		die "$1 is not above the release $t: a release's tag must be above every release's"
	fi
	if [ "$(printf '%s\n%s\n' "$k" "$want" | LC_ALL=C sort | head -n 1)" = "$k" ] && [ "$k" != "$want" ]; then
		if [ -z "$bestkey" ] || [ "$(printf '%s\n%s\n' "$k" "$bestkey" | LC_ALL=C sort | tail -n 1)" = "$k" ]; then
			best=$t
			bestkey=$k
		fi
	fi
done
[ -z "$best" ] || echo "$best"

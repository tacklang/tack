#!/bin/sh
# gate.sh decides whether a tag may be released, before anything is
# built, run from the root of a git checkout holding the tagged commit
# and the main branch:
#
#   sh tools/release/gate.sh TAG COMMIT MAIN RUNS RELEASES PUBKEY
#
# TAG is the tag, COMMIT the commit it names, peeled from an annotated
# tag (git rev-parse "refs/tags/TAG^{commit}"), MAIN the ref of the main
# branch, origin/main in the workflow. RUNS is a file of the ci
# workflow's runs, .github/workflows/ci.yml's, one a line, four fields
# separated by tabs, as the workflow lists them with gh api: the commit
# it ran on, the event that started it, its branch and its conclusion
# (null for a run still going). RELEASES is a file of the published
# releases' tags, one a line (see previous.sh). PUBKEY is the release
# public key file, tools/release/minisign.pub. It fails, saying why,
# unless all of these hold, in this order, the cheap ones first:
#
#   - TAG is vMAJOR.MINOR.PATCH and COMMIT a full commit SHA;
#   - PUBKEY's key is set, not the placeholder UNSET, so a release is not
#     built for hours only to fail at its signing;
#   - COMMIT is on main: an ancestor of MAIN, or MAIN itself;
#   - a run of RUNS is on COMMIT, started by a push, on the branch main,
#     and ended in success; a run started by hand, workflow_dispatch, or
#     on another branch, or one that failed, was cancelled or has not
#     ended, never counts;
#   - TAG is above every release of RELEASES (see previous.sh).
#
# On success it writes the release TAG bootstraps from, previous.sh's
# answer, on standard output, nothing for a first release, and its
# messages on standard error. It needs git, grep, sed and coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "gate: $*" >&2
	exit 1
}

[ $# -eq 6 ] || die "usage: tools/release/gate.sh TAG COMMIT MAIN RUNS RELEASES PUBKEY"
tag=$1
commit=$2
main=$3
runs=$4
releases=$5
pubkey=$6
here=$(dirname "$0")

printf '%s' "$tag" | grep -Eqx 'v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})' ||
	die "the tag '$tag' is not vMAJOR.MINOR.PATCH"
printf '%s' "$commit" | grep -Eqx '[0-9a-f]{40}' || die "'$commit' is not a full commit SHA"

key=$(sed -n 2p "$pubkey") || die "cannot read $pubkey"
[ -n "$key" ] && [ "$key" != UNSET ] ||
	die "the release public key in $pubkey is not set yet (see RELEASING.md): the release could not be signed"

git rev-parse --verify --quiet "$main^{commit}" >/dev/null || die "there is no $main to check $commit against"
git merge-base --is-ancestor "$commit" "$main" || die "$commit is not on $main: only a commit of main is released"

tab=$(printf '\t')
found=0
seen=
while IFS="$tab" read -r sha event branch conclusion || [ -n "$sha" ]; do
	seen="$seen [$sha $event $branch $conclusion]"
	if [ "$sha" = "$commit" ] && [ "$event" = push ] && [ "$branch" = main ] && [ "$conclusion" = success ]; then
		found=1
	fi
done <"$runs"
[ "$found" = 1 ] || die "no run of ci, started by a push to main, passed on $commit; its runs:${seen:- none}"
echo "gate: ci passed on $commit, a commit of $main" >&2

sh "$here/previous.sh" "$tag" <"$releases"

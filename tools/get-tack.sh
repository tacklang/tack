#!/bin/sh
# get-tack.sh downloads a release of the Tack compiler, verifies it and
# installs it, for Linux on x86-64:
#
#   curl -fsSL https://raw.githubusercontent.com/tacklang/tack/main/tools/get-tack.sh | sh
#   curl -fsSL .../get-tack.sh | sh -s -- --version v0.1.0
#
# Options:
#
#   --version vX.Y.Z  the release to get, the latest by default
#   --download DIR    verify and extract the release into DIR, which must
#                     not exist yet, and install nothing: DIR is then a
#                     TACK_HOME, with the compiler at DIR/bin/tack
#   --strict          fail when minisign or gh is missing or the public
#                     key below is not set, rather than warn, as the
#                     release workflow runs it
#
# It downloads SHA256SUMS, its signature SHA256SUMS.minisig, the
# tarball it names, tack-vX.Y.Z-linux-x86_64.tar.xz, and the tarball's
# signature, from $TACK_RELEASES/download/vX.Y.Z/, or, for the latest,
# SHA256SUMS and its signature from $TACK_RELEASES/latest/download/ and
# the rest from the version SHA256SUMS names. TACK_RELEASES is
# https://github.com/tacklang/tack/releases unless set, as a test sets it
# to a file:// directory laid out the same. Then, in this order, it
# checks:
#
#   1. SHA-256: SHA256SUMS holds exactly one line, a digest and the
#      tarball's name, for the version asked for, and the tarball has
#      that digest;
#   2. minisign, when it is on PATH: SHA256SUMS and the tarball verify
#      with the release public key, TACK_RELEASE_KEY below, or
#      TACK_MINISIGN_KEY when the environment sets it, as a test does
#      with a key of its own, and each signature's trusted comment is
#      "tack vX.Y.Z <file>", so a signed file of another release, or
#      another file, does not pass;
#   3. gh, when it is on PATH: gh attestation verify of the tarball with
#      -R tacklang/tack, --signer-workflow
#      tacklang/tack/.github/workflows/release.yml, --source-ref
#      refs/tags/vX.Y.Z and --deny-self-hosted-runners: GitHub's record
#      that the release workflow, run for that tag on GitHub's own
#      runners, built it. It needs gh to be logged in: a gh that is on
#      PATH but not logged in fails this check, and so the install.
#
# Each check that cannot run, minisign or gh not on PATH, or the key not
# set yet, is warned of on standard error, saying what it leaves
# unchecked, and with --strict fails instead. Any check that runs and
# does not pass, and any download that fails, fails the script, exit
# status 1, before anything is extracted or installed.
#
# Then it lists the tarball before extracting it, with its entries' names
# as stored, and fails unless every entry is a regular file or a
# directory, no symbolic link, hard link, device or other kind, and every
# name is the release's one top directory, tack-vX.Y.Z-linux-x86_64, or
# below it, with no .. and no leading /.
#
# The latest release is whatever $TACK_RELEASES/latest/download serves.
# Its SHA256SUMS and signatures are checked as any release's are, but no
# check can tell an older release, served whole with its own signatures
# as the latest, from the latest: the script says which version it
# installs, and --version vX.Y.Z pins the one it gets.
#
# Installing follows make install's rules, with tools/install.sh, which
# the tarball holds, run from the extracted release: the compiler to
# $BINDIR, or $PREFIX/bin, or ~/.local/bin, and std to
# $TACK_HOME/src/std, or ~/.tack/src/std, each replaced whole and only
# once both are copied, so a failed install leaves a previous one as it
# was. It refuses, before anything is written, a TACK_HOME, or ~/.tack,
# inside a Tack source tree, a directory holding a tack.mod or below
# one, symbolic links resolved: installing there would replace that
# tree's own src/std. The whole script is one function called on its
# last line, so a download of it cut short runs nothing. It needs curl,
# sha256sum, tar, xz, grep, sed and coreutils.
set -eu

# TACK_REPO is the repository the releases are of.
TACK_REPO=tacklang/tack

# TACK_RELEASE_KEY is the release public key, the second line of
# tools/release/minisign.pub, kept the same as it and as the README's by
# make test-release.
TACK_RELEASE_KEY=RWTmnXMYKLo0PX3oG7Qy0y5ODyfM5aHeRVgvSTM9yIbdI/2ibij7Adxn

# version_pattern is what a release's tag must be, vMAJOR.MINOR.PATCH.
version_pattern='v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})'

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "get-tack: $*" >&2
	exit 1
}

# warn writes its arguments as a warning on standard error, or fails as
# die does with --strict.
warn() {
	if [ "$strict" = 1 ]; then
		die "$* (--strict)"
	fi
	echo "get-tack: WARNING: $*" >&2
}

# fetch downloads URL $1 to file $2, failing on any HTTP error, and
# following redirects only to https.
fetch() {
	curl --fail --silent --show-error --location \
		--proto '=https,file' --proto-redir '=https' \
		--output "$2" "$1" || die "cannot download $1"
}

# in_source_tree succeeds when directory $1, symbolic links resolved, or
# any directory above it holds a tack.mod.
in_source_tree() {
	d=$(realpath -m "$1")
	while :; do
		if [ -e "$d/tack.mod" ]; then
			tree=$d
			return 0
		fi
		[ "$d" != / ] || return 1
		d=$(dirname "$d")
	done
}

# check_members fails unless every entry of tarball $1 is a regular
# file or a directory named $2, its one top directory, or below it, with
# no .. component and no leading /. tar lists each name as stored, with
# --absolute-names, so a leading / is not taken off, and escapes a
# newline in a name, so each entry is one line of each listing.
check_members() {
	LC_ALL=C tar --absolute-names --quoting-style=escape -tJf "$1" >"$tmp/names" ||
		die "cannot list $1"
	LC_ALL=C tar --absolute-names --quoting-style=escape -tvJf "$1" >"$tmp/kinds" ||
		die "cannot list $1"
	[ "$(grep -c '' "$tmp/names")" = "$(grep -c '' "$tmp/kinds")" ] || die "the two listings of $1 differ in length"
	while IFS= read -r l; do
		case $l in
		-* | d*) ;;
		*) die "$1 holds an entry that is no regular file or directory: $l" ;;
		esac
	done <"$tmp/kinds"
	while IFS= read -r n; do
		case $n in
		"$2" | "$2/") ;;
		"$2"/*) ;;
		*) die "$1 holds $n, outside its one top directory $2" ;;
		esac
		case /$n/ in
		*/../*) die "$1 holds $n, which climbs out with .." ;;
		esac
	done <"$tmp/names"
}

# main is the whole script.
main() {
	version=
	download=
	strict=0
	while [ $# -gt 0 ]; do
		case $1 in
		--version)
			[ $# -ge 2 ] || die "--version needs a version"
			version=$2
			shift 2
			;;
		--download)
			[ $# -ge 2 ] || die "--download needs a directory"
			download=$2
			shift 2
			;;
		--strict)
			strict=1
			shift
			;;
		*) die "unknown argument $1; usage: get-tack.sh [--version vX.Y.Z] [--download DIR] [--strict]" ;;
		esac
	done
	if [ -n "$version" ]; then
		printf '%s' "$version" | grep -Eqx "$version_pattern" || die "--version must be vMAJOR.MINOR.PATCH, not '$version'"
		case $version in
		*'
'*) die "--version must be one line" ;;
		esac
	fi
	if [ -n "$download" ] && { [ -e "$download" ] || [ -L "$download" ]; }; then
		die "$download is already there: --download needs a directory that does not exist yet"
	fi
	case $(uname -s)/$(uname -m) in
	Linux/x86_64) ;;
	*) die "releases are built for Linux on x86_64 alone, not $(uname -s) on $(uname -m)" ;;
	esac
	for t in curl sha256sum tar xz grep sed realpath mktemp; do
		command -v "$t" >/dev/null || die "$t is not on PATH"
	done
	key=${TACK_MINISIGN_KEY:-$TACK_RELEASE_KEY}
	releases=${TACK_RELEASES:-https://github.com/$TACK_REPO/releases}
	releases=${releases%/}

	if [ -z "$download" ]; then
		home=${TACK_HOME:-}
		if [ -z "$home" ]; then
			[ -n "${HOME:-}" ] || die "neither TACK_HOME nor HOME is set"
			home=$HOME/.tack
		fi
		case $home in
		/*) ;;
		*) die "TACK_HOME, or HOME, must be an absolute path, not $home" ;;
		esac
		if in_source_tree "$home"; then
			die "$home is inside a Tack source tree, $tree, which holds a tack.mod: installing std there would replace that tree's own"
		fi
	fi

	tmp=$(mktemp -d "${TMPDIR:-/tmp}/get-tack.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM

	if [ -n "$version" ]; then
		sums_from=$releases/download/$version
	else
		sums_from=$releases/latest/download
	fi
	fetch "$sums_from/SHA256SUMS" "$tmp/SHA256SUMS"
	fetch "$sums_from/SHA256SUMS.minisig" "$tmp/SHA256SUMS.minisig"

	# 1. SHA-256.
	lines=$(grep -c '' "$tmp/SHA256SUMS") || die "SHA256SUMS is empty"
	[ "$lines" -eq 1 ] || die "SHA256SUMS holds $lines lines, not the one of the one tarball a release has"
	line=$(cat "$tmp/SHA256SUMS")
	printf '%s' "$line" | grep -Eqx "[0-9a-f]{64}  tack-$version_pattern-linux-x86_64\\.tar\\.xz" ||
		die "SHA256SUMS's line is not a digest and a tarball's name: $line"
	want=${line%%  *}
	name=${line#*  }
	named=${name#tack-}
	named=${named%-linux-x86_64.tar.xz}
	if [ -n "$version" ]; then
		[ "$named" = "$version" ] || die "SHA256SUMS of $version names $name, a tarball of another version"
	else
		version=$named
		echo "get-tack: the latest release is $version"
	fi
	fetch "$releases/download/$version/$name" "$tmp/$name"
	fetch "$releases/download/$version/$name.minisig" "$tmp/$name.minisig"
	got=$(sha256sum "$tmp/$name")
	got=${got%% *}
	[ "$got" = "$want" ] || die "$name has SHA-256 $got, not $want, the one SHA256SUMS gives: it is not the release's"
	echo "get-tack: SHA-256 of $name matches SHA256SUMS"

	# 2. minisign.
	if ! command -v minisign >/dev/null; then
		warn "minisign is not on PATH, so the signatures of SHA256SUMS and $name are not checked"
	elif [ "$key" = UNSET ]; then
		warn "no release public key is set in this script, so the signatures of SHA256SUMS and $name are not checked"
	else
		for f in SHA256SUMS "$name"; do
			comment=$(minisign -V -P "$key" -m "$tmp/$f" -x "$tmp/$f.minisig" -Q) ||
				die "the signature of $f does not verify with the release public key $key"
			[ "$comment" = "tack $version $f" ] ||
				die "the signature of $f is of '$comment', not of 'tack $version $f'"
		done
		echo "get-tack: minisign signatures of SHA256SUMS and $name verify"
	fi

	# 3. GitHub's attestation.
	if ! command -v gh >/dev/null; then
		warn "gh is not on PATH, so GitHub's attestation that the release workflow built $name is not checked"
	else
		gh attestation verify "$tmp/$name" -R "$TACK_REPO" \
			--signer-workflow "$TACK_REPO/.github/workflows/release.yml" \
			--source-ref "refs/tags/$version" --deny-self-hosted-runners >"$tmp/gh.out" 2>&1 || {
			cat "$tmp/gh.out" >&2
			die "gh attestation verify of $name failed (gh must be logged in, with gh auth login, to check it; take gh off PATH to skip the check with a warning)"
		}
		echo "get-tack: GitHub's attestation of $name verifies"
	fi

	check_members "$tmp/$name" "tack-$version-linux-x86_64"
	mkdir "$tmp/x"
	tar -xJf "$tmp/$name" -C "$tmp/x" --no-same-owner || die "cannot extract $name"
	top=$tmp/x/tack-$version-linux-x86_64
	[ "$(ls -A "$tmp/x")" = "tack-$version-linux-x86_64" ] && [ -x "$top/bin/tack" ] && [ -f "$top/tools/install.sh" ] ||
		die "$name does not hold the one directory tack-$version-linux-x86_64 with bin/tack and tools/install.sh"
	echo "get-tack: $name holds tack $version"

	if [ -n "$download" ]; then
		mv -T "$top" "$download" || die "cannot move the release to $download"
		echo "get-tack: $version is in $download; set TACK_HOME to it and put its bin on PATH to use it"
		return 0
	fi
	(cd "$top" && sh tools/install.sh install bin/tack) || die "installing $version failed"
	echo "get-tack: installed tack $version; make sure the directory it is in is on PATH"
}

main "$@"

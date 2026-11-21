#!/bin/sh
# sign.sh signs a release with minisign:
#
#   sh tools/release/sign.sh DIR PUBKEY
#
# DIR holds the release's files, each tarball, tack-*.tar.xz, and
# SHA256SUMS; PUBKEY is the public key file the signatures must verify
# with, tools/release/minisign.pub in the workflow. The secret key and
# its password come from the environment, never from the command line:
# MINISIGN_SECRET_KEY, the whole text of the secret key file minisign -G
# writes, and MINISIGN_PASSPHRASE, its password, which must not be empty,
# since a release key is always password-protected. VERSION is the
# release's tag.
#
# It writes the key to a file of its own in a new directory below
# $TMPDIR, made under umask 077, so the file is mode 600 from the start,
# and removes the directory however the script ends. Each file F gets
# F.minisig, whose trusted comment, which the signature covers, is
#
#   tack VERSION F
#
# so a signature of one release's file cannot pass for another's, nor
# one file's for another file's (see tools/get-tack.sh). The password is
# given to minisign on its standard input, which minisign reads it from
# when that is no terminal, and never written anywhere else; the script
# runs no command with set -x. Each signature is then verified with
# PUBKEY and its trusted comment checked, so a secret key that is not
# PUBKEY's fails the release before anything is published. It needs
# minisign and coreutils.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "sign: $*" >&2
	exit 1
}

[ $# -eq 2 ] || die "usage: VERSION=vX.Y.Z tools/release/sign.sh DIR PUBKEY"
dir=$1
pubkey=$(realpath -e "$2") || die "no public key file $2"
[ -n "${VERSION:-}" ] || die "VERSION is not set"
[ -n "${MINISIGN_SECRET_KEY:-}" ] || die "MINISIGN_SECRET_KEY is not set"
[ -n "${MINISIGN_PASSPHRASE:-}" ] || die "MINISIGN_PASSPHRASE is not set: the release key is password-protected"
command -v minisign >/dev/null || die "minisign is not on PATH"

keydir=$(umask 077 && mktemp -d "${TMPDIR:-/tmp}/sign.XXXXXX")
trap 'rm -rf "$keydir"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
(umask 077 && printf '%s\n' "$MINISIGN_SECRET_KEY" >"$keydir/key")

cd "$dir"
set -- tack-*.tar.xz
[ -f "$1" ] || die "$PWD holds no tarball, tack-*.tar.xz"
[ -f SHA256SUMS ] || die "$PWD holds no SHA256SUMS"
for f in "$@" SHA256SUMS; do
	rm -f "$f.minisig"
	printf '%s\n' "$MINISIGN_PASSPHRASE" |
		minisign -S -s "$keydir/key" -m "$f" -x "$f.minisig" -t "tack $VERSION $f" >/dev/null 2>"$keydir/err" ||
		{
			cat "$keydir/err" >&2
			die "signing $f failed"
		}
	[ -s "$f.minisig" ] || die "minisign wrote no $f.minisig"
	comment=$(minisign -V -p "$pubkey" -m "$f" -x "$f.minisig" -Q) ||
		die "$f.minisig does not verify with $pubkey: the secret key is not that public key's"
	[ "$comment" = "tack $VERSION $f" ] || die "$f.minisig's trusted comment is '$comment', not 'tack $VERSION $f'"
	echo "sign: signed $f"
done

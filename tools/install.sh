#!/bin/sh
# install.sh does the work of the Makefile's install and install-std,
# run from the root of the port's tree, the directory make runs it in:
#
#   sh tools/install.sh std           what make install-std does
#   sh tools/install.sh check         make install's checks, before it builds
#   sh tools/install.sh install STAGE make install's last step: STAGE, the
#                                     compiler the bootstrap built, and std
#
# Every path it is given comes from the environment, never from text
# make pasted into a command, so a path holding a quote, a backtick or a
# space is taken as it is: TACK_HOME and HOME say where std goes, and
# BINDIR, PREFIX and HOME where the compiler goes, as the Makefile says.
#
# std is installed to $TACK_HOME/src/std, or to ~/.tack/src/std when
# TACK_HOME is unset or empty, where the compiler looks for it
# (driver/main.tck's find_std). That directory, its parent with every
# symbolic link resolved, and the directory it names when it is itself a
# symbolic link must neither be inside this source tree nor hold it, and
# TACK_HOME, or HOME when it is used, must be absolute: otherwise
# installing would remove this tree's own src/std, or the whole tree,
# and it fails before anything is written. The new std is copied in
# full to a directory beside the old one, the builtins' documentation
# and the runtime header written into it from runtime/, read-only, and
# only then renamed into place, the old one renamed aside first and
# removed last, so a failed copy leaves the old std as it was and a
# file deleted from src/std does not linger.
#
# install checks, before anything is replaced, that std's directory and
# the compiler's can be written and that $BINDIR/tack is not a
# directory. It copies std beside the old one and the compiler into
# $BINDIR under a name of its own, and only then renames both into
# place, std first, putting the old std back when the compiler's rename
# fails, so a reinstall replaces both or neither. A rename never leaves
# a half-written tack, and a process already running the old tack runs
# on unaffected.
#
# Ended by SIGHUP, SIGINT or SIGTERM, or by any failure, it undoes what
# it did: the old std is put back when it was renamed aside, and the new
# one taken back out when the compiler was not yet renamed. Ended by
# SIGKILL, it leaves its temporary files, named for its PID; each later
# run removes those of a run no longer running, putting back an old std
# left aside with nothing in its place (see sweep).
#
# Each message starts with the target's name, install-std: or install:,
# and the script exits 1 after it. It needs coreutils alone.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "$*" >&2
	exit 1
}

# writable_dir succeeds when path $1, or the nearest of its ancestors
# that exists, is a directory this process can write to.
writable_dir() {
	d=$1
	while [ ! -e "$d" ] && [ ! -L "$d" ]; do
		d=$(dirname "$d")
	done
	[ -d "$d" ] && [ -w "$d" ]
}

# std_dest sets home, the directory std is installed below as given,
# srcdir, its src with every symbolic link resolved, and dest, the
# directory std is renamed into, srcdir/std, failing with the message
# of target $1 when any of them is not allowed (see above).
std_dest() {
	if [ -n "${TACK_HOME:-}" ]; then
		home=$TACK_HOME
		case $home in
		/*) ;;
		*) die "$1: TACK_HOME must be an absolute path, not $home" ;;
		esac
	elif [ -n "${HOME:-}" ]; then
		case $HOME in
		/*) ;;
		*) die "$1: HOME must be an absolute path, not $HOME" ;;
		esac
		home=$HOME/.tack
	else
		die "$1: neither TACK_HOME nor HOME is set"
	fi
	root=$(realpath -e .)
	srcdir=$(realpath -m "$home/src")
	dest=$srcdir/std
	named=$(realpath -m "$home/src/std")
	for d in "$dest" "$named"; do
		case "$d/" in
		"$root/"*) die "$1: $d is inside this source tree, $root" ;;
		esac
		case "$root/" in
		"$d/"*) die "$1: $d holds this source tree, $root" ;;
		esac
	done
	writable_dir "$srcdir" || die "$1: cannot write $srcdir"
}

# bin_dest sets bindir, the directory the compiler is installed to, as
# BINDIR, PREFIX and HOME say, failing when it is not absolute, cannot
# be written, or holds a directory named tack.
bin_dest() {
	if [ -n "${BINDIR:-}" ]; then
		bindir=$BINDIR
		var=BINDIR
	elif [ -n "${PREFIX:-}" ]; then
		bindir=$PREFIX/bin
		var=PREFIX
	elif [ -n "${HOME:-}" ]; then
		bindir=$HOME/.local/bin
		var=HOME
	else
		die "install: neither BINDIR, PREFIX nor HOME is set"
	fi
	case $bindir in
	/*) ;;
	*) die "install: $var must be an absolute path, not ${BINDIR:-${PREFIX:-$HOME}}" ;;
	esac
	if [ -d "$bindir/tack" ] && [ ! -L "$bindir/tack" ]; then
		die "install: $bindir/tack is a directory"
	fi
	writable_dir "$bindir" || die "install: cannot write $bindir"
}

# The temporary files and directories made so far, removed on exit;
# the old std once renamed aside, put back by restore_std; and staged,
# 1 once newstd is complete. Whether a rename has happened is read from
# the files themselves, since a signal can end the script between a
# rename and the line after it.
newstd=
newbin=
oldstd=
staged=0

# cleanup runs on every exit, a signal's included: when install ends
# with the new std in place, newstd renamed away, and the compiler not,
# newbin still there, it takes the new std back out, so neither is
# replaced; when the old std is renamed aside and nothing is at dest, it
# puts the old std back; then it removes the temporary files left.
cleanup() {
	if [ "$staged" = 1 ] && [ -n "$newbin" ] && [ -e "$newbin" ] && [ ! -e "$newstd" ] && [ -e "$dest" ]; then
		mv -T "$dest" "$newstd" || echo "install: cannot take the new std back out of $dest" >&2
	fi
	if [ -n "$oldstd" ] && [ ! -e "$dest" ] && [ ! -L "$dest" ]; then
		mv -T "$oldstd" "$dest" || echo "install: cannot put the old std, $oldstd, back at $dest" >&2
	fi
	if [ -n "$newstd" ]; then rm -rf "$newstd"; fi
	if [ -n "$newbin" ]; then rm -f "$newbin"; fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# sweep removes from directory $1 what a run of this script that ended
# without its cleanup, killed by SIGKILL, left there: each entry named
# exactly .std.old.<pid>, .std.install.<pid> or .tack.install.<pid>,
# for the kinds $2 names, std or tack, owned by this user, whose pid no
# process runs. Nothing else is touched. A .std.old.<pid> is the std
# that run renamed aside; when nothing is at dest it is put back there
# instead of removed, since it was the std installed last.
sweep() {
	[ -d "$1" ] || return 0
	for p in "$1"/.std.old.* "$1"/.std.install.* "$1"/.tack.install.*; do
		[ -e "$p" ] || [ -L "$p" ] || continue
		name=${p##*/}
		case $name in
		.std.old.*) kind=std pid=${name#.std.old.} ;;
		.std.install.*) kind=std pid=${name#.std.install.} ;;
		.tack.install.*) kind=tack pid=${name#.tack.install.} ;;
		*) continue ;;
		esac
		[ "$kind" = "$2" ] || continue
		case $pid in
		'' | *[!0-9]*) continue ;;
		esac
		[ -O "$p" ] || continue
		if kill -0 "$pid" 2>/dev/null; then
			continue
		fi
		case $name in
		.std.old.*)
			if [ ! -e "$dest" ] && [ ! -L "$dest" ]; then
				echo "$3: putting back the std an earlier run left at $p" >&2
				mv -T "$p" "$dest"
				continue
			fi
			;;
		esac
		rm -rf "$p"
	done
}

# stage_std copies std, and the runtime's copies, to newstd, a new
# directory beside dest.
stage_std() {
	mkdir -p "$srcdir"
	p=$srcdir/.std.install.$$
	[ ! -e "$p" ] && [ ! -L "$p" ] || die "$p is in the way"
	newstd=$p
	cp -R src/std "$newstd"
	mkdir -p "$newstd/builtin" "$newstd/include/tack"
	cp runtime/builtin.tck "$newstd/builtin/builtin.tck"
	cp runtime/tack/tack.h "$newstd/include/tack/tack.h"
	chmod a-w "$newstd/builtin/builtin.tck" "$newstd/include/tack/tack.h"
	staged=1
}

# swap_std renames dest aside, to oldstd, when there is one, and newstd
# into its place, putting dest back when that rename fails.
swap_std() {
	if [ -e "$dest" ] || [ -L "$dest" ]; then
		p=$srcdir/.std.old.$$
		[ ! -e "$p" ] && [ ! -L "$p" ] || die "$p is in the way"
		oldstd=$p
		mv -T "$dest" "$oldstd"
	fi
	if ! mv -T "$newstd" "$dest"; then
		restore_std
		exit 1
	fi
}

# unswap_std undoes swap_std: the new std back to newstd, for cleanup to
# remove, and the old one back at dest.
unswap_std() {
	mv -T "$dest" "$newstd"
	restore_std
}

# restore_std puts the old std, renamed aside by swap_std, back at dest.
restore_std() {
	if [ -n "$oldstd" ]; then
		mv -T "$oldstd" "$dest"
		oldstd=
	fi
}

# drop_old removes the old std once nothing can need it back.
drop_old() {
	if [ -n "$oldstd" ]; then
		rm -rf "$oldstd"
		oldstd=
	fi
}

case "${1:-}" in
std)
	[ $# -eq 1 ] || die "usage: tools/install.sh std"
	std_dest install-std
	sweep "$srcdir" std install-std
	echo "installing std to $home/src/std"
	stage_std
	swap_std
	newstd=
	drop_old
	;;
check)
	[ $# -eq 1 ] || die "usage: tools/install.sh check"
	bin_dest
	std_dest install
	;;
install)
	[ $# -eq 2 ] || die "usage: tools/install.sh install STAGE"
	bin_dest
	std_dest install
	sweep "$srcdir" std install
	sweep "$bindir" tack install
	echo "installing std to $home/src/std"
	stage_std
	echo "installing tack to $bindir/tack"
	mkdir -p "$bindir"
	newbin=$bindir/.tack.install.$$
	cp "$2" "$newbin"
	swap_std
	if ! mv -fT "$newbin" "$bindir/tack"; then
		unswap_std
		exit 1
	fi
	newstd=
	newbin=
	drop_old
	;;
*)
	die "usage: tools/install.sh std | check | install STAGE"
	;;
esac

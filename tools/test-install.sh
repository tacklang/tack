#!/bin/sh
# test-install.sh checks make install and make install-std, the way a
# first install and a reinstall use them, and every way they must
# refuse, with TACK, the compiler named by its one argument, a path or a
# name looked up on PATH, as the compiler that builds the port. make
# test-install runs it from the root of the port's tree, the directory
# it must be run from.
#
# It copies the tree, all of it but bin/, into a temporary directory of
# its own below $TMPDIR, or /tmp when TMPDIR is unset, and runs every
# make there or in smaller copies, the Makefile, tools/, src/ and
# runtime/ alone, so the tree it is run from is never written to. Every
# make and every run of an installed compiler gets an environment of
# PATH, TMPDIR and what the check names alone, through env -i, with HOME
# a directory below that temporary one, so nothing is ever installed
# into the real home, and no TACK_HOME, TACK_CACHE or XDG_CACHE_HOME it
# inherited can point a run at another std or cache. Two stand-ins for
# a compiler, shell scripts that write a copy of themselves where --out
# says, the second adding a line each time, so the stages it builds all
# differ, check install's failures and paths without building the port.
#
# It checks, in order:
#
#   - that make install-std, with neither TACK_HOME nor HOME set, fails
#     saying so, and that make install fails, before it builds anything,
#     with neither TACK_HOME nor HOME, with TACK_HOME but no BINDIR,
#     PREFIX or HOME, and with a TACK that is not there, writing nothing
#     in each case;
#   - that make install-std refuses, deleting nothing, a TACK_HOME or a
#     HOME that is not absolute, given in the environment or on make's
#     command line, and every destination inside the tree or holding it:
#     TACK_HOME the tree, with a trailing slash, through a symbolic link,
#     below the tree's src/std, or the tree's src/std/..; ~/.tack, its
#     src or its src/std a symbolic link into the tree; and the tree
#     below the destination; that make install refuses the same; and
#     that it installs where ~/.tack/src/std is a symbolic link to a
#     directory elsewhere, replacing the link and keeping that directory,
#     to a TACK_HOME holding a * beside a directory it would match, to
#     /a with the tree at /ab, and to a home that does not exist yet;
#   - that a failed copy leaves the std installed before as it was;
#   - with a stand-in for mv first on PATH that signals the script, that
#     install-std ended by SIGTERM between its two renames puts the old
#     std back, and that after one ended by SIGKILL there, which leaves
#     no std, the next run puts the old std back from what it left,
#     removes the rest, and keeps every entry near those names that no
#     run made, or that a running process did;
#   - a first install with HOME alone set, to a path with a space in it:
#     the compiler at ~/.local/bin/tack, the same bytes as stage 3 and
#     stage 2 of the install's bootstrap, and std at ~/.tack/src/std, the
#     same files as the tree's src/std, its copies of the builtins'
#     documentation and the runtime header runtime/'s, read-only;
#   - that the installed compiler builds and runs a program that imports
#     std/fmt, and passes tack test of std/hex, run in the installed std,
#     with nothing in the environment but HOME pointing at a std;
#   - that make install-std replaces what is there, read-only copies
#     included, so a file deleted from src/std, or one that was never in
#     it, does not linger; that an empty TACK_HOME counts as unset; and
#     that a set TACK_HOME wins over HOME, which is then never written;
#   - a reinstall with the installed compiler as TACK and BINDIR its own
#     directory, which wins over PREFIX, and TACK_HOME set: the same
#     bytes, as a new file, and nothing else left in BINDIR;
#   - that an install whose stage 2 and stage 3 differ fails, leaving
#     the compiler and std installed before as they were; that a BINDIR
#     that cannot be written, or holds a directory named tack, fails
#     before anything is built or replaced; that PREFIX and TACK_HOME
#     install where they say; that a reinstall replaces a compiler that
#     differs; that a BINDIR holding a backtick, or quotes, is taken
#     as it is, running nothing; and that install ended by SIGTERM before
#     the compiler's rename keeps the compiler and std as they were, and
#     that the install after one ended by SIGKILL there removes what it
#     left and installs both.
#
# Every run of clang goes through a stand-in for it that records each
# failed run, with clang's standard error, the free space of the
# temporary directory and the same command again with -v, which shows
# the linker's command; a failed check shows that record.
#
# The checks of directories that cannot be written or read are skipped,
# saying so, when it runs as root, which can write and read them all.
# It stops at the first check that fails, saying which, with exit status
# 1, and removes its temporary directory however it ends. It needs make,
# clang, coreutils, cmp and diff, and no Go.
set -eu

if [ $# -ne 1 ]; then
	echo "usage: tools/test-install.sh TACK" >&2
	exit 2
fi
tack=$(command -v "$1") || {
	echo "test-install: FAIL: no compiler $1" >&2
	exit 1
}
case $tack in
/*) ;;
*) tack="$(pwd)/$tack" ;;
esac
src=$(pwd)

tmp=$(mktemp -d "${TMPDIR:-/tmp}/test-install.XXXXXX")
trap 'chmod -R u+rwx "$tmp"; rm -rf "$tmp"' EXIT
tmp=$(realpath -e "$tmp")
runtmp="$tmp/tmp"
tree="$tmp/tree"
home="$tmp/home dir"
mkdir "$runtmp" "$tree" "$home"
for f in "$src"/*; do
	[ "${f##*/}" = bin ] || cp -R "$f" "$tree/"
done
root=0
[ "$(id -u)" -ne 0 ] || root=1

# clanglog records each run of clang that fails, from a stand-in for
# clang first on PATH for every run the script makes: the command line,
# clang's standard error, the free space of the temporary directory, and
# the same command run again with -v, which names the linker's command
# line, and how that ended. The stand-in passes clang's output and exit
# status through unchanged, its standard error once clang ends.
clanglog="$tmp/clang-failures.log"
realclang=$(command -v clang) || {
	echo "test-install: FAIL: no clang on PATH" >&2
	exit 1
}
mkdir "$tmp/clang"
cat >"$tmp/clang/clang" <<EOF
#!/bin/sh
err=\$(mktemp "$runtmp/clang-stderr.XXXXXX") || exit 1
"$realclang" "\$@" 2>"\$err"
status=\$?
cat "\$err" >&2
if [ \$status -ne 0 ]; then
	{
		echo "== clang exited \$status at \$(date +%T.%N)"
		for a in "\$@"; do printf '  %s\n' "\$a"; done
		echo "-- its standard error:"
		cat "\$err"
		echo "-- free space where TMPDIR, \${TMPDIR:-/tmp}, is:"
		df -h "\${TMPDIR:-/tmp}"
		echo "-- the same command again, with -v:"
		"$realclang" -v "\$@" 2>&1
		echo "-- which exited \$?"
	} >>"$clanglog"
fi
rm -f "\$err"
exit \$status
EOF
chmod +x "$tmp/clang/clang"
PATH="$tmp/clang:$PATH"

# fail reports the check that failed, its arguments, and every run of
# clang that failed, and stops.
fail() {
	echo "test-install: FAIL: $*" >&2
	if [ -s "$clanglog" ]; then
		echo "test-install: the runs of clang that failed:" >&2
		cat "$clanglog" >&2
	fi
	exit 1
}

# clean runs its arguments, VAR=value settings and then a command, as
# env runs them, in an environment of PATH, TMPDIR and those settings
# alone.
clean() {
	env -i PATH="$PATH" TMPDIR="$runtmp" "$@"
}

# must_fail runs its arguments but the first, as clean runs them, and
# fails the check unless they fail and their output holds a line that
# is the first argument.
must_fail() {
	want=$1
	shift
	if out=$(clean "$@" 2>&1); then
		fail "$* succeeded, and must fail with: $want"
	fi
	case "
$out
" in
	*"
$want
"*) ;;
	*) fail "$* did not say: $want; it said:
$out" ;;
	esac
}

# absent fails the check unless none of its arguments exists.
absent() {
	for p in "$@"; do
		if [ -e "$p" ] || [ -L "$p" ]; then
			fail "$p was written"
		fi
	done
}

# check_std fails the check unless $1/src/std holds the same files as
# the src/std of tree $2, the tree's copy by default, its copies of the
# builtins' documentation and the runtime header runtime/'s, read-only,
# and nothing of an install is left beside it.
check_std() {
	std="$1/src/std"
	from=${2:-$tree}
	diff -r "$from/src/std" "$std" >&2 || fail "$std differs from src/std"
	for pair in "builtin/builtin.tck runtime/builtin.tck" "include/tack/tack.h runtime/tack/tack.h"; do
		installed="$std/${pair% *}"
		cmp "$from/${pair#* }" "$installed" || fail "$installed is not ${pair#* }"
		case $(stat -c %A "$installed") in
		*w*) fail "$installed is writable: $(stat -c %A "$installed")" ;;
		esac
	done
	no_leftovers "$(dirname "$std")"
}

# no_leftovers fails the check when directory $1 holds a temporary
# file or directory of an install, .std.* or .tack.*.
no_leftovers() {
	for p in "$1"/.std.* "$1"/.tack.*; do
		absent "$p"
	done
}

# mini makes a tree at $1 of what install-std needs alone, the
# Makefile, tools/, src/ and runtime/ of the tree's copy, with a file
# SENTINEL in its src/std.
mini() {
	mkdir -p "$1"
	cp -R "$tree/Makefile" "$tree/tools" "$tree/src" "$tree/runtime" "$1/"
	echo keep >"$1/src/std/SENTINEL"
}

# intact fails the check unless tree $1 still holds its Makefile and
# the SENTINEL of its src/std.
intact() {
	[ -f "$1/Makefile" ] && [ -f "$1/src/std/SENTINEL" ] || fail "$1 lost files"
}

# refuse makes a tree at $2, runs make install-std in it with the
# settings that follow, and fails the check unless it fails with the
# line $1 and the tree loses nothing.
refuse() {
	want=$1
	t=$2
	shift 2
	must_fail "$want" "$@" make -s -C "$t" install-std
	intact "$t"
}

# The failures that must come before anything is built or written.
must_fail "install-std: neither TACK_HOME nor HOME is set" make -s -C "$tree" install-std
must_fail "install: neither TACK_HOME nor HOME is set" \
	make -s -C "$tree" install TACK="$tack" PREFIX="$tmp/prefix"
must_fail "install: neither BINDIR, PREFIX nor HOME is set" \
	TACK_HOME="$tmp/tack-home" make -s -C "$tree" install TACK="$tack"
absent "$tmp/prefix" "$tmp/tack-home" "$tree/.tack" "$tree/bin"
if clean HOME="$home" make -s -C "$tree" install TACK="$tmp/no-such-tack" >"$tmp/out" 2>&1; then
	fail "make install with a TACK that is not there succeeded"
fi
absent "$home/.tack" "$home/.local"
rm -rf "$tree/bin"

# The destinations that would delete the tree, or part of it.
g="$tmp/guard"
mkdir "$g"
T="$g/A/tree"
mini "$T"
refuse "install-std: TACK_HOME must be an absolute path, not ." "$T" HOME="$g/A/home" TACK_HOME=.
must_fail "install-std: TACK_HOME must be an absolute path, not ." \
	HOME="$g/A/home" make -s -C "$T" install-std TACK_HOME=.
intact "$T"
mkdir "$g/A/cwd"
(cd "$g/A/cwd" && must_fail "install-std: TACK_HOME must be an absolute path, not rel" \
	HOME="$g/A/home" TACK_HOME=rel make -s -C "$T" install-std)
absent "$T/rel" "$g/A/cwd/rel"
refuse "install-std: HOME must be an absolute path, not rel" "$T" HOME=rel
inside="install-std: $T/src/std is inside this source tree, $T"
refuse "$inside" "$T" HOME="$g/A/home" TACK_HOME="$T"
refuse "$inside" "$T" HOME="$g/A/home" TACK_HOME="$T/"
ln -s "$T" "$g/A/link"
refuse "$inside" "$T" HOME="$g/A/home" TACK_HOME="$g/A/link"
mkdir -p "$g/D/home"
ln -s "$T" "$g/D/home/.tack"
refuse "$inside" "$T" HOME="$g/D/home"
mkdir -p "$g/E/home/.tack"
ln -s "$T/src" "$g/E/home/.tack/src"
refuse "$inside" "$T" HOME="$g/E/home"
mkdir -p "$g/L/home/.tack/src"
ln -s "$T/src/std" "$g/L/home/.tack/src/std"
refuse "$inside" "$T" HOME="$g/L/home"
refuse "install-std: $T/src/std/x/src/std is inside this source tree, $T" "$T" \
	HOME="$g/A/home" TACK_HOME="$T/src/std/x"
absent "$T/src/std/x"
refuse "install-std: $T/src/src/std is inside this source tree, $T" "$T" \
	HOME="$g/A/home" TACK_HOME="$T/src/std/.."
absent "$T/src/src"
X="$g/G/x"
mini "$X/src/std/repo"
refuse "install-std: $X/src/std holds this source tree, $X/src/std/repo" "$X/src/std/repo" \
	HOME="$g/G/home" TACK_HOME="$X"
must_fail "install: $T/src/std is inside this source tree, $T" \
	HOME="$g/A/home" TACK_HOME="$T" make -s -C "$T" install TACK="$tack"
intact "$T"
absent "$T/bin" "$g/A/home"
must_fail "install: $tree/src/std is inside this source tree, $tree" \
	HOME="$home" TACK_HOME="$tree" make -s -C "$tree" install TACK="$tack"
absent "$tree/bin" "$home/.local"

# The destinations near those that must still work.
mkdir -p "$g/F/home/.tack/src" "$g/F/target"
echo keep >"$g/F/target/KEEP"
ln -s "$g/F/target" "$g/F/home/.tack/src/std"
clean HOME="$g/F/home" make -s -C "$T" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std over a link to another directory: $(cat "$tmp/out")"
[ -f "$g/F/target/KEEP" ] || fail "make install-std removed what its link pointed to"
[ "$(stat -c %F "$g/F/home/.tack/src/std")" = directory ] || fail "~/.tack/src/std is still a link"
check_std "$g/F/home/.tack" "$T"
mkdir -p "$g/J/g*" "$g/J/gdecoy/src/std"
echo keep >"$g/J/gdecoy/src/std/DECOY"
clean HOME="$g/J/home" TACK_HOME="$g/J/g*" make -s -C "$T" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std to a TACK_HOME holding *: $(cat "$tmp/out")"
[ -f "$g/J/gdecoy/src/std/DECOY" ] || fail "make install-std to g* removed gdecoy's std"
check_std "$g/J/g*" "$T"
mini "$g/P/ab"
clean HOME="$g/P/home" TACK_HOME="$g/P/a" make -s -C "$g/P/ab" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std to /a from /ab: $(cat "$tmp/out")"
check_std "$g/P/a" "$g/P/ab"
clean HOME="$g/Q/new home" make -s -C "$T" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std to a new home: $(cat "$tmp/out")"
check_std "$g/Q/new home/.tack" "$T"
intact "$T"

# A failed copy keeps the std installed before.
if [ $root -eq 1 ]; then
	echo "test-install: skipped the unreadable std check: running as root"
else
	echo mark >"$g/Q/new home/.tack/src/std/MARK"
	chmod 000 "$T/src/std/hex"
	if clean HOME="$g/Q/new home" make -s -C "$T" install-std >"$tmp/out" 2>&1; then
		fail "make install-std with an unreadable src/std/hex succeeded"
	fi
	chmod 755 "$T/src/std/hex"
	[ -f "$g/Q/new home/.tack/src/std/MARK" ] || fail "a failed install-std replaced the std there"
	rm "$g/Q/new home/.tack/src/std/MARK"
	check_std "$g/Q/new home/.tack" "$T"
fi

# A stand-in for mv, first on PATH, that signals the script running it,
# its parent, with $KILLSIG when its arguments match $KILLPAT, and
# fails, and otherwise runs mv.
mkdir "$tmp/shim"
realmv=$(command -v mv)
cat >"$tmp/shim/mv" <<EOF
#!/bin/sh
case "\$*" in
\$KILLPAT) kill -"\$KILLSIG" "\$PPID"; exit 1 ;;
esac
exec "$realmv" "\$@"
EOF
chmod +x "$tmp/shim/mv"

# shimmed runs its arguments as clean does, with the stand-in for mv
# first on PATH.
shimmed() {
	env -i PATH="$tmp/shim:$PATH" TMPDIR="$runtmp" "$@"
}

# install-std ended by SIGTERM between its two renames puts the old std
# back; ended by SIGKILL there it leaves nothing at the destination, and
# the next run puts the old std back from what it left, removes the
# rest, and touches nothing else.
qstd="$g/Q/new home/.tack/src"
echo mark >"$qstd/std/MARK"
if shimmed HOME="$g/Q/new home" KILLSIG=TERM KILLPAT='-T */.std.install.* */std' \
	make -s -C "$T" install-std >"$tmp/out" 2>&1; then
	fail "make install-std ended by SIGTERM succeeded"
fi
[ -f "$qstd/std/MARK" ] || fail "make install-std ended by SIGTERM did not put the old std back: $(ls -A "$qstd")"
no_leftovers "$qstd"
if shimmed HOME="$g/Q/new home" KILLSIG=KILL KILLPAT='-T */.std.install.* */std' \
	make -s -C "$T" install-std >"$tmp/out" 2>&1; then
	fail "make install-std ended by SIGKILL succeeded"
fi
absent "$qstd/std"
for p in "$qstd"/.std.old.* "$qstd"/.std.install.*; do
	[ -e "$p" ] || fail "make install-std ended by SIGKILL left no $p"
done
for decoy in .std.old.notapid .std.old.12x x.std.old.1 .std.install.$$ .tack.install.1 .std.old; do
	mkdir "$qstd/$decoy"
done
clean HOME="$g/Q/new home" make -s -C "$T" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std after one ended by SIGKILL: $(cat "$tmp/out")"
case $(cat "$tmp/out") in
*"install-std: putting back the std an earlier run left at $qstd/.std.old."*) ;;
*) fail "make install-std after one ended by SIGKILL did not put the old std back: $(cat "$tmp/out")" ;;
esac
for decoy in .std.old.notapid .std.old.12x x.std.old.1 .std.install.$$ .tack.install.1 .std.old; do
	[ -d "$qstd/$decoy" ] || fail "make install-std removed $qstd/$decoy, which no run of it made"
	rmdir "$qstd/$decoy"
done
absent "$qstd/std/MARK"
check_std "$g/Q/new home/.tack" "$T"

# A first install, with HOME alone.
clean HOME="$home" make -s -C "$tree" install TACK="$tack" >"$tmp/out" 2>&1 ||
	fail "make install: $(cat "$tmp/out")"
bin="$home/.local/bin/tack"
[ -x "$bin" ] || fail "make install did not install $bin"
cmp "$tree/bin/install/stage3" "$bin" || fail "$bin is not the stage the bootstrap built"
cmp "$tree/bin/install/stage2" "$tree/bin/install/stage3" || fail "stage 2 and stage 3 differ"
check_std "$home/.tack"
no_leftovers "$home/.local/bin"

# The installed compiler, on std alone.
mkdir "$tmp/hello"
echo "module hello" >"$tmp/hello/tack.mod"
cat >"$tmp/hello/main.tck" <<'EOF'
package main

import fmt from "std/fmt"

func main() {
    fmt.printf("hello, %s\n", "installed tack")
}
EOF
(cd "$tmp/hello" && clean HOME="$home" "$bin" --out "$tmp/hello/hello" .) >"$tmp/out" 2>&1 ||
	fail "the installed tack cannot build a program: $(cat "$tmp/out")"
got=$(clean "$tmp/hello/hello") || fail "the program the installed tack built failed"
[ "$got" = "hello, installed tack" ] || fail "the program the installed tack built printed: $got"
(cd "$home/.tack/src/std" && clean HOME="$home" "$bin" test ./hex) >"$tmp/out" 2>&1 ||
	fail "tack test ./hex in the installed std: $(cat "$tmp/out")"

# Reinstalling std: nothing lingers.
stale="$home/.tack/src/std/builtin/stale.tck"
printf 'package builtin\n\nvar x = 1\n' >"$stale"
cp "$tree/src/std/hex/hex_test.tck" "$tmp/hex_test.tck"
rm "$tree/src/std/hex/hex_test.tck"
clean HOME="$home" make -s -C "$tree" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std over an install: $(cat "$tmp/out")"
absent "$stale" "$home/.tack/src/std/hex/hex_test.tck"
check_std "$home/.tack"
mv "$tmp/hex_test.tck" "$tree/src/std/hex/hex_test.tck"
clean TACK_HOME= HOME="$home" make -s -C "$tree" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std with TACK_HOME empty: $(cat "$tmp/out")"
[ "$(cat "$tmp/out")" = "installing std to $home/.tack/src/std" ] ||
	fail "make install-std with TACK_HOME empty said: $(cat "$tmp/out")"
check_std "$home/.tack"
clean TACK_HOME="$tmp/tack home" HOME="$tmp/unused" make -s -C "$tree" install-std >"$tmp/out" 2>&1 ||
	fail "make install-std with TACK_HOME: $(cat "$tmp/out")"
check_std "$tmp/tack home"
absent "$tmp/unused"

# A reinstall over the compiler building it.
cp "$bin" "$tmp/first"
inode=$(stat -c %i "$bin")
clean TACK_HOME="$tmp/tack home 2" HOME="$home" make -s -C "$tree" install TACK="$bin" \
	BINDIR="$home/.local/bin" PREFIX="$tmp/unused" >"$tmp/out" 2>&1 ||
	fail "make install over the compiler building it: $(cat "$tmp/out")"
cmp "$tmp/first" "$bin" || fail "the install over the compiler building it is not the first install"
[ "$(stat -c %i "$bin")" != "$inode" ] || fail "the reinstall did not replace $bin"
[ "$(ls -A "$home/.local/bin")" = tack ] || fail "$home/.local/bin holds more than tack: $(ls -A "$home/.local/bin")"
check_std "$tmp/tack home 2"
absent "$tmp/unused"

# Stand-ins for a compiler: fake writes a copy of itself where --out
# says, the same bytes each time; fake2 is another such; drift adds a
# line to each copy, so each stage it builds differs from the last.
cat >"$tmp/fake" <<'EOF'
#!/bin/sh
out=
while [ $# -gt 0 ]; do
	case $1 in --out) out=$2; shift ;; esac
	shift
done
cp "$0" "$out"
# fake
EOF
sed 's/^# fake$/# fake2/' "$tmp/fake" >"$tmp/fake2"
sed 's/^cp "$0" "$out"$/cp "$0" "$out" \&\& echo "# drift" >>"$out"/' "$tmp/fake" >"$tmp/drift"
chmod +x "$tmp/fake" "$tmp/fake2" "$tmp/drift"
cmp -s "$tmp/fake" "$tmp/drift" && fail "drift is fake"

# An install whose stage 2 and stage 3 differ keeps what was there.
echo mark >"$home/.tack/src/std/MARK"
if clean HOME="$home" make -s -C "$tree" install TACK="$tmp/drift" >"$tmp/out" 2>&1; then
	fail "make install with stage 2 and stage 3 different succeeded"
fi
cmp "$tmp/first" "$bin" || fail "a failed install replaced $bin"
[ -f "$home/.tack/src/std/MARK" ] || fail "a failed install replaced the std there"
rm "$home/.tack/src/std/MARK"
check_std "$home/.tack"
no_leftovers "$home/.local/bin"

# A BINDIR that cannot hold the compiler fails before anything is built.
rm -rf "$tree/bin"
if [ $root -eq 1 ]; then
	echo "test-install: skipped the unwritable BINDIR checks: running as root"
else
	mkdir "$tmp/ro"
	chmod a-w "$tmp/ro"
	must_fail "install: cannot write $tmp/ro" \
		HOME="$home" make -s -C "$tree" install TACK="$tmp/fake" BINDIR="$tmp/ro"
	must_fail "install: cannot write $tmp/ro/bin" \
		HOME="$home" make -s -C "$tree" install TACK="$tmp/fake" BINDIR="$tmp/ro/bin"
	absent "$tree/bin" "$tmp/ro/tack" "$tmp/ro/bin"
	chmod u+w "$tmp/ro"
fi
mkdir -p "$tmp/dirbin/tack"
echo keep >"$tmp/dirbin/tack/KEEP"
must_fail "install: $tmp/dirbin/tack is a directory" \
	HOME="$home" make -s -C "$tree" install TACK="$tmp/fake" BINDIR="$tmp/dirbin"
[ "$(ls -A "$tmp/dirbin/tack")" = KEEP ] || fail "$tmp/dirbin/tack changed"
absent "$tree/bin"
cmp "$tmp/first" "$bin" || fail "a refused install replaced $bin"
check_std "$home/.tack"

# PREFIX and TACK_HOME, and a reinstall of a compiler that differs.
clean TACK_HOME="$tmp/fake home" HOME="$tmp/unused" \
	make -s -C "$tree" install TACK="$tmp/fake" PREFIX="$tmp/pre fix" >"$tmp/out" 2>&1 ||
	fail "make install with PREFIX: $(cat "$tmp/out")"
cmp "$tmp/fake" "$tmp/pre fix/bin/tack" || fail "the install with PREFIX did not install stage 3"
check_std "$tmp/fake home"
absent "$tmp/unused"
clean TACK_HOME="$tmp/fake home" HOME="$tmp/unused" \
	make -s -C "$tree" install TACK="$tmp/fake2" PREFIX="$tmp/pre fix" >"$tmp/out" 2>&1 ||
	fail "make install over another compiler: $(cat "$tmp/out")"
cmp "$tmp/fake2" "$tmp/pre fix/bin/tack" || fail "the reinstall did not replace $tmp/pre fix/bin/tack"
no_leftovers "$tmp/pre fix/bin"

# A BINDIR with a backtick, or quotes, in it runs nothing.
odd="$tmp/b\`touch $tmp/pwned\`"
clean HOME="$home" TACK_HOME="$tmp/fake home" \
	make -s -C "$tree" install TACK="$tmp/fake" BINDIR="$odd" >"$tmp/out" 2>&1 ||
	fail "make install with a backtick in BINDIR: $(cat "$tmp/out")"
cmp "$tmp/fake" "$odd/tack" || fail "make install with a backtick in BINDIR did not install there"
absent "$tmp/pwned"
odd="$tmp/q\"x'y"
clean HOME="$home" TACK_HOME="$tmp/fake home" \
	make -s -C "$tree" install TACK="$tmp/fake" BINDIR="$odd" >"$tmp/out" 2>&1 ||
	fail "make install with quotes in BINDIR: $(cat "$tmp/out")"
cmp "$tmp/fake" "$odd/tack" || fail "make install with quotes in BINDIR did not install there"
cmp "$tmp/first" "$bin" || fail "an install elsewhere replaced $bin"

# install ended by SIGTERM with the new std in place and the compiler
# not renamed yet keeps both as they were; ended by SIGKILL there, the
# next install removes what it left and installs both.
fstd="$tmp/fake home/src"
echo mark >"$fstd/std/MARK"
if shimmed TACK_HOME="$tmp/fake home" HOME="$tmp/unused" KILLSIG=TERM KILLPAT='-fT *' \
	make -s -C "$tree" install TACK="$tmp/fake" PREFIX="$tmp/pre fix" >"$tmp/out" 2>&1; then
	fail "make install ended by SIGTERM succeeded"
fi
cmp "$tmp/fake2" "$tmp/pre fix/bin/tack" || fail "make install ended by SIGTERM replaced the compiler"
[ -f "$fstd/std/MARK" ] || fail "make install ended by SIGTERM replaced std: $(ls -A "$fstd")"
no_leftovers "$fstd"
no_leftovers "$tmp/pre fix/bin"
if shimmed TACK_HOME="$tmp/fake home" HOME="$tmp/unused" KILLSIG=KILL KILLPAT='-fT *' \
	make -s -C "$tree" install TACK="$tmp/fake" PREFIX="$tmp/pre fix" >"$tmp/out" 2>&1; then
	fail "make install ended by SIGKILL succeeded"
fi
for p in "$fstd"/.std.old.* "$tmp/pre fix/bin"/.tack.install.*; do
	[ -e "$p" ] || fail "make install ended by SIGKILL left no $p"
done
clean TACK_HOME="$tmp/fake home" HOME="$tmp/unused" \
	make -s -C "$tree" install TACK="$tmp/fake" PREFIX="$tmp/pre fix" >"$tmp/out" 2>&1 ||
	fail "make install after one ended by SIGKILL: $(cat "$tmp/out")"
cmp "$tmp/fake" "$tmp/pre fix/bin/tack" || fail "make install after one ended by SIGKILL did not install the compiler"
absent "$fstd/std/MARK"
check_std "$tmp/fake home"
no_leftovers "$tmp/pre fix/bin"
absent "$tmp/unused"

echo "test-install: ok"

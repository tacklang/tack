#!/bin/sh
# test.sh checks the release scripts of tools/release and
# tools/get-tack.sh, with TACK, the compiler named by its one argument, a
# path or a name looked up on PATH, which must be built from this tree.
# make test-release runs it from the root of the port's tree, the
# directory it must be run from.
#
# Everything it writes is below a temporary directory of its own, below
# $TMPDIR or /tmp, removed however it ends: the releases it packages,
# the minisign keys it makes for the test alone, a password-protected
# one and one with no password, both THROWAWAY TEST KEYS that sign
# nothing but these test releases, and the homes it installs into. A
# release is served from a file:// directory laid out as GitHub's
# releases are, latest/download/ and download/<tag>/, through
# get-tack.sh's TACK_RELEASES. Every run of get-tack.sh has an
# environment of its own, through env -i, with HOME a directory below
# the temporary one, and a PATH of links to the tools it needs and no
# others, so that whether minisign and gh are found is the check's to
# say; gh is always a stand-in, a script that records how it was run and
# passes or fails as told, so no check ever reaches GitHub.
#
# It checks, in order:
#
#   - that TACK --version prints tack and the version of this tree's
#     driver/version.tck;
#   - stamp.sh: that it refuses every VERSION that is no
#     vMAJOR.MINOR.PATCH, a prerelease, a missing v, a leading zero, a
#     number of ten digits, white space, a quote and a second line among
#     them, writing nothing, and that it stamps a copy, version.tck's
#     line and every stdout line of corpus/runs/port-only.txtar and
#     nothing else, and refuses to stamp it twice;
#   - previous.sh: the highest release below a tag by SemVer's order,
#     numbers compared as numbers, nothing for no release, and a failure
#     for a tag not above every release, itself among them, and for a
#     line that is no tag, an empty one included;
#   - gate.sh, in a repository of its own with a main and a side branch:
#     that a tag passes only on a commit of main, with a ci run on that
#     commit, started by a push to main, that succeeded, and the release
#     key set; that a run started by hand, one on another branch or
#     another commit, one that failed or has not ended, a commit off
#     main, an unset key, a tag not above every release, and a malformed
#     tag or commit each fail;
#   - check-artifact.sh: that it takes the build's tarball and refuses
#     other bytes, another name, a file beside it and a malformed digest;
#   - glibc-check.sh: that a program TACK builds, linked as the compiler
#     is, read with the readelf READELF names, or llvm-readelf, or readelf,
#     needs no glibc above tools/release/glibc-floor
#     and no library but libc.so.6; and, through a stand-in readelf, that
#     a symbol above the floor, another library, no glibc version and a
#     malformed floor each fail;
#   - crosscheck.sh, with stand-ins for Go's stage 1: the same stage 2
#     passes; another fails; a stage 1 that writes nothing fails even
#     with OUT filled beforehand with stage 2's bytes, which it removes;
#     one that fails, fails; and an OUT that is the tree's own stage 2,
#     as named, through .., through a symbolic link or relative to the
#     tree, is refused with stage 2 left as it was;
#   - package.sh: that two tarballs made from the same inputs in other
#     directories, under umasks 022 and 077, are the same bytes, that the
#     tarball holds the layout package.sh gives, every entry owned by 0,
#     dated SOURCE_DATE_EPOCH and of its mode, bin/tack TACK's bytes and
#     src/std this tree's, and that it refuses a missing
#     SOURCE_DATE_EPOCH, a bad VERSION and a tree with no LICENSE;
#   - sums.sh and sign.sh: SHA256SUMS as sha256sum -c reads it; with the
#     password-protected key, signatures that verify with its public key
#     and carry the trusted comment "tack <version> <file>", the key
#     written mode 600 and removed after, and neither the password nor
#     the key in the output; and a failure for a wrong password, an empty
#     one, and a secret key that is not the public key's;
#   - get-tack.sh, installing: the latest release, its compiler and std
#     where make install puts them, which builds and runs a program that
#     imports std/fmt; a release by its version into PREFIX and
#     TACK_HOME; --download; with minisign or gh missing, or the key
#     unset, a warning naming what goes unchecked, and with --strict a
#     failure;
#   - get-tack.sh, refusing, with nothing installed each time: a
#     tampered tarball, a SHA256SUMS with a wrong digest, signed or not,
#     a signature by another key, one whose trusted comment names
#     another release, a SHA256SUMS of another release or of two
#     tarballs, a missing signature, a failed attestation, a tarball
#     holding a symbolic link, a hard link, a device, a name with .., an
#     absolute name or a second top directory, a bad --version, a
#     TACK_HOME inside a source tree or not absolute, and a --download
#     directory that is there already; and that gh is run with the
#     release workflow as the signer, the tag as the source ref and
#     self-hosted runners denied;
#   - that the release public key is the same in tools/release/minisign.pub,
#     get-tack.sh and the README; that every action each workflow uses is
#     pinned to a full commit SHA with its version, vX.Y.Z, beside it, and
#     that no run script expands ${{ }}, so values reach them through env
#     alone; and that the release workflow gates its build on gate.sh,
#     builds in fedora:45 pinned by digest, as ci, an unprivileged
#     account, and gives the release environment, and with it the
#     secrets, to the publish job alone;
#   - make install-from-release, from the file:// release, in a copy of
#     this tree, which installs the compiler it builds and std.
#
# It stops at the first check that fails, saying which, with exit
# status 1. It needs make, git, minisign, curl, GNU tar, xz, clang,
# llvm-readelf or readelf, cmp, diff and coreutils, and no Go.
set -eu

if [ $# -ne 1 ]; then
	echo "usage: tools/release/test.sh TACK" >&2
	exit 2
fi
tack=$(command -v "$1") || {
	echo "test-release: FAIL: no compiler $1" >&2
	exit 1
}
tack=$(realpath -e "$tack")
root=$(realpath -e .)
[ -f "$root/tools/release/stamp.sh" ] || {
	echo "test-release: FAIL: run it from the root of the port's tree" >&2
	exit 1
}
for t in make minisign curl tar xz clang cmp diff sha256sum; do
	command -v "$t" >/dev/null || {
		echo "test-release: FAIL: $t is not on PATH" >&2
		exit 1
	}
done

tmp=$(mktemp -d "${TMPDIR:-/tmp}/test-release.XXXXXX")
trap 'chmod -R u+rwx "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT
tmp=$(realpath -e "$tmp")
mkdir "$tmp/tmp"
epoch=1700000000
step=start

# fail reports the check that failed, its arguments, and stops.
fail() {
	echo "test-release: FAIL: $step: $*" >&2
	exit 1
}

# check names the check that runs next, for fail.
check() {
	step=$*
	echo "test-release: $step"
}

# clean runs its arguments, VAR=value settings and then a command, as
# env runs them, in an environment of TMPDIR and those settings alone,
# PATH among them when they give it, and this process's PATH otherwise.
clean() {
	env -i PATH="$PATH" TMPDIR="$tmp/tmp" "$@"
}

# expect_fail runs its arguments but the first as clean runs them, with
# their output in $tmp/out, and fails the check unless they fail and the
# output holds the first argument.
expect_fail() {
	want=$1
	shift
	if clean "$@" >"$tmp/out" 2>&1; then
		fail "$* succeeded; it must fail with: $want"
	fi
	grep -qF -- "$want" "$tmp/out" || fail "$* did not say: $want; it said: $(cat "$tmp/out")"
}

# expect_ok runs its arguments as clean runs them, with their output in
# $tmp/out, and fails the check, showing it, unless they succeed.
expect_ok() {
	clean "$@" >"$tmp/out" 2>&1 || fail "$* failed: $(cat "$tmp/out")"
}

# --- the version ---

check "tack --version prints the tree's version"
line=$(grep -x 'var tack_version = ".*"' "$root/driver/version.tck") || fail "driver/version.tck has no tack_version line"
tree_version=${line#var tack_version = \"}
tree_version=${tree_version%\"}
got=$(clean "$tack" --version) || fail "$tack --version failed"
[ "$got" = "tack $tree_version" ] || fail "$tack --version says '$got', not 'tack $tree_version'"

# --- stamp.sh ---

check "stamp.sh refuses what is no release tag"
mkdir -p "$tmp/stamp/driver" "$tmp/stamp/corpus/runs"
cp "$root/driver/version.tck" "$tmp/stamp/driver/"
cp "$root/corpus/runs/port-only.txtar" "$tmp/stamp/corpus/runs/"
if [ "$tree_version" = devel ]; then
	cp -R "$tmp/stamp" "$tmp/stamp.orig"
	for v in "" 1.2.3 v1.2 v1.2.3.4 v01.2.3 v1.02.3 v1.2.03 v1.2.3-rc.1 v1.2.3+build " v1.2.3" "v1.2.3 " V1.2.3 \
		v1234567890.0.0 'v1.2.3"' 'v1.2.3\n' "v1.2.3
v1.2.4" "v1.2.3/x"; do
		if clean VERSION="$v" sh "$root/tools/release/stamp.sh" "$tmp/stamp" >"$tmp/out" 2>&1; then
			fail "stamp.sh took VERSION '$v'"
		fi
		diff -r "$tmp/stamp.orig" "$tmp/stamp" >/dev/null || fail "stamp.sh wrote the tree for VERSION '$v'"
	done
	expect_fail "VERSION is not set" sh "$root/tools/release/stamp.sh" "$tmp/stamp"

	check "stamp.sh stamps version.tck and port-only.txtar alone"
	n=$(grep -cxF 'stdout "tack devel\n"' "$tmp/stamp/corpus/runs/port-only.txtar")
	[ "$n" -ge 1 ] || fail "port-only.txtar has no stdout \"tack devel\\n\" line"
	expect_ok VERSION=v12.0.345 sh "$root/tools/release/stamp.sh" "$tmp/stamp"
	grep -qx 'var tack_version = "v12.0.345"' "$tmp/stamp/driver/version.tck" || fail "version.tck is not stamped"
	m=$(grep -cxF 'stdout "tack v12.0.345\n"' "$tmp/stamp/corpus/runs/port-only.txtar") || m=0
	[ "$m" -eq "$n" ] || fail "port-only.txtar has $m stamped lines, not $n"
	sed 's/v12\.0\.345/devel/' "$tmp/stamp/driver/version.tck" | cmp -s - "$tmp/stamp.orig/driver/version.tck" ||
		fail "stamp.sh changed more of version.tck than its line"
	sed 's/tack v12\.0\.345\\n/tack devel\\n/' "$tmp/stamp/corpus/runs/port-only.txtar" |
		cmp -s - "$tmp/stamp.orig/corpus/runs/port-only.txtar" || fail "stamp.sh changed more of port-only.txtar than its stdout lines"
	expect_fail "does not hold the line" VERSION=v1.0.0 sh "$root/tools/release/stamp.sh" "$tmp/stamp"
else
	echo "test-release: this tree is stamped $tree_version; stamp.sh's checks need a tree that says devel, and are skipped"
fi

# --- previous.sh ---

# previous fails the check unless previous.sh, given tag $1 and the
# lines of $2, writes $3.
previous() {
	got=$(printf '%s' "$2" | clean sh "$root/tools/release/previous.sh" "$1") || fail "previous.sh $1 failed on '$2'"
	[ "$got" = "$3" ] || fail "previous.sh $1 on '$2' gave '$got', not '$3'"
}

check "previous.sh names the release below a tag, and refuses one not above every release"
previous v0.1.0 "" ""
previous v0.10.0 "v0.1.0
v0.2.0
v0.9.3
" v0.9.3
previous v1.0.0 "v0.10.0
v0.9.3
v0.2.0" v0.10.0
previous v2.0.0 "v1.9.9
v1.10.0
v1.10.1" v1.10.1
previous v0.0.1 "v0.0.0" v0.0.0
expect_fail "v0.1.0 is not above the release v0.1.0" sh -c 'printf "v0.1.0\n" | sh "$1" v0.1.0' sh "$root/tools/release/previous.sh"
expect_fail "v1.0.0 is not above the release v2.0.0" sh -c 'printf "v2.0.0\nv0.9.0\n" | sh "$1" v1.0.0' sh "$root/tools/release/previous.sh"
expect_fail "v0.9.0 is not above the release v0.10.0" sh -c 'printf "v0.10.0\n" | sh "$1" v0.9.0' sh "$root/tools/release/previous.sh"
for bad in "latest" "v1.0" "" "v1.0.0-rc.1" "v01.0.0"; do
	if printf '%s\n' "$bad" | clean sh "$root/tools/release/previous.sh" v9.0.0 >"$tmp/out" 2>&1; then
		fail "previous.sh took the listed tag '$bad'"
	fi
done
expect_fail "TAG must be" sh "$root/tools/release/previous.sh" v1.0

# --- gate.sh ---

check "gate.sh passes a tag only on main, after a ci push run passed, with the key set"
# A repository of three commits: m1 and m2 on main, s1 on a side branch.
g=$tmp/gaterepo
mkdir -p "$g"
(
	cd "$g"
	git init -q -b main
	git -c user.name=t -c user.email=t@t commit -q --allow-empty -m m1
	git -c user.name=t -c user.email=t@t commit -q --allow-empty -m m2
	git checkout -q -b side
	git -c user.name=t -c user.email=t@t commit -q --allow-empty -m s1
	git checkout -q main
) || fail "making the gate's repository"
m1=$(git -C "$g" rev-parse main~1)
m2=$(git -C "$g" rev-parse main)
s1=$(git -C "$g" rev-parse side)
printf 'untrusted comment: test\nRWTESTKEY\n' >"$tmp/key.pub"
printf 'untrusted comment: placeholder\nUNSET\n' >"$tmp/unset.pub"
printf 'v0.1.0\n' >"$tmp/rel"
: >"$tmp/norel"

# runs writes the ci runs of file $tmp/runs from its arguments, one run
# each, its four fields separated by spaces.
runs() {
	: >"$tmp/runs"
	for r in "$@"; do
		printf '%s\n' "$r" | tr ' ' '\t' >>"$tmp/runs"
	done
}

# gate runs gate.sh in the gate's repository for tag $1, commit $2, the
# releases of file $3 and the key file $4, and checks that it writes $5
# when $6 is ok, and fails saying $5 otherwise.
gate() {
	if (cd "$g" && clean sh "$root/tools/release/gate.sh" "$1" "$2" main "$tmp/runs" "$3" "$4") >"$tmp/out" 2>"$tmp/err"; then
		[ "$6" = ok ] || fail "gate.sh passed $1 at $2: $(cat "$tmp/err")"
		[ "$(cat "$tmp/out")" = "$5" ] || fail "gate.sh wrote '$(cat "$tmp/out")', not '$5'"
	else
		[ "$6" = fail ] || fail "gate.sh refused $1 at $2: $(cat "$tmp/err")"
		grep -qF -- "$5" "$tmp/err" || fail "gate.sh did not say: $5; it said: $(cat "$tmp/err")"
	fi
}

runs "$m2 push main success"
gate v0.2.0 "$m2" "$tmp/rel" "$tmp/key.pub" v0.1.0 ok
gate v0.1.0 "$m2" "$tmp/norel" "$tmp/key.pub" "" ok
runs "$m1 push main success"
gate v0.2.0 "$m1" "$tmp/rel" "$tmp/key.pub" v0.1.0 ok
runs "$m2 push main failure" "$m2 workflow_dispatch main success" "$m2 push side success" "$m1 push main success" "$m2 push main null"
gate v0.2.0 "$m2" "$tmp/rel" "$tmp/key.pub" "no run of ci, started by a push to main, passed on $m2" fail
runs "$s1 workflow_dispatch side success" "$s1 push side success" "$s1 push main success"
gate v0.2.0 "$s1" "$tmp/rel" "$tmp/key.pub" "$s1 is not on main" fail
runs
gate v0.2.0 "$m2" "$tmp/rel" "$tmp/key.pub" "its runs: none" fail
runs "$m2 push main success"
gate v0.2.0 "$m2" "$tmp/rel" "$tmp/unset.pub" "the release public key in $tmp/unset.pub is not set yet" fail
gate v0.1.0 "$m2" "$tmp/rel" "$tmp/key.pub" "v0.1.0 is not above the release v0.1.0" fail
gate v0.2 "$m2" "$tmp/rel" "$tmp/key.pub" "is not vMAJOR.MINOR.PATCH" fail
gate v0.2.0 "${m2%?}" "$tmp/rel" "$tmp/key.pub" "is not a full commit SHA" fail
if (cd "$g" && clean sh "$root/tools/release/gate.sh" v0.2.0 "$m2" nosuchbranch "$tmp/runs" "$tmp/rel" "$tmp/key.pub") >/dev/null 2>"$tmp/err"; then
	fail "gate.sh passed with no main branch"
fi
grep -qF "there is no nosuchbranch" "$tmp/err" || fail "gate.sh did not name the missing branch: $(cat "$tmp/err")"

# --- check-artifact.sh ---

check "check-artifact.sh takes the build's tarball alone"
mkdir -p "$tmp/art"
printf 'tarball bytes\n' >"$tmp/art/tack-v1.2.3-linux-x86_64.tar.xz"
asha=$(sha256sum "$tmp/art/tack-v1.2.3-linux-x86_64.tar.xz" | cut -d' ' -f1)
expect_ok sh "$root/tools/release/check-artifact.sh" "$tmp/art" tack-v1.2.3-linux-x86_64.tar.xz "$asha"
expect_fail "is not the tarball the build job made" sh "$root/tools/release/check-artifact.sh" "$tmp/art" tack-v1.2.3-linux-x86_64.tar.xz \
	"$(printf '%s' "$asha" | tr 0-9a-f 1-9a-f0)"
expect_fail "not tack-v1.2.4-linux-x86_64.tar.xz alone" sh "$root/tools/release/check-artifact.sh" "$tmp/art" tack-v1.2.4-linux-x86_64.tar.xz "$asha"
expect_fail "is not a SHA-256" sh "$root/tools/release/check-artifact.sh" "$tmp/art" tack-v1.2.3-linux-x86_64.tar.xz "${asha%?}"
: >"$tmp/art/extra"
expect_fail "not tack-v1.2.3-linux-x86_64.tar.xz alone" sh "$root/tools/release/check-artifact.sh" "$tmp/art" tack-v1.2.3-linux-x86_64.tar.xz "$asha"

# --- glibc-check.sh ---

if [ -n "${READELF:-}" ]; then
	readelf=$READELF
elif command -v llvm-readelf >/dev/null; then
	readelf=llvm-readelf
else
	readelf=readelf
fi
check "glibc-check.sh holds a program TACK builds to the glibc floor with $readelf"
command -v "$readelf" >/dev/null || fail "$readelf is not on PATH; set READELF to a readelf"
# TACK may be a wrapper script, as porttest's is, so the check reads a
# program TACK builds, linked as the compiler is, and stage 3 itself is
# checked by build.sh.
mkdir "$tmp/elf"
printf 'package main\n\nfunc main() {\n    print("ok\\n")\n}\n' >"$tmp/elf/main.tck"
(cd "$tmp/elf" && clean HOME="$tmp/elf" TACK_HOME="$root" TACK_CACHE="$tmp/elf/cache" "$tack" --out prog main.tck) >"$tmp/out" 2>&1 ||
	fail "TACK cannot build a program to check: $(cat "$tmp/out")"
clean READELF="$readelf" sh "$root/tools/release/glibc-check.sh" "$tmp/elf/prog" "$root/tools/release/glibc-floor" >"$tmp/out" 2>&1 ||
	fail "a program TACK builds does not pass the glibc floor: $(cat "$tmp/out")"
# With READELF unset it takes llvm-readelf, else readelf, from PATH; a
# PATH holding neither fails, saying so.
if command -v llvm-readelf >/dev/null; then picked=llvm-readelf; else picked=readelf; fi
expect_ok sh "$root/tools/release/glibc-check.sh" "$tmp/elf/prog" "$root/tools/release/glibc-floor"
grep -qF "with $picked" "$tmp/out" || fail "glibc-check.sh did not pick $picked: $(cat "$tmp/out")"
mkdir "$tmp/noelf"
for t in sh cat grep sed sort tail tr; do
	ln -s "$(realpath -e "$(command -v "$t")")" "$tmp/noelf/$t"
done
expect_fail "neither llvm-readelf nor readelf is on PATH" PATH="$tmp/noelf" \
	sh "$root/tools/release/glibc-check.sh" "$tmp/elf/prog" "$root/tools/release/glibc-floor"
grep -qx '[0-9][0-9]*\.[0-9][0-9]*' "$root/tools/release/glibc-floor" || fail "tools/release/glibc-floor is not MAJOR.MINOR"
# A stand-in readelf writes the symbols and libraries the check is given.
mkdir "$tmp/fakeelf"
printf '#!/bin/sh\ncase $1 in\n--dyn-syms) cat "%s" ;;\n--dynamic) cat "%s" ;;\nesac\n' \
	"$tmp/fakeelf/syms" "$tmp/fakeelf/dyn" >"$tmp/fakeelf/readelf"
chmod +x "$tmp/fakeelf/readelf"
printf '2.34\n' >"$tmp/fakeelf/floor"

# fakeelf sets the stand-in's symbols to versions $1 and its libraries
# to $2.
fakeelf() {
	: >"$tmp/fakeelf/syms"
	for v in $1; do
		printf '     1: 0000000000000000     0 FUNC    GLOBAL DEFAULT  UND f%s@GLIBC_%s (2)\n' "$v" "$v" >>"$tmp/fakeelf/syms"
	done
	: >"$tmp/fakeelf/dyn"
	for l in $2; do
		printf ' 0x0000000000000001 (NEEDED)             Shared library: [%s]\n' "$l" >>"$tmp/fakeelf/dyn"
	done
}

fakeelf "2.2.5 2.34 2.17" libc.so.6
expect_ok READELF="$tmp/fakeelf/readelf" sh "$root/tools/release/glibc-check.sh" "$tack" "$tmp/fakeelf/floor"
fakeelf "2.2.5 2.39 2.34" libc.so.6
expect_fail "needs glibc 2.39, above the floor 2.34, for: f2.39@GLIBC_2.39" READELF="$tmp/fakeelf/readelf" \
	sh "$root/tools/release/glibc-check.sh" "$tack" "$tmp/fakeelf/floor"
fakeelf "2.34.1" libc.so.6
expect_fail "needs glibc 2.34.1, above the floor 2.34" READELF="$tmp/fakeelf/readelf" sh "$root/tools/release/glibc-check.sh" "$tack" "$tmp/fakeelf/floor"
fakeelf "2.17" "libc.so.6 libm.so.6"
expect_fail "needs the libraries libc.so.6 libm.so.6 , not libc.so.6 alone" READELF="$tmp/fakeelf/readelf" \
	sh "$root/tools/release/glibc-check.sh" "$tack" "$tmp/fakeelf/floor"
fakeelf "" libc.so.6
expect_fail "names no glibc version" READELF="$tmp/fakeelf/readelf" sh "$root/tools/release/glibc-check.sh" "$tack" "$tmp/fakeelf/floor"
printf '2.x\n' >"$tmp/fakeelf/badfloor"
expect_fail "not a glibc version MAJOR.MINOR" READELF="$tmp/fakeelf/readelf" sh "$root/tools/release/glibc-check.sh" "$tack" "$tmp/fakeelf/badfloor"

# --- crosscheck.sh ---

check "crosscheck.sh passes a stage 2 Go's stage 1 builds the same, and fails one it builds otherwise"
mkdir -p "$tmp/cross/tree/bin/bootstrap" "$tmp/cross/tree/driver"
printf 'stage 2\n' >"$tmp/cross/tree/bin/bootstrap/stage2"
# Stand-ins for Go's stage 1 write, where --out says, the stage 2 they
# are given, the same bytes as the tree's or others.
for k in same other; do
	if [ $k = same ]; then bytes='stage 2'; else bytes='stage 2, otherwise'; fi
	printf '#!/bin/sh\nwhile [ $# -gt 0 ]; do\n\tif [ "$1" = --out ]; then out=$2; fi\n\tshift\ndone\nprintf "%s\\n" >"$out"\n' \
		"$bytes" >"$tmp/cross/$k"
	chmod +x "$tmp/cross/$k"
done
expect_ok sh "$root/tools/release/crosscheck.sh" "$tmp/cross/tree" "$tmp/cross/same" "$tmp/cross/out1"
expect_fail "stage 2 from Go's stage 1 is not stage 2 from the previous release's" \
	sh "$root/tools/release/crosscheck.sh" "$tmp/cross/tree" "$tmp/cross/other" "$tmp/cross/out2"

check "crosscheck.sh compares only what this build writes, never the tree's stage 2 with itself"
# A stand-in that exits 0 and writes nothing, and one that fails.
printf '#!/bin/sh\nexit 0\n' >"$tmp/cross/noop"
printf '#!/bin/sh\nexit 1\n' >"$tmp/cross/broken"
chmod +x "$tmp/cross/noop" "$tmp/cross/broken"
# OUT filled beforehand with stage 2's bytes must not pass a build that
# wrote nothing.
cp "$tmp/cross/tree/bin/bootstrap/stage2" "$tmp/cross/out3"
expect_fail "Go's stage 1 exited 0 but wrote no" sh "$root/tools/release/crosscheck.sh" "$tmp/cross/tree" "$tmp/cross/noop" "$tmp/cross/out3"
[ ! -e "$tmp/cross/out3" ] || fail "crosscheck.sh left the stale OUT in place"
expect_fail "Go's stage 1 failed to build stage 2" sh "$root/tools/release/crosscheck.sh" "$tmp/cross/tree" "$tmp/cross/broken" "$tmp/cross/out4"
# OUT naming the tree's own stage 2, as it is, through .., or through a
# symbolic link, is refused before anything is written there.
ln -s "$tmp/cross/tree/bin/bootstrap/stage2" "$tmp/cross/link-to-stage2"
for o in "$tmp/cross/tree/bin/bootstrap/stage2" "$tmp/cross/tree/bin/../bin/bootstrap/stage2" "$tmp/cross/link-to-stage2"; do
	expect_fail "is the tree's own stage 2" sh "$root/tools/release/crosscheck.sh" "$tmp/cross/tree" "$tmp/cross/other" "$o"
	[ "$(cat "$tmp/cross/tree/bin/bootstrap/stage2")" = "stage 2" ] || fail "crosscheck.sh wrote the tree's stage 2 through $o"
done
(cd "$tmp/cross/tree" && expect_fail "is the tree's own stage 2" sh "$root/tools/release/crosscheck.sh" . "$tmp/cross/other" bin/bootstrap/stage2)

# --- package.sh ---

check "package.sh makes the same bytes twice"
mkdir "$tmp/pkg1" "$tmp/pkg2"
(cd "$tmp/pkg1" && umask 022 && clean VERSION=v0.0.1 SOURCE_DATE_EPOCH=$epoch \
	sh "$root/tools/release/package.sh" "$root" "$tack" "$tmp/pkg1/a.tar.xz") >"$tmp/out" 2>&1 || fail "$(cat "$tmp/out")"
(cd "$tmp/pkg2" && umask 077 && clean VERSION=v0.0.1 SOURCE_DATE_EPOCH=$epoch \
	sh "$root/tools/release/package.sh" "$root" "$tack" "$tmp/pkg2/b.tar.xz") >"$tmp/out" 2>&1 || fail "$(cat "$tmp/out")"
cmp "$tmp/pkg1/a.tar.xz" "$tmp/pkg2/b.tar.xz" || fail "two tarballs of the same inputs differ"

check "the tarball holds the release's layout"
top=tack-v0.0.1-linux-x86_64
LC_ALL=C TZ=UTC tar -tvJf "$tmp/pkg1/a.tar.xz" --numeric-owner --full-time >"$tmp/listing"
when=$(TZ=UTC date -d "@$epoch" '+%Y-%m-%d %H:%M:%S')
set -f
while IFS= read -r l; do
	set -- $l
	[ "$2" = 0/0 ] || fail "an entry is owned by $2, not 0/0: $l"
	[ "$4 $5" = "$when" ] || fail "an entry is dated $4 $5, not $when: $l"
	case $6 in
	"$top" | "$top"/*) ;;
	*) fail "an entry is outside $top: $l" ;;
	esac
	case $6 in
	"$top/bin/tack") want=-rwxr-xr-x ;;
	*/) want=drwxr-xr-x ;;
	*) want=-rw-r--r-- ;;
	esac
	[ "$1" = "$want" ] || fail "$6 is $1, not $want"
done <"$tmp/listing"
set +f
mkdir "$tmp/x"
tar -xJf "$tmp/pkg1/a.tar.xz" -C "$tmp/x"
cmp "$tmp/x/$top/runtime/tack.mod" "$root/runtime/tack.mod" || fail "runtime/tack.mod is not this tree's"
[ "$(ls -A "$tmp/x/$top" | LC_ALL=C sort | tr "\n" " ")" = "LICENSE README bin runtime src tools " ] ||
	fail "the tarball's top directory holds $(ls -A "$tmp/x/$top" | tr '\n' ' ')"
cmp "$tmp/x/$top/bin/tack" "$tack" || fail "bin/tack is not TACK"
diff -r "$root/src/std" "$tmp/x/$top/src/std" >/dev/null || fail "src/std is not this tree's"
cmp "$tmp/x/$top/src/std/builtin/builtin.tck" "$root/runtime/builtin.tck" || fail "std's builtin.tck is not runtime's"
cmp "$tmp/x/$top/runtime/tack/tack.h" "$root/runtime/tack/tack.h" || fail "runtime/tack/tack.h is not this tree's"
cmp "$tmp/x/$top/tools/install.sh" "$root/tools/install.sh" || fail "tools/install.sh is not this tree's"
cmp "$tmp/x/$top/LICENSE" "$root/LICENSE" || fail "LICENSE is not this tree's"
grep -q "^Tack v0.0.1, " "$tmp/x/$top/README" || fail "README does not name v0.0.1"
! grep -q @VERSION@ "$tmp/x/$top/README" || fail "README still holds @VERSION@"
[ "$(ls -A "$tmp/pkg1" | tr '\n' ' ')" = "a.tar.xz " ] || fail "package.sh left $(ls -A "$tmp/pkg1")"

check "package.sh refuses what it cannot make the same everywhere"
expect_fail "SOURCE_DATE_EPOCH must be set" VERSION=v0.0.1 sh "$root/tools/release/package.sh" "$root" "$tack" "$tmp/p.tar.xz"
expect_fail "SOURCE_DATE_EPOCH must be set" VERSION=v0.0.1 SOURCE_DATE_EPOCH=1e9 sh "$root/tools/release/package.sh" "$root" "$tack" "$tmp/p.tar.xz"
expect_fail "VERSION must be a release tag" VERSION=0.0.1 SOURCE_DATE_EPOCH=$epoch sh "$root/tools/release/package.sh" "$root" "$tack" "$tmp/p.tar.xz"
mkdir -p "$tmp/nolicense/tools/release" "$tmp/nolicense/runtime/tack" "$tmp/nolicense/src"
cp -R "$root/src/std" "$tmp/nolicense/src/std"
cp "$root/tools/install.sh" "$tmp/nolicense/tools/"
cp "$root/tools/release/README.txt" "$tmp/nolicense/tools/release/"
cp "$root/runtime/builtin.tck" "$tmp/nolicense/runtime/"
cp "$root/runtime/tack/tack.h" "$tmp/nolicense/runtime/tack/"
cp "$root/runtime/tack.mod" "$tmp/nolicense/runtime/"
expect_fail "has no LICENSE" VERSION=v0.0.1 SOURCE_DATE_EPOCH=$epoch sh "$root/tools/release/package.sh" "$tmp/nolicense" "$tack" "$tmp/p.tar.xz"
[ ! -e "$tmp/p.tar.xz" ] || fail "a refused package wrote $tmp/p.tar.xz"

# --- the keys ---

check "making the throwaway test keys"
keys=$tmp/keys
mkdir "$keys"
# Both keys are THROWAWAY TEST KEYS, made here and removed with $tmp.
clean minisign -G -W -p "$keys/nopass.pub" -s "$keys/nopass.key" -c "THROWAWAY TEST KEY, no password" >"$tmp/out" 2>&1 ||
	fail "minisign -G -W: $(cat "$tmp/out")"
printf 'test-only-password\ntest-only-password\n' |
	clean minisign -G -p "$keys/pass.pub" -s "$keys/pass.key" -c "THROWAWAY TEST KEY, password-protected" >"$tmp/out" 2>&1 ||
	fail "minisign -G: $(cat "$tmp/out")"
pubkey=$(sed -n 2p "$keys/pass.pub")
otherkey=$(sed -n 2p "$keys/nopass.pub")

# --- sums.sh and sign.sh ---

# release makes a release VERSION $1 in directory $2: the tarball, as
# package.sh writes it, SHA256SUMS and the signatures by the
# password-protected key, through sign.sh.
release() {
	mkdir -p "$2"
	clean VERSION="$1" SOURCE_DATE_EPOCH=$epoch \
		sh "$root/tools/release/package.sh" "$root" "$tack" "$2/tack-$1-linux-x86_64.tar.xz" >"$tmp/out" 2>&1 ||
		fail "package.sh: $(cat "$tmp/out")"
	clean sh "$root/tools/release/sums.sh" "$2" >"$tmp/out" 2>&1 || fail "sums.sh: $(cat "$tmp/out")"
	clean VERSION="$1" MINISIGN_SECRET_KEY="$(cat "$keys/pass.key")" MINISIGN_PASSPHRASE=test-only-password \
		sh "$root/tools/release/sign.sh" "$2" "$keys/pass.pub" >"$tmp/out" 2>&1 || fail "sign.sh: $(cat "$tmp/out")"
}

check "sums.sh writes SHA256SUMS"
mkdir "$tmp/sums"
cp "$tmp/pkg1/a.tar.xz" "$tmp/sums/tack-v0.0.1-linux-x86_64.tar.xz"
expect_ok sh "$root/tools/release/sums.sh" "$tmp/sums"
[ "$(grep -c '' "$tmp/sums/SHA256SUMS")" -eq 1 ] || fail "SHA256SUMS holds other than one line"
(cd "$tmp/sums" && sha256sum --check --strict --quiet SHA256SUMS) || fail "sha256sum -c rejects SHA256SUMS"
mkdir "$tmp/nosums"
expect_fail "holds no tarball" sh "$root/tools/release/sums.sh" "$tmp/nosums"

check "sign.sh signs with a password-protected key, mode 600, removed after"
# A stand-in for minisign first on PATH records the mode of each secret
# key file it is given to sign with, then runs minisign.
mkdir "$tmp/spy"
realminisign=$(command -v minisign)
cat >"$tmp/spy/minisign" <<EOF
#!/bin/sh
prev=
for a in "\$@"; do
	if [ "\$prev" = -s ]; then stat -c %a "\$a" >>"$tmp/spy/modes"; fi
	prev=\$a
done
exec "$realminisign" "\$@"
EOF
chmod +x "$tmp/spy/minisign"
mkdir "$tmp/signtmp"
env -i PATH="$tmp/spy:$PATH" TMPDIR="$tmp/signtmp" VERSION=v0.0.1 MINISIGN_SECRET_KEY="$(cat "$keys/pass.key")" \
	MINISIGN_PASSPHRASE=test-only-password sh "$root/tools/release/sign.sh" "$tmp/sums" "$keys/pass.pub" >"$tmp/out" 2>&1 ||
	fail "sign.sh: $(cat "$tmp/out")"
[ "$(sort -u "$tmp/spy/modes")" = 600 ] || fail "the key file was mode $(sort -u "$tmp/spy/modes" | tr '\n' ' '), not 600"
[ -z "$(ls -A "$tmp/signtmp")" ] || fail "sign.sh left $(ls -A "$tmp/signtmp") in its TMPDIR"
! grep -q test-only-password "$tmp/out" || fail "sign.sh's output holds the password"
! grep -qF "$(sed -n 2p "$keys/pass.key")" "$tmp/out" || fail "sign.sh's output holds the secret key"
for f in SHA256SUMS tack-v0.0.1-linux-x86_64.tar.xz; do
	c=$(clean minisign -V -P "$pubkey" -m "$tmp/sums/$f" -x "$tmp/sums/$f.minisig" -Q) || fail "$f.minisig does not verify"
	[ "$c" = "tack v0.0.1 $f" ] || fail "$f.minisig's trusted comment is '$c'"
done

check "sign.sh refuses a wrong password, none, and a key that is not the public key's"
expect_fail "signing" VERSION=v0.0.1 MINISIGN_SECRET_KEY="$(cat "$keys/pass.key")" MINISIGN_PASSPHRASE=wrong \
	sh "$root/tools/release/sign.sh" "$tmp/sums" "$keys/pass.pub"
expect_fail "MINISIGN_PASSPHRASE is not set" VERSION=v0.0.1 MINISIGN_SECRET_KEY="$(cat "$keys/pass.key")" \
	sh "$root/tools/release/sign.sh" "$tmp/sums" "$keys/pass.pub"
expect_fail "MINISIGN_SECRET_KEY is not set" VERSION=v0.0.1 MINISIGN_PASSPHRASE=x \
	sh "$root/tools/release/sign.sh" "$tmp/sums" "$keys/pass.pub"
expect_fail "is not that public key's" VERSION=v0.0.1 MINISIGN_SECRET_KEY="$(cat "$keys/nopass.key")" MINISIGN_PASSPHRASE=x \
	sh "$root/tools/release/sign.sh" "$tmp/sums" "$keys/pass.pub"

# --- get-tack.sh ---

check "making the file:// releases"
mirror=$tmp/mirror
release v0.0.1 "$mirror/download/v0.0.1"
release v0.0.2 "$mirror/download/v0.0.2"
mkdir -p "$mirror/latest/download"
cp "$mirror/download/v0.0.2/SHA256SUMS" "$mirror/download/v0.0.2/SHA256SUMS.minisig" "$mirror/latest/download/"

# farm makes directory $1 hold links to the tools get-tack.sh and
# install.sh need, and no others, and minisign too when $2 is 1, and the
# stand-in for gh when $3 is 1.
farm() {
	mkdir "$1"
	for t in sh curl sha256sum tar xz grep sed realpath mktemp uname cat cp mv rm mkdir chmod ls dirname cut head tr \
		id env sort ln touch basename find stat cmp; do
		p=$(command -v "$t") || fail "$t is not on PATH"
		ln -s "$(realpath -e "$p")" "$1/$t"
	done
	[ "$2" = 0 ] || ln -s "$realminisign" "$1/minisign"
	[ "$3" = 0 ] || ln -s "$tmp/gh" "$1/gh"
}

# The stand-in for gh appends its arguments to $tmp/gh.log, one line,
# and exits with the status $tmp/gh.exit holds.
cat >"$tmp/gh" <<EOF
#!/bin/sh
echo "\$*" >>"$tmp/gh.log"
exit "\$(cat "$tmp/gh.exit")"
EOF
chmod +x "$tmp/gh"
echo 0 >"$tmp/gh.exit"
farm "$tmp/farm-none" 0 0
farm "$tmp/farm-sig" 1 0
farm "$tmp/farm-all" 1 1

# get runs get-tack.sh with PATH $1, HOME $2, the file:// releases of
# $mirror, or those of $MIRROR when set, and its arguments but the first
# two, more settings first among them, as clean runs it, its output in
# $tmp/out, and returns its exit status. The key it verifies with never
# depends on the key committed in get-tack.sh: TACK_MINISIGN_KEY is the
# test key, or UNSET, the placeholder, when KEY is UNSET; only when KEY
# is committed does the run get no TACK_MINISIGN_KEY, and use the key
# get-tack.sh holds.
get() {
	path=$1
	h=$2
	shift 2
	case ${KEY:-test} in
	test) k=$pubkey ;;
	UNSET) k=UNSET ;;
	committed) k= ;;
	*) fail "get: KEY is $KEY, not test, UNSET or committed" ;;
	esac
	# The settings, the arguments up to the first that is an option or
	# holds no =, go before sh and the script, the rest after them.
	n=0
	for a in "$@"; do
		case $a in
		-*) break ;;
		*=*) n=$((n + 1)) ;;
		*) break ;;
		esac
	done
	total=$#
	i=0
	for a in "$@"; do
		[ "$i" -ne "$n" ] || set -- "$@" sh "$root/tools/get-tack.sh"
		set -- "$@" "$a"
		i=$((i + 1))
	done
	[ "$i" -ne "$n" ] || set -- "$@" sh "$root/tools/get-tack.sh"
	shift "$total"
	if [ -n "$k" ]; then
		set -- TACK_MINISIGN_KEY="$k" "$@"
	fi
	env -i PATH="$path" HOME="$h" TMPDIR="$tmp/tmp" TACK_RELEASES="file://${MIRROR:-$mirror}" \
		"$@" >"$tmp/out" 2>&1
}

# get_fails runs get with its arguments but the first, which must fail
# saying the first, installing nothing in the home it is given.
get_fails() {
	want=$1
	shift
	if get "$@"; then
		fail "get-tack.sh $* succeeded; it must fail with: $want"
	fi
	grep -qF -- "$want" "$tmp/out" || fail "get-tack.sh $* did not say: $want; it said: $(cat "$tmp/out")"
	[ ! -e "$2/.local" ] && [ ! -e "$2/.tack" ] || fail "get-tack.sh $* failed, but installed into $2"
	[ -z "$(ls -A "$tmp/tmp")" ] || fail "get-tack.sh $* left $(ls -A "$tmp/tmp") in its TMPDIR"
}

check "get-tack.sh installs the latest release, every check run"
h=$tmp/home1
mkdir "$h"
: >"$tmp/gh.log"
get "$tmp/farm-all" "$h" || fail "$(cat "$tmp/out")"
grep -qF "the latest release is v0.0.2" "$tmp/out" || fail "it did not name the latest release: $(cat "$tmp/out")"
! grep -q WARNING "$tmp/out" || fail "it warned with every check run: $(cat "$tmp/out")"
cmp "$h/.local/bin/tack" "$tack" || fail "the installed compiler is not the release's"
diff -r "$root/src/std" "$h/.tack/src/std" >/dev/null || fail "the installed std is not the release's"
[ "$(stat -c %a "$h/.tack/src/std/builtin/builtin.tck")" = 444 ] || fail "std's builtin.tck is not read-only"
grep -qx "attestation verify $tmp/tmp/get-tack\.[^ ]*/tack-v0\.0\.2-linux-x86_64\.tar\.xz -R tacklang/tack --signer-workflow tacklang/tack/\.github/workflows/release\.yml --source-ref refs/tags/v0\.0\.2 --deny-self-hosted-runners" "$tmp/gh.log" ||
	fail "gh was not run as gh attestation verify <tarball> -R tacklang/tack with the release workflow, the tag and no self-hosted runner: $(cat "$tmp/gh.log")"
[ -z "$(ls -A "$tmp/tmp")" ] || fail "get-tack.sh left $(ls -A "$tmp/tmp") in its TMPDIR"

check "the installed compiler builds and runs a program that imports std/fmt"
mkdir "$tmp/hello"
printf 'package main\n\nimport fmt from "std/fmt"\n\nfunc main() {\n    fmt.printf("hello %%d\\n", 42)\n}\n' >"$tmp/hello/main.tck"
(cd "$tmp/hello" && env -i PATH="$h/.local/bin:$PATH" HOME="$h" TMPDIR="$tmp/tmp" tack --out hello main.tck) >"$tmp/out" 2>&1 ||
	fail "building with the installed compiler: $(cat "$tmp/out")"
[ "$("$tmp/hello/hello")" = "hello 42" ] || fail "the program it built printed otherwise"

check "get-tack.sh installs a release by its version into PREFIX and TACK_HOME"
h=$tmp/home2
mkdir "$h"
get "$tmp/farm-all" "$h" PREFIX="$tmp/prefix" TACK_HOME="$tmp/tackhome" --version v0.0.1 || fail "$(cat "$tmp/out")"
cmp "$tmp/prefix/bin/tack" "$tack" || fail "the compiler is not in PREFIX/bin"
[ -d "$tmp/tackhome/src/std" ] || fail "std is not in TACK_HOME"
[ ! -e "$h/.local" ] && [ ! -e "$h/.tack" ] || fail "it installed into HOME too"
grep -qF "tack-v0.0.1-linux-x86_64.tar.xz -R tacklang/tack --signer-workflow tacklang/tack/.github/workflows/release.yml --source-ref refs/tags/v0.0.1 " "$tmp/gh.log" ||
	fail "gh did not verify v0.0.1 as its tag's"

check "get-tack.sh --download extracts a release and installs nothing"
h=$tmp/home3
mkdir "$h"
get "$tmp/farm-all" "$h" --version v0.0.1 --download "$tmp/dl" || fail "$(cat "$tmp/out")"
cmp "$tmp/dl/bin/tack" "$tack" || fail "--download holds no bin/tack"
[ -f "$tmp/dl/src/std/fmt/fmt.tck" ] || [ -d "$tmp/dl/src/std/fmt" ] || fail "--download holds no std"
[ ! -e "$h/.local" ] && [ ! -e "$h/.tack" ] || fail "--download installed"
get_fails "is already there" "$tmp/farm-all" "$h" --download "$tmp/dl"

check "get-tack.sh warns of each check it cannot run, and fails with --strict"
h=$tmp/home4
mkdir "$h"
get "$tmp/farm-none" "$h" || fail "$(cat "$tmp/out")"
grep -qF "WARNING: minisign is not on PATH" "$tmp/out" || fail "no warning of minisign: $(cat "$tmp/out")"
grep -qF "WARNING: gh is not on PATH" "$tmp/out" || fail "no warning of gh: $(cat "$tmp/out")"
h=$tmp/home5
mkdir "$h"
KEY=UNSET
get "$tmp/farm-sig" "$h" || fail "$(cat "$tmp/out")"
KEY=test
grep -qF "WARNING: no release public key is set" "$tmp/out" || fail "no warning of the unset key: $(cat "$tmp/out")"
mkdir "$tmp/home6"
get_fails "minisign is not on PATH, so" "$tmp/farm-none" "$tmp/home6" --strict
get_fails "gh is not on PATH, so" "$tmp/farm-sig" "$tmp/home6" --strict
KEY=UNSET
get_fails "no release public key is set" "$tmp/farm-all" "$tmp/home6" --strict
KEY=test

check "get-tack.sh's own key, the committed one, rejects a release signed with the test key"
committed=$(sed -n 's/^TACK_RELEASE_KEY=\(.*\)$/\1/p' "$root/tools/get-tack.sh")
[ -n "$committed" ] || fail "get-tack.sh holds no TACK_RELEASE_KEY line"
mkdir "$tmp/home-committed"
KEY=committed
if [ "$committed" = UNSET ]; then
	# With no key committed yet, the signatures go unchecked, warned of.
	get "$tmp/farm-sig" "$tmp/home-committed" --download "$tmp/dl-committed" || fail "$(cat "$tmp/out")"
	grep -qF "WARNING: no release public key is set" "$tmp/out" || fail "no warning of the unset key: $(cat "$tmp/out")"
else
	[ "$committed" != "$pubkey" ] || fail "the committed key is the test key"
	get_fails "the signature of SHA256SUMS does not verify with the release public key $committed" \
		"$tmp/farm-sig" "$tmp/home-committed"
	get_fails "the signature of SHA256SUMS does not verify with the release public key $committed" \
		"$tmp/farm-all" "$tmp/home-committed" --strict --version v0.0.1
fi
KEY=test

# tamper makes $tmp/m-$1, a copy of $mirror for the check to change, and
# sets MIRROR to it.
tamper() {
	MIRROR=$tmp/m-$1
	cp -R "$mirror" "$MIRROR"
}

h=$tmp/home7
mkdir "$h"
v2=download/v0.0.2
tb=tack-v0.0.2-linux-x86_64.tar.xz

check "get-tack.sh refuses a tampered tarball"
tamper tarball
printf 'X' | dd of="$MIRROR/$v2/$tb" bs=1 seek=100 conv=notrunc 2>/dev/null
get_fails "is not the release's" "$tmp/farm-all" "$h" --version v0.0.2
get_fails "is not the release's" "$tmp/farm-none" "$h" --version v0.0.2

check "get-tack.sh refuses a wrong checksum, signed or not"
tamper sums
sed 's/^./0/' "$MIRROR/$v2/SHA256SUMS" >"$tmp/s" && cmp -s "$tmp/s" "$MIRROR/$v2/SHA256SUMS" && sed 's/^./1/' "$MIRROR/$v2/SHA256SUMS" >"$tmp/s"
cp "$tmp/s" "$MIRROR/$v2/SHA256SUMS"
get_fails "is not the release's" "$tmp/farm-none" "$h" --version v0.0.2
tamper sums-matching
printf 'X' | dd of="$MIRROR/$v2/$tb" bs=1 seek=100 conv=notrunc 2>/dev/null
(cd "$MIRROR/$v2" && sha256sum "$tb" >SHA256SUMS)
get_fails "the signature of SHA256SUMS does not verify" "$tmp/farm-sig" "$h" --version v0.0.2

check "get-tack.sh refuses a signature by another key"
tamper other-key
clean minisign -S -s "$keys/nopass.key" -m "$MIRROR/$v2/$tb" -x "$MIRROR/$v2/$tb.minisig" -t "tack v0.0.2 $tb" >/dev/null 2>&1 ||
	fail "signing with the other key failed"
get_fails "the signature of $tb does not verify" "$tmp/farm-sig" "$h" --version v0.0.2
tamper other-key-sums
clean minisign -S -s "$keys/nopass.key" -m "$MIRROR/$v2/SHA256SUMS" -x "$MIRROR/$v2/SHA256SUMS.minisig" -t "tack v0.0.2 SHA256SUMS" >/dev/null 2>&1 ||
	fail "signing with the other key failed"
get_fails "the signature of SHA256SUMS does not verify" "$tmp/farm-sig" "$h" --version v0.0.2
[ "$otherkey" != "$pubkey" ] || fail "the two test keys are the same"

check "get-tack.sh refuses a signature of another release, and another release's SHA256SUMS"
tamper comment
printf 'test-only-password\n' | clean minisign -S -s "$keys/pass.key" -m "$MIRROR/$v2/$tb" -x "$MIRROR/$v2/$tb.minisig" \
	-t "tack v0.0.1 $tb" >/dev/null 2>&1 || fail "signing with another comment failed"
get_fails "is of 'tack v0.0.1 $tb', not of 'tack v0.0.2 $tb'" "$tmp/farm-sig" "$h" --version v0.0.2
tamper replay
cp "$MIRROR/download/v0.0.1/SHA256SUMS" "$MIRROR/download/v0.0.1/SHA256SUMS.minisig" "$MIRROR/$v2/"
get_fails "SHA256SUMS of v0.0.2 names tack-v0.0.1-linux-x86_64.tar.xz" "$tmp/farm-sig" "$h" --version v0.0.2
tamper replay-latest
cp "$MIRROR/download/v0.0.1/SHA256SUMS" "$MIRROR/latest/download/SHA256SUMS"
get_fails "the signature of SHA256SUMS does not verify" "$tmp/farm-sig" "$h"
tamper comment-sums
printf 'test-only-password\n' | clean minisign -S -s "$keys/pass.key" -m "$MIRROR/$v2/SHA256SUMS" -x "$MIRROR/$v2/SHA256SUMS.minisig" \
	-t "tack v0.0.1 SHA256SUMS" >/dev/null 2>&1 || fail "signing with another comment failed"
get_fails "is of 'tack v0.0.1 SHA256SUMS', not of 'tack v0.0.2 SHA256SUMS'" "$tmp/farm-sig" "$h" --version v0.0.2
tamper two-lines
cat "$MIRROR/download/v0.0.1/SHA256SUMS" >>"$MIRROR/$v2/SHA256SUMS"
get_fails "SHA256SUMS holds 2 lines" "$tmp/farm-none" "$h" --version v0.0.2

check "get-tack.sh refuses, before extracting, a tarball holding what is no file or directory, or outside its top"
top2=tack-v0.0.2-linux-x86_64
# craft makes $tmp/m-$1, a copy of $mirror whose v0.0.2 tarball is the
# release's with what add, run in the release's extracted copy, appends
# to it, signed again with the test key, so every check before the
# listing passes.
craft() {
	tamper "$1"
	c=$tmp/craft-$1
	mkdir -p "$c"
	tar -xJf "$MIRROR/$v2/$tb" -C "$c"
	(cd "$c" && tar -cf t.tar "$top2" && : >evil && add) || fail "crafting the tarball of $1"
	rm -f "$MIRROR/$v2/$tb"
	xz -c "$c/t.tar" >"$MIRROR/$v2/$tb"
	rm -f "$MIRROR/$v2/SHA256SUMS" "$MIRROR/$v2/"*.minisig
	clean sh "$root/tools/release/sums.sh" "$MIRROR/$v2" >/dev/null 2>&1 || fail "sums.sh of $1"
	clean VERSION=v0.0.2 MINISIGN_SECRET_KEY="$(cat "$keys/pass.key")" MINISIGN_PASSPHRASE=test-only-password \
		sh "$root/tools/release/sign.sh" "$MIRROR/$v2" "$keys/pass.pub" >/dev/null 2>&1 || fail "sign.sh of $1"
}
add() { ln -s tack "$top2/bin/link" && tar -rf t.tar "$top2/bin/link"; }
craft symlink
get_fails "holds an entry that is no regular file or directory: l" "$tmp/farm-sig" "$h" --version v0.0.2
add() { ln "$top2/bin/tack" "$top2/bin/hard" && tar -cf t.tar "$top2"; }
craft hardlink
get_fails "holds an entry that is no regular file or directory: h" "$tmp/farm-sig" "$h" --version v0.0.2
add() { tar -rf t.tar -C / --transform "s,^dev/null\$,$top2/null," dev/null; }
craft device
get_fails "holds an entry that is no regular file or directory: c" "$tmp/farm-sig" "$h" --version v0.0.2
add() { tar -rf t.tar --transform "s,^evil\$,$top2/../evil," evil 2>/dev/null; }
craft dotdot
get_fails "holds $top2/../evil, which climbs out with .." "$tmp/farm-sig" "$h" --version v0.0.2
add() { tar -rf t.tar -P --transform "s,^evil\$,/$top2/evil," evil; }
craft absolute
get_fails "holds /$top2/evil, outside its one top directory" "$tmp/farm-sig" "$h" --version v0.0.2
add() { tar -rf t.tar --transform 's,^evil$,other/evil,' evil; }
craft second-top
get_fails "holds other/evil, outside its one top directory" "$tmp/farm-sig" "$h" --version v0.0.2
MIRROR=

check "get-tack.sh refuses a missing signature and a failed attestation"
tamper no-sig
rm "$MIRROR/$v2/$tb.minisig"
get_fails "cannot download" "$tmp/farm-none" "$h" --version v0.0.2
MIRROR=
echo 1 >"$tmp/gh.exit"
get_fails "gh attestation verify of $tb failed" "$tmp/farm-all" "$h" --version v0.0.2
echo 0 >"$tmp/gh.exit"
get_fails "cannot download" "$tmp/farm-none" "$h" --version v0.0.9

check "get-tack.sh refuses bad arguments and a home in a source tree"
for v in v1.0 1.0.0 v1.0.0-rc.1 "v1.0.0 "; do
	get_fails "--version must be vMAJOR.MINOR.PATCH" "$tmp/farm-all" "$h" --version "$v"
done
get_fails "unknown argument" "$tmp/farm-all" "$h" --frobnicate
mkdir -p "$tmp/srctree/deep"
: >"$tmp/srctree/tack.mod"
get_fails "is inside a Tack source tree, $tmp/srctree" "$tmp/farm-all" "$h" TACK_HOME="$tmp/srctree/deep"
get_fails "is inside a Tack source tree, $tmp/srctree" "$tmp/farm-all" "$tmp/srctree"
ln -s "$tmp/srctree/deep" "$tmp/link-into-tree"
get_fails "is inside a Tack source tree, $tmp/srctree" "$tmp/farm-all" "$h" TACK_HOME="$tmp/link-into-tree"
get_fails "must be an absolute path" "$tmp/farm-all" "$h" TACK_HOME=relative/home
[ ! -e "$tmp/srctree/deep/src" ] && [ ! -e "$tmp/srctree/.tack" ] || fail "it wrote into the source tree"

# --- the key and the workflow ---

check "the release public key is the same everywhere"
filekey=$(sed -n 2p "$root/tools/release/minisign.pub")
[ -n "$filekey" ] || fail "tools/release/minisign.pub has no key on its second line"
scriptkey=$(sed -n 's/^TACK_RELEASE_KEY=\(.*\)$/\1/p' "$root/tools/get-tack.sh")
[ "$scriptkey" = "$filekey" ] || fail "get-tack.sh's key is '$scriptkey', minisign.pub's '$filekey'"
grep -qF -- "-P $filekey" "$root/README.md" || fail "the README does not give the key '$filekey' as minisign -P's"
grep -qF -- "--signer-workflow tacklang/tack/.github/workflows/release.yml" "$root/README.md" &&
	grep -qF -- "--source-ref refs/tags/vX.Y.Z --deny-self-hosted-runners" "$root/README.md" ||
	fail "the README's gh attestation verify does not hold it to the release workflow, the tag and GitHub's runners"

check "the glibc floor is the same in the READMEs as in tools/release/glibc-floor"
floor=$(cat "$root/tools/release/glibc-floor")
grep -qF "glibc $floor" "$root/README.md" || fail "the README does not state the glibc floor $floor"
grep -qF "glibc $floor" "$root/tools/release/README.txt" || fail "the tarball's README does not state the glibc floor $floor"

check "the workflows pin every action to a commit and keep secrets in env"
wf=$root/.github/workflows/release.yml
[ -f "$wf" ] || fail "there is no $wf"
for w in "$root"/.github/workflows/*.yml; do
	grep -E '^[[:space:]]*(-[[:space:]]+)?uses:' "$w" >"$tmp/uses" || continue
	while IFS= read -r l; do
		printf '%s\n' "$l" | grep -Eq 'uses:[[:space:]]+[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}[[:space:]]+# v[0-9]+\.[0-9]+\.[0-9]+$' ||
			fail "${w##*/}: an action is not pinned to a full commit SHA with its # vX.Y.Z beside it: $l"
	done <"$tmp/uses"
	awk '/^[[:space:]]*(-[[:space:]]+)?run:/ { inrun = 1; ind = match($0, /[^ ]/); if (/\$\{\{/) { print; bad = 1 }; next }
		inrun && match($0, /[^ ]/) && match($0, /[^ ]/) <= ind { inrun = 0 }
		inrun && /\$\{\{/ { print; bad = 1 }
		END { exit bad }' "$w" >"$tmp/out" || fail "${w##*/}: a run step expands \${{ }} in its script, not through env: $(cat "$tmp/out")"
done
grep -q '^  gate:$' "$wf" && grep -q 'sh tools/release/gate.sh' "$wf" && grep -q '^    needs: gate$' "$wf" &&
	grep -q 'actions/workflows/ci.yml/runs' "$wf" || fail "the release workflow does not gate the build on gate.sh and ci's runs"
[ "$(grep -c 'image: fedora:45@sha256:[0-9a-f]\{64\}$' "$wf")" = 2 ] && ! grep -q 'image: fedora:45$' "$wf" &&
	grep -q 'runuser -u ci' "$wf" ||
	fail "the release workflow does not build and publish in fedora:45 pinned by digest, building as ci, an unprivileged account"
# The release environment, and every secret, belong to publish alone,
# the last job of the file.
pub=$(grep -n '^  publish:$' "$wf" | cut -d: -f1)
[ -n "$pub" ] || fail "the release workflow has no publish job"
[ "$(grep -n '^    environment: release$' "$wf" | cut -d: -f1)" -gt "$pub" ] || fail "publish does not run in the release environment"
[ "$(grep -c 'environment:' "$wf")" = 1 ] || fail "another job than publish names an environment"
grep -n 'secrets\.' "$wf" | cut -d: -f1 | while IFS= read -r n; do
	[ "$n" -gt "$pub" ] || fail "a secret is read outside the publish job, at line $n"
done
! grep -q '^  [a-z]*:$' "$wf" || [ "$(grep -n '^  [a-z]*:$' "$wf" | tail -n 1 | cut -d: -f1)" = "$pub" ] ||
	fail "publish is not the release workflow's last job"

# --- make install-from-release ---

check "make install-from-release installs from a release"
tree=$tmp/tree
mkdir "$tree"
for f in "$root"/*; do
	[ "${f##*/}" = bin ] || cp -R "$f" "$tree/"
done
h=$tmp/home8
mkdir "$h"
mkdir "$tmp/gh-only"
ln -s "$tmp/gh" "$tmp/gh-only/gh"
: >"$tmp/gh.log"
(cd "$tree" && env -i PATH="$tmp/gh-only:$PATH" HOME="$h" TMPDIR="$tmp/tmp" TACK_RELEASES="file://$mirror" \
	TACK_MINISIGN_KEY="$pubkey" make -s install-from-release) >"$tmp/out" 2>&1 ||
	fail "make install-from-release: $(cat "$tmp/out")"
grep -qF "the latest release is v0.0.2" "$tmp/out" || fail "it did not get the latest release: $(cat "$tmp/out")"
grep -qF "tack-v0.0.2-linux-x86_64.tar.xz -R tacklang/tack" "$tmp/gh.log" || fail "it did not verify the attestation"
cmp "$h/.local/bin/tack" "$tree/bin/install/stage3" || fail "it did not install the stage 3 it built"
[ -d "$h/.tack/src/std" ] || fail "it did not install std"

echo "test-release: all checks passed"

#!/bin/sh
# glibc-check.sh checks that a compiler a release ships runs on every
# glibc from the release's floor up:
#
#   sh tools/release/glibc-check.sh BINARY FLOOR
#
# FLOOR is a file holding the oldest glibc releases support, MAJOR.MINOR,
# tools/release/glibc-floor. It reads BINARY with the readelf READELF
# names, or llvm-readelf when READELF is unset or empty and llvm-readelf
# is on PATH, or binutils' readelf otherwise, which reads the same two
# listings alike (see readelf_tool), saying which it uses. It reads
# BINARY's dynamic symbols, --dyn-syms --wide, and fails
# when any symbol needs a glibc version, GLIBC_x.y or GLIBC_x.y.z, above
# the floor, naming the highest and the symbols that need it; and reads
# its dynamic section, --dynamic, and fails unless the libraries it
# needs, its NEEDED entries, are libc.so.6 alone. build.sh runs it on
# stage 3 before the tarball is made. It needs llvm-readelf or readelf,
# grep, sed and sort.
set -eu

# die writes its arguments as one line to standard error and exits 1.
die() {
	echo "glibc-check: $*" >&2
	exit 1
}

[ $# -eq 2 ] || die "usage: tools/release/glibc-check.sh BINARY FLOOR"
binary=$1
# readelf_tool writes the readelf to use: $READELF when set, else
# llvm-readelf, else readelf, the first on PATH, failing when none is.
readelf_tool() {
	if [ -n "${READELF:-}" ]; then
		command -v "$READELF" >/dev/null || die "READELF, $READELF, is not on PATH"
		echo "$READELF"
	elif command -v llvm-readelf >/dev/null; then
		echo llvm-readelf
	elif command -v readelf >/dev/null; then
		echo readelf
	else
		die "neither llvm-readelf nor readelf is on PATH (set READELF to a readelf)"
	fi
}

readelf=$(readelf_tool)
echo "glibc-check: reading $binary with $readelf"
floor=$(cat "$2") || die "cannot read $2"
printf '%s' "$floor" | grep -Eqx '[0-9]+\.[0-9]+' || die "$2 holds '$floor', not a glibc version MAJOR.MINOR"
[ -f "$binary" ] || die "$binary is not a file"

syms=$("$readelf" --dyn-syms --wide "$binary") || die "$readelf --dyn-syms cannot read $binary"
versions=$(printf '%s\n' "$syms" | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)+' | sed 's/^GLIBC_//' | sort -uV) || versions=
[ -n "$versions" ] || die "$binary names no glibc version: it is not the dynamically linked compiler a release ships"
highest=$(printf '%s\n' "$versions" | tail -n 1)
top=$(printf '%s\n%s\n' "$floor" "$highest" | sort -V | tail -n 1)
if [ "$top" != "$floor" ] && [ "$highest" != "$floor" ]; then
	exact=$(printf '%s' "$highest" | sed 's/\./\\./g')
	needs=$(printf '%s\n' "$syms" | grep -oE '[^ ]*@+GLIBC_[0-9.]+' | grep -E "@+GLIBC_$exact\$" | sort -u | tr '\n' ' ')
	die "$binary needs glibc $highest, above the floor $floor, for: $needs"
fi

dynamic=$("$readelf" --dynamic "$binary") || die "$readelf --dynamic cannot read $binary"
needed=$(printf '%s\n' "$dynamic" | sed -n 's/.*(NEEDED).*\[\(.*\)\].*/\1/p' | tr '\n' ' ')
[ "$needed" = "libc.so.6 " ] || die "$binary needs the libraries ${needed:-none}, not libc.so.6 alone"
echo "glibc-check: $binary needs glibc $highest at most, within the floor $floor, and libc.so.6 alone"

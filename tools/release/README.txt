Tack @VERSION@, the Tack compiler, for Linux on x86-64.

This directory is a TACK_HOME: bin/tack is the compiler and src/std the
standard library it looks for at $TACK_HOME/src/std. To use it where it
is, set

    export TACK_HOME="$PWD"
    export PATH="$PWD/bin:$PATH"

from this directory. To install it, the compiler to ~/.local/bin and std
to ~/.tack/src/std, or where PREFIX, BINDIR and TACK_HOME say, run

    sh tools/install.sh install bin/tack

from this directory, or use tools/get-tack.sh, which downloads a
release, verifies it and installs it so. The compiler needs glibc 2.34
or newer, and clang on PATH to build programs.

The source, the documentation and how to verify a release are at
https://github.com/tacklang/tack. The license is in LICENSE.

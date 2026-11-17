// A clang that writes the argv[0] it was given to its standard error and
// exits 1. A script cannot show its argv[0]: the kernel gives its
// interpreter the script's path in its place, so the build probe that
// runs it compiles this file to a binary named clang first.
#include <stdio.h>

int main(int argc, char **argv) {
    fprintf(stderr, "fake clang: argv[0] is %s\n", argc > 0 ? argv[0] : "(none)");
    return 1;
}

// The types the C files of the standard library share with the C the
// Tack compiler generates. A std C file includes it as <tack/tack.h>; the
// driver compiles every one with this file's include directory,
// $TACK_HOME/src/std/include, on the include path.
//
// It holds only what is the same for every program. The tack.h a build
// generates for a program's own C files is another file: it declares that
// program's extern funcs and the types their signatures use, and holds
// these same definitions where it needs them.

#ifndef TACK_STD_TACK_H
#define TACK_STD_TACK_H

#include <stdint.h>

// A Tack string, passed by value: a pointer to its bytes and their count.
// Its bytes are not zero-terminated, and a zeroed string, of length 0,
// holds a null pointer. The generated C defines tack_string with exactly
// these members, in this order, so the two are one type to the linker.
typedef struct {
    const unsigned char *ptr;
    int64_t len;
} tack_string;

#endif

// The C every Tack program shares: the string type and the macros that
// build strings, defined here once for the generated source, the C files
// of the standard library and a program's own C files alike. Each
// includes it as <tack/tack.h>, and the driver compiles each with this
// file's include directory, $TACK_HOME/src/std/include, on the include
// path.
//
// It holds only what is the same for every program. The tack.h a build
// generates for a program's own C files is another file: it declares that
// program's extern funcs and the types their signatures use, and includes
// this one when a signature uses a string. The generated source always
// includes it.
//
// Every name it defines, its include guard among them, is reserved: no
// extern func or type a program's tack.h declares may take one.

#ifndef TACK_STD_TACK_H
#define TACK_STD_TACK_H

#include <stdint.h>

// A Tack string, passed by value: a pointer to its bytes and their count.
// Its bytes are not zero-terminated, and a zeroed string, of length 0,
// holds a null pointer. A string is one value that happens to occupy two
// words, so it is one struct rather than a pointer and a length passed
// separately, and can be returned, stored and nested like any other.
typedef struct {
    const unsigned char *ptr;
    int64_t len;
} tack_string;

// TACK_C_STR_LEN(a) is the length of the string the byte array a holds,
// its size less the zero that ends it. A pointer does not compile.
//
// The second term is 0 for an array and fails to compile for a pointer,
// whose size says nothing about its string: an array and a pointer to its
// first element have different types, and a pointer and one to its first
// element have the same, which makes the char array's size negative. The
// generated source reads the length of each string in its pool with it.
#define TACK_C_STR_LEN(x) (sizeof (x) - 1 + 0 * sizeof (char[1 - 2 * __builtin_types_compatible_p(__typeof__(x), __typeof__(&(x)[0]))]))
// TACK_STR(lit) is the string literal lit as a tack_string. Only a
// literal compiles.
#define TACK_STR(lit) ((tack_string){(const unsigned char *)("" lit ""), sizeof ("" lit "") - 1})
// TACK_STR_N(p, n) is the n bytes at p, a const unsigned char * or an
// unsigned char *, as a tack_string. Any other pointer does not compile.
//
// _Generic restricts p to the two pointers a string's bytes can be, so a
// char * or NULL fails to compile rather than converting silently, as it
// would under the build's -w.
#define TACK_STR_N(p, n) ((tack_string){_Generic((p), const unsigned char *: (p), unsigned char *: (p)), (int64_t)(n)})

#endif

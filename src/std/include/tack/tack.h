// The C every Tack program shares: the string type and the macros that
// build strings, and any, its type ids and the helpers that read one,
// defined here once for the generated source, the C files of the standard
// library and a program's own C files alike. Each
// includes it as <tack/tack.h>, and the driver compiles each with this
// file's include directory, $TACK_HOME/src/std/include, on the include
// path.
//
// It holds only what is the same for every program. The tack.h a build
// generates for a program's own C files is another file: it declares that
// program's extern funcs and the types their signatures use, and includes
// this one when a signature uses a string or any. The generated source
// always includes it.
//
// Every name it defines, its include guard among them, is reserved: no
// extern func or type a program's tack.h declares may take one.

#ifndef TACK_STD_TACK_H
#define TACK_STD_TACK_H

#include <stdbool.h>
#include <stddef.h>
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

// A Tack any, passed by value: the id of the type of the value it refers
// to, one of the TACK_TYPE_ ids below, and a pointer to that value, which
// is valid only for the call it was passed to. C reads the value only
// through the pointer, after checking the id, as the helpers below do,
// and never writes through it, keeps it or returns it: the value may be a
// constant in read-only memory, and is the caller's, gone once the call
// returns. ...any arrives as a view of them, tack_view_any, declared in
// the program's tack.h.
typedef struct {
    uint32_t type;
    const void *ptr;
} tack_any;

// The type ids an any can hold, one for each type a value can be passed
// as any with, fixed for good: C is written against them. 0 is no type,
// so a zeroed tack_any is recognisably empty; 13 to 255 are kept for
// types to come. Each names the C type ptr points to: bool, int8_t
// through uint64_t, size_t for unsafe_size_t, uintptr_t for
// unsafe_uintptr_t, and tack_string.
#define TACK_TYPE_INVALID 0
#define TACK_TYPE_BOOL 1
#define TACK_TYPE_INT8 2
#define TACK_TYPE_INT16 3
#define TACK_TYPE_INT32 4
#define TACK_TYPE_INT64 5
#define TACK_TYPE_UINT8 6
#define TACK_TYPE_UINT16 7
#define TACK_TYPE_UINT32 8
#define TACK_TYPE_UINT64 9
#define TACK_TYPE_UNSAFE_SIZE_T 10
#define TACK_TYPE_UNSAFE_UINTPTR_T 11
#define TACK_TYPE_STRING 12

// TACK_ANY_IS(a, T) reports whether the tack_any a holds a T, where T is
// the end of one of the ids above: TACK_ANY_IS(a, INT64). Any other T
// does not compile.
#define TACK_ANY_IS(a, T) ((a).type == TACK_TYPE_##T)

// The unpack helpers. Each, named after a type, stores the value a holds
// in *v and returns true when a holds that type, and returns false,
// leaving *v alone, otherwise: tack_any_int64(a, &v). They return a copy,
// never the pointer, so C has to cast const away to write the caller's
// value, which it must not do. They are functions rather than macros so
// that clang checks v's type, which a cast in a macro would hide.
static inline bool tack_any_bool(tack_any a, bool *v) {
    if (a.type != TACK_TYPE_BOOL) {
        return false;
    }
    *v = *(const bool *)a.ptr;
    return true;
}

static inline bool tack_any_int8(tack_any a, int8_t *v) {
    if (a.type != TACK_TYPE_INT8) {
        return false;
    }
    *v = *(const int8_t *)a.ptr;
    return true;
}

static inline bool tack_any_int16(tack_any a, int16_t *v) {
    if (a.type != TACK_TYPE_INT16) {
        return false;
    }
    *v = *(const int16_t *)a.ptr;
    return true;
}

static inline bool tack_any_int32(tack_any a, int32_t *v) {
    if (a.type != TACK_TYPE_INT32) {
        return false;
    }
    *v = *(const int32_t *)a.ptr;
    return true;
}

static inline bool tack_any_int64(tack_any a, int64_t *v) {
    if (a.type != TACK_TYPE_INT64) {
        return false;
    }
    *v = *(const int64_t *)a.ptr;
    return true;
}

static inline bool tack_any_uint8(tack_any a, uint8_t *v) {
    if (a.type != TACK_TYPE_UINT8) {
        return false;
    }
    *v = *(const uint8_t *)a.ptr;
    return true;
}

static inline bool tack_any_uint16(tack_any a, uint16_t *v) {
    if (a.type != TACK_TYPE_UINT16) {
        return false;
    }
    *v = *(const uint16_t *)a.ptr;
    return true;
}

static inline bool tack_any_uint32(tack_any a, uint32_t *v) {
    if (a.type != TACK_TYPE_UINT32) {
        return false;
    }
    *v = *(const uint32_t *)a.ptr;
    return true;
}

static inline bool tack_any_uint64(tack_any a, uint64_t *v) {
    if (a.type != TACK_TYPE_UINT64) {
        return false;
    }
    *v = *(const uint64_t *)a.ptr;
    return true;
}

static inline bool tack_any_unsafe_size_t(tack_any a, size_t *v) {
    if (a.type != TACK_TYPE_UNSAFE_SIZE_T) {
        return false;
    }
    *v = *(const size_t *)a.ptr;
    return true;
}

static inline bool tack_any_unsafe_uintptr_t(tack_any a, uintptr_t *v) {
    if (a.type != TACK_TYPE_UNSAFE_UINTPTR_T) {
        return false;
    }
    *v = *(const uintptr_t *)a.ptr;
    return true;
}

static inline bool tack_any_string(tack_any a, tack_string *v) {
    if (a.type != TACK_TYPE_STRING) {
        return false;
    }
    *v = *(const tack_string *)a.ptr;
    return true;
}

// tack_any_signed(a, &v) stores in v the value of a signed integer of any
// width, widened to int64_t, which keeps it, and returns true, or returns
// false, leaving v alone, when a holds anything else. It is what %d needs.
static inline bool tack_any_signed(tack_any a, int64_t *v) {
    switch (a.type) {
    case TACK_TYPE_INT8:
        *v = *(const int8_t *)a.ptr;
        return true;
    case TACK_TYPE_INT16:
        *v = *(const int16_t *)a.ptr;
        return true;
    case TACK_TYPE_INT32:
        *v = *(const int32_t *)a.ptr;
        return true;
    case TACK_TYPE_INT64:
        *v = *(const int64_t *)a.ptr;
        return true;
    default:
        return false;
    }
}

// tack_any_unsigned(a, &v) is tack_any_signed for the unsigned integers,
// unsafe_size_t and unsafe_uintptr_t among them, widened to uint64_t,
// which holds each at any width a target gives it.
static inline bool tack_any_unsigned(tack_any a, uint64_t *v) {
    switch (a.type) {
    case TACK_TYPE_UINT8:
        *v = *(const uint8_t *)a.ptr;
        return true;
    case TACK_TYPE_UINT16:
        *v = *(const uint16_t *)a.ptr;
        return true;
    case TACK_TYPE_UINT32:
        *v = *(const uint32_t *)a.ptr;
        return true;
    case TACK_TYPE_UINT64:
        *v = *(const uint64_t *)a.ptr;
        return true;
    case TACK_TYPE_UNSAFE_SIZE_T:
        *v = *(const size_t *)a.ptr;
        return true;
    case TACK_TYPE_UNSAFE_UINTPTR_T:
        *v = *(const uintptr_t *)a.ptr;
        return true;
    default:
        return false;
    }
}

#endif

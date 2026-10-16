// The C every Tack program shares: the string type and the macros that
// build strings, the error type and the macros that build errors,
// any, its type ids and the helpers that read one, and the allocator
// handle and the macros that read and grow a list,
// defined here once for the generated source, the C files of the standard
// library and a program's own C files alike. Each
// includes it as <tack/tack.h>, and the driver compiles each with this
// file's include directory, $TACK_HOME/src/std/include, on the include
// path.
//
// It holds only what is the same for every program. The tack.h a build
// generates for a program's own C files is another file: it declares that
// program's extern funcs and the types their signatures use, and includes
// this one when a signature uses a string, an error, any, a list or an
// Allocator. The generated source always includes it.
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

// A Tack error, passed by value: a code, 0 when there is no error, and a
// message, whose bytes must outlive every use of the error, as a string
// literal's do. Only the code says whether there is an error; a zeroed
// tack_error, code 0 and a message of length 0 holding a null pointer, is
// no error, Tack's noerror. A function that returns an error returns it
// as its last result, in its results struct when it has several.
typedef struct {
    int32_t code;
    tack_string msg;
} tack_error;

// TACK_ERROR(code, lit) is an error with that code and the string literal
// lit as its message: TACK_ERROR(2, "not found"). Only a literal compiles,
// as for TACK_STR.
#define TACK_ERROR(code, lit) ((tack_error){(int32_t)(code), TACK_STR(lit)})
// TACK_NO_ERROR is no error, as noerror is in Tack: code 0 and an empty
// message.
#define TACK_NO_ERROR ((tack_error){0, {0, 0}})

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

// A Tack Allocator, passed by value: an opaque handle on one of the
// allocators std's C keeps, the index of its entry and the generation of
// that entry, so a handle to an allocator that has ended can be told from
// one to its successor. c_allocator, the one there is today, is index 0,
// generation 0, and lowers to C's malloc, realloc and free. A list holds
// the handle of the allocator it grows and is freed through. C never
// makes one, changes one or reads its fields.
typedef struct {
    uint32_t index;
    uint32_t gen;
} tack_allocator;

// TACK_C_ALLOCATOR is c_allocator.
#define TACK_C_ALLOCATOR ((tack_allocator){0, 0})

// A Tack list, list[T], is a struct of its own per element type, which
// the program's tack.h defines, tack_list_int64 for list[int64]:
//
//     typedef struct {
//         T *ptr;               // the buffer, null while it is empty
//         int64_t len;          // the number of elements
//         int64_t cap;          // how many the buffer has room for
//         tack_allocator alloc; // what it grows and is freed through
//     } tack_list_T;
//
// The list belongs to Tack: C gets one only as a mut parameter, a pointer
// to the caller's list, which it may read, write elements of and append
// to during the call through the macros below, and never keeps, frees,
// shrinks, or gives another buffer or allocator.

// tack_list_grow_ returns a buffer with room for at least need elements of
// size elem and alignment align, holding the cap elements of ptr's, from
// the allocator a, and stores its capacity in *newcap; ptr may be null when
// cap is 0. ptr itself is returned when it has room already. It returns
// null, leaving ptr and *newcap as they were, when need is negative, the
// size of the buffer would not fit in a size_t or a ptrdiff_t, the
// alignment is more than the allocator gives, a is no allocator, or the
// allocator fails. TACK_LIST_APPEND and TACK_LIST_RESERVE call it, and
// TACK_LIST_APPEND_N through TACK_LIST_RESERVE; nothing else should.
void *tack_list_grow_(void *ptr, int64_t cap, int64_t need, size_t elem, size_t align, tack_allocator a,
                      int64_t *newcap);

// tack_list_free_ gives a list's buffer, ptr, of cap elements of size elem
// and alignment align, back to the allocator a it came from. A null ptr,
// an empty list's, is nothing to free. The generated source calls it when
// a list's owner's block ends; C must not.
void tack_list_free_(void *ptr, int64_t cap, size_t elem, size_t align, tack_allocator a);

// tack_c_allocator_live_ returns how many buffers c_allocator has given
// out and not had back, for tests that check every list is freed.
int64_t tack_c_allocator_live_(void);

// tack_c_allocator_fail_after_ makes c_allocator grant the next n requests
// for memory and refuse every one after them, or refuse none again when
// n is negative, for tests of what a failed allocation does.
void tack_c_allocator_fail_after_(int64_t n);

// TACK_LIST_LEN(l) is the number of elements of the list l, a
// tack_list_ struct, not a pointer to one.
#define TACK_LIST_LEN(l) ((l).len)

// TACK_LIST_AT(l, i) is element i of the list l, an lvalue, which C reads
// and writes as any other. Nothing checks i: C must keep it at least 0
// and below TACK_LIST_LEN(l), as it must every index it uses.
#define TACK_LIST_AT(l, i) ((l).ptr[i])

// TACK_LIST_APPEND(lp, v) appends v to the list lp points to, growing its
// buffer through its allocator when it is full, and is true; or, when the
// buffer cannot grow, false, the list left as it was. lp and v are each
// evaluated once, lp first, and v is converted to the element type as an
// assignment would, so clang checks it as one, and before the buffer can
// move, so v may read an element of the list. v is the macro's variadic
// argument, so a compound literal, whose braces hold commas, can be one:
// TACK_LIST_APPEND(l, (Point){1, 2}).
#define TACK_LIST_APPEND(lp, ...) __extension__ ({ \
    __typeof__(lp) tack_lp_ = (lp); \
    __typeof__(*tack_lp_->ptr) tack_v_ = (__VA_ARGS__); \
    bool tack_ok_ = true; \
    if (tack_lp_->len == tack_lp_->cap) { \
        int64_t tack_cap_ = tack_lp_->cap; \
        int64_t tack_need_ = tack_lp_->len < INT64_MAX ? tack_lp_->len + 1 : -1; \
        void *tack_p_ = tack_list_grow_(tack_lp_->ptr, tack_lp_->cap, tack_need_, \
            sizeof *tack_lp_->ptr, _Alignof(__typeof__(*tack_lp_->ptr)), tack_lp_->alloc, &tack_cap_); \
        if (tack_p_ == 0) { \
            tack_ok_ = false; \
        } else { \
            tack_lp_->ptr = tack_p_; \
            tack_lp_->cap = tack_cap_; \
        } \
    } \
    if (tack_ok_) { \
        tack_lp_->ptr[tack_lp_->len++] = tack_v_; \
    } \
    tack_ok_; \
})

// TACK_LIST_RESERVE(lp, n) makes room in the list lp points to for n
// more elements, an int64_t, growing its buffer through its allocator
// when it has less, so that its capacity is at least its length plus n,
// and is true; or, when n is negative, its length plus n overflows an
// int64_t, or the buffer cannot grow, false, the list left as it was. Its
// length and elements never change. The buffer grows as an append's
// does, to at least double its capacity, so making room for one element
// before each append keeps appending in amortized constant time. lp and n
// are each evaluated once, lp first.
#define TACK_LIST_RESERVE(lp, n) __extension__ ({ \
    __typeof__(lp) tack_rlp_ = (lp); \
    int64_t tack_rn_ = (n); \
    bool tack_rok_ = tack_rn_ >= 0 && tack_rn_ <= INT64_MAX - tack_rlp_->len; \
    if (tack_rok_ && tack_rlp_->len + tack_rn_ > tack_rlp_->cap) { \
        int64_t tack_rcap_ = tack_rlp_->cap; \
        void *tack_rp_ = tack_list_grow_(tack_rlp_->ptr, tack_rlp_->cap, tack_rlp_->len + tack_rn_, \
            sizeof *tack_rlp_->ptr, _Alignof(__typeof__(*tack_rlp_->ptr)), tack_rlp_->alloc, &tack_rcap_); \
        if (tack_rp_ == 0) { \
            tack_rok_ = false; \
        } else { \
            tack_rlp_->ptr = tack_rp_; \
            tack_rlp_->cap = tack_rcap_; \
        } \
    } \
    tack_rok_; \
})

// TACK_LIST_APPEND_N(lp, src, n) appends the n elements src points to, an
// int64_t count of a pointer to the list's element type, to the list lp
// points to, all or none: it makes room for all of them first, as
// TACK_LIST_RESERVE(lp, n) does, and is true; or, when it cannot, false,
// the list left as it was, its length, capacity and elements. n of 0
// appends nothing, allocates nothing and is true, and src may then be
// null. lp, src and n are each evaluated once, in that order. src must
// not point into the list's own buffer, which making room may move.
#define TACK_LIST_APPEND_N(lp, src, n) __extension__ ({ \
    __typeof__(lp) tack_alp_ = (lp); \
    const __typeof__(*tack_alp_->ptr) *tack_asrc_ = (src); \
    int64_t tack_an_ = (n); \
    bool tack_aok_ = TACK_LIST_RESERVE(tack_alp_, tack_an_); \
    if (tack_aok_) { \
        for (int64_t tack_ai_ = 0; tack_ai_ < tack_an_; tack_ai_++) { \
            tack_alp_->ptr[tack_alp_->len + tack_ai_] = tack_asrc_[tack_ai_]; \
        } \
        tack_alp_->len += tack_an_; \
    } \
    tack_aok_; \
})

// TACK_LIST_POP(lp) is the last element of the list lp points to, which
// it takes out of the list, leaving it one element shorter. The list
// must not be empty: C must check TACK_LIST_LEN first, as Tack proves it
// or checks it. lp is evaluated once. Nothing is copied, allocated or
// freed: an element that holds a list is its taker's from then on, and
// no longer the list's to free.
#define TACK_LIST_POP(lp) __extension__ ({ \
    __typeof__(lp) tack_plp_ = (lp); \
    tack_plp_->ptr[--tack_plp_->len]; \
})

// TACK_LIST_REMOVE(lp, i) is element i of the list lp points to, an
// int64_t, which it takes out of the list, moving every element after it
// down one place, so the rest keep their order and the list is one
// element shorter. i must be at least 0 and below the list's length: C
// must check it first, as Tack proves it or checks it. lp and i are each
// evaluated once, lp first. Like TACK_LIST_POP, it allocates and frees
// nothing, and the element taken is its taker's from then on.
#define TACK_LIST_REMOVE(lp, i) __extension__ ({ \
    __typeof__(lp) tack_xlp_ = (lp); \
    int64_t tack_xi_ = (i); \
    __typeof__(*tack_xlp_->ptr) tack_xv_ = tack_xlp_->ptr[tack_xi_]; \
    for (int64_t tack_xj_ = tack_xi_ + 1; tack_xj_ < tack_xlp_->len; tack_xj_++) { \
        tack_xlp_->ptr[tack_xj_ - 1] = tack_xlp_->ptr[tack_xj_]; \
    } \
    tack_xlp_->len--; \
    tack_xv_; \
})

#endif

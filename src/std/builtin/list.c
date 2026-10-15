#include <stdlib.h>

#include <tack/tack.h>

// The C of lists: the allocators a list grows and is freed through, and
// the functions std's tack.h declares for them. A list's buffer is only
// ever obtained, grown and given back here, so every unchecked step of a
// list's memory is in this file.
//
// An Allocator is a handle on an entry of allocators, the index of the
// entry and its generation. There is one entry today, c_allocator's,
// which lowers to malloc, realloc and free and counts the buffers it has
// given out and not had back, so tests can check that every list was
// freed. A handle whose index or generation is no entry's is no
// allocator: growing through one fails, and freeing through one does
// nothing, since the memory, if any, is not this allocator's to free.

// An allocator: its generation, which a handle must match, and how it
// grows a buffer and frees one. grow returns a buffer of bytes bytes
// holding the first old bytes of ptr, which it may move, or null, leaving
// ptr as it was.
typedef struct {
    uint32_t gen;
    size_t align;
    void *(*grow)(void *ptr, size_t old, size_t bytes);
    void (*free)(void *ptr, size_t bytes);
} tack_alloc_entry_;

// How many buffers c_allocator has given out and not had back, and how
// many more requests for memory it grants, or -1 for every one.
static int64_t c_live = 0;
static int64_t c_grants = -1;

// c_grow is c_allocator's grow: malloc for a buffer it has not given out,
// realloc for one it has.
static void *c_grow(void *ptr, size_t old, size_t bytes) {
    (void)old;
    if (c_grants == 0) {
        return 0;
    }
    if (c_grants > 0) {
        c_grants--;
    }
    if (ptr == 0) {
        void *p = malloc(bytes);
        if (p != 0) {
            c_live++;
        }
        return p;
    }
    return realloc(ptr, bytes);
}

// c_free is c_allocator's free.
static void c_free(void *ptr, size_t bytes) {
    (void)bytes;
    free(ptr);
    c_live--;
}

// The allocators, by index. malloc's memory is aligned for any type that
// has a fundamental alignment, max_align_t's.
static const tack_alloc_entry_ allocators[] = {
    {0, _Alignof(max_align_t), c_grow, c_free},
};

// entry returns a's allocator, or null when a is no allocator's handle.
static const tack_alloc_entry_ *entry(tack_allocator a) {
    if (a.index >= sizeof allocators / sizeof allocators[0] || allocators[a.index].gen != a.gen) {
        return 0;
    }
    return &allocators[a.index];
}

// fits returns the size in bytes of n elements of size elem in *bytes,
// and whether it is a size an allocation can have: it fits a size_t and
// a ptrdiff_t, so a pointer can move across all of it. A size of 0 is
// asked for as 1, since a buffer must be a pointer that is not null.
static bool fits(int64_t n, size_t elem, size_t *bytes) {
    if (n < 0) {
        return false;
    }
    if (elem == 0) {
        *bytes = 1;
        return true;
    }
    uint64_t un = (uint64_t)n;
    uint64_t limit = PTRDIFF_MAX < SIZE_MAX ? (uint64_t)PTRDIFF_MAX : (uint64_t)SIZE_MAX;
    if (un > limit / elem) {
        return false;
    }
    *bytes = (size_t)(un * elem);
    if (*bytes == 0) {
        *bytes = 1;
    }
    return true;
}

void *tack_list_grow_(void *ptr, int64_t cap, int64_t need, size_t elem, size_t align, tack_allocator a,
                      int64_t *newcap) {
    const tack_alloc_entry_ *al = entry(a);
    if (al == 0 || need < 0 || cap < 0 || align > al->align) {
        return 0;
    }
    if (need <= cap && ptr != 0) {
        return ptr;
    }
    // Double the capacity, from 4, so n appends move the buffer about
    // log2(n) times, but never past what fits; then at least need.
    int64_t want = cap < 4 ? 4 : cap;
    if (want <= INT64_MAX / 2 && cap >= 4) {
        want = cap * 2;
    }
    if (want < need) {
        want = need;
    }
    size_t bytes, old = 0;
    if (!fits(want, elem, &bytes)) {
        want = need;
        if (!fits(want, elem, &bytes)) {
            return 0;
        }
    }
    if (ptr != 0 && !fits(cap, elem, &old)) {
        return 0;
    }
    void *p = al->grow(ptr, old, bytes);
    if (p == 0) {
        return 0;
    }
    *newcap = want;
    return p;
}

void tack_list_free_(void *ptr, int64_t cap, size_t elem, size_t align, tack_allocator a) {
    (void)align;
    const tack_alloc_entry_ *al = entry(a);
    size_t bytes = 0;
    if (ptr == 0 || al == 0) {
        return;
    }
    if (!fits(cap, elem, &bytes)) {
        bytes = 0;
    }
    al->free(ptr, bytes);
}

int64_t tack_c_allocator_live_(void) {
    return c_live;
}

void tack_c_allocator_fail_after_(int64_t n) {
    c_grants = n < 0 ? -1 : n;
}

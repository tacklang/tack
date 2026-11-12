#include <stdlib.h>

#include <tack/tack.h>

// The C of lists: the allocators a list grows and is freed through, and
// the functions std's tack.h declares for them, and the limit on an
// arena's length, whose elements are a list's. A list's buffer is only
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
//
// Every allocator keeps track of every buffer it has given out and not
// had back, and can free them all at once: exit calls
// tack_allocators_release_all_ before it ends the process, so nothing a
// program allocated outlives it, whatever still held it.

// An allocator: its generation, which a handle must match, how it grows a
// buffer and frees one, and how it frees every buffer it still holds.
// grow returns a buffer of bytes bytes holding the first old bytes of
// ptr, which it may move, or null, leaving ptr as it was. release_all
// frees every buffer grow has given out and free has not had back, and
// must allocate nothing, since it runs when an allocator may be refusing
// every request.
typedef struct {
    uint32_t gen;
    size_t align;
    void *(*grow)(void *ptr, size_t old, size_t bytes);
    void (*free)(void *ptr, size_t bytes);
    void (*release_all)(void);
} tack_alloc_entry_;

// A c_header sits in front of every buffer c_allocator gives out, which
// starts just past it, and links the buffer into c_held, the list of every
// buffer c_allocator holds. It is a union with max_align_t so its size is
// a multiple of max_align_t's alignment: malloc's memory is aligned for
// max_align_t, so the buffer past the header is too, as it would be
// without one.
typedef union c_header {
    struct {
        union c_header *prev;
        union c_header *next;
    } link;
    max_align_t align_;
} c_header;

// c_held is the head of the circular, doubly linked list of the headers
// of every buffer c_allocator holds; it links to itself while there are
// none.
static c_header c_held = {.link = {&c_held, &c_held}};

// How many buffers c_allocator has given out and not had back, and how
// many more requests for memory it grants, or -1 for every one.
static int64_t c_live = 0;
static int64_t c_grants = -1;

// c_link puts h at the front of c_held.
static void c_link(c_header *h) {
    h->link.prev = &c_held;
    h->link.next = c_held.link.next;
    c_held.link.next->link.prev = h;
    c_held.link.next = h;
}

// c_unlink takes h out of c_held.
static void c_unlink(c_header *h) {
    h->link.prev->link.next = h->link.next;
    h->link.next->link.prev = h->link.prev;
}

// c_grow is c_allocator's grow: malloc for a buffer it has not given out,
// realloc for one it has, each with room for the header in front.
static void *c_grow(void *ptr, size_t old, size_t bytes) {
    (void)old;
    if (c_grants == 0) {
        return 0;
    }
    if (c_grants > 0) {
        c_grants--;
    }
    // fits keeps bytes within PTRDIFF_MAX, but the header must fit on top.
    uint64_t limit = PTRDIFF_MAX < SIZE_MAX ? (uint64_t)PTRDIFF_MAX : (uint64_t)SIZE_MAX;
    if (bytes > limit - sizeof(c_header)) {
        return 0;
    }
    if (ptr == 0) {
        c_header *h = malloc(sizeof(c_header) + bytes);
        if (h == 0) {
            return 0;
        }
        c_link(h);
        c_live++;
        return h + 1;
    }
    // realloc copies the links with the header, so after a move the
    // neighbors, which still point at the old header, are pointed at the
    // new one; nothing reads the old header after realloc. On failure
    // the old buffer, still linked, is left as it was.
    c_header *h = realloc((c_header *)ptr - 1, sizeof(c_header) + bytes);
    if (h == 0) {
        return 0;
    }
    h->link.prev->link.next = h;
    h->link.next->link.prev = h;
    return h + 1;
}

// c_free is c_allocator's free.
static void c_free(void *ptr, size_t bytes) {
    (void)bytes;
    c_header *h = (c_header *)ptr - 1;
    c_unlink(h);
    free(h);
    c_live--;
}

// c_release_all is c_allocator's release_all: it frees every buffer in
// c_held and leaves c_held empty. Nothing frees one of those buffers
// again: only exit calls it, and exit returns to no list that held one.
static void c_release_all(void) {
    c_header *h = c_held.link.next;
    while (h != &c_held) {
        c_header *next = h->link.next;
        free(h);
        c_live--;
        h = next;
    }
    c_held.link.prev = &c_held;
    c_held.link.next = &c_held;
}

// The allocators, by index. c_allocator's buffers are aligned for any
// type that has a fundamental alignment, max_align_t's: malloc's are,
// and the header in front of each is a multiple of that alignment.
static const tack_alloc_entry_ allocators[] = {
    {0, _Alignof(max_align_t), c_grow, c_free, c_release_all},
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

void tack_allocators_release_all_(void) {
    for (size_t i = 0; i < sizeof allocators / sizeof allocators[0]; i++) {
        allocators[i].release_all();
    }
}

int64_t tack_c_allocator_live_(void) {
    return c_live;
}

void tack_c_allocator_fail_after_(int64_t n) {
    c_grants = n < 0 ? -1 : n;
}

// arenaMax is the most elements an arena may hold, its sentinel among
// them: 2^32 - 1, so every id, below it, fits a uint32_t, or less while a
// test has lowered it (see tack_arena_limit_).
#define ARENA_MAX 4294967295u
static uint64_t arenaMax = ARENA_MAX;

bool tack_arena_room_(int64_t len, int64_t n) {
    if (len < 0 || n < 0 || (uint64_t)len > arenaMax) {
        return false;
    }
    return (uint64_t)n <= arenaMax - (uint64_t)len;
}

void tack_arena_limit_(int64_t n) {
    arenaMax = n < 0 || (uint64_t)n > ARENA_MAX ? ARENA_MAX : (uint64_t)n;
}

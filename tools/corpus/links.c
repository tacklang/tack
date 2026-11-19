// The symbolic links of tools/corpus: telling a link from what it points
// at, which std/os's lstat does not report, so that the runner's walks
// of a run's trees never follow one (see walk_entries in runexec.tck).
#include <errno.h>
#include <stdint.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include "tack.h"

// corpus_link_target appends to out the target of the symbolic link at
// path and returns 1, or returns 0 when path is no symbolic link, and
// -1 when it cannot be looked at or read, path holds a 0 byte or is too
// long, or there is no room.
int64_t corpus_link_target(tack_view_uint8 path, tack_list_uint8 *out) {
    char p[4096];
    if (path.len >= (int64_t)sizeof p || memchr(path.ptr, 0, (size_t)path.len) != NULL) {
        return -1;
    }
    memcpy(p, path.ptr, (size_t)path.len);
    p[path.len] = 0;
    struct stat st;
    if (lstat(p, &st) != 0) {
        return -1;
    }
    if (!S_ISLNK(st.st_mode)) {
        return 0;
    }
    char t[4096];
    ssize_t n = readlink(p, t, sizeof t);
    if (n < 0 || n >= (ssize_t)sizeof t) {
        return -1;
    }
    if (!TACK_LIST_APPEND_N(out, (const uint8_t *)t, (int64_t)n)) {
        return -1;
    }
    return 1;
}

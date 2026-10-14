// geo's extern func, over geo's types, which tack.h names geo_Point and
// geo_Rect.
#include "tack.h"

int64_t area(geo_Rect r) {
    return (r.max.x - r.min.x) * (r.max.y - r.min.y);
}

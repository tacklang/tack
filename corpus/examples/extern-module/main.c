// main's extern func. tack.h declares main's Point as Point and geo's as
// geo_Point.
#include "tack.h"

Point lift(geo_Point p, tack_string label) {
    return (Point){.x = p.x, .y = p.y, .label = label};
}

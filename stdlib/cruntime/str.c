// DexLang str module - Type conversion utilities
// Converts between string and numeric/boolean types.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* These build the DexString directly rather than returning a malloc'd char*
 * for the caller to copy. The old shape cost a strdup, then a strlen, then a
 * second allocation, then a free -- four operations and two allocations to
 * produce one string. snprintf already reports the length, so there is nothing
 * to measure afterwards. On `"k" + str.fromLong(i)` in a loop this is a third
 * of the work. They are marked RawReturn in stdlib/str.go, which is what tells
 * codegen the DexString is already built. */

static DexString* dex_str_fromInt(int n) {
    char buf[32];
    int len = snprintf(buf, sizeof(buf), "%d", n);
    if (len < 0) len = 0;
    if ((size_t)len >= sizeof(buf)) len = (int)sizeof(buf) - 1;
    return dex_string_new(buf, (size_t)len);
}

static DexString* dex_str_fromLong(long n) {
    char buf[32];
    int len = snprintf(buf, sizeof(buf), "%ld", n);
    if (len < 0) len = 0;
    if ((size_t)len >= sizeof(buf)) len = (int)sizeof(buf) - 1;
    return dex_string_new(buf, (size_t)len);
}

static DexString* dex_str_fromDouble(double d) {
    char buf[64];
    int len = snprintf(buf, sizeof(buf), "%g", d);
    if (len < 0) len = 0;
    if ((size_t)len >= sizeof(buf)) len = (int)sizeof(buf) - 1;
    return dex_string_new(buf, (size_t)len);
}

static DexString* dex_str_fromBool(_Bool b) {
    return b ? dex_string_new("true", 4) : dex_string_new("false", 5);
}

static int dex_str_toInt(const char* s) {
    return dex_parse_int(s);
}

static long dex_str_toLong(const char* s) {
    return dex_parse_long(s);
}

static double dex_str_toDouble(const char* s) {
    return dex_parse_double(s);
}

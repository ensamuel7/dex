// DexLang str module - Type conversion utilities
// Converts between string and numeric/boolean types.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static char* dex_str_fromInt(int n) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%d", n);
    return strdup(buf);
}

static char* dex_str_fromLong(long n) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%ld", n);
    return strdup(buf);
}

static char* dex_str_fromDouble(double d) {
    char buf[64];
    snprintf(buf, sizeof(buf), "%g", d);
    return strdup(buf);
}

static char* dex_str_fromBool(_Bool b) {
    return strdup(b ? "true" : "false");
}

static int dex_str_toInt(const char* s) {
    return atoi(s);
}

static long dex_str_toLong(const char* s) {
    return atol(s);
}

static double dex_str_toDouble(const char* s) {
    return atof(s);
}

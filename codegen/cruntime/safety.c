#include <limits.h>

static void dex_panic(const char* msg) {
    fprintf(stderr, "runtime error: %s\n", msg);
    exit(1);
}

static inline int dex_checked_index(long index, long len) {
    if (index < 0 || index >= len) {
        fprintf(stderr, "runtime error: index %ld out of bounds (length %ld)\n", index, len);
        exit(1);
    }
    return (int)index;
}

static inline int dex_check_nonzero_int(int v) {
    if (v == 0) { dex_panic("division by zero"); }
    return v;
}

static inline long dex_check_nonzero_long(long v) {
    if (v == 0) { dex_panic("division by zero"); }
    return v;
}

static inline double dex_check_nonzero_double(double v) {
    if (v == 0.0) { dex_panic("division by zero"); }
    return v;
}

static inline int dex_div_int(int a, int b) {
    if (b == 0) { dex_panic("division by zero"); }
    return b == -1 ? -a : a / b;
}

static inline int dex_mod_int(int a, int b) {
    if (b == 0) { dex_panic("division by zero"); }
    return b == -1 ? 0 : a % b;
}

static inline long dex_div_long(long a, long b) {
    if (b == 0) { dex_panic("division by zero"); }
    return b == -1 ? -a : a / b;
}

static inline long dex_mod_long(long a, long b) {
    if (b == 0) { dex_panic("division by zero"); }
    return b == -1 ? 0 : a % b;
}

static inline int dex_double_to_int(double v) {
    if (v != v) { return 0; }
    if (v >= 2147483647.0) { return INT_MAX; }
    if (v <= -2147483648.0) { return INT_MIN; }
    return (int)v;
}

static inline long dex_double_to_long(double v) {
    if (v != v) { return 0; }
    if (v >= 9223372036854775807.0) { return LONG_MAX; }
    if (v <= -9223372036854775808.0) { return LONG_MIN; }
    return (long)v;
}

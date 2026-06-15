
static void dex_panic(const char* msg) {
    fprintf(stderr, "runtime error: %s\n", msg);
    exit(1);
}

static inline void dex_bounds_check(int index, int len) {
    if (index < 0 || index >= len) {
        fprintf(stderr, "runtime error: index %d out of bounds (length %d)\n", index, len);
        exit(1);
    }
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

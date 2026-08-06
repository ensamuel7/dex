// DexLang Map Runtime
// Hash map implementation using open addressing with linear probing.
// Uses DexObjHeader for reference counting.
// Capacity is always a power of 2 for fast bitwise masking.

// FNV-1a hash for strings
static unsigned int dex_hash_str(const char* data, int len) {
    unsigned int h = 2166136261u;
    for (int i = 0; i < len; i++) {
        h ^= (unsigned char)data[i];
        h *= 16777619u;
    }
    return h;
}

// Hash for integers (splitmix32 finalizer — good avalanche properties)
static unsigned int dex_hash_int(int key) {
    unsigned int k = (unsigned int)key;
    k ^= k >> 16;
    k *= 0x45d9f3bu;
    k ^= k >> 16;
    return k;
}

// Macro to define a map type for a given key/value combination.
// KEY_TYPE: C type of the key
// VAL_TYPE: C type of the value
// SUFFIX: unique suffix for function names (e.g. str_int)
// HASH_KEY(k): expression to hash a key
// KEY_EQ(a,b): expression to compare keys
// KEY_RETAIN(k): expression to retain a key (noop for value types)
// KEY_RELEASE(k): expression to release a key (noop for value types)
// VAL_RETAIN(v): expression to retain a value (noop for value types)
// VAL_RELEASE(v): expression to release a value (noop for value types)
// KEY_COPY(k): expression to copy/retain a key for storage
// VAL_COPY(v): expression to copy/retain a value for storage

#define DEX_MAP_DEFINE(KEY_TYPE, VAL_TYPE, SUFFIX, HASH_KEY, KEY_EQ, KEY_RETAIN, KEY_RELEASE, VAL_RETAIN, VAL_RELEASE, KEY_COPY, VAL_COPY) \
\
typedef struct { \
    KEY_TYPE key; \
    VAL_TYPE value; \
    _Bool occupied; \
} DexMapEntry_##SUFFIX; \
\
typedef struct { \
    DexObjHeader header; \
    DexMapEntry_##SUFFIX* entries; \
    int cap; \
    int len; \
} DexMap_##SUFFIX; \
\
static void dex_map_##SUFFIX##_destroy(void* ptr) { \
    DexMap_##SUFFIX* m = (DexMap_##SUFFIX*)ptr; \
    for (int i = 0; i < m->cap; i++) { \
        if (m->entries[i].occupied) { \
            KEY_RELEASE(m->entries[i].key); \
            VAL_RELEASE(m->entries[i].value); \
        } \
    } \
    free(m->entries); \
} \
\
static DexMap_##SUFFIX* dex_map_##SUFFIX##_new(void) { \
    DexMap_##SUFFIX* m = (DexMap_##SUFFIX*)dex_obj_alloc(sizeof(DexMap_##SUFFIX), dex_map_##SUFFIX##_destroy); \
    m->cap = 16; \
    m->len = 0; \
    m->entries = (DexMapEntry_##SUFFIX*)calloc((size_t)m->cap, sizeof(DexMapEntry_##SUFFIX)); \
    if (!m->entries) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); } \
    return m; \
} \
\
static void dex_map_##SUFFIX##_resize(DexMap_##SUFFIX* m, int newCap) { \
    int oldCap = m->cap; \
    DexMapEntry_##SUFFIX* oldEntries = m->entries; \
    m->cap = newCap; \
    m->entries = (DexMapEntry_##SUFFIX*)calloc((size_t)m->cap, sizeof(DexMapEntry_##SUFFIX)); \
    if (!m->entries) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); } \
    int mask = m->cap - 1; \
    m->len = 0; \
    for (int i = 0; i < oldCap; i++) { \
        if (oldEntries[i].occupied) { \
            unsigned int h = HASH_KEY(oldEntries[i].key) & (unsigned int)mask; \
            while (m->entries[h].occupied) { \
                h = (h + 1) & (unsigned int)mask; \
            } \
            m->entries[h].key = oldEntries[i].key; \
            m->entries[h].value = oldEntries[i].value; \
            m->entries[h].occupied = 1; \
            m->len++; \
        } \
    } \
    free(oldEntries); \
} \
\
static void dex_map_##SUFFIX##_set(DexMap_##SUFFIX* m, KEY_TYPE key, VAL_TYPE value) { \
    if (m->len * 4 >= m->cap * 3) { \
        dex_map_##SUFFIX##_resize(m, m->cap * 2); \
    } \
    int mask = m->cap - 1; \
    unsigned int h = HASH_KEY(key) & (unsigned int)mask; \
    while (m->entries[h].occupied) { \
        if (KEY_EQ(m->entries[h].key, key)) { \
            VAL_RELEASE(m->entries[h].value); \
            m->entries[h].value = VAL_COPY(value); \
            return; \
        } \
        h = (h + 1) & (unsigned int)mask; \
    } \
    m->entries[h].key = KEY_COPY(key); \
    m->entries[h].value = VAL_COPY(value); \
    m->entries[h].occupied = 1; \
    m->len++; \
} \
\
static VAL_TYPE dex_map_##SUFFIX##_get(DexMap_##SUFFIX* m, KEY_TYPE key) { \
    int mask = m->cap - 1; \
    unsigned int h = HASH_KEY(key) & (unsigned int)mask; \
    while (m->entries[h].occupied) { \
        if (KEY_EQ(m->entries[h].key, key)) { \
            VAL_TYPE v = m->entries[h].value; \
            VAL_RETAIN(v); \
            return v; \
        } \
        h = (h + 1) & (unsigned int)mask; \
    } \
    VAL_TYPE zero; \
    memset(&zero, 0, sizeof(zero)); \
    return zero; \
} \
\
static _Bool dex_map_##SUFFIX##_has(DexMap_##SUFFIX* m, KEY_TYPE key) { \
    int mask = m->cap - 1; \
    unsigned int h = HASH_KEY(key) & (unsigned int)mask; \
    while (m->entries[h].occupied) { \
        if (KEY_EQ(m->entries[h].key, key)) { \
            return 1; \
        } \
        h = (h + 1) & (unsigned int)mask; \
    } \
    return 0; \
} \
\
static void dex_map_##SUFFIX##_remove(DexMap_##SUFFIX* m, KEY_TYPE key) { \
    int mask = m->cap - 1; \
    unsigned int h = HASH_KEY(key) & (unsigned int)mask; \
    while (m->entries[h].occupied) { \
        if (KEY_EQ(m->entries[h].key, key)) { \
            KEY_RELEASE(m->entries[h].key); \
            VAL_RELEASE(m->entries[h].value); \
            m->entries[h].occupied = 0; \
            m->len--; \
            /* Backward-shift deletion: re-insert displaced entries */ \
            unsigned int j = (h + 1) & (unsigned int)mask; \
            while (m->entries[j].occupied) { \
                unsigned int natural = HASH_KEY(m->entries[j].key) & (unsigned int)mask; \
                /* Check if entry j is displaced past the gap at h */ \
                _Bool displaced = (j > h) ? (natural <= h || natural > j) : (natural <= h && natural > j); \
                if (displaced) { \
                    m->entries[h] = m->entries[j]; \
                    m->entries[j].occupied = 0; \
                    h = j; \
                } \
                j = (j + 1) & (unsigned int)mask; \
            } \
            return; \
        } \
        h = (h + 1) & (unsigned int)mask; \
    } \
} \
\
static int dex_map_##SUFFIX##_len(DexMap_##SUFFIX* m) { \
    return m->len; \
} \
\
static void dex_map_##SUFFIX##_clear(DexMap_##SUFFIX* m) { \
    for (int i = 0; i < m->cap; i++) { \
        if (m->entries[i].occupied) { \
            KEY_RELEASE(m->entries[i].key); \
            VAL_RELEASE(m->entries[i].value); \
            m->entries[i].occupied = 0; \
        } \
    } \
    m->len = 0; \
}

// Helper macros for retain/release of different types
#define DEX_NOOP(x) (void)0
#define DEX_RETAIN_STR(x) dex_retain(x)
#define DEX_RELEASE_STR(x) dex_release(x)
#define DEX_COPY_STR(x) (dex_retain(x), x)
#define DEX_COPY_VAL(x) (x)

// String key helpers
#define DEX_HASH_STR_KEY(k) dex_hash_str((k)->data, (int)(k)->len)
#define DEX_STR_KEY_EQ(a,b) (((a)->len == (b)->len) && memcmp((a)->data, (b)->data, (a)->len) == 0)

// Int key helpers
#define DEX_HASH_INT_KEY(k) dex_hash_int(k)
#define DEX_INT_KEY_EQ(a,b) ((a) == (b))

// Keys function macro: returns an array of keys
#define DEX_MAP_KEYS_DEFINE(KEY_TYPE, KEY_ARRAY_TYPE, SUFFIX, KEY_PUSH_FN, KEY_ARRAY_NEW_FN) \
static KEY_ARRAY_TYPE* dex_map_##SUFFIX##_keys(DexMap_##SUFFIX* m) { \
    KEY_ARRAY_TYPE* arr = KEY_ARRAY_NEW_FN(); \
    for (int i = 0; i < m->cap; i++) { \
        if (m->entries[i].occupied) { \
            KEY_PUSH_FN(arr, m->entries[i].key); \
        } \
    } \
    return arr; \
}

// Values function macro: returns an array of values
#define DEX_MAP_VALUES_DEFINE(VAL_TYPE, VAL_ARRAY_TYPE, SUFFIX, VAL_PUSH_FN, VAL_ARRAY_NEW_FN) \
static VAL_ARRAY_TYPE* dex_map_##SUFFIX##_values(DexMap_##SUFFIX* m) { \
    VAL_ARRAY_TYPE* arr = VAL_ARRAY_NEW_FN(); \
    for (int i = 0; i < m->cap; i++) { \
        if (m->entries[i].occupied) { \
            VAL_PUSH_FN(arr, m->entries[i].value); \
        } \
    } \
    return arr; \
}

// Instantiate string-keyed maps
DEX_MAP_DEFINE(DexString*, int, str_int, DEX_HASH_STR_KEY, DEX_STR_KEY_EQ, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_NOOP, DEX_NOOP, DEX_COPY_STR, DEX_COPY_VAL)
DEX_MAP_DEFINE(DexString*, _Bool, str_bool, DEX_HASH_STR_KEY, DEX_STR_KEY_EQ, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_NOOP, DEX_NOOP, DEX_COPY_STR, DEX_COPY_VAL)
DEX_MAP_DEFINE(DexString*, DexString*, str_str, DEX_HASH_STR_KEY, DEX_STR_KEY_EQ, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_COPY_STR, DEX_COPY_STR)
DEX_MAP_DEFINE(DexString*, long, str_long, DEX_HASH_STR_KEY, DEX_STR_KEY_EQ, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_NOOP, DEX_NOOP, DEX_COPY_STR, DEX_COPY_VAL)
DEX_MAP_DEFINE(DexString*, double, str_double, DEX_HASH_STR_KEY, DEX_STR_KEY_EQ, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_NOOP, DEX_NOOP, DEX_COPY_STR, DEX_COPY_VAL)
DEX_MAP_DEFINE(DexString*, unsigned char, str_char, DEX_HASH_STR_KEY, DEX_STR_KEY_EQ, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_NOOP, DEX_NOOP, DEX_COPY_STR, DEX_COPY_VAL)

// Instantiate int-keyed maps
DEX_MAP_DEFINE(int, int, int_int, DEX_HASH_INT_KEY, DEX_INT_KEY_EQ, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_COPY_VAL, DEX_COPY_VAL)
DEX_MAP_DEFINE(int, _Bool, int_bool, DEX_HASH_INT_KEY, DEX_INT_KEY_EQ, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_COPY_VAL, DEX_COPY_VAL)
DEX_MAP_DEFINE(int, DexString*, int_str, DEX_HASH_INT_KEY, DEX_INT_KEY_EQ, DEX_NOOP, DEX_NOOP, DEX_RETAIN_STR, DEX_RELEASE_STR, DEX_COPY_VAL, DEX_COPY_STR)
DEX_MAP_DEFINE(int, long, int_long, DEX_HASH_INT_KEY, DEX_INT_KEY_EQ, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_COPY_VAL, DEX_COPY_VAL)
DEX_MAP_DEFINE(int, double, int_double, DEX_HASH_INT_KEY, DEX_INT_KEY_EQ, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_COPY_VAL, DEX_COPY_VAL)
DEX_MAP_DEFINE(int, unsigned char, int_char, DEX_HASH_INT_KEY, DEX_INT_KEY_EQ, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_NOOP, DEX_COPY_VAL, DEX_COPY_VAL)

// Keys functions for string-keyed maps
DEX_MAP_KEYS_DEFINE(DexString*, DexArrayString, str_int, dex_array_string_push, dex_array_string_new)
DEX_MAP_KEYS_DEFINE(DexString*, DexArrayString, str_bool, dex_array_string_push, dex_array_string_new)
DEX_MAP_KEYS_DEFINE(DexString*, DexArrayString, str_str, dex_array_string_push, dex_array_string_new)
DEX_MAP_KEYS_DEFINE(DexString*, DexArrayString, str_long, dex_array_string_push, dex_array_string_new)
DEX_MAP_KEYS_DEFINE(DexString*, DexArrayString, str_double, dex_array_string_push, dex_array_string_new)
DEX_MAP_KEYS_DEFINE(DexString*, DexArrayString, str_char, dex_array_string_push, dex_array_string_new)

// Keys functions for int-keyed maps
DEX_MAP_KEYS_DEFINE(int, DexArrayInt, int_int, dex_array_int_push, dex_array_int_new)
DEX_MAP_KEYS_DEFINE(int, DexArrayInt, int_bool, dex_array_int_push, dex_array_int_new)
DEX_MAP_KEYS_DEFINE(int, DexArrayInt, int_str, dex_array_int_push, dex_array_int_new)
DEX_MAP_KEYS_DEFINE(int, DexArrayInt, int_long, dex_array_int_push, dex_array_int_new)
DEX_MAP_KEYS_DEFINE(int, DexArrayInt, int_double, dex_array_int_push, dex_array_int_new)
DEX_MAP_KEYS_DEFINE(int, DexArrayInt, int_char, dex_array_int_push, dex_array_int_new)

// Values functions for all map types
DEX_MAP_VALUES_DEFINE(int, DexArrayInt, str_int, dex_array_int_push, dex_array_int_new)
DEX_MAP_VALUES_DEFINE(_Bool, DexArrayBool, str_bool, dex_array_bool_push, dex_array_bool_new)
DEX_MAP_VALUES_DEFINE(DexString*, DexArrayString, str_str, dex_array_string_push, dex_array_string_new)
DEX_MAP_VALUES_DEFINE(long, DexArrayLong, str_long, dex_array_long_push, dex_array_long_new)
DEX_MAP_VALUES_DEFINE(double, DexArrayDouble, str_double, dex_array_double_push, dex_array_double_new)
DEX_MAP_VALUES_DEFINE(unsigned char, DexArrayChar, str_char, dex_array_char_push, dex_array_char_new)

DEX_MAP_VALUES_DEFINE(int, DexArrayInt, int_int, dex_array_int_push, dex_array_int_new)
DEX_MAP_VALUES_DEFINE(_Bool, DexArrayBool, int_bool, dex_array_bool_push, dex_array_bool_new)
DEX_MAP_VALUES_DEFINE(DexString*, DexArrayString, int_str, dex_array_string_push, dex_array_string_new)
DEX_MAP_VALUES_DEFINE(long, DexArrayLong, int_long, dex_array_long_push, dex_array_long_new)
DEX_MAP_VALUES_DEFINE(double, DexArrayDouble, int_double, dex_array_double_push, dex_array_double_new)
DEX_MAP_VALUES_DEFINE(unsigned char, DexArrayChar, int_char, dex_array_char_push, dex_array_char_new)

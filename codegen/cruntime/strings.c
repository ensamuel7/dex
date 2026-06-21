// DexLang String Runtime
// DexString is a refcounted, length-prefixed string.

typedef struct {
    DexObjHeader hdr;
    size_t len;
    char data[];
} DexString;

static void dex_string_destroy(void* ptr) {
    (void)ptr; // data[] is part of the struct, freed with it
}

static DexString* dex_string_new(const char* s, size_t len) {
    DexString* ds = (DexString*)dex_obj_alloc(sizeof(DexString) + len + 1, dex_string_destroy);
    ds->len = len;
    memcpy(ds->data, s, len);
    ds->data[len] = '\0';
    return ds;
}

static DexString* dex_string_from_lit(const char* s) {
    return dex_string_new(s, strlen(s));
}

static DexString* dex_string_from_cstr(const char* s) {
    if (!s) return dex_string_new("", 0);
    DexString* ds = dex_string_new(s, strlen(s));
    free((void*)s);
    return ds;
}

static DexString* dex_string_empty(void) {
    return dex_string_new("", 0);
}

static DexString* dex_str_concat(DexString* a, DexString* b) {
    size_t newlen = a->len + b->len;
    DexString* ds = (DexString*)dex_obj_alloc(sizeof(DexString) + newlen + 1, dex_string_destroy);
    ds->len = newlen;
    memcpy(ds->data, a->data, a->len);
    memcpy(ds->data + a->len, b->data, b->len);
    ds->data[newlen] = '\0';
    return ds;
}

#include "runtime.h"
#include "../stdlib/cruntime/json.c"
#include "../stdlib/cruntime/json_value.c"

typedef struct {
    int id;
    _Bool ok;
    long big;
    double ratio;
    DexString* name;
} FuzzInner;

typedef struct {
    int x;
    DexString* label;
    FuzzInner inner;
} FuzzOuter;

static DexStructFieldDesc fuzz_inner_fields[] = {
    {"id", offsetof(FuzzInner, id), 0, 0, NULL},
    {"ok", offsetof(FuzzInner, ok), 1, 0, NULL},
    {"big", offsetof(FuzzInner, big), 3, 0, NULL},
    {"ratio", offsetof(FuzzInner, ratio), 4, 0, NULL},
    {"name", offsetof(FuzzInner, name), 2, 0, NULL},
};

static DexStructFieldDesc fuzz_outer_fields[] = {
    {"x", offsetof(FuzzOuter, x), 0, 0, NULL},
    {"label", offsetof(FuzzOuter, label), 2, 0, NULL},
    {"inner", offsetof(FuzzOuter, inner), 5, 5, fuzz_inner_fields},
};

static void fuzz_outer_release(FuzzOuter* o) {
    dex_release(o->label);
    dex_release(o->inner.name);
}

static DexString* fuzz_reencode(DexString* text) {
    DexJsonValue* v = dex_jv_parse_str(text);
    DexString* out = dex_jv_encode(v);
    dex_release(v);
    return out;
}

static void fuzz_check_roundtrip(DexJsonValue* v) {
    DexString* once = dex_jv_encode(v);
    DexString* twice = fuzz_reencode(once);
    DexString* thrice = fuzz_reencode(twice);
    if (twice->len != thrice->len || memcmp(twice->data, thrice->data, twice->len) != 0) {
        fprintf(stderr, "json round trip unstable:\n  %s\n  %s\n  %s\n", once->data, twice->data, thrice->data);
        abort();
    }
    dex_release(thrice);
    dex_release(twice);
    dex_release(once);
}

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    char* text = dex_fuzz_cstr(data, size);

    DexJsonValue* v = dex_jv_parse(text);
    if (!dex_jv_is_null(v)) fuzz_check_roundtrip(v);
    int n = dex_jv_len(v);
    for (int i = 0; i < n && i < 4; i++) {
        DexJsonValue* child = dex_jv_index(v, i);
        dex_release(child);
    }
    dex_release(v);

    (void)dex_json_valid(text);

    FuzzOuter strict = {0};
    if (dex_json_decode_struct_checked(text, &strict, 3, fuzz_outer_fields)) fuzz_outer_release(&strict);

    FuzzOuter lenient = {0};
    dex_json_decode_struct(text, &lenient, 3, fuzz_outer_fields);
    fuzz_outer_release(&lenient);

    if (dex_json_is_array(text)) {
        int len = dex_json_array_len(text);
        for (int i = 0; i < len && i < 8; i++) {
            const char* raw = dex_json_array_get_raw(text, i);
            FuzzOuter el = {0};
            if (dex_json_decode_struct_checked(raw, &el, 3, fuzz_outer_fields)) fuzz_outer_release(&el);
            free((void*)raw);
        }
    }

    int len = dex_json_array_len(text);
    for (int i = 0; i < len && i < 8; i++) {
        const char* raw = dex_json_array_get_raw(text, i);
        FuzzOuter el = {0};
        dex_json_decode_struct(raw, &el, 3, fuzz_outer_fields);
        fuzz_outer_release(&el);
        free((void*)raw);
        const char* s = dex_json_array_get(text, i);
        free((void*)s);
    }

    free((void*)dex_json_get(text, "label"));
    free(text);
    return 0;
}

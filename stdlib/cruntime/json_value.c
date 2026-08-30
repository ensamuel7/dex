// DexLang json.Value Runtime
//
// A refcounted JSON document tree. This is the type that lets Dex code *say* a
// JSON value directly — `let frame: json.Value = [4, id, code, desc, {}]` —
// instead of assembling wire text by hand, which is what forced quote escaping
// and the sprawl of set/get/arrayPush helpers this replaces.
//
// Arrays and objects share one child vector. An object additionally carries a
// parallel key vector, so the two cases differ only by whether `keys` is set,
// and insertion order is preserved for both.

typedef enum {
    DEX_JV_NULL = 0,
    DEX_JV_BOOL,
    DEX_JV_NUM,
    DEX_JV_STR,
    DEX_JV_ARR,
    DEX_JV_OBJ
} DexJsonKind;

// The typedef is emitted ahead of the user struct definitions, so that a struct
// may hold a json.Value field; only the body is defined here.
struct DexJsonValue {
    DexObjHeader hdr;
    int kind;
    _Bool b;
    double num;
    // Numbers arrive as either an integer or a double. Remembering which lets
    // `1` re-encode as `1` rather than `1.0`, so a decode/encode round trip does
    // not quietly rewrite a protocol's integers.
    _Bool is_int;
    long inum;
    DexString* str;              // DEX_JV_STR
    struct DexJsonValue** items; // DEX_JV_ARR and DEX_JV_OBJ
    DexString** keys;            // DEX_JV_OBJ only, parallel to items
    int len;
    int cap;
};

static void dex_jv_destroy(void* ptr) {
    DexJsonValue* v = (DexJsonValue*)ptr;
    if (v->str) dex_release(v->str);
    for (int i = 0; i < v->len; i++) {
        if (v->items && v->items[i]) dex_release(v->items[i]);
        if (v->keys && v->keys[i]) dex_release(v->keys[i]);
    }
    free(v->items);
    free(v->keys);
}

static DexJsonValue* dex_jv_alloc(int kind) {
    DexJsonValue* v = (DexJsonValue*)dex_obj_alloc(sizeof(DexJsonValue), dex_jv_destroy);
    v->kind = kind;
    v->b = 0;
    v->num = 0.0;
    v->is_int = 0;
    v->inum = 0;
    v->str = NULL;
    v->items = NULL;
    v->keys = NULL;
    v->len = 0;
    v->cap = 0;
    return v;
}

DexJsonValue* dex_jv_null(void) { return dex_jv_alloc(DEX_JV_NULL); }

DexJsonValue* dex_jv_bool(_Bool b) {
    DexJsonValue* v = dex_jv_alloc(DEX_JV_BOOL);
    v->b = b;
    return v;
}

DexJsonValue* dex_jv_int(long n) {
    DexJsonValue* v = dex_jv_alloc(DEX_JV_NUM);
    v->is_int = 1;
    v->inum = n;
    v->num = (double)n;
    return v;
}

DexJsonValue* dex_jv_double(double d) {
    DexJsonValue* v = dex_jv_alloc(DEX_JV_NUM);
    v->num = d;
    v->inum = (long)d;
    return v;
}

// Takes a reference to `s` rather than copying: string values are immutable, so
// sharing the buffer is safe and keeps literal-heavy code allocation-light.
DexJsonValue* dex_jv_string(DexString* s) {
    DexJsonValue* v = dex_jv_alloc(DEX_JV_STR);
    if (s) { dex_retain(s); v->str = s; }
    else v->str = dex_string_empty();
    return v;
}

// Takes ownership of `s` instead of retaining it, for callers handing over a
// string they just built. Using dex_jv_string there would leave the caller's
// reference with nobody to drop it.
DexJsonValue* dex_jv_string_owned(DexString* s) {
    DexJsonValue* v = dex_jv_alloc(DEX_JV_STR);
    v->str = s ? s : dex_string_empty();
    return v;
}

DexJsonValue* dex_jv_string_cstr(const char* s) {
    DexJsonValue* v = dex_jv_alloc(DEX_JV_STR);
    v->str = dex_string_from_lit(s ? s : "");
    return v;
}

DexJsonValue* dex_jv_array(void) { return dex_jv_alloc(DEX_JV_ARR); }
DexJsonValue* dex_jv_object(void) { return dex_jv_alloc(DEX_JV_OBJ); }

static int dex_jv_reserve(DexJsonValue* v, int want) {
    if (want <= v->cap) return 1;
    int cap = v->cap ? v->cap * 2 : 8;
    while (cap < want) cap *= 2;
    DexJsonValue** items = (DexJsonValue**)realloc(v->items, (size_t)cap * sizeof(DexJsonValue*));
    if (!items) return 0;
    v->items = items;
    if (v->kind == DEX_JV_OBJ) {
        DexString** keys = (DexString**)realloc(v->keys, (size_t)cap * sizeof(DexString*));
        if (!keys) return 0;
        v->keys = keys;
    }
    v->cap = cap;
    return 1;
}

// Appends to an array. Takes ownership of `child` — callers pass a value they
// have just constructed and do not release it themselves.
DexJsonValue* dex_jv_push(DexJsonValue* arr, DexJsonValue* child) {
    if (!arr || arr->kind != DEX_JV_ARR) { dex_release(child); return arr; }
    if (!dex_jv_reserve(arr, arr->len + 1)) { dex_release(child); return arr; }
    arr->items[arr->len++] = child;
    return arr;
}

// Sets a key on an object, replacing an existing entry with the same key so a
// literal with a repeated key keeps last-wins semantics. Takes ownership of
// `child`; borrows `key`.
DexJsonValue* dex_jv_put(DexJsonValue* obj, DexString* key, DexJsonValue* child) {
    if (!obj || obj->kind != DEX_JV_OBJ || !key) { dex_release(child); return obj; }
    for (int i = 0; i < obj->len; i++) {
        if (obj->keys[i] && obj->keys[i]->len == key->len &&
            memcmp(obj->keys[i]->data, key->data, key->len) == 0) {
            dex_release(obj->items[i]);
            obj->items[i] = child;
            return obj;
        }
    }
    if (!dex_jv_reserve(obj, obj->len + 1)) { dex_release(child); return obj; }
    dex_retain(key);
    obj->keys[obj->len] = key;
    obj->items[obj->len] = child;
    obj->len++;
    return obj;
}

DexJsonValue* dex_jv_put_cstr(DexJsonValue* obj, const char* key, DexJsonValue* child) {
    DexString* k = dex_string_from_lit(key ? key : "");
    dex_jv_put(obj, k, child);
    dex_release(k);
    return obj;
}

// --- Reading ---

// Missing lookups yield a fresh null rather than NULL, so `v["absent"].int()`
// gives 0 instead of crashing. Every accessor below therefore has a total,
// zero-valued answer for the wrong shape, matching how struct decode already
// treats absent keys.
DexJsonValue* dex_jv_index(DexJsonValue* v, int i) {
    if (!v || v->kind != DEX_JV_ARR || i < 0 || i >= v->len) return dex_jv_null();
    dex_retain(v->items[i]);
    return v->items[i];
}

DexJsonValue* dex_jv_get(DexJsonValue* v, DexString* key) {
    if (!v || v->kind != DEX_JV_OBJ || !key) return dex_jv_null();
    for (int i = 0; i < v->len; i++) {
        if (v->keys[i] && v->keys[i]->len == key->len &&
            memcmp(v->keys[i]->data, key->data, key->len) == 0) {
            dex_retain(v->items[i]);
            return v->items[i];
        }
    }
    return dex_jv_null();
}

int dex_jv_len(DexJsonValue* v) {
    if (!v) return 0;
    if (v->kind == DEX_JV_ARR || v->kind == DEX_JV_OBJ) return v->len;
    if (v->kind == DEX_JV_STR) return (int)v->str->len;
    return 0;
}

_Bool dex_jv_has(DexJsonValue* v, DexString* key) {
    if (!v || v->kind != DEX_JV_OBJ || !key) return 0;
    for (int i = 0; i < v->len; i++) {
        if (v->keys[i] && v->keys[i]->len == key->len &&
            memcmp(v->keys[i]->data, key->data, key->len) == 0) return 1;
    }
    return 0;
}

// Only available when the array runtime is emitted, which codegen signals with
// DEX_HAVE_ARRAYS. A program that names json.Value but never touches an array
// would otherwise fail to compile on a function it never calls.
#ifdef DEX_HAVE_ARRAYS
DexArrayString* dex_jv_keys(DexJsonValue* v) {
    DexArrayString* out = dex_array_string_new();
    if (!v || v->kind != DEX_JV_OBJ) return out;
    for (int i = 0; i < v->len; i++) {
        dex_array_string_push(out, v->keys[i]);
    }
    return out;
}
#endif

// A number reads as a string and vice versa: wire formats routinely disagree
// about whether an id is quoted, and forcing every call site to branch on that
// is exactly the friction this type exists to remove.
long dex_jv_as_long(DexJsonValue* v) {
    if (!v) return 0;
    switch (v->kind) {
    case DEX_JV_NUM:  return v->is_int ? v->inum : (long)v->num;
    case DEX_JV_BOOL: return v->b ? 1 : 0;
    case DEX_JV_STR:  return atol(v->str->data);
    default: return 0;
    }
}

int dex_jv_as_int(DexJsonValue* v) { return (int)dex_jv_as_long(v); }

double dex_jv_as_double(DexJsonValue* v) {
    if (!v) return 0.0;
    switch (v->kind) {
    case DEX_JV_NUM:  return v->is_int ? (double)v->inum : v->num;
    case DEX_JV_BOOL: return v->b ? 1.0 : 0.0;
    case DEX_JV_STR:  return atof(v->str->data);
    default: return 0.0;
    }
}

_Bool dex_jv_as_bool(DexJsonValue* v) {
    if (!v) return 0;
    switch (v->kind) {
    case DEX_JV_BOOL: return v->b;
    case DEX_JV_NUM:  return dex_jv_as_double(v) != 0.0;
    case DEX_JV_STR:  return strcmp(v->str->data, "true") == 0;
    default: return 0;
    }
}

static const char* dex_jv_encode_cstr(DexJsonValue* v);

// A string value returns its contents; anything else returns its JSON text, so
// `.string()` on an object gives you the object rather than an empty string.
DexString* dex_jv_as_string(DexJsonValue* v) {
    if (!v) return dex_string_empty();
    if (v->kind == DEX_JV_STR) { dex_retain(v->str); return v->str; }
    if (v->kind == DEX_JV_NULL) return dex_string_empty();
    return dex_string_from_cstr(dex_jv_encode_cstr(v));
}

_Bool dex_jv_is_null(DexJsonValue* v)   { return !v || v->kind == DEX_JV_NULL; }
_Bool dex_jv_is_bool(DexJsonValue* v)   { return v && v->kind == DEX_JV_BOOL; }
_Bool dex_jv_is_number(DexJsonValue* v) { return v && v->kind == DEX_JV_NUM; }
_Bool dex_jv_is_string(DexJsonValue* v) { return v && v->kind == DEX_JV_STR; }
_Bool dex_jv_is_array(DexJsonValue* v)  { return v && v->kind == DEX_JV_ARR; }
_Bool dex_jv_is_object(DexJsonValue* v) { return v && v->kind == DEX_JV_OBJ; }

// --- Encoding ---

typedef struct {
    char* buf;
    size_t cap;
    size_t len;
    int failed;
} DexJvBuf;

static int dex_jv_buf_need(DexJvBuf* b, size_t extra) {
    if (b->failed) return 0;
    if (b->len + extra + 1 <= b->cap) return 1;
    size_t cap = b->cap ? b->cap : 128;
    while (cap < b->len + extra + 1) cap *= 2;
    char* nb = (char*)realloc(b->buf, cap);
    if (!nb) { b->failed = 1; return 0; }
    b->buf = nb;
    b->cap = cap;
    return 1;
}

static void dex_jv_buf_put(DexJvBuf* b, const char* s, size_t n) {
    if (!dex_jv_buf_need(b, n)) return;
    memcpy(b->buf + b->len, s, n);
    b->len += n;
}

static void dex_jv_buf_char(DexJvBuf* b, char c) {
    if (!dex_jv_buf_need(b, 1)) return;
    b->buf[b->len++] = c;
}

static void dex_jv_buf_quoted(DexJvBuf* b, const char* s, size_t n) {
    dex_jv_buf_char(b, '"');
    for (size_t i = 0; i < n; i++) {
        unsigned char c = (unsigned char)s[i];
        switch (c) {
        case '"':  dex_jv_buf_put(b, "\\\"", 2); break;
        case '\\': dex_jv_buf_put(b, "\\\\", 2); break;
        case '\n': dex_jv_buf_put(b, "\\n", 2); break;
        case '\r': dex_jv_buf_put(b, "\\r", 2); break;
        case '\t': dex_jv_buf_put(b, "\\t", 2); break;
        case '\b': dex_jv_buf_put(b, "\\b", 2); break;
        case '\f': dex_jv_buf_put(b, "\\f", 2); break;
        default:
            if (c < 0x20) {
                char esc[7];
                snprintf(esc, sizeof(esc), "\\u%04x", c);
                dex_jv_buf_put(b, esc, 6);
            } else {
                dex_jv_buf_char(b, (char)c);
            }
        }
    }
    dex_jv_buf_char(b, '"');
}

static void dex_jv_write(DexJvBuf* b, DexJsonValue* v) {
    if (!v) { dex_jv_buf_put(b, "null", 4); return; }
    switch (v->kind) {
    case DEX_JV_NULL: dex_jv_buf_put(b, "null", 4); break;
    case DEX_JV_BOOL:
        if (v->b) dex_jv_buf_put(b, "true", 4);
        else dex_jv_buf_put(b, "false", 5);
        break;
    case DEX_JV_NUM: {
        char tmp[40];
        int n;
        if (v->is_int) n = snprintf(tmp, sizeof(tmp), "%ld", v->inum);
        else n = snprintf(tmp, sizeof(tmp), "%.17g", v->num);
        dex_jv_buf_put(b, tmp, (size_t)n);
        break;
    }
    case DEX_JV_STR: dex_jv_buf_quoted(b, v->str->data, v->str->len); break;
    case DEX_JV_ARR:
        dex_jv_buf_char(b, '[');
        for (int i = 0; i < v->len; i++) {
            if (i) dex_jv_buf_char(b, ',');
            dex_jv_write(b, v->items[i]);
        }
        dex_jv_buf_char(b, ']');
        break;
    case DEX_JV_OBJ:
        dex_jv_buf_char(b, '{');
        for (int i = 0; i < v->len; i++) {
            if (i) dex_jv_buf_char(b, ',');
            dex_jv_buf_quoted(b, v->keys[i]->data, v->keys[i]->len);
            dex_jv_buf_char(b, ':');
            dex_jv_write(b, v->items[i]);
        }
        dex_jv_buf_char(b, '}');
        break;
    }
}

static const char* dex_jv_encode_cstr(DexJsonValue* v) {
    DexJvBuf b = {NULL, 0, 0, 0};
    dex_jv_write(&b, v);
    if (b.failed || !b.buf) { free(b.buf); return strdup("null"); }
    b.buf[b.len] = '\0';
    return b.buf;
}

DexString* dex_jv_encode(DexJsonValue* v) {
    return dex_string_from_cstr(dex_jv_encode_cstr(v));
}

// --- Parsing ---

typedef struct {
    const char* p;
    int failed;
    int depth;
} DexJvParser;

// Bounds recursion so a hostile payload of nested brackets cannot exhaust the C
// stack. Wire input reaching this parser is untrusted by definition.
#define DEX_JV_MAX_DEPTH 200

static DexJsonValue* dex_jv_parse_value(DexJvParser* ps);

static void dex_jv_skip_ws(DexJvParser* ps) {
    while (*ps->p == ' ' || *ps->p == '\t' || *ps->p == '\n' || *ps->p == '\r') ps->p++;
}

static DexString* dex_jv_parse_string(DexJvParser* ps) {
    // The decoding itself lives in core runtime, shared with the json module's
    // dex_json_get — one implementation, so the two cannot drift apart.
    const char* cursor = ps->p;
    char* buf = NULL;
    size_t n = 0;
    if (!dex_json_unescape_string(&cursor, &buf, &n)) {
        ps->failed = 1;
        return dex_string_empty();
    }
    ps->p = cursor;
    DexString* s = dex_string_new(buf, n);
    free(buf);
    return s;
}

static DexJsonValue* dex_jv_parse_number(DexJvParser* ps) {
    const char* start = ps->p;
    if (*ps->p == '-' || *ps->p == '+') ps->p++;
    int is_int = 1;
    while (*ps->p) {
        char c = *ps->p;
        if (c >= '0' && c <= '9') { ps->p++; continue; }
        if (c == '.' || c == 'e' || c == 'E') { is_int = 0; ps->p++; continue; }
        if ((c == '-' || c == '+') && (ps->p[-1] == 'e' || ps->p[-1] == 'E')) { ps->p++; continue; }
        break;
    }
    if (ps->p == start) { ps->failed = 1; return dex_jv_null(); }
    char tmp[64];
    size_t n = (size_t)(ps->p - start);
    if (n >= sizeof(tmp)) n = sizeof(tmp) - 1;
    memcpy(tmp, start, n);
    tmp[n] = '\0';
    return is_int ? dex_jv_int(atol(tmp)) : dex_jv_double(atof(tmp));
}

static DexJsonValue* dex_jv_parse_value(DexJvParser* ps) {
    if (ps->depth >= DEX_JV_MAX_DEPTH) { ps->failed = 1; return dex_jv_null(); }
    dex_jv_skip_ws(ps);
    switch (*ps->p) {
    case '\0': ps->failed = 1; return dex_jv_null();
    case 'n':
        if (strncmp(ps->p, "null", 4) != 0) { ps->failed = 1; return dex_jv_null(); }
        ps->p += 4;
        return dex_jv_null();
    case 't':
        if (strncmp(ps->p, "true", 4) != 0) { ps->failed = 1; return dex_jv_null(); }
        ps->p += 4;
        return dex_jv_bool(1);
    case 'f':
        if (strncmp(ps->p, "false", 5) != 0) { ps->failed = 1; return dex_jv_null(); }
        ps->p += 5;
        return dex_jv_bool(0);
    case '"': {
        DexString* s = dex_jv_parse_string(ps);
        DexJsonValue* v = dex_jv_string(s);
        dex_release(s);
        return v;
    }
    case '[': {
        ps->p++;
        ps->depth++;
        DexJsonValue* arr = dex_jv_array();
        dex_jv_skip_ws(ps);
        if (*ps->p == ']') { ps->p++; ps->depth--; return arr; }
        for (;;) {
            DexJsonValue* child = dex_jv_parse_value(ps);
            if (ps->failed) { dex_release(child); dex_release(arr); ps->depth--; return dex_jv_null(); }
            dex_jv_push(arr, child);
            dex_jv_skip_ws(ps);
            if (*ps->p == ',') { ps->p++; continue; }
            if (*ps->p == ']') { ps->p++; break; }
            ps->failed = 1;
            dex_release(arr);
            ps->depth--;
            return dex_jv_null();
        }
        ps->depth--;
        return arr;
    }
    case '{': {
        ps->p++;
        ps->depth++;
        DexJsonValue* obj = dex_jv_object();
        dex_jv_skip_ws(ps);
        if (*ps->p == '}') { ps->p++; ps->depth--; return obj; }
        for (;;) {
            dex_jv_skip_ws(ps);
            DexString* key = dex_jv_parse_string(ps);
            if (ps->failed) { dex_release(key); dex_release(obj); ps->depth--; return dex_jv_null(); }
            dex_jv_skip_ws(ps);
            if (*ps->p != ':') { dex_release(key); dex_release(obj); ps->failed = 1; ps->depth--; return dex_jv_null(); }
            ps->p++;
            DexJsonValue* child = dex_jv_parse_value(ps);
            if (ps->failed) { dex_release(key); dex_release(child); dex_release(obj); ps->depth--; return dex_jv_null(); }
            dex_jv_put(obj, key, child);
            dex_release(key);
            dex_jv_skip_ws(ps);
            if (*ps->p == ',') { ps->p++; continue; }
            if (*ps->p == '}') { ps->p++; break; }
            ps->failed = 1;
            dex_release(obj);
            ps->depth--;
            return dex_jv_null();
        }
        ps->depth--;
        return obj;
    }
    default:
        return dex_jv_parse_number(ps);
    }
}

// Parses JSON text. Malformed input yields a null value rather than a crash, so
// `json.decode(...).isNull()` is the check for "this was not JSON". Trailing
// content after the top-level value is rejected — `{} garbage` is not valid.
DexJsonValue* dex_jv_parse(const char* text) {
    if (!text) return dex_jv_null();
    DexJvParser ps = {text, 0, 0};
    DexJsonValue* v = dex_jv_parse_value(&ps);
    if (!ps.failed) {
        dex_jv_skip_ws(&ps);
        if (*ps.p != '\0') ps.failed = 1;
    }
    if (ps.failed) { dex_release(v); return dex_jv_null(); }
    return v;
}

DexJsonValue* dex_jv_parse_str(DexString* s) {
    return dex_jv_parse(s ? s->data : "");
}
